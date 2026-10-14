package service

import (
	"context"
	"fmt"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/markdown"
	"github.com/pentaqilo/netsidian/internal/ports"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type vaultLoadJob struct {
	status ports.VaultLoad
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *Services) StartLocalVaultLoad(ctx context.Context, a *Actor, root string) (*domain.Space, error) {
	sp, root, err := s.prepareLocalVault(ctx, a, root)
	if err != nil {
		return nil, err
	}
	lock := s.spaceMu(sp.ID)
	lock.Lock()
	defer lock.Unlock()
	if err = s.startVaultLoad(a, sp, root); err != nil {
		return sp, err
	}
	return sp, nil
}

func (s *Services) startVaultLoad(a *Actor, sp *domain.Space, root string) error {
	ctx, cancel := context.WithCancel(context.Background())
	job := &vaultLoadJob{status: ports.VaultLoad{SpaceID: sp.ID, UserID: a.UserID, Phase: "queued", UpdatedAt: time.Now()}, cancel: cancel, done: make(chan struct{})}
	s.mu.Lock()
	if old := s.loads[sp.ID]; old != nil && old.status.Active() {
		s.mu.Unlock()
		cancel()
		return domain.Conflict("vault is already loading")
	}
	s.loads[sp.ID] = job
	s.mu.Unlock()
	if repo, ok := s.Docs.(ports.VaultLoadRepo); ok {
		if err := repo.SaveVaultLoad(ctx, &job.status); err != nil {
			cancel()
			close(job.done)
			s.mu.Lock()
			delete(s.loads, sp.ID)
			s.mu.Unlock()
			return err
		}
	}
	actor := *a
	go func() {
		defer close(job.done)
		defer cancel()
		select {
		case s.loadSlots <- struct{}{}:
			defer func() { <-s.loadSlots }()
		case <-ctx.Done():
			return
		}
		lastSave := time.Time{}
		progress := func(phase string, completed, total, skipped int, message string) {
			s.mu.Lock()
			changed := job.status.Phase != phase
			job.status.Phase = phase
			job.status.Completed = completed
			job.status.Total = total
			job.status.Skipped = skipped
			job.status.Error = message
			job.status.UpdatedAt = time.Now()
			snapshot := job.status
			s.mu.Unlock()
			if changed || time.Since(lastSave) > time.Second {
				if repo, ok := s.Docs.(ports.VaultLoadRepo); ok {
					_ = repo.SaveVaultLoad(ctx, &snapshot)
				}
				lastSave = time.Now()
			}
		}
		count, err := s.scanLocalVault(ctx, &actor, sp, root, progress)
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		status := job.status
		s.mu.Unlock()
		if err != nil {
			progress("error", status.Completed, status.Total, status.Skipped, err.Error())
		} else {
			progress("ready", count, status.Total, status.Skipped, "")
		}
		s.PublishEvent(&domain.Event{SpaceID: sp.ID, EventType: "vault.loaded"})
	}()
	return nil
}

func (s *Services) VaultLoadStatus(ctx context.Context, a *Actor, space string) (*ports.VaultLoad, error) {
	if _, err := s.Require(ctx, a, space, domain.ActionRead); err != nil {
		return nil, err
	}
	s.mu.Lock()
	job := s.loads[space]
	if job != nil {
		copy := job.status
		s.mu.Unlock()
		return &copy, nil
	}
	s.mu.Unlock()
	if repo, ok := s.Docs.(ports.VaultLoadRepo); ok {
		return repo.VaultLoad(ctx, space)
	}
	return nil, nil
}
func (s *Services) vaultLoading(space string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads[space] != nil && s.loads[space].status.Active()
}
func (s *Services) cancelVaultLoad(space string) {
	s.mu.Lock()
	job := s.loads[space]
	s.mu.Unlock()
	if job != nil {
		job.cancel()
		<-job.done
		s.mu.Lock()
		delete(s.loads, space)
		s.mu.Unlock()
	}
}
func (s *Services) RetryVaultLoad(ctx context.Context, a *Actor, space string) error {
	lock := s.spaceMu(space)
	lock.Lock()
	defer lock.Unlock()
	role, err := s.Role(ctx, a, space)
	if err != nil {
		return err
	}
	if a.Key != nil || role != domain.RoleOwner {
		return domain.Forbidden("vault owner required")
	}
	sp, err := s.Spaces.Space(ctx, space)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(sp.StorageBinding, "local-folder:") {
		return domain.Invalid("not a local folder vault")
	}
	return s.startVaultLoad(a, sp, strings.TrimPrefix(sp.StorageBinding, "local-folder:"))
}

// Persisted incomplete scans resume after a restart, reusing committed documents.
func (s *Services) ResumeVaultLoads(ctx context.Context) {
	repo, ok := s.Docs.(ports.VaultLoadRepo)
	if !ok {
		return
	}
	jobs, err := repo.PendingVaultLoads(ctx)
	if err != nil {
		return
	}
	for _, job := range jobs {
		sp, err := s.Spaces.Space(ctx, job.SpaceID)
		if err == nil && sp != nil && strings.HasPrefix(sp.StorageBinding, "local-folder:") {
			_ = s.startVaultLoad(&Actor{UserID: job.UserID}, sp, strings.TrimPrefix(sp.StorageBinding, "local-folder:"))
		}
	}
}

type loadProgress func(phase string, completed, total, skipped int, message string)

func (s *Services) scanLocalVault(ctx context.Context, a *Actor, sp *domain.Space, root string, progress loadProgress) (int, error) {
	report := func(phase string, completed, total, skipped int) {
		if progress != nil {
			progress(phase, completed, total, skipped, "")
		}
	}
	report("scanning", 0, 0, 0)
	existing, err := s.Docs.DocumentsInSpace(ctx, sp.ID, true)
	if err != nil {
		return 0, err
	}
	byKey := map[string]*domain.Document{}
	for _, d := range existing {
		byKey[d.NormKey] = d
	}
	dirty := map[string]bool{}
	if repo, ok := s.Index.(interface {
		DocumentsNeedingIndex(context.Context, string) ([]*domain.Document, error)
	}); ok {
		docs, err := repo.DocumentsNeedingIndex(ctx, sp.ID)
		if err != nil {
			return 0, err
		}
		for _, d := range docs {
			dirty[d.ID] = true
		}
	} else {
		for _, d := range existing {
			dirty[d.ID] = true
		}
	}
	type candidate struct{ full, canon, key string }
	var files []candidate
	skipped := 0
	last := time.Now()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".obsidian", ".netsidian", ".netsidian-trash", "node_modules", ".trash":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		canon, key, invalid := domain.NormalizePath(filepath.ToSlash(rel))
		info, err := d.Info()
		if invalid != nil || err != nil || !info.Mode().IsRegular() || info.Size() > s.Cfg.MaxNoteBytes {
			skipped++
			return nil
		}
		files = append(files, candidate{path, canon, key})
		if time.Since(last) > 250*time.Millisecond {
			report("scanning", 0, len(files), skipped)
			last = time.Now()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	count := 0
	bulk, bulkOK := s.Index.(ports.DocumentImportRepo)
	var pending []ports.ImportedDocument
	pendingBytes := 0
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		if err := bulk.ImportDocumentBatch(ctx, sp.ID, pending); err != nil {
			return err
		}
		pending = nil
		pendingBytes = 0
		return nil
	}
	report("indexing", 0, len(files), skipped)
	for i, file := range files {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		doc := byKey[file.key]
		if doc != nil && !dirty[doc.ID] {
			count++
			report("indexing", i+1, len(files), skipped)
			continue
		}
		// Bounded reads prevent an unexpectedly large .md file exhausting memory.
		handle, err := os.Open(file.full)
		if err != nil {
			skipped++
			report("indexing", i+1, len(files), skipped)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(handle, s.Cfg.MaxNoteBytes+1))
		handle.Close()
		if err != nil || int64(len(data)) > s.Cfg.MaxNoteBytes {
			skipped++
			report("indexing", i+1, len(files), skipped)
			continue
		}
		var revision *domain.Revision
		if doc == nil {
			id, rev := domain.NewID("doc"), domain.NewID("rev")
			now := time.Now()
			hist, err := s.State.SaveHistory(sp.ID, id, rev, data)
			if err != nil {
				return count, err
			}
			doc = &domain.Document{ID: id, SpaceID: sp.ID, Path: file.canon, NormKey: file.key, Kind: "note", CurrentRevision: rev, CreatedAt: now, UpdatedAt: now}
			revision = &domain.Revision{ID: rev, DocumentID: id, SHA256: shaHex(data), ByteLength: int64(len(data)), Creator: a.UserID, HistoryLocation: hist, CreatedAt: now}
			if !bulkOK {
				err = s.Docs.InsertDocument(ctx, doc, revision)
			}
			if err != nil {
				return count, err
			}
			byKey[file.key] = doc
		}
		idx := markdown.Parse(string(data), domain.BaseName(doc.Path))
		if bulkOK {
			pending = append(pending, ports.ImportedDocument{Document: doc, Revision: revision, Index: idx})
			pendingBytes += len(data)
			if len(pending) >= 64 || pendingBytes >= 8<<20 {
				err = flush()
			}
		} else if repo, ok := s.Index.(interface {
			ReplaceDocumentIndexDeferred(context.Context, string, *domain.Document, string, *ports.IndexData) error
		}); ok {
			err = repo.ReplaceDocumentIndexDeferred(ctx, sp.ID, doc, doc.CurrentRevision, idx)
		} else {
			err = s.Index.ReplaceDocumentIndex(ctx, sp.ID, doc, doc.CurrentRevision, idx)
		}
		if err != nil {
			return count, fmt.Errorf("index %s: %w", doc.Path, err)
		}
		count++
		report("indexing", i+1, len(files), skipped)
	}
	if bulkOK {
		if err := flush(); err != nil {
			return count, err
		}
	}
	report("resolving", len(files), len(files), skipped)
	if err := s.Index.ResolveLinks(ctx, sp.ID); err != nil {
		return count, err
	}
	return count, nil
}

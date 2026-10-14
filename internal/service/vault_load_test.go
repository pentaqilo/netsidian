package service

import (
	"context"
	"fmt"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type loadIndexProbe struct {
	ports.IndexRepo
	resolves atomic.Int32
	indexed  atomic.Int32
}

func (p *loadIndexProbe) ImportDocumentBatch(ctx context.Context, space string, documents []ports.ImportedDocument) error {
	p.indexed.Add(int32(len(documents)))
	return p.IndexRepo.(ports.DocumentImportRepo).ImportDocumentBatch(ctx, space, documents)
}
func (p *loadIndexProbe) ResolveLinks(ctx context.Context, space string) error {
	p.resolves.Add(1)
	return p.IndexRepo.ResolveLinks(ctx, space)
}
func (p *loadIndexProbe) ReplaceDocumentIndexDeferred(ctx context.Context, space string, d *domain.Document, rev string, index *ports.IndexData) error {
	p.indexed.Add(1)
	return p.IndexRepo.(interface {
		ReplaceDocumentIndexDeferred(context.Context, string, *domain.Document, string, *ports.IndexData) error
	}).ReplaceDocumentIndexDeferred(ctx, space, d, rev, index)
}
func (p *loadIndexProbe) DocumentsNeedingIndex(ctx context.Context, space string) ([]*domain.Document, error) {
	return p.IndexRepo.(interface {
		DocumentsNeedingIndex(context.Context, string) ([]*domain.Document, error)
	}).DocumentsNeedingIndex(ctx, space)
}
func waitVaultReady(t *testing.T, s *Services, a *Actor, space string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		load, err := s.VaultLoadStatus(context.Background(), a, space)
		if err != nil {
			t.Fatal(err)
		}
		if load != nil && load.Phase == "error" {
			t.Fatal(load.Error)
		}
		if load != nil && load.Phase == "ready" {
			s.mu.Lock()
			job := s.loads[space]
			s.mu.Unlock()
			<-job.done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("vault did not finish loading")
}
func TestLargeVaultLoadsInBackgroundAndResolvesLinksOnce(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	root := t.TempDir()
	const count = 3000
	for i := 0; i < count; i++ {
		dir := filepath.Join(root, fmt.Sprintf("Folder-%03d", i/30))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		text := fmt.Sprintf("# Note %d\n\n#large\n[[/Folder-000/Note-0000]]", i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("Note-%04d.md", i)), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	probe := &loadIndexProbe{IndexRepo: h.svc.Index}
	h.svc.Index = probe
	h.svc.loadSlots <- struct{}{} // Deterministically keep work queued until the response is checked.
	started := time.Now()
	sp, err := h.svc.StartLocalVaultLoad(ctx, h.actor, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.svc.cancelVaultLoad(sp.ID) })
	status, err := h.svc.VaultLoadStatus(ctx, h.actor, sp.ID)
	if err != nil || status.Phase != "queued" {
		t.Fatalf("queued status: %+v %v", status, err)
	}
	if _, err = h.svc.CreateNote(ctx, h.actor, sp.ID, "Race.md", "# Race", "", false); err == nil {
		t.Fatal("mutation allowed during initial indexing")
	}
	if _, err = h.svc.VaultLoadStatus(ctx, &Actor{UserID: "outsider"}, sp.ID); err == nil {
		t.Fatal("outsider can see import status")
	}
	<-h.svc.loadSlots
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err = h.svc.ListDocuments(readCtx, h.actor, h.space, false); err != nil {
		t.Fatal("other vault blocked by import:", err)
	}
	waitVaultReady(t, h.svc, h.actor, sp.ID)
	if probe.resolves.Load() != 1 || probe.indexed.Load() != count {
		t.Fatalf("indexed=%d whole-vault resolutions=%d", probe.indexed.Load(), probe.resolves.Load())
	}
	pageRepo := h.svc.Docs.(ports.DocumentPageRepo)
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		page, err := pageRepo.DocumentMetadataPage(ctx, sp.ID, false, cursor, 400)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != count || len(page.Documents) > 400 {
			t.Fatalf("bad page: %+v", page)
		}
		for _, d := range page.Documents {
			if seen[d.ID] {
				t.Fatal("duplicate in keyset pages")
			}
			seen[d.ID] = true
			if len(d.Tags) == 0 {
				t.Fatal("tags missing from paged metadata")
			}
		}
		pages++
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != count || pages != 8 {
		t.Fatalf("listed %d documents in %d pages", len(seen), pages)
	}
	docs, err := h.svc.Docs.DocumentsInSpace(ctx, sp.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	links, err := h.svc.Backlinks(ctx, h.actor, sp.ID, docs[0].ID)
	if err != nil || len(links) != count-1 {
		t.Fatalf("bulk links not resolved: %d %v", len(links), err)
	}
	t.Logf("loaded and verified %d notes in %s", count, time.Since(started))
}
func TestInterruptedVaultLoadResumesWithoutDuplicateDocuments(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	root := t.TempDir()
	for _, name := range []string{"A.md", "B.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("# Note\n[[A]]"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sp, _, err := h.svc.AttachLocalVault(ctx, h.actor, root)
	if err != nil {
		t.Fatal(err)
	}
	docs, _ := h.svc.Docs.DocumentsInSpace(ctx, sp.ID, false)
	// Simulate a crash after committing a document but before writing its index.
	if err = h.svc.Index.RemoveDocumentIndex(ctx, docs[1].ID); err != nil {
		t.Fatal(err)
	}
	if err = h.svc.Docs.(ports.VaultLoadRepo).SaveVaultLoad(ctx, &ports.VaultLoad{SpaceID: sp.ID, UserID: h.actor.UserID, Phase: "indexing", Completed: 1, Total: 2, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	resumed := NewServices(h.svc.Cfg, h.svc.Users, h.svc.Spaces, h.svc.Invites, h.svc.Docs, h.svc.Index, h.svc.Mem, h.svc.Prefs, h.svc.Content, h.svc.State)
	resumed.ResumeVaultLoads(ctx)
	t.Cleanup(func() { resumed.cancelVaultLoad(sp.ID) })
	waitVaultReady(t, resumed, h.actor, sp.ID)
	after, _ := resumed.Docs.DocumentsInSpace(ctx, sp.ID, false)
	if len(after) != 2 || after[0].ID != docs[0].ID || after[1].CurrentRevision != docs[1].CurrentRevision {
		t.Fatal("resume duplicated or replaced committed documents")
	}
	missing, err := resumed.Index.(interface {
		DocumentsNeedingIndex(context.Context, string) ([]*domain.Document, error)
	}).DocumentsNeedingIndex(ctx, sp.ID)
	if err != nil || len(missing) != 0 {
		t.Fatalf("index not repaired: %v %v", missing, err)
	}
}
func TestDetachCancelsQueuedVaultLoadAndPreservesOriginalFiles(t *testing.T) {
	h := newHarness(t)
	root := t.TempDir()
	path := filepath.Join(root, "Original.md")
	if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	h.svc.loadSlots <- struct{}{}
	defer func() { <-h.svc.loadSlots }()
	sp, err := h.svc.StartLocalVaultLoad(context.Background(), h.actor, root)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.svc.DeleteVault(context.Background(), h.actor, sp.ID, sp.Name); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "unchanged" {
		t.Fatal("detach changed original folder")
	}
}

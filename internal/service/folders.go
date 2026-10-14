package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"strings"
	"time"
)

func (s *Services) ListFolders(ctx context.Context, a *Actor, space string) ([]string, error) {
	if _, err := s.Require(ctx, a, space, domain.ActionRead); err != nil {
		return nil, err
	}
	store, ok := s.Content.(ports.FolderStore)
	if !ok {
		return []string{}, nil
	}
	return store.Folders(space)
}
func (s *Services) CreateFolder(ctx context.Context, a *Actor, space, path string) error {
	if _, err := s.Require(ctx, a, space, domain.ActionCreate); err != nil {
		return err
	}
	canon, _, err := domain.NormalizePath(path)
	if err != nil {
		return err
	}
	if err := validFolderRel(canon); err != nil {
		return err
	}
	store, ok := s.Content.(ports.FolderStore)
	if !ok {
		return domain.Unsupported("storage does not support empty folders")
	}
	if err := store.MakeFolder(space, canon); err != nil {
		return err
	}
	s.PublishEvent(&domain.Event{SpaceID: space, EventType: "folder.created"})
	return nil
}

func validFolderRel(canon string) error {
	for _, part := range strings.Split(canon, "/") {
		if strings.HasPrefix(part, ".") {
			return domain.Invalid("reserved folder name")
		}
	}
	return nil
}

func underPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func folderStore(s *Services) (ports.FolderStore, error) {
	store, ok := s.Content.(ports.FolderStore)
	if !ok {
		return nil, domain.Unsupported("storage does not support folder operations")
	}
	return store, nil
}

// ---------- folder rename / move ----------

// FolderRenameResult reports the outcome of a folder rename/move.
type FolderRenameResult struct {
	Path     string   `json:"path"`
	Moved    int      `json:"moved"`
	Failures []string `json:"failures,omitempty"`
}

type folderMove struct {
	id      string
	from    string
	to      string
	key     string
	trashed bool
}

// RenameFolder relocates a folder and every document beneath it. Trashed
// documents keep their tombstones; their hidden trash bytes move too so
// a later restore lands inside the new folder path.
func (s *Services) RenameFolder(ctx context.Context, a *Actor, space, path, newPath string) (*FolderRenameResult, error) {
	if _, err := s.Require(ctx, a, space, domain.ActionEdit); err != nil {
		return nil, err
	}
	store, err := folderStore(s)
	if err != nil {
		return nil, err
	}
	from, _, nerr := domain.NormalizePath(path)
	if nerr != nil {
		return nil, nerr
	}
	to, _, nerr := domain.NormalizePath(newPath)
	if nerr != nil {
		return nil, nerr
	}
	if err := validFolderRel(from); err != nil {
		return nil, err
	}
	if err := validFolderRel(to); err != nil {
		return nil, err
	}
	if from == to {
		return &FolderRenameResult{Path: to}, nil
	}
	if underPrefix(to, from) {
		return nil, domain.Invalid("cannot move a folder into itself")
	}
	// Serialize the whole rename against saves, creates, document
	// renames/trashes, attachment writes and competing folder operations.
	// All metadata below is read inside this lock, so it cannot go stale
	// while the tree moves.
	smu := s.spaceMu(space)
	smu.Lock()
	defer smu.Unlock()
	folders, err := store.Folders(space)
	if err != nil {
		return nil, domain.Unavailable("folder listing failed")
	}
	has := false
	for _, f := range folders {
		if f == from {
			has = true
		}
		if f == to {
			return nil, domain.Conflict("a folder already exists at the target path")
		}
	}
	if !has {
		return nil, domain.NotFound("folder not found")
	}
	docs, err := s.Docs.DocumentsInSpace(ctx, space, true)
	if err != nil {
		return nil, err
	}
	// plan + collision pre-checks
	var moves []folderMove
	for _, d := range docs {
		if !underPrefix(d.Path, from) {
			continue
		}
		m := folderMove{id: d.ID, from: d.Path, to: to + strings.TrimPrefix(d.Path, from), trashed: d.TombstoneAt != nil}
		if !m.trashed {
			canonTo, key, perr := domain.NormalizePath(m.to)
			if perr != nil {
				return nil, perr
			}
			m.to, m.key = canonTo, key
			existing, derr := s.Docs.DocumentByNormKey(ctx, space, key)
			if derr != nil {
				return nil, derr
			}
			if existing != nil && existing.ID != d.ID && !underPrefix(existing.Path, from) {
				return nil, domain.Conflict("target path already exists: " + m.to)
			}
		}
		moves = append(moves, m)
	}
	// journal a pending rename op per active document before the tree
	// moves: a crash between the storage move and the metadata switch is
	// completed (or aborted) by ReconcileOperations at restart.
	renameOps := make(map[string]string, len(moves))
	for _, m := range moves {
		if m.trashed {
			continue
		}
		opID := domain.NewID("op")
		if err := s.Docs.InsertOperation(ctx, &domain.Operation{
			ID: opID, SpaceID: space, DocumentID: m.id, Principal: a.UserID,
			Kind: domain.OpKindFolderRename, Phase: domain.OpPhasePending,
			TargetPath: m.to, OriginPath: m.from, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}); err != nil {
			return nil, domain.Unavailable("could not record the rename operation")
		}
		renameOps[m.id] = opID
	}
	// move the folder tree first: one rename carries every active
	// document file and attachment in the subtree
	if err := store.MoveFolder(space, from, to); err != nil {
		for _, m := range moves {
			if !m.trashed {
				_ = s.Docs.SetOperationPhase(ctx, renameOps[m.id], domain.OpPhaseAborted, "tree move failed")
			}
		}
		if de, ok := err.(*domain.Error); ok {
			return nil, de
		}
		return nil, domain.Unavailable("rename failed at storage layer")
	}
	// switch active document metadata; any failure rolls the whole
	// rename back so exactly one consistent tree remains. Rollback
	// results are checked: an unrecovered element is surfaced loudly
	// instead of being swallowed.
	var done []folderMove
	rollback := func() []string {
		var unrecovered []string
		for i := len(done) - 1; i >= 0; i-- {
			m := done[i]
			_, key, perr := domain.NormalizePath(m.from)
			if perr != nil {
				unrecovered = append(unrecovered, m.to)
				continue
			}
			if err := s.Docs.SetDocumentPath(ctx, m.id, m.from, key, time.Now()); err != nil {
				// metadata could not be reverted: the journaled record stays
				// pending so reconciliation repairs or blocks on it later
				unrecovered = append(unrecovered, m.to)
			}
		}
		if err := store.MoveFolder(space, to, from); err != nil {
			// tree could not be reverted: pending records describe the
			// forward direction and reconciliation completes the rename
			unrecovered = append(unrecovered, to+" (folder tree)")
			return unrecovered
		}
		// the tree is back at its origin: close only records whose
		// metadata provably points at the origin path again
		for _, m := range moves {
			if m.trashed {
				continue
			}
			if d, derr := s.Docs.Document(ctx, m.id); derr == nil && d != nil && d.Path == m.from {
				_ = s.Docs.SetOperationPhase(ctx, renameOps[m.id], domain.OpPhaseAborted, "rename rolled back")
			}
		}
		return unrecovered
	}
	for _, m := range moves {
		if m.trashed {
			continue
		}
		if err := s.Docs.SetDocumentPath(ctx, m.id, m.to, m.key, time.Now()); err != nil {
			unrecovered := rollback()
			if len(unrecovered) > 0 {
				return nil, domain.Unavailable("rename failed and could not be fully rolled back; operator review required for: " + strings.Join(unrecovered, ", "))
			}
			return nil, domain.Unavailable("rename failed at metadata layer; rolled back: " + m.from)
		}
		done = append(done, m)
	}
	// close the records only once every metadata switch succeeded: while a
	// rollback may still follow, the operations must remain actionable
	for _, m := range done {
		_ = s.Docs.SetOperationPhase(ctx, renameOps[m.id], domain.OpPhaseCommitted, "")
	}
	// hidden trash bytes of tombstoned docs move too; their stored path
	// follows the bytes so a later Restore lands inside the new folder
	failures := []string{}
	for _, m := range moves {
		if !m.trashed {
			continue
		}
		if err := s.Content.Move(space, trashPrefix+m.id+"/"+m.from, trashPrefix+m.id+"/"+m.to); err != nil {
			failures = append(failures, m.from+" (trashed copy left behind)")
			continue
		}
		_, key, perr := domain.NormalizePath(m.to)
		if perr != nil {
			if rerr := s.Content.Move(space, trashPrefix+m.id+"/"+m.to, trashPrefix+m.id+"/"+m.from); rerr != nil {
				failures = append(failures, m.from+" (trashed copy inconsistent; operator review)")
			} else {
				failures = append(failures, m.from+" (trashed copy left behind)")
			}
			continue
		}
		if err := s.Docs.SetDocumentPath(ctx, m.id, m.to, key, time.Now()); err != nil {
			if rerr := s.Content.Move(space, trashPrefix+m.id+"/"+m.to, trashPrefix+m.id+"/"+m.from); rerr != nil {
				failures = append(failures, m.from+" (trashed copy inconsistent; operator review)")
			} else {
				failures = append(failures, m.from+" (trashed copy left behind)")
			}
		}
	}
	moved := 0
	for _, m := range moves {
		if !m.trashed {
			moved++
			s.Reindex(ctx, space, m.id)
		}
	}
	s.PublishEvent(&domain.Event{SpaceID: space, EventType: "folder.renamed"})
	return &FolderRenameResult{Path: to, Moved: moved, Failures: failures}, nil
}

// ---------- folder trash / restore ----------

// FolderTrashResult reports the outcome of a folder trash/restore.
type FolderTrashResult struct {
	TrashID   string   `json:"trashId,omitempty"`
	Path      string   `json:"path"`
	Trashed   int      `json:"trashed"`
	Restored  int      `json:"restored,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`
	Failures  []string `json:"failures,omitempty"`
}

// TrashFolder trashes every document under the folder (each with its own
// per-document tombstone) and relocates the remaining directory tree —
// attachments included — into the protected folder-trash area.
func (s *Services) TrashFolder(ctx context.Context, a *Actor, space, path string) (*FolderTrashResult, error) {
	if _, err := s.Require(ctx, a, space, domain.ActionTrash); err != nil {
		return nil, err
	}
	smu := s.spaceMu(space)
	smu.Lock()
	defer smu.Unlock()
	store, err := folderStore(s)
	if err != nil {
		return nil, err
	}
	canon, _, nerr := domain.NormalizePath(path)
	if nerr != nil {
		return nil, nerr
	}
	if err := validFolderRel(canon); err != nil {
		return nil, err
	}
	folders, err := store.Folders(space)
	if err != nil {
		return nil, domain.Unavailable("folder listing failed")
	}
	has := false
	for _, f := range folders {
		if f == canon {
			has = true
		}
	}
	if !has {
		return nil, domain.NotFound("folder not found")
	}
	docs, err := s.Docs.DocumentsInSpace(ctx, space, false)
	if err != nil {
		return nil, err
	}
	res := &FolderTrashResult{Path: canon}
	var docIDs []string
	now := time.Now()
	purge := now.AddDate(0, 0, s.Cfg.TrashRetentionDays)
	// activeLeft true means at least one document (or its bytes) stayed
	// active inside the folder; the directory tree must then stay in place
	// so live bytes are never hidden behind the stash.
	activeLeft := false
	for _, d := range docs {
		if !underPrefix(d.Path, canon) {
			continue
		}
		lock := s.docLock(d.ID)
		lock.Lock()
		tErr := s.Docs.SetDocumentTrash(ctx, d.ID, &now, &purge)
		if tErr != nil {
			res.Failures = append(res.Failures, d.Path+" (could not trash)")
			activeLeft = true
			lock.Unlock()
			continue
		}
		if mErr := s.Content.Move(space, d.Path, trashPrefix+d.ID+"/"+d.Path); mErr != nil {
			// bytes did not move; undo the tombstone so the document
			// stays consistently active with its content
			if rErr := s.Docs.SetDocumentTrash(ctx, d.ID, nil, nil); rErr != nil {
				res.Failures = append(res.Failures, d.Path+" (trashed but bytes stayed; operator review)")
			} else {
				res.Failures = append(res.Failures, d.Path+" (could not move bytes; left active)")
			}
			activeLeft = true
			lock.Unlock()
			continue
		}
		_ = s.Index.RemoveDocumentIndex(ctx, d.ID)
		docIDs = append(docIDs, d.ID)
		res.Trashed++
		lock.Unlock()
	}
	trashID := domain.NewID("ftr")
	if activeLeft {
		// some live bytes are still inside the folder: stashing the tree
		// would hide them; documents that were trashed keep their own
		// per-document trash records and remain individually restorable.
		res.Failures = append(res.Failures, canon+" (folder contents left in place; retry after resolving the listed documents)")
	} else if err := store.StashFolder(space, canon, trashID, docIDs); err != nil {
		if len(docIDs) == 0 {
			if de, ok := err.(*domain.Error); ok {
				return nil, de
			}
			return nil, domain.Unavailable("folder trash failed")
		}
		// documents are safely trashed; keep their bytes in the trash area
		res.Failures = append(res.Failures, canon+" (folder contents left in place)")
	} else {
		res.TrashID = trashID
	}
	_ = s.Index.ResolveLinks(ctx, space)
	s.PublishEvent(&domain.Event{SpaceID: space, EventType: "folder.trashed"})
	return res, nil
}

// ListFolderTrashes lists trashed folders awaiting restore.
func (s *Services) ListFolderTrashes(ctx context.Context, a *Actor, space string) ([]ports.FolderTrashEntry, error) {
	if _, err := s.Require(ctx, a, space, domain.ActionRead); err != nil {
		return nil, err
	}
	store, err := folderStore(s)
	if err != nil {
		return []ports.FolderTrashEntry{}, nil
	}
	return store.StashedFolders(space)
}

// RestoreFolder restores the documents that belonged to a trashed folder
// (per-document rules apply: path conflicts stay trashed and are reported)
// then merges the stashed directory contents — attachments included —
// back onto the original path.
func (s *Services) RestoreFolder(ctx context.Context, a *Actor, space, trashID string) (*FolderTrashResult, error) {
	if _, err := s.Require(ctx, a, space, domain.ActionRestore); err != nil {
		return nil, err
	}
	smu := s.spaceMu(space)
	smu.Lock()
	defer smu.Unlock()
	store, err := folderStore(s)
	if err != nil {
		return nil, err
	}
	entries, err := store.StashedFolders(space)
	if err != nil {
		return nil, domain.Unavailable("trash listing failed")
	}
	var entry *ports.FolderTrashEntry
	for i := range entries {
		if entries[i].TrashID == trashID {
			entry = &entries[i]
		}
	}
	if entry == nil {
		return nil, domain.NotFound("trashed folder not found")
	}
	if entry.Kind == "attachment" {
		if a.Key != nil && len(a.Key.Scopes) > 0 && !hasScope(a.Key.Scopes, domain.ScopeAttachmentsWrite) {
			return nil, domain.Forbidden("key lacks attachments:write")
		}
		if _, err := validateAttachmentPath(entry.Path); err != nil {
			return nil, err
		}
	}
	res := &FolderTrashResult{TrashID: trashID, Path: entry.Path}
	for _, docID := range entry.DocIDs {
		doc, derr := s.Docs.Document(ctx, docID)
		if derr != nil || doc == nil || doc.TombstoneAt == nil {
			continue // already restored or gone
		}
		if rerr := s.restoreDoc(ctx, space, docID); rerr != nil {
			res.Failures = append(res.Failures, doc.Path+" (stays trashed: restore failed)")
		} else {
			res.Restored++
		}
	}
	if p, conflicts, uerr := store.UnstashFolder(space, trashID); uerr != nil {
		res.Failures = append(res.Failures, entry.Path+" (folder contents could not be restored)")
	} else {
		res.Path = p
		if entry.Kind == "attachment" && len(conflicts) == 0 {
			res.Restored = 1
		}
		for _, c := range conflicts {
			res.Conflicts = append(res.Conflicts, c)
			res.Failures = append(res.Failures, c+" (target already exists; trashed original kept in trash)")
		}
	}
	_ = s.Index.ResolveLinks(ctx, space)
	s.PublishEvent(&domain.Event{SpaceID: space, EventType: "folder.restored"})
	return res, nil
}

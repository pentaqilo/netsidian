package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/markdown"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// createReqHash binds an idempotency key to the exact logical request,
// so a replay with different content is a conflict, not a silent reuse.
func createReqHash(spaceID, canon string, data []byte) string {
	return shaHex([]byte(fmt.Sprintf("create|%s|%s|%s", spaceID, canon, shaHex(data))))
}

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func canEdit(r domain.Role) bool {
	return r == domain.RoleEditor || r == domain.RoleOwner
}

// ---------- reads ----------

type NoteContent struct {
	Markdown string
	Revision string
	CanEdit  bool
	Kind     string
	Path     string
	Title    string
}

func (s *Services) ReadNote(ctx context.Context, a *Actor, spaceID, docID string) (*NoteContent, error) {
	role, err := s.Require(ctx, a, spaceID, domain.ActionRead)
	if err != nil {
		return nil, err
	}
	doc, err := s.Docs.Document(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.SpaceID != spaceID || doc.TombstoneAt != nil {
		return nil, domain.NotFound("document not found")
	}
	data, ferr := s.Content.Read(spaceID, doc.Path)
	if ferr == nil {
		// Coherence guard: while a journaled operation is in flight the
		// file may hold bytes the SQL pointer has not adopted yet; serve
		// the last committed revision from history in that window.
		if shaHex(data) != s.currentSHA(ctx, doc) {
			if alt := s.committedBytes(ctx, doc); alt != nil {
				data = alt
			}
		}
	} else if alt := s.committedBytes(ctx, doc); alt != nil {
		data = alt
	} else {
		return nil, domain.Unavailable("document bytes are temporarily unavailable")
	}
	title, _ := s.Index.TitleForDocument(ctx, docID)
	if title == "" {
		title = domain.BaseName(doc.Path)
	}
	return &NoteContent{
		Markdown: string(data), Revision: doc.CurrentRevision,
		CanEdit: canEdit(role) && !s.vaultLoading(spaceID) && (a.Key == nil || len(a.Key.Scopes) == 0 || hasScope(a.Key.Scopes, domain.ScopeDocumentsWrite)), Kind: doc.Kind, Path: doc.Path, Title: title,
	}, nil
}

func (s *Services) currentSHA(ctx context.Context, doc *domain.Document) string {
	rev, err := s.Docs.Revision(ctx, doc.CurrentRevision)
	if err != nil || rev == nil {
		return ""
	}
	return rev.SHA256
}

// committedBytes returns an immutable history copy when the working file
// is mid-swap or lost.
func (s *Services) committedBytes(ctx context.Context, doc *domain.Document) []byte {
	rev, err := s.Docs.Revision(ctx, doc.CurrentRevision)
	if err != nil || rev == nil || rev.HistoryLocation == "" {
		return nil
	}
	b, err := s.State.ReadAbsolute(rev.HistoryLocation)
	if err != nil {
		return nil
	}
	return b
}

// DocMeta is the list/tree shape.
type DocMeta struct {
	Tags            []string   `json:"tags,omitempty"`
	ID              string     `json:"id"`
	Path            string     `json:"path"`
	Kind            string     `json:"kind"`
	Title           string     `json:"title"`
	CurrentRevision string     `json:"currentRevision"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	TombstoneAt     *time.Time `json:"tombstoneAt,omitempty"`
}

// ListDocuments returns the file tree metadata for a space.
func (s *Services) ListDocuments(ctx context.Context, a *Actor, spaceID string, includeTrashed bool) ([]*DocMeta, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	docs, err := s.Docs.DocumentsInSpace(ctx, spaceID, includeTrashed)
	if err != nil {
		return nil, err
	}
	titles, _ := s.Index.SpaceTitles(ctx, spaceID)
	lite, _ := s.Index.DocsLite(ctx, spaceID)
	tags := map[string][]string{}
	for _, d := range lite {
		tags[d.ID] = d.Tags
	}
	out := make([]*DocMeta, 0, len(docs))
	for _, d := range docs {
		title := titles[d.ID]
		if title == "" {
			title = domain.BaseName(d.Path)
		}
		out = append(out, &DocMeta{
			ID: d.ID, Path: d.Path, Kind: d.Kind, Title: title, Tags: tags[d.ID],
			CurrentRevision: d.CurrentRevision, UpdatedAt: d.UpdatedAt, TombstoneAt: d.TombstoneAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// ---------- journaled writes ----------

type SaveResult struct {
	Revision  string `json:"revision"`
	Committed string `json:"committedAt"`
}

// CreateNote creates a document; requires documents:write and a fresh
// (collision-free) path. Idempotent with an Idempotency-Key.
func (s *Services) CreateNote(ctx context.Context, a *Actor, spaceID, path, content, idemKey string, isMemory bool) (*DocMeta, error) {
	return s.createNoteAs(ctx, a, spaceID, path, content, idemKey, isMemory, domain.ActionCreate)
}

// createNoteAs is CreateNote with an explicit gating action: memory
// proposals create their backing note under memory:propose so an agent
// key scoped to memory never needs documents:write.
func (s *Services) createNoteAs(ctx context.Context, a *Actor, spaceID, path, content, idemKey string, isMemory bool, act domain.Action) (*DocMeta, error) {
	if _, err := s.Require(ctx, a, spaceID, act); err != nil {
		return nil, err
	}
	data := []byte(content)
	if int64(len(data)) > s.Cfg.MaxNoteBytes {
		return nil, domain.TooLarge("note exceeds the configured maximum size")
	}
	canon, key, perr := domain.NormalizePath(path)
	if perr != nil {
		return nil, perr
	}
	lower := strings.ToLower(canon)
	if !domain.ValidNotePath(canon) && !strings.HasSuffix(lower, ".canvas") && !strings.HasSuffix(lower, ".base") {
		return nil, domain.Invalid("documents must use the .md extension")
	}
	// Serialize with saves, renames, trashes and folder operations so a
	// collision check cannot be undercut by a concurrent path change.
	smu := s.spaceMu(spaceID)
	smu.Lock()
	defer smu.Unlock()
	if idemKey != "" {
		op, err := s.Docs.FindOperationByIdempotency(ctx, spaceID, a.UserID, idemKey)
		if err != nil {
			return nil, err
		}
		if op != nil {
			if op.RequestHash != createReqHash(spaceID, canon, data) {
				return nil, domain.Conflict("idempotency key was reused with a different request")
			}
			if op.Phase == domain.OpPhaseCommitted {
				if d, _ := s.Docs.Document(ctx, op.DocumentID); d != nil {
					return &DocMeta{ID: d.ID, Path: d.Path, Kind: d.Kind,
						CurrentRevision: d.CurrentRevision, UpdatedAt: d.UpdatedAt}, nil
				}
				return nil, domain.Conflict("create completed but the document no longer exists")
			}
			return nil, domain.Conflict("a create operation with this key is still open")
		}
	}
	existing, err := s.Docs.DocumentByNormKey(ctx, spaceID, key)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, domain.Conflict("a document already exists at that path")
	}
	docID := domain.NewID("doc")
	revID := domain.NewID("rev")
	opID := domain.NewID("op")
	now := time.Now()

	// Step: durable pending bytes + operation row before any side effect.
	pendingPath, err := s.State.WriteJournal(opID, data)
	if err != nil {
		return nil, domain.Unavailable("journal unavailable")
	}
	op := &domain.Operation{
		ID: opID, SpaceID: spaceID, DocumentID: docID, Principal: a.UserID,
		Kind: domain.OpKindCreate, Phase: domain.OpPhasePending,
		NewSHA256: shaHex(data), TargetPath: canon, IdempotencyKey: idemKey,
		PendingPath: pendingPath,
		RequestHash: createReqHash(spaceID, canon, data),
		CreatedAt:   now, UpdatedAt: now,
	}
	if err := s.Docs.InsertOperation(ctx, op); err != nil {
		s.State.RemoveAbsolute(pendingPath)
		return nil, domain.Unavailable("could not record the operation")
	}
	// Preserve revision bytes before making the new file visible.
	histLoc, err := s.State.SaveHistory(spaceID, docID, revID, data)
	if err != nil {
		_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseAborted, "history unavailable before change")
		return nil, domain.Unavailable("revision history unavailable")
	}
	// Step: atomic content write, then SQL finalization.
	if err := s.Content.WriteAtomic(spaceID, canon, data); err != nil {
		s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseAborted, "storage unavailable before change")
		return nil, domain.Unavailable("content storage unavailable")
	}
	_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseStorageWritten, "")
	kind := "note"
	if isMemory {
		kind = "memory"
	}
	doc := &domain.Document{
		ID: docID, SpaceID: spaceID, Path: canon, NormKey: key, Kind: kind,
		CurrentRevision: revID, CreatedAt: now, UpdatedAt: now,
	}
	rev := &domain.Revision{
		ID: revID, DocumentID: docID, SHA256: shaHex(data), ByteLength: int64(len(data)),
		Creator: a.UserID, OperationID: opID, HistoryLocation: histLoc, CreatedAt: now,
	}
	if err := s.Docs.InsertDocument(ctx, doc, rev); err != nil {
		_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseConflict, "finalize failed: "+err.Error())
		return nil, domain.Unavailable("could not finalize the new document")
	}
	_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseCommitted, "")
	s.State.RemoveAbsolute(pendingPath)
	s.Reindex(ctx, spaceID, docID)
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "document.committed", DocumentID: docID, RevisionID: revID, OperationID: opID})
	return &DocMeta{ID: docID, Path: canon, Kind: kind, CurrentRevision: revID, UpdatedAt: now}, nil
}

// SaveContent performs the journaled conditional save. expectedRevision
// comes from If-Match: missing -> 428, mismatch -> 412 with the safe
// current revision exposed to readers.
func (s *Services) SaveContent(ctx context.Context, a *Actor, spaceID, docID, expectedRevision, markdownText, idemKey string, workingSHAs ...string) (*SaveResult, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionEdit); err != nil {
		return nil, err
	}
	if expectedRevision == "" {
		return nil, domain.PreconditionRequired("If-Match with the current revision is required")
	}
	data := []byte(markdownText)
	if int64(len(data)) > s.Cfg.MaxNoteBytes {
		return nil, domain.TooLarge("note exceeds the configured maximum size")
	}

	// Saves are serialized against folder mutations: the space mutation
	// lock is taken before the document lock, so path selection, the
	// storage write, post-write verification and revision finalization
	// form one boundary relative to folder rename/trash/restore. An edit
	// can never be acknowledged against metadata a concurrent folder
	// operation has since moved.
	smu := s.spaceMu(spaceID)
	smu.Lock()
	defer smu.Unlock()
	lock := s.docLock(docID)
	lock.Lock()
	defer lock.Unlock()

	doc, err := s.Docs.Document(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.SpaceID != spaceID || doc.TombstoneAt != nil {
		return nil, domain.NotFound("document not found")
	}

	// Idempotent retry of a completed save returns the same logical revision.
	if idemKey != "" {
		op, err := s.Docs.FindOperationByIdempotency(ctx, spaceID, a.UserID, idemKey)
		if err != nil {
			return nil, err
		}
		if op != nil && op.DocumentID == docID {
			if op.RequestHash != shaHex([]byte(fmt.Sprintf("save|%s|%s|%s", docID, expectedRevision, shaHex(data)))) {
				return nil, domain.Conflict("idempotency key was reused with a different request")
			}
			if op.Phase == domain.OpPhaseCommitted {
				revs, _ := s.Docs.RevisionsForOperation(ctx, op.ID)
				if len(revs) > 0 {
					return &SaveResult{Revision: revs[0].ID}, nil
				}
			}
			return nil, domain.Conflict("an operation with this key is still open")
		} else if op != nil {
			return nil, domain.Conflict("idempotency key already used for another document")
		}
	}

	if doc.CurrentRevision != expectedRevision {
		return nil, domain.PreconditionFailed(doc.CurrentRevision)
	}
	oldRev, err := s.Docs.Revision(ctx, doc.CurrentRevision)
	if err != nil {
		return nil, err
	}
	if oldRev == nil {
		return nil, domain.NotFound("revision state inconsistent")
	}

	if len(workingSHAs) > 0 {
		working, err := s.Content.Read(spaceID, doc.Path)
		if err != nil {
			return nil, err
		}
		if shaHex(working) != workingSHAs[0] {
			return nil, domain.Conflict("document has external changes; reconcile before applying board edits")
		}
	}
	now := time.Now()
	revID := domain.NewID("rev")
	opID := domain.NewID("op")
	pendingPath, err := s.State.WriteJournal(opID, data)
	if err != nil {
		return nil, domain.Unavailable("journal unavailable; draft retained client-side")
	}
	op := &domain.Operation{
		ID: opID, SpaceID: spaceID, DocumentID: docID, Principal: a.UserID,
		Kind: domain.OpKindContent, Phase: domain.OpPhasePending,
		ExpectedRevision: expectedRevision, OldRevision: oldRev.ID,
		NewSHA256: shaHex(data), TargetPath: doc.Path, IdempotencyKey: idemKey,
		PendingPath: pendingPath,
		RequestHash: shaHex([]byte(fmt.Sprintf("save|%s|%s|%s", docID, expectedRevision, shaHex(data)))),
		CreatedAt:   now, UpdatedAt: now,
	}
	if err := s.Docs.InsertOperation(ctx, op); err != nil {
		s.State.RemoveAbsolute(pendingPath)
		return nil, domain.Unavailable("could not record the operation")
	}

	// External-drift check: storage bytes must be the expected old bytes.
	oldBytes, rerr := s.Content.Read(spaceID, doc.Path)
	if rerr != nil {
		_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseAborted, "could not verify existing content")
		return nil, domain.Unavailable("could not read current content; edit retained for retry")
	}
	if rerr == nil && shaHex(oldBytes) != oldRev.SHA256 {
		if shaHex(oldBytes) != shaHex(data) {
			// bytes differ from both expected old and intended new: never guess
			_, _ = s.State.SaveHistory(spaceID, docID, "drift-"+revID, oldBytes)
			_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseConflict, "external modification detected")
			return nil, domain.Conflict("file changed outside Netsidian; both versions preserved for review")
		}
		// bytes already equal the intended new content: adopt them
		if oldRev.HistoryLocation == "" {
			_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseConflict, "original revision unavailable")
			return nil, domain.Conflict("original revision is unavailable; review external changes before saving")
		}
		_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseStorageWritten, "")
	} else {
		// History copy of the old current bytes (immutable), then atomic swap.
		if rerr == nil {
			histLoc, herr := s.State.SaveHistory(spaceID, docID, oldRev.ID, oldBytes)
			if herr != nil {
				_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseAborted, "history unavailable before change")
				return nil, domain.Unavailable("revision history unavailable; edit retained for retry")
			}
			if histLoc != "" {
				if err := s.Docs.UpdateRevisionHistory(ctx, oldRev.ID, histLoc); err != nil {
					return nil, domain.Unavailable("history durability failed")
				}
			}
		}
		if err := s.Content.WriteAtomic(spaceID, doc.Path, data); err != nil {
			_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseAborted, "storage unavailable before change")
			return nil, domain.Unavailable("content storage unavailable; draft retained")
		}
		_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseStorageWritten, "")
	}

	// Post-write verification is a genuine barrier under the space lock:
	// metadata cannot have changed while the write was in flight, so a
	// failed verification read must NOT be skipped — the save is only
	// acknowledged when this check succeeds. The relocation branch stays
	// as defense-in-depth (e.g. storage layers that mutate asynchronously).
	latest, lerr := s.Docs.Document(ctx, docID)
	if lerr != nil || latest == nil {
		// the operation stays in storage_written state: reconciliation at
		// restart replays finalization from the journaled bytes
		return nil, domain.Unavailable("could not verify the saved document; the edit is journaled for recovery")
	}
	writePath := doc.Path
	dest := ""
	if latest.TombstoneAt != nil {
		dest = trashPrefix + docID + "/" + latest.Path
	} else if latest.Path != doc.Path {
		dest = latest.Path
	}
	if dest != "" && dest != writePath {
		if cur, rerr := s.Content.Read(spaceID, writePath); rerr == nil && shaHex(cur) == shaHex(data) {
			if merr := s.Content.Move(spaceID, writePath, dest); merr == nil {
				s.pruneOrphanDirs(spaceID, writePath)
				*doc = *latest
			} else {
				_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseConflict, "path changed mid-save; moving bytes to the new path failed")
				return nil, domain.Conflict("the document moved while saving and the bytes could not be relocated; the edit is journaled for recovery")
			}
		} else {
			_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseConflict, "path changed mid-save; bytes at the old path are not this save's content")
			return nil, domain.Conflict("the document moved while saving; both versions are preserved for review")
		}
	}

	// SQL finalization.
	rev := &domain.Revision{
		ID: revID, DocumentID: docID, Predecessor: oldRev.ID,
		SHA256: shaHex(data), ByteLength: int64(len(data)),
		Creator: a.UserID, OperationID: opID, CreatedAt: now,
	}
	if err := s.finalizeRevision(ctx, doc.ID, rev, now); err != nil {
		return nil, domain.Unavailable("finalization failed; reconciler will resolve the journaled bytes")
	}
	_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseCommitted, "")
	s.State.RemoveAbsolute(pendingPath)

	// Derived indexing + event publication.
	s.Reindex(ctx, spaceID, docID)
	s.afterContentCommitted(ctx, spaceID, docID, a)
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "document.committed", DocumentID: docID, RevisionID: revID, OperationID: opID})
	return &SaveResult{Revision: revID, Committed: now.UTC().Format(time.RFC3339)}, nil
}

func (s *Services) finalizeRevision(ctx context.Context, docID string, rev *domain.Revision, now time.Time) error {
	if err := s.Docs.InsertRevision(ctx, rev); err != nil {
		return err
	}
	return s.Docs.UpdateDocumentCurrent(ctx, docID, rev.ID, now)
}

// pruneOrphanDirs removes directories left empty under a superseded write
// path (a save that followed a mid-flight folder move). Removing stops at
// the first non-empty or protected directory.
func (s *Services) pruneOrphanDirs(spaceID, oldPath string) {
	store, ok := s.Content.(ports.FolderStore)
	if !ok {
		return
	}
	dir := oldPath
	for {
		i := strings.LastIndex(dir, "/")
		if i < 0 {
			return
		}
		dir = dir[:i]
		if err := store.RemoveFolder(spaceID, dir); err != nil {
			return
		}
	}
}

// afterContentCommitted returns an accepted memory to proposed when its
// backing content was edited (memory lifecycle contract).
func (s *Services) afterContentCommitted(ctx context.Context, spaceID, docID string, a *Actor) {
	mem, err := s.Mem.MemoryByDocument(ctx, docID)
	if err != nil || mem == nil || mem.Status != "accepted" {
		return
	}
	_ = s.Mem.UpdateMemoryStatus(ctx, mem.ID, "proposed", "", mem.ReviewedBy, time.Now())
	_ = s.Mem.MemoryAudit(ctx, &ports.AuditEvent{
		ID: domain.NewID("aud"), Actor: a.UserID, Action: "memory.edit_reopened",
		SpaceID: spaceID, Resource: mem.ID, Outcome: "proposed", CreatedAt: time.Now(),
	})
}

// ---------- rename / trash / restore ----------

// Rename moves a document preserving its stable ID.
func (s *Services) Rename(ctx context.Context, a *Actor, spaceID, docID, newPath, expectedRevision string, expectedPaths ...string) error {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionEdit); err != nil {
		return err
	}
	if expectedRevision == "" {
		return domain.PreconditionRequired("If-Match is required")
	}
	smu := s.spaceMu(spaceID)
	smu.Lock()
	defer smu.Unlock()
	lock := s.docLock(docID)
	lock.Lock()
	defer lock.Unlock()
	doc, err := s.Docs.Document(ctx, docID)
	if err != nil {
		return err
	}
	if doc == nil || doc.SpaceID != spaceID || doc.TombstoneAt != nil {
		return domain.NotFound("document not found")
	}
	if len(expectedPaths) > 0 && doc.Path != expectedPaths[0] {
		return domain.Conflict("document moved; reload before retrying")
	}
	if doc.CurrentRevision != expectedRevision {
		return domain.PreconditionFailed(doc.CurrentRevision)
	}
	canon, key, perr := domain.NormalizePath(newPath)
	if perr != nil {
		return perr
	}
	if canon == doc.Path {
		return nil
	}
	existing, err := s.Docs.DocumentByNormKey(ctx, spaceID, key)
	if err != nil {
		return err
	}
	if existing != nil {
		return domain.Conflict("target path already exists")
	}
	if len(expectedPaths) > 0 {
		working, err := s.Content.Read(spaceID, doc.Path)
		if err != nil {
			return err
		}
		if shaHex(working) != s.currentSHA(ctx, doc) {
			return domain.Conflict("task has external changes; reconcile before moving")
		}
		exists, err := s.Content.Exists(spaceID, canon)
		if err != nil {
			return err
		}
		if exists {
			return domain.Conflict("a file already exists at the destination")
		}
	}
	opID := domain.NewID("op")
	now := time.Now()
	if err := s.Docs.InsertOperation(ctx, &domain.Operation{
		ID: opID, SpaceID: spaceID, DocumentID: docID, Principal: a.UserID,
		Kind: domain.OpKindRename, Phase: domain.OpPhasePending,
		ExpectedRevision: expectedRevision, OldRevision: doc.CurrentRevision,
		TargetPath: canon, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return domain.Unavailable("could not record the operation")
	}
	if err := s.Content.Move(spaceID, doc.Path, canon); err != nil {
		_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseAborted, "move failed")
		return domain.Unavailable("rename failed at storage layer")
	}
	if err := s.Docs.SetDocumentPath(ctx, docID, canon, key, time.Now()); err != nil {
		// roll the file back so exactly one logical document remains
		_ = s.Content.Move(spaceID, canon, doc.Path)
		_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseAborted, "metadata switch failed; rolled back")
		return domain.Unavailable("rename failed at metadata layer")
	}
	_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseCommitted, "")
	s.Reindex(ctx, spaceID, docID)
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "document.renamed", DocumentID: docID, OperationID: opID})
	return nil
}

const trashPrefix = ".netsidian-trash/"

// Trash records a tombstone then hides the bytes from ordinary listing.
func (s *Services) Trash(ctx context.Context, a *Actor, spaceID, docID, expectedRevision string) error {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionTrash); err != nil {
		return err
	}
	if expectedRevision == "" {
		return domain.PreconditionRequired("If-Match is required")
	}
	smu := s.spaceMu(spaceID)
	smu.Lock()
	defer smu.Unlock()
	lock := s.docLock(docID)
	lock.Lock()
	defer lock.Unlock()
	doc, err := s.Docs.Document(ctx, docID)
	if err != nil {
		return err
	}
	if doc == nil || doc.SpaceID != spaceID || doc.TombstoneAt != nil {
		return domain.NotFound("document not found")
	}
	if doc.CurrentRevision != expectedRevision {
		return domain.PreconditionFailed(doc.CurrentRevision)
	}
	now := time.Now()
	purge := now.AddDate(0, 0, s.Cfg.TrashRetentionDays)
	if err := s.Docs.SetDocumentTrash(ctx, docID, &now, &purge); err != nil {
		return err
	}
	if err := s.Content.Move(spaceID, doc.Path, trashPrefix+docID+"/"+doc.Path); err != nil {
		if rollback := s.Docs.SetDocumentTrash(ctx, docID, nil, nil); rollback != nil {
			return domain.Unavailable("trash storage failed and metadata rollback failed; operator recovery required")
		}
		return domain.Unavailable("trash storage failed; document remains active")
	}
	_ = s.Index.RemoveDocumentIndex(ctx, docID)
	_ = s.Index.ResolveLinks(ctx, spaceID)
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "document.trashed", DocumentID: docID})
	return nil
}

// Restore brings a trashed document back as an active document.
func (s *Services) Restore(ctx context.Context, a *Actor, spaceID, docID string) error {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRestore); err != nil {
		return err
	}
	smu := s.spaceMu(spaceID)
	smu.Lock()
	defer smu.Unlock()
	return s.restoreDoc(ctx, spaceID, docID)
}

// restoreDoc performs the restore; the caller must already hold spaceMu.
func (s *Services) restoreDoc(ctx context.Context, spaceID, docID string) error {
	lock := s.docLock(docID)
	lock.Lock()
	defer lock.Unlock()
	doc, err := s.Docs.Document(ctx, docID)
	if err != nil {
		return err
	}
	if doc == nil || doc.SpaceID != spaceID || doc.TombstoneAt == nil {
		return domain.NotFound("trashed document not found")
	}
	existing, err := s.Docs.DocumentByNormKey(ctx, spaceID, doc.NormKey)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != docID {
		return domain.Conflict("another document now occupies that path; rename before restoring")
	}
	if err := s.Content.Move(spaceID, trashPrefix+docID+"/"+doc.Path, doc.Path); err != nil {
		return domain.Unavailable("restore failed at storage layer")
	}
	if err := s.Docs.SetDocumentTrash(ctx, docID, nil, nil); err != nil {
		// undo the byte move so the tombstone keeps owning its bytes
		if rerr := s.Content.Move(spaceID, doc.Path, trashPrefix+docID+"/"+doc.Path); rerr != nil {
			return domain.Unavailable("restore failed mid-way and could not be rolled back; the bytes need operator review")
		}
		return err
	}
	s.Reindex(ctx, spaceID, docID) // also re-resolves links across the space
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "document.restored", DocumentID: docID})
	return nil
}

// ---------- history ----------

type RevisionMeta struct {
	ID          string    `json:"id"`
	Predecessor string    `json:"predecessor,omitempty"`
	SHA256      string    `json:"sha256"`
	ByteLength  int64     `json:"byteLength"`
	Creator     string    `json:"creator"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (s *Services) Revisions(ctx context.Context, a *Actor, spaceID, docID string) ([]*RevisionMeta, error) {
	verified, verifyErr := s.Docs.Document(ctx, docID)
	if verifyErr != nil {
		return nil, verifyErr
	}
	if verified == nil || verified.SpaceID != spaceID {
		return nil, domain.NotFound("document not found")
	}
	if a.Key != nil && !hasScope(a.Key.Scopes, domain.ScopeHistoryRead) {
		return nil, domain.Forbidden("key lacks history:read")
	}
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	revs, err := s.Docs.RevisionsForDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	out := make([]*RevisionMeta, 0, len(revs))
	for _, r := range revs {
		out = append(out, &RevisionMeta{ID: r.ID, Predecessor: r.Predecessor, SHA256: r.SHA256,
			ByteLength: r.ByteLength, Creator: r.Creator, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// RevisionContent fetches one historical revision (history:read shape).
func (s *Services) RevisionContent(ctx context.Context, a *Actor, spaceID, docID, revID string) (string, error) {
	verified, verifyErr := s.Docs.Document(ctx, docID)
	if verifyErr != nil {
		return "", verifyErr
	}
	if verified == nil || verified.SpaceID != spaceID {
		return "", domain.NotFound("document not found")
	}
	if a.Key != nil && !hasScope(a.Key.Scopes, domain.ScopeHistoryRead) {
		return "", domain.Forbidden("key lacks history:read")
	}
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return "", err
	}
	rev, err := s.Docs.Revision(ctx, revID)
	if err != nil || rev == nil || rev.DocumentID != docID {
		return "", domain.NotFound("revision not found")
	}
	if rev.HistoryLocation != "" {
		if b, err := s.State.ReadAbsolute(rev.HistoryLocation); err == nil {
			return string(b), nil
		}
	}
	doc, err := s.Docs.Document(ctx, docID)
	if err != nil || doc == nil {
		return "", domain.NotFound("document not found")
	}
	if doc.CurrentRevision == revID {
		b, err := s.Content.Read(spaceID, doc.Path)
		if err != nil {
			return "", domain.Unavailable("revision bytes unavailable")
		}
		return string(b), nil
	}
	return "", domain.NotFound("revision bytes unavailable")
}

// ---------- indexing ----------

// Reindex derives search/link/metadata rows from the committed revision;
// the repository guards generations so jobs never overwrite newer state.
func (s *Services) Reindex(ctx context.Context, spaceID, docID string) {
	doc, err := s.Docs.Document(ctx, docID)
	if err != nil || doc == nil || doc.TombstoneAt != nil {
		return
	}
	data, err := s.Content.Read(spaceID, doc.Path)
	if err != nil {
		if alt := s.committedBytes(ctx, doc); alt != nil {
			data = alt
		} else {
			return
		}
	}
	idx := markdown.Parse(string(data), domain.BaseName(doc.Path))
	_ = s.Index.ReplaceDocumentIndex(ctx, spaceID, doc, doc.CurrentRevision, idx)
	// Wikilinks resolve against current documents; correct siblings too.
	_ = s.Index.ResolveLinks(ctx, spaceID)
}

// ReindexAll rebuilds the whole derived index (startup repair).
func (s *Services) ReindexAll(ctx context.Context) {
	ids, err := s.Spaces.AllSpaceIDs(ctx)
	if err != nil {
		return
	}
	for _, sid := range ids {
		if s.vaultLoading(sid) {
			continue
		}
		var docs []*domain.Document
		if repo, ok := s.Index.(interface {
			DocumentsNeedingIndex(context.Context, string) ([]*domain.Document, error)
		}); ok {
			docs, err = repo.DocumentsNeedingIndex(ctx, sid)
		} else {
			docs, err = s.Docs.DocumentsInSpace(ctx, sid, false)
		}
		if err != nil {
			continue
		}
		for _, d := range docs {
			data, readErr := s.Content.Read(sid, d.Path)
			if readErr != nil {
				data = s.committedBytes(ctx, d)
			}
			if data == nil {
				continue
			}
			idx := markdown.Parse(string(data), domain.BaseName(d.Path))
			if repo, ok := s.Index.(interface {
				ReplaceDocumentIndexDeferred(context.Context, string, *domain.Document, string, *ports.IndexData) error
			}); ok {
				_ = repo.ReplaceDocumentIndexDeferred(ctx, sid, d, d.CurrentRevision, idx)
			} else {
				_ = s.Index.ReplaceDocumentIndex(ctx, sid, d, d.CurrentRevision, idx)
			}
		}
		if len(docs) > 0 {
			_ = s.Index.ResolveLinks(ctx, sid)
		}
	}
}

// ---------- journal reconciliation ----------

// ReconcileOperations resolves operations left open by a crash/shutdown.
// It never creates a second logical revision for the same request.
func (s *Services) ReconcileOperations(ctx context.Context) (resolved int, blocked []*domain.Operation) {
	ops, err := s.Docs.OpenOperations(ctx)
	if err != nil {
		return 0, nil
	}
	for _, op := range ops {
		smu := s.spaceMu(op.SpaceID)
		smu.Lock()
		lock := s.docLock(op.DocumentID)
		lock.Lock()
		_, blk := s.reconcileOne(ctx, op)
		lock.Unlock()
		smu.Unlock()
		if blk {
			blocked = append(blocked, op)
		} else {
			resolved++
		}
	}
	return resolved, blocked
}

func (s *Services) reconcileOne(ctx context.Context, op *domain.Operation) (resolved, blocked bool) {
	doc, err := s.Docs.Document(ctx, op.DocumentID)
	if err != nil {
		return false, true
	}
	if op.Kind == domain.OpKindCreate {
		if op.PendingPath != "" {
			s.State.RemoveAbsolute(op.PendingPath)
		}
		if doc != nil {
			// document row exists: ensure committed phase
			_ = s.Docs.SetOperationPhase(ctx, op.ID, domain.OpPhaseCommitted, "reconciled create")
			return true, false
		}
		_ = s.Docs.SetOperationPhase(ctx, op.ID, domain.OpPhaseAborted, "no document at restart")
		_ = s.Docs.DeleteOperation(ctx, op.ID)
		return true, false
	}
	if op.Kind == domain.OpKindFolderRename {
		// A folder rename was interrupted (crash or failed rollback) between
		// the tree move and the metadata switch. The journaled origin and
		// target paths plus the observable bytes decide the safe direction;
		// a record is only resolved once its invariants hold.
		if doc == nil {
			if err := s.Docs.SetOperationPhase(ctx, op.ID, domain.OpPhaseAborted, "document gone at restart"); err != nil {
				return false, true
			}
			return true, false
		}
		if op.OriginPath == "" {
			// record from a pre-origin-path build: only a verifiably
			// completed rename may close; anything else stays actionable
			if doc.Path == op.TargetPath {
				ok, eerr := s.Content.Exists(doc.SpaceID, op.TargetPath)
				if eerr == nil && ok {
					if err := s.Docs.SetOperationPhase(ctx, op.ID, domain.OpPhaseCommitted, "reconciled folder rename"); err != nil {
						return false, true
					}
					return true, false
				}
			}
			return false, true
		}
		atTarget := doc.Path == op.TargetPath
		atOrigin := doc.Path == op.OriginPath
		bytesTarget, terr := s.Content.Exists(doc.SpaceID, op.TargetPath)
		bytesOrigin, oerr := s.Content.Exists(doc.SpaceID, op.OriginPath)
		if terr != nil || oerr != nil {
			// storage could not be inspected: absence may be a lie, so the
			// record stays actionable instead of guessing a direction
			return false, true
		}
		finish := func(phase domain.OperationPhase, msg string) (bool, bool) {
			if err := s.Docs.SetOperationPhase(ctx, op.ID, phase, msg); err != nil {
				return false, true
			}
			return true, false
		}
		if bytesTarget == bytesOrigin {
			// bytes at BOTH locations (two unreconciled versions) or at
			// neither: no safe direction exists. Keep both files and leave
			// the record actionable so every reconciliation reports it.
			return false, true
		}
		switch {
		case bytesTarget && atTarget:
			// target verified present with origin verified absent
			return finish(domain.OpPhaseCommitted, "reconciled folder rename")
		case bytesTarget && atOrigin:
			// tree moved but metadata rolled back: complete the rename
			if _, key, perr := domain.NormalizePath(op.TargetPath); perr == nil &&
				s.Docs.SetDocumentPath(ctx, doc.ID, op.TargetPath, key, time.Now()) == nil {
				s.Reindex(ctx, doc.SpaceID, doc.ID)
				return finish(domain.OpPhaseCommitted, "completed interrupted folder rename")
			}
			return false, true
		case bytesOrigin && atOrigin:
			// origin verified present with target verified absent
			return finish(domain.OpPhaseAborted, "rename fully rolled back")
		case bytesOrigin && atTarget:
			// rollback moved the tree back but failed to revert metadata:
			// pull the document back to the origin where its bytes live
			if _, key, perr := domain.NormalizePath(op.OriginPath); perr == nil &&
				s.Docs.SetDocumentPath(ctx, doc.ID, op.OriginPath, key, time.Now()) == nil {
				s.Reindex(ctx, doc.SpaceID, doc.ID)
				return finish(domain.OpPhaseAborted, "rolled back interrupted folder rename")
			}
			return false, true
		default:
			// document moved to an unrelated path: unverifiable, keep
			// actionable (operator attention, surfaced as blocked)
			return false, true
		}
	}
	if doc == nil {
		return false, true
	}
	data, err := s.Content.Read(doc.SpaceID, doc.Path)
	if err != nil {
		return false, true
	}
	sum := shaHex(data)
	oldRev, _ := s.Docs.Revision(ctx, op.OldRevision)

	switch {
	case sum == op.NewSHA256:
		// storage already holds the intended bytes: replay finalization only
		if doc.CurrentRevision == op.OldRevision && oldRev != nil {
			rev := &domain.Revision{
				ID: domain.NewID("rev"), DocumentID: doc.ID, Predecessor: oldRev.ID,
				SHA256: op.NewSHA256, ByteLength: int64(len(data)),
				Creator: op.Principal, OperationID: op.ID, CreatedAt: time.Now(),
			}
			if err := s.finalizeRevision(ctx, doc.ID, rev, time.Now()); err != nil {
				return false, true
			}
		}
		_ = s.Docs.SetOperationPhase(ctx, op.ID, domain.OpPhaseCommitted, "reconciled")
		if op.PendingPath != "" {
			s.State.RemoveAbsolute(op.PendingPath)
		}
		s.Reindex(ctx, doc.SpaceID, doc.ID)
		return true, false

	case oldRev != nil && sum == oldRev.SHA256 && op.PendingPath != "":
		// storage still holds old bytes: retry the atomic swap from journal
		pending, err := s.State.ReadAbsolute(op.PendingPath)
		if err != nil || shaHex(pending) != op.NewSHA256 {
			return false, true
		}
		if err := s.Content.WriteAtomic(doc.SpaceID, doc.Path, pending); err != nil {
			return false, true
		}
		rev := &domain.Revision{
			ID: domain.NewID("rev"), DocumentID: doc.ID, Predecessor: oldRev.ID,
			SHA256: op.NewSHA256, ByteLength: int64(len(pending)),
			Creator: op.Principal, OperationID: op.ID, CreatedAt: time.Now(),
		}
		if err := s.finalizeRevision(ctx, doc.ID, rev, time.Now()); err != nil {
			return false, true
		}
		_ = s.Docs.SetOperationPhase(ctx, op.ID, domain.OpPhaseCommitted, "reconciled swap")
		s.State.RemoveAbsolute(op.PendingPath)
		s.Reindex(ctx, doc.SpaceID, doc.ID)
		return true, false

	default:
		_ = s.Docs.SetOperationPhase(ctx, op.ID, domain.OpPhaseConflict,
			"bytes differ from both old and intended; operator review required")
		return false, true
	}
}

// LookupDocument resolves a canonical path without scanning vault metadata or reading its body.
func (s *Services) LookupDocument(ctx context.Context, a *Actor, spaceID, path string) (*domain.Document, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	_, key, err := domain.NormalizePath(path)
	if err != nil {
		return nil, err
	}
	doc, lookupErr := s.Docs.DocumentByNormKey(ctx, spaceID, key)
	if lookupErr != nil {
		return nil, lookupErr
	}
	if doc == nil || doc.TombstoneAt != nil {
		return nil, domain.NotFound("document not found")
	}
	return doc, nil
}

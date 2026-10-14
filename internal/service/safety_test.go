package service

// Safety regressions requested by the independent CP-A review
// (review/cr2/REVIEW-INDEPENDENT-CP-A.md): fault injection around folder
// metadata/storage failures, crash-restart recovery of folder renames,
// restore-collision retry, attachment size boundaries and attachment key
// scopes. All doubles wrap the real SQLite repository and local storage.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/storage/local"
)

// ---------- fault-injecting doubles ----------

type flakyDocs struct {
	ports.DocumentRepo
	failPathTo    func(to string) bool
	failReadAfter func() bool
}

func (f *flakyDocs) SetDocumentPath(ctx context.Context, docID, path, normKey string, updatedAt time.Time) error {
	if f.failPathTo != nil && f.failPathTo(path) {
		return errors.New("injected metadata failure")
	}
	return f.DocumentRepo.SetDocumentPath(ctx, docID, path, normKey, updatedAt)
}

func (f *flakyDocs) Document(ctx context.Context, id string) (*domain.Document, error) {
	if f.failReadAfter != nil && f.failReadAfter() {
		return nil, errors.New("injected metadata read failure")
	}
	return f.DocumentRepo.Document(ctx, id)
}

type flakyTrashDocs struct {
	ports.DocumentRepo
	failNextTombstone bool
}

func (f *flakyTrashDocs) SetDocumentTrash(ctx context.Context, docID string, tomb, purge *time.Time) error {
	if f.failNextTombstone && tomb != nil {
		f.failNextTombstone = false
		return errors.New("injected tombstone failure")
	}
	return f.DocumentRepo.SetDocumentTrash(ctx, docID, tomb, purge)
}

type flakyContent struct {
	ports.ContentStore
	ports.FolderStore
	failMoveTo         func(to string) bool
	failMoveFolderFrom func(from string) bool
	existsErr          func(path string) bool
}

func (f *flakyContent) Exists(spaceID, rel string) (bool, error) {
	if f.existsErr != nil && f.existsErr(rel) {
		return false, errors.New("injected exists failure")
	}
	return f.ContentStore.Exists(spaceID, rel)
}

func (f *flakyContent) Move(spaceID, fromRel, toRel string) error {
	if f.failMoveTo != nil && f.failMoveTo(toRel) {
		return errors.New("injected storage move failure")
	}
	return f.ContentStore.Move(spaceID, fromRel, toRel)
}

func (f *flakyContent) MoveFolder(spaceID, fromRel, toRel string) error {
	if f.failMoveFolderFrom != nil && f.failMoveFolderFrom(fromRel) {
		return errors.New("injected folder move failure")
	}
	return f.FolderStore.MoveFolder(spaceID, fromRel, toRel)
}

// ---------- restart-capable stack ----------

type safetyStack struct {
	svc   *Services
	done  sync.Once
	close func()
}

func openSafetyStack(t *testing.T, dir string) *safetyStack {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(dir, "state", "database", "netsidian.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	content, err := local.NewContent(filepath.Join(dir, "state", "content"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := local.NewState(filepath.Join(dir, "state", "journal"), filepath.Join(dir, "state", "history"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewServices(Config{AuthMode: "none", MaxNoteBytes: 1 << 20, TrashRetentionDays: 30},
		db, db, db, db, db, db, db, content, state)
	st := &safetyStack{svc: svc}
	st.close = func() { st.done.Do(func() { _ = db.SQL.Close() }) }
	t.Cleanup(st.close)
	return st
}

func (st *safetyStack) principal(t *testing.T) (*Actor, string) {
	t.Helper()
	ctx := context.Background()
	u, err := st.svc.EnsureNoAuthPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spaces, _, err := st.svc.Spaces.SpacesForUser(ctx, u.ID)
	if err != nil || len(spaces) == 0 {
		t.Fatalf("personal space missing: %v", err)
	}
	return &Actor{UserID: u.ID, IsOperator: true, Local: true}, spaces[0].ID
}

func pngOfSize(n int) []byte {
	b := append([]byte{}, pngBytes...)
	for len(b) < n {
		b = append(b, 'x')
	}
	return b[:n]
}

// ---------- F1: folder rename fault injection + restart recovery ----------

func TestFolderRenameMetadataFailureRollsBack(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for _, p := range []string{"Folder/a.md", "Folder/b.md", "Folder/c.md"} {
		if _, err := h.svc.CreateNote(ctx, h.actor, h.space, p, "body of "+p, "", false); err != nil {
			t.Fatal(err)
		}
	}
	h.svc.Docs = &flakyDocs{DocumentRepo: h.svc.Docs,
		failPathTo: func(to string) bool { return to == "Folder2/b.md" }}
	_, err := h.svc.RenameFolder(ctx, h.actor, h.space, "Folder", "Folder2")
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected rolled-back rename failure, got %v", err)
	}
	docs, _ := h.svc.ListDocuments(ctx, h.actor, h.space, true)
	for _, d := range docs {
		if !strings.HasPrefix(d.Path, "Folder/") {
			t.Fatalf("metadata not rolled back: %v", d.Path)
		}
		nc, err := h.svc.ReadNote(ctx, h.actor, h.space, d.ID)
		if err != nil || nc.Markdown != "body of "+d.Path {
			t.Fatalf("content lost after rollback: %v %+v", err, nc)
		}
	}
	folders, _ := h.svc.ListFolders(ctx, h.actor, h.space)
	for _, f := range folders {
		if f == "Folder2" {
			t.Fatalf("rolled-back rename left target folder: %v", folders)
		}
	}
	// a fully rolled-back rename must leave no actionable journal records
	if resolved, blocked := h.svc.ReconcileOperations(ctx); resolved != 0 || len(blocked) != 0 {
		t.Fatalf("rolled-back rename left actionable records: resolved=%d blocked=%v", resolved, blocked)
	}
}

func TestRenameFailedRollbackRepairedAtReopen(t *testing.T) {
	dir := t.TempDir()
	st := openSafetyStack(t, dir)
	ctx := context.Background()
	actor, space := st.principal(t)
	for _, p := range []string{"Before/a.md", "Before/b.md"} {
		if _, err := st.svc.CreateNote(ctx, actor, space, p, "original", "", false); err != nil {
			t.Fatal(err)
		}
	}
	origDocs := st.svc.Docs
	// forward switch fails for b; the rollback revert for a also fails
	st.svc.Docs = &flakyDocs{DocumentRepo: origDocs,
		failPathTo: func(to string) bool { return to == "After/b.md" || to == "Before/a.md" }}
	if _, err := st.svc.RenameFolder(ctx, actor, space, "Before", "After"); err == nil {
		t.Fatal("expected rename failure")
	}
	st.svc.Docs = origDocs
	st.close() // process restart with the faulty records in the journal

	st2 := openSafetyStack(t, dir)
	actor2, space2 := st2.principal(t)
	resolved, blocked := st2.svc.ReconcileOperations(ctx)
	if len(blocked) != 0 {
		t.Fatalf("recovery should repair, not block: %v", blocked)
	}
	if resolved < 1 {
		t.Fatal("failed rollback records were neither repaired nor blocked")
	}
	docs, err := st2.svc.ListDocuments(ctx, actor2, space2, false)
	if err != nil || len(docs) != 2 {
		t.Fatalf("docs: %v %+v", err, docs)
	}
	for _, d := range docs {
		if !strings.HasPrefix(d.Path, "Before/") {
			t.Fatalf("document left at %s after failed rollback repair", d.Path)
		}
		nc, e := st2.svc.ReadNote(ctx, actor2, space2, d.ID)
		if e != nil || nc.Markdown != "original" {
			t.Fatalf("metadata at %s has no matching bytes: %v", d.Path, e)
		}
	}
	if r2, b2 := st2.svc.ReconcileOperations(ctx); r2 != 0 || len(b2) != 0 {
		t.Fatalf("journal not clean after repair: resolved=%d blocked=%v", r2, b2)
	}
}

func TestRenameRollbackTreeFailureCompletesForwardAtReopen(t *testing.T) {
	dir := t.TempDir()
	st := openSafetyStack(t, dir)
	ctx := context.Background()
	actor, space := st.principal(t)
	if _, err := st.svc.CreateNote(ctx, actor, space, "Before/a.md", "keep", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.svc.CreateNote(ctx, actor, space, "Before/b.md", "keep", "", false); err != nil {
		t.Fatal(err)
	}
	origDocs, origContent := st.svc.Docs, st.svc.Content
	fc := &flakyContent{ContentStore: origContent, FolderStore: origContent.(ports.FolderStore),
		failMoveFolderFrom: func(from string) bool { return from == "After" }} // only the rollback move
	st.svc.Content = fc
	st.svc.Docs = &flakyDocs{DocumentRepo: origDocs,
		failPathTo: func(to string) bool { return to == "After/b.md" }}
	if _, err := st.svc.RenameFolder(ctx, actor, space, "Before", "After"); err == nil {
		t.Fatal("expected rename failure")
	}
	st.svc.Docs, st.svc.Content = origDocs, origContent
	st.close()

	st2 := openSafetyStack(t, dir)
	actor2, space2 := st2.principal(t)
	resolved, blocked := st2.svc.ReconcileOperations(ctx)
	if len(blocked) != 0 {
		t.Fatalf("reconcile should complete the forward rename: %v", blocked)
	}
	if resolved < 2 {
		t.Fatalf("expected 2 operations resolved, got %d", resolved)
	}
	docs, err := st2.svc.ListDocuments(ctx, actor2, space2, false)
	if err != nil || len(docs) != 2 {
		t.Fatalf("docs: %v %+v", err, docs)
	}
	for _, d := range docs {
		if !strings.HasPrefix(d.Path, "After/") {
			t.Fatalf("forward rename not completed: %s", d.Path)
		}
		if nc, e := st2.svc.ReadNote(ctx, actor2, space2, d.ID); e != nil || nc.Markdown != "keep" {
			t.Fatalf("bytes missing at %s after completion: %v", d.Path, e)
		}
	}
	folders, _ := st2.svc.ListFolders(ctx, actor2, space2)
	for _, f := range folders {
		if f == "Before" {
			t.Fatalf("stale origin folder after forward completion: %v", folders)
		}
	}
}

func TestSaveRenameOverlapInvariants(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "Box/a.md", "v0", "", false)
	if err != nil {
		t.Fatal(err)
	}
	folder, other := "Box", "BoxAlt"
	for i := 1; i <= 25; i++ {
		text := "v" + string(rune('A'+i%26)) + string(rune('a'+i%26))
		cur, err := h.svc.Docs.Document(ctx, doc.ID)
		if err != nil || cur == nil {
			t.Fatal(err)
		}
		saveErr := make(chan error, 1)
		renErr := make(chan error, 1)
		from, to := folder, other
		saveFirst := i%2 == 0
		if saveFirst {
			go func() {
				_, e := h.svc.SaveContent(ctx, h.actor, h.space, doc.ID, cur.CurrentRevision, text, "")
				saveErr <- e
			}()
			go func() {
				_, e := h.svc.RenameFolder(ctx, h.actor, h.space, from, to)
				renErr <- e
			}()
		} else {
			go func() {
				_, e := h.svc.RenameFolder(ctx, h.actor, h.space, from, to)
				renErr <- e
			}()
			go func() {
				_, e := h.svc.SaveContent(ctx, h.actor, h.space, doc.ID, cur.CurrentRevision, text, "")
				saveErr <- e
			}()
		}
		saved, renamed := false, false
		for !saved || !renamed {
			select {
			case e := <-saveErr:
				saved = true
				if e != nil {
					t.Fatalf("iteration %d: save: %v", i, e)
				}
			case e := <-renErr:
				renamed = true
				if e != nil {
					t.Fatalf("iteration %d: rename: %v", i, e)
				}
			case <-time.After(15 * time.Second):
				t.Fatalf("iteration %d: overlap deadlocked (save=%v rename=%v)", i, saved, renamed)
			}
		}
		// the acknowledged bytes live at exactly the document's current path
		cur, _ = h.svc.Docs.Document(ctx, doc.ID)
		data, rerr := h.svc.Content.Read(h.space, cur.Path)
		if rerr != nil || string(data) != text {
			t.Fatalf("iteration %d: acknowledged content not at current path %s: %v %q", i, cur.Path, rerr, data)
		}
		if ok, _ := h.svc.Content.Exists(h.space, from+"/a.md"); ok && from != cur.Path[:strings.LastIndex(cur.Path, "/")] {
			t.Fatalf("iteration %d: orphan bytes left at %s/a.md", i, from)
		}
		folder, other = other, from
	}
}

func TestRenameAmbiguousBytesStayBlockedAcrossReopen(t *testing.T) {
	// both-present ambiguity with the document at the origin: blocked,
	// both files intact, still blocked after reopen and reruns
	dir := t.TempDir()
	st := openSafetyStack(t, dir)
	ctx := context.Background()
	actor, space := st.principal(t)
	if _, err := st.svc.CreateNote(ctx, actor, space, "Before/a.md", "origin bytes", "", false); err != nil {
		t.Fatal(err)
	}
	if err := st.svc.Content.WriteAtomic(space, "After/a.md", []byte("target bytes")); err != nil {
		t.Fatal(err)
	}
	docs, _ := st.svc.ListDocuments(ctx, actor, space, false)
	if err := st.svc.Docs.InsertOperation(ctx, &domain.Operation{
		ID: domain.NewID("op"), SpaceID: space, DocumentID: docs[0].ID, Principal: actor.UserID,
		Kind: domain.OpKindFolderRename, Phase: domain.OpPhasePending,
		TargetPath: "After/a.md", OriginPath: "Before/a.md",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	st.close()

	st2 := openSafetyStack(t, dir)
	if resolved, blocked := st2.svc.ReconcileOperations(ctx); len(blocked) != 1 || resolved != 0 {
		t.Fatalf("ambiguity silently resolved at restart: resolved=%d blocked=%d", resolved, len(blocked))
	}
	for p, want := range map[string]string{"Before/a.md": "origin bytes", "After/a.md": "target bytes"} {
		if b, e := st2.svc.Content.Read(space, p); e != nil || string(b) != want {
			t.Fatalf("ambiguous recovery damaged %s: %v %q", p, e, b)
		}
	}
	if _, blocked := st2.svc.ReconcileOperations(ctx); len(blocked) != 1 {
		t.Fatal("ambiguous record must stay actionable across reruns")
	}
}

func TestRenameMissingBothAtTargetBlocked(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "After/a.md", "gone", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Content.Delete(h.space, "After/a.md"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Docs.InsertOperation(ctx, &domain.Operation{
		ID: domain.NewID("op"), SpaceID: h.space, DocumentID: doc.ID, Principal: h.actor.UserID,
		Kind: domain.OpKindFolderRename, Phase: domain.OpPhasePending,
		TargetPath: "After/a.md", OriginPath: "Before/a.md",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if resolved, blocked := h.svc.ReconcileOperations(ctx); len(blocked) != 1 || resolved != 0 {
		t.Fatalf("bytes missing at both paths must block: resolved=%d blocked=%d", resolved, len(blocked))
	}
}

func TestRenameExistsErrorBlocksUntilInspectable(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "After/a.md", "target bytes", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Docs.InsertOperation(ctx, &domain.Operation{
		ID: domain.NewID("op"), SpaceID: h.space, DocumentID: doc.ID, Principal: h.actor.UserID,
		Kind: domain.OpKindFolderRename, Phase: domain.OpPhasePending,
		TargetPath: "After/a.md", OriginPath: "Before/a.md",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	fc := &flakyContent{ContentStore: h.svc.Content, FolderStore: h.svc.Content.(ports.FolderStore),
		existsErr: func(p string) bool { return p == "Before/a.md" }}
	h.svc.Content = fc
	// a state that would normally commit must not be certified while the
	// origin cannot be inspected: absence must be verified, not assumed
	if resolved, blocked := h.svc.ReconcileOperations(ctx); len(blocked) != 1 || resolved != 0 {
		t.Fatalf("Exists error must block: resolved=%d blocked=%d", resolved, len(blocked))
	}
	fc.existsErr = nil
	if resolved, blocked := h.svc.ReconcileOperations(ctx); len(blocked) != 0 || resolved != 1 {
		t.Fatalf("record should resolve once storage is inspectable: resolved=%d blocked=%d", resolved, len(blocked))
	}
}

func TestRenameUnrecoverableSurfacesBlocked(t *testing.T) {
	dir := t.TempDir()
	st := openSafetyStack(t, dir)
	ctx := context.Background()
	actor, space := st.principal(t)
	doc, err := st.svc.CreateNote(ctx, actor, space, "Before/a.md", "orphan me", "", false)
	if err != nil {
		t.Fatal(err)
	}
	// journaled rename whose bytes are then destroyed outside the engine:
	// neither origin nor target can be verified, so the record must stay
	// actionable (blocked) instead of being silently resolved.
	if err := st.svc.Docs.InsertOperation(ctx, &domain.Operation{
		ID: domain.NewID("op"), SpaceID: space, DocumentID: doc.ID, Principal: actor.UserID,
		Kind: domain.OpKindFolderRename, Phase: domain.OpPhasePending,
		TargetPath: "After/a.md", OriginPath: "Before/a.md",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.svc.Content.Delete(space, "Before/a.md"); err != nil {
		t.Fatal(err)
	}
	st.close()

	st2 := openSafetyStack(t, dir)
	_, blocked := st2.svc.ReconcileOperations(ctx)
	if len(blocked) != 1 {
		t.Fatalf("unverifiable rename must be reported blocked, got %v", blocked)
	}
	if _, b2 := st2.svc.ReconcileOperations(ctx); len(b2) != 1 {
		t.Fatal("blocked record must stay actionable across reconcile passes")
	}
}

func TestFolderRenameCrashRecoveredAtRestart(t *testing.T) {
	dir := t.TempDir()
	st := openSafetyStack(t, dir)
	ctx := context.Background()
	actor, space := st.principal(t)
	one, err := st.svc.CreateNote(ctx, actor, space, "Folder/a.md", "alpha", "", false)
	if err != nil {
		t.Fatal(err)
	}
	two, err := st.svc.CreateNote(ctx, actor, space, "Folder/b.md", "beta", "", false)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash between the tree move and the metadata switch the
	// way RenameFolder records it: journaled pending ops, moved tree,
	// partially applied metadata.
	store := st.svc.Content.(ports.FolderStore)
	for _, m := range []struct{ id, from, to string }{{one.ID, "Folder/a.md", "Folder2/a.md"}, {two.ID, "Folder/b.md", "Folder2/b.md"}} {
		if err := st.svc.Docs.InsertOperation(ctx, &domain.Operation{
			ID: domain.NewID("op"), SpaceID: space, DocumentID: m.id, Principal: actor.UserID,
			Kind: domain.OpKindFolderRename, Phase: domain.OpPhasePending,
			TargetPath: m.to, OriginPath: m.from, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.MoveFolder(space, "Folder", "Folder2"); err != nil {
		t.Fatal(err)
	}
	if _, key, e := domain.NormalizePath("Folder2/a.md"); e == nil {
		if err := st.svc.Docs.SetDocumentPath(ctx, one.ID, "Folder2/a.md", key, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	st.close() // the process dies here

	st2 := openSafetyStack(t, dir)
	actor2, space2 := st2.principal(t)
	resolved, blocked := st2.svc.ReconcileOperations(ctx)
	if len(blocked) > 0 {
		t.Fatalf("reconcile blocked: %v", blocked)
	}
	if resolved < 2 {
		t.Fatalf("expected at least 2 resolved operations, got %d", resolved)
	}
	docs, _ := st2.svc.ListDocuments(ctx, actor2, space2, false)
	if len(docs) != 2 {
		t.Fatalf("docs: %+v", docs)
	}
	for _, d := range docs {
		if !strings.HasPrefix(d.Path, "Folder2/") {
			t.Fatalf("interrupted rename not completed: %s", d.Path)
		}
		nc, err := st2.svc.ReadNote(ctx, actor2, space2, d.ID)
		if err != nil || nc.Markdown != "alpha" && nc.Markdown != "beta" {
			t.Fatalf("content lost across restart: %v %+v", err, nc)
		}
	}
	if _, err := st2.svc.Content.Read(space2, "Folder/a.md"); err == nil {
		t.Fatal("stale bytes at the old path after restart")
	}
}

// ---------- F2: restore collision keeps both versions, retry works ----------

func TestRestoreCollisionPreservesBothAndRetries(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.svc.Content.WriteAtomic(h.space, "F/image.png", []byte("original attachment")); err != nil {
		t.Fatal(err)
	}
	tr, err := h.svc.TrashFolder(ctx, h.actor, h.space, "F")
	if err != nil || tr.TrashID == "" {
		t.Fatalf("trash: %v %+v", err, tr)
	}
	if err := h.svc.Content.WriteAtomic(h.space, "F/image.png", []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.RestoreFolder(ctx, h.actor, h.space, tr.TrashID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) == 0 || len(res.Failures) == 0 {
		t.Fatalf("collision not surfaced: %+v", res)
	}
	entries, _ := h.svc.ListFolderTrashes(ctx, h.actor, h.space)
	if len(entries) != 1 {
		t.Fatalf("recovery entry vanished on collision: %+v", entries)
	}
	if cur, _ := h.svc.Content.Read(h.space, "F/image.png"); string(cur) != "replacement" {
		t.Fatalf("replacement was overwritten: %q", cur)
	}
	// resolve the destination, then retry: the original returns intact
	if err := h.svc.Content.Delete(h.space, "F/image.png"); err != nil {
		t.Fatal(err)
	}
	res2, err := h.svc.RestoreFolder(ctx, h.actor, h.space, tr.TrashID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Conflicts) > 0 || len(res2.Failures) > 0 {
		t.Fatalf("retry reported problems: %+v", res2)
	}
	if cur, _ := h.svc.Content.Read(h.space, "F/image.png"); string(cur) != "original attachment" {
		t.Fatalf("original not restored on retry: %q", cur)
	}
	if entries, _ := h.svc.ListFolderTrashes(ctx, h.actor, h.space); len(entries) != 0 {
		t.Fatalf("entry not cleaned after complete restore: %+v", entries)
	}
}

// ---------- F3: folder trash checks every storage result ----------

func TestFolderTrashMoveFailureKeepsDocumentConsistent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "F/n.md", "keep me", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Content.WriteAtomic(h.space, "F/pic.png", pngBytes); err != nil {
		t.Fatal(err)
	}
	fc := &flakyContent{ContentStore: h.svc.Content,
		FolderStore: h.svc.Content.(ports.FolderStore),
		failMoveTo:  func(to string) bool { return strings.HasPrefix(to, ".netsidian-trash/") }}
	h.svc.Content = fc
	res, err := h.svc.TrashFolder(ctx, h.actor, h.space, "F")
	if err != nil {
		t.Fatal(err)
	}
	if res.Trashed != 0 || len(res.Failures) == 0 || res.TrashID != "" {
		t.Fatalf("move failure swallowed or tree stashed anyway: %+v", res)
	}
	docs, _ := h.svc.ListDocuments(ctx, h.actor, h.space, false)
	found := false
	for _, d := range docs {
		if d.ID == doc.ID && d.Path == "F/n.md" {
			found = true
		}
	}
	if !found {
		t.Fatal("document with failed byte move disappeared from the active list")
	}
	nc, err := h.svc.ReadNote(ctx, h.actor, h.space, doc.ID)
	if err != nil || nc.Markdown != "keep me" {
		t.Fatalf("document content damaged by failed trash: %v %+v", err, nc)
	}
	if ok, _ := h.svc.Content.Exists(h.space, "F/pic.png"); !ok {
		t.Fatal("attachment hidden behind a stash that was never created")
	}
	fc.failMoveTo = nil // fault cleared
	res2, err := h.svc.TrashFolder(ctx, h.actor, h.space, "F")
	if err != nil || res2.Trashed != 1 || res2.TrashID == "" || len(res2.Failures) > 0 {
		t.Fatalf("retry after fault: %v %+v", err, res2)
	}
	if rr, err := h.svc.RestoreFolder(ctx, h.actor, h.space, res2.TrashID); err != nil {
		t.Fatal(err)
	} else if rr.Restored != 1 {
		t.Fatalf("restore: %+v", rr)
	}
	nc, err = h.svc.ReadNote(ctx, h.actor, h.space, doc.ID)
	if err != nil || nc.Markdown != "keep me" {
		t.Fatalf("round trip lost content: %v %+v", err, nc)
	}
}

func TestFolderTrashTombstoneFailureStaysActive(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "F/n.md", "stay", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Content.WriteAtomic(h.space, "F/pic.png", pngBytes); err != nil {
		t.Fatal(err)
	}
	h.svc.Docs = &flakyTrashDocs{DocumentRepo: h.svc.Docs, failNextTombstone: true}
	res, err := h.svc.TrashFolder(ctx, h.actor, h.space, "F")
	if err != nil {
		t.Fatal(err)
	}
	if res.Trashed != 0 || res.TrashID != "" || len(res.Failures) == 0 {
		t.Fatalf("tombstone failure reported as success: %+v", res)
	}
	docs, _ := h.svc.ListDocuments(ctx, h.actor, h.space, false)
	for _, d := range docs {
		if d.ID == doc.ID && d.Path != "F/n.md" {
			t.Fatalf("path changed although trash failed: %s", d.Path)
		}
	}
	if ok, _ := h.svc.Content.Exists(h.space, "F/pic.png"); !ok {
		t.Fatal("live attachment bytes were stashed although a document stayed active")
	}
}

// ---------- F4: attachment boundaries and key scopes ----------

func TestAttachmentSizeLimitBoundaries(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.svc.Cfg.MaxAttachBytes = 64
	for _, n := range []int{63, 64} {
		if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "ok"+string(rune('a'+n%26))+".png", pngOfSize(n)); err != nil {
			t.Fatalf("size %d rejected: %v", n, err)
		}
	}
	_, err := h.svc.PutAttachment(ctx, h.actor, h.space, "big.png", pngOfSize(65))
	var de *domain.Error
	if err == nil || !errors.As(err, &de) || de.Code != domain.ErrCodeTooLarge || de.Status != 413 {
		t.Fatalf("oversize must be 413 too_large, got %v", err)
	}
	if _, rerr := h.svc.Content.Read(h.space, "big.png"); rerr == nil {
		t.Fatal("rejected upload left a stored file")
	}
}

func TestAttachmentKeyScopes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "pic.png", pngBytes); err != nil {
		t.Fatal(err)
	}
	mkKey := func(scopes ...string) *Actor {
		return &Actor{UserID: h.actor.UserID,
			Key: &ports.APIKeyRecord{ID: "key_test", Scopes: scopes, SpaceIDs: []string{h.space}}}
	}
	if _, _, err := h.svc.ReadAttachment(ctx, mkKey(domain.ScopeDocumentsRead), h.space, "pic.png"); err == nil ||
		!strings.Contains(err.Error(), "attachments:read") {
		t.Fatalf("documents:read key must not read attachments, got %v", err)
	}
	// documented inheritance: an empty scope list inherits the holder's role
	if _, _, err := h.svc.ReadAttachment(ctx, mkKey(), h.space, "pic.png"); err != nil {
		t.Fatalf("scope-less key should inherit the holder's read role: %v", err)
	}
	if _, _, err := h.svc.ReadAttachment(ctx, mkKey(domain.ScopeAttachmentsRead), h.space, "pic.png"); err != nil {
		t.Fatalf("attachments:read key rejected: %v", err)
	}
	if _, err := h.svc.PutAttachment(ctx, mkKey(domain.ScopeDocumentsWrite), h.space, "x.png", pngBytes); err == nil ||
		!strings.Contains(err.Error(), "attachments:write") {
		t.Fatalf("documents:write key must not write attachments, got %v", err)
	}
	if _, err := h.svc.PutAttachment(ctx, mkKey(domain.ScopeAttachmentsWrite), h.space, "x.png", pngBytes); err != nil {
		t.Fatalf("attachments:write key rejected: %v", err)
	}
}

func TestSaveVerificationFailureFinalizesAtReconcile(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "a.md", "old", "", false)
	if err != nil {
		t.Fatal(err)
	}
	origDocs := h.svc.Docs
	armed := false
	fd := &flakyDocs{DocumentRepo: origDocs, failReadAfter: func() bool { return armed }}
	h.svc.Docs = fd
	original := h.svc.Content
	p := &pausedContent{ContentStore: original, FolderStore: original.(ports.FolderStore),
		entered: make(chan struct{}), resume: make(chan struct{})}
	h.svc.Content = p
	done := make(chan error, 1)
	go func() {
		_, e := h.svc.SaveContent(ctx, h.actor, h.space, doc.ID, doc.CurrentRevision, "new edit", "")
		done <- e
	}()
	<-p.entered
	armed = true // metadata re-read fails exactly at the post-write verification
	close(p.resume)
	if e := <-done; e == nil {
		t.Fatal("save must not be acknowledged when post-write verification fails")
	}
	armed = false
	h.svc.Docs = origDocs
	// the revision was not acknowledged: current revision is still the old one
	cur, _ := origDocs.Document(ctx, doc.ID)
	if cur.CurrentRevision != doc.CurrentRevision {
		t.Fatal("unverified save acknowledged a new revision")
	}
	// reconciliation replays finalization from the journaled bytes
	resolved, blocked := h.svc.ReconcileOperations(ctx)
	if resolved < 1 || len(blocked) != 0 {
		t.Fatalf("interrupted write not finalized: resolved=%d blocked=%v", resolved, blocked)
	}
	nc, err := h.svc.ReadNote(ctx, h.actor, h.space, doc.ID)
	if err != nil || nc.Markdown != "new edit" {
		t.Fatalf("journal replay lost the edit: %v %+v", err, nc)
	}
}

func TestNoteTrashStorageFailureKeepsActive(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d, e := h.svc.CreateNote(ctx, h.actor, h.space, "keep.md", "keep safe", "", false)
	if e != nil {
		t.Fatal(e)
	}
	h.svc.Content = &flakyContent{ContentStore: h.svc.Content, failMoveTo: func(to string) bool { return strings.HasPrefix(to, trashPrefix) }}
	if e = h.svc.Trash(ctx, h.actor, h.space, d.ID, d.CurrentRevision); e == nil {
		t.Fatal("failed storage move reported success")
	}
	current, e := h.svc.Docs.Document(ctx, d.ID)
	if e != nil || current.TombstoneAt != nil {
		t.Fatalf("failed trash hid document: %+v %v", current, e)
	}
	content, e := h.svc.ReadNote(ctx, h.actor, h.space, d.ID)
	if e != nil || content.Markdown != "keep safe" {
		t.Fatalf("active bytes lost: %+v %v", content, e)
	}
}

// History failures must never acknowledge a save or overwrite recoverable bytes.
type failingHistory struct{ ports.StateStore }

func (f failingHistory) SaveHistory(spaceID, docID, revID string, data []byte) (string, error) {
	return "", errors.New("history disk unavailable")
}
func TestHistoryFailurePreservesCurrentDocument(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d, err := h.svc.CreateNote(ctx, h.actor, h.space, "durable.md", "original", "", false)
	if err != nil {
		t.Fatal(err)
	}
	h.svc.State = failingHistory{h.svc.State}
	if _, err = h.svc.SaveContent(ctx, h.actor, h.space, d.ID, d.CurrentRevision, "replacement", ""); err == nil {
		t.Fatal("save succeeded without durable history")
	}
	b, err := h.svc.Content.Read(h.space, "durable.md")
	if err != nil || string(b) != "original" {
		t.Fatalf("lost original: %q %v", b, err)
	}
	if _, err = h.svc.CreateNote(ctx, h.actor, h.space, "new.md", "new", "", false); err == nil {
		t.Fatal("create succeeded without history")
	}
	exists, err := h.svc.Content.Exists(h.space, "new.md")
	if err != nil || exists {
		t.Fatalf("failed create wrote content: %v %v", exists, err)
	}
}

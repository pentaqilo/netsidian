package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/storage/local"
)

type harness struct {
	svc   *Services
	actor *Actor
	space string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "state", "database", "netsidian.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.SQL.Close() })
	content, err := local.NewContent(filepath.Join(dir, "state", "content"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := local.NewState(filepath.Join(dir, "state", "journal"), filepath.Join(dir, "state", "history"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewServices(Config{
		AuthMode:           "none",
		MaxNoteBytes:       1 << 20,
		TrashRetentionDays: 30,
	}, db, db, db, db, db, db, db, content, state)
	ctx := context.Background()
	u, err := svc.EnsureNoAuthPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	// the no-auth principal already owns a personal space
	spaces, _, err := svc.Spaces.SpacesForUser(ctx, u.ID)
	if err != nil || len(spaces) == 0 {
		t.Fatalf("personal space missing: %v", err)
	}
	return &harness{svc: svc, actor: a, space: spaces[0].ID}
}

func TestJourneyCreateSaveConflict(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	meta, err := h.svc.CreateNote(ctx, h.actor, h.space, "Notes/A.md", "# A\n\nbody one\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	nc, err := h.svc.ReadNote(ctx, h.actor, h.space, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if nc.Markdown != "# A\n\nbody one\n" || nc.Revision != meta.CurrentRevision {
		t.Fatalf("read mismatch %+v", nc)
	}
	// missing If-Match must require a precondition
	if _, err := h.svc.SaveContent(ctx, h.actor, h.space, meta.ID, "", "changed", ""); err == nil {
		t.Fatal("expected precondition-required error")
	} else if de, ok := err.(*domain.Error); !ok || de.Code != domain.ErrCodePreconditionReq {
		t.Fatalf("expected 428 code, got %v", err)
	}
	// stale revision fails with current revision attached
	if _, err := h.svc.SaveContent(ctx, h.actor, h.space, meta.ID, "rev_stale", "changed", ""); err == nil {
		t.Fatal("expected precondition failure")
	} else if de, ok := err.(*domain.Error); !ok || de.Code != domain.ErrCodePrecondition || de.CurrentRevision != meta.CurrentRevision {
		t.Fatalf("expected 412 with current revision, got %v", err)
	}
	// correct revision commits
	sr, err := h.svc.SaveContent(ctx, h.actor, h.space, meta.ID, meta.CurrentRevision, "# A v2\n\nbody two\n", "")
	if err != nil {
		t.Fatal(err)
	}
	nc2, err := h.svc.ReadNote(ctx, h.actor, h.space, meta.ID)
	if err != nil || nc2.Markdown != "# A v2\n\nbody two\n" || nc2.Revision != sr.Revision {
		t.Fatalf("after save: %+v %v", nc2, err)
	}
	// revisions history keeps both
	revs, err := h.svc.Revisions(ctx, h.actor, h.space, meta.ID)
	if err != nil || len(revs) != 2 {
		t.Fatalf("revisions=%d %v", len(revs), err)
	}
	old, err := h.svc.RevisionContent(ctx, h.actor, h.space, meta.ID, revs[1].ID)
	if err != nil || old != "# A\n\nbody one\n" {
		t.Fatalf("historical revision content=%q err=%v", old, err)
	}
}

func TestIdempotentCreate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m1, err := h.svc.CreateNote(ctx, h.actor, h.space, "Dup.md", "one", "idem-1", false)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := h.svc.CreateNote(ctx, h.actor, h.space, "Dup.md", "one", "idem-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if m1.ID != m2.ID {
		t.Fatalf("idempotency broken: %s != %s", m1.ID, m2.ID)
	}
	// same key, different payload -> conflict
	if _, err := h.svc.CreateNote(ctx, h.actor, h.space, "Dup.md", "different", "idem-1", false); err == nil {
		t.Fatal("expected conflict for reused idempotency key with new payload")
	} else if de, ok := err.(*domain.Error); !ok || de.Code != domain.ErrCodeConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestSearchTrashRestoreGraph(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	aMeta, err := h.svc.CreateNote(ctx, h.actor, h.space, "A.md", "# Alpha\n\nlinks to [[B]]\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	bMeta, err := h.svc.CreateNote(ctx, h.actor, h.space, "B.md", "# Beta\n\nbeing linked\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := h.svc.Search(ctx, h.actor, &ports.SearchQuery{SpaceID: h.space, Text: "Alpha", Limit: 10})
	if err != nil || len(hits) == 0 || hits[0].DocumentID != aMeta.ID {
		t.Fatalf("search hits=%v err=%v", hits, err)
	}
	// link resolves now that B exists
	bl, err := h.svc.Backlinks(ctx, h.actor, h.space, bMeta.ID)
	if err != nil || len(bl) != 1 || bl[0].DocumentID != aMeta.ID {
		t.Fatalf("backlinks=%v err=%v", bl, err)
	}
	g, err := h.svc.Graph(ctx, h.actor, h.space, 50)
	if err != nil || len(g.Edges) != 1 {
		t.Fatalf("graph edges=%v err=%v", g.Edges, err)
	}
	// trash hides and removes from index
	if err := h.svc.Trash(ctx, h.actor, h.space, bMeta.ID, bMeta.CurrentRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ReadNote(ctx, h.actor, h.space, bMeta.ID); err == nil {
		t.Fatal("trashed note still readable")
	}
	g2, _ := h.svc.Graph(ctx, h.actor, h.space, 50)
	if len(g2.Edges) != 0 {
		t.Fatalf("edge survived trash: %v", g2.Edges)
	}
	// restore brings it back and re-resolves the link
	if err := h.svc.Restore(ctx, h.actor, h.space, bMeta.ID); err != nil {
		t.Fatal(err)
	}
	nc, err := h.svc.ReadNote(ctx, h.actor, h.space, bMeta.ID)
	if err != nil || nc.Markdown != "# Beta\n\nbeing linked\n" {
		t.Fatalf("restore content=%q err=%v", nc.Markdown, err)
	}
	g3, _ := h.svc.Graph(ctx, h.actor, h.space, 50)
	if len(g3.Edges) != 1 {
		t.Fatalf("edge not restored: %v", g3.Edges)
	}
}

func TestRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	open := func() *Services {
		db, err := sqlite.Open(filepath.Join(dir, "state", "database", "netsidian.db"), 5000)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.SQL.Close() })
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
		resolved, blocked := svc.ReconcileOperations(context.Background())
		if len(blocked) != 0 {
			t.Fatalf("unexpected blocked operations: %v", blocked)
		}
		_ = resolved
		return svc
	}
	svc := open()
	ctx := context.Background()
	u, err := svc.EnsureNoAuthPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	spaces, _, err := svc.Spaces.SpacesForUser(ctx, u.ID)
	if err != nil || len(spaces) == 0 {
		t.Fatalf("personal space missing: %v", err)
	}
	space := spaces[0]
	meta, err := svc.CreateNote(ctx, a, space.ID, "Keep.md", "# Kept\n\npersist me\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveContent(ctx, a, space.ID, meta.ID, meta.CurrentRevision, "# Kept v2\n\npersist me\n", ""); err != nil {
		t.Fatal(err)
	}
	// simulate process restart
	svc2 := open()
	u2, err := svc2.EnsureNoAuthPrincipal(ctx)
	if err != nil || u2.ID != u.ID {
		t.Fatalf("principal drift: %v", err)
	}
	a2 := &Actor{UserID: u2.ID, IsOperator: true, Local: true}
	nc, err := svc2.ReadNote(ctx, a2, space.ID, meta.ID)
	if err != nil || nc.Markdown != "# Kept v2\n\npersist me\n" {
		t.Fatalf("persisted content wrong: %q %v", nc.Markdown, err)
	}
}

func TestRoleEnforcementOnReads(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	stranger := &Actor{UserID: "usr_stranger"}
	if _, err := h.svc.ReadNote(ctx, stranger, h.space, "doc_whatever"); err == nil {
		t.Fatal("non-member read allowed")
	} else if de, ok := err.(*domain.Error); !ok || de.Code != domain.ErrCodeNotFound {
		t.Fatalf("want not-found, got %v", err)
	}
	if _, err := h.svc.CreateSpace(ctx, stranger, "Evil", "shared"); err == nil {
		t.Fatal("non-operator can create shared spaces")
	}
}

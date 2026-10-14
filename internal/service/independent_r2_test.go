package service

// Independent R2 regressions from review/cr2/CP-A-independent/r2-validation/,
// copied verbatim except where the review itself instructs a scheduling
// adjustment (REVIEW-INDEPENDENT-CP-A-R2.md line 25): with saves serialized
// against folder changes, rename may wait for a paused save, so the rename
// runs concurrently, the paused save is released, and both are awaited with
// timeouts before the invariant is asserted.

import (
	"context"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/ports"
)

type followPause struct {
	ports.ContentStore
	ports.FolderStore
	writeEntered, writeResume, moveEntered, moveResume chan struct{}
}

func (p *followPause) WriteAtomic(s, path string, b []byte) error {
	close(p.writeEntered)
	<-p.writeResume
	return p.ContentStore.WriteAtomic(s, path, b)
}

func (p *followPause) Move(s, from, to string) error {
	close(p.moveEntered)
	<-p.moveResume
	return p.ContentStore.Move(s, from, to)
}

func TestIndependentR2SecondRenameDuringFollow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d, e := h.svc.CreateNote(ctx, h.actor, h.space, "Before/a.md", "old", "", false)
	if e != nil {
		t.Fatal(e)
	}
	original := h.svc.Content
	p := &followPause{original, original.(ports.FolderStore), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})}
	h.svc.Content = p
	close(p.moveResume) // under serialization a save no longer relocates; a Move here would be a defect
	done := make(chan error, 1)
	go func() {
		_, e := h.svc.SaveContent(ctx, h.actor, h.space, d.ID, d.CurrentRevision, "new edit", "")
		done <- e
	}()
	select {
	case <-p.writeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("write timeout")
	}
	// scheduling adjustment per review line 25: run the rename concurrently
	rdone := make(chan error, 1)
	go func() {
		_, e := h.svc.RenameFolder(ctx, h.actor, h.space, "Before", "After")
		rdone <- e
	}()
	close(p.writeResume)
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("save did not complete")
	}
	select {
	case e := <-rdone:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("rename did not complete")
	}
	_, e = h.svc.RenameFolder(ctx, h.actor, h.space, "After", "Final")
	if e != nil {
		t.Fatal(e)
	}
	current, _ := original.Read(h.space, "Final/a.md")
	orphan, _ := original.Read(h.space, "After/a.md")
	if string(current) != "new edit" {
		t.Fatalf("save and both renames succeeded: Final/a.md=%q, orphan After/a.md=%q", current, orphan)
	}
}

func TestIndependentR2FailedRollbackKeepsRecoveryOpen(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for _, p := range []string{"Before/a.md", "Before/b.md"} {
		if _, e := h.svc.CreateNote(ctx, h.actor, h.space, p, "original", "", false); e != nil {
			t.Fatal(e)
		}
	}
	original := h.svc.Docs
	calls := 0
	h.svc.Docs = &flakyDocs{DocumentRepo: original, failPathTo: func(path string) bool { calls++; return calls >= 2 }}
	if _, e := h.svc.RenameFolder(ctx, h.actor, h.space, "Before", "After"); e == nil {
		t.Fatal("expected failure")
	}
	h.svc.Docs = original
	_, blocked := h.svc.ReconcileOperations(ctx)
	docs, e := h.svc.ListDocuments(ctx, h.actor, h.space, false)
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range docs {
		if _, e := h.svc.Content.Read(h.space, d.Path); e != nil && len(blocked) == 0 {
			t.Fatalf("metadata points at missing %s after failed rollback; reconciliation reports no blocked recovery", d.Path)
		}
	}
}

package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/ports"
	"testing"
	"time"
)

type pausedContent struct {
	ports.ContentStore
	ports.FolderStore
	entered chan struct{}
	resume  chan struct{}
}

func (p *pausedContent) WriteAtomic(s, path string, b []byte) error {
	close(p.entered)
	<-p.resume
	return p.ContentStore.WriteAtomic(s, path, b)
}

// These assert safe behavior: failures are regression evidence, not passing repro tests.
func TestIndependentCPARenameDuringSave(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "Before/a.md", "old", "", false)
	if err != nil {
		t.Fatal(err)
	}
	original := h.svc.Content
	p := &pausedContent{ContentStore: original, FolderStore: original.(ports.FolderStore), entered: make(chan struct{}), resume: make(chan struct{})}
	h.svc.Content = p
	done := make(chan error, 1)
	go func() {
		_, e := h.svc.SaveContent(ctx, h.actor, h.space, doc.ID, doc.CurrentRevision, "new edit", "")
		done <- e
	}()
	select {
	case <-p.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("save never reached storage")
	}
	// Scheduling adapted per review R2 line 25: rename runs concurrently,
	// the paused save is released, and both are awaited with timeouts.
	// Saves are serialized against folder changes (space lock), so rename
	// legitimately waits for the save; the invariant below is the claim.
	rdone := make(chan error, 1)
	go func() {
		_, e := h.svc.RenameFolder(ctx, h.actor, h.space, "Before", "After")
		rdone <- e
	}()
	close(p.resume)
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("save did not complete after release")
	}
	select {
	case e := <-rdone:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("rename did not complete after save released the space lock")
	}
	moved, err := original.Read(h.space, "After/a.md")
	if err != nil {
		t.Fatal(err)
	}
	orphan, _ := original.Read(h.space, "Before/a.md")
	if string(moved) != "new edit" {
		t.Fatalf("both operations succeeded, but new path contains %q and orphan old path contains %q", moved, orphan)
	}
}

func TestIndependentCPARestoreAttachmentCollision(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.svc.Content.WriteAtomic(h.space, "Folder/image.png", []byte("original attachment")); err != nil {
		t.Fatal(err)
	}
	tr, err := h.svc.TrashFolder(ctx, h.actor, h.space, "Folder")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Content.WriteAtomic(h.space, "Folder/image.png", []byte("new attachment")); err != nil {
		t.Fatal(err)
	}
	result, err := h.svc.RestoreFolder(ctx, h.actor, h.space, tr.TrashID)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := h.svc.ListFolderTrashes(ctx, h.actor, h.space)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := h.svc.Content.Read(h.space, "Folder/image.png")
	if len(entries) == 0 && len(result.Failures) == 0 {
		t.Fatalf("restore reports success, keeps %q, deletes original stash and its recovery entry", current)
	}
}

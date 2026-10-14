package service

import (
	"bytes"
	"context"
	"testing"
)

var pngBytes = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R'}

func TestFolderRenameMovesDocuments(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	one, err := h.svc.CreateNote(ctx, h.actor, h.space, "Projects/A/one.md", "one", "", false)
	if err != nil {
		t.Fatal(err)
	}
	two, err := h.svc.CreateNote(ctx, h.actor, h.space, "Projects/A/sub/two.md", "two", "", false)
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.RenameFolder(ctx, h.actor, h.space, "Projects", "Work/Projects")
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "Work/Projects" || res.Moved != 2 {
		t.Fatalf("rename result %+v", res)
	}
	docs, _ := h.svc.ListDocuments(ctx, h.actor, h.space, false)
	got := map[string]string{}
	for _, d := range docs {
		got[d.ID] = d.Path
	}
	if got[one.ID] != "Work/Projects/A/one.md" || got[two.ID] != "Work/Projects/A/sub/two.md" {
		t.Fatalf("paths after rename: %v", got)
	}
	nc, err := h.svc.ReadNote(ctx, h.actor, h.space, one.ID)
	if err != nil || nc.Markdown != "one" {
		t.Fatalf("content lost after rename: %v %+v", err, nc)
	}
	folders, _ := h.svc.ListFolders(ctx, h.actor, h.space)
	for _, f := range folders {
		if f == "Projects" || f == "Projects/A" || f == "Projects/A/sub" {
			t.Fatalf("stale folder left behind: %v", folders)
		}
	}
	// moving a folder into itself must fail
	if _, err := h.svc.RenameFolder(ctx, h.actor, h.space, "Work", "Work/Projects/X"); err == nil {
		t.Fatal("renaming into own descendant allowed")
	}
}

func TestFolderRenameConflictAndDescendant(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.svc.CreateFolder(ctx, h.actor, h.space, "Work/P"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.CreateNote(ctx, h.actor, h.space, "Projects/one.md", "one", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.RenameFolder(ctx, h.actor, h.space, "Projects", "Work/P"); err == nil {
		t.Fatal("rename onto occupied target allowed")
	}
	if _, err := h.svc.RenameFolder(ctx, h.actor, h.space, "Work", "Work/P/inner"); err == nil {
		t.Fatal("rename into descendant allowed")
	}
}

func TestFolderRenameKeepsTrashedDocsRestorable(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	gone, err := h.svc.CreateNote(ctx, h.actor, h.space, "Proj/gone.md", "gone", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Trash(ctx, h.actor, h.space, gone.ID, gone.CurrentRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.RenameFolder(ctx, h.actor, h.space, "Proj", "Renamed"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Restore(ctx, h.actor, h.space, gone.ID); err != nil {
		t.Fatalf("restore after folder rename: %v", err)
	}
	docs, _ := h.svc.ListDocuments(ctx, h.actor, h.space, false)
	for _, d := range docs {
		if d.ID == gone.ID && d.Path != "Renamed/gone.md" {
			t.Fatalf("restored under old path: %s", d.Path)
		}
	}
}

func TestFolderTrashAndRestoreKeepsAttachments(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "Trip/notes.md", "trip notes", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "Trip/map.png", pngBytes); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.CreateFolder(ctx, h.actor, h.space, "Trip/Sub"); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.TrashFolder(ctx, h.actor, h.space, "Trip")
	if err != nil {
		t.Fatal(err)
	}
	if res.Trashed != 1 || res.TrashID == "" || len(res.Failures) > 0 {
		t.Fatalf("trash folder result %+v", res)
	}
	all, _ := h.svc.ListDocuments(ctx, h.actor, h.space, true)
	for _, d := range all {
		if d.ID == doc.ID && d.TombstoneAt == nil {
			t.Fatal("document still active after folder trash")
		}
	}
	atts, _ := h.svc.ListAttachments(ctx, h.actor, h.space, "")
	for _, a := range atts {
		if a.Path == "Trip/map.png" {
			t.Fatal("attachment visible after folder trash")
		}
	}
	trashes, _ := h.svc.ListFolderTrashes(ctx, h.actor, h.space)
	if len(trashes) != 1 || trashes[0].Path != "Trip" || len(trashes[0].DocIDs) != 1 {
		t.Fatalf("folder trash listing %+v", trashes)
	}
	rres, err := h.svc.RestoreFolder(ctx, h.actor, h.space, trashes[0].TrashID)
	if err != nil || len(rres.Failures) > 0 {
		t.Fatalf("restore folder %+v %v", rres, err)
	}
	atts, _ = h.svc.ListAttachments(ctx, h.actor, h.space, "")
	found := false
	for _, a := range atts {
		if a.Path == "Trip/map.png" {
			found = true
		}
	}
	if !found {
		t.Fatal("attachment not restored")
	}
	nc, err := h.svc.ReadNote(ctx, h.actor, h.space, doc.ID)
	if err != nil || nc.Markdown != "trip notes" {
		t.Fatalf("doc not restored: %v", err)
	}
}

func TestAttachmentGuards(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "img/photo.png", pngBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "notes.txt", []byte("plain")); err == nil {
		t.Fatal("txt extension allowed")
	}
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "evil.png", []byte("<html><script>x</script></html>")); err == nil {
		t.Fatal("content mismatch allowed")
	}
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "../escape.png", pngBytes); err == nil {
		t.Fatal("path escape allowed")
	}
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, ".hidden/x.png", pngBytes); err == nil {
		t.Fatal("dot segment allowed")
	}
	if _, err := h.svc.CreateNote(ctx, h.actor, h.space, "conflict.png.md", "x", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "conflict.png.md", pngBytes); err == nil {
		t.Fatal("attachment may shadow a document path")
	}
	data, mime, err := h.svc.ReadAttachment(ctx, h.actor, h.space, "img/photo.png")
	if err != nil || mime != "image/png" || !bytes.Equal(data, pngBytes) {
		t.Fatalf("read attachment %v %+v", err, mime)
	}
	if _, _, err := h.svc.ReadAttachment(ctx, h.actor, h.space, "img/photo.exe"); err == nil {
		t.Fatal("read of disallowed extension")
	}
}

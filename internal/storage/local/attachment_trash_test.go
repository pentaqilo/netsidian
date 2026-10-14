package local

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAttachmentTrashInterruptionKeepsRecoverableRecord(t *testing.T) {
	c := newContent(t)
	space := "sp1"
	id := "ftr_interrupted"
	if err := c.WriteAtomic(space, "picture.png", []byte("original")); err != nil {
		t.Fatal(err)
	}
	meta, content := attachmentTrashPaths(id)
	data, _ := json.Marshal(folderTrashMeta{Kind: "attachment", Path: "picture.png", TrashedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	if err := c.WriteAtomic(space, meta, data); err != nil {
		t.Fatal(err)
	}
	entries, err := c.StashedFolders(space)
	if err != nil || len(entries) != 0 {
		t.Fatalf("pending record shown as trashed: %+v %v", entries, err)
	}
	if err := c.Move(space, "picture.png", content); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewContent(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	entries, err = restarted.StashedFolders(space)
	if err != nil || len(entries) != 1 {
		t.Fatalf("interrupted move lost restore record: %+v %v", entries, err)
	}
	if _, conflicts, err := restarted.UnstashFolder(space, id); err != nil || len(conflicts) != 0 {
		t.Fatalf("restore after interruption: %v %v", conflicts, err)
	}
	data, err = restarted.Read(space, "picture.png")
	if err != nil || string(data) != "original" {
		t.Fatal("original bytes missing")
	}
}
func TestAttachmentTrashRejectsAttachedFolderAndSymlink(t *testing.T) {
	c := newContent(t)
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "photo.png"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.BindExternal("sp1", external); err != nil {
		t.Fatal(err)
	}
	if err := c.StashAttachment("sp1", "photo.png", "ftr_external"); err == nil {
		t.Fatal("attached file moved into app trash")
	}
	if data, err := os.ReadFile(filepath.Join(external, "photo.png")); err != nil || string(data) != "original" {
		t.Fatal("attached original changed")
	}
	if err := c.WriteAtomic("sp2", "photo.png", []byte("managed")); err != nil {
		t.Fatal(err)
	}
	trash := filepath.Join(c.Root, "spaces", "sp2", ".netsidian-trash")
	if err := os.Symlink(t.TempDir(), trash); err != nil {
		t.Fatal(err)
	}
	if err := c.StashAttachment("sp2", "photo.png", "ftr_symlink"); err == nil {
		t.Fatal("trash symlink accepted")
	}
	if data, err := c.Read("sp2", "photo.png"); err != nil || string(data) != "managed" {
		t.Fatal("symlink rejection changed original")
	}
}

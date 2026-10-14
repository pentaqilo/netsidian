package service

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/storage/local"
)

func TestAttachmentTrashRestorePreservesOriginalAndConflict(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "Trip/map.png", pngBytes); err != nil {
		t.Fatal(err)
	}
	entry, err := h.svc.TrashAttachment(ctx, h.actor, h.space, "Trip/map.png")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Kind != "attachment" || entry.TrashID == "" {
		t.Fatalf("entry: %+v", entry)
	}
	if _, _, err := h.svc.ReadAttachment(ctx, h.actor, h.space, "Trip/map.png"); err == nil {
		t.Fatal("trashed attachment served")
	}
	// Fresh storage adapter proves the restore catalog survives a process restart.
	original := h.svc.Content.(*local.Content)
	restarted, err := local.NewContent(original.Root)
	if err != nil {
		t.Fatal(err)
	}
	h.svc.Content = restarted
	entries, err := h.svc.ListFolderTrashes(ctx, h.actor, h.space)
	if err != nil || len(entries) != 1 || entries[0].Kind != "attachment" {
		t.Fatalf("persistent trash: %+v %v", entries, err)
	}
	replacement := append(append([]byte{}, pngBytes...), []byte("replacement")...)
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "Trip/map.png", replacement); err != nil {
		t.Fatal(err)
	}
	result, err := h.svc.RestoreFolder(ctx, h.actor, h.space, entry.TrashID)
	if err != nil || len(result.Conflicts) != 1 {
		t.Fatalf("restore conflict: %+v %v", result, err)
	}
	live, _, _ := h.svc.ReadAttachment(ctx, h.actor, h.space, "Trip/map.png")
	if !bytes.Equal(live, replacement) {
		t.Fatal("restore overwrote replacement")
	}
	if err := h.svc.Content.Delete(h.space, "Trip/map.png"); err != nil {
		t.Fatal(err)
	}
	result, err = h.svc.RestoreFolder(ctx, h.actor, h.space, entry.TrashID)
	if err != nil || len(result.Failures) > 0 || result.Restored != 1 {
		t.Fatalf("restore: %+v %v", result, err)
	}
	live, _, err = h.svc.ReadAttachment(ctx, h.actor, h.space, "Trip/map.png")
	if err != nil || !bytes.Equal(live, pngBytes) {
		t.Fatalf("original not restored: %v", err)
	}
	entries, err = h.svc.ListFolderTrashes(ctx, h.actor, h.space)
	if err != nil || len(entries) != 0 {
		t.Fatalf("restored entry remains: %+v %v", entries, err)
	}
}
func TestAttachmentTrashRejectsPathsMissingFilesAndInsufficientScope(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "map.png", pngBytes); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../map.png", ".netsidian-trash/map.png", "missing.png", "map.md"} {
		if _, err := h.svc.TrashAttachment(ctx, h.actor, h.space, path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	key := &ports.APIKeyRecord{Scopes: []string{domain.ScopeDocumentsDelete}, SpaceIDs: []string{h.space}, PrincipalID: h.actor.UserID}
	actor := &Actor{UserID: h.actor.UserID, Key: key}
	if _, err := h.svc.TrashAttachment(ctx, actor, h.space, "map.png"); err == nil {
		t.Fatal("key without attachment-write scope trashed attachment")
	}
	user := &domain.User{ID: domain.NewID("usr"), DisplayName: "Viewer", CreatedAt: time.Now()}
	if err := h.svc.Users.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Spaces.AddMember(ctx, &domain.Membership{SpaceID: h.space, UserID: user.ID, Role: domain.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.TrashAttachment(ctx, &Actor{UserID: user.ID}, h.space, "map.png"); err == nil {
		t.Fatal("viewer trashed attachment")
	}
	live, _, err := h.svc.ReadAttachment(ctx, h.actor, h.space, "map.png")
	if err != nil || !bytes.Equal(live, pngBytes) {
		t.Fatal("rejected trash changed original")
	}
}

func TestAttachmentMovePreservesBytesAndRejectsOccupiedTarget(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "source.png", pngBytes); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.MoveAttachment(ctx, h.actor, h.space, "source.png", "Folder/moved.png"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.svc.ReadAttachment(ctx, h.actor, h.space, "source.png"); err == nil {
		t.Fatal("move left source active")
	}
	if data, _, err := h.svc.ReadAttachment(ctx, h.actor, h.space, "Folder/moved.png"); err != nil || !bytes.Equal(data, pngBytes) {
		t.Fatal("move changed bytes")
	}
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "occupied.png", pngBytes); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.MoveAttachment(ctx, h.actor, h.space, "Folder/moved.png", "occupied.png"); err == nil {
		t.Fatal("occupied destination overwritten")
	}
	if err := h.svc.MoveAttachment(ctx, h.actor, h.space, "Folder/moved.png", "new.pdf"); err == nil {
		t.Fatal("move changed attachment type")
	}
}

func TestAttachmentImportCannotOverwriteARacingUpload(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.PutAttachmentIfAbsent(ctx, h.actor, h.space, "copy.png", pngBytes); err != nil {
		t.Fatal(err)
	}
	replacement := append(append([]byte{}, pngBytes...), []byte("second upload")...)
	if _, err := h.svc.PutAttachmentIfAbsent(ctx, h.actor, h.space, "copy.png", replacement); err == nil {
		t.Fatal("create-only import overwrote a file")
	}
	got, _, err := h.svc.ReadAttachment(ctx, h.actor, h.space, "copy.png")
	if err != nil || !bytes.Equal(got, pngBytes) {
		t.Fatal("first upload was changed")
	}
}

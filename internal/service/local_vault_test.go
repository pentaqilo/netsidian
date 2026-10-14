package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/storage/local"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteManagedVaultRequiresNameAndRemovesContent(t *testing.T) {
	st := openSafetyStack(t, t.TempDir())
	actor, _ := st.principal(t)
	ctx := context.Background()
	sp, err := st.svc.CreateSpace(ctx, actor, "Disposable", "shared")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.svc.CreateNote(ctx, actor, sp.ID, "A.md", "# Secret\n", "", false); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(st.svc.Content.(*local.Content).Root, "spaces", sp.ID)
	if err = st.svc.DeleteVault(ctx, actor, sp.ID, "wrong"); err == nil {
		t.Fatal("wrong confirmation accepted")
	}
	if _, err = os.Stat(filepath.Join(root, "A.md")); err != nil {
		t.Fatal("content removed before confirmation", err)
	}
	if err = st.svc.DeleteVault(ctx, actor, sp.ID, sp.Name); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("managed content still exists: %v", err)
	}
	if got, err := st.svc.Spaces.Space(ctx, sp.ID); err != nil || got != nil {
		t.Fatalf("metadata still exists: %v %v", got, err)
	}
}

func TestAttachLocalVaultIndexesInPlaceAndDetachKeepsFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Vault")
	if err := os.MkdirAll(filepath.Join(root, "Folder"), 0700); err != nil {
		t.Fatal(err)
	}
	source := []byte("# First\n\nLink [[Folder/Second]]\n")
	if err := os.WriteFile(filepath.Join(root, "README.md"), source, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Folder", "Second.md"), []byte("# Second\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Folder", "image.png"), []byte("\x89PNG\r\n\x1a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.md"), []byte("must not appear"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "External link")); err != nil {
		t.Fatal(err)
	}
	testAttachedFolder(t, root, 2, "README.md", source)
}

// Set NETSIDIAN_TEST_VAULT_ROOT for the local acceptance run. The folder is
// indexed and detached without changing its contents.
func TestAttachRealLocalVaultReadOnly(t *testing.T) {
	root := os.Getenv("NETSIDIAN_TEST_VAULT_ROOT")
	if root == "" {
		t.Skip("set NETSIDIAN_TEST_VAULT_ROOT for local acceptance")
	}
	source, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	testAttachedFolder(t, root, 1, "README.md", source)
}

func testAttachedFolder(t *testing.T, root string, min int, sample string, before []byte) {
	t.Helper()
	st := openSafetyStack(t, t.TempDir())
	actor, _ := st.principal(t)
	ctx := context.Background()
	sp, count, err := st.svc.AttachLocalVault(ctx, actor, root)
	if err != nil {
		t.Fatal(err)
	}
	if count < min {
		t.Fatalf("indexed %d notes, want at least %d", count, min)
	}
	docs, err := st.svc.ListDocuments(ctx, actor, sp.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := st.svc.ListFolders(ctx, actor, sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) == 0 {
		t.Fatal("attached folder tree is empty")
	}
	if _, err := os.Stat(filepath.Join(root, "Folder", "image.png")); err == nil {
		attachments, e := st.svc.ListAttachments(ctx, actor, sp.ID, "")
		if e != nil {
			t.Fatal(e)
		}
		if len(attachments) != 1 || attachments[0].Path != "Folder/image.png" {
			t.Fatalf("attached image missing: %+v", attachments)
		}
	}
	found := false
	for _, d := range docs {
		if d.Path == "External link/outside.md" {
			t.Fatal("symlink target escaped the vault")
		}
		if d.Path == sample {
			found = true
			n, e := st.svc.ReadNote(ctx, actor, sp.ID, d.ID)
			if e != nil {
				t.Fatal(e)
			}
			if n.Markdown != string(before) {
				t.Fatal("attached note differs from source")
			}
		}
	}
	if !found {
		t.Fatalf("%s missing", sample)
	}
	if err := st.svc.DeleteVault(ctx, actor, sp.ID, sp.Name); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, sample))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("detach changed original bytes")
	}
}

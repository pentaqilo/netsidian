package sqlite

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"path/filepath"
	"testing"
	"time"
)

func TestStorageBindingAndProfileCommitTogether(t *testing.T) {
	ctx := context.Background()
	db, e := Open(filepath.Join(t.TempDir(), "state.db"), 5000)
	if e != nil {
		t.Fatal(e)
	}
	defer db.SQL.Close()
	id := "spc_storage"
	e = db.CreateSpace(ctx, &domain.Space{ID: id, Slug: "storage", Name: "Storage", Kind: "shared", StorageBinding: "local-main", CreatedAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.SaveVaultStorage(ctx, id, []byte("sealed"), "s3-vault"); e != nil {
		t.Fatal(e)
	}
	sp, _ := db.Space(ctx, id)
	b, e := db.VaultStorage(ctx, id)
	if e != nil || sp.StorageBinding != "s3-vault" || string(b) != "sealed" {
		t.Fatal("binding/profile mismatch", e)
	}
	db.SQL.Exec(`DELETE FROM vault_storage WHERE space_id=?`, id)
	if _, e = db.VaultStorage(ctx, id); e == nil {
		t.Fatal("missing profile would fall back to service storage")
	}
	if e = db.SaveVaultStorage(ctx, "spc_missing", []byte("sealed"), "s3-vault"); e == nil {
		t.Fatal("orphan profile accepted")
	}
}

func TestFolderNamedVaultDoesNotRequireProviderCredentials(t *testing.T) {
	ctx := context.Background()
	db, e := Open(filepath.Join(t.TempDir(), "state.db"), 5000)
	if e != nil {
		t.Fatal(e)
	}
	defer db.SQL.Close()
	for i, binding := range []string{"local-folder:/tmp/desktop-vault", `local-folder:C:\Notes\my-vault`, "local-main", "s3-main"} {
		id := domain.NewID("spc")
		if e = db.CreateSpace(ctx, &domain.Space{ID: id, Slug: id, Name: "Local", Kind: "shared", StorageBinding: binding, CreatedAt: time.Now()}); e != nil {
			t.Fatal(e)
		}
		profile, err := db.VaultStorage(ctx, id)
		if err != nil || profile != nil {
			t.Fatalf("binding %d %q incorrectly requires provider credentials: %v", i, binding, err)
		}
	}
}

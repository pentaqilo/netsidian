package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func TestDerivedMetadataUsesBoundedIndexedPages(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	ctx := context.Background()
	if err := db.CreateSpace(ctx, &domain.Space{ID: "vault", Slug: "vault", Name: "Vault", Kind: "shared", StorageBinding: "local-main", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 2003; i++ {
		id := fmt.Sprintf("doc-%04d", i)
		path := id + ".md"
		fm := "{}"
		if i >= 2000 {
			fm = `{"netsidian":{"derived":{"sources":[{"documentId":"source","revision":"rev"}]}}}`
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO documents(id,space_id,path,norm_key,kind,current_revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, id, "vault", path, path, "note", "rev", nowText(), nowText()); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_index(document_id,space_id,revision,frontmatter) VALUES(?,?,?,?)`, id, "vault", "rev", fm); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	first, more, err := db.DerivedMetadataPage(ctx, "vault", "", 2)
	if err != nil || len(first) != 2 || !more {
		t.Fatalf("first page: %+v %v %v", first, more, err)
	}
	second, more, err := db.DerivedMetadataPage(ctx, "vault", first[1].DocumentID, 2)
	if err != nil || len(second) != 1 || more {
		t.Fatalf("second page: %+v %v %v", second, more, err)
	}
	missing, more, err := db.DerivedMetadataPage(ctx, "other", "", 2)
	if err != nil || len(missing) != 0 || more {
		t.Fatal("cross-vault index metadata leaked", err)
	}
	rows, err := db.SQL.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT document_id FROM metadata_index WHERE space_id=? AND json_type(frontmatter,'$.netsidian.derived')='object' AND document_id>? ORDER BY document_id LIMIT 3`, "vault", "")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var a, b, c int
		var detail string
		if err := rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail
	}
	if !strings.Contains(plan, "ix_memory_derived_space_document") {
		t.Fatalf("derived status must not scan ordinary vault metadata: %s", plan)
	}
}

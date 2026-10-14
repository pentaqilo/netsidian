package sqlite

import (
	"context"
	"fmt"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"path/filepath"
	"testing"
	"time"
)

// A realistic index-size regression: retrieval stays bounded and filters
// apply in SQL before pagination. Log timings without flaky hardware gates.
func TestSearchLargeVaultBoundedFilters(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "search.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	ctx := context.Background()
	if err := db.CreateSpace(ctx, &domain.Space{ID: "vault", Slug: "large", Name: "Large", Kind: "shared", StorageBinding: "local-main", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 5000; i++ {
		id := fmt.Sprintf("doc-%05d", i)
		path := fmt.Sprintf("Knowledge/%05d.md", i)
		if i%10 == 0 {
			path = fmt.Sprintf("Agent Memory/%05d.md", i)
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO documents(id,space_id,path,norm_key,kind,current_revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, []any{id, "vault", path, path, "note", "rev", nowText(), nowText()}},
			{`INSERT INTO metadata_index(document_id,space_id,revision,title) VALUES(?,?,?,?)`, []any{id, "vault", "rev", "Topic"}},
			{`INSERT INTO search_index(document_id,space_id,revision,title,body) VALUES(?,?,?,?,?)`, []any{id, "vault", "rev", "Topic", "zebrawood knowledge storage decision"}},
			{`INSERT INTO document_tags(document_id,space_id,tag) VALUES(?,?,?)`, []any{id, "vault", "research/item"}},
		} {
			if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for i := 0; i < 25; i++ {
		hits, err := db.Search(ctx, &ports.SearchQuery{SpaceID: "vault", Text: "zebra", FolderPrefix: "Agent Memory", Tags: []string{"research/item"}, TagMode: "exact", Limit: 8, Offset: i * 8})
		if err != nil || len(hits) != 8 {
			t.Fatalf("page %d hits %d %v", i, len(hits), err)
		}
	}
	t.Logf("5000 indexed documents: average prefix+folder+tag retrieval %s", time.Since(start)/25)
	hits, err := db.Search(ctx, &ports.SearchQuery{SpaceID: "vault", Text: "!!!", Limit: 8})
	if err != nil || len(hits) != 0 {
		t.Fatalf("punctuation-only %d %v", len(hits), err)
	}
	hits, err = db.Search(ctx, &ports.SearchQuery{SpaceID: "vault", Tags: []string{"research"}, Limit: 8})
	if err != nil || len(hits) != 8 {
		t.Fatalf("tag subtree %d %v", len(hits), err)
	}
}

package sqlite

import (
	"context"
	"fmt"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/markdown"
	"github.com/pentaqilo/netsidian/internal/ports"
	"path/filepath"
	"strings"
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

func TestInlineCodeSearchMigratesExistingIndex(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "index.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	now := time.Now()
	space := &domain.Space{ID: "v", Slug: "latarnia", Name: "Latarnia", Kind: "shared", StorageBinding: "local-main", CreatedAt: now}
	if err := db.CreateSpace(ctx, space); err != nil {
		t.Fatal(err)
	}
	doc := &domain.Document{ID: "doc", SpaceID: space.ID, Path: "Linki.md", NormKey: "linki.md", Kind: "note", CurrentRevision: "rev", CreatedAt: now, UpdatedAt: now}
	if err := db.InsertDocument(ctx, doc, &domain.Revision{ID: doc.CurrentRevision, DocumentID: doc.ID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	const body = "Edytuj `config/links.json`. Każdy link ma pola `name` i `url`."
	old := &ports.IndexData{Title: "Linki", BodyPlain: "Edytuj                    . Każdy link ma pola        i      ."}
	if err := db.ReplaceDocumentIndex(ctx, space.ID, doc, doc.CurrentRevision, old); err != nil {
		t.Fatal(err)
	}
	// Replay the v10-to-v11 upgrade on the pre-change derived data.
	if _, err := db.SQL.Exec(`UPDATE meta SET value='10' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := db.migrate(); err != nil {
		t.Fatal(err)
	}
	stale, err := db.DocumentsNeedingIndex(ctx, space.ID)
	if err != nil || len(stale) != 1 || stale[0].ID != doc.ID || stale[0].CurrentRevision != "rev" {
		t.Fatalf("expected original revision pending repair: %+v %v", stale, err)
	}
	if err := db.ReplaceDocumentIndexDeferred(ctx, space.ID, stale[0], "rev", markdown.Parse(body, "Linki")); err != nil {
		t.Fatal(err)
	}
	for _, term := range []string{"config/links.json", "name", "url"} {
		hits, err := db.Search(ctx, &ports.SearchQuery{SpaceID: space.ID, Text: term})
		if err != nil || len(hits) != 1 || hits[0].DocumentID != doc.ID {
			t.Fatalf("%q: hits=%v err=%v", term, hits, err)
		}
		if !strings.Contains(strings.NewReplacer("<mark>", "", "</mark>", "").Replace(hits[0].Snippet), term) {
			t.Errorf("%q not in snippet: %s", term, hits[0].Snippet)
		}
	}
	if err := db.migrate(); err != nil {
		t.Fatal(err)
	}
	if pending, err := db.DocumentsNeedingIndex(ctx, space.ID); err != nil || len(pending) != 0 {
		t.Fatalf("unexpected repeat repair: %v %v", pending, err)
	}
	if n := countSQL(t, db, `SELECT count(*) FROM revisions WHERE document_id='doc'`); n != 1 {
		t.Errorf("repair changed history: %d revisions", n)
	}
}

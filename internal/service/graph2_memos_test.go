package service

import (
	"context"
	"fmt"
	"github.com/pentaqilo/netsidian/internal/ports"
	"testing"
	"time"
)

func TestMemoTagFiltersAndStablePagination(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	ordinary, err := h.svc.CreateNote(ctx, h.actor, h.space, "Memos/ordinary.md", "ordinary note", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		folder := "Work"
		if i%2 == 0 {
			folder = "Personal"
		}
		if _, err := h.svc.CreateNote(ctx, h.actor, h.space, fmt.Sprintf("%s/%02d.md", folder, i), fmt.Sprintf("---\ntags: [memo, project]\n---\nThought %d with searchable phrase\n", i), "", false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.svc.CreateNote(ctx, h.actor, h.space, "Other/not-a-memo.md", "#memoish unrelated", "", false); err != nil {
		t.Fatal(err)
	}
	first, err := h.svc.Memos(ctx, h.actor, &ports.MemoQuery{SpaceID: h.space, Limit: 7}, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 25 || len(first.Memos) != 7 || first.NextCursor == "" {
		t.Fatalf("bad memo page: %+v", first)
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := h.svc.Memos(ctx, h.actor, &ports.MemoQuery{SpaceID: h.space, Limit: 7}, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range page.Memos {
			if seen[m.ID] || m.ID == ordinary.ID {
				t.Fatal("duplicate or untagged note")
			}
			seen[m.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 25 {
		t.Fatalf("missing memos: %d", len(seen))
	}
	folder := "Work"
	filtered, err := h.svc.Memos(ctx, h.actor, &ports.MemoQuery{SpaceID: h.space, Folder: &folder, Tag: "#project", Text: "searchable phrase", Limit: 50}, "")
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 12 {
		t.Fatalf("folder/tag/content filtering: %+v", filtered)
	}
	if _, err := h.svc.Memos(ctx, h.actor, &ports.MemoQuery{SpaceID: h.space, Folder: &folder, Limit: 7}, first.NextCursor); err == nil {
		t.Fatal("cursor accepted with different filters")
	}
	future, err := h.svc.Memos(ctx, h.actor, &ports.MemoQuery{SpaceID: h.space, From: time.Now().Add(time.Hour)}, "")
	if err != nil || future.Total != 0 {
		t.Fatalf("date filter: %+v %v", future, err)
	}
	if err := h.svc.Trash(ctx, h.actor, h.space, first.Memos[0].ID, first.Memos[0].Revision); err != nil {
		t.Fatal(err)
	}
	after, err := h.svc.Memos(ctx, h.actor, &ports.MemoQuery{SpaceID: h.space}, "")
	if err != nil || after.Total != 24 {
		t.Fatalf("trashed memo remains: %+v %v", after, err)
	}
}
func TestGraph2CompleteLinksWithoutSampling(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for i := 0; i < 1005; i++ {
		if _, err := h.svc.CreateNote(ctx, h.actor, h.space, fmt.Sprintf("Complete-%04d.md", i), "A graph note", "", false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.svc.CreateNote(ctx, h.actor, h.space, "hub.md", "[[Complete-0000]] [[Complete-0000]] [[missing idea]]", "", false); err != nil {
		t.Fatal(err)
	}
	g, err := h.svc.Graph2(ctx, h.actor, h.space)
	if err != nil {
		t.Fatal(err)
	}
	if g.Total != 1006 {
		t.Fatalf("sampled notes: %d", g.Total)
	}
	unresolved := 0
	for _, n := range g.Nodes {
		if n.Kind == "unresolved" {
			unresolved++
		}
	}
	if unresolved != 1 || len(g.Edges) != 2 {
		t.Fatalf("links missing: %+v", g)
	}
	pairs := map[string]bool{}
	for _, e := range g.Edges {
		key := e.Source + "/" + e.Target
		if pairs[key] {
			t.Fatal("duplicate explicit link")
		}
		pairs[key] = true
	}
}

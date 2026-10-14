package service

import (
	"context"
	"testing"
)

func TestDocumentPickerLinksResolveAfterTargetCreationAndRebuild(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source, err := h.svc.CreateNote(ctx, h.actor, h.space, "Projects/Source.md", "[[/Archive/Alpha]] and [[Alpha]]", "", false)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]string{}
	for _, path := range []string{"Projects/Alpha.md", "Inbox/Alpha.md", "Archive/Alpha.md"} {
		doc, err := h.svc.CreateNote(ctx, h.actor, h.space, path, "# Alpha", "", false)
		if err != nil {
			t.Fatal(err)
		}
		targets[path] = doc.ID
	}
	check := func() {
		t.Helper()
		for path, id := range targets {
			links, err := h.svc.Backlinks(ctx, h.actor, h.space, id)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if path == "Inbox/Alpha.md" {
				want = 0
			}
			if len(links) != want {
				t.Fatalf("%s got %d incoming links, want %d", path, len(links), want)
			}
			if want == 1 && links[0].DocumentID != source.ID {
				t.Fatalf("wrong source: %+v", links)
			}
		}
	}
	check()
	h.svc.ReindexAll(ctx)
	check()
}

package service

import (
	"context"
	"testing"
)

func TestMemoryLinksReviewStoredReferenceHasGraphEdge(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	detail, err := h.svc.CreateNote(ctx, h.actor, h.space, "Projects/Overview.md", "# Overview", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "project", Text: "See project", Refs: []string{detail.ID}}, "reference")
	if err != nil {
		t.Fatal(err)
	}
	index, err := h.svc.LookupDocument(ctx, h.actor, h.space, MemoryIndexPath)
	if err != nil {
		t.Fatal(err)
	}
	assertBacklink := func(want bool) {
		t.Helper()
		links, err := h.svc.Backlinks(ctx, h.actor, h.space, detail.ID)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, link := range links {
			if link.DocumentID == index.ID {
				found = true
			}
		}
		if found != want {
			t.Fatalf("index backlink=%v want %v: %v", found, want, links)
		}
	}
	assertBacklink(true)
	if err = h.svc.Rename(ctx, h.actor, h.space, detail.ID, "Projects/Renamed.md", detail.CurrentRevision); err != nil {
		t.Fatal(err)
	}
	assertBacklink(true)
}

func TestMemoryLinksReviewLegacyBareIDIndexHasBacklink(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	detail, err := h.svc.CreateNote(ctx, h.actor, h.space, "agent-memory/details/project.md", "# Project", "", false)
	if err != nil {
		t.Fatal(err)
	}
	source := "- memory: {\"key\":\"project\",\"text\":\"Details\",\"detail\":\"" + detail.ID + "\"}\n"
	index, err := h.svc.CreateNote(ctx, h.actor, h.space, MemoryIndexPath, source, "", false)
	if err != nil {
		t.Fatal(err)
	}
	backlinks, err := h.svc.Backlinks(ctx, h.actor, h.space, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, link := range backlinks {
		if link.DocumentID == index.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("legacy memory reference missing from backlinks")
	}
	note, err := h.svc.ReadNote(ctx, h.actor, h.space, index.ID)
	if err != nil {
		t.Fatal(err)
	}
	if note.Markdown != source {
		t.Fatal("read/indexing rewrote source document")
	}
}

func TestMemoryLinksReviewTextIDsAreNotReferences(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "Mention.md", "# Mention", "", false)
	if err != nil {
		t.Fatal(err)
	}
	source := "- memory: {\"key\":\"literal\",\"text\":\"Do not link " + doc.ID + "\"}\n"
	if _, err = h.svc.CreateNote(ctx, h.actor, h.space, MemoryIndexPath, source, "", false); err != nil {
		t.Fatal(err)
	}
	backlinks, err := h.svc.Backlinks(ctx, h.actor, h.space, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(backlinks) != 0 {
		t.Fatal("IDs in memory text were incorrectly indexed as references")
	}
}

func TestMemoryLinksReviewMissingIDDoesNotRetargetSameNamedFile(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	target, err := h.svc.CreateNote(ctx, h.actor, h.space, "Original.md", "# Original", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "stable", Text: "Original reference", Refs: []string{target.ID}}, "stable-ref"); err != nil {
		t.Fatal(err)
	}
	if err = h.svc.Trash(ctx, h.actor, h.space, target.ID, target.CurrentRevision); err != nil {
		t.Fatal(err)
	}
	other, err := h.svc.CreateNote(ctx, h.actor, h.space, target.ID+".md", "# Different document", "", false)
	if err != nil {
		t.Fatal(err)
	}
	backlinks, err := h.svc.Backlinks(ctx, h.actor, h.space, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(backlinks) != 0 {
		t.Fatal("stable ID reference was redirected to a different same-named file")
	}
}

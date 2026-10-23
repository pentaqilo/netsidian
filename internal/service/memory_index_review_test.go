package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func TestMemoryReviewResponseBudgetAndContinuation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		_, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: fmt.Sprintf("entry-%d", i), Text: strings.Repeat("ą", 200)}, fmt.Sprintf("seed-%d", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	offset := 0
	revision := ""
	queryHash := ""
	seen := map[string]bool{}
	for page := 0; page < 10; page++ {
		result, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{MaxBytes: 1200, Limit: 20, Offset: offset, Revision: revision, QueryHash: queryHash})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(result)
		if len(data) > 1200 {
			t.Fatalf("response exceeds budget: %d > 1200", len(data))
		}
		for _, e := range result.Entries {
			if seen[e.Key] {
				t.Fatalf("duplicate %s", e.Key)
			}
			seen[e.Key] = true
		}
		if !result.HasMore {
			break
		}
		if result.NextOffset <= offset {
			t.Fatal("pagination made no progress")
		}
		offset = result.NextOffset
		revision = result.IndexRevision
		queryHash = result.QueryHash
	}
	if len(seen) != 8 {
		t.Fatalf("only retrieved %d of 8", len(seen))
	}
}

func TestMemoryReviewReadOnlyCannotConfirmRemember(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	in := &MemoryRememberInput{Key: "address", Text: "jasnie panie"}
	if _, err := h.svc.RememberMemory(ctx, h.actor, h.space, in, "seed-address"); err != nil {
		t.Fatal(err)
	}
	reader := *h.actor
	reader.Key = &ports.APIKeyRecord{SpaceIDs: []string{h.space}, Scopes: []string{domain.ScopeDocumentsRead}}
	if _, err := h.svc.RememberMemory(ctx, &reader, h.space, in, "reader-address"); err == nil {
		t.Fatal("read-only token may not claim remember even if entry unchanged")
	}
}

func TestMemoryReviewIndependentWritesWithoutIdempotencyCollision(t *testing.T) {
	h := newHarness(t)
	for _, key := range []string{"first", "second"} {
		if _, err := h.svc.RememberMemory(context.Background(), h.actor, h.space, &MemoryRememberInput{Key: key, Text: key}, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMemoryReviewDetailCannotOverwriteUnrelatedDocument(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "Projects/Important.md", "Keep this project content.", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "project", Text: "Project overview", Detail: "Replacement", DetailPath: doc.Path}, "overwrite-attempt")
	if err == nil {
		t.Fatal("detail must not silently replace an unrelated existing document; link with refs instead")
	}
	note, readErr := h.svc.ReadNote(ctx, h.actor, h.space, doc.ID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if note.Markdown != "Keep this project content." {
		t.Fatal("unrelated document was overwritten")
	}
}

func TestMemoryReviewIndexPreservationReplayAndReferences(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "Projects/Plan.md", "Do not duplicate project body.", "", false)
	if err != nil {
		t.Fatal(err)
	}
	input := &MemoryRememberInput{Key: "projects.website", Text: "Read current plan", Scope: "project", Refs: []string{doc.Path}, Detail: "Long research details."}
	receipt, err := h.svc.RememberMemory(ctx, h.actor, h.space, input, "project-first")
	if err != nil {
		t.Fatal(err)
	}
	index, err := h.svc.LookupDocument(ctx, h.actor, h.space, MemoryIndexPath)
	if err != nil {
		t.Fatal(err)
	}
	note, err := h.svc.ReadNote(ctx, h.actor, h.space, index.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(note.Markdown, "Long research details.") {
		t.Fatal("detail duplicated in index")
	}
	if _, err = h.svc.SaveContent(ctx, h.actor, h.space, index.ID, note.Revision, note.Markdown+"\nHuman annotation preserved.\n", ""); err != nil {
		t.Fatal(err)
	}
	address := "jasnie panie | exact\nwording"
	if _, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "preferences.address", Text: address}, "address-second"); err != nil {
		t.Fatal(err)
	}
	replay, err := h.svc.RememberMemory(ctx, h.actor, h.space, input, "project-first")
	if err != nil {
		t.Fatal(err)
	}
	if replay.IndexRevision != receipt.IndexRevision {
		t.Fatal("replay did not return original receipt")
	}
	changed := *input
	changed.Text = "Different request"
	if _, err = h.svc.RememberMemory(ctx, h.actor, h.space, &changed, "project-first"); err == nil {
		t.Fatal("reused key accepted different payload")
	}
	if err = h.svc.Rename(ctx, h.actor, h.space, doc.ID, "Projects/Renamed.md", doc.CurrentRevision); err != nil {
		t.Fatal(err)
	}
	recalled, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recalled.Entries) != 2 {
		t.Fatalf("lost entries: %v", recalled.Entries)
	}
	for _, entry := range recalled.Entries {
		if entry.Key == "preferences.address" && entry.Text != address {
			t.Fatal("changed verbatim preference")
		}
		if entry.Key == "projects.website" && (len(entry.Refs) != 1 || entry.Refs[0].Path != "Projects/Renamed.md") {
			t.Fatal("stable ref did not follow rename")
		}
	}
	current, err := h.svc.ReadNote(ctx, h.actor, h.space, index.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(current.Markdown, "Human annotation preserved.") {
		t.Fatal("human text lost")
	}
	if _, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "another", Text: "No", ExpectedRevision: receipt.IndexRevision}, "stale-index"); err == nil {
		t.Fatal("stale index edit accepted")
	}
}

func TestMemoryReviewDocumentChunksAreBoundedAndRevisionPinned(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	body := strings.Repeat("ą\\\"\n", 1500)
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "Chunk.md", body, "", false)
	if err != nil {
		t.Fatal(err)
	}
	offset := 0
	revision := ""
	var rebuilt strings.Builder
	for i := 0; i < 100; i++ {
		chunk, err := h.svc.ReadDocumentChunk(ctx, h.actor, h.space, doc.ID, "", revision, offset, 1024)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(chunk)
		if len(data) > 1024 {
			t.Fatalf("chunk too large: %d", len(data))
		}
		rebuilt.WriteString(chunk.Markdown)
		if !chunk.HasMore {
			break
		}
		if chunk.NextOffset <= offset {
			t.Fatal("no progress")
		}
		offset = chunk.NextOffset
		revision = chunk.Revision
	}
	if rebuilt.String() != body {
		t.Fatal("chunking changed original UTF-8 content")
	}
	if _, err = h.svc.SaveContent(ctx, h.actor, h.space, doc.ID, doc.CurrentRevision, "Changed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = h.svc.ReadDocumentChunk(ctx, h.actor, h.space, doc.ID, "", revision, 0, 1024); err == nil {
		t.Fatal("mixed revisions accepted")
	}
}

func TestMemoryReviewMissingReferencesAndCustomFolder(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	doc, err := h.svc.CreateNote(ctx, h.actor, h.space, "Source.md", "Source body", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Folder: "Team memory", Key: "source", Text: "See source", Refs: []string{doc.ID}}, "custom-folder")
	if err != nil {
		t.Fatal(err)
	}
	if err = h.svc.Trash(ctx, h.actor, h.space, doc.ID, doc.CurrentRevision); err != nil {
		t.Fatal(err)
	}
	result, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Folder: "Team memory"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || len(result.Entries[0].Refs) != 1 || result.Entries[0].Refs[0].Available {
		t.Fatal("missing reference not reported", result)
	}
	defaults, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(defaults.Entries) != 0 {
		t.Fatal("custom folder leaked into default index")
	}
}

func TestMemoryReviewBoardsAndMapsUseStableConfigIDs(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.CreateBoard(ctx, h.actor, h.space, "Tasks/Delivery", nil, "Delivery", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.CreateMindMap(ctx, h.actor, h.space, "Maps/Architecture", "Architecture", ""); err != nil {
		t.Fatal(err)
	}
	_, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "project.resources", Text: "Use these live resources", Refs: []string{"Tasks/Delivery", "Maps/Architecture"}}, "resource-links")
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || len(result.Entries[0].Refs) != 2 {
		t.Fatal(result)
	}
	for _, ref := range result.Entries[0].Refs {
		if !ref.Available || ref.DocumentID == "" {
			t.Fatal("board/map ref did not resolve stable config document", ref)
		}
	}
}

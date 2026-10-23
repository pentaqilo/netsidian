package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func TestMemoryReviewCursorPinsFiltersAndEvidenceGeneration(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source, err := h.svc.CreateNote(ctx, h.actor, h.space, "Evidence.md", "Evidence", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: fmt.Sprintf("a%d", i), Text: "Claim", Refs: []string{source.ID}}, fmt.Sprintf("cursor-%d", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Limit: 1, MaxBytes: 4096, Status: "active"})
	if err != nil || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	if _, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Cursor: first.NextCursor, Query: "changed"}); err == nil {
		t.Fatal("cursor accepted changed filter")
	}
	if _, err := h.svc.SaveContent(ctx, h.actor, h.space, source.ID, source.CurrentRevision, "Changed evidence", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Cursor: first.NextCursor}); err == nil {
		t.Fatal("cursor mixed new evidence generation with old index snapshot")
	}
}
func TestMemoryReviewCurrentPermissionsBeforeReceiptReplay(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	input := &MemoryRememberInput{Key: "a", Text: "Private"}
	_, err := h.svc.RememberMemory(ctx, h.actor, h.space, input, "permission-replay")
	if err != nil {
		t.Fatal(err)
	}
	reader := *h.actor
	reader.Key = &ports.APIKeyRecord{SpaceIDs: []string{h.space}, Scopes: []string{domain.ScopeDocumentsRead}}
	receipt, err := h.svc.RememberMemory(ctx, &reader, h.space, input, "permission-replay")
	if err == nil || receipt != nil {
		t.Fatal("read-only replay leaked write confirmation", receipt, err)
	}
	denied := reader
	denied.Key = &ports.APIKeyRecord{SpaceIDs: []string{"other"}, Scopes: []string{domain.ScopeDocumentsRead}}
	recalled, err := h.svc.RecallMemory(ctx, &denied, h.space, &MemoryRecallInput{})
	if err == nil || recalled != nil {
		t.Fatal("revoked binding exposed private memory", recalled, err)
	}
}
func TestMemoryReviewConcurrentIndexUpdatesKeepAllKeys(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			receipt, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: fmt.Sprintf("parallel.%d", i), Text: fmt.Sprintf("Exact claim %d", i), Kind: "fact"}, fmt.Sprintf("parallel-%d", i))
			if err != nil {
				failures <- err
			} else if !receipt.Verified {
				failures <- fmt.Errorf("unverified receipt")
			}
		}(i)
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	recalled, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{})
	if err != nil || len(recalled.Entries) != 8 {
		t.Fatal("concurrent update lost/duplicated keys", recalled, err)
	}
}
func TestMemoryReviewMergedValidationAndExplicitClearingBeforeDetail(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source, err := h.svc.CreateNote(ctx, h.actor, h.space, "Evidence.md", "Evidence", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "a", Text: "Claim", Kind: "fact", Scope: "project", ValidFrom: "2026-01-01T00:00:00Z", ValidTo: "2026-02-01T00:00:00Z", SourceRefs: []MemorySourceRef{{Target: source.ID, Kind: "document", Revision: source.CurrentRevision}}}, "merged-seed")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "a", Text: "Claim", ValidFrom: "2026-03-01T00:00:00Z", Detail: "Must not save"}, "invalid-merged-interval")
	if err == nil {
		t.Fatal("merged invalid interval accepted")
	}
	detail, _ := h.svc.Docs.DocumentByNormKey(ctx, h.space, mustNormKey("agent-memory/details/a.md"))
	if detail != nil {
		t.Fatal("invalid merged metadata wrote detail")
	}
	_, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "a", Text: "Claim", ValidFrom: "2026-03-01T00:00:00Z", SourceRefs: []MemorySourceRef{}, ClearFields: []string{"validTo", "scope"}}, "explicit-clear")
	if err != nil {
		t.Fatal(err)
	}
	recalled, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "a"})
	if err != nil || recalled.Entries[0].ValidTo != "" || recalled.Entries[0].Scope != "" || len(recalled.Entries[0].SourceRefs) != 0 {
		t.Fatal(recalled, err)
	}
}
func TestMemoryReviewTransitiveClaimEvidenceBecomesStale(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source, err := h.svc.CreateNote(ctx, h.actor, h.space, "Evidence.md", "Observed output", "", false)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "observation", Text: "Observed output", Kind: "episode", SourceRefs: []MemorySourceRef{{Target: source.ID, Kind: "document", Revision: source.CurrentRevision}}}, "transitive-seed")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "lesson", Text: "Qualified lesson", Kind: "inference", EvidenceClass: "inferred", DerivedFrom: []MemoryClaimRef{{Key: "observation", IndexRevision: seed.IndexRevision}}}, "transitive-lesson")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "lesson"})
	if err != nil || fresh.Entries[0].DerivedState != "current" {
		t.Fatal(fresh, err)
	}
	_, err = h.svc.SaveContent(ctx, h.actor, h.space, source.ID, source.CurrentRevision, "New observation", "")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "lesson"})
	if err != nil || stale.Entries[0].DerivedState != "stale" {
		t.Fatal("transitive source edit was not propagated", stale, err)
	}
}

func TestMemoryReviewDoesNotOverwriteUnreconciledExternalIndexEdit(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	_, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "a", Text: "Original"}, "external-seed")
	if err != nil {
		t.Fatal(err)
	}
	human := []byte("# Unreconciled human edit\n- memory: {\"key\":\"a\",\"text\":\"Human\"}\n")
	if err := h.svc.Content.WriteAtomic(h.space, MemoryIndexPath, human); err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "b", Text: "New entry"}, "external-guard")
	if err == nil {
		t.Fatal("memory write overwrote storage edits hidden by committed-history reads")
	}
	after, _ := h.svc.Content.Read(h.space, MemoryIndexPath)
	if string(after) != string(human) {
		t.Fatal("external human bytes lost")
	}
}

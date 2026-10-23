package service

import (
	"context"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func TestMemoryReviewStaleStatusIsNotOwnedRetry(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	old, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "fact", Text: "Original"}, "seed-stale")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "other", Text: "Unrelated edit"}, "unrelated")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.ReviseMemory(ctx, h.actor, h.space, &MemoryReviseInput{Key: "fact", Action: "resolve", ExpectedRevision: old.IndexRevision}, "new-unowned-request")
	if err == nil {
		t.Fatal("an active status is not proof that this stale request committed")
	}
}
func TestMemoryReviewRememberReceiptLossRecoversGuardedUpdate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	old, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "fact", Text: "Original", Kind: "fact"}, "seed-remember-loss")
	if err != nil {
		t.Fatal(err)
	}
	input := &MemoryRememberInput{Key: "fact", Text: "Replacement", Kind: "fact", ExpectedRevision: old.IndexRevision}
	h.svc.Docs = &guideJournalFault{DocumentRepo: h.svc.Docs, kind: domain.OperationKind("memory_index"), failInsert: true}
	_, _ = h.svc.RememberMemory(ctx, h.actor, h.space, input, "remember-receipt-loss")
	index, _ := h.svc.Docs.DocumentByNormKey(ctx, h.space, mustNormKey(MemoryIndexPath))
	before := index.CurrentRevision
	h.svc = guideRestart(h.svc)
	receipt, err := h.svc.RememberMemory(ctx, h.actor, h.space, input, "remember-receipt-loss")
	if err != nil || receipt == nil || !receipt.Verified {
		t.Fatalf("guarded retry must reconcile its own child rather than reject original precondition: %+v %v", receipt, err)
	}
	after, _ := h.svc.Docs.Document(ctx, index.ID)
	if after.CurrentRevision != before {
		t.Fatal("retry duplicated a committed index update")
	}
}

func TestMemoryReviewReceiptLossStillRejectsChangedPayload(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	_, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "b", Text: "Untouched"}, "seed-b")
	if err != nil {
		t.Fatal(err)
	}
	seed, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "a", Text: "Original"}, "seed-a")
	if err != nil {
		t.Fatal(err)
	}
	h.svc.Docs = &guideJournalFault{DocumentRepo: h.svc.Docs, kind: domain.OperationKind("memory_index"), failInsert: true}
	original := &MemoryRememberInput{Key: "a", Text: "Replacement", ExpectedRevision: seed.IndexRevision}
	_, _ = h.svc.RememberMemory(ctx, h.actor, h.space, original, "remember-lost-payload")
	changed := &MemoryRememberInput{Key: "b", Text: "Never saved", ExpectedRevision: seed.IndexRevision}
	receipt, err := h.svc.RememberMemory(ctx, h.actor, h.space, changed, "remember-lost-payload")
	if err == nil {
		t.Fatal("lost receipt must not accept a different key/text under the old identity", receipt)
	}
}
func TestMemoryReviewRevisionLostReceiptBindsClassificationAndReason(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	seed, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "a", Text: "Original", Kind: "fact"}, "reason-seed")
	if err != nil {
		t.Fatal(err)
	}
	original := &MemoryReviseInput{Key: "a", Action: "supersede", Text: "Replacement", Kind: "decision", Reason: "Accepted decision", ExpectedRevision: seed.IndexRevision}
	h.svc.Docs = &guideJournalFault{DocumentRepo: h.svc.Docs, kind: domain.OperationKind("memory_revise"), failInsert: true}
	_, _ = h.svc.ReviseMemory(ctx, h.actor, h.space, original, "revise-lost-payload")
	changed := *original
	changed.Kind = "preference"
	changed.Reason = "Different request"
	receipt, err := h.svc.ReviseMemory(ctx, h.actor, h.space, &changed, "revise-lost-payload")
	if err == nil {
		t.Fatal("lost revision receipt must bind classification and reason", receipt)
	}
}

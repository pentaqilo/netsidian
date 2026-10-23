package service

import (
	"context"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func TestMemoryReviewConsolidationEntryGuardBeforeTargetWrite(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	_, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "a", Text: "Original"}, "guard-seed")
	if err != nil {
		t.Fatal(err)
	}
	text := "Derived"
	_, err = h.svc.ConsolidateMemory(ctx, h.actor, h.space, &MemoryConsolidateInput{OperationID: "missing-index-guard", Targets: []MemoryConsolidateTarget{{Path: "Summary.md", Markdown: &text}}, Entries: []MemoryConsolidateEntry{{Key: "a", Status: "disputed"}}})
	if err == nil {
		t.Fatal("index changes require the plan's expectedRevision")
	}
	doc, _ := h.svc.Docs.DocumentByNormKey(ctx, h.space, mustNormKey("Summary.md"))
	if doc != nil {
		t.Fatal("invalid index plan wrote its target first")
	}
}
func TestMemoryReviewConsolidationRejectsAlreadyStaleEvidence(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source, err := h.svc.CreateNote(ctx, h.actor, h.space, "Evidence.md", "Original", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.SaveContent(ctx, h.actor, h.space, source.ID, source.CurrentRevision, "Changed", "")
	if err != nil {
		t.Fatal(err)
	}
	body := "Summary"
	receipt, err := h.svc.ConsolidateMemory(ctx, h.actor, h.space, &MemoryConsolidateInput{OperationID: "stale-evidence", Sources: []MemoryConsolidateSource{{DocumentID: source.ID, ExpectedRevision: source.CurrentRevision}}, Targets: []MemoryConsolidateTarget{{Path: "Summary.md", Markdown: &body}}})
	if err == nil && receipt != nil && receipt.Verified {
		t.Fatal("stale evidence must not create a verified current summary", receipt)
	}
	doc, _ := h.svc.Docs.DocumentByNormKey(ctx, h.space, mustNormKey("Summary.md"))
	if doc != nil {
		t.Fatal("already-stale plan should fail before writing")
	}
}
func TestMemoryReviewConsolidationFinalReceiptLossIsPartial(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source, err := h.svc.CreateNote(ctx, h.actor, h.space, "Evidence.md", "Original", "", false)
	if err != nil {
		t.Fatal(err)
	}
	body := "Summary"
	plan := &MemoryConsolidateInput{OperationID: "unknown-final-commit", Sources: []MemoryConsolidateSource{{DocumentID: source.ID, ExpectedRevision: source.CurrentRevision}}, Targets: []MemoryConsolidateTarget{{Path: "Summary.md", Markdown: &body}}}
	h.svc.Docs = &guideJournalFault{DocumentRepo: h.svc.Docs, kind: domain.OperationKind("memory_consolidate"), failCommit: true}
	receipt, err := h.svc.ConsolidateMemory(ctx, h.actor, h.space, plan)
	if err != nil || receipt == nil || receipt.Verified || receipt.Outcome != "partial" || receipt.RecoveryRef == "" {
		t.Fatalf("unknown final commit must never be a verified applied receipt: %+v %v", receipt, err)
	}
	status, err := h.svc.MemoryOperationStatus(ctx, h.actor, h.space, plan.OperationID, true, 131072)
	if err != nil || status.Plan == nil {
		t.Fatal("partial recovery plan must remain readable", status, err)
	}
}
func TestMemoryReviewConsolidationPartialKeepsManualIndexEdit(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	seed, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "a", Text: "Original"}, "manual-index-seed")
	if err != nil {
		t.Fatal(err)
	}
	source, err := h.svc.CreateNote(ctx, h.actor, h.space, "Evidence.md", "Evidence", "", false)
	if err != nil {
		t.Fatal(err)
	}
	body := "Summary"
	plan := &MemoryConsolidateInput{OperationID: "manual-index-partial", ExpectedRevision: seed.IndexRevision, Sources: []MemoryConsolidateSource{{DocumentID: source.ID, ExpectedRevision: source.CurrentRevision}}, Targets: []MemoryConsolidateTarget{{Path: "Summary.md", Markdown: &body}}, Entries: []MemoryConsolidateEntry{{Key: "a", Status: "disputed"}}}
	h.svc.Docs = &guideCrashAfterTarget{DocumentRepo: h.svc.Docs, crash: true}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("fault not injected")
			}
		}()
		_, _ = h.svc.ConsolidateMemory(ctx, h.actor, h.space, plan)
	}()
	h.svc = guideRestart(h.svc)
	_, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "a", Text: "Human revision"}, "manual-index-edit")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := h.svc.ConsolidateMemory(ctx, h.actor, h.space, plan)
	if err != nil || receipt == nil || receipt.Verified || receipt.Outcome != "partial" {
		t.Fatal("intervening index edit must block remaining changes", receipt, err)
	}
	current, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "a"})
	if err != nil || current.Entries[0].Text != "Human revision" || current.Entries[0].Status == "disputed" {
		t.Fatal("manual index edit overwritten", current, err)
	}
}
func TestMemoryReviewConsolidationRejectsSelfDependency(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source, err := h.svc.CreateNote(ctx, h.actor, h.space, "Evidence.md", "Human evidence", "", false)
	if err != nil {
		t.Fatal(err)
	}
	body := "Self derived"
	_, err = h.svc.ConsolidateMemory(ctx, h.actor, h.space, &MemoryConsolidateInput{OperationID: "self-dependency", Sources: []MemoryConsolidateSource{{DocumentID: source.ID, ExpectedRevision: source.CurrentRevision}}, Targets: []MemoryConsolidateTarget{{DocumentID: source.ID, ExpectedRevision: source.CurrentRevision, Markdown: &body, AdoptManagedSection: true}}})
	if err == nil {
		t.Fatal("derived target may not depend on itself")
	}
	after, _ := h.svc.Docs.Document(ctx, source.ID)
	if after.CurrentRevision != source.CurrentRevision {
		t.Fatal("cycle validation happened after writing")
	}
}
func TestMemoryReviewClaimOnlyDerivedTargetAndRetraction(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	seed, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "a", Text: "User stated preference", Kind: "preference", EvidenceClass: "user-stated"}, "claim-only-seed")
	if err != nil {
		t.Fatal(err)
	}
	body := "Qualified summary of the stated preference."
	plan := &MemoryConsolidateInput{OperationID: "claim-only", ExpectedRevision: seed.IndexRevision, Sources: []MemoryConsolidateSource{{Key: "a", ExpectedRevision: seed.IndexRevision}}, Targets: []MemoryConsolidateTarget{{Path: "Summary.md", Markdown: &body}}}
	receipt, err := h.svc.ConsolidateMemory(ctx, h.actor, h.space, plan)
	if err != nil || !receipt.Verified {
		t.Fatal(receipt, err)
	}
	doc, _ := h.svc.Docs.DocumentByNormKey(ctx, h.space, mustNormKey("Summary.md"))
	status, err := h.svc.DerivedStatusWithInput(ctx, h.actor, h.space, MemoryDerivedInput{DocumentID: doc.ID})
	if err != nil || len(status.Derived) != 1 || status.Derived[0].Status != "current" {
		t.Fatal("claim-only dependency was ignored/invalid", status, err)
	}
	_, err = h.svc.ReviseMemory(ctx, h.actor, h.space, &MemoryReviseInput{Key: "a", Action: "retract", ExpectedRevision: seed.IndexRevision}, "claim-only-retract")
	if err != nil {
		t.Fatal(err)
	}
	status, err = h.svc.DerivedStatusWithInput(ctx, h.actor, h.space, MemoryDerivedInput{DocumentID: doc.ID})
	if err != nil || status.Derived[0].Status != "stale" {
		t.Fatal("retracted evidence must stale derived target", status, err)
	}
}

var _ ports.DocumentRepo = (*guideJournalFault)(nil)

func TestMemoryReviewRestartReconcilesLostCheckpointBeforeRetry(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source, err := h.svc.CreateNote(ctx, h.actor, h.space, "Evidence.md", "Evidence", "", false)
	if err != nil {
		t.Fatal(err)
	}
	body := "Summary"
	plan := &MemoryConsolidateInput{OperationID: "restart-checkpoint", Sources: []MemoryConsolidateSource{{DocumentID: source.ID, ExpectedRevision: source.CurrentRevision}}, Targets: []MemoryConsolidateTarget{{Path: "Summary.md", Markdown: &body}}}
	h.svc.Docs = &guideCrashAfterTarget{DocumentRepo: h.svc.Docs, crash: true}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("fault not injected")
			}
		}()
		_, _ = h.svc.ConsolidateMemory(ctx, h.actor, h.space, plan)
	}()
	h.svc = guideRestart(h.svc)
	h.svc.ReconcileOperations(ctx)
	status, err := h.svc.MemoryOperationStatus(ctx, h.actor, h.space, plan.OperationID, false, 8192)
	if err != nil || len(status.Steps) != 1 || status.Steps[0].Status != "applied" || status.Phase != "pending" {
		t.Fatal("restart must reconcile the committed child without applying/finalizing new work", status, err)
	}
}

package service

// Independent R3 regression from review/cr2/CP-A-independent/r3-validation/,
// copied verbatim (REVIEW-INDEPENDENT-CP-A-R3.md R3-1): an ambiguous
// folder_rename record with different bytes at BOTH origin and target must
// remain actionable/blocked, never silently certified as committed.

import (
	"context"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func TestIndependentR3AmbiguousRenameRemainsBlocked(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d, e := h.svc.CreateNote(ctx, h.actor, h.space, "After/a.md", "target bytes", "", false)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.svc.Content.WriteAtomic(h.space, "Before/a.md", []byte("different origin bytes")); e != nil {
		t.Fatal(e)
	}
	op := &domain.Operation{ID: domain.NewID("op"), SpaceID: h.space, DocumentID: d.ID, Principal: h.actor.UserID,
		Kind: domain.OpKindFolderRename, Phase: domain.OpPhasePending,
		OriginPath: "Before/a.md", TargetPath: "After/a.md", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if e = h.svc.Docs.InsertOperation(ctx, op); e != nil {
		t.Fatal(e)
	}
	resolved, blocked := h.svc.ReconcileOperations(ctx)
	if len(blocked) != 1 {
		t.Fatalf("ambiguous origin+target bytes silently resolved: resolved=%d blocked=%d", resolved, len(blocked))
	}
}

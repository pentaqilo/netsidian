package service

import (
	"context"
	"encoding/json"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// Startup reconciles committed child proofs into logical checkpoints. It never
// applies a new knowledge step or finalizes a plan without an authorized retry.
func (s *Services) reconcileMemoryConsolidation(ctx context.Context, op *domain.Operation) (bool, bool) {
	var journal consolidateJournal
	if json.Unmarshal([]byte(op.Error), &journal) != nil || journal.Version != consolidateJournalVersion || journal.Plan == nil {
		return false, true
	}
	actor := &Actor{UserID: op.Principal}
	if _, err := s.Require(ctx, actor, op.SpaceID, domain.ActionRead); err != nil {
		return false, true
	}
	changed := false
	for i := range journal.Steps {
		before := journal.Steps[i]
		_, err := s.proveConsolidationStep(ctx, actor, op.SpaceID, &journal, i)
		if err != nil {
			journal.Steps[i].Error = err.Error()
			journal.Steps[i].Status = "blocked"
		}
		if before != journal.Steps[i] {
			changed = true
		}
	}
	if changed {
		data, _ := json.Marshal(journal)
		if err := s.Docs.SetOperationPhase(ctx, op.ID, domain.OpPhasePending, string(data)); err != nil {
			return false, true
		}
	}
	return false, true
}

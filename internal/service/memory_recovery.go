package service

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/pentaqilo/netsidian/internal/domain"
)

type MemoryOperationStatus struct {
	OperationID string                      `json:"operationId"`
	Phase       string                      `json:"phase"`
	RecoveryRef string                      `json:"recoveryRef"`
	Steps       []ConsolidationStep         `json:"steps"`
	Receipt     *MemoryConsolidationReceipt `json:"receipt,omitempty"`
	Plan        *MemoryConsolidateInput     `json:"plan,omitempty"`
}

func memoryRecoveryRef(space, operation string) string {
	return "/api/v1/spaces/" + url.PathEscape(space) + "/agent/memory/operation?operationId=" + url.QueryEscape(operation)
}

// Read-only recovery lookup. It never applies pending steps or rewrites journals.
// Lookup is principal-bound and authorization is rechecked on every invocation.
func (s *Services) MemoryOperationStatus(ctx context.Context, a *Actor, space, operation string, includePlan bool, maxBytes int) (*MemoryOperationStatus, error) {
	if _, err := s.Require(ctx, a, space, domain.ActionRead); err != nil {
		return nil, err
	}
	if operation == "" || len(operation) > 256 {
		return nil, domain.Invalid("operationId is required, at most 256 bytes")
	}
	if maxBytes == 0 {
		maxBytes = 8192
	}
	if maxBytes < 1024 || maxBytes > 131072 {
		return nil, domain.Invalid("maxBytes must be 1024..131072")
	}
	op, err := s.Docs.FindOperationByIdempotency(ctx, space, a.UserID, consolidateOpKey(operation))
	if err != nil {
		return nil, err
	}
	if op == nil || op.Kind != domain.OperationKind("memory_consolidate") {
		return nil, domain.NotFound("memory operation not found")
	}
	out := &MemoryOperationStatus{OperationID: operation, Phase: string(op.Phase), RecoveryRef: memoryRecoveryRef(space, operation), Steps: []ConsolidationStep{}}
	var journal consolidateJournal
	if json.Unmarshal([]byte(op.Error), &journal) == nil && journal.Version == consolidateJournalVersion && journal.Plan != nil {
		out.Steps = journal.Steps
		out.Receipt = journal.Receipt
		if includePlan {
			out.Plan = journal.Plan
		}
	} else if op.Phase == domain.OpPhaseCommitted {
		var receipt MemoryConsolidationReceipt
		if json.Unmarshal([]byte(op.Error), &receipt) != nil || receipt.OperationID != operation {
			return nil, domain.Unavailable("memory recovery receipt is unreadable")
		}
		out.Receipt = &receipt
		out.Steps = receipt.Steps
	} else {
		return nil, domain.Unavailable("memory recovery journal is unreadable; do not retry with a new identity")
	}
	data, _ := json.Marshal(out)
	if len(data) > maxBytes {
		return nil, domain.TooLarge("recovery status exceeds maxBytes; omit includePlan or raise the bounded budget")
	}
	return out, nil
}

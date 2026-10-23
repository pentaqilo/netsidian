package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// A small durable request binding survives loss of the final verified receipt.
// It contains no second copy of knowledge; ordinary child document journals own
// the actual persisted bytes. Changed payloads cannot steal an old child step.
func (s *Services) memoryIntent(ctx context.Context, a *Actor, space, idem, kind, hash string, create bool) (bool, error) {
	if idem == "" {
		return false, nil
	}
	key := memoryStepKey(idem, "intent")
	op, err := s.Docs.FindOperationByIdempotency(ctx, space, a.UserID, key)
	if err != nil {
		return false, err
	}
	if op != nil {
		if op.Kind != domain.OperationKind("memory_intent") || op.RequestHash != kind+":"+hash || op.Phase != domain.OpPhaseCommitted {
			return false, domain.Conflict("idempotency key was reused with a different memory request")
		}
		return true, nil
	}
	if !create {
		return false, nil
	}
	now := time.Now()
	err = s.Docs.InsertOperation(ctx, &domain.Operation{ID: domain.NewID("op"), SpaceID: space, Principal: a.UserID, Kind: domain.OperationKind("memory_intent"), Phase: domain.OpPhaseCommitted, IdempotencyKey: key, RequestHash: kind + ":" + hash, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return false, domain.Unavailable("memory request binding could not be recorded; no knowledge writes attempted")
	}
	return true, nil
}
func (s *Services) memoryOwnedRevision(ctx context.Context, a *Actor, space, idem, docID, expected, body string) (string, error) {
	if idem == "" {
		return "", nil
	}
	op, err := s.Docs.FindOperationByIdempotency(ctx, space, a.UserID, memoryStepKey(idem, "index"))
	if err != nil {
		return "", err
	}
	if op == nil {
		return "", nil
	}
	if op.Phase != domain.OpPhaseCommitted || op.DocumentID != docID || op.NewSHA256 != shaHex([]byte(body)) {
		return "", domain.Conflict("memory child completion is unknown or its saved bytes changed")
	}
	if op.Kind == domain.OpKindContent && expected != "" && op.ExpectedRevision != expected {
		return "", domain.Conflict("memory child belongs to a different revision guard")
	}
	if op.Kind != domain.OpKindContent && op.Kind != domain.OpKindCreate {
		return "", domain.Conflict("unexpected memory child operation kind")
	}
	revisions, err := s.Docs.RevisionsForOperation(ctx, op.ID)
	if err != nil {
		return "", err
	}
	for _, revision := range revisions {
		if revision.DocumentID == docID && revision.SHA256 == op.NewSHA256 {
			return revision.ID, nil
		}
	}
	return "", domain.Unavailable("memory child revision is not reconciled; retry the original request after recovery")
}
func (s *Services) saveMemoryReceipt(ctx context.Context, a *Actor, space, idem, kind, hash string, receipt any) error {
	if idem == "" {
		return nil
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	now := time.Now()
	if err := s.Docs.InsertOperation(ctx, &domain.Operation{ID: domain.NewID("op"), SpaceID: space, Principal: a.UserID, Kind: domain.OperationKind(kind), Phase: domain.OpPhaseCommitted, RequestHash: hash, IdempotencyKey: idem, Error: string(data), CreatedAt: now, UpdatedAt: now}); err != nil {
		return domain.Unavailable("memory bytes committed but final receipt could not be recorded; retry with the same key and payload")
	}
	return nil
}

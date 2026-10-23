package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// Guarded correction and retraction for indexed memory. A correction
// updates the stable topic key in place and pins an immutable
// reference to the previous claim revision in `supersedes`; the prior
// claim and its evidence stay addressable through the index document's
// normal history. Retraction marks a claim out of active use while its
// audit history remains; it is not deletion and never purges history.
// A genuine unresolved contradiction is marked disputed, and both
// sides stay recallable - the newest statement is not automatically true.

// MemoryReviseInput is one guarded revision of an existing entry.
// ExpectedRevision is mandatory: revisions are the safety precondition
// for correction operations, unlike legacy remember compatibility.
type MemoryReviseInput struct {
	Folder           string `json:"folder,omitempty"`
	Key              string `json:"key"`
	Action           string `json:"action"` // supersede|retract|dispute|resolve
	ExpectedRevision string `json:"expectedRevision"`
	Text             string `json:"text,omitempty"` // replacement claim for supersede
	Kind             string `json:"kind,omitempty"`
	EvidenceClass    string `json:"evidenceClass,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

// MemoryRevisionReceipt confirms one committed revision.
type MemoryRevisionReceipt struct {
	Key                string           `json:"key"`
	Action             string           `json:"action"`
	Status             string           `json:"status"`
	IndexPath          string           `json:"indexPath"`
	IndexRevision      string           `json:"indexRevision"`
	SupersededRevision string           `json:"supersededRevision,omitempty"`
	Supersedes         []MemoryClaimRef `json:"supersedes,omitempty"`
	Verified           bool             `json:"verified"`
}

var memoryReviseActions = map[string]bool{"supersede": true, "retract": true, "dispute": true, "resolve": true}

// revisionAlreadyApplied reports whether the persisted index already
// holds the exact post-state of this logical request, which is the
// signature of a committed write whose receipt was lost.
func revisionAlreadyApplied(entries []*memoryLine, in *MemoryReviseInput) *memoryLine {
	for _, l := range entries {
		if l.key != in.Key {
			continue
		}
		switch in.Action {
		case "supersede":
			if l.text == in.Text && l.status == "active" {
				for _, pin := range l.supersedes {
					if pin.Key == in.Key && pin.IndexRevision == in.ExpectedRevision {
						return l
					}
				}
			}
		case "retract":
			if l.status == "retracted" {
				return l
			}
		case "dispute":
			if l.status == "disputed" {
				return l
			}
		case "resolve":
			if l.status == "active" {
				return l
			}
		}
		return nil
	}
	return nil
}

// ReviseMemory applies a guarded status/claim change to one entry.
func (s *Services) ReviseMemory(ctx context.Context, a *Actor, spaceID string, in *MemoryReviseInput, idemKey string) (*MemoryRevisionReceipt, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	if _, err := s.Require(ctx, a, spaceID, domain.ActionEdit); err != nil {
		return nil, err
	}
	if in == nil || !memoryReviseActions[in.Action] {
		return nil, domain.Invalid("action must be supersede, retract, dispute or resolve")
	}
	if in.ExpectedRevision == "" {
		return nil, domain.PreconditionRequired("expectedRevision from recall is required for memory revisions")
	}
	if !memoryKeyRe.MatchString(in.Key) {
		return nil, domain.Invalid("key must match ^[a-z0-9][a-z0-9._-]{0,95}$")
	}
	if in.Action == "supersede" && in.Text == "" {
		return nil, domain.Invalid("supersede requires the replacement text")
	}
	if in.Text != "" && in.Action != "supersede" {
		return nil, domain.Invalid("text may change only when superseding a claim")
	}
	if in.Kind != "" && !memoryKinds[in.Kind] {
		return nil, domain.Invalid("kind must be one of fact, preference, decision, episode, procedure or inference")
	}
	if in.EvidenceClass != "" && !memoryEvidenceClasses[in.EvidenceClass] {
		return nil, domain.Invalid("evidenceClass must be one of user-stated, observed, sourced or inferred")
	}
	if err := validateMemoryField("reason", in.Reason, 500); err != nil {
		return nil, err
	}
	if in.Action == "supersede" {
		if err := validateMemoryField("text", in.Text, 500); err != nil {
			return nil, err
		}
	}
	if err := validateMemoryField("idempotencyKey", idemKey, 1024); err != nil {
		return nil, err
	}
	folder, err := memoryFolder(in.Folder)
	if err != nil {
		return nil, err
	}
	indexPath := folder + "/index.md"
	lock := s.memoryMu(spaceID + ":" + folder)
	lock.Lock()
	defer lock.Unlock()

	request, _ := json.Marshal(in)
	requestHash := shaHex(request)
	hasIntent, intentErr := s.memoryIntent(ctx, a, spaceID, idemKey, "memory_revise", requestHash, false)
	if intentErr != nil {
		return nil, intentErr
	}

	if idemKey != "" {
		op, err := s.Docs.FindOperationByIdempotency(ctx, spaceID, a.UserID, idemKey)
		if err != nil {
			return nil, err
		}
		if op != nil {
			if op.Kind != domain.OperationKind("memory_revise") || op.RequestHash != requestHash {
				return nil, domain.Conflict("idempotency key was reused with a different request")
			}
			var receipt MemoryRevisionReceipt
			if json.Unmarshal([]byte(op.Error), &receipt) != nil {
				return nil, domain.Conflict("incomplete revision receipt; inspect the index before retrying")
			}
			// Receipt replay rechecks current authorization above
			// before revealing the old result.
			return &receipt, nil
		}
	}

	index, err := s.Docs.DocumentByNormKey(ctx, spaceID, mustNormKey(indexPath))
	if err != nil {
		return nil, err
	}
	if index == nil || index.TombstoneAt != nil {
		return nil, domain.Conflict("memory index is unavailable; nothing to revise")
	}
	note, err := s.ReadNote(ctx, a, spaceID, index.ID)
	if err != nil {
		return nil, err
	}
	if note.Revision != in.ExpectedRevision {
		// The revision may have committed while its receipt was lost
		// (crash or journal fault). Recover only with proof that the
		// committed write is this request's own child operation under
		// the deterministic step key; a matching persisted state alone
		// is never treated as an owned retry.
		if recovered := revisionAlreadyApplied(parseMemoryIndex(note.Markdown), in); recovered != nil && hasIntent {
			produced, err := s.memoryOwnedRevision(ctx, a, spaceID, idemKey, index.ID, in.ExpectedRevision, note.Markdown)
			if err != nil {
				return nil, err
			}
			if produced == "" || produced != note.Revision {
				return nil, domain.Conflict("revision child is not the current committed request")
			}
			receipt := &MemoryRevisionReceipt{Key: in.Key, Action: in.Action, Status: recovered.status, IndexPath: indexPath, IndexRevision: note.Revision, Verified: true}
			if in.Action == "supersede" {
				for _, pin := range recovered.supersedes {
					if pin.Key == in.Key && pin.IndexRevision == in.ExpectedRevision {
						receipt.SupersededRevision, receipt.Supersedes = in.ExpectedRevision, []MemoryClaimRef{pin}
					}
				}
			}
			if idemKey != "" {
				data, _ := json.Marshal(receipt)
				now := time.Now()
				if err := s.Docs.InsertOperation(ctx, &domain.Operation{ID: domain.NewID("op"), SpaceID: spaceID, Principal: a.UserID, Kind: domain.OperationKind("memory_revise"), Phase: domain.OpPhaseCommitted, RequestHash: requestHash, IdempotencyKey: idemKey, Error: string(data), CreatedAt: now, UpdatedAt: now}); err != nil {
					return nil, domain.Unavailable("recovered revision is committed but its receipt could not be journaled; retry with the same key")
				}
			}
			return receipt, nil
		}
		return nil, domain.Conflict("memory index changed; recall and merge before retrying")
	}
	if len(note.Markdown) > 1<<20 {
		return nil, domain.TooLarge("memory index exceeds 1 MiB; split memory into scoped folders")
	}
	if err := memoryIndexGuard(note.Markdown, in.Key); err != nil {
		return nil, err
	}
	var existing *memoryLine
	for _, entry := range parseMemoryIndex(note.Markdown) {
		if entry.key != in.Key {
			continue
		}
		if existing != nil {
			return nil, domain.Conflict("duplicate memory keys in index; resolve them before revising")
		}
		existing = entry
	}
	if existing == nil {
		return nil, domain.NotFound("memory key not found; recall before revising")
	}
	if existing.schemaVersion > MemorySchemaVersion {
		return nil, domain.Unsupported("memory entry uses a newer schema version; this server refuses to rewrite it")
	}

	line := &memoryLine{}
	*line = *existing
	line.refs = append([]string(nil), existing.refs...)
	line.sourceRefs = append([]MemorySourceRef(nil), existing.sourceRefs...)
	line.supersedes = append([]MemoryClaimRef(nil), existing.supersedes...)
	line.derivedFrom = append([]MemoryClaimRef(nil), existing.derivedFrom...)
	line.extra = map[string]json.RawMessage{}
	for k, v := range existing.extra {
		line.extra[k] = v
	}

	receipt := &MemoryRevisionReceipt{Key: in.Key, Action: in.Action, IndexPath: indexPath}
	switch in.Action {
	case "supersede":
		// The previous claim stays addressable at the index revision
		// that still contains it, even though the key now holds the
		// replacement.
		pin := MemoryClaimRef{VaultID: spaceID, IndexDocumentID: index.ID, Key: in.Key, IndexRevision: note.Revision}
		line.supersedes = append(line.supersedes, pin)
		receipt.SupersededRevision, receipt.Supersedes = note.Revision, []MemoryClaimRef{pin}
		line.text = in.Text
		line.status = "active"
		if in.Kind != "" {
			line.kind = in.Kind
		}
		if in.EvidenceClass != "" {
			line.evidenceClass = in.EvidenceClass
		}
	case "retract":
		line.status = "retracted"
	case "dispute":
		line.status = "disputed"
	case "resolve":
		line.status = "active"
	}
	receipt.Status = line.status
	if in.Reason != "" {
		line.reason = in.Reason
	}
	// Revising converts the entry to the versioned representation so
	// provenance and temporal semantics become explicit.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if line.render() != existing.render() {
		if line.recordedAt == "" || (in.Action == "supersede" && line.text != existing.text) {
			line.recordedAt = now
		}
		line.updatedAt = now
	}
	if err := validateMergedMemory(line); err != nil {
		return nil, err
	}

	merged, changed := mergeMemoryIndex(note.Markdown, line)
	if !changed {
		receipt.IndexRevision, receipt.Verified = note.Revision, true
	} else {
		if _, err := s.memoryIntent(ctx, a, spaceID, idemKey, "memory_revise", requestHash, true); err != nil {
			return nil, err
		}
		saved, err := s.SaveContent(ctx, a, spaceID, index.ID, note.Revision, merged, memoryStepKey(idemKey, "index"), shaHex([]byte(note.Markdown)))
		if err != nil {
			return nil, err
		}
		receipt.IndexRevision = saved.Revision
	}
	verifiedNote, err := s.ReadNote(ctx, a, spaceID, index.ID)
	if err != nil {
		return nil, err
	}
	verified := false
	for _, parsed := range parseMemoryIndex(verifiedNote.Markdown) {
		if parsed.key == in.Key && parsed.status == line.status && parsed.render() == line.render() {
			verified = true
		}
	}
	if !verified {
		return nil, domain.Unavailable("memory revision could not be verified; inspect " + indexPath + " before retrying")
	}
	receipt.Verified = true
	if idemKey != "" {
		data, _ := json.Marshal(receipt)
		now := time.Now()
		op := &domain.Operation{ID: domain.NewID("op"), SpaceID: spaceID, Principal: a.UserID, Kind: domain.OperationKind("memory_revise"), Phase: domain.OpPhaseCommitted, RequestHash: requestHash, IdempotencyKey: idemKey, Error: string(data), CreatedAt: now, UpdatedAt: now}
		if err := s.Docs.InsertOperation(ctx, op); err != nil {
			return nil, domain.Unavailable("revised memory but receipt could not be recorded; retry with the same key")
		}
	}
	return receipt, nil
}

// reviseChildOwned proves that the committed index write is this
// request's own child: the deterministic step key derived from the
// idempotency key must name a committed content operation on this very
// index document. Without that proof a matching persisted state is
// never treated as an owned retry.

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

const maxDerivedNodes = 256
const maxDerivedDepth = 8
const maxDerivedSources = 16

type MemoryDerivedInput struct {
	Cursor     string `json:"cursor,omitempty"`
	DocumentID string `json:"documentId,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	MaxBytes   int    `json:"maxBytes,omitempty"`
}
type MemoryDerivedPage struct {
	Scanned    int                     `json:"scanned"`
	Derived    []DerivedDocumentStatus `json:"derived"`
	Truncated  bool                    `json:"truncated,omitempty"`
	NextCursor string                  `json:"nextCursor,omitempty"`
	Generation int64                   `json:"generation"`
}
type derivedCursor struct {
	Space      string `json:"s"`
	Generation int64  `json:"g"`
	After      string `json:"a"`
	Limit      int    `json:"l"`
	MaxBytes   int    `json:"b"`
}
type derivedInspector struct {
	svc      *Services
	ctx      context.Context
	actor    *Actor
	space    string
	reader   ports.DerivedIndexReader
	cache    map[string]*DerivedDocumentStatus
	visiting map[string]bool
	seen     map[string]bool
}

func (s *Services) newDerivedInspector(ctx context.Context, a *Actor, space string) (*derivedInspector, error) {
	reader, ok := s.Index.(ports.DerivedIndexReader)
	if !ok {
		return nil, domain.Unsupported("bounded derived metadata index is unavailable for this repository")
	}
	return &derivedInspector{svc: s, ctx: ctx, actor: a, space: space, reader: reader, cache: map[string]*DerivedDocumentStatus{}, visiting: map[string]bool{}, seen: map[string]bool{}}, nil
}
func (i *derivedInspector) inspect(id string, depth int, chain []string) (*DerivedDocumentStatus, error) {
	if i.visiting[id] {
		return &DerivedDocumentStatus{DocumentID: id, Status: "cyclic", Cycle: append(append([]string{}, chain...), id)}, nil
	}
	if cached, ok := i.cache[id]; ok {
		return cached, nil
	}
	if depth > maxDerivedDepth {
		return nil, domain.TooLarge("derived dependency depth exceeds 8; reduce the dependency chain")
	}
	i.seen[id] = true
	if len(i.seen) > maxDerivedNodes {
		return nil, domain.TooLarge("derived dependency inspection exceeds 256 nodes; use a smaller page or inspect one document")
	}
	row, err := i.reader.DerivedMetadata(i.ctx, i.space, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		i.cache[id] = nil
		return nil, nil
	}
	namespace, _ := row.Frontmatter["netsidian"].(map[string]any)
	marker, marked := namespace["derived"].(map[string]any)
	if !marked {
		i.cache[id] = nil
		return nil, nil
	}
	out := &DerivedDocumentStatus{DocumentID: id, Path: row.Path, Revision: row.Revision, Sources: []DerivedSourceStatus{}}
	if version, present := marker["schemaVersion"]; present {
		number, ok := version.(int)
		if !ok {
			if numeric, valid := version.(float64); valid && numeric == 1 {
				number = 1
				ok = true
			}
		}
		if !ok || number != 1 {
			out.Status = "invalid"
			i.cache[id] = out
			return out, nil
		}
	}

	if row.IndexedRevision != row.Revision {
		out.Status = "unindexed"
		i.cache[id] = out
		return out, nil
	}
	rawSources, ok := marker["sources"].([]any)
	rawClaims, claimsOK := marker["claims"].([]any)
	if (!ok && marker["sources"] != nil) || (!claimsOK && marker["claims"] != nil) || len(rawSources)+len(rawClaims) == 0 || len(rawSources)+len(rawClaims) > maxDerivedSources {
		out.Status = "invalid"
		i.cache[id] = out
		return out, nil
	}
	if operationID, ok := marker["operationId"].(string); ok && operationID != "" {
		operation, err := i.svc.Docs.Operation(i.ctx, operationID)
		if err != nil {
			return nil, err
		}
		if operation == nil || operation.SpaceID != i.space || operation.Kind != domain.OperationKind("memory_consolidate") || operation.Phase != domain.OpPhaseCommitted {
			out.Status = "provisional"
		}
	}
	if state, _ := marker["state"].(string); state == "provisional" {
		out.Status = "provisional"
	}
	i.visiting[id] = true
	defer delete(i.visiting, id)
	// Claim dependencies (durable index claims pinned by the
	// consolidation that produced this document) participate in
	// staleness exactly like pinned document sources.
	refs := derivedClaimRefs(marker)
	if len(refs) != len(rawClaims) {
		out.Status = "invalid"
	}
	if len(refs) > 0 {
		switch i.svc.memoryClaimsState(i.ctx, i.actor, i.space, refs) {
		case "broken":
			if out.Status != "cyclic" && out.Status != "invalid" && out.Status != "provisional" {
				out.Status = "broken"
			}
		case "cyclic":
			out.Status = "cyclic"
		case "stale", "unavailable":
			if out.Status == "" {
				out.Status = "stale"
			}
		}
	}
	for _, raw := range rawSources {
		source, ok := raw.(map[string]any)
		if !ok {
			out.Status = "invalid"
			continue
		}
		sourceID, _ := source["documentId"].(string)
		revision, _ := source["revision"].(string)
		if sourceID == "" || revision == "" || len(sourceID) > 128 || len(revision) > 128 {
			out.Status = "invalid"
			continue
		}
		status := DerivedSourceStatus{DocumentID: sourceID, PinnedRevision: revision}
		document, err := i.svc.Docs.Document(i.ctx, sourceID)
		if err != nil {
			return nil, err
		}
		if document == nil || document.SpaceID != i.space || document.TombstoneAt != nil {
			if out.Status != "cyclic" && out.Status != "invalid" {
				out.Status = "broken"
			}
			out.Sources = append(out.Sources, status)
			continue
		}
		status.Available = true
		status.Path = document.Path
		status.CurrentRevision = document.CurrentRevision
		status.Stale = revision != document.CurrentRevision
		nested, err := i.inspect(sourceID, depth+1, append(chain, id))
		if err != nil {
			return nil, err
		}
		if nested != nil && nested.Status != "current" {
			status.Stale = true
			switch nested.Status {
			case "cyclic":
				out.Status = "cyclic"
				out.Cycle = nested.Cycle
			case "broken":
				if out.Status != "cyclic" && out.Status != "invalid" {
					out.Status = "broken"
				}
			default:
				if out.Status == "" {
					out.Status = "stale"
				}
			}
		}
		if status.Stale && out.Status == "" {
			out.Status = "stale"
		}
		out.Sources = append(out.Sources, status)
	}
	if out.Status == "" {
		out.Status = "current"
	}
	i.cache[id] = out
	return out, nil
}

// derivedClaimRefs decodes the claim pins stored in a derived marker.
// YAML encoding lowercases struct field names, so both spellings are
// accepted to stay lossless across encoders.
func derivedClaimRefs(marker map[string]any) []MemoryClaimRef {
	raw, ok := marker["claims"].([]any)
	if !ok {
		return nil
	}
	str := func(m map[string]any, names ...string) string {
		for _, n := range names {
			if v, ok := m[n].(string); ok {
				return v
			}
		}
		return ""
	}
	refs := []MemoryClaimRef{}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ref := MemoryClaimRef{
			VaultID:         str(m, "vaultId", "vaultid"),
			IndexDocumentID: str(m, "indexDocumentId", "indexdocumentid"),
			Key:             str(m, "key"),
			IndexRevision:   str(m, "indexRevision", "indexrevision"),
			Fingerprint:     str(m, "fingerprint"),
			ExpectedStatus:  str(m, "expectedStatus", "expectedstatus"),
		}
		if ref.Key != "" && ref.IndexDocumentID != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

func derivedPageCursor(space string, generation int64, after string, limit, budget int) string {
	raw, _ := json.Marshal(derivedCursor{Space: space, Generation: generation, After: after, Limit: limit, MaxBytes: budget})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DerivedStatusWithInput uses only bounded, indexed candidate pages and exact
// source IDs. Ordinary edits/imports already publish the frontmatter projection.
func (s *Services) DerivedStatusWithInput(ctx context.Context, a *Actor, space string, in MemoryDerivedInput) (*MemoryDerivedPage, error) {
	if _, err := s.Require(ctx, a, space, domain.ActionRead); err != nil {
		return nil, err
	}
	ctx = memoryClaimContext(ctx, s, a, space)
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.MaxBytes == 0 {
		in.MaxBytes = 8192
	}
	if in.Limit < 1 || in.Limit > 50 || in.MaxBytes < 1024 || in.MaxBytes > 32768 {
		return nil, domain.Invalid("derived limit must be 1..50 and maxBytes 1024..32768")
	}
	generation, err := s.Docs.MaxEventSeq(ctx, space)
	if err != nil {
		return nil, err
	}
	after := ""
	if in.Cursor != "" {
		if len(in.Cursor) > 1024 || in.DocumentID != "" {
			return nil, domain.Invalid("invalid derived continuation")
		}
		data, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		if err != nil {
			return nil, domain.Invalid("invalid derived continuation")
		}
		var cursor derivedCursor
		if json.Unmarshal(data, &cursor) != nil || cursor.Space != space || cursor.Limit != in.Limit || cursor.MaxBytes != in.MaxBytes {
			return nil, domain.Invalid("derived continuation belongs to a different request")
		}
		if cursor.Generation != generation {
			return nil, domain.Conflict("vault changed; restart derived status retrieval")
		}
		after = cursor.After
	}
	inspector, err := s.newDerivedInspector(ctx, a, space)
	if err != nil {
		return nil, err
	}
	var candidates []ports.DerivedMetadata
	more := false
	if in.DocumentID != "" {
		row, err := inspector.reader.DerivedMetadata(ctx, space, in.DocumentID)
		if err != nil {
			return nil, err
		}
		if row != nil {
			candidates = append(candidates, *row)
		}
	} else {
		candidates, more, err = inspector.reader.DerivedMetadataPage(ctx, space, after, in.Limit)
		if err != nil {
			return nil, err
		}
	}
	out := &MemoryDerivedPage{Derived: []DerivedDocumentStatus{}, Generation: generation}
	for pos, candidate := range candidates {
		status, err := inspector.inspect(candidate.DocumentID, 0, nil)
		if err != nil {
			return nil, err
		}
		out.Scanned++
		if status == nil {
			continue
		}
		out.Derived = append(out.Derived, *status)
		hasMore := more || pos+1 < len(candidates)
		out.Truncated = hasMore
		if hasMore {
			out.NextCursor = derivedPageCursor(space, generation, candidate.DocumentID, in.Limit, in.MaxBytes)
		} else {
			out.NextCursor = ""
		}
		encoded, _ := json.Marshal(out)
		if len(encoded) > in.MaxBytes {
			out.Derived = out.Derived[:len(out.Derived)-1]
			if len(out.Derived) == 0 {
				return nil, domain.TooLarge("one derived status entry exceeds maxBytes; increase the budget")
			}
			out.Truncated = true
			out.NextCursor = derivedPageCursor(space, generation, after, in.Limit, in.MaxBytes)
			break
		}
		after = candidate.DocumentID
	}
	latest, err := s.Docs.MaxEventSeq(ctx, space)
	if err != nil {
		return nil, err
	}
	if latest != generation {
		return nil, domain.Conflict("vault changed during derived inspection; restart retrieval")
	}
	encoded, _ := json.Marshal(out)
	if len(encoded) > in.MaxBytes {
		return nil, domain.TooLarge("derived status metadata exceeds maxBytes")
	}
	return out, nil
}

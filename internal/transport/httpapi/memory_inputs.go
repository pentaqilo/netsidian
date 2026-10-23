package httpapi

import "github.com/pentaqilo/netsidian/internal/service"

// Evidence requests deliberately exclude server-computed availability,
// verification and path fields. Strict REST/MCP decoders reject those inputs.
type memorySourceInput struct {
	Target     string `json:"target" jsonschema:"Stable document ID/path, external URL or honest agent-supplied interaction locator"`
	Kind       string `json:"kind,omitempty" jsonschema:"document, external or interaction"`
	Revision   string `json:"revision,omitempty" jsonschema:"Pinned document revision; current revision is resolved when omitted"`
	Locator    string `json:"locator,omitempty" jsonschema:"Optional section or source locator, at most 1024 characters"`
	Quote      string `json:"quote,omitempty" jsonschema:"Optional exact quote, at most 500 characters"`
	ObservedAt string `json:"observedAt,omitempty" jsonschema:"Optional actual source time in RFC3339; omit unknown time"`
}

func sourceInputs(in []memorySourceInput) []service.MemorySourceRef {
	if in == nil {
		return nil
	}
	out := make([]service.MemorySourceRef, 0, len(in))
	for _, source := range in {
		out = append(out, service.MemorySourceRef{Target: source.Target, Kind: source.Kind, Revision: source.Revision, Locator: source.Locator, Quote: source.Quote, ObservedAt: source.ObservedAt})
	}
	return out
}

type memoryClaimInput struct {
	VaultID         string `json:"vaultId,omitempty"`
	IndexDocumentID string `json:"indexDocumentId,omitempty"`
	Key             string `json:"key"`
	IndexRevision   string `json:"indexRevision,omitempty"`
}

func claimInputs(in []memoryClaimInput) []service.MemoryClaimRef {
	if in == nil {
		return nil
	}
	out := make([]service.MemoryClaimRef, 0, len(in))
	for _, claim := range in {
		out = append(out, service.MemoryClaimRef{VaultID: claim.VaultID, IndexDocumentID: claim.IndexDocumentID, Key: claim.Key, IndexRevision: claim.IndexRevision})
	}
	return out
}

type memoryRememberRequest struct {
	service.MemoryRememberInput
	SourceRefs  []memorySourceInput `json:"sourceRefs,omitempty"`
	Supersedes  []memoryClaimInput  `json:"supersedes,omitempty"`
	DerivedFrom []memoryClaimInput  `json:"derivedFrom,omitempty"`
}

func (in memoryRememberRequest) input() *service.MemoryRememberInput {
	out := in.MemoryRememberInput
	out.SourceRefs = sourceInputs(in.SourceRefs)
	out.Supersedes = claimInputs(in.Supersedes)
	out.DerivedFrom = claimInputs(in.DerivedFrom)
	return &out
}

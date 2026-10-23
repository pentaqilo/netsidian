package httpapi

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
)

// Typed workflow tools over the existing service layer. They translate
// inputs and compact outputs only: authorization, revision guards,
// idempotency and storage rules stay in the shared services, identical
// to the REST routes the generic adapters use.

func mcpToolError(err error) *mcp.CallToolResult {
	out := map[string]any{"error": err.Error()}
	if de, ok := err.(*domain.Error); ok {
		out["code"] = string(de.Code)
		switch de.Code {
		case domain.ErrCodePrecondition, domain.ErrCodePreconditionReq:
			out["guidance"] = "Resolve any partial or completion-unknown operation first. For a confirmed uncommitted revision conflict, read and merge current content; a changed payload needs a new logical key."
		case domain.ErrCodeForbidden:
			out["guidance"] = "Check the credential's vault binding and scopes via netsidian_connect; reads never gain writes."
		case domain.ErrCodeConflict:
			out["guidance"] = "Inspect the current state before retrying; keep the original idempotency key for the same logical mutation."
		}
	}
	return mcpText(out, true)
}

func (s *Server) mcpActor(ctx context.Context) (*service.Actor, error) {
	actor := desktopMCPActor(ctx)
	if actor == nil {
		actor, _ = ctx.Value(actorKey).(*service.Actor)
	}
	if actor == nil {
		return nil, domain.Unauthenticated("authenticated MCP identity required")
	}
	return actor, nil
}

// mcpVault resolves the vault for a typed tool. The vault may be omitted
// only when exactly one vault is accessible to this credential; the
// service layer re-checks the actual operation permission either way.
func (s *Server) mcpVault(ctx context.Context, actor *service.Actor, vault string) (string, error) {
	who, err := s.Svc.AgentWhoami(ctx, actor)
	if err != nil {
		return "", err
	}
	if vault != "" {
		for _, v := range who.Vaults {
			if v.ID == vault {
				return vault, nil
			}
		}
		return "", domain.Forbidden("vault " + vault + " is not accessible to this credential; use a vaults[].id from netsidian_connect")
	}
	if len(who.Vaults) == 1 {
		return who.Vaults[0].ID, nil
	}
	ids := make([]string, 0, len(who.Vaults))
	for _, v := range who.Vaults {
		ids = append(ids, v.ID)
	}
	if len(ids) == 0 {
		return "", domain.Forbidden("this credential grants no vault; check scopes and vault bindings in netsidian_connect")
	}
	return "", domain.Invalid("vault is required when more than one vault is accessible; pass one of: " + strings.Join(ids, ", "))
}

type mcpSearchInput struct {
	Vault    string   `json:"vaultId,omitempty" jsonschema:"Vault ID; may be omitted only when exactly one vault is accessible"`
	Query    string   `json:"query,omitempty" jsonschema:"Search words matching token prefixes; required unless folder or tags is given"`
	Folder   string   `json:"folder,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Kinds    []string `json:"kinds,omitempty"`
	Limit    int      `json:"limit,omitempty" jsonschema:"1 to 20 results, default 8"`
	MaxBytes int      `json:"maxBytes,omitempty" jsonschema:"Hard response budget, 1024 to 32768, default 8192"`
	Cursor   string   `json:"cursor,omitempty" jsonschema:"Opaque nextCursor from a previous search; follow it unchanged"`
}

type mcpDocReadInput struct {
	Revision string `json:"revision,omitempty" jsonschema:"Required for continuation; use revision from first read"`
	Vault    string `json:"vaultId,omitempty" jsonschema:"Vault ID; may be omitted only when exactly one vault is accessible"`
	Doc      string `json:"documentId,omitempty" jsonschema:"Document ID (never a file path)"`
	Path     string `json:"path,omitempty" jsonschema:"Document path, e.g. agent-memory/index.md; use doc or path, not both"`
	Offset   int    `json:"offset,omitempty" jsonschema:"Byte offset into the markdown, default 0"`
	MaxBytes int    `json:"maxBytes,omitempty" jsonschema:"Serialized response budget, 1024 to 32768, default 8192"`
}

type mcpDocCreateInput struct {
	Vault          string  `json:"vaultId,omitempty"`
	Path           string  `json:"path" jsonschema:"New document path ending in .md"`
	Markdown       *string `json:"markdown" jsonschema:"Full initial Markdown content"`
	IdempotencyKey string  `json:"idempotencyKey,omitempty" jsonschema:"Reuse the same key when retrying this logical create"`
}

type mcpDocUpdateInput struct {
	Vault          string  `json:"vaultId,omitempty"`
	Doc            string  `json:"documentId,omitempty" jsonschema:"Document ID (never a file path)"`
	Path           string  `json:"path,omitempty" jsonschema:"Document path; use doc or path, not both"`
	Markdown       *string `json:"markdown" jsonschema:"Complete replacement Markdown; an explicit empty string clears the document"`
	IfMatch        string  `json:"revision" jsonschema:"Current revision from a recent read; required"`
	IdempotencyKey string  `json:"idempotencyKey,omitempty" jsonschema:"Reuse the same key when retrying this logical update"`
}

type mcpMemoryRecallInput struct {
	Cursor    string `json:"cursor,omitempty" jsonschema:"Opaque nextCursor; pass it alone with vaultId for generation- and filter-pinned continuation"`
	Folder    string `json:"folder,omitempty"`
	Revision  string `json:"revision,omitempty" jsonschema:"Required with offset; use indexRevision from first recall page"`
	QueryHash string `json:"queryHash,omitempty" jsonschema:"Required with offset; use queryHash from first recall page; the full filter set must match"`
	Vault     string `json:"vaultId,omitempty"`
	Query     string `json:"query,omitempty" jsonschema:"Case-insensitive substring over key, scope and text"`
	Scope     string `json:"scope,omitempty" jsonschema:"Exact scope filter, case-insensitive"`
	Key       string `json:"key,omitempty" jsonschema:"Exact stable key"`
	Kind      string `json:"kind,omitempty" jsonschema:"Exact kind filter: fact, preference, decision, episode, procedure or inference"`
	ScopeID   string `json:"scopeId,omitempty" jsonschema:"Exact scopeId filter of versioned entries"`
	Status    string `json:"status,omitempty" jsonschema:"active (default when set), superseded, disputed, retracted or all; unset keeps legacy behavior (everything)"`
	ValidOn   string `json:"validOn,omitempty" jsonschema:"RFC3339 instant; return only entries valid at that time"`
	Limit     int    `json:"limit,omitempty" jsonschema:"1 to 50 entries, default 20"`
	Offset    int    `json:"offset,omitempty"`
	MaxBytes  int    `json:"maxBytes,omitempty" jsonschema:"Response budget, 1024 to 32768, default 8192"`
}

type mcpMemoryRememberInput struct {
	Folder           string              `json:"folder,omitempty"`
	ExpectedRevision string              `json:"expectedRevision,omitempty" jsonschema:"Optional index revision from recall; rejects stale edits"`
	DetailRevision   string              `json:"detailRevision,omitempty" jsonschema:"Required when replacing existing linked detail content"`
	Vault            string              `json:"vaultId,omitempty"`
	Key              string              `json:"key" jsonschema:"Stable entry key matching ^[a-z0-9][a-z0-9._-]{0,95}$; reuse it to update the same topic"`
	Text             string              `json:"text" jsonschema:"Exact short text, at most 500 characters"`
	Scope            string              `json:"scope,omitempty" jsonschema:"Optional context label such as user or project, at most 64 characters"`
	Refs             []string            `json:"refs,omitempty" jsonschema:"Existing document paths/IDs, folders, boards or maps to reference, at most 16"`
	Detail           string              `json:"detail,omitempty" jsonschema:"Long Markdown kept in a linked detail document, never duplicated in the index"`
	DetailPath       string              `json:"detailPath,omitempty" jsonschema:"Optional .md path inside the memory details folder"`
	Kind             string              `json:"kind,omitempty" jsonschema:"fact, preference, decision, episode, procedure or inference"`
	ScopeID          string              `json:"scopeId,omitempty" jsonschema:"Stable machine-readable scope id, at most 64 characters"`
	Status           string              `json:"status,omitempty" jsonschema:"active, superseded, disputed or retracted"`
	EvidenceClass    string              `json:"evidenceClass,omitempty" jsonschema:"user-stated, observed, sourced or inferred"`
	ValidFrom        string              `json:"validFrom,omitempty" jsonschema:"RFC3339 instant from which the claim holds"`
	ValidTo          string              `json:"validTo,omitempty" jsonschema:"RFC3339 instant until which the claim holds"`
	SourceRefs       []memorySourceInput `json:"sourceRefs,omitempty" jsonschema:"Up to 16 pinned evidence references, distinct from latest-resource refs"`
	Supersedes       []memoryClaimInput  `json:"supersedes,omitempty"`
	DerivedFrom      []memoryClaimInput  `json:"derivedFrom,omitempty"`
	ClearFields      []string            `json:"clearFields,omitempty" jsonschema:"Explicitly clear optional metadata; omitted or null fields preserve existing values"`
	IdempotencyKey   string              `json:"idempotencyKey,omitempty" jsonschema:"Reuse the same key when retrying this logical remember"`
}

type mcpMemoryReviseInput struct {
	Folder           string `json:"folder,omitempty"`
	Vault            string `json:"vaultId,omitempty"`
	Key              string `json:"key" jsonschema:"Existing entry key to revise"`
	Action           string `json:"action" jsonschema:"supersede (replace text, pin prior claim), retract, dispute or resolve"`
	ExpectedRevision string `json:"expectedRevision" jsonschema:"indexRevision from recall; required, stale revisions are rejected"`
	Text             string `json:"text,omitempty" jsonschema:"Replacement claim, supersede only, at most 500 characters"`
	Kind             string `json:"kind,omitempty"`
	EvidenceClass    string `json:"evidenceClass,omitempty"`
	Reason           string `json:"reason,omitempty" jsonschema:"Optional audit note for the revision, at most 500 characters"`
	IdempotencyKey   string `json:"idempotencyKey,omitempty" jsonschema:"Reuse the same key when retrying this logical revision"`
}

type mcpConsolidateSource struct {
	Key              string `json:"key,omitempty" jsonschema:"Index entry key relied upon"`
	DocumentID       string `json:"documentId,omitempty" jsonschema:"Document id relied upon"`
	ExpectedRevision string `json:"expectedRevision,omitempty" jsonschema:"Revision the plan was built against"`
}

type mcpConsolidateTarget struct {
	DocumentID          string  `json:"documentId,omitempty" jsonschema:"Existing target document"`
	Path                string  `json:"path,omitempty" jsonschema:"New target document path"`
	ExpectedRevision    string  `json:"expectedRevision,omitempty" jsonschema:"Required for existing targets; from a prior recall/read"`
	Markdown            *string `json:"markdown" jsonschema:"Managed-section body; human content outside the managed block is preserved"`
	AdoptManagedSection bool    `json:"adoptManagedSection,omitempty" jsonschema:"Explicitly own an existing human-authored document by appending a managed section"`
}

type mcpConsolidateEntry struct {
	Key        string   `json:"key"`
	Status     string   `json:"status,omitempty" jsonschema:"active, superseded, disputed or retracted"`
	Text       string   `json:"text,omitempty" jsonschema:"Replacement claim, at most 500 characters"`
	Supersedes []string `json:"supersedes,omitempty" jsonschema:"Keys this entry replaces; they become superseded"`
}

type mcpMemoryConsolidateInput struct {
	Folder           string                 `json:"folder,omitempty"`
	Vault            string                 `json:"vaultId,omitempty"`
	OperationID      string                 `json:"operationId" jsonschema:"Stable id across retries; identical replays resume, changed plans are rejected"`
	ExpectedRevision string                 `json:"expectedRevision,omitempty" jsonschema:"Required index revision for entry changes; claim sources may inherit this pin"`
	DryRun           bool                   `json:"dryRun,omitempty" jsonschema:"Validate the plan and report conflicts without writing"`
	Sources          []mcpConsolidateSource `json:"sources,omitempty" jsonschema:"Pinned evidence the plan relies on, at most 16"`
	Targets          []mcpConsolidateTarget `json:"targets,omitempty" jsonschema:"Topical document edits, at most 1"`
	Entries          []mcpConsolidateEntry  `json:"entries,omitempty" jsonschema:"Index entry status/text changes, at most 32"`
}

func (s *Server) addMCPWorkflowTools(server *mcp.Server) {
	readonly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: mcpBoolPtr(false), DestructiveHint: mcpBoolPtr(false)}
	guarded := &mcp.ToolAnnotations{IdempotentHint: true, OpenWorldHint: mcpBoolPtr(false), DestructiveHint: mcpBoolPtr(true)}

	mcp.AddTool(server, &mcp.Tool{Name: "netsidian_search", Description: "Search snippets within a byte budget. Use returned nextCursor unchanged for more results.", Annotations: readonly}, func(ctx context.Context, r *mcp.CallToolRequest, in mcpSearchInput) (*mcp.CallToolResult, any, error) {
		actor, err := s.mcpActor(ctx)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		space, err := s.mcpVault(ctx, actor, in.Vault)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		if in.Limit < 0 || in.Limit > 20 || in.MaxBytes < 0 || (in.MaxBytes > 0 && in.MaxBytes < 1024) || in.MaxBytes > 32768 {
			return mcpToolError(domain.Invalid("limit must be 1..20 and maxBytes 1024..32768")), nil, nil
		}
		resp, err := s.Svc.AgentContext(ctx, actor, space, &service.AgentContextRequest{
			Query: in.Query, Folder: in.Folder, Tags: in.Tags, Kinds: in.Kinds,
			MaxResults: in.Limit, MaxBytes: in.MaxBytes, Cursor: in.Cursor,
		})
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		items := make([]map[string]any, 0, len(resp.Items))
		for _, item := range resp.Items {
			items = append(items, map[string]any{"documentId": item.DocumentID, "path": item.Path, "title": item.Title, "revision": item.Revision, "excerpt": item.Excerpt})
		}
		out := map[string]any{"vault": space, "items": items, "truncated": resp.Truncated, "budgetUsed": resp.BudgetUsed}
		if resp.NextCursor != "" {
			out["nextCursor"] = resp.NextCursor
		}
		budget := in.MaxBytes
		if budget == 0 {
			budget = 8192
		}
		data, _ := json.Marshal(out)
		if len(data) > budget {
			return mcpToolError(domain.TooLarge("search response exceeds maxBytes; increase the budget")), nil, nil
		}
		return mcpText(out, false), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{Name: "netsidian_document_read", Description: "Read by documentId or path. Bounded JSON; continue with nextOffset and the returned revision.", Annotations: readonly}, func(ctx context.Context, r *mcp.CallToolRequest, in mcpDocReadInput) (*mcp.CallToolResult, any, error) {
		actor, err := s.mcpActor(ctx)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		space, err := s.mcpVault(ctx, actor, in.Vault)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		chunk, err := s.Svc.ReadDocumentChunk(ctx, actor, space, in.Doc, in.Path, in.Revision, in.Offset, in.MaxBytes)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		return mcpText(chunk, false), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{Name: "netsidian_document_create", Description: "Create Markdown at a new path. Reuse idempotencyKey for identical retries.", Annotations: guarded}, func(ctx context.Context, r *mcp.CallToolRequest, in mcpDocCreateInput) (*mcp.CallToolResult, any, error) {
		actor, err := s.mcpActor(ctx)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		space, err := s.mcpVault(ctx, actor, in.Vault)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		if in.Path == "" || in.Markdown == nil {
			return mcpText(map[string]any{"error": "path and markdown are both required"}, true), nil, nil
		}
		if err := checkMCPKeyHeader(in.IdempotencyKey); err != nil {
			return mcpToolError(err), nil, nil
		}
		doc, err := s.Svc.CreateNote(ctx, actor, space, in.Path, *in.Markdown, in.IdempotencyKey, false)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		return mcpText(map[string]any{"documentId": doc.ID, "path": doc.Path, "revision": doc.CurrentRevision, "created": true}, false), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{Name: "netsidian_document_update", Description: "Replace Markdown using its current revision. Read all chunks first. Explicit empty clears content; stale revisions fail.", Annotations: guarded}, func(ctx context.Context, r *mcp.CallToolRequest, in mcpDocUpdateInput) (*mcp.CallToolResult, any, error) {
		actor, err := s.mcpActor(ctx)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		space, err := s.mcpVault(ctx, actor, in.Vault)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		if (in.Doc == "") == (in.Path == "") {
			return mcpText(map[string]any{"error": "provide exactly one of documentId or path"}, true), nil, nil
		}
		if in.Markdown == nil {
			return mcpText(map[string]any{"error": "markdown is required; pass an explicit empty string to clear the document"}, true), nil, nil
		}
		if in.IfMatch == "" {
			return mcpText(map[string]any{"error": "revision from a recent read from a recent read is required"}, true), nil, nil
		}
		if err := checkMCPKeyHeader(in.IdempotencyKey); err != nil {
			return mcpToolError(err), nil, nil
		}
		docID := in.Doc
		if docID == "" {
			doc, err := s.Svc.LookupDocument(ctx, actor, space, in.Path)
			if err != nil {
				return mcpToolError(err), nil, nil
			}
			docID = doc.ID
		}
		saved, err := s.Svc.SaveContent(ctx, actor, space, docID, in.IfMatch, *in.Markdown, in.IdempotencyKey)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		return mcpText(map[string]any{"documentId": docID, "revision": saved.Revision, "updated": true}, false), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{Name: "netsidian_memory_recall", Description: "Read filtered memory index entries and links, without linked bodies. Continue with nextOffset and indexRevision.", Annotations: readonly}, func(ctx context.Context, r *mcp.CallToolRequest, in mcpMemoryRecallInput) (*mcp.CallToolResult, any, error) {
		actor, err := s.mcpActor(ctx)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		space, err := s.mcpVault(ctx, actor, in.Vault)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		res, err := s.Svc.RecallMemory(ctx, actor, space, &service.MemoryRecallInput{Cursor: in.Cursor, Folder: in.Folder, Revision: in.Revision, QueryHash: in.QueryHash, Query: in.Query, Scope: in.Scope, Key: in.Key, Kind: in.Kind, ScopeID: in.ScopeID, Status: in.Status, ValidOn: in.ValidOn, Limit: in.Limit, Offset: in.Offset, MaxBytes: in.MaxBytes})
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		return mcpText(res, false), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{Name: "netsidian_memory_remember", Description: "Upsert a short memory by key; link existing resources or store detail separately. Preserves other entries; verifies saves.", Annotations: guarded}, func(ctx context.Context, r *mcp.CallToolRequest, in mcpMemoryRememberInput) (*mcp.CallToolResult, any, error) {
		actor, err := s.mcpActor(ctx)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		space, err := s.mcpVault(ctx, actor, in.Vault)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		if err := checkMCPKeyHeader(in.IdempotencyKey); err != nil {
			return mcpToolError(err), nil, nil
		}
		srcRefs := sourceInputs(in.SourceRefs)
		receipt, err := s.Svc.RememberMemory(ctx, actor, space, &service.MemoryRememberInput{
			Folder: in.Folder, ExpectedRevision: in.ExpectedRevision, DetailRevision: in.DetailRevision, Key: in.Key, Text: in.Text, Scope: in.Scope, Refs: in.Refs, Detail: in.Detail, DetailPath: in.DetailPath,
			Kind: in.Kind, ScopeID: in.ScopeID, Status: in.Status, EvidenceClass: in.EvidenceClass, ValidFrom: in.ValidFrom, ValidTo: in.ValidTo, SourceRefs: srcRefs, Supersedes: claimInputs(in.Supersedes), DerivedFrom: claimInputs(in.DerivedFrom), ClearFields: in.ClearFields,
		}, in.IdempotencyKey)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		return mcpText(receipt, false), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{Name: "netsidian_memory_revise", Description: "Guarded correction/retraction of one indexed entry: supersede, retract, dispute or resolve. expectedRevision required; supersede pins the prior claim, history is never purged.", Annotations: guarded}, func(ctx context.Context, r *mcp.CallToolRequest, in mcpMemoryReviseInput) (*mcp.CallToolResult, *service.MemoryRevisionReceipt, error) {
		actor, err := s.mcpActor(ctx)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		space, err := s.mcpVault(ctx, actor, in.Vault)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		if err := checkMCPKeyHeader(in.IdempotencyKey); err != nil {
			return mcpToolError(err), nil, nil
		}
		receipt, err := s.Svc.ReviseMemory(ctx, actor, space, &service.MemoryReviseInput{
			Folder: in.Folder, Key: in.Key, Action: in.Action, ExpectedRevision: in.ExpectedRevision, Text: in.Text, Kind: in.Kind, EvidenceClass: in.EvidenceClass, Reason: in.Reason,
		}, in.IdempotencyKey)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		return mcpText(receipt, false), receipt, nil
	})

	mcp.AddTool(server, &mcp.Tool{Name: "netsidian_memory_consolidate", Description: "Apply a bounded consolidation plan (targets + index entry changes) with a durable resumable journal. Partial failures return honest receipts; replay the same operationId to resume; never rolls back user edits.", Annotations: guarded}, func(ctx context.Context, r *mcp.CallToolRequest, in mcpMemoryConsolidateInput) (*mcp.CallToolResult, *service.MemoryConsolidationReceipt, error) {
		actor, err := s.mcpActor(ctx)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		space, err := s.mcpVault(ctx, actor, in.Vault)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		plan := &service.MemoryConsolidateInput{Folder: in.Folder, OperationID: in.OperationID, ExpectedRevision: in.ExpectedRevision, DryRun: in.DryRun}
		for _, src := range in.Sources {
			plan.Sources = append(plan.Sources, service.MemoryConsolidateSource{Key: src.Key, DocumentID: src.DocumentID, ExpectedRevision: src.ExpectedRevision})
		}
		for _, t := range in.Targets {
			plan.Targets = append(plan.Targets, service.MemoryConsolidateTarget{DocumentID: t.DocumentID, Path: t.Path, ExpectedRevision: t.ExpectedRevision, Markdown: t.Markdown, AdoptManagedSection: t.AdoptManagedSection})
		}
		for _, e := range in.Entries {
			plan.Entries = append(plan.Entries, service.MemoryConsolidateEntry{Key: e.Key, Status: e.Status, Text: e.Text, Supersedes: e.Supersedes})
		}
		receipt, err := s.Svc.ConsolidateMemory(ctx, actor, space, plan)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		return mcpText(receipt, false), receipt, nil
	})
}

func checkMCPKeyHeader(key string) error {
	if len(key) > 1024 || strings.ContainsAny(key, "\x00\r\n") {
		return domain.Invalid("invalid idempotencyKey")
	}
	return nil
}

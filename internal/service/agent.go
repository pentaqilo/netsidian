package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// Agent contract version served over REST and MCP alike (agent-memory
// PLAN.md). Payloads evolve independently of the database schema.
const AgentContract = "netsidian-agent-v2"

// ---- whoami ----

type AgentWhoami struct {
	Contract  string         `json:"contract"`
	Principal AgentPrincipal `json:"principal"`
	Auth      AgentAuth      `json:"auth"`
	Vaults    []AgentVault   `json:"vaults"`
	Tools     []AgentToolRef `json:"tools"`
}

type AgentPrincipal struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
}

type AgentAuth struct {
	Method     string   `json:"method"` // session | api-key | local
	KeyID      string   `json:"keyId,omitempty"`
	KeyLabel   string   `json:"keyLabel,omitempty"`
	Scopes     []string `json:"scopes,omitempty"`
	BindVaults []string `json:"keyBoundVaults,omitempty"`
	ReadOnly   bool     `json:"readOnly"`
}

type AgentVault struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Role              string   `json:"role"`
	AllowedOperations []string `json:"allowedOperations"`
}

type AgentToolRef struct {
	Name        string `json:"name"`
	MinContract string `json:"minContract"`
}

// AgentWhoami reports exactly what this credential may do before a client
// enables tools (capabilities discovery). Human sessions inherit the role
// matrix; keys intersect it with their scopes and per-vault grants.
func (s *Services) AgentWhoami(ctx context.Context, a *Actor) (*AgentWhoami, error) {
	if a == nil || a.UserID == "" {
		return nil, domain.Unauthenticated("authentication required")
	}
	spaces, roles, err := s.Spaces.SpacesForUser(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	w := &AgentWhoami{
		Contract:  AgentContract,
		Principal: AgentPrincipal{UserID: a.UserID, DisplayName: a.DisplayName},
		Auth:      AgentAuth{Method: "session"},
		Vaults:    []AgentVault{},
	}
	if a.Key != nil {
		w.Auth = AgentAuth{Method: "api-key", KeyID: a.Key.ID, KeyLabel: a.Key.Label,
			Scopes: a.Key.Scopes, BindVaults: a.Key.SpaceIDs}
	} else if a.Local {
		w.Auth.Method = "local"
	}
	actions := []domain.Action{domain.ActionRead, domain.ActionSearch, domain.ActionExport,
		domain.ActionCreate, domain.ActionEdit, domain.ActionAttachments, domain.ActionTrash, domain.ActionRestore, domain.ActionPurge, domain.ActionMembers, domain.ActionMemoryPropose, domain.ActionMemoryReview, domain.ActionMemoryDelete}
	anyWrite := false
	for i, sp := range spaces {
		role := roles[i]
		allowed := []string{}
		if a.Key != nil {
			bound := false
			for _, id := range a.Key.SpaceIDs {
				if id == sp.ID {
					bound = true
					break
				}
			}
			if !bound {
				continue // key is not granted this vault at all
			}
		}
		for _, act := range actions {
			if !domain.RoleCan(role, act) {
				continue
			}
			if a.Key != nil && len(a.Key.Scopes) > 0 {
				scope := domain.ScopeForAction(act)
				if scope != "" && !hasScope(a.Key.Scopes, scope) {
					continue
				}
			}
			allowed = append(allowed, string(act))
			if act != domain.ActionRead && act != domain.ActionSearch && act != domain.ActionExport {
				anyWrite = true
			}
		}
		for _, special := range []struct{ scope, operation string }{{domain.ScopeGraphRead, "graph"}, {domain.ScopeAttachmentsRead, "attachments_read"}, {domain.ScopeHistoryRead, "history_read"}, {domain.ScopeEventsRead, "events"}} {
			if (special.scope == domain.ScopeHistoryRead || special.scope == domain.ScopeEventsRead) && a.Key != nil && (!hasScope(a.Key.Scopes, special.scope) || !hasScope(a.Key.Scopes, domain.ScopeDocumentsRead)) {
				continue
			}
			if domain.RoleCan(role, domain.ActionRead) && (a.Key == nil || len(a.Key.Scopes) == 0 || hasScope(a.Key.Scopes, special.scope)) {
				allowed = append(allowed, special.operation)
			}
		}
		if len(allowed) == 0 {
			continue
		}
		w.Vaults = append(w.Vaults, AgentVault{ID: sp.ID, Name: sp.Name, Role: string(role), AllowedOperations: allowed})
	}
	w.Auth.ReadOnly = !anyWrite
	w.Tools = []AgentToolRef{
		{Name: "documents", MinContract: AgentContract},
		{Name: "search", MinContract: AgentContract},
		{Name: "graph-analysis", MinContract: AgentContract},
		{Name: "context", MinContract: AgentContract},
		{Name: "changes-since", MinContract: AgentContract},
	}
	return w, nil
}

// ---- agent context ----

type AgentContextRequest struct {
	Query              string    `json:"query"`
	Folder             string    `json:"folder,omitempty"`
	Tags               []string  `json:"tags,omitempty"`
	Kinds              []string  `json:"kinds,omitempty"` // note | memory
	UpdatedAfter       time.Time `json:"updatedAfter,omitempty"`
	IncludeMemories    *bool     `json:"includeMemories,omitempty"`
	IncludeStale       bool      `json:"includeStale,omitempty"`
	IncludeGraph       *bool     `json:"includeGraph,omitempty"`
	GraphDepth         int       `json:"graphDepth,omitempty"`
	MaxResults         int       `json:"maxResults,omitempty"`
	MaxTokens          int       `json:"maxTokens,omitempty"` // hint only; MaxBytes is the hard budget
	MaxBytes           int       `json:"maxBytes,omitempty"`
	SnippetChars       int       `json:"snippetChars,omitempty"`
	Cursor             string    `json:"cursor,omitempty"`
	ExcludeDocumentIDs []string  `json:"excludeDocumentIds,omitempty"`
}

type AgentContextItem struct {
	DocumentID string   `json:"documentId"`
	Path       string   `json:"path"`
	Title      string   `json:"title"`
	Revision   string   `json:"revision"`
	Kind       string   `json:"kind"`
	Excerpt    string   `json:"excerpt"`
	Score      float64  `json:"score"`
	Reasons    []string `json:"reasons"`
}

type AgentRelationship struct {
	SourceID    string `json:"sourceId"`
	TargetID    string `json:"targetId"`
	Kind        string `json:"kind"` // explicit-link
	Direction   string `json:"direction"`
	Fragment    string `json:"fragment,omitempty"`
	EvidenceRev string `json:"evidenceRevision"`
}

type AgentMemoryItem struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	Title            string   `json:"title"`
	DocumentID       string   `json:"documentId"`
	ApprovedRevision string   `json:"approvedRevision"`
	Stale            bool     `json:"stale,omitempty"`
	StaleReasons     []string `json:"staleReasons,omitempty"`
	ExpiresAt        string   `json:"expiresAt,omitempty"`
}

type AgentBudget struct {
	Bytes           int `json:"bytes"`
	EstimatedTokens int `json:"estimatedTokens"`
}

type AgentContextResponse struct {
	Contract        string              `json:"contract"`
	Items           []AgentContextItem  `json:"items"`
	Relationships   []AgentRelationship `json:"relationships"`
	Memories        []AgentMemoryItem   `json:"-"`
	NextCursor      string              `json:"nextCursor,omitempty"`
	Truncated       bool                `json:"truncated"`
	BudgetUsed      AgentBudget         `json:"budgetUsed"`
	IndexGeneration int64               `json:"indexGeneration"`
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// AgentContext composes one authorized retrieval pass: lexical FTS first,
// filters and bounded graph evidence, packed
// deterministically into a byte budget (plan: token-budgeted context).
func (s *Services) AgentContext(ctx context.Context, a *Actor, spaceID string, req *AgentContextRequest) (*AgentContextResponse, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionSearch); err != nil {
		return nil, err
	}
	req.Query = strings.TrimSpace(req.Query)
	if utf8.RuneCountInString(req.Query) > 400 {
		return nil, domain.Invalid("query exceeds 400 characters")
	}
	if req.Query == "" && req.Folder == "" && len(req.Tags) == 0 {
		return nil, domain.Invalid("query, folder or tags required")
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 8
	}
	if req.MaxResults > 20 {
		req.MaxResults = 20
	}
	if req.MaxBytes <= 0 {
		req.MaxBytes = 8192
	}
	if req.MaxBytes > 131072 {
		req.MaxBytes = 131072
	}
	if req.SnippetChars <= 0 {
		req.SnippetChars = 240
	}
	if req.SnippetChars > 600 {
		req.SnippetChars = 600
	}
	if req.IncludeMemories != nil && *req.IncludeMemories {
		return nil, domain.Unsupported("use document search with a memory folder; dedicated memories are retired")
	}
	includeGraph := false
	if req.IncludeGraph != nil {
		includeGraph = *req.IncludeGraph
	}
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	if includeGraph {
		if _, err := s.requireReadScope(ctx, a, spaceID, domain.ScopeGraphRead); err != nil {
			return nil, err
		}
	}
	if req.GraphDepth <= 0 {
		req.GraphDepth = 1
	}
	if req.GraphDepth > 2 {
		req.GraphDepth = 2
	}
	if len(req.Tags) > 20 || len(req.Kinds) > 4 {
		return nil, domain.Invalid("too many tag or kind filters")
	}
	if len(req.ExcludeDocumentIDs) > 200 {
		return nil, domain.Invalid("too many excluded documents")
	}

	if req.Folder != "" {
		canon, _, err := domain.NormalizePath(req.Folder)
		if err != nil {
			return nil, err
		}
		req.Folder = canon
	}
	scope := contextScope(spaceID, req)
	gen, err := s.Docs.MaxEventSeq(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	offset := 0
	if req.Cursor != "" {
		c, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, domain.Invalid("malformed cursor")
		}
		if c.Scope != scope {
			return nil, domain.Conflict("cursor belongs to a different query scope; restart iteration")
		}
		if c.Gen != gen {
			return nil, domain.Conflict("index changed since this cursor was issued; restart iteration")
		}
		offset = c.Offset
	}

	// Search applies all filters before stable SQL pagination, so later
	// pages remain reachable even in very large vaults.
	hits, err := s.Index.Search(ctx, &ports.SearchQuery{SpaceID: spaceID, Text: req.Query, Tags: req.Tags, FolderPrefix: req.Folder, Kinds: req.Kinds, UpdatedAfter: req.UpdatedAfter, ExcludeIDs: req.ExcludeDocumentIDs, Limit: req.MaxResults + 1, Offset: offset})
	if err != nil {
		return nil, err
	}
	type cand struct {
		hit     *ports.SearchHit
		doc     *domain.Document
		score   float64
		reasons []string
	}
	candidates := []cand{}
	for _, h := range hits {
		d, err := s.Docs.Document(ctx, h.DocumentID)
		if err != nil {
			return nil, err
		}
		if d == nil || d.SpaceID != spaceID || d.TombstoneAt != nil {
			return nil, domain.Conflict("documents changed during retrieval; restart iteration")
		}
		reasons := []string{}
		if req.Query != "" {
			reasons = append(reasons, "lexical")
		}
		if req.Folder != "" {
			reasons = append(reasons, "folder")
		}
		if len(req.Tags) > 0 {
			reasons = append(reasons, "tag")
		}
		candidates = append(candidates, cand{h, d, -h.Score, reasons})
	}

	resp := &AgentContextResponse{Contract: AgentContract,
		Items: []AgentContextItem{}, Relationships: []AgentRelationship{}, Memories: []AgentMemoryItem{}}

	// 3. pack items into the byte budget after the cursor offset
	used, consumed, truncated := 0, 0, false
	for _, c := range candidates {

		if len(resp.Items) >= req.MaxResults {
			truncated = true
			break
		}
		excerpt := clipRunes(stripMarks(c.hit.Snippet), req.SnippetChars)
		it := AgentContextItem{DocumentID: c.doc.ID, Path: c.doc.Path, Title: c.hit.Title,
			Revision: c.doc.CurrentRevision, Kind: c.doc.Kind,
			Excerpt: excerpt, Score: c.score, Reasons: c.reasons}
		if c.doc.Kind == "memory" {
			it.Reasons = append(it.Reasons, "memory")
		}
		encoded, _ := json.Marshal(it)
		cost := len(encoded)
		if used+cost > req.MaxBytes {
			truncated = true
			break
		}
		used += cost
		resp.Items = append(resp.Items, it)
		consumed++
	}

	// 4. bounded graph evidence around the returned seeds
	if includeGraph && len(resp.Items) > 0 {
		seeds := make([]string, 0, 8)
		for _, it := range resp.Items {
			if len(seeds) >= 8 {
				break
			}
			seeds = append(seeds, it.DocumentID)
		}
		edges, err := s.Index.Adjacency(ctx, spaceID, seeds, req.GraphDepth, 60)
		if err != nil {
			return nil, err
		}
		linked := map[string]bool{}
		for _, e := range edges {
			resp.Relationships = append(resp.Relationships, AgentRelationship{
				SourceID: e.SourceID, TargetID: e.TargetID, Kind: "explicit-link",
				Direction: e.Direction, Fragment: e.Fragment, EvidenceRev: e.SourceRevision})
			linked[e.SourceID] = true
			linked[e.TargetID] = true
		}
		for i := range resp.Items {
			if linked[resp.Items[i].DocumentID] && !hasReason(resp.Items[i].Reasons, "link-neighbor") {
				resp.Items[i].Reasons = append(resp.Items[i].Reasons, "link-neighbor")
			}
		}
	}

	// 5. cursor only while more filtered candidates remain
	if consumed < len(candidates) {
		resp.NextCursor = encodeCursor(&cursorState{Scope: scope, Offset: offset + consumed, Gen: gen})
	}
	resp.Truncated = truncated || consumed < len(candidates)
	resp.BudgetUsed = AgentBudget{Bytes: used, EstimatedTokens: used / 4}
	resp.IndexGeneration = gen
	// The hard budget covers the serialized response, including
	// relationship evidence, cursor and budget metadata. Trim secondary evidence
	// first; never advance the cursor past a document omitted by the budget.
	size := func() int {
		for i := 0; i < 3; i++ {
			b, _ := json.Marshal(resp)
			resp.BudgetUsed = AgentBudget{Bytes: len(b) + 1, EstimatedTokens: (len(b) + 4) / 4}
		}
		b, _ := json.Marshal(resp)
		return len(b) + 1
	}
	for size() > req.MaxBytes {
		resp.Truncated = true
		if len(resp.Relationships) > 0 {
			resp.Relationships = resp.Relationships[:len(resp.Relationships)-1]
			continue
		}
		if len(resp.Memories) > 0 {
			resp.Memories = resp.Memories[:len(resp.Memories)-1]
			continue
		}
		if len(resp.Items) > 1 {
			resp.Items = resp.Items[:len(resp.Items)-1]
			consumed--
			resp.NextCursor = encodeCursor(&cursorState{Scope: scope, Offset: offset + consumed, Gen: gen})
			continue
		}
		if len(resp.Items) == 1 && resp.Items[0].Excerpt != "" {
			resp.Items[0].Excerpt = ""
			continue
		}
		if len(resp.Items) == 1 && resp.Items[0].Title != "" {
			resp.Items[0].Title = ""
			continue
		}
		return nil, domain.Invalid("maxBytes is too small for a result and its metadata; increase the budget")
	}
	if len(resp.Items) == 0 && len(candidates) > 0 {
		return nil, domain.Invalid("maxBytes is too small for the next document; increase the budget")
	}
	current, err := s.Docs.MaxEventSeq(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	if current != gen {
		return nil, domain.Conflict("documents changed during retrieval; restart iteration")
	}
	return resp, nil
}

func hasReason(rs []string, want string) bool {
	for _, r := range rs {
		if r == want {
			return true
		}
	}
	return false
}

func stripMarks(s string) string {
	s = strings.ReplaceAll(s, "<mark>", "")
	s = strings.ReplaceAll(s, "</mark>", "")
	return s
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ---- cursors ----

type cursorState struct {
	Scope  string `json:"s"`
	Offset int    `json:"o"`
	Gen    int64  `json:"g"`
}

// contextScope binds a cursor to principal-independent query identity:
// vault + query + filters + budgets. A mismatched cursor is rejected
// instead of silently changing result sets.
func contextScope(spaceID string, req *AgentContextRequest) string {
	copy := *req
	copy.Cursor = ""
	b, _ := json.Marshal(struct {
		Space   string
		Request AgentContextRequest
	}{spaceID, copy})
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(sum[:])[:16]
}

func encodeCursor(c *cursorState) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (*cursorState, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	c := &cursorState{}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	if c.Offset < 0 || c.Scope == "" {
		return nil, domain.Invalid("malformed cursor")
	}
	return c, nil
}

// ---- changes/since ----

type AgentChanges struct {
	Contract   string              `json:"contract"`
	Changed    []AgentChangeEntry  `json:"changed"`
	Tombstones []AgentChangeEntry  `json:"tombstones"`
	Memory     []AgentMemoryChange `json:"-"`
	NextCursor string              `json:"nextCursor"`
}

type AgentChangeEntry struct {
	DocumentID string `json:"documentId"`
	Revision   string `json:"revision,omitempty"`
	EventType  string `json:"eventType"`
	At         string `json:"at"`
}

type AgentMemoryChange struct {
	MemoryID   string `json:"memoryId,omitempty"`
	DocumentID string `json:"documentId"`
	At         string `json:"at"`
}

// ChangesSince replays the durable per-space outbox from an opaque cursor.
// Scoped to the caller's vault; no cross-space stream is reachable.
func (s *Services) ChangesSince(ctx context.Context, a *Actor, spaceID, cursor string, limit int) (*AgentChanges, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionSearch); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	seq := int64(0)
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		var state struct {
			Vault   string `json:"vault"`
			Seq     int64  `json:"seq"`
			Version int    `json:"version"`
		}
		if err != nil || json.Unmarshal(raw, &state) != nil || state.Version != 1 || state.Seq < 0 {
			return nil, domain.Invalid("malformed change cursor")
		}
		if state.Vault != spaceID {
			return nil, domain.Invalid("change cursor belongs to a different vault")
		}
		seq = state.Seq
	}

	events, err := s.Docs.EventsAfter(ctx, spaceID, seq, limit+1)
	if err != nil {
		return nil, err
	}
	if len(events) > limit {
		events = events[:limit]
	}
	out := &AgentChanges{Contract: AgentContract, Changed: []AgentChangeEntry{},
		Tombstones: []AgentChangeEntry{}, Memory: []AgentMemoryChange{}}
	last := seq
	for _, e := range events {
		at := e.CreatedAt.UTC().Format(time.RFC3339)
		switch e.EventType {
		case "document.committed", "document.renamed", "document.restored":
			out.Changed = append(out.Changed, AgentChangeEntry{DocumentID: e.DocumentID, Revision: e.RevisionID, EventType: e.EventType, At: at})
		case "document.trashed":
			out.Tombstones = append(out.Tombstones, AgentChangeEntry{DocumentID: e.DocumentID, EventType: e.EventType, At: at})
		}
		last = e.Seq
	}
	if len(events) > 0 {
		raw, _ := json.Marshal(struct {
			Vault   string `json:"vault"`
			Seq     int64  `json:"seq"`
			Version int    `json:"version"`
		}{spaceID, last, 1})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	} else {
		out.NextCursor = cursor
	}
	return out, nil
}

// ---- memory staleness ----

// decorateMemory compares the accepted revision and every source revision
// with the current ones; stale memories are surfaced, never silently used.
func (s *Services) decorateMemory(ctx context.Context, m *ports.MemoryRecord) AgentMemoryItem {
	item := AgentMemoryItem{ID: m.ID, Kind: m.Kind, Title: m.Title, DocumentID: m.DocumentID,
		ApprovedRevision: m.ApprovedRevision}
	if m.ExpiresAt != nil {
		item.ExpiresAt = m.ExpiresAt.UTC().Format(time.RFC3339)
	}
	var reasons []string
	if m.Status == "accepted" && m.ApprovedRevision != "" {
		if d, err := s.Docs.Document(ctx, m.DocumentID); err == nil && d != nil {
			if d.TombstoneAt != nil || d.CurrentRevision != m.ApprovedRevision {
				reasons = append(reasons, "memory note changed after approval")
			}
		}
	}
	if srcs, err := s.Mem.MemorySources(ctx, m.ID); err == nil {
		for _, src := range srcs {
			if src.SourceRevision == "" {
				continue
			}
			if d, err := s.Docs.Document(ctx, src.SourceDocumentID); err != nil || d == nil {
				reasons = append(reasons, "source "+src.SourceDocumentID+" is unavailable")
			} else {
				if d.TombstoneAt != nil || d.CurrentRevision != src.SourceRevision {
					reasons = append(reasons, "source "+src.SourceDocumentID+" changed or was removed after its pinned revision")
				}
			}
		}
	}
	if len(reasons) > 0 {
		item.Stale = true
		item.StaleReasons = reasons
	}
	return item
}

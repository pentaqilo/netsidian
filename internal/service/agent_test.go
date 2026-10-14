// Agent data interface acceptance (review/agent-memory/PLAN.md):
// whoami capability projection, byte-budgeted deterministic context with
// cursor scope binding, outbox change feed replay, idempotent memory
// proposals with provenance validation, and staleness surfacing.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pentaqilo/netsidian/internal/ports"
	"strings"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func agentErrCode(t *testing.T, err error) domain.ErrorCode {
	t.Helper()
	de, ok := err.(*domain.Error)
	if !ok {
		t.Fatalf("want *domain.Error, got %T: %v", err, err)
	}
	return de.Code
}

func boolp(v bool) *bool { return &v }

// seedNotes creates n matching notes in the vault.
func seedNotes(t *testing.T, h *harness, ctx context.Context, vault, topic string, n int) []string {
	t.Helper()
	ids := []string{}
	for i := 0; i < n; i++ {
		d, err := h.svc.CreateNote(ctx, h.actor, vault, topic+"-"+string(rune('a'+i))+".md",
			"# "+topic+" log "+string(rune('a'+i))+"\n\nNotes about the "+topic+" experiment, batch "+topic+".\n", "", false)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.ID)
	}
	return ids
}

func TestAgentWhoamiKeyScopes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	vault, err := h.svc.CreateSpace(ctx, h.actor, "AgentVault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	viewer := w01Join(t, h, h.actor, vault.ID, "agent-viewer@x.local", domain.RoleViewer)
	rec, secret, err := h.svc.CreateKey(ctx, viewer, "ro-agent", []string{vault.ID},
		[]string{domain.ScopeDocumentsRead, domain.ScopeSearchRead}, 30)
	if err != nil {
		t.Fatal(err)
	}
	_, u, err := h.svc.AuthenticateKey(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	keyActor := &Actor{UserID: u.ID, DisplayName: u.DisplayName, Key: rec}
	who, err := h.svc.AgentWhoami(ctx, keyActor)
	if err != nil {
		t.Fatal(err)
	}
	if who.Contract != AgentContract || who.Auth.Method != "api-key" || who.Auth.KeyID != rec.ID {
		t.Fatalf("auth projection wrong: %+v", who.Auth)
	}
	if !who.Auth.ReadOnly {
		t.Fatal("read-only key must project ReadOnly")
	}
	if len(who.Vaults) != 1 || who.Vaults[0].ID != vault.ID {
		t.Fatalf("vault projection wrong: %+v", who.Vaults)
	}
	ops := strings.Join(who.Vaults[0].AllowedOperations, ",")
	if !strings.Contains(ops, "read") || strings.Contains(ops, "create") || strings.Contains(ops, "edit") {
		t.Fatalf("scope filter wrong: %s", ops)
	}
	// the same user via session keeps role rights (no scope filter);
	// a viewer role is still read-only by role, an editor is not
	who2, err := h.svc.AgentWhoami(ctx, viewer)
	if err != nil {
		t.Fatal(err)
	}
	if who2.Auth.Method != "session" || !who2.Auth.ReadOnly {
		t.Fatalf("viewer session projection wrong: %+v", who2.Auth)
	}
	editor := w01Join(t, h, h.actor, vault.ID, "agent-editor@x.local", domain.RoleEditor)
	who3, err := h.svc.AgentWhoami(ctx, editor)
	if err != nil {
		t.Fatal(err)
	}
	if who3.Auth.ReadOnly {
		t.Fatalf("editor session must not project ReadOnly: %+v", who3.Auth)
	}
}

func TestAgentContextBudgetCursorAndIsolation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	vault, err := h.svc.CreateSpace(ctx, h.actor, "CtxVault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	other, err := h.svc.CreateSpace(ctx, h.actor, "OtherVault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	seedNotes(t, h, ctx, vault.ID, "quantum", 12)
	seedNotes(t, h, ctx, other.ID, "quantum", 3)
	req := func() *AgentContextRequest {
		return &AgentContextRequest{Query: "quantum", MaxBytes: 700,
			IncludeMemories: boolp(false), IncludeGraph: boolp(false)}
	}
	r1, err := h.svc.AgentContext(ctx, h.actor, vault.ID, req())
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Items) == 0 || r1.BudgetUsed.Bytes > 700 {
		t.Fatalf("budget violated: items=%d bytes=%d", len(r1.Items), r1.BudgetUsed.Bytes)
	}
	if !r1.Truncated || r1.NextCursor == "" {
		t.Fatalf("expected truncated page with cursor: %+v", r1.Truncated)
	}
	// determinism: same request, same order
	r1b, err := h.svc.AgentContext(ctx, h.actor, vault.ID, req())
	if err != nil {
		t.Fatal(err)
	}
	if len(r1b.Items) != len(r1.Items) {
		t.Fatal("nondeterministic page size")
	}
	for i := range r1.Items {
		if r1.Items[i].DocumentID != r1b.Items[i].DocumentID {
			t.Fatal("nondeterministic ordering")
		}
	}
	// paging resumes with disjoint items
	r2, err := h.svc.AgentContext(ctx, h.actor, vault.ID, func() *AgentContextRequest {
		q := req()
		q.Cursor = r1.NextCursor
		return q
	}())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, it := range r1.Items {
		seen[it.DocumentID] = true
	}
	for _, it := range r2.Items {
		if seen[it.DocumentID] {
			t.Fatal("cursor replayed an already-served document")
		}
	}
	// cursor is bound to its vault: replaying in another vault is rejected
	q := req()
	q.Cursor = r1.NextCursor
	if code := agentErrCode(t, func() error { _, e := h.svc.AgentContext(ctx, h.actor, other.ID, q); return e }()); code != domain.ErrCodeConflict {
		t.Fatalf("cross-vault cursor: want conflict, got %s", code)
	}
	// non-member gets nothing from the vault
	stranger := &Actor{UserID: "usr_stranger_agent"}
	if code := agentErrCode(t, func() error { _, e := h.svc.AgentContext(ctx, stranger, vault.ID, req()); return e }()); code != domain.ErrCodeForbidden && code != domain.ErrCodeNotFound {
		t.Fatalf("non-member: want forbidden/not_found, got %s", code)
	}
}

func TestChangesSinceReplay(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	vault, err := h.svc.CreateSpace(ctx, h.actor, "FeedVault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	ids := seedNotes(t, h, ctx, vault.ID, "feed", 3)
	cur, err := h.svc.Docs.Document(ctx, ids[0])
	if err != nil || cur == nil {
		t.Fatal("seed doc missing")
	}
	if _, err := h.svc.SaveContent(ctx, h.actor, vault.ID, ids[0], cur.CurrentRevision, "v2 body\n", ""); err != nil {
		t.Fatal(err)
	}
	first, err := h.svc.ChangesSince(ctx, h.actor, vault.ID, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Changed) != 2 || first.NextCursor == "" {
		t.Fatalf("limit not honoured: %+v", first)
	}
	if _, err := h.svc.ChangesSince(ctx, h.actor, h.space, first.NextCursor, 100); err == nil {
		t.Fatal("change cursor accepted in another vault")
	}
	rest, err := h.svc.ChangesSince(ctx, h.actor, vault.ID, first.NextCursor, 100)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, c := range append(first.Changed, rest.Changed...) {
		seen[c.DocumentID+":"+c.Revision]++
	}
	for k, v := range seen {
		if v != 1 {
			t.Fatalf("event replayed across pages: %s", k)
		}
	}
	if len(rest.Changed) < 2 {
		t.Fatalf("tail missing: %+v", rest)
	}
	// trashing surfaces as a tombstone, not a change
	tDoc, err := h.svc.Docs.Document(ctx, ids[1])
	if err != nil || tDoc == nil {
		t.Fatal("doc vanished")
	}
	if err := h.svc.Trash(ctx, h.actor, vault.ID, ids[1], tDoc.CurrentRevision); err != nil {
		t.Fatal(err)
	}
	after, err := h.svc.ChangesSince(ctx, h.actor, vault.ID, rest.NextCursor, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range after.Tombstones {
		if c.DocumentID == ids[1] {
			found = true
		}
	}
	if !found {
		t.Fatalf("tombstone missing: %+v", after)
	}
}

func TestMemoryProposeIdempotencyAndProvenance(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	vault, err := h.svc.CreateSpace(ctx, h.actor, "MemVault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	other, err := h.svc.CreateSpace(ctx, h.actor, "MemOther", "shared")
	if err != nil {
		t.Fatal(err)
	}
	src, err := h.svc.CreateNote(ctx, h.actor, vault.ID, "src.md", "# Source\n\ndecision text\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := h.svc.CreateNote(ctx, h.actor, other.ID, "foreign.md", "elsewhere\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	p := &Proposal{Kind: "decision", Title: "Release gate policy", Content: "Gate must be green.",
		Sources: []Source{{DocumentID: src.ID}}, Confidence: 0.9}
	m1, err := h.svc.ProposeMemoryKey(ctx, h.actor, vault.ID, p, "k-1")
	if err != nil {
		t.Fatal(err)
	}
	m2, err := h.svc.ProposeMemoryKey(ctx, h.actor, vault.ID, p, "k-1")
	if err != nil {
		t.Fatal(err)
	}
	if m1.ID != m2.ID {
		t.Fatalf("replay must return the same memory: %s vs %s", m1.ID, m2.ID)
	}
	// exactly one backing note exists
	mems, err := h.svc.ListMemories(ctx, h.actor, vault.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range mems {
		if m.Title == "Release gate policy" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("proposal count = %d, want 1", n)
	}
	// same key, different body -> conflict
	p2 := &Proposal{Kind: "decision", Title: "Totally different", Content: "other",
		Sources: []Source{{DocumentID: src.ID}}}
	if code := agentErrCode(t, func() error { _, e := h.svc.ProposeMemoryKey(ctx, h.actor, vault.ID, p2, "k-1"); return e }()); code != domain.ErrCodeConflict {
		t.Fatalf("key reuse with new body: want conflict, got %s", code)
	}
	// cross-vault source rejected
	pb := &Proposal{Kind: "fact", Title: "Bad provenance", Content: "x",
		Sources: []Source{{DocumentID: foreign.ID}}}
	if code := agentErrCode(t, func() error { _, e := h.svc.ProposeMemoryKey(ctx, h.actor, vault.ID, pb, "k-2"); return e }()); code != domain.ErrCodeInvalid {
		t.Fatalf("foreign source: want invalid, got %s", code)
	}
	// revision pinned to the wrong document rejected
	pc := &Proposal{Kind: "fact", Title: "Bad pin", Content: "x",
		Sources: []Source{{DocumentID: src.ID, SourceRevision: "rev_nope"}}}
	if code := agentErrCode(t, func() error { _, e := h.svc.ProposeMemoryKey(ctx, h.actor, vault.ID, pc, "k-3"); return e }()); code != domain.ErrCodeInvalid {
		t.Fatalf("bogus revision: want invalid, got %s", code)
	}
	// valid pinned revision accepted and shown in detail
	pd := &Proposal{Kind: "fact", Title: "Pinned fact", Content: "y",
		Sources: []Source{{DocumentID: src.ID, SourceRevision: src.CurrentRevision}}}
	md, err := h.svc.ProposeMemoryKey(ctx, h.actor, vault.ID, pd, "k-4")
	if err != nil {
		t.Fatal(err)
	}
	det, err := h.svc.GetMemory(ctx, h.actor, vault.ID, md.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Sources) != 1 || det.Sources[0].SourceRevision != src.CurrentRevision {
		t.Fatalf("pinned revision missing: %+v", det.Sources)
	}
}

func TestMemoryStalenessSurfaced(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	vault, err := h.svc.CreateSpace(ctx, h.actor, "StaleVault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	src, err := h.svc.CreateNote(ctx, h.actor, vault.ID, "src.md", "# S\n\nv1\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	mem, err := h.svc.ProposeMemoryKey(ctx, h.actor, vault.ID, &Proposal{
		Kind: "fact", Title: "Stale probe", Content: "c",
		Sources: []Source{{DocumentID: src.ID, SourceRevision: src.CurrentRevision}}}, "k-s")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ReviewMemory(ctx, h.actor, vault.ID, mem.ID, true); err != nil {
		t.Fatal(err)
	}
	det, err := h.svc.GetMemory(ctx, h.actor, vault.ID, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if det.Stale {
		t.Fatalf("fresh accepted memory must not be stale: %+v", det.StaleReasons)
	}
	if _, err := h.svc.SaveContent(ctx, h.actor, vault.ID, src.ID, src.CurrentRevision, "# S\n\nv2 drifted\n", ""); err != nil {
		t.Fatal(err)
	}
	det, err = h.svc.GetMemory(ctx, h.actor, vault.ID, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !det.Stale || !strings.Contains(strings.Join(det.StaleReasons, "|"), "pinned revision") {
		t.Fatalf("drift not surfaced: %+v", det)
	}
}

func TestAgentPaginationBeyondSixtyAndHardJSONBudget(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	v, e := h.svc.CreateSpace(ctx, h.actor, "Large retrieval", "shared")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 85; i++ {
		_, e = h.svc.CreateNote(ctx, h.actor, v.ID, fmt.Sprintf("Research/note-%03d.md", i), "# Retrieval\n\nquantum \"evidence\" and unicode ąąąąąą\n", "", false)
		if e != nil {
			t.Fatal(e)
		}
	}
	_, e = h.svc.CreateNote(ctx, h.actor, v.ID, "OtherResearch/foreign.md", "quantum", "", false)
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 100; page++ {
		r, e := h.svc.AgentContext(ctx, h.actor, v.ID, &AgentContextRequest{Query: "quantum", Folder: "Research", MaxResults: 10, MaxBytes: 1800, Cursor: cursor, IncludeMemories: boolp(false)})
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(r)
		if len(b)+1 > 1800 || r.BudgetUsed.Bytes != len(b)+1 {
			t.Fatalf("JSON byte budget wrong: actual=%d reported=%d", len(b)+1, r.BudgetUsed.Bytes)
		}
		for _, it := range r.Items {
			if seen[it.DocumentID] {
				t.Fatal("duplicate across pages")
			}
			if !strings.HasPrefix(it.Path, "Research/") {
				t.Fatal("folder boundary escaped")
			}
			seen[it.DocumentID] = true
		}
		if r.NextCursor == "" {
			break
		}
		if r.NextCursor == cursor {
			t.Fatal("cursor did not progress")
		}
		cursor = r.NextCursor
	}
	if len(seen) != 85 {
		t.Fatalf("pagination stopped after %d of 85 documents", len(seen))
	}
}

func TestAgentSpecializedScopeIsolation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	a := *h.actor
	a.Key = &ports.APIKeyRecord{SpaceIDs: []string{h.space}, Scopes: []string{domain.ScopeDocumentsRead, domain.ScopeSearchRead}}
	if _, e := h.svc.AgentContext(ctx, &a, h.space, &AgentContextRequest{Query: "probe", IncludeMemories: boolp(true)}); e == nil {
		t.Fatal("memory read without scope permitted")
	}
	if _, e := h.svc.AgentContext(ctx, &a, h.space, &AgentContextRequest{Query: "probe", IncludeGraph: boolp(true)}); e == nil {
		t.Fatal("graph read without scope permitted")
	}
	if _, e := h.svc.Graph2(ctx, &a, h.space); e == nil {
		t.Fatal("Graph 2 read without graph scope permitted")
	}
	a.Key.Scopes = []string{domain.ScopeGraphRead}
	if _, e := h.svc.Graph2(ctx, &a, h.space); e != nil {
		t.Fatalf("graph-only key cannot read Graph 2: %v", e)
	}
	a.Key.SpaceIDs = []string{"spc_foreign"}
	if _, e := h.svc.Graph2(ctx, &a, h.space); e == nil {
		t.Fatal("unbound vault graph exposed")
	}
	a.Key.SpaceIDs = []string{h.space}
	a.Key.Scopes = []string{domain.ScopeMemoryRead}
	if _, e := h.svc.ListMemories(ctx, &a, h.space, nil); e != nil {
		t.Fatalf("memory-only key cannot read memory: %v", e)
	}
	a.Key.SpaceIDs = []string{"spc_foreign"}
	if _, e := h.svc.ListMemories(ctx, &a, h.space, nil); e == nil {
		t.Fatal("unbound vault memory exposed")
	}
}

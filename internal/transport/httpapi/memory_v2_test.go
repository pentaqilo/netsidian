package httpapi

// Transport coverage for the versioned agent memory surface: REST
// recall/remember/revise/consolidate/derived routes and the new MCP
// tools, including the guarded revision flow end to end.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
)

func TestMemoryV2RESTSurface(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, false)
	env.setup(t, "memop@x.io")
	ua, err := env.svc.Users.UserByEmail(ctx, "memop@x.io")
	if err != nil || ua == nil {
		t.Fatal("operator missing")
	}
	acc := &service.Actor{UserID: ua.ID, IsOperator: true}
	spaces, _, err := env.svc.Spaces.SpacesForUser(ctx, ua.ID)
	if err != nil || len(spaces) == 0 {
		t.Fatal("no operator space")
	}
	vault := spaces[0].ID
	_, secret, err := env.svc.CreateKey(ctx, acc, "mem", []string{vault},
		[]string{domain.ScopeDocumentsRead, domain.ScopeDocumentsWrite, domain.ScopeSearchRead}, 30)
	if err != nil || secret == "" {
		t.Fatal(err)
	}
	base := "/api/v1/spaces/" + vault + "/agent/memory"

	resp := env.callHdrs(t, "POST", base+"/remember", `{"key":"pref","text":"likes dark mode","kind":"preference","scopeId":"workspace-a","evidenceClass":"user-stated"}`, secret, map[string]string{"Idempotency-Key": "mem-1"})
	if resp.StatusCode != 200 {
		t.Fatalf("remember status %d", resp.StatusCode)
	}
	var remember service.MemoryReceipt
	if err := json.NewDecoder(resp.Body).Decode(&remember); err != nil || !remember.Verified {
		t.Fatalf("remember body: %v", err)
	}

	resp = env.callHdrs(t, "POST", base+"/recall", `{"kind":"preference"}`, secret, nil)
	var recall service.MemoryRecallResult
	if err := json.NewDecoder(resp.Body).Decode(&recall); err != nil || recall.Matched != 1 || recall.Entries[0].ScopeID != "workspace-a" {
		t.Fatalf("recall body: %v %+v", err, recall)
	}

	// Revise without expectedRevision must be 428 precondition_required.
	resp = env.callHdrs(t, "POST", base+"/revise", `{"key":"pref","action":"retract"}`, secret, nil)
	if resp.StatusCode != 428 {
		t.Fatalf("revise without expectedRevision: %d", resp.StatusCode)
	}
	resp = env.callHdrs(t, "POST", base+"/revise", `{"key":"pref","action":"supersede","text":"prefers auto dark mode","expectedRevision":"`+recall.IndexRevision+`"}`, secret, map[string]string{"Idempotency-Key": "mem-2"})
	if resp.StatusCode != 200 {
		t.Fatalf("revise status %d", resp.StatusCode)
	}
	var revise service.MemoryRevisionReceipt
	if err := json.NewDecoder(resp.Body).Decode(&revise); err != nil || revise.SupersededRevision != recall.IndexRevision || !revise.Verified {
		t.Fatalf("revise body: %v %+v", err, revise)
	}
	resp = env.callHdrs(t, "POST", base+"/recall", `{"key":"pref"}`, secret, nil)
	if err := json.NewDecoder(resp.Body).Decode(&recall); err != nil || recall.Entries[0].Text != "prefers auto dark mode" {
		t.Fatalf("corrected entry not recallable: %v %+v", err, recall.Entries)
	}

	// Consolidate dry-run then apply through REST.
	resp = env.callHdrs(t, "POST", base+"/consolidate", `{"operationId":"rest-plan","expectedRevision":"`+recall.IndexRevision+`","dryRun":true,"entries":[{"key":"pref","status":"disputed"}]}`, secret, nil)
	var dry service.MemoryConsolidationReceipt
	if err := json.NewDecoder(resp.Body).Decode(&dry); err != nil || dry.Outcome != "dry-run" {
		t.Fatalf("dry-run: %v %+v", err, dry)
	}
	resp = env.callHdrs(t, "POST", base+"/consolidate", `{"operationId":"rest-plan","expectedRevision":"`+recall.IndexRevision+`","entries":[{"key":"pref","status":"disputed"}]}`, secret, nil)
	var applied service.MemoryConsolidationReceipt
	if err := json.NewDecoder(resp.Body).Decode(&applied); err != nil || applied.Outcome != "applied" || !applied.Verified {
		t.Fatalf("consolidate: %v %+v", err, applied)
	}

	// Derived marker scan over REST.
	if _, err := env.svc.CreateNote(ctx, acc, vault, "derived-note.md", "---\nnetsidian:\n  derived:\n    sources:\n      - documentId: doc_missing\n        revision: rev_x\n---\nbody\n", "", false); err != nil {
		t.Fatal(err)
	}
	resp = env.callHdrs(t, "GET", base+"/derived", "", secret, nil)
	var derived service.MemoryDerivedResult
	if err := json.NewDecoder(resp.Body).Decode(&derived); err != nil || len(derived.Derived) != 1 || derived.Derived[0].Status != "broken" {
		t.Fatalf("derived: %v %+v", err, derived)
	}
}

func TestMemoryV2MCPReviseAndConsolidateTools(t *testing.T) {
	e, _, _, token := mcpFixture(t)
	session := connectMCP(t, e, token)
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, out := mcpCall(t, session, name, args)
		if result.IsError {
			t.Fatalf("%s: %v", name, out)
		}
		return out
	}
	call("netsidian_memory_remember", map[string]any{"key": "topic.fact", "text": "original claim", "kind": "fact", "idempotencyKey": "mcp-mem-1"})
	recall := call("netsidian_memory_recall", map[string]any{"key": "topic.fact"})
	revision := recall["indexRevision"].(string)
	if revision == "" {
		t.Fatalf("recall missing indexRevision: %+v", recall)
	}
	revise := call("netsidian_memory_revise", map[string]any{"key": "topic.fact", "action": "supersede", "text": "corrected claim", "expectedRevision": revision, "reason": "user corrected", "idempotencyKey": "mcp-revise-1"})
	if revise["supersededRevision"] != revision || revise["verified"] != true {
		t.Fatalf("revise receipt: %+v", revise)
	}
	created := call("netsidian_document_create", map[string]any{"path": "notes/summary.md", "markdown": "old", "idempotencyKey": "mcp-target-1"})
	evidence := call("netsidian_document_create", map[string]any{"path": "notes/evidence.md", "markdown": "Independent source", "idempotencyKey": "mcp-evidence"})
	consolidate := call("netsidian_memory_consolidate", map[string]any{
		"operationId": "mcp-plan", "expectedRevision": revise["indexRevision"], "sources": []map[string]any{{"documentId": evidence["documentId"], "expectedRevision": evidence["revision"]}},
		"targets": []any{map[string]any{"documentId": created["documentId"], "expectedRevision": created["revision"], "markdown": "merged", "adoptManagedSection": true}},
		"entries": []any{map[string]any{"key": "topic.fact", "status": "disputed"}},
	})
	if consolidate["outcome"] != "applied" || consolidate["verified"] != true {
		t.Fatalf("consolidate: %+v", consolidate)
	}
	// Connect exposes the shared policy hook for generated guidance.
	_, connection := mcpCall(t, session, "netsidian_connect", map[string]any{})
	memory, ok := connection["memory"].(map[string]any)
	if !ok || memory["policyVersion"] != service.MemoryPolicyVersion || memory["schemaVersion"] != float64(service.MemorySchemaVersion) {
		t.Fatalf("connect memory policy hook missing: %+v", connection["memory"])
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/pentaqilo/netsidian/internal/service"
)

// Isolated transport execution, not an LLM quality/latency benchmark.
func TestMemoryFreshSessionReviewWorkflow(t *testing.T) {
	e, actor, vault, token := mcpFixture(t)
	ctx := context.Background()
	first := connectMCP(t, e, token)
	results := []map[string]any{}
	call := func(sessionName string, name string, args map[string]any) map[string]any {
		t.Helper()
		session := first
		if sessionName == "fresh" {
			session = connectMCP(t, e, token)
		}
		result, out := mcpCall(t, session, name, args)
		if result.IsError {
			t.Fatalf("%s: %+v", name, out)
		}
		if (name == "netsidian_memory_revise" || name == "netsidian_memory_consolidate") && result.StructuredContent == nil {
			t.Fatal("workflow receipt lacks negotiated structured content", name)
		}
		data, _ := json.Marshal(result)
		results = append(results, map[string]any{"tool": name, "serializedResultBytes": len(data), "result": result})
		return out
	}
	doc := call("initial", "netsidian_document_create", map[string]any{"path": "Projects/Offline/decision.md", "markdown": "# Accepted decision\nUse SQLite for offline work.", "idempotencyKey": "fresh-decision"})
	remembered := call("initial", "netsidian_memory_remember", map[string]any{"key": "project.storage", "text": "Use SQLite for offline work.", "kind": "decision", "evidenceClass": "sourced", "scope": "project", "scopeId": "offline-project", "refs": []string{doc["documentId"].(string)}, "sourceRefs": []map[string]any{{"target": doc["documentId"], "kind": "document", "revision": doc["revision"]}}, "idempotencyKey": "fresh-memory"})
	before, _ := e.svc.Docs.MaxEventSeq(ctx, vault)
	recalled := call("fresh", "netsidian_memory_recall", map[string]any{"key": "project.storage", "status": "active", "scopeId": "offline-project", "maxBytes": 2048})
	entries := recalled["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["text"] != "Use SQLite for offline work." {
		t.Fatal(recalled)
	}
	call("fresh", "netsidian_document_read", map[string]any{"documentId": doc["documentId"], "maxBytes": 2048})
	after, _ := e.svc.Docs.MaxEventSeq(ctx, vault)
	if before != after {
		t.Fatal("recall/reflection evidence reads wrote knowledge")
	}
	correction := map[string]any{"key": "project.storage", "action": "supersede", "text": "SQLite is the accepted offline database; deployment conditions remain explicit.", "reason": "Clarify scope without broadening evidence.", "expectedRevision": remembered["indexRevision"], "idempotencyKey": "fresh-correction"}
	receipt := call("fresh", "netsidian_memory_revise", correction)
	replay := call("fresh", "netsidian_memory_revise", correction)
	if receipt["indexRevision"] != replay["indexRevision"] || receipt["verified"] != true {
		t.Fatal("correction replay changed revision", receipt, replay)
	}
	body := "SQLite was accepted for this offline project, based on the linked decision."
	plan := map[string]any{"operationId": "fresh-consolidation", "sources": []map[string]any{{"documentId": doc["documentId"], "expectedRevision": doc["revision"]}}, "targets": []map[string]any{{"path": "Projects/Offline/summary.md", "markdown": body}}, "dryRun": true}
	before, _ = e.svc.Docs.MaxEventSeq(ctx, vault)
	call("fresh", "netsidian_memory_consolidate", plan)
	after, _ = e.svc.Docs.MaxEventSeq(ctx, vault)
	if before != after {
		t.Fatal("dry-run wrote knowledge")
	}
	plan["dryRun"] = false
	applied := call("fresh", "netsidian_memory_consolidate", plan)
	if applied["verified"] != true {
		t.Fatal(applied)
	}
	target, err := e.svc.LookupDocument(ctx, actor, vault, "Projects/Offline/summary.md")
	if err != nil {
		t.Fatal(err)
	}
	status, err := e.svc.DerivedStatusWithInput(ctx, actor, vault, service.MemoryDerivedInput{DocumentID: target.ID})
	if err != nil || len(status.Derived) != 1 || status.Derived[0].Status != "current" {
		t.Fatalf("committed summary not current: %+v %v", status, err)
	}
	_, err = e.svc.SaveContent(ctx, actor, vault, doc["documentId"].(string), doc["revision"].(string), "# Revised source\nConditions changed.", "fresh-source-change")
	if err != nil {
		t.Fatal(err)
	}
	status, err = e.svc.DerivedStatusWithInput(ctx, actor, vault, service.MemoryDerivedInput{DocumentID: target.ID})
	if err != nil || status.Derived[0].Status != "stale" {
		t.Fatalf("changed source did not stale summary: %+v %v", status, err)
	}
	total := 0
	for _, r := range results {
		total += r["serializedResultBytes"].(int)
	}
	t.Logf("Fixed fresh-session workflow: %d calls, %d cumulative serialized result bytes; reflection/dry-run zero writes", len(results), total)
	if path := os.Getenv("NETSIDIAN_TEST_WORKFLOW_OUTPUT"); path != "" {
		data, _ := json.Marshal(results)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

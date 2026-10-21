package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pentaqilo/netsidian/internal/domain"
)

func TestMCPWorkflowReviewTypedMemoryAndDocuments(t *testing.T) {
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
	created := call("netsidian_document_create", map[string]any{"path": "Projects/Typed.md", "markdown": "# Project", "idempotencyKey": "typed-create"})
	search := call("netsidian_search", map[string]any{"folder": "Projects", "maxBytes": 1024})
	encodedSearch, _ := json.Marshal(search)
	if len(encodedSearch) > 1024 || len(search["items"].([]any)) != 1 {
		t.Fatal("bounded search failed", search)
	}
	read := call("netsidian_document_read", map[string]any{"documentId": created["documentId"], "maxBytes": 1024})
	if read["markdown"] != "# Project" {
		t.Fatal(read)
	}
	call("netsidian_document_update", map[string]any{"documentId": created["documentId"], "revision": read["revision"], "markdown": "# Revised", "idempotencyKey": "typed-update"})
	receipt := call("netsidian_memory_remember", map[string]any{"key": "preferences.address", "text": "jasnie panie", "idempotencyKey": "typed-preference"})
	receiptJSON, _ := json.Marshal(receipt)
	t.Logf("Verified preference receipt: %d JSON bytes", len(receiptJSON))
	if len(receiptJSON) > 1024 {
		t.Fatal("preference receipt unexpectedly large")
	}
	_, connection := mcpCall(t, session, "netsidian_connect", map[string]any{})
	connectionJSON, _ := json.Marshal(connection)
	t.Logf("Authenticated connect: %d JSON bytes", len(connectionJSON))
	call("netsidian_memory_remember", map[string]any{"key": "projects.site", "text": "See current project", "scope": "project", "refs": []string{"Projects/Typed.md"}, "detail": "Long details kept outside index.", "idempotencyKey": "typed-project"})
	recall := call("netsidian_memory_recall", map[string]any{"key": "preferences.address"})
	entries := recall["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["text"] != "jasnie panie" {
		t.Fatal(recall)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "netsidian_document_update", Arguments: map[string]any{"documentId": created["documentId"], "revision": read["revision"], "content": "WRONG"}})
	if err == nil && !result.IsError {
		t.Fatal("misspelled field accepted")
	}
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "netsidian_memory_remember", Arguments: map[string]any{"key": "unexpected", "text": "test", "unknown": true}})
	if err == nil && !result.IsError {
		t.Fatal("unknown memory field accepted")
	}
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	total, _ := json.Marshal(tools.Tools)
	t.Logf("MCP tool definitions: %d bytes for %d tools", len(total), len(tools.Tools))
}

func TestMCPWorkflowReviewReadOnlyAndVaultSelection(t *testing.T) {
	e, owner, space, _ := mcpFixture(t)
	ctx := context.Background()
	_, reader, err := e.svc.CreateKey(ctx, owner, "typed reader", []string{space}, []string{domain.ScopeDocumentsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	session := connectMCP(t, e, reader)
	result, out := mcpCall(t, session, "netsidian_memory_remember", map[string]any{"key": "denied", "text": "No write"})
	if !result.IsError {
		t.Fatal(out)
	}
	if _, err = e.svc.LookupDocument(ctx, owner, space, "agent-memory/index.md"); err == nil {
		t.Fatal("denied write created index")
	}
	other, err := e.svc.CreateSpace(ctx, owner, "Other", "shared")
	if err != nil {
		t.Fatal(err)
	}
	_, both, err := e.svc.CreateKey(ctx, owner, "two vaults", []string{space, other.ID}, []string{domain.ScopeDocumentsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	two := connectMCP(t, e, both)
	result, out = mcpCall(t, two, "netsidian_memory_recall", map[string]any{})
	if !result.IsError {
		t.Fatal("ambiguous vault selected", out)
	}
	result, out = mcpCall(t, two, "netsidian_memory_recall", map[string]any{"vaultId": space})
	if result.IsError {
		t.Fatal(out)
	}
}

func TestMCPWorkflowReviewWriteOnlyCredentialCannotMaintainIndex(t *testing.T) {
	e, owner, space, _ := mcpFixture(t)
	_, token, err := e.svc.CreateKey(context.Background(), owner, "write only", []string{space}, []string{domain.ScopeDocumentsWrite}, 1)
	if err != nil {
		t.Fatal(err)
	}
	session := connectMCP(t, e, token)
	_, connection := mcpCall(t, session, "netsidian_connect", map[string]any{})
	if connection["memory"].(map[string]any)["writeAllowed"] != false {
		t.Fatal("index maintenance needs both read and write")
	}
	result, out := mcpCall(t, session, "netsidian_memory_remember", map[string]any{"key": "no-read", "text": "Denied"})
	if !result.IsError {
		t.Fatal(out)
	}
}

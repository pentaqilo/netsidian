package httpapi

// Coverage for the versioned portable memory policy surface: the MCP
// tool, the static resources and the public REST route must expose the
// same policy version and text as the generated package.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pentaqilo/netsidian/internal/memorypolicy"
	"github.com/pentaqilo/netsidian/internal/service"
)

func TestMemoryPolicyToolResourcesAndRoute(t *testing.T) {
	e, _, _, token := mcpFixture(t)
	session := connectMCP(t, e, token)

	result, out := mcpCall(t, session, "netsidian_memory_policy", map[string]any{"part": "netsidian"})
	t.Logf("Negotiated MCP protocol: %s", session.InitializeResult().ProtocolVersion)
	if result.StructuredContent == nil {
		t.Fatal("policy tool must provide typed structured metadata")
	}
	structured, _ := json.Marshal(result.StructuredContent)
	var meta map[string]any
	_ = json.Unmarshal(structured, &meta)
	if meta["policyVersion"] != memorypolicy.Version || meta["text"] != nil {
		t.Fatal("policy metadata must identify version without duplicating body", meta)
	}
	if _, err := memoryPolicyContent("core", 1024); err == nil {
		t.Fatal("indivisible policy section must respect serialized budget")
	}
	if result.IsError {
		t.Fatalf("policy tool errored: %+v", out)
	}
	if out["policyVersion"] != service.MemoryPolicyVersion || out["part"] != "netsidian" {
		t.Fatalf("policy tool identity: %+v", out)
	}
	if text, _ := out["text"].(string); text != memorypolicy.Assets["references/netsidian.md"] {
		t.Fatal("policy tool text diverges from the generated policy package")
	}
	if _, err := memoryPolicyContent("nonexistent-section", 0); err == nil {
		t.Fatal("unknown policy section must be rejected")
	}

	listed, err := session.ListResources(context.Background(), &mcp.ListResourcesParams{})
	if err != nil {
		t.Fatal(err)
	}
	want := "netsidian://memory-policy/" + memorypolicy.Version + "/core"
	found := false
	for _, r := range listed.Resources {
		if r.URI == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("versioned policy resource not advertised: %s", want)
	}
	read, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: want})
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Contents) != 1 || read.Contents[0].Text != memorypolicy.Assets["SKILL.md"] {
		t.Fatal("policy resource content diverges from the generated policy package")
	}

	resp := e.callHdrs(t, "GET", "/api/v1/agent/memory-policy?part=core", "", token, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("memory-policy route status %d", resp.StatusCode)
	}
	var rest memoryPolicyResult
	if err := json.NewDecoder(resp.Body).Decode(&rest); err != nil || rest.PolicyVersion != service.MemoryPolicyVersion || rest.Text != memorypolicy.Assets["SKILL.md"] {
		t.Fatalf("memory-policy route body: %v %+v", err, rest)
	}
	resp = e.callHdrs(t, "GET", "/api/v1/agent/memory-policy?part=bogus", "", token, nil)
	if resp.StatusCode < 400 {
		t.Fatalf("unknown section must fail, got %d", resp.StatusCode)
	}
}

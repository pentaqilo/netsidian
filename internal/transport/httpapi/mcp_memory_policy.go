package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/memorypolicy"
)

var memoryPolicyParts = map[string]string{
	"core": "SKILL.md", "netsidian": "references/netsidian.md", "local": "references/local.md",
	"remote": "references/remote.md", "schema": "references/schema.md", "examples": "references/examples.md", "recovery": "references/recovery.md",
	"local-helper": "scripts/local_memory.py",
}
var memoryPolicyPartNames = []string{"core", "netsidian", "local", "remote", "schema", "examples", "recovery", "local-helper"}

type memoryPolicyInput struct {
	Part     string `json:"part,omitempty" jsonschema:"core (default), netsidian, local, remote, schema, examples, recovery or local-helper; load only needed sections"`
	MaxBytes int    `json:"maxBytes,omitempty" jsonschema:"Serialized response budget 1024..32768, default 8192"`
}
type memoryPolicyMetadata struct {
	PolicyVersion string   `json:"policyVersion"`
	Part          string   `json:"part"`
	Parts         []string `json:"parts"`
}
type memoryPolicyResult struct {
	PolicyVersion string   `json:"policyVersion"`
	Part          string   `json:"part"`
	Text          string   `json:"text"`
	Parts         []string `json:"parts"`
}

func memoryPolicyContent(part string, maxBytes int) (*memoryPolicyResult, error) {
	if part == "" {
		part = "core"
	}
	name, ok := memoryPolicyParts[part]
	if !ok {
		return nil, domain.Invalid("unknown memory policy section")
	}
	if maxBytes == 0 {
		maxBytes = 8192
	}
	if maxBytes < 1024 || maxBytes > 32768 {
		return nil, domain.Invalid("maxBytes must be 1024..32768")
	}
	out := &memoryPolicyResult{PolicyVersion: memorypolicy.Version, Part: part, Text: memorypolicy.Assets[name], Parts: memoryPolicyPartNames}
	data, _ := json.Marshal(out)
	if len(data) > maxBytes {
		return nil, domain.TooLarge("policy section exceeds maxBytes; request a larger budget")
	}
	return out, nil
}
func (s *Server) handleMemoryPolicy(w http.ResponseWriter, r *http.Request) {
	budget := 0
	if text := r.URL.Query().Get("maxBytes"); text != "" {
		var err error
		budget, err = strconv.Atoi(text)
		if err != nil {
			writeErr(w, domain.Invalid("maxBytes must be an integer"))
			return
		}
	}
	out, err := memoryPolicyContent(r.URL.Query().Get("part"), budget)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// Static versioned instructions contain no credential, vault metadata or user
// data. They are equally available to resources clients and tools-only clients.
func addMCPMemoryPolicy(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "netsidian_memory_policy", Description: "Read a section of the versioned portable memory policy. Static instructions only; no vault reads or writes.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: mcpBoolPtr(false)}}, func(ctx context.Context, r *mcp.CallToolRequest, in memoryPolicyInput) (*mcp.CallToolResult, *memoryPolicyMetadata, error) {
		out, err := memoryPolicyContent(in.Part, in.MaxBytes)
		if err != nil {
			return mcpToolError(err), nil, nil
		}
		return mcpText(out, false), &memoryPolicyMetadata{PolicyVersion: out.PolicyVersion, Part: out.Part, Parts: out.Parts}, nil
	})
	for _, part := range memoryPolicyPartNames {
		part := part
		uri := "netsidian://memory-policy/" + memorypolicy.Version + "/" + part
		content := memorypolicy.Assets[memoryPolicyParts[part]]
		mime := "text/markdown"
		if part == "local-helper" {
			mime = "text/x-python"
		}
		server.AddResource(&mcp.Resource{URI: uri, Name: "Agent memory policy: " + part, MIMEType: mime, Size: int64(len(content))}, func(ctx context.Context, r *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: mime, Text: content}}}, nil
		})
	}
}

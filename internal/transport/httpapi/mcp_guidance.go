package httpapi

import (
	"context"
	"strings"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
)

const mcpReadGuide = "Vaults: {space} must be the vault ID from netsidian_connect vaults[].id or GET /api/v1/spaces, never its name or slug. Memory: focused tools use markdown consistently. REST creates documents with {path, content} and updates content with {markdown} and the current If-Match revision. An explicit empty markdown string clears the document. A verified memory receipt is sufficient; generic document writes need targeted read-back before claiming persistence. Preserve the user’s preference wording verbatim; do not translate or correct names or honorifics. Documents: {doc} is an ID, never a file path. Read content directly with GET /api/v1/spaces/{space}/documents/by-path and query.path, or resolve GET /api/v1/spaces/{space}/documents/lookup with query.path then read GET /api/v1/spaces/{space}/documents/{doc} using the returned id. Maps: GET /api/v1/spaces/{space}/mindmaps/mindmap accepts the folder path or its mindmap.md file in query.path; use the returned folder path for mutations."

func (s *Server) mcpCredential(ctx context.Context) (*service.AgentWhoami, error) {
	actor := desktopMCPActor(ctx)
	if actor == nil {
		actor, _ = ctx.Value(actorKey).(*service.Actor)
	}
	if actor == nil {
		return nil, domain.Unauthenticated("authenticated MCP identity required")
	}
	return s.Svc.AgentWhoami(ctx, actor)
}

func mcpMemoryWritable(who *service.AgentWhoami) bool {
	if who == nil {
		return false
	}
	for _, vault := range who.Vaults {
		read, write := false, false
		for _, op := range vault.AllowedOperations {
			if op == "read" {
				read = true
			}
			if op == "create" || op == "edit" {
				write = true
			}
		}
		if read && write {
			return true
		}
	}
	return false
}

// Catalog availability describes transport compatibility, intersected with a
// known read-only credential. Fine-grained rights remain in vault operations
// and scopes; every call still goes through the authoritative REST handlers.
func mcpCredentialOperation(op mcpOperation, who *service.AgentWhoami) mcpOperation {
	if !op.Available || who == nil {
		return op
	}
	if who.Auth.ReadOnly && !mcpReadOperation(op) {
		op.Available = false
		op.Reason = "this credential is read-only; writes require the matching scope and vault role"
		return op
	}
	required := mcpKnowledgeGrants(op)
	if len(required) == 0 {
		return op
	}
	for _, vault := range who.Vaults {
		complete := true
		for _, want := range required {
			found := false
			for _, have := range vault.AllowedOperations {
				if have == want {
					found = true
					break
				}
			}
			if !found {
				complete = false
				break
			}
		}
		if complete {
			return op
		}
	}
	op.Available = false
	op.Reason = "no accessible vault grants the required operations: " + strings.Join(required, ", ") + "; inspect netsidian_connect vaults.allowedOperations and auth.scopes"
	return op
}

// These are permission prerequisites for the core knowledge surfaces, not a
// substitute for checking the target vault, revision and request body at call time.
func mcpKnowledgeGrants(op mcpOperation) []string {
	if strings.Contains(op.Path, "/agent/memory/") {
		if mcpReadOperation(op) {
			return []string{"read"}
		}
		return []string{"read", "edit"}
	}
	if strings.Contains(op.Path, "/documents") {
		if op.Method == "GET" || op.Method == "HEAD" {
			if strings.Contains(op.Path, "/revisions") {
				return []string{"read", "history_read"}
			}
			return []string{"read"}
		}
		if strings.HasSuffix(op.Path, "/trash") {
			return []string{"trash"}
		}
		if strings.HasSuffix(op.Path, "/restore") {
			return []string{"restore"}
		}
		if op.Method == "POST" && strings.HasSuffix(op.Path, "/documents") {
			return []string{"create"}
		}
		return []string{"edit"}
	}
	if strings.Contains(op.Path, "/mindmaps") || strings.Contains(op.Path, "/boards") {
		if mcpReadOperation(op) {
			return []string{"read"}
		}
		return []string{"read", "edit"}
	}
	if strings.Contains(op.Path, "/attachments") {
		if op.Method == "DELETE" {
			return []string{"trash"}
		}
		if mcpReadOperation(op) {
			return []string{"attachments_read"}
		}
		return []string{"attachments"}
	}
	return nil
}

func documentIDParameterHint(name, value string) string {
	if name == "doc" && (strings.ContainsAny(value, "/\\") || strings.HasSuffix(strings.ToLower(value), ".md")) {
		return documentReadGuide
	}
	return ""
}

func memoryVaultCapabilities(who *service.AgentWhoami) map[string]map[string]bool {
	result := map[string]map[string]bool{}
	if who == nil {
		return result
	}
	for _, vault := range who.Vaults {
		read, write := false, false
		for _, op := range vault.AllowedOperations {
			if op == "read" {
				read = true
			}
			if op == "edit" {
				write = true
			}
		}
		result[vault.ID] = map[string]bool{"recall": read, "remember": read && write, "revise": read && write, "consolidate": read && write, "derivedStatus": read}
	}
	return result
}

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func TestMCPMapReaderConfigPathAliasAndLiteralFolder(t *testing.T) {
	e, owner, space, _ := mcpFixture(t)
	ctx := context.Background()
	for _, folder := range []string{"Maps/Launch planning", "Literal/mindmap.md"} {
		if _, err := e.svc.CreateMindMap(ctx, owner, space, folder, "Test map", ""); err != nil {
			t.Fatal(err)
		}
	}
	_, secret, err := e.svc.CreateKey(ctx, owner, "isolated map reader", []string{space}, []string{domain.ScopeDocumentsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	session := connectMCP(t, e, secret)
	for _, tc := range []struct{ query, want string }{
		{"Maps/Launch planning", "Maps/Launch planning"},
		{"Maps/Launch planning/mindmap.md", "Maps/Launch planning"},
		{"Literal/mindmap.md", "Literal/mindmap.md"},
		{"Literal/mindmap.md/mindmap.md", "Literal/mindmap.md"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			r := e.call(t, http.MethodGet, "/api/v1/spaces/"+space+"/mindmaps/mindmap?path="+url.QueryEscape(tc.query), "", "", secret)
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if r.StatusCode != 200 || body["path"] != tc.want {
				t.Fatalf("status=%d body=%v", r.StatusCode, body)
			}
			result, response := mcpCall(t, session, "netsidian_read", mcpAPIInput{Operation: "GET /api/v1/spaces/{space}/mindmaps/mindmap", PathParameters: map[string]string{"space": space}, Query: map[string]any{"path": tc.query}})
			if result.IsError || response["status"] != float64(200) {
				t.Fatalf("MCP alias read: %v", response)
			}
		})
	}
	result, denied := mcpCall(t, session, "netsidian_write", mcpAPIInput{Operation: "POST /api/v1/spaces/{space}/documents", PathParameters: map[string]string{"space": space}, Body: map[string]string{"path": "agent-memory/denied.md", "content": "must not be saved"}})
	if !result.IsError {
		t.Fatalf("read-only key authorized a write: %v", denied)
	}
	if doc, _ := e.svc.LookupDocument(ctx, owner, space, "agent-memory/denied.md"); doc != nil {
		t.Fatal("read-only request saved content")
	}
}

func TestMCPMistakenDocumentPathHasLookupGuidance(t *testing.T) {
	e, _, space, secret := mcpFixture(t)
	session := connectMCP(t, e, secret)
	result, out := mcpCall(t, session, "netsidian_read", mcpAPIInput{Operation: "GET /api/v1/spaces/{space}/documents/{doc}", PathParameters: map[string]string{"space": space, "doc": "Research/Interview notes.md"}})
	body, _ := json.Marshal(out)
	if !result.IsError || !strings.Contains(string(body), "documents/lookup") {
		t.Fatalf("expected actionable ID/path guidance: %s", body)
	}
}

func TestMCPConnectCapabilitiesArePerCredentialAndMemoryAware(t *testing.T) {
	e, owner, space, writer := mcpFixture(t)
	_, reader, err := e.svc.CreateKey(context.Background(), owner, "reader", []string{space}, []string{domain.ScopeDocumentsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, attachmentWriter, err := e.svc.CreateKey(context.Background(), owner, "attachments only", []string{space}, []string{domain.ScopeAttachmentsWrite}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token            string
		readOnly, memory bool
	}{{writer, false, true}, {reader, true, false}, {attachmentWriter, false, false}, {reader, true, false}, {writer, false, true}} {
		session := connectMCP(t, e, tc.token)
		result, setup := mcpCall(t, session, "netsidian_connect", map[string]any{})
		if result.IsError || setup["connected"] != true || setup["readOnly"] != tc.readOnly {
			t.Fatalf("bad capability summary: %v", setup)
		}
		memory := setup["memory"].(map[string]any)
		if memory["writeAllowed"] != tc.memory || memory["requiredWriteScope"] != domain.ScopeDocumentsWrite {
			t.Fatalf("bad memory capability: %v", memory)
		}
		raw, _ := json.Marshal(setup)
		if strings.Contains(string(raw), tc.token) {
			t.Fatal("credential leaked into MCP output")
		}
		_, op := mcpCall(t, session, "netsidian_discover_api", map[string]any{"operation": "POST /api/v1/spaces/{space}/documents"})
		if !tc.memory && op["available"] != false {
			t.Fatalf("credential without document write rights advertised document writes: %v", op)
		}
	}
}

func TestHTTPReadByPathPermissionsAndValidation(t *testing.T) {
	e, owner, space, _ := mcpFixture(t)
	ctx := context.Background()
	doc, err := e.svc.CreateNote(ctx, owner, space, "Research/Interview notes.md", "# Research\n\nReadable.", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, reader, err := e.svc.CreateKey(ctx, owner, "reader", []string{space}, []string{domain.ScopeDocumentsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/spaces/" + space + "/documents/"
	r := e.call(t, "GET", base+"by-path?path="+url.QueryEscape(doc.Path), "", "", reader)
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 200 || body["documentId"] != doc.ID || body["canEdit"] != false || r.Header.Get("ETag") == "" {
		t.Fatalf("bad path read: %d %v", r.StatusCode, body)
	}
	session := connectMCP(t, e, reader)
	result, read := mcpCall(t, session, "netsidian_read", mcpAPIInput{Operation: "GET /api/v1/spaces/{space}/documents/by-path", PathParameters: map[string]string{"space": space}, Query: map[string]any{"path": doc.Path}})
	if result.IsError || read["status"] != float64(200) {
		t.Fatalf("path convenience unavailable in MCP: %v", read)
	}
	for _, path := range []string{"", "../escape.md", "/absolute.md", "bad\\path.md", "%2e%2e/escape.md"} {
		if r := e.call(t, "GET", base+"by-path?path="+url.QueryEscape(path), "", "", reader); r.StatusCode != 400 {
			t.Fatalf("invalid path %q status=%d", path, r.StatusCode)
		}
	}
	if r := e.call(t, "GET", base+"Interview.md", "", "", reader); r.StatusCode != 400 {
		t.Fatalf("wrong leaf path status=%d", r.StatusCode)
	}
	if r := e.call(t, "GET", base+"by-path?path="+url.QueryEscape(doc.Path), "", "", ""); r.StatusCode != 401 {
		t.Fatalf("anonymous content read status=%d", r.StatusCode)
	}
	other, err := e.svc.CreateSpace(ctx, owner, "Other bound vault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	_, otherKey, err := e.svc.CreateKey(ctx, owner, "other vault", []string{other.ID}, []string{domain.ScopeDocumentsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r := e.call(t, "GET", base+"by-path?path="+url.QueryEscape(doc.Path), "", "", otherKey); r.StatusCode != 403 {
		t.Fatalf("unbound credential read status=%d", r.StatusCode)
	}
	_, noRead, err := e.svc.CreateKey(ctx, owner, "graph only", []string{space}, []string{domain.ScopeGraphRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r := e.call(t, "GET", base+"by-path?path="+url.QueryEscape(doc.Path), "", "", noRead); r.StatusCode != 403 {
		t.Fatalf("missing document scope status=%d", r.StatusCode)
	}
}

func TestMCPMapAliasesRejectInvalidAndUnboundReads(t *testing.T) {
	e, owner, space, _ := mcpFixture(t)
	ctx := context.Background()
	if _, err := e.svc.CreateMindMap(ctx, owner, space, "Maps/Plan", "Plan", ""); err != nil {
		t.Fatal(err)
	}
	_, reader, err := e.svc.CreateKey(ctx, owner, "reader", []string{space}, []string{domain.ScopeDocumentsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	root := "/api/v1/spaces/" + space + "/mindmaps/mindmap?path="
	for _, path := range []string{"../escape/mindmap.md", "/absolute/mindmap.md", "Maps\\Plan\\mindmap.md", "Maps/%2e%2e/mindmap.md"} {
		r := e.call(t, "GET", root+url.QueryEscape(path), "", "", reader)
		if r.StatusCode != 400 {
			t.Fatalf("invalid map path %q status=%d", path, r.StatusCode)
		}
	}
	other, err := e.svc.CreateSpace(ctx, owner, "Other map vault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	_, foreign, err := e.svc.CreateKey(ctx, owner, "foreign", []string{other.ID}, []string{domain.ScopeDocumentsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r := e.call(t, "GET", root+url.QueryEscape("Maps/Plan/mindmap.md"), "", "", foreign); r.StatusCode != 403 {
		t.Fatalf("unbound map alias read: %d", r.StatusCode)
	}
	if r := e.call(t, "GET", root+url.QueryEscape("Maps/Plan/mindmap.md"), "", "", ""); r.StatusCode != 401 {
		t.Fatalf("anonymous map alias read: %d", r.StatusCode)
	}
}

func TestDocumentUpdateRequiresExplicitMarkdownWithoutDataLoss(t *testing.T) {
	e, owner, space, secret := mcpFixture(t)
	ctx := context.Background()
	original := "Address me as jasnie panie."
	doc, err := e.svc.CreateNote(ctx, owner, space, "agent-memory/preferences.md", original, "", false)
	if err != nil {
		t.Fatal(err)
	}
	before, err := e.svc.ReadNote(ctx, owner, space, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := e.svc.Revisions(ctx, owner, space, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	route := "/api/v1/spaces/" + space + "/documents/" + doc.ID + "/content"
	for _, body := range []string{`{"content":"Mirror my language."}`, `{}`, `null`, `{"markdown":null}`, `{"markdown":123}`} {
		r := e.callHdrs(t, "PUT", route, body, secret, map[string]string{"If-Match": before.Revision})
		if r.StatusCode != 400 {
			t.Fatalf("body=%s status=%d", body, r.StatusCode)
		}
	}
	session := connectMCP(t, e, secret)
	result, out := mcpCall(t, session, "netsidian_write", mcpAPIInput{Operation: "PUT /api/v1/spaces/{space}/documents/{doc}/content", PathParameters: map[string]string{"space": space, "doc": doc.ID}, IfMatch: before.Revision, IdempotencyKey: "wrong-memory-payload", Body: map[string]string{"content": "Mirror my language."}})
	if !result.IsError {
		t.Fatalf("incorrect memory payload accepted: %v", out)
	}
	after, err := e.svc.ReadNote(ctx, owner, space, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterRevisions, err := e.svc.Revisions(ctx, owner, space, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Markdown != original || after.Revision != before.Revision || len(afterRevisions) != len(revisions) {
		t.Fatal("invalid payload changed document or revision history")
	}
	wanted := original + "\nMirror my language."
	for i, text := range []string{wanted, ""} {
		payload, _ := json.Marshal(map[string]string{"markdown": text})
		r := e.callHdrs(t, "PUT", route, string(payload), secret, map[string]string{"If-Match": after.Revision})
		if r.StatusCode != 200 {
			t.Fatalf("valid update %d status=%d", i, r.StatusCode)
		}
		after, err = e.svc.ReadNote(ctx, owner, space, doc.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Markdown != text {
			t.Fatalf("read-back mismatch: %q", after.Markdown)
		}
	}
}

package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
)

type mcpBearerTransport struct{ token string }

func (t mcpBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header = r.Header.Clone()
	if t.token != "" {
		clone.Header.Set("Authorization", "Bearer "+t.token)
	}
	return http.DefaultTransport.RoundTrip(clone)
}
func connectMCP(t *testing.T, e *testEnv, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "netsidian-acceptance", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpBearerTransport{token}}, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}
func mcpCall(t *testing.T, session *mcp.ClientSession, name string, args any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if out == nil {
		t.Fatal("tool result did not contain JSON")
	}
	return result, out
}
func mcpFixture(t *testing.T) (*testEnv, *service.Actor, string, string) {
	t.Helper()
	e := newTestEnv(t, false)
	e.setup(t, "mcp@example.test")
	u, _ := e.svc.Users.UserByEmail(context.Background(), "mcp@example.test")
	a := &service.Actor{UserID: u.ID, IsOperator: true}
	spaces, _, _ := e.svc.Spaces.SpacesForUser(context.Background(), u.ID)
	_, token, err := e.svc.CreateKey(context.Background(), a, "MCP acceptance", []string{spaces[0].ID}, []string{domain.ScopeDocumentsRead, domain.ScopeDocumentsWrite, domain.ScopeSearchRead, domain.ScopeGraphRead}, 7)
	if err != nil {
		t.Fatal(err)
	}
	return e, a, spaces[0].ID, token
}
func TestMCPSetupAndLiveMethodCoverage(t *testing.T) {
	e, _, _, token := mcpFixture(t)
	anonymous := connectMCP(t, e, "")
	tools, err := anonymous.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "netsidian_connect" {
		t.Fatal("anonymous connection exposed data tools")
	}
	_, setup := mcpCall(t, anonymous, "netsidian_connect", map[string]any{})
	if setup["settingsUrl"] != e.srv.URL+"/settings/mcp" || setup["connected"] != false {
		t.Fatalf("bad setup: %+v", setup)
	}
	authenticated := connectMCP(t, e, token)
	seen := map[string]bool{}
	cursor := 0
	for {
		result, page := mcpCall(t, authenticated, "netsidian_discover_api", map[string]any{"limit": 50, "cursor": cursor})
		if result.IsError {
			t.Fatal(page)
		}
		for _, item := range page["operations"].([]any) {
			op := item.(map[string]any)
			seen[op["id"].(string)] = true
			if op["available"] == false && op["reason"] == nil {
				t.Fatal("unavailable method not classified")
			}
		}
		next, ok := page["nextCursor"].(float64)
		if !ok {
			break
		}
		cursor = int(next)
	}
	var api map[string]any
	if err := json.Unmarshal(e.hs.apiDocJSON, &api); err != nil {
		t.Fatal(err)
	}
	count := 0
	for path, item := range api["paths"].(map[string]any) {
		for method := range item.(map[string]any) {
			if !isHTTPMethod(method) {
				continue
			}
			count++
			if !seen[strings.ToUpper(method)+" "+path] {
				t.Fatalf("MCP missing live method %s %s", method, path)
			}
		}
	}
	heads := 0
	for path, item := range api["paths"].(map[string]any) {
		if _, ok := item.(map[string]any)["get"]; ok {
			heads++
			if !seen["HEAD "+path] {
				t.Fatalf("missing implicit HEAD %s", path)
			}
		}
	}
	if count+heads != len(seen) {
		t.Fatalf("REST=%d implicit HEAD=%d MCP=%d", count, heads, len(seen))
	}
	t.Logf("all %d live methods accounted for", count)
	request, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := e.srv.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross-origin MCP request accepted")
	}
}

func TestMCPDocumentRoundtripAndPermissionGuards(t *testing.T) {
	e, actor, vault, token := mcpFixture(t)
	session := connectMCP(t, e, token)
	paths := map[string]string{"space": vault}
	original := "---\ntags: [agent-memory]\n---\n# Topic\n\nFirst observation."
	result, created := mcpCall(t, session, "netsidian_api", mcpAPIInput{Operation: "POST /api/v1/spaces/{space}/documents", PathParameters: paths, Body: map[string]string{"path": "agent-memory/topic.md", "content": original}, IdempotencyKey: "mcp-create"})
	if result.IsError || created["status"] != float64(201) {
		t.Fatalf("create: %+v", created)
	}
	doc := created["body"].(map[string]any)
	paths["doc"] = doc["id"].(string)
	revision := doc["currentRevision"].(string)
	_, read := mcpCall(t, session, "netsidian_api", mcpAPIInput{Operation: "GET /api/v1/spaces/{space}/documents/{doc}", PathParameters: paths})
	if read["body"].(map[string]any)["markdown"] != original || read["etag"] != `"`+revision+`"` {
		t.Fatal("read lost content or ETag")
	}
	update := mcpAPIInput{Operation: "PUT /api/v1/spaces/{space}/documents/{doc}/content", PathParameters: paths, Body: map[string]string{"markdown": original + "\n\nSecond observation."}, IfMatch: revision, IdempotencyKey: "mcp-save"}
	result, saved := mcpCall(t, session, "netsidian_api", update)
	if result.IsError || saved["status"] != float64(200) {
		t.Fatalf("save: %+v", saved)
	}
	result, replay := mcpCall(t, session, "netsidian_api", update)
	if result.IsError || replay["body"].(map[string]any)["revision"] != saved["body"].(map[string]any)["revision"] {
		t.Fatal("idempotent replay created a second revision")
	}
	update.IdempotencyKey = "mcp-stale"
	result, stale := mcpCall(t, session, "netsidian_api", update)
	if !result.IsError || stale["status"] != float64(412) {
		t.Fatalf("stale write accepted: %+v", stale)
	}
	_, readonly, err := e.svc.CreateKey(context.Background(), actor, "read only", []string{vault}, []string{domain.ScopeDocumentsRead}, 7)
	if err != nil {
		t.Fatal(err)
	}
	reader := connectMCP(t, e, readonly)
	result, denied := mcpCall(t, reader, "netsidian_api", mcpAPIInput{Operation: "POST /api/v1/spaces/{space}/documents", PathParameters: map[string]string{"space": vault}, Body: map[string]string{"path": "Denied.md", "content": "deny"}})
	if !result.IsError || denied["status"] != float64(403) {
		t.Fatal("MCP bypassed PAT scopes")
	}
	result, _ = mcpCall(t, session, "netsidian_api", mcpAPIInput{Operation: "GET /api/v1/spaces/{space}/documents/{doc}", PathParameters: map[string]string{"space": "../other", "doc": paths["doc"]}})
	if !result.IsError {
		t.Fatal("MCP accepted path traversal")
	}
	result, _ = mcpCall(t, session, "netsidian_api", mcpAPIInput{Operation: "POST /api/v1/me/keys", Body: map[string]any{}})
	if !result.IsError {
		t.Fatal("MCP exposed credential creation")
	}
}

func TestMCPBoundedResponsesAndMethodGuards(t *testing.T) {
	e, _, space, token := mcpFixture(t)
	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `"sample-revision"`)
		switch r.URL.Query().Get("mode") {
		case "large":
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, strings.Repeat("x", 8192))
		case "binary":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte{0xff, 0, 0x80, 42})
		default:
			writeJSON(w, 200, map[string]any{"revision": "sample-revision", "markdown": strings.Repeat("unnecessary body ", 100)})
		}
	})
	srv := httptest.NewServer(e.hs.newMCPHandler(handler, []apiRoute{{Method: "GET", Pattern: "/api/v1/spaces/{space}/sample"}}))
	t.Cleanup(srv.Close)
	fixture := *e
	fixture.srv = srv
	session := connectMCP(t, &fixture, token)
	input := mcpAPIInput{Operation: "GET /api/v1/spaces/{space}/sample", PathParameters: map[string]string{"space": space}, Query: map[string]any{"mode": "large"}, MaxResponseBytes: 1024}
	result, large := mcpCall(t, session, "netsidian_read", input)
	if !result.IsError || large["responseOmitted"] != true || large["responseBytes"] != float64(8192) || large["status"] != float64(200) {
		t.Fatalf("overflow was not bounded honestly: %+v", large)
	}
	input.Query["mode"] = "binary"
	result, binary := mcpCall(t, session, "netsidian_read", input)
	if result.IsError || binary["bodyEncoding"] != "base64" {
		t.Fatal(binary)
	}
	decoded, err := base64.StdEncoding.DecodeString(binary["body"].(string))
	if err != nil || !bytes.Equal(decoded, []byte{0xff, 0, 0x80, 42}) {
		t.Fatal("binary response corrupted")
	}
	input.Query = nil
	input.MaxResponseBytes = 4096
	input.Fields = []string{"revision"}
	result, projected := mcpCall(t, session, "netsidian_read", input)
	if result.IsError || len(projected["body"].(map[string]any)) != 1 {
		t.Fatal(projected)
	}
	input.Operation = "HEAD /api/v1/spaces/{space}/sample"
	input.Fields = nil
	result, head := mcpCall(t, session, "netsidian_read", input)
	if result.IsError || head["body"] != nil || head["etag"] != `"sample-revision"` {
		t.Fatal(head)
	}
	before := calls.Load()
	result, _ = mcpCall(t, session, "netsidian_write", input)
	if !result.IsError || calls.Load() != before {
		t.Fatal("mutation tool accepted HEAD")
	}
	input.Operation = "GET /api/v1/spaces/{space}/sample"
	input.PathParameters["extra"] = "wrong"
	result, _ = mcpCall(t, session, "netsidian_read", input)
	if !result.IsError || calls.Load() != before {
		t.Fatal("unknown path parameter dispatched")
	}
	delete(input.PathParameters, "extra")
	input.IfMatch = "revision\r\nInjected: header"
	result, _ = mcpCall(t, session, "netsidian_read", input)
	if !result.IsError || calls.Load() != before {
		t.Fatal("invalid header dispatched")
	}
}

func TestMCPResolvedSchemasAndRevokedCredentials(t *testing.T) {
	e, actor, space, token := mcpFixture(t)
	session := connectMCP(t, e, token)
	_, schema := mcpCall(t, session, "netsidian_discover_api", map[string]any{"operation": "POST /api/v1/spaces/{space}/documents"})
	raw, _ := json.Marshal(schema)
	if bytes.Contains(raw, []byte(`"$ref"`)) {
		t.Fatal("request schema still contains unresolved references")
	}
	parameters := schema["parameters"].([]any)
	hasSpace := false
	for _, parameter := range parameters {
		if parameter.(map[string]any)["name"] == "space" {
			hasSpace = true
		}
	}
	if !hasSpace {
		t.Fatal("shared space path parameter omitted")
	}
	_, lookup := mcpCall(t, session, "netsidian_api", mcpAPIInput{Operation: "GET /api/v1/spaces/{space}/documents/lookup", PathParameters: map[string]string{"space": space}, Query: map[string]any{"path": "agent-memory/missing.md"}})
	if lookup["status"] != float64(404) {
		t.Fatal(lookup)
	}
	key, secret, err := e.svc.CreateKey(context.Background(), actor, "revocation", []string{space}, []string{domain.ScopeDocumentsRead}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.svc.RevokeKey(context.Background(), actor, key.ID); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	request.Header.Set("Authorization", "Bearer "+secret)
	request.Header.Set("Content-Type", "application/json")
	response, err := e.srv.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 || response.Header.Get("WWW-Authenticate") == "" {
		t.Fatal("revoked PAT did not fail authentication")
	}
}

func TestMCPHostPolicyAndAnonymousRateKey(t *testing.T) {
	e, _, _, _ := mcpFixture(t)
	e.hs.PublicURL = "https://netsidian.example"
	handler := e.hs.newMCPHandler(http.NotFoundHandler(), nil)
	request := func(host, origin, address string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+host+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		r.RemoteAddr = address
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("attacker.example", "", "127.0.0.1:1234"); w.Code != 403 {
		t.Fatal("public Host was not pinned")
	}
	if w := request("netsidian.example", "https://netsidian.example", "127.0.0.1:1234"); w.Code != 200 {
		t.Fatalf("valid public origin rejected: %d", w.Code)
	}
	// Empty the existing peer bucket; a new TCP port must not reset the limit.
	e.hs.limiter.mu.Lock()
	e.hs.limiter.buckets["mcp:127.0.0.1"] = &bucket{last: time.Now()}
	e.hs.limiter.mu.Unlock()
	if w := request("netsidian.example", "", "127.0.0.1:5678"); w.Code != 429 {
		t.Fatal("TCP port change bypassed anonymous rate budget")
	}
	e.hs.PublicURL = "http://[invalid"
	if w := request("netsidian.example", "", "127.0.0.1:5678"); w.Code != 503 {
		t.Fatal("invalid public URL was not handled safely")
	}
}

package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/service"
)

func desktopMCPFixture(t *testing.T) (*testEnv, *DesktopMCPManager, *service.Actor, string, string) {
	t.Helper()
	e := newTestEnv(t, false)
	e.svc.Cfg.AuthMode = "none"
	e.hs.NativePick = func() (string, error) { return "", nil }
	user, err := e.svc.EnsureNoAuthPrincipal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	actor := &service.Actor{UserID: user.ID, Local: true, IsOperator: true, ExecutionTarget: "desktop"}
	spaces, _, err := e.svc.Spaces.SpacesForUser(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := e.svc.CreateSpace(context.Background(), actor, "Not shared", "shared")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewDesktopMCPManager(e.hs, filepath.Join(t.TempDir(), "desktop-mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	e.hs.DesktopMCP = manager
	t.Cleanup(manager.Close)
	return e, manager, actor, spaces[0].ID, other.ID
}
func desktopClientFixture(e *testEnv, endpoint string) *testEnv {
	copy := *e
	copy.srv = &httptest.Server{URL: strings.TrimSuffix(endpoint, "/mcp")}
	return &copy
}

func TestDesktopMCPNoTokenVaultAndWritePolicy(t *testing.T) {
	e, manager, actor, allowed, foreign := desktopMCPFixture(t)
	if status := manager.Status(); status.Running || status.Config.Enabled || !status.Config.ReadOnly {
		t.Fatal("desktop MCP was not disabled/read-only by default")
	}
	cfg := DesktopMCPConfig{Enabled: true, Port: 0, AllowUnauthenticated: true, ReadOnly: true, VaultIDs: []string{allowed}}
	if err := manager.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	status := manager.Status()
	endpoint, _ := url.Parse(status.URL)
	if endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("listener did not bind IPv4 loopback")
	}
	session := connectMCP(t, desktopClientFixture(e, status.URL), "")
	_, setup := mcpCall(t, session, "netsidian_connect", map[string]any{})
	if _, exists := setup["tokenEnvironmentVariable"]; exists {
		t.Fatal("token-free setup unnecessarily requires a token environment variable")
	}
	if setup["connected"] != true || setup["settingsUrl"] != nil || setup["settingsLocation"] == nil {
		t.Fatal(setup)
	}
	_, listed := mcpCall(t, session, "netsidian_read", mcpAPIInput{Operation: "GET /api/v1/spaces"})
	vaults := listed["body"].(map[string]any)["spaces"].([]any)
	if len(vaults) != 1 || vaults[0].(map[string]any)["space"].(map[string]any)["id"] != allowed {
		t.Fatal("unselected vault metadata leaked")
	}
	result, denied := mcpCall(t, session, "netsidian_read", mcpAPIInput{Operation: "GET /api/v1/spaces/{space}/documents", PathParameters: map[string]string{"space": foreign}})
	if !result.IsError || denied["status"] != float64(403) {
		t.Fatal("unselected vault readable")
	}
	create := mcpAPIInput{Operation: "POST /api/v1/spaces/{space}/documents", PathParameters: map[string]string{"space": allowed}, Body: map[string]string{"path": "agent-memory/Desktop.md", "content": "---\ntags: [agent-memory]\n---\n# Local MCP"}, IdempotencyKey: "desktop-create"}
	result, _ = mcpCall(t, session, "netsidian_write", create)
	if !result.IsError {
		t.Fatal("read-only setting allowed creation")
	}
	cfg.ReadOnly = false
	if err := manager.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	if manager.Status().URL != status.URL {
		t.Fatal("same-port policy update unexpectedly changed endpoint")
	}
	result, created := mcpCall(t, session, "netsidian_write", create)
	if result.IsError || created["status"] != float64(201) {
		t.Fatalf("token-free creation failed: %+v", created)
	}
	_, readOnlyToken, err := e.svc.CreateKey(context.Background(), actor, "readonly desktop", []string{allowed}, []string{domain.ScopeDocumentsRead}, 7)
	if err != nil {
		t.Fatal(err)
	}
	reader := connectMCP(t, desktopClientFixture(e, status.URL), readOnlyToken)
	create.Body = map[string]string{"path": "Denied.md", "content": "no"}
	create.IdempotencyKey = "desktop-readonly-denial"
	result, denied = mcpCall(t, reader, "netsidian_write", create)
	if !result.IsError || denied["status"] != float64(403) {
		t.Fatal("desktop policy elevated PAT write scopes")
	}
	for _, path := range []string{"/api/v1/native/mcp", "/api/v1/spaces"} {
		response, err := http.Get(strings.TrimSuffix(status.URL, "/mcp") + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatal("desktop listener exposed REST/administration routes")
		}
	}
	request, _ := http.NewRequest("POST", status.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross-origin desktop access accepted")
	}
	request.Header.Del("Origin")
	// The previous Do consumed and closed the request body.
	request.Body, err = request.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer invalid")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("invalid PAT fell back to anonymous local access")
	}
}

func TestDesktopMCPPersistencePortConflictAndPATMode(t *testing.T) {
	e, manager, _, allowed, _ := desktopMCPFixture(t)
	cfg := DesktopMCPConfig{Enabled: true, Port: 0, ReadOnly: true, VaultIDs: []string{allowed}}
	if err := manager.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	old := manager.Status().URL
	anonymous := connectMCP(t, desktopClientFixture(e, old), "")
	tools, err := anonymous.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 {
		t.Fatal("PAT-required mode exposed private tools")
	}
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	_, port, _ := net.SplitHostPort(occupied.Addr().String())
	cfg.Port, _ = strconv.Atoi(port)
	if err := manager.Apply(cfg); err == nil {
		t.Fatal("occupied port accepted")
	}
	if manager.Status().URL != old {
		t.Fatal("failed configuration replaced running listener")
	}
	manager.Close()
	restored, err := NewDesktopMCPManager(e.hs, manager.file)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if !restored.Status().Running || restored.Status().Config.AllowUnauthenticated {
		t.Fatal("saved policy not restored safely")
	}
	cfg = restored.Status().Config
	cfg.Enabled = false
	if err := restored.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	if restored.Status().Running || restored.Status().URL != "" {
		t.Fatal("disabled listener still running")
	}
	final, err := NewDesktopMCPManager(e.hs, manager.file)
	if err != nil {
		t.Fatal(err)
	}
	defer final.Close()
	if final.Status().Running {
		t.Fatal("disabled configuration reopened listener")
	}
}

func TestDesktopMCPRejectsScopeElevationAndWebRuntime(t *testing.T) {
	e, manager, actor, allowed, foreign := desktopMCPFixture(t)
	cfg := DesktopMCPConfig{Enabled: true, Port: 0, AllowUnauthenticated: true, VaultIDs: []string{allowed}}
	for _, key := range []*ports.APIKeyRecord{{SpaceIDs: []string{foreign}, Scopes: []string{domain.ScopeDocumentsRead}}, {SpaceIDs: []string{allowed}, Scopes: []string{domain.ScopeMembersManage}}} {
		original := *actor
		original.Key = key
		if _, err := scopedDesktopActor(&original, cfg); err == nil {
			t.Fatal("empty vault/scope intersection was elevated")
		}
	}
	e.hs.NativePick = nil
	if _, err := NewDesktopMCPManager(e.hs, manager.file); err == nil {
		t.Fatal("web runtime enabled desktop listener")
	}
}

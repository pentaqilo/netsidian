// Transport-level agent data interface tests (agent-memory PLAN.md):
// scoped API keys drive whoami/context/changes/memo-proposal over HTTP,
// including Idempotency-Key replay and vault binding enforcement.
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
)

func (e *testEnv) callHdrs(t *testing.T, method, path, body, bearer string, hdrs map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Netsidian-Requested", "1")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestAgentSurfaceOverHTTP(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, false)
	env.setup(t, "agentop@x.io")
	ua, err := env.svc.Users.UserByEmail(ctx, "agentop@x.io")
	if err != nil || ua == nil {
		t.Fatal("operator missing")
	}
	acc := &service.Actor{UserID: ua.ID, IsOperator: true}
	spaces, _, err := env.svc.Spaces.SpacesForUser(ctx, ua.ID)
	if err != nil || len(spaces) == 0 {
		t.Fatal("no operator space")
	}
	vault := spaces[0].ID
	for i := 0; i < 3; i++ {
		if _, err := env.svc.CreateNote(ctx, acc, vault, "agent-fixture-"+string(rune('a'+i))+".md",
			"# fixture\n\nzebrawood alignment notes\n", "", false); err != nil {
			t.Fatal(err)
		}
	}
	_, secret, err := env.svc.CreateKey(ctx, acc, "agent", []string{vault},
		[]string{domain.ScopeDocumentsRead, domain.ScopeSearchRead,
			domain.ScopeDocumentsWrite, domain.ScopeGraphRead}, 30)
	if err != nil || secret == "" {
		t.Fatalf("CreateKey: %v", err)
	}

	// whoami
	resp := env.callHdrs(t, "GET", "/api/v1/agent/whoami", "", secret, map[string]string{"X-Correlation-Id": "corr-1"})
	if resp.StatusCode != 200 {
		t.Fatalf("whoami status %d", resp.StatusCode)
	}
	var who service.AgentWhoami
	if err := json.NewDecoder(resp.Body).Decode(&who); err != nil {
		t.Fatal(err)
	}
	if who.Contract != service.AgentContract || who.Auth.Method != "api-key" || who.Auth.KeyID == "" {
		t.Fatalf("whoami body: %+v", who.Auth)
	}
	if len(who.Vaults) != 1 || who.Vaults[0].ID != vault {
		t.Fatalf("whoami vaults: %+v", who.Vaults)
	}

	// context search through the scoped key
	resp = env.callHdrs(t, "POST", "/api/v1/spaces/"+vault+"/agent/context",
		`{"query":"zebrawood","maxBytes":4000,"includeMemories":false,"includeGraph":false}`, secret, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("context status %d", resp.StatusCode)
	}
	var ctxResp service.AgentContextResponse
	if err := json.NewDecoder(resp.Body).Decode(&ctxResp); err != nil {
		t.Fatal(err)
	}
	if len(ctxResp.Items) == 0 || ctxResp.BudgetUsed.Bytes > 4000 {
		t.Fatalf("context payload: items=%d bytes=%d", len(ctxResp.Items), ctxResp.BudgetUsed.Bytes)
	}

	// malformed cursor rejected
	resp = env.callHdrs(t, "POST", "/api/v1/spaces/"+vault+"/agent/context",
		`{"query":"zebrawood","cursor":"###"}`, secret, nil)
	if resp.StatusCode != 400 {
		t.Fatalf("bad cursor status %d", resp.StatusCode)
	}

	// Ordinary memory documents use the same replay-safe create endpoint.
	body := `{"path":"Agent Memory/HTTP fact.md","content":"# HTTP fact\n[[agent-fixture-a]]"}`
	route := "/api/v1/spaces/" + vault + "/documents"
	r1 := env.callHdrs(t, "POST", route, body, secret, map[string]string{"Idempotency-Key": "hk-1"})
	if r1.StatusCode != 201 {
		bb, _ := io.ReadAll(r1.Body)
		t.Fatalf("create status %d: %s", r1.StatusCode, bb)
	}
	var id1 struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r1.Body).Decode(&id1); err != nil || id1.ID == "" {
		t.Fatalf("create body: %v", err)
	}
	r2 := env.callHdrs(t, "POST", route, body, secret, map[string]string{"Idempotency-Key": "hk-1"})
	if r2.StatusCode > 299 {
		t.Fatalf("replay status %d", r2.StatusCode)
	}
	var id2 struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r2.Body).Decode(&id2); err != nil || id2.ID != id1.ID {
		t.Fatalf("replay different doc %+v", id2)
	}
	r3 := env.callHdrs(t, "POST", route, strings.Replace(body, "HTTP fact\\n", "Different fact\\n", 1), secret, map[string]string{"Idempotency-Key": "hk-1"})
	if r3.StatusCode != 409 {
		t.Fatalf("body swap status %d", r3.StatusCode)
	}
	retired := env.callHdrs(t, "GET", "/api/v1/spaces/"+vault+"/memories", "", secret, nil)
	if retired.StatusCode != 404 {
		t.Fatalf("retired memories status %d", retired.StatusCode)
	}

	// change feed with cursor continuation
	resp = env.callHdrs(t, "GET", "/api/v1/spaces/"+vault+"/changes/since?limit=2", "", secret, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("changes status %d", resp.StatusCode)
	}
	var feed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		t.Fatal(err)
	}
	if feed["contract"] != service.AgentContract || feed["nextCursor"] == "" {
		t.Fatalf("feed body: %v", feed)
	}

	// vault binding enforced: second vault returns not found for this key
	sp2, err := env.svc.CreateSpace(ctx, acc, "Unbound Vault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	resp = env.callHdrs(t, "GET", "/api/v1/spaces/"+sp2.ID+"/changes/since", "", secret, nil)
	if resp.StatusCode != 403 && resp.StatusCode != 404 {
		t.Fatalf("unbound vault status %d", resp.StatusCode)
	}
}

func TestAgentSurfaceRequiresAuth(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t, "op2@x.io")
	if resp := env.call(t, "GET", "/api/v1/agent/whoami", "", "", ""); resp.StatusCode != 401 {
		t.Fatalf("anonymous whoami status %d", resp.StatusCode)
	}
}

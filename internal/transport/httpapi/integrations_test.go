package httpapi

import (
	"context"
	"encoding/json"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
	"testing"
)

func TestIntegrationDiagnosticsRequireHumanOperator(t *testing.T) {
	env := newTestEnv(t, false)
	cookie := env.setup(t, "diagnostics@example.test")
	r := env.call(t, "GET", "/api/v1/me/integrations", "", cookie, "")
	if r.StatusCode != 200 {
		t.Fatalf("operator status=%d", r.StatusCode)
	}
	var result map[string]any
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["storage"] != "local" || result["backendReachable"] != true || result["oauthConfigured"] != false {
		t.Fatalf("status=%v", result)
	}
	if len(result) != 3 {
		t.Fatalf("unexpected configuration exposure=%v", result)
	}
	ctx := context.Background()
	user, _ := env.svc.Users.UserByEmail(ctx, "diagnostics@example.test")
	spaces, _, _ := env.svc.Spaces.SpacesForUser(ctx, user.ID)
	_, secret, err := env.svc.CreateKey(ctx, &service.Actor{UserID: user.ID, IsOperator: true}, "diagnostic test", []string{spaces[0].ID}, []string{domain.ScopeDocumentsRead}, 7)
	if err != nil {
		t.Fatal(err)
	}
	r = env.call(t, "GET", "/api/v1/me/integrations", "", "", secret)
	if r.StatusCode != 403 {
		t.Fatalf("API key status=%d", r.StatusCode)
	}
	r = env.call(t, "GET", "/api/v1/me/integrations", "", "", "")
	if r.StatusCode != 401 {
		t.Fatalf("anonymous status=%d", r.StatusCode)
	}
}

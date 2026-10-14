package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/service"
)

func TestMixedAuthRequiresLocalBootstrapBeforeProviderLogin(t *testing.T) {
	env := newTestEnv(t, true)
	env.svc.Cfg.AuthMode = "mixed"
	env.svc.Cfg.OIDCAllowAllUsers = true
	ctx := context.Background()
	claims := map[string]any{"sub": "member"}
	if _, err := env.svc.ExternalLogin(ctx, "https://issuer.example", "member", "", "Member", false, claims, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("provider sign-in bypassed local administrator setup")
	}
	if count, _ := env.svc.Users.UserCount(ctx); count != 0 {
		t.Fatal("provider created an account before setup")
	}
	env.setup(t, "admin@example.test")
	if _, err := env.svc.ExternalLogin(ctx, "https://issuer.example", "member", "", "Member", false, claims, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestAdministrationAuthorizationAndRevocation(t *testing.T) {
	env := newTestEnv(t, true)
	ctx := context.Background()
	adminCookie := env.setup(t, "admin@example.test")
	signup := env.call(t, "POST", "/api/v1/auth/signup", `{"email":"member@example.test","password":"0123456789ab","displayName":"Member"}`, "", "")
	if signup.StatusCode != 201 {
		t.Fatal(signup.StatusCode)
	}
	memberCookie := ""
	for _, cookie := range signup.Cookies() {
		if cookie.Name == sessionCookie {
			memberCookie = cookie.Value
		}
	}
	member, _ := env.svc.Users.UserByEmail(ctx, "member@example.test")
	admin, _ := env.svc.Users.UserByEmail(ctx, "admin@example.test")
	memberActor := &service.Actor{UserID: member.ID}
	spaces, _, _ := env.svc.Spaces.SpacesForUser(ctx, member.ID)
	if member.IsOperator || len(spaces) != 1 {
		t.Fatal("signup must create a regular account with a personal vault")
	}
	for _, path := range []string{"/api/v1/admin/users", "/api/v1/admin/settings", "/api/v1/admin/audit"} {
		if r := env.call(t, "GET", path, "", memberCookie, ""); r.StatusCode != 403 {
			t.Fatalf("member %s: %d", path, r.StatusCode)
		}
		if r := env.call(t, "GET", path, "", adminCookie, ""); r.StatusCode != 200 {
			t.Fatalf("admin %s: %d", path, r.StatusCode)
		}
	}
	ownSpaces, _, _ := env.svc.Spaces.SpacesForUser(ctx, admin.ID)
	_, adminKey, err := env.svc.CreateKey(ctx, &service.Actor{UserID: admin.ID, IsOperator: true}, "admin agent", []string{ownSpaces[0].ID}, []string{domain.ScopeDocumentsRead}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if r := env.call(t, "GET", "/api/v1/admin/users", "", "", adminKey); r.StatusCode != 403 {
		t.Fatalf("operator API key allowed: %d", r.StatusCode)
	}
	for _, body := range []string{`{"disabled":true}`, `{"isOperator":false}`} {
		if r := env.call(t, "PATCH", "/api/v1/admin/users/"+admin.ID, body, adminCookie, ""); r.StatusCode != 409 {
			t.Fatalf("self-lockout accepted: %d", r.StatusCode)
		}
	}
	_, memberKey, err := env.svc.CreateKey(ctx, memberActor, "member agent", []string{spaces[0].ID}, []string{domain.ScopeDocumentsRead}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if r := env.call(t, "PATCH", "/api/v1/admin/users/"+member.ID, `{"disabled":true}`, adminCookie, ""); r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	if r := env.call(t, "GET", "/api/v1/me", "", memberCookie, ""); r.StatusCode != 401 {
		t.Fatalf("disabled session valid: %d", r.StatusCode)
	}
	if _, _, err := env.svc.NewSession(ctx, member.ID); err == nil {
		t.Fatal("disabled account received a new session")
	}
	if r := env.call(t, "PATCH", "/api/v1/admin/users/"+member.ID, `{"disabled":false}`, adminCookie, ""); r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	if r := env.call(t, "GET", "/api/v1/me", "", "", memberKey); r.StatusCode != 401 {
		t.Fatalf("re-enabled account restored revoked key: %d", r.StatusCode)
	}
	resp := env.call(t, "GET", "/api/v1/admin/audit", "", adminCookie, "")
	var audit struct {
		Events []map[string]any `json:"events"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&audit); err != nil || len(audit.Events) != 2 {
		t.Fatalf("audit: %+v %v", audit, err)
	}
}

func TestExternalOnlyRejectsExistingLocalSessions(t *testing.T) {
	env := newTestEnv(t, false)
	cookie := env.setup(t, "local@example.test")
	env.svc.Cfg.AuthMode = "external"
	env.svc.Cfg.OIDCAllowAllUsers = true
	if r := env.call(t, "GET", "/api/v1/me", "", cookie, ""); r.StatusCode != 401 {
		t.Fatalf("local session bypassed external-only policy: %d", r.StatusCode)
	}
}

func TestFirstExternalAccountDoesNotBootstrapAnAdministrator(t *testing.T) {
	env := newTestEnv(t, false)
	env.svc.Cfg.AuthMode = "external"
	env.svc.Cfg.OIDCAllowAllUsers = true
	u, err := env.svc.ExternalLogin(context.Background(), "https://issuer.example", "first", "", "First", false, map[string]any{}, time.Now().Add(time.Hour))
	if err != nil || u.IsOperator {
		t.Fatalf("first external identity gained admin rights: %+v %v", u, err)
	}
}

func TestLoginAndRegistrationRejectCrossOriginPosts(t *testing.T) {
	env := newTestEnv(t, true)
	for _, path := range []string{"setup", "login", "signup"} {
		request := httptest.NewRequest("POST", "http://notes.example/api/v1/auth/"+path, strings.NewReader(`{}`))
		request.Header.Set("Origin", "https://attacker.example")
		request.Header.Set("X-Netsidian-Requested", "1")
		response := httptest.NewRecorder()
		env.hs.Handler().ServeHTTP(response, request)
		if response.Code != 403 {
			t.Fatalf("cross-origin %s allowed: %d", path, response.Code)
		}
	}
}

func TestConcurrentAdministratorChangesCannotRemoveAllAdmins(t *testing.T) {
	env := newTestEnv(t, true)
	ctx := context.Background()
	env.setup(t, "a@example.test")
	b, err := env.svc.SignupAccount(ctx, "b@example.test", "0123456789ab", "B")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := env.svc.Users.UserByEmail(ctx, "a@example.test")
	actorA := &service.Actor{UserID: a.ID, IsOperator: true}
	yes, no := true, false
	if err = env.svc.UpdateAdminUser(ctx, actorA, b.ID, ports.AdminChange{IsOperator: &yes}); err != nil {
		t.Fatal(err)
	}
	actorB := &service.Actor{UserID: b.ID, IsOperator: true}
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for _, job := range []struct {
		actor  *service.Actor
		target string
	}{{actorA, b.ID}, {actorB, a.ID}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errors <- env.svc.UpdateAdminUser(ctx, job.actor, job.target, ports.AdminChange{IsOperator: &no})
		}()
	}
	wg.Wait()
	close(errors)
	successes := 0
	for err := range errors {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal(fmt.Sprintf("concurrent changes succeeded %d times", successes))
	}
	users, _, err := env.svc.Users.(service.AdminRepo).AdminUsers(ctx, "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, u := range users {
		if u.IsOperator && !u.Disabled {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("remaining admins: %d", count)
	}
}

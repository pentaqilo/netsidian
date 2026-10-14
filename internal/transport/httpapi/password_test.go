package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// POST /api/v1/auth/password over HTTP: cookie sessions rotate, every other
// credential of the account is revoked atomically, key principals are denied,
// and the admin audit feed records the change.

// firstSpaceID reads the operator's personal vault id (setup creates one).
func firstSpaceID(t *testing.T, env *testEnv, cookie string) string {
	t.Helper()
	resp := env.call(t, "GET", "/api/v1/spaces", "", cookie, "")
	if resp.StatusCode != 200 {
		t.Fatalf("spaces status %d", resp.StatusCode)
	}
	var out struct {
		Spaces []struct {
			Space struct {
				ID string `json:"id"`
			} `json:"space"`
		} `json:"spaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Spaces) == 0 {
		t.Fatalf("spaces body: %v", err)
	}
	return out.Spaces[0].Space.ID
}

func createAgentKey(t *testing.T, env *testEnv, cookie string) (id, secret string) {
	t.Helper()
	spaceID := firstSpaceID(t, env, cookie)
	resp := env.call(t, "POST", "/api/v1/me/keys", `{"label":"agent","spaceIds":["`+spaceID+`"],"scopes":["documents:read"],"expiresInDays":30}`, cookie, "")
	if resp.StatusCode != 201 {
		t.Fatalf("key create status %d", resp.StatusCode)
	}
	var created struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.Secret == "" {
		t.Fatalf("key create body: %v", err)
	}
	return created.ID, created.Secret
}

func TestPasswordChangeRouteRotatesAndRevokes(t *testing.T) {
	env := newTestEnv(t, false)
	cookie := env.setup(t, "pw@x.io")

	// mint a personal API key that must die with the password change
	keyID, keySecret := createAgentKey(t, env, cookie)
	_ = keyID

	// wrong current password: 401 and nothing changes
	resp := env.call(t, "POST", "/api/v1/auth/password", `{"currentPassword":"wrongcurrent1","newPassword":"brandnewpass10"}`, cookie, "")
	if resp.StatusCode != 401 {
		t.Fatalf("wrong current password status %d", resp.StatusCode)
	}
	if resp = env.call(t, "GET", "/api/v1/me", "", cookie, ""); resp.StatusCode != 200 {
		t.Fatal("session must survive a failed change")
	}

	// correct change: 200 with revocation counts
	resp = env.call(t, "POST", "/api/v1/auth/password", `{"currentPassword":"0123456789ab","newPassword":"brandnewpass10"}`, cookie, "")
	if resp.StatusCode != 200 {
		t.Fatalf("change status %d", resp.StatusCode)
	}
	var out struct {
		OK              bool `json:"ok"`
		ReauthRequired  bool `json:"reauthRequired"`
		SessionsRevoked int  `json:"sessionsRevoked"`
		KeysRevoked     int  `json:"keysRevoked"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || !out.ReauthRequired || out.SessionsRevoked < 1 || out.KeysRevoked != 1 {
		t.Fatalf("unexpected body %+v", out)
	}
	cleared := false
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie && c.Value == "" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("password rotation did not clear browser cookie")
	}

	// the old cookie is dead (revoked server-side and cleared in the response)
	if resp = env.call(t, "GET", "/api/v1/me", "", cookie, ""); resp.StatusCode != 401 {
		t.Fatalf("old cookie status %d, want 401", resp.StatusCode)
	}
	// the old API key is dead
	if resp = env.call(t, "GET", "/api/v1/me", "", "", keySecret); resp.StatusCode != 401 {
		t.Fatalf("revoked key status %d, want 401", resp.StatusCode)
	}

	// old password no longer logs in, new one does
	if resp = env.call(t, "POST", "/api/v1/auth/login", `{"email":"pw@x.io","password":"0123456789ab"}`, "", ""); resp.StatusCode != 401 {
		t.Fatalf("old password login status %d, want 401", resp.StatusCode)
	}
	resp = env.call(t, "POST", "/api/v1/auth/login", `{"email":"pw@x.io","password":"brandnewpass10"}`, "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("new password login status %d", resp.StatusCode)
	}
	var newCookie string
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			newCookie = c.Value
		}
	}
	if newCookie == "" {
		t.Fatal("no session cookie from new-password login")
	}
	resp = env.call(t, "GET", "/api/v1/admin/audit", "", newCookie, "")
	if resp.StatusCode != 200 {
		t.Fatalf("audit status %d", resp.StatusCode)
	}
	var audit struct {
		Events []struct {
			Action string `json:"action"`
			Actor  string `json:"actor"`
		} `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&audit); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range audit.Events {
		if e.Action == "password.change" {
			found = true
		}
	}
	if !found {
		t.Fatalf("password.change missing from admin audit: %+v", audit.Events)
	}
}

func TestPasswordChangeRequiresCSRFAndAdvertisesEligibility(t *testing.T) {
	env := newTestEnv(t, false)
	cookie := env.setup(t, "pw@x.io")
	resp := env.call(t, "GET", "/api/v1/me", "", cookie, "")
	var me struct {
		CanChangePassword bool `json:"canChangePassword"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil || !me.CanChangePassword {
		t.Fatal("eligible human cannot see password change", err)
	}
	_, key := createAgentKey(t, env, cookie)
	resp = env.call(t, "GET", "/api/v1/me", "", "", key)
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil || me.CanChangePassword {
		t.Fatal("key principal has password-change eligibility", err)
	}
	for _, crossOrigin := range []bool{false, true} {
		req, err := http.NewRequest("POST", env.srv.URL+"/api/v1/auth/password", strings.NewReader(`{"currentPassword":"0123456789ab","newPassword":"brandnewpass10"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		if crossOrigin {
			req.Header.Set("X-Netsidian-Requested", "1")
			req.Header.Set("Origin", "https://foreign.example")
		}
		resp, err := env.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("CSRF mutation accepted: %d", resp.StatusCode)
		}
	}
	if resp := env.call(t, "GET", "/api/v1/me", "", cookie, ""); resp.StatusCode != 200 {
		t.Fatal("CSRF rejection altered session")
	}
}

func TestPasswordChangeRouteDeniesKeyPrincipals(t *testing.T) {
	env := newTestEnv(t, false)
	cookie := env.setup(t, "pw@x.io")
	_, keySecret := createAgentKey(t, env, cookie)
	resp := env.call(t, "POST", "/api/v1/auth/password", `{"currentPassword":"0123456789ab","newPassword":"brandnewpass10"}`, "", keySecret)
	if resp.StatusCode != 403 {
		t.Fatalf("key principal change-password status %d, want 403", resp.StatusCode)
	}
	// and the human session still works with the old password
	if resp = env.call(t, "GET", "/api/v1/me", "", cookie, ""); resp.StatusCode != 200 {
		t.Fatal("human session must be unaffected")
	}
}

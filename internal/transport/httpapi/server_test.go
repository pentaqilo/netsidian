package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/service"
	"github.com/pentaqilo/netsidian/internal/storage/local"
)

// Gate A transport negative tests (Implementation Review 2026-09-26):
// cross-space history denial, restricted-key key-management denial,
// signup gating. Creation of the foreign fixtures goes through the
// service; every denial attempt goes through HTTP.

type testEnv struct {
	srv *httptest.Server
	svc *service.Services
	hs  *Server
}

func newTestEnv(t *testing.T, signup bool) *testEnv {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "test.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	content, err := local.NewContent(filepath.Join(dir, "content"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := local.NewState(filepath.Join(dir, "journal"), filepath.Join(dir, "history"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewServices(
		service.Config{AuthMode: "local", SignupEnabled: signup,
			MaxNoteBytes: 1 << 20, MaxAttachBytes: 1 << 20, TrashRetentionDays: 30},
		db, db, db, db, db, db, db, content, state)
	hs := New(svc)
	ts := httptest.NewServer(hs.Handler())
	t.Cleanup(ts.Close)
	return &testEnv{srv: ts, svc: svc, hs: hs}
}

func (e *testEnv) call(t *testing.T, method, path, body, cookie, bearer string) *http.Response {
	t.Helper()
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Netsidian-Requested", "1")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (e *testEnv) setup(t *testing.T, email string) string {
	t.Helper()
	resp := e.call(t, "POST", "/api/v1/auth/setup",
		`{"email":"`+email+`","password":"0123456789ab","displayName":"Op"}`, "", "")
	if resp.StatusCode != 201 {
		t.Fatalf("setup status %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	t.Fatal("no session cookie from setup")
	return ""
}

func (e *testEnv) login(t *testing.T, email string) string {
	t.Helper()
	resp := e.call(t, "POST", "/api/v1/auth/login",
		`{"email":"`+email+`","password":"0123456789ab"}`, "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	t.Fatal("no session cookie from login")
	return ""
}

func TestVaultDeleteRouteChecksOwnerAndTypedName(t *testing.T) {
	env := newTestEnv(t, true)
	owner := env.setup(t, "delete-owner@x.io")
	created := env.call(t, "POST", "/api/v1/spaces", `{"name":"Disposable Vault","kind":"shared"}`, owner, "")
	if created.StatusCode != 201 {
		t.Fatalf("create status %d", created.StatusCode)
	}
	var payload struct {
		Space struct {
			ID string `json:"id"`
		} `json:"space"`
	}
	if err := json.NewDecoder(created.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	url := "/api/v1/spaces/" + payload.Space.ID
	otherResp := env.call(t, "POST", "/api/v1/auth/signup", `{"email":"delete-other@x.io","password":"0123456789ab","displayName":"Other"}`, "", "")
	if otherResp.StatusCode != 201 {
		t.Fatalf("signup status %d", otherResp.StatusCode)
	}
	other := env.login(t, "delete-other@x.io")
	if r := env.call(t, "DELETE", url, `{"confirmation":"Disposable Vault"}`, other, ""); r.StatusCode != 404 && r.StatusCode != 403 {
		t.Fatalf("non-owner deletion status %d", r.StatusCode)
	}
	if r := env.call(t, "DELETE", url, `{"confirmation":"wrong"}`, owner, ""); r.StatusCode != 400 {
		t.Fatalf("wrong name status %d", r.StatusCode)
	}
	if r := env.call(t, "DELETE", url, `{"confirmation":"Disposable Vault"}`, owner, ""); r.StatusCode != 200 {
		t.Fatalf("delete status %d", r.StatusCode)
	}
	if r := env.call(t, "GET", url, "", owner, ""); r.StatusCode != 404 {
		t.Fatalf("deleted vault status %d", r.StatusCode)
	}
	if r := env.call(t, "GET", "/api/v1/native/status", "", owner, ""); r.StatusCode != 404 {
		t.Fatalf("server exposed native route: %d", r.StatusCode)
	}
}

func TestSignupGating(t *testing.T) {
	ctx := context.Background()
	closed := newTestEnv(t, false)
	cookie := closed.setup(t, "op-a@x.io")
	if resp := closed.call(t, "POST", "/api/v1/auth/signup",
		`{"email":"two@x.io","password":"0123456789ab","displayName":"Two"}`, cookie, ""); resp.StatusCode != 404 {
		t.Fatalf("signup while disabled must 404, got %d", resp.StatusCode)
	}
	_ = cookie

	open := newTestEnv(t, true)
	if resp := open.call(t, "POST", "/api/v1/auth/signup",
		`{"email":"early@x.io","password":"0123456789ab","displayName":"E"}`, "", ""); resp.StatusCode != 409 {
		t.Fatalf("signup before setup must 409, got %d", resp.StatusCode)
	}
	open.setup(t, "op-b@x.io")
	resp := open.call(t, "POST", "/api/v1/auth/signup",
		`{"email":"two@x.io","password":"0123456789ab","displayName":"Two"}`, "", "")
	if resp.StatusCode != 201 {
		t.Fatalf("signup status %d", resp.StatusCode)
	}
	// second signup with the same email must conflict
	if resp := open.call(t, "POST", "/api/v1/auth/signup",
		`{"email":"two@x.io","password":"0123456789ab","displayName":"Two"}`, "", ""); resp.StatusCode != 409 {
		t.Fatalf("duplicate signup must 409, got %d", resp.StatusCode)
	}
	// the signed-up user is not an operator
	u, err := open.svc.Users.UserByEmail(ctx, "two@x.io")
	if err != nil || u == nil || u.IsOperator {
		t.Fatalf("signup user wrong shape: %v %+v", err, u)
	}
}

func TestCrossSpaceRevisionLeakDenied(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	cookieA := env.setup(t, "a@x.io")
	// B receives a private personal space at signup and creates a secret note.
	if _, err := env.svc.SignupAccount(ctx, "b@x.io", "0123456789ab", "B"); err != nil {
		t.Fatal(err)
	}
	ub, err := env.svc.Users.UserByEmail(ctx, "b@x.io")
	if err != nil || ub == nil {
		t.Fatalf("user b: %v", err)
	}
	accB := &service.Actor{UserID: ub.ID}
	spacesB, _, err := env.svc.Spaces.SpacesForUser(ctx, ub.ID)
	if err != nil || len(spacesB) != 1 {
		t.Fatalf("personal space for b: %v", err)
	}
	spaceB := spacesB[0]
	doc, err := env.svc.CreateNote(ctx, accB, spaceB.ID, "secret.md", "B-SECRET-DATA", "", false)
	if err != nil {
		t.Fatal(err)
	}
	// A's own personal space
	spacesA, _, err := env.svc.Spaces.SpacesForUser(ctx, func() string {
		ua, err := env.svc.Users.UserByEmail(ctx, "a@x.io")
		if err != nil || ua == nil {
			t.Fatal("user a missing")
		}
		return ua.ID
	}())
	if err != nil || len(spacesA) == 0 {
		t.Fatalf("spaces for a: %v", err)
	}
	// The leak attempt: B's document ids inside A's space URL.
	revURL := "/api/v1/spaces/" + spacesA[0].ID + "/documents/" + doc.ID + "/revisions"
	if resp := env.call(t, "GET", revURL, "", cookieA, ""); resp.StatusCode != 404 {
		t.Fatalf("cross-space revisions must 404, got %d", resp.StatusCode)
	}
	// even with B's own session the bytes stay confined to B's space
	cookieB := env.login(t, "b@x.io")
	if resp := env.call(t, "GET", revURL, "", cookieB, ""); resp.StatusCode != 404 {
		t.Fatalf("B's foreign-space revisions must also 404, got %d", resp.StatusCode)
	}
	// in B's space the revisions exist (proves the fixture is real)
	resp := env.call(t, "GET", "/api/v1/spaces/"+spaceB.ID+"/documents/"+doc.ID+"/revisions", "", cookieB, "")
	if resp.StatusCode != 200 {
		t.Fatalf("own-space revisions must 200, got %d", resp.StatusCode)
	}
	var revs []map[string]any
	var payload struct {
		Revisions []map[string]any `json:"revisions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || len(payload.Revisions) == 0 {
		t.Fatalf("decode revisions: %v %v", err, payload.Revisions)
	}
	revs = payload.Revisions
	revID, _ := revs[0]["id"].(string)
	contentURL := "/api/v1/spaces/" + spacesA[0].ID + "/documents/" + doc.ID + "/revisions/" + revID
	if resp := env.call(t, "GET", contentURL, "", cookieA, ""); resp.StatusCode != 404 {
		t.Fatalf("cross-space revision bytes must 404, got %d", resp.StatusCode)
	}
}

func TestBearerKeyCannotManageKeysOrSpaces(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, false)
	env.setup(t, "op@x.io")
	ua, err := env.svc.Users.UserByEmail(ctx, "op@x.io")
	if err != nil || ua == nil {
		t.Fatal("operator missing")
	}
	acc := &service.Actor{UserID: ua.ID, IsOperator: true}
	spaces, _, err := env.svc.Spaces.SpacesForUser(ctx, ua.ID)
	if err != nil || len(spaces) == 0 {
		t.Fatal("no operator space")
	}
	rec, secret, err := env.svc.CreateKey(ctx, acc, "read-only", []string{spaces[0].ID},
		[]string{domain.ScopeDocumentsRead}, 30)
	if err != nil || rec == nil || secret == "" {
		t.Fatalf("CreateKey: %v", err)
	}
	if resp := env.call(t, "POST", "/api/v1/me/keys",
		`{"label":"escalation","spaceIds":[],"scopes":[]}`, "", secret); resp.StatusCode != 403 {
		t.Fatalf("bearer key creating keys must 403, got %d", resp.StatusCode)
	}
	if resp := env.call(t, "POST", "/api/v1/spaces",
		`{"name":"Escalated","kind":"shared"}`, "", secret); resp.StatusCode != 403 {
		t.Fatalf("bearer key creating spaces must 403, got %d", resp.StatusCode)
	}
}

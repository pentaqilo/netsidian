package httpapi

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/service"
	"github.com/pentaqilo/netsidian/internal/storage/local"
	"github.com/pentaqilo/netsidian/internal/storage/providers"
)

// Transport tests for the second-factor flow: enrollment/confirmation, the
// login challenge (which must not authorize ordinary API calls), recovery-code
// login, factor-gated disable, and operator recovery.

func newFactorEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "test.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.SQL.Close() })
	content, err := local.NewContent(filepath.Join(dir, "content"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := local.NewState(filepath.Join(dir, "journal"), filepath.Join(dir, "history"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewServices(
		service.Config{AuthMode: "local", SignupEnabled: true,
			MaxNoteBytes: 1 << 20, MaxAttachBytes: 1 << 20, TrashRetentionDays: 30},
		db, db, db, db, db, db, db, content, state)
	mgr, err := providers.New(content, db, []byte("test-instance-secret-32-bytes!!!"))
	if err != nil {
		t.Fatal(err)
	}
	svc.Seal = mgr
	hs := New(svc)
	ts := httptest.NewServer(hs.Handler())
	t.Cleanup(ts.Close)
	return &testEnv{srv: ts, svc: svc, hs: hs}
}

// totpForTest mirrors RFC 6238 so the test can produce a valid code from the
// secret the enrollment endpoint returned.
func totpForTest(t *testing.T, secret string, offset int64) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	step := time.Now().Unix()/30 + offset
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	o := sum[len(sum)-1] & 0x0f
	v := (int64(sum[o]&0x7f) << 24) | (int64(sum[o+1]&0xff) << 16) | (int64(sum[o+2]&0xff) << 8) | int64(sum[o+3]&0xff)
	return fmt.Sprintf("%06d", v%1000000)
}

func decodeBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return m
}

func sessionFrom(resp *http.Response) string {
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	return ""
}

// enrollConfirm enrolls and confirms a factor for the given session, returning
// the shared secret and recovery codes.
func enrollConfirm(t *testing.T, env *testEnv, cookie, password string) (string, []string) {
	t.Helper()
	resp := env.call(t, "POST", "/api/v1/auth/totp/enroll", `{"password":"`+password+`"}`, cookie, "")
	if resp.StatusCode != 201 {
		t.Fatalf("enroll status %d: %v", resp.StatusCode, decodeBody(t, resp))
	}
	view := decodeBody(t, resp)
	secret, _ := view["secret"].(string)
	raw, _ := view["recoveryCodes"].([]any)
	codes := make([]string, len(raw))
	for i, c := range raw {
		codes[i], _ = c.(string)
	}
	if secret == "" || len(codes) == 0 {
		t.Fatalf("enroll view incomplete: %v", view)
	}
	resp = env.call(t, "POST", "/api/v1/auth/totp/confirm", `{"code":"`+totpForTest(t, secret, 0)+`"}`, cookie, "")
	if resp.StatusCode != 200 {
		t.Fatalf("confirm status %d: %v", resp.StatusCode, decodeBody(t, resp))
	}
	return secret, codes
}

func TestHTTPEnrollConfirmLoginChallengeComplete(t *testing.T) {
	env := newFactorEnv(t)
	cookie := env.setup(t, "op@x.io")
	secret, _ := enrollConfirm(t, env, cookie, "0123456789ab")

	// Login now yields a challenge and no session cookie.
	resp := env.call(t, "POST", "/api/v1/auth/login", `{"email":"op@x.io","password":"0123456789ab"}`, "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if body["challenge"] != true {
		t.Fatalf("expected challenge response: %v", body)
	}
	if sessionFrom(resp) != "" {
		t.Fatal("a pending challenge must not set a session cookie")
	}
	challengeToken, _ := body["challengeToken"].(string)
	if challengeToken == "" {
		t.Fatal("missing challenge token")
	}

	// Completing with a valid code issues the session.
	resp = env.call(t, "POST", "/api/v1/auth/challenge/totp",
		`{"challengeToken":"`+challengeToken+`","code":"`+totpForTest(t, secret, 1)+`"}`, "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("challenge completion status %d: %v", resp.StatusCode, decodeBody(t, resp))
	}
	session := sessionFrom(resp)
	if session == "" {
		t.Fatal("challenge completion must issue a session cookie")
	}
	resp = env.call(t, "GET", "/api/v1/me", "", session, "")
	if resp.StatusCode != 200 || decodeBody(t, resp)["totpEnabled"] != true {
		t.Fatalf("me should report totpEnabled: %v", decodeBody(t, resp))
	}
}

func TestHTTPChallengeCannotAuthorizeAPI(t *testing.T) {
	env := newFactorEnv(t)
	cookie := env.setup(t, "op@x.io")
	enrollConfirm(t, env, cookie, "0123456789ab")

	resp := env.call(t, "POST", "/api/v1/auth/login", `{"email":"op@x.io","password":"0123456789ab"}`, "", "")
	challengeToken, _ := decodeBody(t, resp)["challengeToken"].(string)

	// As a bearer key it is not a key.
	if r := env.call(t, "GET", "/api/v1/spaces", "", "", challengeToken); r.StatusCode != 401 {
		t.Fatalf("challenge token must not work as a bearer key (status %d)", r.StatusCode)
	}
	// As a session cookie it is not a session.
	if r := env.call(t, "GET", "/api/v1/spaces", "", challengeToken, ""); r.StatusCode != 401 {
		t.Fatalf("challenge token must not work as a session cookie (status %d)", r.StatusCode)
	}
}

func TestHTTPRecoveryCodeCompletesChallenge(t *testing.T) {
	env := newFactorEnv(t)
	cookie := env.setup(t, "op@x.io")
	_, codes := enrollConfirm(t, env, cookie, "0123456789ab")

	resp := env.call(t, "POST", "/api/v1/auth/login", `{"email":"op@x.io","password":"0123456789ab"}`, "", "")
	challengeToken, _ := decodeBody(t, resp)["challengeToken"].(string)
	resp = env.call(t, "POST", "/api/v1/auth/challenge/recovery",
		`{"challengeToken":"`+challengeToken+`","code":"`+codes[0]+`"}`, "", "")
	if resp.StatusCode != 200 || sessionFrom(resp) == "" {
		t.Fatalf("recovery completion failed: status %d", resp.StatusCode)
	}
}

func TestHTTPDisableRequiresFactor(t *testing.T) {
	env := newFactorEnv(t)
	cookie := env.setup(t, "op@x.io")
	secret, codes := enrollConfirm(t, env, cookie, "0123456789ab")
	cookie = factorSession(t, env, "op@x.io", "0123456789ab", codes[9])

	if r := env.call(t, "POST", "/api/v1/auth/totp/disable", `{"password":"0123456789ab","code":"000000"}`, cookie, ""); r.StatusCode != 401 {
		t.Fatalf("disable with invalid code must fail (status %d)", r.StatusCode)
	}
	if r := env.call(t, "POST", "/api/v1/auth/totp/disable", `{"password":"wrongpassword1","code":"`+totpForTest(t, secret, 1)+`"}`, cookie, ""); r.StatusCode != 401 {
		t.Fatalf("disable with wrong password must fail (status %d)", r.StatusCode)
	}
	if r := env.call(t, "POST", "/api/v1/auth/totp/disable", `{"password":"0123456789ab","code":"`+totpForTest(t, secret, 1)+`"}`, cookie, ""); r.StatusCode != 200 {
		t.Fatalf("valid disable failed (status %d): %v", r.StatusCode, decodeBody(t, r))
	}
	// Login is single-factor again.
	resp := env.call(t, "POST", "/api/v1/auth/login", `{"email":"op@x.io","password":"0123456789ab"}`, "", "")
	if resp.StatusCode != 200 || sessionFrom(resp) == "" {
		t.Fatal("login should issue a session after disable")
	}
}

func TestHTTPAdminTOTPResetIsOperatorOnly(t *testing.T) {
	env := newFactorEnv(t)
	opCookie := env.setup(t, "op@x.io")

	// A second account enrolls its own factor.
	resp := env.call(t, "POST", "/api/v1/auth/signup", `{"email":"user@x.io","password":"userpassword1","displayName":"User"}`, "", "")
	if resp.StatusCode != 201 {
		t.Fatalf("signup status %d", resp.StatusCode)
	}
	userCookie := sessionFrom(resp)
	_, codes := enrollConfirm(t, env, userCookie, "userpassword1")
	userCookie = factorSession(t, env, "user@x.io", "userpassword1", codes[9])

	// The user is not an operator and cannot reset their own factor via admin.
	if r := env.call(t, "POST", "/api/v1/admin/users/"+decodeBody(t, mustMe(t, env, userCookie))["userId"].(string)+"/totp-reset", "", userCookie, ""); r.StatusCode != 403 {
		t.Fatalf("non-operator reset must be forbidden (status %d)", r.StatusCode)
	}

	// The operator can reset it; the user then signs in with password only.
	userID := decodeBody(t, mustMe(t, env, userCookie))["userId"].(string)
	if r := env.call(t, "POST", "/api/v1/admin/users/"+userID+"/totp-reset", `{"password":"0123456789ab"}`, opCookie, ""); r.StatusCode != 200 {
		t.Fatalf("operator reset failed (status %d): %v", r.StatusCode, decodeBody(t, r))
	}
	resp = env.call(t, "POST", "/api/v1/auth/login", `{"email":"user@x.io","password":"userpassword1"}`, "", "")
	if resp.StatusCode != 200 || sessionFrom(resp) == "" {
		t.Fatalf("recovered user must sign in with password only: status %d", resp.StatusCode)
	}
}

func mustMe(t *testing.T, env *testEnv, cookie string) *http.Response {
	t.Helper()
	resp := env.call(t, "GET", "/api/v1/me", "", cookie, "")
	if resp.StatusCode != 200 {
		t.Fatalf("me status %d", resp.StatusCode)
	}
	return resp
}

func TestHTTPPasswordChangeRequiresFactor(t *testing.T) {
	env := newFactorEnv(t)
	cookie := env.setup(t, "op@x.io")
	secret, codes := enrollConfirm(t, env, cookie, "0123456789ab")
	cookie = factorSession(t, env, "op@x.io", "0123456789ab", codes[9])
	// A confirmed factor forces step-up: no code, no change.
	if r := env.call(t, "POST", "/api/v1/auth/password", `{"currentPassword":"0123456789ab","newPassword":"brandnewpass10"}`, cookie, ""); r.StatusCode != 401 {
		t.Fatalf("password change without a factor code should be 401: %d %v", r.StatusCode, decodeBody(t, r))
	}
	if r := env.call(t, "POST", "/api/v1/auth/password", `{"currentPassword":"0123456789ab","newPassword":"brandnewpass10","code":"`+totpForTest(t, secret, 1)+`"}`, cookie, ""); r.StatusCode != 200 {
		t.Fatalf("password change with a valid factor failed: %d %v", r.StatusCode, decodeBody(t, r))
	}
}

func factorSession(t *testing.T, env *testEnv, email, password, code string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp := env.call(t, "POST", "/api/v1/auth/login", string(payload), "", "")
	token, _ := decodeBody(t, resp)["challengeToken"].(string)
	payload, _ = json.Marshal(map[string]string{"challengeToken": token, "code": code})
	resp = env.call(t, "POST", "/api/v1/auth/challenge/recovery", string(payload), "", "")
	if resp.StatusCode != 200 || sessionFrom(resp) == "" {
		t.Fatalf("factor login failed: %d", resp.StatusCode)
	}
	return sessionFrom(resp)
}

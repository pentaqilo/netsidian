package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/pentaqilo/netsidian/internal/authpolicy"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/service"
	"github.com/pentaqilo/netsidian/internal/storage/providers"
)

func TestOAuthVerifiedLoginAndReplay(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
	if err != nil {
		t.Fatal(err)
	}
	var issuer, nonce, challenge string
	used := false
	groups := []string{"admins"}
	tokenAudience := "netsidian-test"
	tokenExpiry := time.Now().Add(time.Hour)
	wrongNonce := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			r.ParseForm()
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			refresh := r.Form.Get("grant_type") == "refresh_token" && r.Form.Get("refresh_token") == "refresh-me"
			if !refresh && (used || r.Form.Get("code") != "single-use-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge) {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			used = true
			issuedNonce := nonce
			if wrongNonce {
				issuedNonce = "wrong-nonce"
			}
			claims, _ := json.Marshal(map[string]any{"iss": issuer, "sub": "stable-subject", "aud": tokenAudience, "exp": tokenExpiry.Unix(), "iat": time.Now().Unix(), "nonce": issuedNonce, "profile": map[string]any{"mail": "oidc@example.test", "verified": true, "display": "OIDC User"}, "groups": groups})
			signed, _ := signer.Sign(claims)
			jwt, _ := signed.CompactSerialize()
			json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh-me", "token_type": "Bearer", "expires_in": 3600, "id_token": jwt})
		default:
			w.WriteHeader(404)
		}
	}))
	defer provider.Close()
	issuer = provider.URL
	env := newTestEnv(t, false)
	env.svc.Cfg.AuthMode = "external"
	env.svc.Cfg.OIDCIssuer = issuer
	env.svc.Cfg.OIDCAllowedClaims = authpolicy.Rules{"/groups": {"members", "admins"}}
	env.svc.Cfg.OIDCAdminClaims = authpolicy.Rules{"/groups": {"admins"}}
	env.hs.OAuth, err = NewOAuthLogin(context.Background(), issuer, "netsidian-test", "secret", env.srv.URL, "Test SSO", make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := providers.New(env.svc.Content, env.svc.Users.(ports.StorageSecretRepo), make([]byte, 32))
	env.hs.OAuth.SetProfileClaims("/profile/mail", "/profile/display", "/profile/verified")
	env.hs.OAuth.ConfigureStorageTokens(manager, env.svc.Users.(ports.StorageSecretRepo), env.svc)
	env.svc.RefreshExternalAccess = env.hs.OAuth.RefreshExternalAccess
	// Rebuild routes with the now configured provider.
	handler := env.hs.Handler()
	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest("GET", env.srv.URL+"/api/v1/auth/oauth/start?next=%2Fs%2Ftest", nil))
	if start.Code != 302 {
		t.Fatalf("start: %d %s", start.Code, start.Body.String())
	}
	authorization, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := authorization.Query()
	nonce = q.Get("nonce")
	challenge = q.Get("code_challenge")
	if nonce == "" || q.Get("state") == "" || challenge == "" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("missing protections: %v", q)
	}
	cookies := start.Result().Cookies()
	if len(cookies) != 1 || cookies[0].SameSite != http.SameSiteLaxMode || !cookies[0].HttpOnly {
		t.Fatalf("login cookie: %+v", cookies)
	}
	callbackURL := env.srv.URL + "/api/v1/auth/oauth/callback?code=single-use-code&state=" + q.Get("state")
	callback := httptest.NewRequest("GET", callbackURL, nil)
	callback.AddCookie(cookies[0])
	done := httptest.NewRecorder()
	handler.ServeHTTP(done, callback)
	if done.Code != 303 || done.Header().Get("Location") != "/s/test" {
		t.Fatalf("callback: %d %s", done.Code, done.Body.String())
	}
	foundSession := false
	var loginUID string
	for _, c := range done.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			foundSession = true
			uid, ok, e := env.svc.ResolveSession(context.Background(), c.Value)
			if e != nil || !ok {
				t.Fatal("invalid issued session")
			}
			loginUID = uid
			u, _ := env.svc.Users.UserByID(context.Background(), uid)
			if u.DisplayName != "OIDC User" || u.Email != "oidc@example.test" || !u.IsOperator {
				t.Fatalf("account: %+v", u)
			}
		}
	}
	if !foundSession {
		t.Fatal("missing session")
	}

	spaces, _, _ := env.svc.Spaces.SpacesForUser(context.Background(), loginUID)
	_, pat, err := env.svc.CreateKey(context.Background(), &service.Actor{UserID: loginUID}, "refresh test", []string{spaces[0].ID}, []string{"documents:read"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	proofRepo := env.svc.Users.(service.ExternalIdentityRepo)
	if err = proofRepo.UpdateExternalAuth(context.Background(), loginUID, map[string]any{"groups": []any{"admins"}}, time.Now().Add(-time.Minute), true); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", env.srv.URL+"/api/v1/agent/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+pat)
	renewed := httptest.NewRecorder()
	handler.ServeHTTP(renewed, req)
	if renewed.Code != 200 {
		t.Fatalf("PAT did not revalidate external identity: %d %s", renewed.Code, renewed.Body.String())
	}
	proof, _ := proofRepo.ExternalAuth(context.Background(), loginUID)
	if !proof.ExpiresAt.After(time.Now()) {
		t.Fatal("refreshed claim evidence not persisted")
	}
	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, callback.Clone(context.Background()))
	if replay.Code != 401 {
		t.Fatalf("replay permitted: %d", replay.Code)
	}
	bad := httptest.NewRequest("GET", strings.Replace(callbackURL, q.Get("state"), "attacker", 1), nil)
	bad.AddCookie(cookies[0])
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, bad)
	if denied.Code != 401 {
		t.Fatalf("state mismatch permitted: %d", denied.Code)
	}

	for _, scenario := range []string{"audience", "expiry", "nonce"} {
		used = false
		tokenAudience = "netsidian-test"
		tokenExpiry = time.Now().Add(time.Hour)
		wrongNonce = false
		if scenario == "audience" {
			tokenAudience = "another-client"
		}
		if scenario == "expiry" {
			tokenExpiry = time.Now().Add(-time.Hour)
		}
		if scenario == "nonce" {
			wrongNonce = true
		}
		restart := httptest.NewRecorder()
		handler.ServeHTTP(restart, httptest.NewRequest("GET", env.srv.URL+"/api/v1/auth/oauth/start", nil))
		target, _ := url.Parse(restart.Header().Get("Location"))
		nonce = target.Query().Get("nonce")
		challenge = target.Query().Get("code_challenge")
		request := httptest.NewRequest("GET", env.srv.URL+"/api/v1/auth/oauth/callback?code=single-use-code&state="+target.Query().Get("state"), nil)
		request.AddCookie(restart.Result().Cookies()[0])
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatalf("invalid %s accepted: %d", scenario, response.Code)
		}
	}
	tokenAudience = "netsidian-test"
	tokenExpiry = time.Now().Add(time.Hour)
	wrongNonce = false
	// A valid signature is not an admission grant. A denied later login
	// withdraws the existing account's sessions and claim evidence too.
	groups = []string{"outsider"}
	used = false
	restart := httptest.NewRecorder()
	handler.ServeHTTP(restart, httptest.NewRequest("GET", env.srv.URL+"/api/v1/auth/oauth/start", nil))
	newAuth, _ := url.Parse(restart.Header().Get("Location"))
	nonce = newAuth.Query().Get("nonce")
	challenge = newAuth.Query().Get("code_challenge")
	rejectedRequest := httptest.NewRequest("GET", env.srv.URL+"/api/v1/auth/oauth/callback?code=single-use-code&state="+newAuth.Query().Get("state"), nil)
	rejectedRequest.AddCookie(restart.Result().Cookies()[0])
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, rejectedRequest)
	if rejected.Code != 403 {
		t.Fatalf("signed outsider admitted: %d %s", rejected.Code, rejected.Body.String())
	}
	for _, cookie := range done.Result().Cookies() {
		if cookie.Name == sessionCookie {
			request := httptest.NewRequest("GET", env.srv.URL+"/api/v1/me", nil)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 401 {
				t.Fatalf("old external session survived denied claims: %d", response.Code)
			}
		}
	}
}

func TestOAuthCookieTamperAndExpiry(t *testing.T) {
	// Exercise encrypted flow integrity independently of the provider.
	key := make([]byte, 32)
	block, _ := newTestCipher(key)
	o := &OAuthLogin{cipher: block}
	encoded, e := o.seal(oauthFlow{State: "state", Expires: time.Now().Add(time.Minute).Unix()})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = o.open(encoded); e != nil {
		t.Fatal(e)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(encoded)
	raw[len(raw)-1] ^= 1
	if _, e = o.open(base64.RawURLEncoding.EncodeToString(raw)); e == nil {
		t.Fatal("tampered cookie accepted")
	}
	expired, _ := o.seal(oauthFlow{State: "state", Expires: time.Now().Add(-time.Minute).Unix()})
	if _, e = o.open(expired); e == nil {
		t.Fatal("expired cookie accepted")
	}
}

func newTestCipher(key []byte) (cipher.AEAD, error) {
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}

func TestOAuthBrowserFailureRedirect(t *testing.T) {
	req := httptest.NewRequest("GET", "http://app/api/v1/auth/oauth/callback", nil)
	req.Header.Set("Accept", "text/html")
	response := httptest.NewRecorder()
	oauthError(response, req, domain.Forbidden("private policy detail"))
	if response.Code != 303 || response.Header().Get("Location") != "/auth?oauth_error=access_denied" {
		t.Fatal("browser error not recoverable", response.Code, response.Header())
	}
	req.Header.Set("Accept", "application/json")
	response = httptest.NewRecorder()
	oauthError(response, req, domain.Forbidden("denied"))
	if response.Code != 403 {
		t.Fatal("API error changed", response.Code)
	}
}

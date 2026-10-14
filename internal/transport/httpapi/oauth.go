package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pentaqilo/netsidian/internal/authpolicy"
	"github.com/pentaqilo/netsidian/internal/domain"
	"golang.org/x/oauth2"
)

type OAuthLogin struct {
	config                                    oauth2.Config
	verifier                                  *oidc.IDTokenVerifier
	issuer                                    string
	label                                     string
	emailClaim, nameClaim, emailVerifiedClaim string
	storage                                   *oauthStorage
	cipher                                    cipher.AEAD
}

func (o *OAuthLogin) SetScopes(scopes []string) {
	o.config.Scopes = []string{oidc.ScopeOpenID}
	seen := map[string]bool{oidc.ScopeOpenID: true}
	for _, scope := range scopes {
		if !seen[scope] {
			o.config.Scopes = append(o.config.Scopes, scope)
			seen[scope] = true
		}
	}
}

type oauthFlow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Expires  int64  `json:"e"`
	Next     string `json:"next"`
}

const oauthCookie = "nsd_oauth"

func NewOAuthLogin(ctx context.Context, issuer, clientID, clientSecret, publicURL, label string, secret []byte) (*OAuthLogin, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &OAuthLogin{config: oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, Endpoint: provider.Endpoint(), RedirectURL: strings.TrimRight(publicURL, "/") + "/api/v1/auth/oauth/callback", Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}, verifier: provider.Verifier(&oidc.Config{ClientID: clientID}), issuer: issuer, label: label, cipher: aead}, nil
}
func oauthRandom() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func (o *OAuthLogin) seal(flow oauthFlow) (string, error) {
	b, err := json.Marshal(flow)
	if err != nil {
		return "", err
	}
	n := make([]byte, o.cipher.NonceSize())
	if _, err = rand.Read(n); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(o.cipher.Seal(n, n, b, []byte(oauthCookie))), nil
}
func (o *OAuthLogin) open(value string) (*oauthFlow, error) {
	b, err := base64.RawURLEncoding.DecodeString(value)
	n := o.cipher.NonceSize()
	if err != nil || len(b) < n {
		return nil, fmt.Errorf("invalid login cookie")
	}
	b, err = o.cipher.Open(nil, b[:n], b[n:], []byte(oauthCookie))
	if err != nil {
		return nil, err
	}
	var f oauthFlow
	err = json.Unmarshal(b, &f)
	if err != nil || f.Expires < time.Now().Unix() {
		return nil, fmt.Errorf("expired login")
	}
	return &f, nil
}

func (s *Server) handleOAuthProviders(w http.ResponseWriter, r *http.Request) {
	providers := []map[string]string{}
	if s.OAuth != nil {
		providers = append(providers, map[string]string{"name": s.OAuth.label, "url": "/api/v1/auth/oauth/start"})
	}
	writeJSON(w, 200, map[string]any{"providers": providers})
}
func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		writeErr(w, domain.Unsupported("OAuth is not configured"))
		return
	}
	next := r.URL.Query().Get("next")
	if (!strings.HasPrefix(next, "/s/") && next != "/settings/mcp") || strings.ContainsAny(next, "\\\r\n") {
		next = "/"
	}
	flow := oauthFlow{State: oauthRandom(), Nonce: oauthRandom(), Verifier: oauth2.GenerateVerifier(), Expires: time.Now().Add(10 * time.Minute).Unix(), Next: next}
	value, err := s.OAuth.seal(flow)
	if err != nil {
		writeErr(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: value, Path: "/api/v1/auth/oauth", MaxAge: 600, HttpOnly: true, Secure: schemeOf(r, s.TrustedProxy) == "https", SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, s.OAuth.config.AuthCodeURL(flow.State, oidc.Nonce(flow.Nonce), oauth2.S256ChallengeOption(flow.Verifier)), http.StatusFound)
}
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Path: "/api/v1/auth/oauth", MaxAge: -1, HttpOnly: true, Secure: schemeOf(r, s.TrustedProxy) == "https", SameSite: http.SameSiteLaxMode})
	if s.OAuth == nil {
		oauthError(w, r, domain.Unsupported("OAuth is not configured"))
		return
	}
	c, err := r.Cookie(oauthCookie)
	if err != nil {
		oauthError(w, r, domain.Unauthenticated("login cookie missing; restart sign-in"))
		return
	}
	flow, err := s.OAuth.open(c.Value)
	if err != nil || subtle.ConstantTimeCompare([]byte(flowState(flow)), []byte(r.URL.Query().Get("state"))) != 1 {
		oauthError(w, r, domain.Unauthenticated("invalid or expired login state"))
		return
	}
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		oauthError(w, r, domain.Unauthenticated("provider did not authorize login"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	// Bound discovery/token/JWKS HTTP responses through a timeout-bearing client.
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: 15 * time.Second})
	token, err := s.OAuth.config.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		oauthError(w, r, domain.Unauthenticated("provider token exchange failed"))
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		oauthError(w, r, domain.Unauthenticated("provider omitted identity token"))
		return
	}
	id, err := s.OAuth.verifier.Verify(ctx, raw)
	if err != nil || id == nil || subtle.ConstantTimeCompare([]byte(idNonce(id)), []byte(flow.Nonce)) != 1 {
		oauthError(w, r, domain.Unauthenticated("invalid identity token"))
		return
	}
	var verifiedClaims map[string]any
	if err = id.Claims(&verifiedClaims); err != nil {
		oauthError(w, r, domain.Unauthenticated("invalid identity claims"))
		return
	}
	emailPath, namePath, verifiedPath := s.OAuth.emailClaim, s.OAuth.nameClaim, s.OAuth.emailVerifiedClaim
	if emailPath == "" {
		emailPath = "/email"
	}
	if namePath == "" {
		namePath = "/name"
	}
	if verifiedPath == "" {
		verifiedPath = "/email_verified"
	}
	email, _ := authpolicy.Value(verifiedClaims, emailPath).(string)
	name, _ := authpolicy.Value(verifiedClaims, namePath).(string)
	verified, _ := authpolicy.Value(verifiedClaims, verifiedPath).(bool)
	u, err := s.Svc.ExternalLogin(ctx, s.OAuth.issuer, id.Subject, email, name, verified, verifiedClaims, id.Expiry)
	if err != nil {
		oauthError(w, r, err)
		return
	}
	if err = s.OAuth.saveStorageToken(ctx, u.ID, id.Subject, raw, token); err != nil {
		oauthError(w, r, domain.Unavailable("could not save provider credentials"))
		return
	}
	session, expires, err := s.Svc.NewSession(ctx, u.ID)
	if err != nil {
		oauthError(w, r, err)
		return
	}
	setSessionCookie(w, r, session, expires, s.TrustedProxy)
	http.Redirect(w, r, flow.Next, http.StatusSeeOther)
}
func flowState(f *oauthFlow) string {
	if f == nil {
		return ""
	}
	return f.State
}
func idNonce(id *oidc.IDToken) string {
	if id == nil {
		return ""
	}
	return id.Nonce
}

func (o *OAuthLogin) SetProfileClaims(email, name, verified string) {
	o.emailClaim = email
	o.nameClaim = name
	o.emailVerifiedClaim = verified
}

// Browsers get a recoverable sign-in screen; API clients retain JSON errors.
func oauthError(w http.ResponseWriter, r *http.Request, err error) {
	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		writeErr(w, err)
		return
	}
	code := "signin_failed"
	var de *domain.Error
	if errors.As(err, &de) {
		if de.Code == domain.ErrCodeForbidden {
			code = "access_denied"
		}
		if de.Code == domain.ErrCodeConflict {
			code = "account_conflict"
		}
	}
	http.Redirect(w, r, "/auth?oauth_error="+code, http.StatusSeeOther)
}

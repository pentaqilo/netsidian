package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/pentaqilo/netsidian/internal/authpolicy"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/service"
	"github.com/pentaqilo/netsidian/internal/storage/providers"
	"golang.org/x/oauth2"
)

type oauthStorage struct {
	manager *providers.Manager
	repo    ports.StorageSecretRepo
	svc     *service.Services
	mu      sync.Mutex
	gates   map[string]chan struct{}
}
type savedOAuthToken struct {
	Issuer  string
	Subject string
	IDToken string
	Token   oauth2.Token
}

func (o *OAuthLogin) ConfigureStorageTokens(m *providers.Manager, r ports.StorageSecretRepo, s *service.Services) {
	o.storage = &oauthStorage{manager: m, repo: r, svc: s, gates: map[string]chan struct{}{}}
}
func (o *OAuthLogin) saveStorageToken(ctx context.Context, user, subject, raw string, t *oauth2.Token) error {
	if o.storage == nil {
		return nil
	}
	b, e := o.storage.manager.Seal("oauth:"+user, savedOAuthToken{o.issuer, subject, raw, *t})
	if e != nil {
		return e
	}
	return o.storage.repo.SaveOAuthStorageToken(ctx, user, b)
}

// StorageIdentityToken returns only verified ID tokens from this deployment's
// provider. Refresh tokens stay sealed in SQLite; the browser never receives them.
func (o *OAuthLogin) StorageIdentityToken(ctx context.Context, user string) (string, error) {
	return o.storageIdentityToken(ctx, user, false)
}

func (o *OAuthLogin) storageIdentityToken(ctx context.Context, user string, force bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: 15 * time.Second})
	if o.storage == nil {
		return "", fmt.Errorf("OIDC storage federation unavailable")
	}
	o.storage.mu.Lock()
	gate := o.storage.gates[user]
	if gate == nil {
		gate = make(chan struct{}, 1)
		o.storage.gates[user] = gate
	}
	o.storage.mu.Unlock()
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	u, e := o.storage.svc.Users.UserByID(ctx, user)
	if e != nil {
		return "", e
	}
	if u == nil || u.Disabled {
		return "", fmt.Errorf("storage credential owner unavailable")
	}
	if force && o.storage.svc.CheckUserAccess(ctx, u) == nil {
		force = false
	}
	b, e := o.storage.repo.OAuthStorageToken(ctx, user)
	if e != nil {
		return "", e
	}
	if b == nil {
		return "", fmt.Errorf("sign in with OIDC before using federated storage")
	}
	var saved savedOAuthToken
	if e = o.storage.manager.Open("oauth:"+user, b, &saved); e != nil {
		return "", e
	}
	if saved.Issuer != o.issuer {
		return "", fmt.Errorf("storage credential issuer changed; sign in again")
	}
	id, e := o.verifier.Verify(ctx, saved.IDToken)
	if force || e != nil || id.Expiry.Before(time.Now().Add(time.Minute)) {
		if saved.Token.RefreshToken == "" {
			return "", fmt.Errorf("OIDC storage token expired; sign in again with offline_access scope")
		}
		// Force refresh even if the access token expires later than the ID token.
		saved.Token.Expiry = time.Now().Add(-time.Minute)
		token, err := o.config.TokenSource(ctx, &saved.Token).Token()
		if err != nil {
			return "", fmt.Errorf("OIDC storage refresh failed; sign in again")
		}
		raw, ok := token.Extra("id_token").(string)
		if !ok {
			return "", fmt.Errorf("OIDC refresh omitted identity token")
		}
		id, e = o.verifier.Verify(ctx, raw)
		if e != nil || id.Subject != saved.Subject {
			return "", fmt.Errorf("invalid refreshed storage identity")
		}
		saved.IDToken = raw
		saved.Token = *token
		if e = o.saveStorageToken(ctx, user, saved.Subject, raw, token); e != nil {
			return "", e
		}
	}
	if id.Subject != saved.Subject {
		return "", fmt.Errorf("storage identity mismatch")
	}
	var claims map[string]any
	if e = id.Claims(&claims); e != nil {
		return "", e
	}
	cfg := o.storage.svc.Cfg
	if !cfg.OIDCAllowAllUsers && !cfg.OIDCAllowedClaims.Match(claims) {
		return "", fmt.Errorf("storage owner no longer meets access policy")
	}
	return saved.IDToken, nil
}

// RefreshExternalAccess rechecks PAT admission and roles using a fresh verified
// identity token. Missing/invalid refresh grants require interactive sign-in.
func (o *OAuthLogin) RefreshExternalAccess(ctx context.Context, user string) error {
	raw, err := o.storageIdentityToken(ctx, user, true)
	if err != nil {
		return err
	}
	id, err := o.verifier.Verify(ctx, raw)
	if err != nil {
		return err
	}
	var claims map[string]any
	if err = id.Claims(&claims); err != nil {
		return err
	}
	email, _ := authpolicy.Value(claims, o.emailClaim).(string)
	name, _ := authpolicy.Value(claims, o.nameClaim).(string)
	verified, _ := authpolicy.Value(claims, o.emailVerifiedClaim).(bool)
	u, err := o.storage.svc.ExternalLogin(ctx, o.issuer, id.Subject, email, name, verified, claims, id.Expiry)
	if err != nil {
		return err
	}
	if u.ID != user {
		return fmt.Errorf("refreshed identity mismatch")
	}
	return nil
}

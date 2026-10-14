package service

import (
	"context"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/authpolicy"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func TestExternalAdmissionRolesAndExistingSessionRevocation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.svc.Cfg.AuthMode = "external"
	h.svc.Cfg.OIDCIssuer = "https://issuer.example"
	h.svc.Cfg.OIDCAllowedClaims = authpolicy.Rules{"/groups": {"members", "admins"}}
	h.svc.Cfg.OIDCAdminClaims = authpolicy.Rules{"/groups": {"admins"}}
	login := func(subject, role string) (string, bool, error) {
		u, err := h.svc.ExternalLogin(ctx, "https://issuer.example", subject, "", subject, false, map[string]any{"groups": []any{role}}, time.Now().Add(2*time.Hour))
		if err != nil {
			return "", false, err
		}
		return u.ID, u.IsOperator, nil
	}
	baseline, _ := h.svc.Users.UserCount(ctx)
	if _, _, err := login("denied", "outsider"); err == nil {
		t.Fatal("outsider admitted")
	}
	if count, _ := h.svc.Users.UserCount(ctx); count != baseline {
		t.Fatal("denied identity provisioned")
	}
	if _, admin, err := login("first", "members"); err != nil || admin {
		t.Fatal("first external member became admin", err)
	}
	id, admin, err := login("operator", "admins")
	if err != nil || !admin {
		t.Fatal("admin claim failed", err)
	}
	token, expiry, err := h.svc.NewSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if expiry.After(time.Now().Add(time.Hour + time.Minute)) {
		t.Fatal("external session exceeds evidence bound")
	}
	if _, admin, err = login("operator", "members"); err != nil || admin {
		t.Fatal("admin role was not removed", err)
	}
	if _, ok, err := h.svc.ResolveSession(ctx, token); err != nil || ok {
		t.Fatal("old elevated session survived role downgrade")
	}
	u, _ := h.svc.Users.UserByID(ctx, id)
	h.svc.Cfg.OIDCAllowedClaims = authpolicy.Rules{"/groups": {"other"}}
	if err = h.svc.CheckUserAccess(ctx, u); err == nil {
		t.Fatal("current policy did not reject old evidence")
	}
	h.svc.Cfg.OIDCAllowedClaims = authpolicy.Rules{"/groups": {"members", "admins"}}
	repo := h.svc.Users.(ExternalIdentityRepo)
	if err = repo.UpdateExternalAuth(ctx, id, map[string]any{"groups": []any{"members"}}, time.Now().Add(-time.Minute), false); err != nil {
		t.Fatal(err)
	}
	if err = h.svc.CheckUserAccess(ctx, u); err == nil {
		t.Fatal("expired identity evidence admitted")
	}
}

func TestLastLocalAdministratorAndManagedExternalRoles(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.svc.Cfg.AuthMode = "mixed"
	local, err := h.svc.Users.UserByID(ctx, h.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	h.svc.Cfg.OIDCAllowAllUsers = true
	h.svc.Cfg.OIDCAdminClaims = authpolicy.Rules{"/role": {"admin"}}
	external, err := h.svc.ExternalLogin(ctx, "https://issuer.example", "sso", "", "SSO Admin", false, map[string]any{"role": "admin"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	no := false
	if err = h.svc.UpdateAdminUser(ctx, &Actor{UserID: external.ID, IsOperator: true}, local.ID, ports.AdminChange{IsOperator: &no}); err == nil {
		t.Fatal("last local admin demoted using a temporary external admin")
	}
	if err = h.svc.UpdateAdminUser(ctx, &Actor{UserID: local.ID, IsOperator: true}, external.ID, ports.AdminChange{IsOperator: &no}); err == nil {
		t.Fatal("provider managed role edited locally")
	}
}

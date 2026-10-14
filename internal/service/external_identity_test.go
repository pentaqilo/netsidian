package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"testing"
	"time"
)

func TestExternalIdentityDoesNotLinkByEmail(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.svc.Cfg.AuthMode = "external"
	h.svc.Cfg.OIDCAllowAllUsers = true
	proof := map[string]any{}
	expiry := time.Now().Add(time.Hour)
	u, e := h.svc.ExternalLogin(ctx, "https://issuer.example", "subject-a", "new@example.test", "External", true, proof, expiry)
	if e != nil {
		t.Fatal(e)
	}
	again, e := h.svc.ExternalLogin(ctx, "https://issuer.example", "subject-a", "changed@example.test", "Changed", true, proof, expiry)
	if e != nil || again.ID != u.ID {
		t.Fatalf("stable subject: %+v %v", again, e)
	}
	if _, e = h.svc.ExternalLogin(ctx, "https://issuer.example", "subject-b", "new@example.test", "Attacker", true, proof, expiry); e == nil {
		t.Fatal("email takeover permitted")
	}
	if _, e = h.svc.ExternalLogin(ctx, "https://other.example", "subject-a", "new@example.test", "Attacker", true, proof, expiry); e == nil {
		t.Fatal("issuer takeover permitted")
	}
	unverified, e := h.svc.ExternalLogin(ctx, "https://issuer.example", "subject-c", "new@example.test", "Unverified", false, proof, expiry)
	if e != nil || unverified.Email != "" || unverified.IsOperator {
		t.Fatalf("unverified identity: %+v %v", unverified, e)
	}
	spaces, _, _ := h.svc.Spaces.SpacesForUser(ctx, u.ID)
	if len(spaces) != 1 {
		t.Fatal("personal vault missing")
	}
	if _, e = h.svc.ExternalLogin(ctx, "https://issuer.example", "", "", "", false, proof, expiry); e == nil || e.(*domain.Error).Code != domain.ErrCodeUnauthenticated {
		t.Fatalf("empty subject: %v", e)
	}
}

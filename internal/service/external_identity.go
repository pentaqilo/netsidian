package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"strings"
	"time"
)

type ExternalIdentityRepo interface {
	ExternalUser(context.Context, string, string) (*domain.User, error)
	CreateExternalUser(context.Context, string, string, *domain.User) error
	ExternalAuth(context.Context, string) (*ports.ExternalAuth, error)
	UpdateExternalAuth(context.Context, string, map[string]any, time.Time, bool) error
}

// ExternalLogin identifies accounts by verified issuer and subject. It never
// trusts an email claim to take over a local account. New external users get
// their own personal vault. External roles are always determined by verified
// claims, never by who happens to reach the deployment first.
func (s *Services) ExternalLogin(ctx context.Context, issuer, subject, email, name string, verifiedEmail bool, claims map[string]any, expiresAt time.Time) (*domain.User, error) {
	if issuer == "" || subject == "" {
		return nil, domain.Unauthenticated("provider returned an empty identity")
	}
	repo, ok := s.Users.(ExternalIdentityRepo)
	if !ok {
		return nil, domain.Unsupported("external identities unavailable")
	}
	lock := s.docLock("instance-setup")
	lock.Lock()
	defer lock.Unlock()
	if s.Cfg.AuthMode == "mixed" {
		count, err := s.Users.UserCount(ctx)
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, domain.Conflict("complete local administrator setup before provider sign-in")
		}
	}
	u, err := repo.ExternalUser(ctx, issuer, subject)
	if err != nil {
		return nil, err
	}
	allowed := (s.Cfg.OIDCIssuer == "" || issuer == s.Cfg.OIDCIssuer) && expiresAt.After(time.Now()) && (s.Cfg.OIDCAllowedClaims.Match(claims) || s.Cfg.OIDCAllowAllUsers)
	operator := allowed && s.Cfg.OIDCAdminClaims.Match(claims)
	if !allowed {
		// A rejected subsequent login must also withdraw existing access.
		if u != nil {
			if err := repo.UpdateExternalAuth(ctx, u.ID, claims, expiresAt, false); err != nil {
				return nil, err
			}
			if err := s.Users.RevokeUserSessions(ctx, u.ID); err != nil {
				return nil, err
			}
		}
		return nil, domain.Forbidden("your identity does not meet this instance's access policy")
	}
	// Limit the lifetime of stored claim evidence; policy changes are checked
	// on every API request and provider changes require a fresh sign-in.
	if limit := time.Now().Add(time.Hour); expiresAt.After(limit) {
		expiresAt = limit
	}
	if u != nil {
		if u.Disabled {
			return nil, domain.Unauthenticated("account is disabled")
		}
		if err := repo.UpdateExternalAuth(ctx, u.ID, claims, expiresAt, operator); err != nil {
			return nil, err
		}
		u.IsOperator = operator
		return s.ensureExternalVault(ctx, u)
	}
	if !verifiedEmail {
		email = ""
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email != "" {
		existing, e := s.Users.UserByEmail(ctx, email)
		if e != nil {
			return nil, e
		}
		if existing != nil {
			return nil, domain.Conflict("an account already uses this email; ask the operator to resolve the account conflict")
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Member"
	}
	u = &domain.User{ID: domain.NewID("usr"), Email: email, DisplayName: name, IsOperator: operator, CreatedAt: time.Now()}
	if err = repo.CreateExternalUser(ctx, issuer, subject, u); err != nil {
		return nil, err
	}
	if err := repo.UpdateExternalAuth(ctx, u.ID, claims, expiresAt, operator); err != nil {
		return nil, err
	}
	return s.ensureExternalVault(ctx, u)
}

// CheckUserAccess rechecks stored, verified claim evidence against the current
// policy for cookie and bearer identities, so restarts cannot preserve old grants.
func (s *Services) CheckUserAccess(ctx context.Context, u *domain.User) error {
	if u == nil || u.Disabled {
		return domain.Unauthenticated("account is unavailable")
	}
	repo, ok := s.Users.(ExternalIdentityRepo)
	if !ok {
		return nil
	}
	proof, err := repo.ExternalAuth(ctx, u.ID)
	if err != nil {
		return err
	}
	if proof == nil {
		if s.Cfg.AuthMode == "external" {
			return domain.Unauthenticated("use the configured sign-in provider")
		}
		return nil
	}
	if (s.Cfg.AuthMode != "external" && s.Cfg.AuthMode != "mixed") || (s.Cfg.OIDCIssuer != "" && proof.Issuer != s.Cfg.OIDCIssuer) || !proof.ExpiresAt.After(time.Now()) {
		return domain.Unauthenticated("single sign-on session expired; sign in again")
	}
	if !s.Cfg.OIDCAllowedClaims.Match(proof.Claims) && !s.Cfg.OIDCAllowAllUsers {
		return domain.Forbidden("account no longer meets the access policy")
	}
	u.IsOperator = s.Cfg.OIDCAdminClaims.Match(proof.Claims)
	return nil
}

func (s *Services) ensureExternalVault(ctx context.Context, u *domain.User) (*domain.User, error) {
	spaces, roles, err := s.Spaces.SpacesForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	hasPersonal := false
	for i, sp := range spaces {
		if sp.Kind == "personal" && roles[i] == domain.RoleOwner {
			hasPersonal = true
			break
		}
	}
	if !hasPersonal {
		_, err = s.CreateSpace(ctx, &Actor{UserID: u.ID, IsOperator: u.IsOperator}, "Personal", "personal")
	}
	return u, err
}

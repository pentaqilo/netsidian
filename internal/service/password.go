package service

import (
	"context"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// PasswordChangeResult reports the revocations performed alongside the new
// password. The policy (docs/roadmap.md, docs/authentication.md): changing a
// password revokes every session and every personal API key of the account,
// because a password change is the standard response to a suspected
// compromise and partial revocation invites silent stale credentials.
type PasswordChangeResult struct {
	SessionsRevoked int `json:"sessionsRevoked"`
	KeysRevoked     int `json:"keysRevoked"`
}

// ChangePassword rotates the caller's own local-account password after
// verifying the current one. Rules:
//   - human sessions only: API-key principals cannot rotate passwords;
//   - local-account eligibility only: provider-managed accounts (no local
//     password hash) are rejected with an explicit conflict, not silently;
//   - the current password must verify; failures reveal nothing beyond
//     "invalid credentials" and change no state;
//   - the new password must meet the same length policy as account creation
//     and must differ from the current one;
//   - when a second factor is confirmed for the account, a valid current code
//     is also required (step-up), so a stolen password alone cannot rotate the
//     credential and revoke every session;
//   - on success the hash update, session revocation, PAT revocation and the
//     password.change audit event commit atomically.
func (s *Services) ChangePassword(ctx context.Context, a *Actor, currentPassword, newPassword, code string) (*PasswordChangeResult, error) {
	if a == nil || a.Key != nil {
		return nil, domain.Forbidden("password change requires a human session")
	}
	if s.Cfg.AuthMode != "local" && s.Cfg.AuthMode != "mixed" {
		return nil, domain.Unsupported("password change requires local or mixed authentication")
	}
	if len(currentPassword) > 1<<20 || len(newPassword) > 1024 || len(newPassword) < 10 {
		return nil, domain.Invalid("current password exceeds request limit or new password must be 10–1024 UTF-8 bytes")
	}
	repo, ok := s.Users.(ports.PasswordChangeRepo)
	if !ok {
		return nil, domain.Unsupported("password change is not supported by this storage backend")
	}
	u, err := s.Users.UserByID(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	if u == nil || u.Disabled {
		return nil, domain.Unauthenticated("account is unavailable")
	}
	if u.PasswordHash == "" {
		return nil, domain.Conflict("this account signs in through the identity provider and has no local password")
	}
	if !verifyPassword(currentPassword, u.PasswordHash) {
		return nil, domain.Unauthenticated("current password is incorrect")
	}
	if newPassword == currentPassword {
		return nil, domain.Invalid("new password must differ from the current password")
	}
	var proof *ports.FactorProof
	if repo, ok := s.totpRepo(); ok {
		enr, err := repo.GetTOTPEnrollment(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		if enr != nil && enr.Confirmed {
			p, err := s.factorProof(ctx, repo, u.ID, code)
			if err != nil {
				return nil, err
			}
			proof = &p
		}
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	var sessions, keys int
	if proof != nil {
		factorRepo, ok := s.Users.(ports.PasswordFactorRepo)
		if !ok {
			return nil, domain.Unsupported("atomic factor password change unavailable")
		}
		sessions, keys, err = factorRepo.ChangePasswordWithFactor(ctx, u.ID, u.PasswordHash, hash, *proof, a.Corr)
	} else {
		sessions, keys, err = repo.ChangePassword(ctx, u.ID, u.PasswordHash, hash, a.Corr)
	}
	if err != nil {
		return nil, err
	}
	return &PasswordChangeResult{SessionsRevoked: sessions, KeysRevoked: keys}, nil
}

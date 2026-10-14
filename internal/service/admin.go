package service

import (
	"context"
	"strings"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

type AdminRepo interface {
	AdminUsers(context.Context, string, int, int) ([]ports.AdminUser, int, error)
	AdminUpdateUser(context.Context, string, string, ports.AdminChange, string) error
	AdminAudit(context.Context, int) ([]ports.AuditEvent, error)
}

func (s *Services) RequireAdministrator(ctx context.Context, a *Actor) error {
	if a == nil || a.Key != nil || s.Cfg.AuthMode == "none" {
		return domain.Forbidden("a signed-in administrator is required")
	}
	u, err := s.Users.UserByID(ctx, a.UserID)
	if err != nil {
		return err
	}
	if err = s.CheckUserAccess(ctx, u); err != nil {
		return err
	}
	if !u.IsOperator {
		return domain.Forbidden("administrator access required")
	}
	return nil
}

func (s *Services) ListAdminUsers(ctx context.Context, a *Actor, query string, offset int) ([]ports.AdminUser, int, error) {
	if err := s.RequireAdministrator(ctx, a); err != nil {
		return nil, 0, err
	}
	repo, ok := s.Users.(AdminRepo)
	if !ok {
		return nil, 0, domain.Unsupported("account administration unavailable")
	}
	if offset < 0 || offset > 1000000 || len(query) > 200 {
		return nil, 0, domain.Invalid("invalid account filter")
	}
	return repo.AdminUsers(ctx, strings.TrimSpace(query), offset, 50)
}

func (s *Services) UpdateAdminUser(ctx context.Context, a *Actor, id string, change ports.AdminChange) error {
	if err := s.RequireAdministrator(ctx, a); err != nil {
		return err
	}
	if change.Disabled == nil && change.IsOperator == nil && !change.RevokeSessions {
		return domain.Invalid("choose an account change")
	}
	repo, ok := s.Users.(AdminRepo)
	if !ok {
		return domain.Unsupported("account administration unavailable")
	}
	return repo.AdminUpdateUser(ctx, a.UserID, id, change, a.Corr)
}

func (s *Services) ListAdminAudit(ctx context.Context, a *Actor, offset int) ([]ports.AuditEvent, error) {
	if err := s.RequireAdministrator(ctx, a); err != nil {
		return nil, err
	}
	if offset < 0 || offset > 1000000 {
		return nil, domain.Invalid("invalid audit offset")
	}
	repo, ok := s.Users.(AdminRepo)
	if !ok {
		return nil, domain.Unsupported("account administration unavailable")
	}
	return repo.AdminAudit(ctx, offset)
}

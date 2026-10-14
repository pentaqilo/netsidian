package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"strings"
)

type sharingDirectory interface {
	SharingUsers(context.Context, string) ([]ports.SharingUser, error)
}

func (s *Services) SearchSharingUsers(ctx context.Context, a *Actor, spaceID, query string) ([]ports.SharingUser, error) {
	if a == nil || a.Key != nil || s.Cfg.AuthMode == "none" {
		return nil, domain.Forbidden("sharing requires a signed-in human")
	}
	if _, err := s.Require(ctx, a, spaceID, domain.ActionMembers); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 || len(query) > 200 {
		return nil, domain.Invalid("enter between 2 and 200 characters")
	}
	repo, ok := s.Users.(sharingDirectory)
	if !ok {
		return nil, domain.Unsupported("user directory unavailable")
	}
	return repo.SharingUsers(ctx, query)
}

func (s *Services) ShareWithUser(ctx context.Context, a *Actor, spaceID, userID string, role domain.Role) error {
	if a == nil || a.Key != nil || s.Cfg.AuthMode == "none" {
		return domain.Forbidden("sharing requires a signed-in human")
	}
	lock := s.docLock("members:" + spaceID)
	lock.Lock()
	defer lock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionMembers); err != nil {
		return err
	}
	if !role.Valid() {
		return domain.Invalid("invalid role")
	}
	u, err := s.Users.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	if u == nil || u.Disabled {
		return domain.NotFound("active user not found")
	}
	member, err := s.Spaces.Membership(ctx, spaceID, userID)
	if err != nil {
		return err
	}
	if member != nil {
		return domain.Conflict("user already has access; edit their role")
	}
	if err := s.Spaces.AddMember(ctx, &domain.Membership{ID: domain.NewID("mb"), SpaceID: spaceID, UserID: userID, Role: role}); err != nil {
		return err
	}
	if _, err := s.Spaces.BumpPermissionVersion(ctx, spaceID); err != nil {
		return err
	}
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "space.members"})
	return nil
}

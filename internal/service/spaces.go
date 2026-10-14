package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// AttachLocalVault retains the synchronous API contract for existing callers.
// The workspace uses StartLocalVaultLoad so large scans never hold its request.
func (s *Services) AttachLocalVault(ctx context.Context, a *Actor, root string) (*domain.Space, int, error) {
	sp, root, err := s.prepareLocalVault(ctx, a, root)
	if err != nil {
		return nil, 0, err
	}
	count, err := s.scanLocalVault(ctx, a, sp, root, nil)
	if err != nil {
		return sp, count, err
	}
	return sp, count, nil
}

func (s *Services) prepareLocalVault(ctx context.Context, a *Actor, root string) (*domain.Space, string, error) {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	if a == nil || a.Key != nil {
		return nil, "", domain.Forbidden("a local session is required")
	}
	binder, ok := s.Content.(interface {
		BindExternal(string, string) error
		UnbindExternal(string)
	})
	if !ok {
		return nil, "", domain.Unsupported("local folder storage is unavailable")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, "", err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, "", domain.Invalid("choose a real directory")
	}
	existing, _, err := s.Spaces.SpacesForUser(ctx, a.UserID)
	if err != nil {
		return nil, "", err
	}
	for _, sp := range existing {
		if sp.StorageBinding == "local-folder:"+root {
			return nil, "", domain.Conflict("this folder is already attached as a vault")
		}
	}
	sp, err := s.CreateSpace(ctx, a, filepath.Base(root), "shared")
	if err != nil {
		return nil, "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			binder.UnbindExternal(sp.ID)
			_ = s.Spaces.DeleteSpace(ctx, sp.ID)
		}
	}()
	if err = binder.BindExternal(sp.ID, root); err != nil {
		return nil, "", err
	}
	sp.StorageBinding = "local-folder:" + root
	if err = s.Spaces.SetStorageBinding(ctx, sp.ID, sp.StorageBinding); err != nil {
		return nil, "", err
	}
	cleanup = false
	return sp, root, nil
}

// CreateSpace creates a space and makes the creator its owner. Personal
// spaces are limited to one per user; shared spaces require explicit
// membership for anyone else.
func (s *Services) CreateSpace(ctx context.Context, a *Actor, name, kind string) (*domain.Space, error) {
	if a == nil || a.Key != nil {
		return nil, domain.Forbidden("space creation requires a human session")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return nil, domain.Invalid("space name required (max 120)")
	}
	if kind != "personal" {
		kind = "shared"
	}
	if kind == "personal" {
		existing, roles, err := s.Spaces.SpacesForUser(ctx, a.UserID)
		if err != nil {
			return nil, err
		}
		for i, sp := range existing {
			if sp.Kind == "personal" && roles[i] == domain.RoleOwner {
				return nil, domain.Conflict("you already have a personal space")
			}
		}
	}
	slug := Slugify(name)
	for i := 0; ; i++ {
		taken, err := s.Spaces.SlugTaken(ctx, slug)
		if err != nil {
			return nil, err
		}
		if !taken {
			break
		}
		i++
		slug = Slugify(name) + "-" + randomSuffix(i)
	}
	sp := &domain.Space{
		ID: domain.NewID("spc"), Slug: slug, Name: name, Kind: kind,
		StorageBinding: s.Cfg.StorageBinding, CreatedAt: time.Now(),
	}
	if sp.StorageBinding == "" {
		sp.StorageBinding = "local-main"
	}
	if err := s.Spaces.CreateSpace(ctx, sp); err != nil {
		return nil, err
	}
	if err := s.Spaces.AddMember(ctx, &domain.Membership{
		ID: domain.NewID("mb"), SpaceID: sp.ID, UserID: a.UserID, Role: domain.RoleOwner,
	}); err != nil {
		_ = s.Spaces.DeleteSpace(ctx, sp.ID)
		return nil, err
	}
	return sp, nil
}

func randomSuffix(i int) string {
	const d = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 3)
	v := i*7919 + 104729
	for j := range b {
		v = v*1103515245 + 12345
		b[j] = d[(v>>13)&31]
	}
	return string(b)
}

func (s *Services) ListSpaces(ctx context.Context, a *Actor) ([]*SpaceWithRole, error) {
	spaces, roles, err := s.Spaces.SpacesForUser(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]*SpaceWithRole, 0, len(spaces))
	for i, sp := range spaces {
		if a.Key != nil {
			permitted := false
			for _, id := range a.Key.SpaceIDs {
				if id == sp.ID {
					permitted = true
				}
			}
			if !permitted {
				continue
			}
		}
		n, err := s.countMembers(ctx, sp.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, &SpaceWithRole{Space: sp, Role: roles[i], MemberCount: n})
	}
	return out, nil
}

type SpaceWithRole struct {
	Space       *domain.Space `json:"space"`
	Role        domain.Role   `json:"role"`
	MemberCount int           `json:"memberCount"`
}

func (s *Services) countMembers(ctx context.Context, spaceID string) (int, error) {
	members, err := s.Spaces.Members(ctx, spaceID)
	if err != nil {
		return 0, err
	}
	return len(members), nil
}

// SpaceDetail returns space + members + pending invitations for owners
// (members may see members, invitations are owner-only per UI-06).
type SpaceDetail struct {
	Space       *domain.Space           `json:"space"`
	Role        domain.Role             `json:"role"`
	Members     []*ports.MemberWithUser `json:"members"`
	Invitations []*ports.Invitation     `json:"invitations,omitempty"`
}

func (s *Services) GetSpaceDetail(ctx context.Context, a *Actor, spaceID string) (*SpaceDetail, error) {
	role, err := s.Require(ctx, a, spaceID, domain.ActionRead)
	if err != nil {
		return nil, err
	}
	sp, err := s.Spaces.Space(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	if a.Key != nil {
		return &SpaceDetail{Space: sp, Role: role, Members: []*ports.MemberWithUser{}}, nil
	}
	members, err := s.Spaces.Members(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	d := &SpaceDetail{Space: sp, Role: role, Members: members}
	if role == domain.RoleOwner {
		invs, err := s.Invites.InvitationsForSpace(ctx, spaceID)
		if err != nil {
			return nil, err
		}
		for _, iv := range invs {
			iv.TokenHash = "" // never leak token material
		}
		d.Invitations = invs
	}
	return d, nil
}

// Invite creates an invitation. Success means the server created the
// invitation; delivery state is separate (no email transport in P0, the
// invite link is returned to the owner).
func (s *Services) Invite(ctx context.Context, a *Actor, spaceID, email string, role domain.Role) (inviteURL string, inv *ports.Invitation, err error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionMembers); err != nil {
		return "", nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return "", nil, domain.Invalid("valid email required")
	}
	if !role.Valid() || role == "" {
		role = domain.RoleViewer
	}
	if role == domain.RoleOwner {
		return "", nil, domain.Invalid("ownership is transferred, not invited")
	}
	token, err := randomToken(24)
	if err != nil {
		return "", nil, err
	}
	inv = &ports.Invitation{
		ID: domain.NewID("inv"), SpaceID: spaceID, Email: email, Role: role,
		TokenHash: hashToken(token), IssuedBy: a.UserID,
		ExpiresAt: time.Now().Add(14 * 24 * time.Hour), CreatedAt: time.Now(),
	}
	if err := s.Invites.CreateInvitation(ctx, inv); err != nil {
		return "", nil, err
	}
	return "/invite?token=" + token, inv, nil
}

// AcceptInvitation joins the current user to the invited space.
func (s *Services) AcceptInvitation(ctx context.Context, a *Actor, token string) (*domain.Space, error) {
	if a == nil || a.Key != nil {
		return nil, domain.Forbidden("invitation acceptance requires a human session")
	}
	inv, err := s.Invites.InvitationByTokenHash(ctx, hashToken(token))
	if err != nil {
		return nil, err
	}
	if inv == nil || inv.ConsumedAt != nil || inv.RevokedAt != nil || time.Now().After(inv.ExpiresAt) {
		return nil, domain.NotFound("invitation invalid or expired")
	}
	u, err := s.Users.UserByID(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	if u == nil || !strings.EqualFold(u.Email, inv.Email) {
		return nil, domain.Forbidden("sign in with the invited email address")
	}
	existing, err := s.Spaces.Membership(ctx, inv.SpaceID, a.UserID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		if err := s.Spaces.AddMember(ctx, &domain.Membership{
			ID: domain.NewID("mb"), SpaceID: inv.SpaceID, UserID: a.UserID, Role: inv.Role,
		}); err != nil {
			return nil, err
		}
	}
	if err := s.Invites.ConsumeInvitation(ctx, inv.ID, time.Now()); err != nil {
		return nil, err
	}
	if _, err := s.Spaces.BumpPermissionVersion(ctx, inv.SpaceID); err != nil {
		return nil, err
	}
	s.PublishEvent(&domain.Event{SpaceID: inv.SpaceID, EventType: "space.members"})
	return s.Spaces.Space(ctx, inv.SpaceID)
}

// SetMemberRole changes a role with last-owner protection.
func (s *Services) SetMemberRole(ctx context.Context, a *Actor, spaceID, memberID string, role domain.Role) error {
	lock := s.docLock("members:" + spaceID)
	lock.Lock()
	defer lock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionMembers); err != nil {
		return err
	}
	if !role.Valid() {
		return domain.Invalid("invalid role")
	}
	members, err := s.Spaces.Members(ctx, spaceID)
	if err != nil {
		return err
	}
	var target *ports.MemberWithUser
	ownerCount := 0
	for _, m := range members {
		if m.Membership.ID == memberID {
			target = m
		}
		if m.Role == domain.RoleOwner {
			ownerCount++
		}
	}
	if target == nil {
		return domain.NotFound("member not found")
	}
	if target.Role == domain.RoleOwner && role != domain.RoleOwner && ownerCount <= 1 {
		return domain.Conflict("cannot demote the last owner without a transfer")
	}
	if err := s.Spaces.SetMemberRole(ctx, spaceID, memberID, role); err != nil {
		return err
	}
	if _, err := s.Spaces.BumpPermissionVersion(ctx, spaceID); err != nil {
		return err
	}
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "space.members"})
	return nil
}

// RemoveMember revokes membership with last-owner protection.
func (s *Services) RemoveMember(ctx context.Context, a *Actor, spaceID, memberID string) error {
	lock := s.docLock("members:" + spaceID)
	lock.Lock()
	defer lock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionMembers); err != nil {
		return err
	}
	members, err := s.Spaces.Members(ctx, spaceID)
	if err != nil {
		return err
	}
	var target *ports.MemberWithUser
	ownerCount := 0
	for _, m := range members {
		if m.Membership.ID == memberID {
			target = m
		}
		if m.Role == domain.RoleOwner {
			ownerCount++
		}
	}
	if target == nil {
		return domain.NotFound("member not found")
	}
	if target.Role == domain.RoleOwner && ownerCount <= 1 {
		return domain.Conflict("cannot remove the last owner")
	}
	if err := s.Spaces.RemoveMember(ctx, spaceID, memberID); err != nil {
		return err
	}
	if _, err := s.Spaces.BumpPermissionVersion(ctx, spaceID); err != nil {
		return err
	}
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "space.members"})
	return nil
}

// ArchiveSpace marks a space archived (owner only).
func (s *Services) ArchiveSpace(ctx context.Context, a *Actor, spaceID string, archived bool) error {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionSpaceManage); err != nil {
		return err
	}
	sp, err := s.Spaces.Space(ctx, spaceID)
	if err != nil {
		return err
	}
	if sp.Kind == "personal" && archived {
		return domain.Conflict("personal spaces cannot be archived")
	}
	if archived {
		now := time.Now()
		return s.Spaces.ArchiveSpace(ctx, spaceID, &now)
	}
	return s.Spaces.ArchiveSpace(ctx, spaceID, nil)
}

// DeleteVault permanently removes a managed vault. An attached desktop
// folder is only unlinked; its original files are never removed.
func (s *Services) DeleteVault(ctx context.Context, a *Actor, spaceID, confirmation string) error {
	if a == nil || a.Key != nil {
		return domain.Forbidden("vault deletion requires a human session")
	}
	role, err := s.Role(ctx, a, spaceID)
	if err != nil {
		return err
	}
	if !domain.RoleCan(role, domain.ActionSpaceManage) {
		return domain.Forbidden("vault owner required")
	}
	sp, err := s.Spaces.Space(ctx, spaceID)
	if err != nil {
		return err
	}
	if sp == nil {
		return domain.NotFound("vault not found")
	}
	if confirmation != sp.Name {
		return domain.Invalid("type the vault name to confirm deletion")
	}
	lock := s.spaceMu(spaceID)
	lock.Lock()
	defer lock.Unlock()
	s.cancelVaultLoad(spaceID)
	if _, err := s.Require(ctx, a, spaceID, domain.ActionSpaceManage); err != nil {
		return err
	}
	sp, err = s.Spaces.Space(ctx, spaceID)
	if err != nil {
		return err
	}
	if sp == nil {
		return domain.NotFound("vault not found")
	}
	if confirmation != sp.Name {
		return domain.Invalid("type the vault name to confirm deletion")
	}
	if purger, ok := s.Content.(interface{ PurgeSpace(string) error }); ok {
		if err := purger.PurgeSpace(spaceID); err != nil {
			return err
		}
	}
	if purger, ok := s.State.(interface{ PurgeSpace(string) error }); ok {
		if err := purger.PurgeSpace(spaceID); err != nil {
			return err
		}
	}
	if err := s.Spaces.DeleteSpace(ctx, spaceID); err != nil {
		return err
	}
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "space.deleted"})
	return nil
}

// RenameSpace changes a vault's display name. Ownership-level action
// (space_manage); members keep access; IDs, paths and storage never
// change. Personal vaults may also be renamed by their owner.
func (s *Services) RenameSpace(ctx context.Context, a *Actor, spaceID, name string) (*domain.Space, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionSpaceManage); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return nil, domain.Invalid("vault name must be 1-120 characters")
	}
	if err := s.Spaces.RenameSpace(ctx, spaceID, name); err != nil {
		return nil, err
	}
	sp, err := s.Spaces.Space(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "space.updated"})
	return sp, nil
}

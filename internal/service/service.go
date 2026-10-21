// Package service contains the shared Go domain services used by both
// the HTTP transport and native Wails bridge. Services receive an
// explicit Actor; a browser-supplied user ID is never authorization.
package service

import (
	"context"
	"strings"
	"sync"

	"github.com/pentaqilo/netsidian/internal/authpolicy"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// Actor is the trusted identity resolved by the transport layer:
// session user (hosted), key principal (integrations) or the local
// principal resolved in Go (native bridge).
type Actor struct {
	UserID          string
	DisplayName     string
	IsOperator      bool
	Key             *ports.APIKeyRecord // nil for human sessions
	Local           bool                // trusted local principal (native)
	Corr            string              // X-Correlation-Id for audit joins
	ExecutionTarget string              // web|desktop|mobile
}

func (a *Actor) Human() bool { return a.Key == nil }

// Services bundles the shared services and adapters.
type Services struct {
	RefreshExternalAccess func(context.Context, string) error
	Cfg                   Config
	Users                 ports.UserRepo
	Spaces                ports.SpaceRepo
	Invites               ports.InvitationRepo
	Docs                  ports.DocumentRepo
	Index                 ports.IndexRepo
	Mem                   ports.MemoryRepo
	Prefs                 ports.PreferenceRepo
	Content               ports.ContentStore
	State                 ports.StateStore
	// Seal encrypts second-factor secrets at rest with the instance key.
	// providers.Manager satisfies it; nil means sealing is unavailable and
	// second-factor enrollment is refused rather than storing plaintext.
	Seal ports.Sealer

	mu        sync.Mutex
	locks     map[string]*sync.Mutex // per-document writer locks
	memoryOps map[string]*sync.Mutex // serializes memory index workflows, outside document locks
	spaceOps  map[string]*sync.Mutex // per-space mutation serialization
	hubs      map[string]*EventHub   // per-space in-process fanout
	PermVer   map[string]int64
	attachMu  sync.Mutex
	loads     map[string]*vaultLoadJob
	loadSlots chan struct{}
}

// Config is the subset of instance configuration services need.
type Config struct {
	StorageBinding string

	AuthMode           string
	SignupEnabled      bool
	OIDCIssuer         string
	OIDCAllowedClaims  authpolicy.Rules
	OIDCAdminClaims    authpolicy.Rules
	OIDCAllowAllUsers  bool
	MaxNoteBytes       int64
	MaxAttachBytes     int64
	TrashRetentionDays int
}

func NewServices(cfg Config, users ports.UserRepo, spaces ports.SpaceRepo, invites ports.InvitationRepo,
	docs ports.DocumentRepo, index ports.IndexRepo, mem ports.MemoryRepo, prefs ports.PreferenceRepo,
	content ports.ContentStore, state ports.StateStore) *Services {
	return &Services{
		Cfg: cfg, Users: users, Spaces: spaces, Invites: invites, Docs: docs,
		Index: index, Mem: mem, Prefs: prefs, Content: content, State: state,
		locks:     map[string]*sync.Mutex{},
		spaceOps:  map[string]*sync.Mutex{},
		memoryOps: map[string]*sync.Mutex{},
		hubs:      map[string]*EventHub{},
		loads:     map[string]*vaultLoadJob{},
		loadSlots: make(chan struct{}, 1),
	}
}

// spaceMu obtains the per-space mutation lock. Every operation that mixes
// storage renames with document metadata changes (save, create, rename,
// trash, restore, attachment writes, folder rename/trash/restore) holds it
// for its whole critical section, so folder operations can never observe
// or leave a half-applied document state. Lock order is always
// spaceMu before docLock; no code path may take spaceMu while holding a
// docLock.
func (s *Services) spaceMu(spaceID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.spaceOps[spaceID]
	if !ok {
		l = &sync.Mutex{}
		s.spaceOps[spaceID] = l
	}
	return l
}

// docLock obtains the per-document writer lock.
func (s *Services) docLock(docID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.locks[docID]
	if !ok {
		l = &sync.Mutex{}
		s.locks[docID] = l
	}
	return l
}

// Hub returns the live event fanout for a space.
func (s *Services) Hub(spaceID string) *EventHub {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.hubs[spaceID]
	if !ok {
		h = NewEventHub()
		s.hubs[spaceID] = h
	}
	return h
}

// Role resolves the actor's current role in a space. Membership is the
// single source of truth; keys must additionally cover the space.
func (s *Services) Role(ctx context.Context, a *Actor, spaceID string) (domain.Role, error) {
	sp, err := s.Spaces.Space(ctx, spaceID)
	if err != nil {
		return "", err
	}
	if sp == nil {
		// do not let callers discover unknown spaces
		return "", domain.NotFound("space not found")
	}
	m, err := s.Spaces.Membership(ctx, spaceID, a.UserID)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", domain.NotFound("space not found") // hide non-member existence
	}
	return m.Role, nil
}

// Require authorizes an action for the actor against a space, enforcing
// the role matrix and, for API keys, the key space grants + scopes.
func (s *Services) Require(ctx context.Context, a *Actor, spaceID string, action domain.Action) (domain.Role, error) {
	role, err := s.Role(ctx, a, spaceID)
	if err != nil {
		return "", err
	}
	if !domain.RoleCan(role, action) {
		return role, domain.Forbidden("role " + string(role) + " may not perform this action")
	}
	if action != domain.ActionRead && action != domain.ActionSearch && s.vaultLoading(spaceID) {
		return role, domain.Conflict("vault is loading; you can browse notes while indexing finishes")
	}
	if a.Key != nil {
		inSpace := false
		for _, id := range a.Key.SpaceIDs {
			if id == spaceID {
				inSpace = true
				break
			}
		}
		if !inSpace {
			return role, domain.Forbidden("key is not bound to this space")
		}
		// an empty scope list means the key inherits the holder's full
		// role within its bound spaces; listing scopes restricts it
		if len(a.Key.Scopes) > 0 {
			scope := domain.ScopeForAction(action)
			if scope != "" && !hasScope(a.Key.Scopes, scope) {
				return role, domain.Forbidden("key lacks scope " + scope)
			}
		}
	}
	return role, nil
}

// CanRead gates read-shaped access and additionally requires
// documents:read for key callers when the action is content reads.
func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// slugify derives a URL-safe space slug.
func Slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
			prevDash = r == '-'
		case r == ' ' || r == '_' || r == '.':
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "space"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

// requireReadScope authorizes read-only specialized surfaces without requiring
// document-content permission (memory and graph have their own scopes).
func (s *Services) requireReadScope(ctx context.Context, a *Actor, spaceID, scope string) (domain.Role, error) {
	role, err := s.Role(ctx, a, spaceID)
	if err != nil {
		return role, err
	}
	if !domain.RoleCan(role, domain.ActionRead) {
		return role, domain.Forbidden("vault read permission required")
	}
	if a.Key != nil {
		bound := false
		for _, id := range a.Key.SpaceIDs {
			if id == spaceID {
				bound = true
				break
			}
		}
		if !bound {
			return role, domain.Forbidden("key is not bound to this space")
		}
	}
	if a.Key != nil && len(a.Key.Scopes) > 0 && !hasScope(a.Key.Scopes, scope) {
		return role, domain.Forbidden("key lacks scope " + scope)
	}
	return role, nil
}

package service

import (
	"context"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// Settings is the public instance descriptor served at GET /api/v1/settings.
type Settings struct {
	AuthMode      string `json:"authMode"`      // none|local
	NeedsSetup    bool   `json:"needsSetup"`    // local mode with zero users
	SignupEnabled bool   `json:"signupEnabled"` // open local registration
	Version       string `json:"version"`
	Features      struct {
		Graph   bool `json:"graph"`
		Memory  bool `json:"memory"`
		Search  bool `json:"search"`
		Connect bool `json:"connect"` // key management available
	} `json:"features"`
}

func (s *Services) GetSettings(ctx context.Context) (*Settings, error) {
	st := &Settings{AuthMode: s.Cfg.AuthMode, Version: Version, SignupEnabled: s.Cfg.SignupEnabled && (s.Cfg.AuthMode == "local" || s.Cfg.AuthMode == "mixed")}
	st.Features.Graph = true
	st.Features.Memory = false
	st.Features.Search = true
	st.Features.Connect = true
	if s.Cfg.AuthMode == "local" || s.Cfg.AuthMode == "mixed" {
		n, err := s.Users.UserCount(ctx)
		if err != nil {
			return nil, err
		}
		st.NeedsSetup = n == 0
	}
	return st, nil
}

// Version is the build version reported to clients.
const Version = "0.4.0"

// ---------- preferences ----------

// GetPreferences returns the caller's UI preferences (open tabs, theme…).
func (s *Services) GetPreferences(ctx context.Context, a *Actor) (map[string]any, error) {
	p, err := s.Prefs.Preferences(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		p = map[string]any{}
	}
	return p, nil
}

// SetPreferences replaces the caller's preferences document. Per-vault
// workspace state (phase 2, C01) is merged back from the stored copy:
// it is managed exclusively through the workspace API and a whole-doc
// preferences replacement must never drop it.
func (s *Services) SetPreferences(ctx context.Context, a *Actor, prefs map[string]any) error {
	if len(prefs) > 64 {
		return domain.Invalid("too many preference keys")
	}
	lock := s.docLock("prefs:" + a.UserID)
	lock.Lock()
	defer lock.Unlock()
	existing, err := s.Prefs.Preferences(ctx, a.UserID)
	if err == nil {
		for k, v := range existing {
			if strings.HasPrefix(k, vaultKeyPrefix) {
				if _, replaced := prefs[k]; !replaced {
					prefs[k] = v
				}
			}
		}
	}
	return s.Prefs.SetPreferences(ctx, a.UserID, prefs)
}

// ---------- key management ----------

type KeyMeta struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Scopes     []string   `json:"scopes"`
	SpaceIDs   []string   `json:"spaceIds"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func (s *Services) ListKeys(ctx context.Context, a *Actor) ([]*KeyMeta, error) {
	if a.Key != nil {
		return nil, domain.Forbidden("key management requires a human session")
	}
	keys, err := s.Users.ListKeys(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]*KeyMeta, 0, len(keys))
	for _, k := range keys {
		out = append(out, &KeyMeta{ID: k.ID, Label: k.Label, Scopes: k.Scopes,
			SpaceIDs: k.SpaceIDs, ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt,
			RevokedAt: k.RevokedAt, CreatedAt: k.CreatedAt})
	}
	return out, nil
}

// RevokeKey revokes the caller's own key immediately.
func (s *Services) RevokeKey(ctx context.Context, a *Actor, keyID string) error {
	if a.Key != nil {
		return domain.Forbidden("key management requires a human session")
	}
	k, err := s.Users.KeyByID(ctx, keyID)
	if err != nil {
		return err
	}
	if k == nil || k.PrincipalID != a.UserID {
		return domain.NotFound("key not found")
	}
	return s.Users.RevokeKey(ctx, keyID)
}

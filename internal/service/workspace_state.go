package service

import (
	"context"
	"encoding/json"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// Workspace state is per-user, per-vault (space) UI restoration data:
// open tabs, active document, tree expansion, pane widths, and graph
// scope. It is distinct from appearance (a user-level preference) and
// it is scoped to exactly one vault so results from one vault can never
// repaint another. Authorization is enforced server-side: a caller must
// have read access to the vault to read or write its workspace state.
//
// The state is namespaced inside the caller's preferences document
// under a per-vault key, reusing the existing durable preferences store
// while keeping the two concepts separate in the API and code.

// WorkspaceState is the persisted per-vault UI restoration blob.
type WorkspaceState struct {
	OpenDocs        []string       `json:"openDocs"`
	ActiveDoc       string         `json:"activeDoc"`
	ExpandedFolders []string       `json:"expandedFolders"`
	PaneWidths      map[string]int `json:"paneWidths,omitempty"`
	GraphScope      map[string]any `json:"graphScope,omitempty"`
	Custom          map[string]any `json:"custom,omitempty"`
}

// vaultKey namespaces workspace state inside the preferences document.
const vaultKeyPrefix = "workspace:"

func workspacePrefKey(spaceID string) string { return vaultKeyPrefix + spaceID }

// GetWorkspace returns the caller's saved workspace state for one vault,
// or an empty state when none is stored. Requires read access to the
// vault; a caller who loses membership can no longer read another
// vault's state even if it still exists on disk.
func (s *Services) GetWorkspace(ctx context.Context, a *Actor, spaceID string) (*WorkspaceState, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	prefs, err := s.Prefs.Preferences(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	raw, ok := prefs[workspacePrefKey(spaceID)]
	if !ok {
		return &WorkspaceState{}, nil
	}
	// Preferences are stored as a generic map; re-marshal through JSON to
	// reach the typed shape regardless of the concrete map types.
	out := &WorkspaceState{}
	buf, err := json.Marshal(raw)
	if err != nil {
		return &WorkspaceState{}, nil // corrupt entry degrades to empty
	}
	if err := json.Unmarshal(buf, out); err != nil {
		return &WorkspaceState{}, nil
	}
	return out, nil
}

// SetWorkspace persists the caller's workspace state for one vault.
// It merges into the preferences document without clobbering other
// per-vault keys or top-level preferences. Requires write-capable
// membership (edit) so a viewer cannot mutate workspace state.
func (s *Services) SetWorkspace(ctx context.Context, a *Actor, spaceID string, st *WorkspaceState) error {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return err
	}
	if st == nil {
		return domain.Invalid("workspace state is required")
	}
	if len(st.OpenDocs) > 256 || len(st.ExpandedFolders) > 512 {
		return domain.Invalid("workspace state is too large")
	}
	// Guard against cross-vault document ids: every open/active id must
	// resolve inside this same vault or be dropped; never leak another
	// vault's document here.
	if st.ActiveDoc != "" && !s.docInVault(ctx, spaceID, st.ActiveDoc) {
		st.ActiveDoc = ""
	}
	filtered := st.OpenDocs[:0]
	for _, id := range st.OpenDocs {
		if s.docInVault(ctx, spaceID, id) {
			filtered = append(filtered, id)
		}
	}
	st.OpenDocs = filtered

	lock := s.docLock("prefs:" + a.UserID)
	lock.Lock()
	defer lock.Unlock()
	prefs, err := s.Prefs.Preferences(ctx, a.UserID)
	if err != nil {
		return err
	}
	if prefs == nil {
		prefs = map[string]any{}
	}
	// Store a JSON-normalized copy so reads never see stale field types.
	buf, err := json.Marshal(st)
	if err != nil {
		return domain.Invalid("workspace state could not be encoded")
	}
	var normalized any
	if err := json.Unmarshal(buf, &normalized); err != nil {
		return domain.Invalid("workspace state could not be encoded")
	}
	prefs[workspacePrefKey(spaceID)] = normalized
	return s.Prefs.SetPreferences(ctx, a.UserID, prefs)
}

// docInVault reports whether docID currently exists inside spaceID
// (not trashed). Used to keep restored workspace tabs from referencing
// documents from a different or deleted vault.
func (s *Services) docInVault(ctx context.Context, spaceID, docID string) bool {
	doc, err := s.Docs.Document(ctx, docID)
	return err == nil && doc != nil && doc.SpaceID == spaceID && doc.TombstoneAt == nil
}

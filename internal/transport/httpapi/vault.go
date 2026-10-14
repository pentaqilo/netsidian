package httpapi

import (
	"archive/zip"
	"net/http"
	"strings"
	"unicode"

	"github.com/pentaqilo/netsidian/internal/service"
)

// handleSpaceRename renames a vault (owner-level). Identity is
// untouched; only the display name changes.
func (s *Server) handleSpaceRename(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	sp, err := s.Svc.RenameSpace(r.Context(), actorOf(r), spaceID(r), in.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"space": sp})
}

// handleWorkspaceGet returns the caller's saved workspace state for the
// vault (per-user, per-vault restoration data; phase 2 P2-V04).
func (s *Server) handleWorkspaceGet(w http.ResponseWriter, r *http.Request) {
	st, err := s.Svc.GetWorkspace(r.Context(), actorOf(r), spaceID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, st)
}

// handleWorkspacePut persists the caller's workspace state for the
// vault. Server-side membership and document-ownership checks apply.
func (s *Server) handleWorkspacePut(w http.ResponseWriter, r *http.Request) {
	var st service.WorkspaceState
	if !decode(w, r, &st) {
		return
	}
	if err := s.Svc.SetWorkspace(r.Context(), actorOf(r), spaceID(r), &st); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleVaultExport streams the vault as a zip of ordinary folders and
// files plus a separable metadata manifest (P2-V05). Authorization is
// verified before the first byte.
func (s *Server) handleVaultExport(w http.ResponseWriter, r *http.Request) {
	sp, err := s.Svc.VaultForExport(r.Context(), actorOf(r), spaceID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeExportName(sp.Name)+`-export.zip"`)
	zw := zip.NewWriter(w)
	if _, err := s.Svc.ExportVault(r.Context(), actorOf(r), spaceID(r), zw); err != nil {
		return // stream aborted; the client sees a truncated archive
	}
	_ = zw.Close()
}

// safeExportName renders a safe ASCII zip download name.
func safeExportName(name string) string {
	var b strings.Builder
	for _, rn := range name {
		switch {
		case unicode.IsLetter(rn) || unicode.IsDigit(rn) || rn == '-':
			b.WriteRune(rn)
		case rn == ' ' || rn == '_' || rn == '.':
			b.WriteRune('-')
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	if out == "" || out == "-" {
		return "vault"
	}
	return strings.Trim(out, "-")
}

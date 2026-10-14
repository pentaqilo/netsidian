package httpapi

import (
	"net/http"
	"strconv"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func adminOffset(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("offset")
	if raw == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 || offset > 1000000 {
		return 0, domain.Invalid("invalid offset")
	}
	return offset, nil
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	offset, err := adminOffset(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	users, total, err := s.Svc.ListAdminUsers(r.Context(), actorOf(r), r.URL.Query().Get("q"), offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"users": users, "total": total, "offset": offset, "limit": 50})
}

func (s *Server) handleAdminUserUpdate(w http.ResponseWriter, r *http.Request) {
	// Authorize before decoding or returning account-specific information.
	if err := s.Svc.RequireAdministrator(r.Context(), actorOf(r)); err != nil {
		writeErr(w, err)
		return
	}
	var change ports.AdminChange
	if !decode(w, r, &change) {
		return
	}
	if err := s.Svc.UpdateAdminUser(r.Context(), actorOf(r), r.PathValue("id"), change); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	offset, err := adminOffset(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	events, err := s.Svc.ListAdminAudit(r.Context(), actorOf(r), offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []map[string]any{}
	for _, event := range events {
		out = append(out, map[string]any{"id": event.ID, "actor": event.Actor, "action": event.Action, "resource": event.Resource, "outcome": event.Outcome, "details": event.Details, "createdAt": event.CreatedAt})
	}
	writeJSON(w, 200, map[string]any{"events": out, "offset": offset, "limit": 50})
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if err := s.Svc.RequireAdministrator(r.Context(), actorOf(r)); err != nil {
		writeErr(w, err)
		return
	}
	cfg := s.Svc.Cfg
	writeJSON(w, 200, map[string]any{"authMode": cfg.AuthMode, "signupEnabled": cfg.SignupEnabled && (cfg.AuthMode == "local" || cfg.AuthMode == "mixed"), "oidcIssuer": cfg.OIDCIssuer, "oidcAllowedClaims": cfg.OIDCAllowedClaims, "oidcAdminClaims": cfg.OIDCAdminClaims, "oidcAllowAllUsers": cfg.OIDCAllowAllUsers, "externalSessionMinutes": 60})
}

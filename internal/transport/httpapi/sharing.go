package httpapi

import (
	"github.com/pentaqilo/netsidian/internal/domain"
	"net/http"
)

func (s *Server) handleSharingUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Svc.SearchSharingUsers(r.Context(), actorOf(r), spaceID(r), r.URL.Query().Get("q"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"users": users})
}
func (s *Server) handleShareWithUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID string      `json:"userId"`
		Role   domain.Role `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.Svc.ShareWithUser(r.Context(), actorOf(r), spaceID(r), in.UserID, in.Role); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]bool{"ok": true})
}

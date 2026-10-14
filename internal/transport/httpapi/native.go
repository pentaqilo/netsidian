package httpapi

import (
	"net/http"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func (s *Server) handleNativeAttach(w http.ResponseWriter, r *http.Request) {
	if s.NativePick == nil {
		writeErr(w, domain.NotFound("native picker unavailable"))
		return
	}
	root, err := s.NativePick()
	if err != nil {
		writeErr(w, err)
		return
	}
	if root == "" {
		writeJSON(w, 200, map[string]bool{"cancelled": true})
		return
	}
	if r.URL.Query().Get("background") == "true" {
		space, err := s.Svc.StartLocalVaultLoad(r.Context(), actorOf(r), root)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 202, map[string]any{"space": space, "role": "owner", "documents": 0, "loading": true})
		return
	}
	space, count, err := s.Svc.AttachLocalVault(r.Context(), actorOf(r), root)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"space": space, "role": "owner", "documents": count})
}

func (s *Server) handleVaultLoad(w http.ResponseWriter, r *http.Request) {
	load, err := s.Svc.VaultLoadStatus(r.Context(), actorOf(r), spaceID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"load": load})
}
func (s *Server) handleVaultLoadRetry(w http.ResponseWriter, r *http.Request) {
	if err := s.Svc.RetryVaultLoad(r.Context(), actorOf(r), spaceID(r)); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]bool{"loading": true})
}

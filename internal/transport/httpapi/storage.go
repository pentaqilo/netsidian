package httpapi

import (
	"github.com/pentaqilo/netsidian/internal/ports"
	"net/http"
)

func (s *Server) handleStorageOptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"providers": s.Svc.StorageOptions(), "base": s.Svc.Cfg.StorageBinding})
}
func (s *Server) handleStorageGet(w http.ResponseWriter, r *http.Request) {
	v, e := s.Svc.VaultStorage(r.Context(), actorOf(r), spaceID(r))
	if e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) handleStoragePut(w http.ResponseWriter, r *http.Request) {
	var v ports.StorageConfig
	if !decode(w, r, &v) {
		return
	}
	if e := s.Svc.ConfigureVaultStorage(r.Context(), actorOf(r), spaceID(r), v); e != nil {
		writeErr(w, e)
		return
	}
	s.handleStorageGet(w, r)
}

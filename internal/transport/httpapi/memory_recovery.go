package httpapi

import (
	"net/http"
	"strconv"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func (s *Server) handleMemoryOperation(w http.ResponseWriter, r *http.Request) {
	if !s.agentAllow(w, r) {
		return
	}
	var err error
	maxBytes := 0
	if raw := r.URL.Query().Get("maxBytes"); raw != "" {
		maxBytes, err = strconv.Atoi(raw)
		if err != nil {
			writeErr(w, domain.Invalid("maxBytes must be an integer"))
			return
		}
	}
	includePlan := false
	if raw := r.URL.Query().Get("includePlan"); raw != "" {
		includePlan, err = strconv.ParseBool(raw)
		if err != nil {
			writeErr(w, domain.Invalid("includePlan must be a boolean"))
			return
		}
	}
	result, err := s.Svc.MemoryOperationStatus(r.Context(), actorOf(r), spaceID(r), r.URL.Query().Get("operationId"), includePlan, maxBytes)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

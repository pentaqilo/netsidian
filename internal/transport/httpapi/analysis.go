package httpapi

import (
	"net/http"
	"strconv"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// handleGraphAnalysis serves the separate versioned analysis route (phase-2
// contract C02). The legacy GET /graph response shape stays untouched.
func (s *Server) handleGraphAnalysis(w http.ResponseWriter, r *http.Request) {
	q := &ports.GraphQuery{
		Query:     r.URL.Query().Get("query"),
		Scope:     r.URL.Query().Get("scope"),
		ScopeIDs:  r.URL.Query()["scopeId"],
		ScopePath: r.URL.Query().Get("scopePath"),
		Mode:      r.URL.Query().Get("mode"),
		Direction: r.URL.Query().Get("direction"),
		Depth:     -1, // absent means the per-scope default
	}
	if v := r.URL.Query().Get("depth"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeErr(w, domain.Invalid("depth must be an integer"))
			return
		}
		q.Depth = n
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeErr(w, domain.Invalid("limit must be an integer"))
			return
		}
		q.Limit = n
	}
	g, err := s.Svc.GraphAnalysis(r.Context(), actorOf(r), spaceID(r), q)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, g)
}

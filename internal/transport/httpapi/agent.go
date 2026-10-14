package httpapi

// Agent data interface (review/agent-memory/PLAN.md): machine-facing
// endpoints over the same governed service layer humans use — capability
// discovery, budgeted context retrieval and a per-vault change feed. All
// handlers enforce the actor's role+scope authorization via the service;
// the extra per-principal limiter below protects the retrieval surface
// from agent loops, which the mutation-only wrap() limiter does not cover.

import (
	"net/http"
	"strings"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
)

// agentAllow applies the agent-surface budget to every agent endpoint
// call, reads and writes alike, keyed per principal.
func (s *Server) agentAllow(w http.ResponseWriter, r *http.Request) bool {
	a := actorOf(r)
	if a == nil {
		return true // wrap() already rejected
	}
	if !s.limiter.allow("agent:" + a.UserID) {
		writeErr(w, domain.RateLimited("agent surface rate limit exceeded"))
		return false
	}
	return true
}

func (s *Server) handleAgentWhoami(w http.ResponseWriter, r *http.Request) {
	if !s.agentAllow(w, r) {
		return
	}
	who, err := s.Svc.AgentWhoami(r.Context(), actorOf(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, who)
}

func (s *Server) handleAgentContext(w http.ResponseWriter, r *http.Request) {
	if !s.agentAllow(w, r) {
		return
	}
	var req service.AgentContextRequest
	if !decode(w, r, &req) {
		return
	}
	resp, err := s.Svc.AgentContext(r.Context(), actorOf(r), spaceID(r), &req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, resp)
}

func (s *Server) handleChangesSince(w http.ResponseWriter, r *http.Request) {
	if !s.agentAllow(w, r) {
		return
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	limit := intQuery(r, "limit", 100)
	if limit < 1 || limit > 500 {
		writeErr(w, domain.Invalid("limit must be between 1 and 500"))
		return
	}
	changes, err := s.Svc.ChangesSince(r.Context(), actorOf(r), spaceID(r), cursor, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, changes)
}

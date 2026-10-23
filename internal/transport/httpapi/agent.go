package httpapi

// Agent data interface (review/agent-memory/PLAN.md): machine-facing
// endpoints over the same governed service layer humans use — capability
// discovery, budgeted context retrieval and a per-vault change feed. All
// handlers enforce the actor's role+scope authorization via the service;
// the extra per-principal limiter below protects the retrieval surface
// from agent loops, which the mutation-only wrap() limiter does not cover.

import (
	"net/http"
	"strconv"
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

// ---- agent memory surface ----
// REST mirrors of the governed memory services so non-MCP agents get
// the same typed recall filters, guarded revisions and resumable
// consolidation. Idempotency-Key replays identical requests.

func (s *Server) handleMemoryRecall(w http.ResponseWriter, r *http.Request) {
	if !s.agentAllow(w, r) {
		return
	}
	var req service.MemoryRecallInput
	if !decodeMemory(w, r, &req) {
		return
	}
	res, err := s.Svc.RecallMemory(r.Context(), actorOf(r), spaceID(r), &req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleMemoryRemember(w http.ResponseWriter, r *http.Request) {
	if !s.agentAllow(w, r) {
		return
	}
	var req memoryRememberRequest
	if !decodeMemory(w, r, &req) {
		return
	}
	receipt, err := s.Svc.RememberMemory(r.Context(), actorOf(r), spaceID(r), req.input(), r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, receipt)
}

func (s *Server) handleMemoryRevise(w http.ResponseWriter, r *http.Request) {
	if !s.agentAllow(w, r) {
		return
	}
	var req service.MemoryReviseInput
	if !decodeMemory(w, r, &req) {
		return
	}
	receipt, err := s.Svc.ReviseMemory(r.Context(), actorOf(r), spaceID(r), &req, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, receipt)
}

func (s *Server) handleMemoryConsolidate(w http.ResponseWriter, r *http.Request) {
	if !s.agentAllow(w, r) {
		return
	}
	var req service.MemoryConsolidateInput
	if !decodeMemory(w, r, &req) {
		return
	}
	receipt, err := s.Svc.ConsolidateMemory(r.Context(), actorOf(r), spaceID(r), &req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, receipt)
}

func (s *Server) handleMemoryDerived(w http.ResponseWriter, r *http.Request) {
	if !s.agentAllow(w, r) {
		return
	}
	query := r.URL.Query()
	limit, budget := 0, 0
	if text := query.Get("limit"); text != "" {
		var err error
		limit, err = strconv.Atoi(text)
		if err != nil {
			writeErr(w, domain.Invalid("limit must be an integer"))
			return
		}
	}
	if text := query.Get("maxBytes"); text != "" {
		var err error
		budget, err = strconv.Atoi(text)
		if err != nil {
			writeErr(w, domain.Invalid("maxBytes must be an integer"))
			return
		}
	}
	res, err := s.Svc.DerivedStatusWithInput(r.Context(), actorOf(r), spaceID(r), service.MemoryDerivedInput{Cursor: query.Get("cursor"), DocumentID: query.Get("documentId"), Limit: limit, MaxBytes: budget})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
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

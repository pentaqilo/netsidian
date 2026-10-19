// Package httpapi implements the versioned Netsidian REST transport.
// The same services also back the native bridge; the browser is never
// trusted with identities and the server build never registers
// privileged local-only routes.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
)

type Server struct {
	DesktopMCP    *DesktopMCPManager
	DesktopPolicy *DesktopMCPConfig
	OAuth         *OAuthLogin
	Svc           *service.Services
	NativePick    func() (string, error) // set only by the desktop bootstrap
	// LocalFolderPick enables the /local/* folder browser so session users
	// of a same-machine deployment can attach a server-side folder as a
	// vault root (NETSIDIAN_LOCAL_FOLDER_PICK=1). Off by default.
	LocalFolderPick bool
	// TrustedProxy indicates X-Forwarded-Proto may be honored.
	TrustedProxy bool
	PublicURL    string
	limiter      *rateLimiter

	// API contract generated from the live route table on Handler().
	apiDocJSON, apiDocYAML []byte
	apiDocUndoc            []string
}

func New(svc *service.Services) *Server {
	return &Server{Svc: svc, limiter: newRateLimiter()}
}

// Handler builds the complete mux. Static assets are mounted separately
// by the bootstrap; this mux owns /api and /healthz.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	reg := &routeRegistry{}
	doc := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, h)
		reg.record(pattern)
	}
	doc("GET /api/v1/auth/oauth/providers", s.preAuth(s.handleOAuthProviders))
	if s.OAuth != nil {
		doc("GET /api/v1/auth/oauth/start", s.preAuth(s.handleOAuthStart))
		doc("GET /api/v1/auth/oauth/callback", s.preAuth(s.handleOAuthCallback))
	}
	if s.DesktopMCP != nil {
		doc("GET /api/v1/native/mcp", s.wrap(s.handleDesktopMCPGet, false))
		doc("PUT /api/v1/native/mcp", s.wrap(s.handleDesktopMCPPut, false))
	}
	if s.NativePick != nil {
		doc("GET /api/v1/native/status", s.wrap(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"native": true}) }, false))
		doc("POST /api/v1/native/vaults/attach", s.wrap(s.handleNativeAttach, false))
	}
	if s.LocalFolderPick {
		doc("GET /api/v1/local/status", s.wrap(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"enabled": true}) }, false))
		doc("GET /api/v1/local/folders", s.wrap(s.handleLocalFolders, false))
		doc("POST /api/v1/local/folders", s.wrap(s.handleLocalFolderCreate, false))
		doc("POST /api/v1/local/vaults", s.wrap(s.handleLocalVaultAttach, false))
	}

	doc("GET /api/v1/health", s.handleHealth)
	doc("GET /healthz", s.handleHealth)
	doc("GET /livez", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	doc("GET /api/v1/openapi.yaml", s.handleOpenAPIYAML)
	doc("GET /api/v1/openapi.json", s.handleOpenAPIJSON)
	doc("GET /api/v1/settings", s.wrap(s.handleSettings, true))

	doc("POST /api/v1/auth/setup", s.preAuth(s.handleSetup))
	doc("POST /api/v1/auth/login", s.preAuth(s.handleLogin))
	doc("POST /api/v1/auth/signup", s.preAuth(s.handleSignup))
	doc("POST /api/v1/auth/logout", s.wrap(s.handleLogout, false))
	doc("POST /api/v1/auth/password", s.wrap(s.handleChangePassword, false))
	doc("POST /api/v1/auth/challenge/totp", s.preAuth(s.handleChallengeTOTP))
	doc("POST /api/v1/auth/challenge/recovery", s.preAuth(s.handleChallengeRecovery))
	doc("POST /api/v1/auth/totp/enroll", s.wrap(s.handleTOTPEnroll, false))
	doc("POST /api/v1/auth/totp/confirm", s.wrap(s.handleTOTPConfirm, false))
	doc("POST /api/v1/auth/totp/disable", s.wrap(s.handleTOTPDisable, false))
	doc("POST /api/v1/auth/totp/recovery-codes", s.wrap(s.handleTOTPRecovery, false))
	doc("GET /api/v1/me/totp", s.wrap(s.handleTOTPStatus, false))
	doc("POST /api/v1/admin/users/{id}/totp-reset", s.wrap(s.handleAdminTOTPReset, false))
	doc("GET /api/v1/me", s.wrap(s.handleMe, false))
	doc("GET /api/v1/admin/settings", s.wrap(s.handleAdminSettings, false))
	doc("GET /api/v1/admin/users", s.wrap(s.handleAdminUsers, false))
	doc("PATCH /api/v1/admin/users/{id}", s.wrap(s.handleAdminUserUpdate, false))
	doc("GET /api/v1/admin/audit", s.wrap(s.handleAdminAudit, false))
	doc("GET /api/v1/me/integrations", s.wrap(s.handleIntegrations, false))
	doc("GET /api/v1/me/preferences", s.wrap(s.handlePrefsGet, false))
	doc("PUT /api/v1/me/preferences", s.wrap(s.handlePrefsPut, false))
	doc("GET /api/v1/me/keys", s.wrap(s.handleKeysList, false))
	doc("POST /api/v1/me/keys", s.wrap(s.handleKeysCreate, false))
	doc("DELETE /api/v1/me/keys/{id}", s.wrap(s.handleKeysRevoke, false))
	doc("POST /api/v1/invitations/accept", s.wrap(s.handleInviteAccept, false))

	doc("GET /api/v1/storage/providers", s.wrap(s.handleStorageOptions, false))
	doc("GET /api/v1/spaces/{space}/storage", s.wrap(s.handleStorageGet, false))
	doc("PUT /api/v1/spaces/{space}/storage", s.wrap(s.handleStoragePut, false))
	doc("GET /api/v1/spaces", s.wrap(s.handleSpacesList, false))
	doc("POST /api/v1/spaces", s.wrap(s.handleSpacesCreate, false))
	doc("GET /api/v1/spaces/{space}", s.wrap(s.handleSpaceGet, false))
	doc("DELETE /api/v1/spaces/{space}", s.wrap(s.handleSpaceDelete, false))
	doc("POST /api/v1/spaces/{space}/archive", s.wrap(s.handleSpaceArchive(true), false))
	doc("POST /api/v1/spaces/{space}/unarchive", s.wrap(s.handleSpaceArchive(false), false))
	doc("PUT /api/v1/spaces/{space}", s.wrap(s.handleSpaceRename, false))
	doc("GET /api/v1/spaces/{space}/workspace", s.wrap(s.handleWorkspaceGet, false))
	doc("GET /api/v1/spaces/{space}/load", s.wrap(s.handleVaultLoad, false))
	doc("POST /api/v1/spaces/{space}/load/retry", s.wrap(s.handleVaultLoadRetry, false))
	doc("PUT /api/v1/spaces/{space}/workspace", s.wrap(s.handleWorkspacePut, false))
	doc("GET /api/v1/spaces/{space}/export", s.wrap(s.handleVaultExport, false))
	doc("POST /api/v1/spaces/{space}/invitations", s.wrap(s.handleInviteCreate, false))
	doc("DELETE /api/v1/spaces/{space}/invitations/{id}", s.wrap(s.handleInviteRevoke, false))
	doc("GET /api/v1/spaces/{space}/sharing/users", s.wrap(s.handleSharingUsers, false))
	doc("POST /api/v1/spaces/{space}/members", s.wrap(s.handleShareWithUser, false))
	doc("PUT /api/v1/spaces/{space}/members/{member}", s.wrap(s.handleMemberRole, false))
	doc("DELETE /api/v1/spaces/{space}/members/{member}", s.wrap(s.handleMemberRemove, false))

	doc("GET /api/v1/spaces/{space}/folders", s.wrap(s.handleFolders, false))
	doc("POST /api/v1/spaces/{space}/folders", s.wrap(s.handleFolders, false))
	doc("POST /api/v1/spaces/{space}/folders/rename", s.wrap(s.handleFolderRename, false))
	doc("POST /api/v1/spaces/{space}/folders/trash", s.wrap(s.handleFolderTrash, false))
	doc("GET /api/v1/spaces/{space}/folders/trash", s.wrap(s.handleFolderTrashes, false))
	doc("POST /api/v1/spaces/{space}/folders/restore", s.wrap(s.handleFolderRestore, false))
	doc("POST /api/v1/spaces/{space}/attachments/move", s.wrap(s.handleAttachmentMove, false))
	doc("DELETE /api/v1/spaces/{space}/attachments/{path...}", s.wrap(s.handleAttachmentTrash, false))
	doc("GET /api/v1/spaces/{space}/attachments", s.wrap(s.handleAttachmentsList, false))
	doc("PUT /api/v1/spaces/{space}/attachments/{path...}", s.wrap(s.handleAttachmentPut, false))
	doc("GET /api/v1/spaces/{space}/attachments/{path...}", s.wrap(s.handleAttachmentGet, false))
	doc("GET /api/v1/spaces/{space}/text-files/{path...}", s.wrap(s.handleTextFileGet, false))
	doc("PUT /api/v1/spaces/{space}/text-files/{path...}", s.wrap(s.handleTextFilePut, false))
	doc("GET /api/v1/spaces/{space}/documents/lookup", s.wrap(s.handleDocLookup, false))
	doc("GET /api/v1/spaces/{space}/documents", s.wrap(s.handleDocsList, false))
	doc("POST /api/v1/spaces/{space}/documents", s.wrap(s.handleDocCreate, false))
	doc("GET /api/v1/spaces/{space}/documents/{doc}", s.wrap(s.handleDocGet, false))
	doc("PUT /api/v1/spaces/{space}/documents/{doc}/content", s.wrap(s.handleDocSave, false))
	doc("POST /api/v1/spaces/{space}/documents/{doc}/rename", s.wrap(s.handleDocRename, false))
	doc("POST /api/v1/spaces/{space}/documents/{doc}/trash", s.wrap(s.handleDocTrash, false))
	doc("POST /api/v1/spaces/{space}/documents/{doc}/restore", s.wrap(s.handleDocRestore, false))
	doc("GET /api/v1/spaces/{space}/documents/{doc}/revisions", s.wrap(s.handleRevisions, false))
	doc("GET /api/v1/spaces/{space}/documents/{doc}/revisions/{rev}", s.wrap(s.handleRevisionGet, false))
	doc("GET /api/v1/spaces/{space}/documents/{doc}/backlinks", s.wrap(s.handleBacklinks, false))

	// folder-backed Kanban boards
	doc("GET /api/v1/spaces/{space}/boards", s.wrap(s.handleBoardsList, false))
	doc("POST /api/v1/spaces/{space}/boards", s.wrap(s.handleBoardCreate, false))
	doc("GET /api/v1/spaces/{space}/boards/board", s.wrap(s.handleBoardGet, false))
	doc("PUT /api/v1/spaces/{space}/boards/config", s.wrap(s.handleBoardConfig, false))
	doc("POST /api/v1/spaces/{space}/boards/tasks", s.wrap(s.handleBoardTaskCreate, false))
	doc("POST /api/v1/spaces/{space}/boards/move", s.wrap(s.handleBoardTaskMove, false))
	doc("POST /api/v1/spaces/{space}/boards/reorder", s.wrap(s.handleBoardReorder, false))

	// folder-backed mind maps
	doc("POST /api/v1/spaces/{space}/mindmaps/nodes/links", s.wrap(s.handleMindMapNodeLink, false))
	doc("GET /api/v1/spaces/{space}/mindmaps", s.wrap(s.handleMindMapsList, false))
	doc("POST /api/v1/spaces/{space}/mindmaps", s.wrap(s.handleMindMapCreate, false))
	doc("GET /api/v1/spaces/{space}/mindmaps/mindmap", s.wrap(s.handleMindMapGet, false))
	doc("PUT /api/v1/spaces/{space}/mindmaps/config", s.wrap(s.handleMindMapConfig, false))
	doc("POST /api/v1/spaces/{space}/mindmaps/nodes", s.wrap(s.handleMindMapNodeCreate, false))
	doc("POST /api/v1/spaces/{space}/mindmaps/nodes/reference", s.wrap(s.handleMindMapNodeReference, false))
	doc("POST /api/v1/spaces/{space}/mindmaps/nodes/move", s.wrap(s.handleMindMapNodeMove, false))
	doc("POST /api/v1/spaces/{space}/mindmaps/nodes/delete", s.wrap(s.handleMindMapNodeDelete, false))
	doc("POST /api/v1/spaces/{space}/mindmaps/nodes/files", s.wrap(s.handleMindMapNodeFiles, false))

	doc("GET /api/v1/spaces/{space}/search", s.wrap(s.handleSearch, false))
	doc("GET /api/v1/spaces/{space}/graph2", s.wrap(s.handleGraph2, false))
	doc("GET /api/v1/spaces/{space}/memos", s.wrap(s.handleMemos, false))
	doc("GET /api/v1/spaces/{space}/graph", s.wrap(s.handleGraph, false))
	doc("GET /api/v1/spaces/{space}/graph/analysis", s.wrap(s.handleGraphAnalysis, false))
	doc("GET /api/v1/spaces/{space}/tags", s.wrap(s.handleTags, false))

	// agent data interface (agent-memory PLAN.md)
	doc("GET /api/v1/agent/whoami", s.wrap(s.handleAgentWhoami, false))
	doc("POST /api/v1/spaces/{space}/agent/context", s.wrap(s.handleAgentContext, false))
	doc("GET /api/v1/spaces/{space}/changes/since", s.wrap(s.handleChangesSince, false))

	doc("GET /api/v1/spaces/{space}/events", s.wrap(s.handleEvents, false))

	s.apiDocJSON, s.apiDocYAML, s.apiDocUndoc = buildAPIDoc(reg)
	mux.Handle("/mcp", s.newMCPHandler(mux, reg.routes))
	return mux
}

// ---- middleware ----

type ctxKey int

const actorKey ctxKey = 1

// wrap resolves identity + enforces mutation protections. public=true
// allows unauthenticated access (settings before setup).
func (s *Server) wrap(h func(http.ResponseWriter, *http.Request), public bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		}
		a, err := s.identify(r)
		if err != nil {
			// setup gate: with zero users in local mode only setup works
			if !public {
				writeErr(w, err)
				return
			}
		}
		if a != nil {
			if corr := r.Header.Get("X-Correlation-Id"); corr != "" {
				if len(corr) > 64 {
					corr = corr[:64]
				}
				a.Corr = corr
			}
			if isMutation(r.Method) {
				if err := s.checkCSRF(r, a); err != nil {
					writeErr(w, err)
					return
				}
				if !s.limiter.allow(a.UserID) {
					writeErr(w, domain.RateLimited("too many requests; slow down"))
					return
				}
			}
			r = r.WithContext(context.WithValue(r.Context(), actorKey, a))
		}
		h(w, r)
	}
}

// preAuth protects the pre-auth endpoints (setup/login/signup) with the
// same body cap and a burst limiter keyed on client IP; the wrap() limiter
// cannot apply there because no principal exists yet.
func (s *Server) preAuth(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isMutation(r.Method) {
			if err := s.checkCSRF(r, &service.Actor{}); err != nil {
				writeErr(w, err)
				return
			}
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		ip := r.RemoteAddr
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
		if !s.limiter.allow("preauth:" + ip) {
			writeErr(w, domain.RateLimited("too many attempts; slow down"))
			return
		}
		h(w, r)
	}
}

func isMutation(m string) bool {
	return m != http.MethodGet && m != http.MethodHead
}

// checkCSRF protects cookie-authenticated state changes: the Origin must
// match Host (same-origin) and a custom header must be present, so any
// third-party simple form/post is rejected. Bearer-key callers are not
// cookie-bearing and exempt.
func (s *Server) checkCSRF(r *http.Request, a *service.Actor) error {
	if a.Key != nil {
		return nil
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		host := r.Host
		if !strings.EqualFold(origin, schemeOf(r, s.TrustedProxy)+"://"+host) {
			return domain.Forbidden("cross-origin mutation rejected")
		}
	} else if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return domain.Forbidden("cross-site mutation rejected")
	}
	if r.Header.Get("X-Netsidian-Requested") == "" {
		return domain.Forbidden("missing X-Netsidian-Requested header")
	}
	return nil
}

func schemeOf(r *http.Request, trustProxy bool) string {
	if r.TLS != nil {
		return "https"
	}
	if trustProxy && r.Header.Get("X-Forwarded-Proto") != "" {
		return r.Header.Get("X-Forwarded-Proto")
	}
	return "http"
}

// simple bursty limiter keyed on principal.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	last    time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter() *rateLimiter { return &rateLimiter{buckets: map[string]*bucket{}} }

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b := rl.buckets[key]
	if b == nil {
		b = &bucket{tokens: 40, last: now}
		rl.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * 10 // refill 10/s
	if b.tokens > 60 {
		b.tokens = 60
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	if len(rl.buckets) > 10000 && now.Sub(rl.last) > time.Minute {
		for k, v := range rl.buckets {
			if now.Sub(v.last) > 10*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.last = now
	}
	return true
}

// ---- helpers ----

func actorOf(r *http.Request) *service.Actor {
	a, _ := r.Context().Value(actorKey).(*service.Actor)
	return a
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errBody struct {
	Error struct {
		Code            string `json:"code"`
		Message         string `json:"message"`
		Retryable       bool   `json:"retryable"`
		CurrentRevision string `json:"currentRevision,omitempty"`
	} `json:"error"`
}

func writeErr(w http.ResponseWriter, err error) {
	var de *domain.Error
	if !errors.As(err, &de) {
		de = &domain.Error{Code: domain.ErrCodeUnavailable, Status: 500, Message: "internal error"}
	}
	var b errBody
	b.Error.Code = string(de.Code)
	b.Error.Message = de.Message
	b.Error.Retryable = de.Retryable
	b.Error.CurrentRevision = de.CurrentRevision
	writeJSON(w, de.Status, b)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, domain.Invalid("malformed JSON body"))
		return false
	}
	return true
}

func spaceID(r *http.Request) string { return r.PathValue("space") }
func docID(r *http.Request) string   { return r.PathValue("doc") }

func intQuery(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

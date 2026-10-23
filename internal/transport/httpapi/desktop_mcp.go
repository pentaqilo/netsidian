package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/service"
)

type DesktopMCPConfig struct {
	Enabled              bool     `json:"enabled"`
	Port                 int      `json:"port"`
	AllowUnauthenticated bool     `json:"allowUnauthenticated"`
	ReadOnly             bool     `json:"readOnly"`
	AllowTrash           bool     `json:"allowTrash"`
	VaultIDs             []string `json:"vaultIds"`
}
type DesktopMCPStatus struct {
	Config  DesktopMCPConfig `json:"config"`
	Running bool             `json:"running"`
	URL     string           `json:"url"`
	Error   string           `json:"error,omitempty"`
}
type desktopMCPActorKey struct{}

func desktopMCPActor(ctx context.Context) *service.Actor {
	a, _ := ctx.Value(desktopMCPActorKey{}).(*service.Actor)
	return a
}

type desktopMCPHandler struct{ handler http.Handler }
type DesktopMCPManager struct {
	mu             sync.Mutex
	owner          *Server
	file           string
	config         DesktopMCPConfig
	server         *http.Server
	cancel         context.CancelFunc
	url, lastError string
	current        atomic.Pointer[desktopMCPHandler]
}

// NewDesktopMCPManager is called only by native bootstrap. It never opens a
// listener until explicitly enabled, and restores only the saved local policy.
func NewDesktopMCPManager(owner *Server, file string) (*DesktopMCPManager, error) {
	if owner.NativePick == nil || owner.Svc.Cfg.AuthMode != "none" {
		return nil, fmt.Errorf("desktop MCP requires the native local runtime")
	}
	m := &DesktopMCPManager{owner: owner, file: file, config: DesktopMCPConfig{Port: 3210, ReadOnly: true, VaultIDs: []string{}}}
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		m.lastError = "Could not read saved MCP settings: " + err.Error()
		return m, nil
	}
	saved := m.config
	if err = json.Unmarshal(data, &saved); err != nil {
		m.lastError = "Saved MCP settings are invalid"
		return m, nil
	}
	if err = m.apply(saved, false); err != nil {
		m.config = saved
		m.lastError = err.Error()
	}
	return m, nil
}
func (m *DesktopMCPManager) Status() DesktopMCPStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.config
	cfg.VaultIDs = append([]string{}, cfg.VaultIDs...)
	return DesktopMCPStatus{Config: cfg, Running: m.server != nil, URL: m.url, Error: m.lastError}
}
func (m *DesktopMCPManager) Apply(cfg DesktopMCPConfig) error { return m.apply(cfg, true) }
func (m *DesktopMCPManager) apply(cfg DesktopMCPConfig, persist bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg.Port < 0 || cfg.Port > 65535 {
		return domain.Invalid("port must be between 0 and 65535")
	}
	if len(cfg.VaultIDs) > 1000 {
		return domain.Invalid("too many selected vaults")
	}
	cfg.VaultIDs = append([]string{}, cfg.VaultIDs...)
	seen := map[string]bool{}
	for _, id := range cfg.VaultIDs {
		if id == "" || seen[id] {
			return domain.Invalid("select distinct vaults")
		}
		seen[id] = true
	}
	if cfg.Enabled {
		if len(cfg.VaultIDs) == 0 {
			return domain.Invalid("select at least one vault before enabling MCP")
		}
		user, err := m.owner.Svc.EnsureNoAuthPrincipal(context.Background())
		if err != nil {
			return err
		}
		actor := &service.Actor{UserID: user.ID, Local: true}
		for _, id := range cfg.VaultIDs {
			if _, err = m.owner.Svc.Require(context.Background(), actor, id, domain.ActionRead); err != nil {
				return err
			}
		}
	}
	var listener net.Listener
	reuse := cfg.Enabled && m.server != nil && cfg.Port == m.config.Port
	if cfg.Enabled && !reuse {
		var err error
		listener, err = net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)))
		if err != nil {
			return domain.Conflict("MCP port is unavailable; choose another port")
		}
	}
	if persist {
		if err := saveDesktopMCPConfig(m.file, cfg); err != nil {
			if listener != nil {
				listener.Close()
			}
			return domain.Unavailable("could not save desktop MCP settings")
		}
	}
	if !reuse {
		m.stopLocked()
	}
	m.config = cfg
	m.lastError = ""
	if !cfg.Enabled {
		return nil
	}
	endpoint := m.url
	if listener != nil {
		endpoint = "http://" + listener.Addr().String() + "/mcp"
	}
	clone := New(m.owner.Svc)
	clone.PublicURL = strings.TrimSuffix(endpoint, "/mcp")
	clone.DesktopPolicy = &cfg
	api := clone.Handler()
	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			writeErr(w, domain.Forbidden("desktop MCP only accepts loopback clients"))
			return
		}
		// Resolve credentials before substituting a scoped local actor. A supplied
		// invalid token must never fall back to token-free access.
		var actor *service.Actor
		if authorization := r.Header.Get("Authorization"); authorization != "" {
			if !strings.HasPrefix(authorization, "Bearer ") {
				writeErr(w, domain.Unauthenticated("use a bearer PAT"))
				return
			}
			actor, err = m.owner.identify(r)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="netsidian"`)
				writeErr(w, err)
				return
			}
		} else if cfg.AllowUnauthenticated {
			user, e := m.owner.Svc.EnsureNoAuthPrincipal(r.Context())
			if e != nil {
				writeErr(w, e)
				return
			}
			actor = &service.Actor{UserID: user.ID, DisplayName: user.DisplayName, IsOperator: user.IsOperator, Local: true}
		}
		if actor != nil {
			scoped, e := scopedDesktopActor(actor, cfg)
			if e != nil {
				writeErr(w, e)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), desktopMCPActorKey{}, scoped))
		}
		api.ServeHTTP(w, r)
	})
	m.current.Store(&desktopMCPHandler{handler: protected})
	m.url = endpoint
	if listener != nil {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 40 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			entry := m.current.Load()
			if entry == nil {
				http.NotFound(w, r)
				return
			}
			entry.handler.ServeHTTP(w, r)
		})}
		m.server = server
		go func() {
			if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
				m.mu.Lock()
				if m.server == server {
					m.lastError = "MCP listener stopped: " + err.Error()
					m.server = nil
					m.url = ""
				}
				m.mu.Unlock()
			}
		}()
	}
	return nil
}
func (m *DesktopMCPManager) stopLocked() {
	m.current.Store(nil)
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.server != nil {
		m.server.Close()
		m.server = nil
	}
	m.url = ""
}
func (m *DesktopMCPManager) Close() { m.mu.Lock(); defer m.mu.Unlock(); m.stopLocked() }
func saveDesktopMCPConfig(path string, cfg DesktopMCPConfig) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".desktop-mcp-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = json.NewEncoder(file).Encode(cfg); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}
func scopedDesktopActor(original *service.Actor, cfg DesktopMCPConfig) (*service.Actor, error) {
	actor := *original
	key := ports.APIKeyRecord{ID: "desktop-local", Label: "Local desktop MCP", PrincipalID: actor.UserID}
	scopes := []string{domain.ScopeDocumentsRead, domain.ScopeAttachmentsRead, domain.ScopeSearchRead, domain.ScopeGraphRead, domain.ScopeHistoryRead, domain.ScopeExportRead, domain.ScopeEventsRead}
	if !cfg.ReadOnly {
		scopes = append(scopes, domain.ScopeDocumentsWrite, domain.ScopeAttachmentsWrite, domain.ScopeHistoryRestore)
		if cfg.AllowTrash {
			scopes = append(scopes, domain.ScopeDocumentsDelete)
		}
	}
	allowed := func(values []string, want string) bool {
		for _, value := range values {
			if value == want {
				return true
			}
		}
		return false
	}
	for _, id := range cfg.VaultIDs {
		if original.Key == nil || allowed(original.Key.SpaceIDs, id) {
			key.SpaceIDs = append(key.SpaceIDs, id)
		}
	}
	for _, scope := range scopes {
		if original.Key == nil || len(original.Key.Scopes) == 0 || allowed(original.Key.Scopes, scope) {
			key.Scopes = append(key.Scopes, scope)
		}
	}
	if len(key.SpaceIDs) == 0 || len(key.Scopes) == 0 {
		return nil, domain.Forbidden("token does not grant access permitted by desktop MCP settings")
	}
	if original.Key != nil {
		key.ID = original.Key.ID
		key.Label = original.Key.Label
	}
	actor.Key = &key
	actor.ExecutionTarget = "desktop"
	return &actor, nil
}
func desktopMCPOperation(op mcpOperation, cfg *DesktopMCPConfig) bool {
	if cfg.ReadOnly && !mcpReadOperation(op) {
		return false
	}
	if (op.Method == "GET" || op.Method == "HEAD") && (op.Path == "/api/v1/spaces" || op.Path == "/api/v1/agent/whoami" || op.Path == "/api/v1/agent/memory-policy") {
		return true
	}
	if !strings.Contains(op.Path, "{space}") {
		return false
	}
	for _, human := range []string{"/members", "/invitations", "/storage", "/load/retry"} {
		if strings.Contains(op.Path, human) {
			return false
		}
	}
	return true
}
func (s *Server) handleDesktopMCPGet(w http.ResponseWriter, r *http.Request) {
	if actorOf(r).Key != nil {
		writeErr(w, domain.Forbidden("desktop MCP settings require the local UI"))
		return
	}
	writeJSON(w, 200, s.DesktopMCP.Status())
}
func (s *Server) handleDesktopMCPPut(w http.ResponseWriter, r *http.Request) {
	if actorOf(r).Key != nil {
		writeErr(w, domain.Forbidden("desktop MCP settings require the local UI"))
		return
	}
	cfg := DesktopMCPConfig{Port: 3210, ReadOnly: true}
	if !decode(w, r, &cfg) {
		return
	}
	if err := s.DesktopMCP.Apply(cfg); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, s.DesktopMCP.Status())
}

package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pentaqilo/netsidian/internal/authpolicy"
)

// Config is the validated runtime configuration derived from the
// Netsidian Configuration contract (NETSIDIAN_* variables).
type Config struct {
	StorageEndpoints []string
	STSEndpoints     []string
	StorageDriver    string
	S3Bucket         string
	S3Region         string
	S3Endpoint       string
	S3Prefix         string
	S3PathStyle      bool

	OIDCIssuer             string
	OIDCClientID           string
	OIDCClientSecret       string
	OIDCLabel              string
	OIDCAllowedClaims      authpolicy.Rules
	OIDCAdminClaims        authpolicy.Rules
	OIDCAllowAllUsers      bool
	OIDCScopes             []string
	OIDCEmailClaim         string
	OIDCNameClaim          string
	OIDCEmailVerifiedClaim string

	PublicURL             string
	BindHost              string
	Port                  int
	AuthMode              string // none | local | external | mixed
	DatabaseDriver        string // sqlite (postgres planned)
	SQLitePath            string
	StateDir              string // durable application state
	StorageRoot           string // managed local content root (storage binding local-main)
	StorageMode           string // managed (unsupported modes fail closed)
	BusyTimeoutMS         int
	MaxNoteBytes          int64
	MaxAttachBytes        int64
	SignupEnabled         bool
	LocalFolderPick       bool // expose server-side folder browsing so sessions can attach a folder as a vault root (opt-in, local deployments)
	TrustedProxy          bool // honor X-Forwarded-Proto from a trusted TLS proxy
	RevisionRetentionDays int
	TrashRetentionDays    int
	LogLevel              string
	AuthSecret            []byte // 32-byte session/CSRF secret
	InstanceKind          string // server | native
}

func Load() (*Config, error) { return LoadFor("server") }

// LoadFor selects the runtime at the composition root, never from a browser
// or an environment flag. Desktop owns its loopback, no-auth, local profile.
func LoadFor(kind string) (*Config, error) {
	if kind != "server" && kind != "native" {
		return nil, fmt.Errorf("invalid runtime kind %q", kind)
	}
	c := &Config{
		StorageEndpoints:       strings.Fields(env("NETSIDIAN_STORAGE_ALLOWED_ENDPOINTS", "")),
		STSEndpoints:           strings.Fields(env("NETSIDIAN_STORAGE_TRUSTED_STS_ENDPOINTS", "")),
		StorageDriver:          env("NETSIDIAN_STORAGE_DRIVER", "local"),
		S3Bucket:               env("NETSIDIAN_S3_BUCKET", ""),
		S3Region:               env("NETSIDIAN_S3_REGION", "us-east-1"),
		S3Endpoint:             env("NETSIDIAN_S3_ENDPOINT", ""),
		S3Prefix:               env("NETSIDIAN_S3_PREFIX", "netsidian"),
		S3PathStyle:            envBool("NETSIDIAN_S3_PATH_STYLE", false),
		OIDCIssuer:             env("NETSIDIAN_OIDC_ISSUER", ""),
		OIDCClientID:           env("NETSIDIAN_OIDC_CLIENT_ID", ""),
		OIDCClientSecret:       env("NETSIDIAN_OIDC_CLIENT_SECRET", ""),
		OIDCEmailClaim:         env("NETSIDIAN_OIDC_EMAIL_CLAIM", "/email"),
		OIDCNameClaim:          env("NETSIDIAN_OIDC_NAME_CLAIM", "/name"),
		OIDCEmailVerifiedClaim: env("NETSIDIAN_OIDC_EMAIL_VERIFIED_CLAIM", "/email_verified"),
		OIDCLabel:              env("NETSIDIAN_OIDC_LABEL", "Single sign-on"),
		PublicURL:              env("NETSIDIAN_PUBLIC_URL", ""),
		BindHost:               env("NETSIDIAN_BIND", "127.0.0.1"),
		AuthMode:               strings.ToLower(env("NETSIDIAN_AUTH_MODE", "none")),
		DatabaseDriver:         strings.ToLower(env("NETSIDIAN_DATABASE_DRIVER", "sqlite")),
		StateDir:               env("NETSIDIAN_STATE_DIR", defaultStateDir()),
		StorageRoot:            env("NETSIDIAN_STORAGE_ROOT", ""),
		StorageMode:            strings.ToLower(env("NETSIDIAN_STORAGE_MODE", "managed")),
		LocalFolderPick:        envBool("NETSIDIAN_LOCAL_FOLDER_PICK", false),
		TrustedProxy:           envBool("NETSIDIAN_TRUSTED_PROXY", false),
		InstanceKind:           kind,
		MaxNoteBytes:           int64(envInt("NETSIDIAN_MAX_NOTE_BYTES", 2097152)),
		MaxAttachBytes:         int64(envInt("NETSIDIAN_MAX_ATTACHMENT_BYTES", 26214400)),
		BusyTimeoutMS:          envInt("NETSIDIAN_SQLITE_BUSY_TIMEOUT_MS", 5000),
		RevisionRetentionDays:  envInt("NETSIDIAN_REVISION_RETENTION_DAYS", 30),
		TrashRetentionDays:     envInt("NETSIDIAN_TRASH_RETENTION_DAYS", 30),
		LogLevel:               env("NETSIDIAN_LOG_LEVEL", "info"),
	}
	if kind == "native" {
		c.AuthMode = "none"
		c.BindHost = "127.0.0.1"
		c.StorageDriver = "local"
		c.LocalFolderPick = false
		c.TrustedProxy = false
		c.PublicURL = ""
		c.OIDCIssuer = ""
		c.OIDCClientID = ""
		c.OIDCClientSecret = ""
	}
	var err error
	if kind != "native" {
		c.LocalFolderPick, err = securityBool("NETSIDIAN_LOCAL_FOLDER_PICK", false)
		if err != nil {
			return nil, err
		}
		c.TrustedProxy, err = securityBool("NETSIDIAN_TRUSTED_PROXY", false)
		if err != nil {
			return nil, err
		}
		c.SignupEnabled, err = securityBool("NETSIDIAN_SIGNUP_ENABLED", false)
		if err != nil {
			return nil, err
		}
		c.OIDCAllowAllUsers, err = securityBool("NETSIDIAN_OIDC_ALLOW_ALL_USERS", false)
		if err != nil {
			return nil, err
		}
		c.OIDCAllowedClaims, err = authpolicy.Parse(os.Getenv("NETSIDIAN_OIDC_ALLOWED_CLAIMS"))
		if err != nil {
			return nil, fmt.Errorf("NETSIDIAN_OIDC_ALLOWED_CLAIMS: %w", err)
		}
		c.OIDCAdminClaims, err = authpolicy.Parse(os.Getenv("NETSIDIAN_OIDC_ADMIN_CLAIMS"))
		if err != nil {
			return nil, fmt.Errorf("NETSIDIAN_OIDC_ADMIN_CLAIMS: %w", err)
		}
		for _, pointer := range []string{c.OIDCEmailClaim, c.OIDCNameClaim, c.OIDCEmailVerifiedClaim} {
			raw, _ := json.Marshal(map[string][]string{pointer: {"validate"}})
			if _, e := authpolicy.Parse(string(raw)); e != nil {
				return nil, fmt.Errorf("invalid OIDC profile claim pointer: %w", e)
			}
		}
		c.OIDCScopes = strings.Fields(env("NETSIDIAN_OIDC_SCOPES", "openid profile email"))
	}
	c.Port, err = envIntErr("NETSIDIAN_PORT", 3000)
	if err != nil {
		return nil, err
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, fmt.Errorf("NETSIDIAN_PORT must be between 1 and 65535")
	}
	switch c.AuthMode {
	case "none", "local":
	case "external", "mixed":
		if len(c.OIDCAllowedClaims) == 0 && !c.OIDCAllowAllUsers {
			return nil, fmt.Errorf("OAuth requires NETSIDIAN_OIDC_ALLOWED_CLAIMS or explicit NETSIDIAN_OIDC_ALLOW_ALL_USERS=true")
		}
		if len(c.OIDCAllowedClaims) > 0 && c.OIDCAllowAllUsers {
			return nil, fmt.Errorf("choose claim admission rules or allow-all, not both")
		}
		if c.AuthMode == "external" && len(c.OIDCAdminClaims) == 0 {
			return nil, fmt.Errorf("external-only auth requires NETSIDIAN_OIDC_ADMIN_CLAIMS for administrator access")
		}
		if c.OIDCIssuer == "" || c.OIDCClientID == "" || c.PublicURL == "" {
			return nil, fmt.Errorf("external/mixed auth requires NETSIDIAN_OIDC_ISSUER, NETSIDIAN_OIDC_CLIENT_ID and NETSIDIAN_PUBLIC_URL")
		}
		for _, raw := range []string{c.OIDCIssuer, c.PublicURL} {
			u, e := url.Parse(raw)
			if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return nil, fmt.Errorf("invalid OAuth URL")
			}
			if raw == c.PublicURL && u.Path != "" && u.Path != "/" {
				return nil, fmt.Errorf("NETSIDIAN_PUBLIC_URL must be an origin without a path")
			}
			if u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "localhost" && (net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback()))) {
				return nil, fmt.Errorf("OAuth URLs require HTTPS except on loopback")
			}
		}
		c.OIDCIssuer = strings.TrimRight(c.OIDCIssuer, "/")
		c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	default:
		return nil, fmt.Errorf("invalid NETSIDIAN_AUTH_MODE %q (choose none|local|external|mixed)", c.AuthMode)
	}
	if c.StorageDriver != "local" && c.StorageDriver != "s3" {
		return nil, fmt.Errorf("NETSIDIAN_STORAGE_DRIVER must be local or s3")
	}
	if c.StorageDriver == "s3" && c.S3Bucket == "" {
		return nil, fmt.Errorf("NETSIDIAN_S3_BUCKET required for S3 storage")
	}
	if c.DatabaseDriver != "sqlite" {
		return nil, fmt.Errorf("NETSIDIAN_DATABASE_DRIVER=%q is not implemented; only sqlite is available in this build", c.DatabaseDriver)
	}
	if c.StorageMode != "managed" && c.StorageMode != "external-managed" {
		return nil, fmt.Errorf("invalid NETSIDIAN_STORAGE_MODE %q", c.StorageMode)
	}
	if c.StorageMode == "external-managed" {
		// The label promises read-only protection but no boundary in this
		// build enforces it yet; fail closed rather than write through.
		return nil, fmt.Errorf("NETSIDIAN_STORAGE_MODE=external-managed is not enforced in this build; refusing to start so externally managed files are never written")
	}
	if c.StorageRoot == "" {
		c.StorageRoot = filepath.Join(c.StateDir, "content")
	}
	c.SQLitePath = env("NETSIDIAN_SQLITE_PATH", filepath.Join(c.StateDir, "database", "netsidian.db"))
	secret, err := loadOrCreateSecret(filepath.Join(c.StateDir, "secrets", "auth_secret"))
	if err != nil {
		return nil, err
	}
	c.AuthSecret = secret
	if c.AuthMode == "none" && c.BindHost != "localhost" && (net.ParseIP(c.BindHost) == nil || !net.ParseIP(c.BindHost).IsLoopback()) {
		return nil, fmt.Errorf("no-auth mode must not bind a public interface; set NETSIDIAN_BIND to a loopback address or enable local auth")
	}
	return c, nil
}

func (c *Config) JournalDir() string { return filepath.Join(c.StateDir, "journal", "ops") }
func (c *Config) HistoryDir() string { return filepath.Join(c.StateDir, "history") }
func (c *Config) SpaceRoot(spaceID string) string {
	return filepath.Join(c.StorageRoot, "spaces", spaceID)
}

func loadOrCreateSecret(p string) ([]byte, error) {
	if b, err := os.ReadFile(p); err == nil {
		s, derr := hex.DecodeString(strings.TrimSpace(string(b)))
		if derr == nil && len(s) == 32 {
			return s, nil
		}
		return nil, fmt.Errorf("auth secret file %s is invalid", p)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	s := make([]byte, 32)
	if _, err := rand.Read(s); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(hex.EncodeToString(s)), 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, p); err != nil {
		// Another instance may have created it concurrently; reread.
		if b, rerr := os.ReadFile(p); rerr == nil {
			s2, derr := hex.DecodeString(strings.TrimSpace(string(b)))
			if derr == nil && len(s2) == 32 {
				return s2, nil
			}
		}
		return nil, err
	}
	return s, nil
}

func defaultStateDir() string {
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "netsidian")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "netsidian-state"
	}
	return filepath.Join(home, ".local", "state", "netsidian")
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		return v == "1" || strings.EqualFold(v, "true")
	}
	return def
}

func securityBool(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return value, nil
}

func envInt(k string, def int) int {
	v, err := envIntErr(k, def)
	if err != nil {
		return def
	}
	return v
}

func envIntErr(k string, def int) (int, error) {
	v := os.Getenv(k)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q", k, v)
	}
	return n, nil
}

// Package bootstrap wires configuration, storage adapters and services,
// then starts the runtime for the selected build target. The HTTP API
// and the SPA fallback share one root handler; native builds mount the
// same handler in the desktop window and additionally expose the local
// bridge — the server build never registers privileged bridge routes.
package bootstrap

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/config"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/service"
	"github.com/pentaqilo/netsidian/internal/storage/local"
	"github.com/pentaqilo/netsidian/internal/storage/providers"
	"github.com/pentaqilo/netsidian/internal/storage/s3store"
	"github.com/pentaqilo/netsidian/internal/transport/httpapi"
)

type App struct {
	Cfg    *config.Config
	Svc    *service.Services
	DB     *sqlite.DB
	api    *httpapi.Server
	assets fs.FS
}

func (a *App) EnableNativePicker(pick func() (string, error)) { a.api.NativePick = pick }

// Build opens the durable state, runs crash reconciliation and wires
// services. distFS is the built frontend (frontend/dist contents).
func Build(distFS fs.FS) (*App, error) { return build(distFS, runtimeKind) }

func build(distFS fs.FS, kind string) (*App, error) {
	cfg, err := config.LoadFor(kind)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(cfg.StateDir, ".restore-in-progress")); !os.IsNotExist(err) {
		return nil, fmt.Errorf("instance restore is incomplete; choose a fresh restore target")
	}
	db, err := sqlite.Open(cfg.SQLitePath, cfg.BusyTimeoutMS)
	if err != nil {
		return nil, err
	}
	built := false
	defer func() {
		if !built {
			_ = db.SQL.Close()
		}
	}()
	content, err := local.NewContent(cfg.StorageRoot)
	if err != nil {
		return nil, err
	}
	var contentStore ports.ContentStore = content
	var remote *s3store.Content
	binding := "local-main"
	if cfg.StorageDriver == "s3" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		remote, err = s3store.New(ctx, cfg.S3Bucket, cfg.S3Region, cfg.S3Endpoint, cfg.S3Prefix, cfg.S3PathStyle, content)
		cancel()
		if err != nil {
			db.SQL.Close()
			return nil, err
		}
		remote.SetReadLimit(max(cfg.MaxNoteBytes, cfg.MaxAttachBytes))
		contentStore = remote
		binding = "s3-main"
	}
	ids, err := db.AllSpaceIDs(context.Background())
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		sp, e := db.Space(context.Background(), id)
		if e != nil {
			return nil, e
		}
		if sp != nil && remote != nil && sp.StorageBinding == "local-main" {
			remote.BindManagedLocal(id)
		}
		if sp != nil && remote == nil && sp.StorageBinding == "s3-main" {
			return nil, fmt.Errorf("vault %s requires S3 storage configuration", id)
		}
		if sp != nil && strings.HasPrefix(sp.StorageBinding, "local-folder:") {
			// Native builds always rebind; server builds only when the
			// operator opted into folder-attached vaults (see NETSIDIAN_LOCAL_FOLDER_PICK).
			if !allowExternalBindings && !cfg.LocalFolderPick {
				return nil, fmt.Errorf("local-folder binding %s cannot be opened: enable NETSIDIAN_LOCAL_FOLDER_PICK or remove the vault", id)
			}
			binder := interface{ BindExternal(string, string) error }(content)
			if remote != nil {
				binder = remote
			}
			if e = binder.BindExternal(id, strings.TrimPrefix(sp.StorageBinding, "local-folder:")); e != nil {
				return nil, fmt.Errorf("local vault %s is unavailable: %w", id, e)
			}
		}
	}
	manager, err := providers.New(contentStore, db, cfg.AuthSecret)
	if err != nil {
		return nil, err
	}
	contentStore = manager
	state, err := local.NewState(cfg.JournalDir(), cfg.HistoryDir())
	if err != nil {
		return nil, err
	}
	svc := service.NewServices(
		service.Config{
			StorageBinding:     binding,
			AuthMode:           cfg.AuthMode,
			SignupEnabled:      cfg.SignupEnabled,
			OIDCIssuer:         cfg.OIDCIssuer,
			OIDCAllowedClaims:  cfg.OIDCAllowedClaims,
			OIDCAdminClaims:    cfg.OIDCAdminClaims,
			OIDCAllowAllUsers:  cfg.OIDCAllowAllUsers,
			MaxNoteBytes:       cfg.MaxNoteBytes,
			MaxAttachBytes:     cfg.MaxAttachBytes,
			TrashRetentionDays: cfg.TrashRetentionDays,
		},
		db, db, db, db, db, db, db, contentStore, state,
	)
	// Second-factor secrets are sealed with the same instance key that seals
	// provider storage profiles, so backup/restore verification covers them.
	svc.Seal = manager
	// no-auth mode resolves a single local principal up front.
	if cfg.AuthMode == "none" {
		if _, err := svc.EnsureNoAuthPrincipal(context.Background()); err != nil {
			return nil, err
		}
	}
	api := httpapi.New(svc)
	if cfg.AuthMode == "external" || cfg.AuthMode == "mixed" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		api.OAuth, err = httpapi.NewOAuthLogin(ctx, cfg.OIDCIssuer, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.PublicURL, cfg.OIDCLabel, cfg.AuthSecret)
		cancel()
		if err != nil {
			db.SQL.Close()
			return nil, err
		}
		api.OAuth.SetScopes(cfg.OIDCScopes)
		api.OAuth.SetProfileClaims(cfg.OIDCEmailClaim, cfg.OIDCNameClaim, cfg.OIDCEmailVerifiedClaim)
		api.OAuth.ConfigureStorageTokens(manager, db, svc)
		svc.RefreshExternalAccess = api.OAuth.RefreshExternalAccess
	}

	var tokenSource providers.IDTokenSource
	if api.OAuth != nil {
		tokenSource = api.OAuth.StorageIdentityToken
	}
	if cfg.InstanceKind != "native" {
		manager.Register("s3", providers.S3Factory(content, cfg.StorageEndpoints, cfg.STSEndpoints, tokenSource, max(cfg.MaxNoteBytes, cfg.MaxAttachBytes)))
	}
	api.TrustedProxy = cfg.TrustedProxy
	api.PublicURL = cfg.PublicURL
	api.LocalFolderPick = cfg.LocalFolderPick
	// Resolve journaled operations before serving any request.
	resolved, blocked := svc.ReconcileOperations(context.Background())
	if resolved > 0 || len(blocked) > 0 {
		log.Printf("netsidian: journal reconcile resolved=%d blocked=%d", resolved, len(blocked))
	}
	// Derived indexes are rebuildable; repair in the background.
	svc.ResumeVaultLoads(context.Background())
	go func() {
		svc.ReindexAll(context.Background())
	}()
	assets := dist
	if distFS != nil {
		assets = distFS
	}
	app := &App{Cfg: cfg, Svc: svc, DB: db, api: api, assets: assets}
	built = true
	return app, nil
}

// RootHandler serves the versioned API first and the built SPA for
// everything else (history routing falls back to index.html).
func (a *App) RootHandler() http.Handler {
	api := a.api.Handler()
	spa := spaHandler(a.assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp" || (r.URL.Path == "/healthz" || r.URL.Path == "/livez") {
			api.ServeHTTP(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	})
}

var staticAssetExt = map[string]bool{"js": true, "css": true, "map": true, "json": true, "webmanifest": true,
	"ico": true, "png": true, "jpg": true, "jpeg": true, "gif": true, "svg": true, "webp": true, "avif": true,
	"bmp": true, "woff": true, "woff2": true, "ttf": true, "otf": true, "eot": true, "txt": true, "xml": true,
	"html": true}

// spaHandler serves the embedded frontend with cache semantics:
// hashed assets immutable, index.html never cached.
func spaHandler(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || p == "." {
			p = "index.html"
		}
		if f, err := assets.Open(p); err == nil {
			f.Close()
			if strings.Contains(path.Base(p), ".") && p != "index.html" {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFileFS(w, r, assets, p)
			return
		}
		// SPA fallback for client routes; a raw 404 is reserved for genuine static assets.
		// Stray in-vault hrefs (.mmd, .pdf, dotted note names) must fall back to the app so
		// the SPA router can show its own in-shell view instead of a dead-end plain-text 404.
		if ext := strings.ToLower(path.Ext(p)); ext != "" && staticAssetExt[ext[1:]] {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, assets, "index.html")
	})
}

// dist is set by the platform entrypoint (main package embed).
var dist fs.FS = errorFS{}

// UseDist installs the embedded frontend filesystem.
func UseDist(fsys fs.FS) {
	sub, err := fs.Sub(fsys, "frontend/dist")
	if err != nil {
		panic(err)
	}
	// validate that the build is present
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		log.Printf("netsidian: frontend build not found; run npm run build in frontend/ before packaging")
	}
	dist = sub
}

type errorFS struct{}

func (errorFS) Open(string) (fs.File, error)          { return nil, fs.ErrNotExist }
func (errorFS) ReadDir(string) ([]fs.DirEntry, error) { return nil, fs.ErrNotExist }

package bootstrap

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestNativeCompositionUsesNoAuthAndLocalProviders(t *testing.T) {
	t.Setenv("NETSIDIAN_STATE_DIR", t.TempDir())
	t.Setenv("NETSIDIAN_AUTH_MODE", "external")
	t.Setenv("NETSIDIAN_STORAGE_DRIVER", "s3")
	t.Setenv("NETSIDIAN_BIND", "0.0.0.0")
	app, err := build(nil, "native")
	if err != nil {
		t.Fatal(err)
	}
	defer app.DB.SQL.Close()
	app.EnableNativePicker(func() (string, error) { return "", nil })
	rr := httptest.NewRecorder()
	app.RootHandler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/settings", nil))
	var v struct {
		AuthMode string `json:"authMode"`
		Runtime  struct {
			Kind    string `json:"kind"`
			Storage struct {
				Providers    []string `json:"providers"`
				FolderPicker string   `json:"folderPicker"`
			} `json:"storage"`
		} `json:"runtime"`
	}
	if err = json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.AuthMode != "none" || v.Runtime.Kind != "desktop" || v.Runtime.Storage.FolderPicker != "native" || len(v.Runtime.Storage.Providers) != 1 || v.Runtime.Storage.Providers[0] != "base" {
		t.Fatalf("native composition inconsistent: %+v", v)
	}
	rr = httptest.NewRecorder()
	app.RootHandler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/me", nil))
	if rr.Code != 200 {
		t.Fatal("native requires a login")
	}
}

func TestIncompleteRestoreCannotStart(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NETSIDIAN_STATE_DIR", root)
	t.Setenv("NETSIDIAN_AUTH_MODE", "none")
	t.Setenv("NETSIDIAN_BIND", "127.0.0.1")
	if err := os.WriteFile(filepath.Join(root, ".restore-in-progress"), []byte("interrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := build(nil, "server"); err == nil || !strings.Contains(err.Error(), "restore is incomplete") {
		t.Fatalf("incomplete restore was allowed to start: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "database", "netsidian.db")); !os.IsNotExist(err) {
		t.Fatal("startup initialized a database during interrupted restore")
	}
}

func TestRootHandlerUsesInjectedAssetsAndMountsLiveness(t *testing.T) {
	t.Setenv("NETSIDIAN_STATE_DIR", t.TempDir())
	t.Setenv("NETSIDIAN_AUTH_MODE", "none")
	t.Setenv("NETSIDIAN_BIND", "127.0.0.1")
	app, err := build(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("injected frontend")}}, "server")
	if err != nil {
		t.Fatal(err)
	}
	defer app.DB.SQL.Close()
	for _, path := range []string{"/", "/s/example"} {
		rr := httptest.NewRecorder()
		app.RootHandler().ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != 200 || rr.Body.String() != "injected frontend" {
			t.Fatal("frontend dependency was ignored")
		}
	}
	rr := httptest.NewRecorder()
	app.RootHandler().ServeHTTP(rr, httptest.NewRequest("GET", "/livez", nil))
	if rr.Code != 200 || !strings.Contains(rr.Header().Get("Content-Type"), "application/json") {
		t.Fatal("root liveness fell through to the SPA")
	}
}

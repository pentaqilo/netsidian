package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/service"
	"github.com/pentaqilo/netsidian/internal/storage/local"
)

func newLocalPickEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "test.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	content, err := local.NewContent(filepath.Join(dir, "content"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := local.NewState(filepath.Join(dir, "journal"), filepath.Join(dir, "history"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewServices(
		service.Config{AuthMode: "local", SignupEnabled: true,
			MaxNoteBytes: 1 << 20, MaxAttachBytes: 1 << 20, TrashRetentionDays: 30},
		db, db, db, db, db, db, db, content, state)
	api := New(svc)
	api.LocalFolderPick = true
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)
	return &testEnv{srv: ts, svc: svc}
}

func TestLocalFolderRoutesAbsentByDefault(t *testing.T) {
	env := newTestEnv(t, false)
	resp := env.call(t, "GET", "/api/v1/local/status", "", "", "")
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 when disabled, got %d", resp.StatusCode)
	}
}

func TestLocalFolderBrowseCreateAttach(t *testing.T) {
	env := newLocalPickEnv(t)
	cookie := env.setup(t, "op@x.io")
	root := t.TempDir()
	for _, d := range []string{"alpha", ".hidden", "beta"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	resp := env.call(t, "GET", "/api/v1/local/folders", "", "", "")
	if resp.StatusCode != 401 {
		t.Fatalf("unauthenticated browse: got %d", resp.StatusCode)
	}

	resp = env.call(t, "GET", "/api/v1/local/folders?path="+root, "", cookie, "")
	if resp.StatusCode != 200 {
		t.Fatalf("browse: got %d", resp.StatusCode)
	}
	var listing struct {
		Path    string `json:"path"`
		Parent  string `json:"parent"`
		Folders []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"folders"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatal(err)
	}
	if listing.Path != filepath.Clean(root) || listing.Parent == "" {
		t.Fatalf("path/parent wrong: %+v", listing)
	}
	if len(listing.Folders) != 2 || listing.Folders[0].Name != "alpha" || listing.Folders[1].Name != "beta" {
		t.Fatalf("dotdirs/files must be hidden and sorted: %+v", listing.Folders)
	}

	resp = env.call(t, "GET", "/api/v1/local/folders?path=relative", "", cookie, "")
	if resp.StatusCode != 400 {
		t.Fatalf("relative path: got %d", resp.StatusCode)
	}

	resp = env.call(t, "POST", "/api/v1/local/folders", `{"path":"`+root+`","name":"new vault"}`, cookie, "")
	if resp.StatusCode != 201 {
		t.Fatalf("mkdir: got %d", resp.StatusCode)
	}
	if info, err := os.Stat(filepath.Join(root, "new vault")); err != nil || !info.IsDir() {
		t.Fatal("folder not created on disk")
	}

	resp = env.call(t, "POST", "/api/v1/local/folders", `{"path":"`+root+`","name":"a/b"}`, cookie, "")
	if resp.StatusCode != 400 {
		t.Fatalf("bad name: got %d", resp.StatusCode)
	}

	vaultDir := filepath.Join(root, "new vault")
	if err := os.WriteFile(filepath.Join(vaultDir, "one.md"), []byte("# One\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp = env.call(t, "POST", "/api/v1/local/vaults", `{"path":"`+vaultDir+`"}`, cookie, "")
	if resp.StatusCode != 201 {
		t.Fatalf("attach: got %d", resp.StatusCode)
	}
	var attached map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&attached); err != nil {
		t.Fatal(err)
	}
	if attached["documents"].(float64) != 1 {
		t.Fatalf("expected 1 indexed doc, got %v", attached["documents"])
	}

	resp = env.call(t, "POST", "/api/v1/local/vaults", `{"path":"relative"}`, cookie, "")
	if resp.StatusCode != 400 {
		t.Fatalf("relative attach: got %d", resp.StatusCode)
	}

	// Attaching the same folder twice must conflict.
	resp = env.call(t, "POST", "/api/v1/local/vaults", `{"path":"`+vaultDir+`"}`, cookie, "")
	if resp.StatusCode != 409 {
		t.Fatalf("double attach: got %d", resp.StatusCode)
	}
}

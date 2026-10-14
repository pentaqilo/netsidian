package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/pentaqilo/netsidian/internal/ports"
	"net/http/httptest"
	"testing"
)

func TestRuntimeCapabilitiesMatchRegisteredSurfaces(t *testing.T) {
	env := newTestEnv(t, false)
	for _, mode := range []string{"none", "local", "external", "mixed"} {
		env.svc.Cfg.AuthMode = mode
		for _, picker := range []string{"none", "server", "native"} {
			env.hs.NativePick = nil
			env.hs.LocalFolderPick = picker == "server"
			if picker == "native" {
				env.hs.NativePick = func() (string, error) { return "", nil }
			}
			h := env.hs.Handler()
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/openapi.json", nil))
			var spec struct {
				Paths map[string]any `json:"paths"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
				t.Fatal(err)
			}
			_, nativeRoute := spec.Paths["/api/v1/native/vaults/attach"]
			_, localRoute := spec.Paths["/api/v1/local/vaults"]
			if nativeRoute != (picker == "native") || localRoute != (picker == "server") {
				t.Fatal("descriptor and routes disagree")
			}
			v := env.hs.runtimeCapabilities()
			if v.Auth.PasswordLogin != (mode == "local" || mode == "mixed") || v.Auth.Logout != (mode != "none") || v.Storage.FolderPicker != picker {
				t.Fatalf("wrong descriptor %s %s: %+v", mode, picker, v)
			}
			if (picker == "native") != (v.Kind == "desktop") {
				t.Fatal("runtime target disagrees with native bridge")
			}
		}
	}
	env.svc.Cfg.AuthMode = "local"
	env.hs.NativePick = nil
	env.hs.LocalFolderPick = false
	resp := env.call(t, "GET", "/api/v1/settings", "", "", "")
	defer resp.Body.Close()
	var body struct {
		Runtime  RuntimeCapabilities `json:"runtime"`
		AuthMode string              `json:"authMode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || body.AuthMode != "local" || body.Runtime.Kind != "web" || !body.Runtime.Auth.PasswordLogin {
		t.Fatal("public settings omitted the runtime contract")
	}
	resp = env.call(t, "GET", "/livez", "", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("liveness requires authentication")
	}
}

type unavailableHealthStorage struct{ ports.ContentStore }

func (unavailableHealthStorage) Exists(string, string) (bool, error) {
	return false, errors.New("storage outage")
}
func TestDependencyOutageDoesNotFailLiveness(t *testing.T) {
	env := newTestEnv(t, false)
	env.svc.Content = unavailableHealthStorage{env.svc.Content}
	for _, test := range []struct {
		path   string
		status int
	}{{"/api/v1/health", 503}, {"/healthz", 503}, {"/livez", 200}} {
		r := env.call(t, "GET", test.path, "", "", "")
		r.Body.Close()
		if r.StatusCode != test.status {
			t.Fatalf("%s: %d", test.path, r.StatusCode)
		}
	}
}

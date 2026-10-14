package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/storage/local"
	"github.com/pentaqilo/netsidian/internal/storage/providers"
)

func TestVaultProviderAuthorizationAndAtomicCreation(t *testing.T) {
	env := newTestEnv(t, true)
	cookie := env.setup(t, "op@example.com")
	repo := env.svc.Spaces.(ports.StorageSecretRepo)
	base := env.svc.Content
	manager, e := providers.New(base, repo, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	remote, _ := local.NewContent(t.TempDir())
	manager.Register("s3", func(context.Context, ports.StorageConfig) (ports.ContentStore, error) { return remote, nil })
	env.svc.Content = manager
	resp := env.call(t, "POST", "/api/v1/spaces", `{"name":"Remote","kind":"shared","storage":{"provider":"s3","bucket":"mine","region":"us-east-1","auth":"keys","accessKey":"id","secretKey":"secret"}}`, cookie, "")
	if resp.StatusCode != 201 {
		t.Fatal("create", resp.StatusCode)
	}
	var created struct {
		Space struct {
			ID string `json:"id"`
		}
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	id := created.Space.ID
	resp = env.call(t, "GET", "/api/v1/spaces/"+id+"/storage", "", cookie, "")
	var cfg map[string]any
	json.NewDecoder(resp.Body).Decode(&cfg)
	resp.Body.Close()
	if cfg["secretKey"] != nil || cfg["accessKey"] != nil || cfg["provider"] != "s3" {
		t.Fatal("redaction", cfg)
	}
	before, _, _ := env.svc.Spaces.SpacesForUser(context.Background(), func() string { u, _ := env.svc.Users.UserByEmail(context.Background(), "op@example.com"); return u.ID }())
	resp = env.call(t, "POST", "/api/v1/spaces", `{"name":"Broken","kind":"shared","storage":{"provider":"unknown"}}`, cookie, "")
	if resp.StatusCode < 400 {
		t.Fatal("unsupported provider accepted")
	}
	resp.Body.Close()
	ids, _ := env.svc.Spaces.AllSpaceIDs(context.Background())
	if len(ids) != len(before) {
		t.Fatal("failed creation left an orphan vault")
	}
	// An API token, including one with broad document scopes, cannot manage secrets.
	resp = env.call(t, "POST", "/api/v1/me/keys", `{"label":"agent","spaceIds":["`+id+`"],"scopes":["documents:read","documents:write"],"expiresInDays":7}`, cookie, "")
	var key struct {
		Secret string `json:"secret"`
	}
	json.NewDecoder(resp.Body).Decode(&key)
	resp.Body.Close()
	if key.Secret == "" {
		t.Fatal("key missing")
	}
	resp = env.call(t, "GET", "/api/v1/spaces/"+id+"/storage", "", "", key.Secret)
	if resp.StatusCode != 403 {
		t.Fatal("agent accessed storage settings", resp.StatusCode)
	}
	resp.Body.Close()
}

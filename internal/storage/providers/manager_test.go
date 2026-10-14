package providers

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/storage/local"
)

type secrets struct {
	profiles map[string][]byte
	fail     bool
}

func (s *secrets) VaultStorage(_ context.Context, id string) ([]byte, error) {
	return s.profiles[id], nil
}
func (s *secrets) SaveVaultStorage(_ context.Context, id string, b []byte, _ string) error {
	if s.fail {
		return errors.New("database unavailable")
	}
	s.profiles[id] = b
	return nil
}
func (s *secrets) OAuthStorageToken(context.Context, string) ([]byte, error)   { return nil, nil }
func (s *secrets) SaveOAuthStorageToken(context.Context, string, []byte) error { return nil }
func TestSealingRoutingAndRotation(t *testing.T) {
	ctx := context.Background()
	base, _ := local.NewContent(t.TempDir())
	remote, _ := local.NewContent(t.TempDir())
	repo := &secrets{profiles: map[string][]byte{}}
	key := bytes.Repeat([]byte{7}, 32)
	m, _ := New(base, repo, key)
	factory := func(_ context.Context, v ports.StorageConfig) (ports.ContentStore, error) {
		if v.SecretKey == "bad" {
			return nil, errors.New("bad credential")
		}
		return remote, nil
	}
	m.Register("s3", factory)
	id := "spc_storage"
	cfg := ports.StorageConfig{Provider: "s3", Bucket: "bucket", Region: "us-east-1", Auth: "keys", AccessKey: "access", SecretKey: "super-secret"}
	if e := m.Configure(ctx, id, "owner", cfg); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(repo.profiles[id], []byte("super-secret")) {
		t.Fatal("plaintext credential persisted")
	}
	description, e := m.Describe(ctx, id)
	if e != nil || description.SecretKey != "" || description.AccessKey != "" || description.OwnerID != "" {
		t.Fatal("credential leaked", description, e)
	}
	if e = m.WriteAtomic(id, "hello.md", []byte("remote")); e != nil {
		t.Fatal(e)
	}
	if found, _ := base.Exists(id, "hello.md"); found {
		t.Fatal("custom vault wrote to base")
	}
	// Reopening uses the encrypted durable profile, with the original credential owner.
	reopened, _ := New(base, repo, key)
	reopened.Register("s3", factory)
	b, e := reopened.Read(id, "hello.md")
	if e != nil || string(b) != "remote" {
		t.Fatal("restart routing failed", e)
	}
	var value any
	if e = m.Open("vault:other", repo.profiles[id], &value); e == nil {
		t.Fatal("profile substitution accepted")
	}
	wrong, _ := New(base, repo, bytes.Repeat([]byte{9}, 32))
	wrong.Register("s3", factory)
	if _, e = wrong.Read(id, "hello.md"); e == nil {
		t.Fatal("wrong key fell back to local")
	}
	changed := cfg
	changed.Bucket = "other"
	if e = m.Configure(ctx, id, "owner", changed); e == nil {
		t.Fatal("bucket migration accepted")
	}
	changed = cfg
	changed.SecretKey = "bad"
	if e = m.Configure(ctx, id, "owner", changed); e == nil {
		t.Fatal("unverified credentials saved")
	}
	repo.fail = true
	changed.SecretKey = "new"
	if e = m.Configure(ctx, id, "owner", changed); e == nil {
		t.Fatal("database failure ignored")
	}
	repo.fail = false
	changed.AccessKey = ""
	changed.SecretKey = ""
	if e = m.Configure(ctx, id, "owner", changed); e != nil {
		t.Fatal("retain existing credentials failed", e)
	}
}
func TestEndpointPolicy(t *testing.T) {
	for _, endpoint := range []string{"http://169.254.169.254", "http://127.0.0.1:9510", "https://user:secret@example.org", "https://example.org/?token=secret", "file:///tmp/bucket"} {
		if _, e := endpointClient(endpoint, nil); e == nil {
			t.Fatalf("untrusted endpoint accepted: %s", endpoint)
		}
	}
	c, e := endpointClient("http://127.0.0.1:9510", []string{"http://127.0.0.1:9510"})
	if e != nil {
		t.Fatal(e)
	}
	if e = c.CheckRedirect(nil, nil); e == nil {
		t.Fatal("redirect allowed")
	}
}

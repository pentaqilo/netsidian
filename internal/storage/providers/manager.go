// Package providers routes vault operations through registered providers and
// seals credentials at rest. A provider failure never falls back to local data.
package providers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

type Factory func(context.Context, ports.StorageConfig) (ports.ContentStore, error)
type Manager struct {
	Base      ports.ContentStore
	repo      ports.StorageSecretRepo
	cipher    cipher.AEAD
	mu        sync.Mutex
	factories map[string]Factory
	cache     map[string]ports.ContentStore
	locks     map[string]*sync.Mutex
}

func New(base ports.ContentStore, repo ports.StorageSecretRepo, secret []byte) (*Manager, error) {
	if len(secret) != 32 {
		return nil, fmt.Errorf("storage sealing requires a 32-byte instance key")
	}
	k := hmac.New(sha256.New, secret)
	k.Write([]byte("netsidian/vault-storage/v1"))
	block, err := aes.NewCipher(k.Sum(nil))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Manager{Base: base, repo: repo, cipher: aead, factories: map[string]Factory{}, cache: map[string]ports.ContentStore{}, locks: map[string]*sync.Mutex{}}, nil
}

// Register is called during bootstrap before any requests are served.
func (m *Manager) Register(name string, f Factory) { m.factories[name] = f }
func (m *Manager) Providers() []string {
	out := []string{"base"}
	names := []string{}
	for name := range m.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return append(out, names...)
}
func (m *Manager) Seal(id string, v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	n := make([]byte, m.cipher.NonceSize())
	if _, err = rand.Read(n); err != nil {
		return nil, err
	}
	return m.cipher.Seal(n, n, b, []byte(id)), nil
}
func (m *Manager) Open(id string, b []byte, v any) error {
	n := m.cipher.NonceSize()
	if len(b) < n {
		return fmt.Errorf("invalid sealed storage profile")
	}
	plain, err := m.cipher.Open(nil, b[:n], b[n:], []byte(id))
	if err != nil {
		return fmt.Errorf("storage credential decryption failed")
	}
	return json.Unmarshal(plain, v)
}
func (m *Manager) profile(ctx context.Context, id string) (*ports.StorageConfig, error) {
	b, err := m.repo.VaultStorage(ctx, id)
	if err != nil || b == nil {
		return nil, err
	}
	// OwnerID is intentionally excluded from public JSON. Persist separately.
	var v struct {
		Config ports.StorageConfig
		Owner  string
	}
	if err = m.Open("vault:"+id, b, &v); err != nil {
		return nil, err
	}
	v.Config.OwnerID = v.Owner
	return &v.Config, nil
}
func (m *Manager) Describe(ctx context.Context, id string) (*ports.StorageConfig, error) {
	v, err := m.profile(ctx, id)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return &ports.StorageConfig{Provider: "base"}, nil
	}
	v.AccessKey = ""
	v.SecretKey = ""
	v.SessionToken = ""
	v.OwnerID = ""
	return v, nil
}

// BackupProfile returns the full sealed provider configuration including
// credentials. It exists only for the operator's quiesced backup tooling
// (internal/backup) and must never be reachable from an HTTP handler;
// Describe is the redacted view for all request paths.
func (m *Manager) BackupProfile(ctx context.Context, id string) (*ports.StorageConfig, error) {
	return m.profile(ctx, id)
}
func (m *Manager) Configure(ctx context.Context, id, owner string, v ports.StorageConfig) error {
	lock := m.lock(id)
	lock.Lock()
	defer lock.Unlock()
	factory, ok := m.factories[v.Provider]
	if !ok {
		return fmt.Errorf("unsupported vault storage provider")
	}
	old, err := m.profile(ctx, id)
	if err != nil {
		return err
	}
	if old != nil && (old.Provider != v.Provider || old.Bucket != v.Bucket || old.Region != v.Region || old.Endpoint != v.Endpoint || old.Prefix != v.Prefix || old.PathStyle != v.PathStyle) {
		return fmt.Errorf("storage location is immutable; create a new vault to migrate content")
	}
	if old != nil && v.Auth == old.Auth && v.AccessKey == "" && v.SecretKey == "" && v.SessionToken == "" {
		v.AccessKey = old.AccessKey
		v.SecretKey = old.SecretKey
		v.SessionToken = old.SessionToken
	}
	v.OwnerID = owner
	store, err := factory(ctx, v)
	if err != nil {
		return err
	}
	b, err := m.Seal("vault:"+id, struct {
		Config ports.StorageConfig
		Owner  string
	}{v, owner})
	if err != nil {
		return err
	}
	if err = m.repo.SaveVaultStorage(ctx, id, b, v.Provider+"-vault"); err != nil {
		return err
	}
	m.mu.Lock()
	m.cache[id] = store
	m.mu.Unlock()
	return nil
}
func (m *Manager) store(id string) (ports.ContentStore, error) {
	lock := m.lock(id)
	lock.Lock()
	defer lock.Unlock()
	m.mu.Lock()
	cached := m.cache[id]
	m.mu.Unlock()
	if v := cached; v != nil {
		return v, nil
	}
	ctx := context.Background()
	v, err := m.profile(ctx, id)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return m.Base, nil
	}
	factory, ok := m.factories[v.Provider]
	if !ok {
		return nil, fmt.Errorf("vault provider unavailable")
	}
	c, err := factory(ctx, *v)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.cache[id] = c
	m.mu.Unlock()
	return c, nil
}
func (m *Manager) WriteAtomic(id, p string, b []byte) error {
	c, e := m.store(id)
	if e != nil {
		return e
	}
	return c.WriteAtomic(id, p, b)
}
func (m *Manager) Read(id, p string) ([]byte, error) {
	c, e := m.store(id)
	if e != nil {
		return nil, e
	}
	return c.Read(id, p)
}
func (m *Manager) Exists(id, p string) (bool, error) {
	c, e := m.store(id)
	if e != nil {
		return false, e
	}
	return c.Exists(id, p)
}
func (m *Manager) Move(id, a, b string) error {
	c, e := m.store(id)
	if e != nil {
		return e
	}
	return c.Move(id, a, b)
}
func (m *Manager) Delete(id, p string) error {
	c, e := m.store(id)
	if e != nil {
		return e
	}
	return c.Delete(id, p)
}
func (m *Manager) BindExternal(id, p string) error {
	f, ok := m.Base.(interface{ BindExternal(string, string) error })
	if !ok {
		return domain.Unsupported("local folders unavailable")
	}
	return f.BindExternal(id, p)
}
func (m *Manager) UnbindExternal(id string) {
	if f, ok := m.Base.(interface{ UnbindExternal(string) }); ok {
		f.UnbindExternal(id)
	}
}

// Purge prepares the provider before metadata removal can cascade its profile.
func (m *Manager) PurgeSpace(id string) error {
	c, e := m.store(id)
	if e != nil {
		return e
	}
	f, ok := c.(interface{ PurgeSpace(string) error })
	if !ok {
		return domain.Unsupported("provider cannot purge vaults")
	}
	return f.PurgeSpace(id)
}
func (m *Manager) ListFiles(id, p string) ([]ports.FileEntry, error) {
	c, e := m.store(id)
	if e != nil {
		return nil, e
	}
	f, ok := c.(ports.AttachmentStore)
	if !ok {
		return nil, domain.Unsupported("provider cannot list attachments")
	}
	return f.ListFiles(id, p)
}

func (m *Manager) lock(id string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks[id] == nil {
		m.locks[id] = &sync.Mutex{}
	}
	return m.locks[id]
}

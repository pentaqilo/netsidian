package backup

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/storage/local"
	"github.com/pentaqilo/netsidian/internal/storage/providers"
)

type fixture struct {
	t           *testing.T
	dir         string // outer temp dir (instance A lives under dir/a)
	stateDir    string
	storageRoot string
	dbPath      string
	secret      []byte
	db          *sqlite.DB
}

func newFixture(t *testing.T, root string) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: root,
		stateDir:    filepath.Join(root, "state"),
		storageRoot: filepath.Join(root, "content"),
		dbPath:      filepath.Join(root, "state", "database", "netsidian.db"),
	}
	if err := os.MkdirAll(filepath.Join(f.stateDir, "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.secret = make([]byte, 32)
	if _, err := rand.Read(f.secret); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.stateDir, "secrets", "auth_secret"),
		[]byte(hex.EncodeToString(f.secret)), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(f.dbPath, 5000)
	if err != nil {
		t.Fatal(err)
	}
	f.db = db
	f.seed("u1", "Operator")
	t.Cleanup(func() { db.SQL.Close() })
	return f
}

func (f *fixture) seed(userID, name string) {
	f.t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := f.db.SQL.Exec(`INSERT INTO users (id, display_name, email, password_hash, is_operator, created_at) VALUES (?,?,?,?,1,?)`,
		userID, name, userID+"@example.test", "hash", now); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.db.SQL.Exec(`INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at) VALUES (?,?,?,?,?)`,
		"s1", userID, "tokhash", "2999-01-01T00:00:00Z", now); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.db.SQL.Exec(`INSERT INTO api_keys (id, label, principal_id, secret_hash, scopes, space_ids, created_at) VALUES (?,?,?,?,?,?,?)`,
		"k1", "cli", userID, "keyhash", `["read"]`, `[]`, now); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) space(id, binding string) {
	f.t.Helper()
	if _, err := f.db.SQL.Exec(`INSERT INTO spaces (id, slug, name, storage_binding, created_at) VALUES (?,?,?,?,?)`,
		id, id, "Vault "+id, binding, time.Now().UTC().Format(time.RFC3339)); err != nil {
		f.t.Fatal(err)
	}
}

// seedDoc writes content + history bytes and the DB rows for one live
// document with a single revision, mirroring what the services produce.
func (f *fixture) seedDoc(spaceID, docID, path, content string) {
	f.t.Helper()
	f.seedDocRev(spaceID, docID, docID+"-rev", path, content)
}

func (f *fixture) seedDocRev(spaceID, docID, revID, path, content string) {
	f.t.Helper()
	sha := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := f.db.SQL.Exec(`INSERT INTO documents (id, space_id, path, norm_key, kind, current_revision, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		docID, spaceID, path, strings.ToLower(path), "note", revID, now, now); err != nil {
		f.t.Fatal(err)
	}
	histRel := filepath.Join(spaceID, docID, revID+".md")
	histLoc := filepath.Join(f.stateDir, "history", histRel)
	if err := os.MkdirAll(filepath.Dir(histLoc), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(histLoc, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.db.SQL.Exec(`INSERT INTO revisions (id, document_id, sha256, byte_length, creator, history_location, created_at) VALUES (?,?,?,?,?,?,?)`,
		revID, docID, sha, len(content), "u1", histLoc, now); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.db.SQL.Exec(`INSERT INTO metadata_index (document_id, space_id, revision, title) VALUES (?,?,?,?)`,
		docID, spaceID, revID, path); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.db.SQL.Exec(`INSERT INTO search_index (document_id, space_id, revision, title, body) VALUES (?,?,?,?,?)`,
		docID, spaceID, revID, path, content); err != nil {
		f.t.Fatal(err)
	}
}

// putContent writes managed local content bytes for a space-relative path.
func (f *fixture) putContent(spaceID, rel, content string) {
	f.t.Helper()
	full := filepath.Join(f.storageRoot, "spaces", spaceID, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) backup(t *testing.T, opts Options) string {
	t.Helper()
	if opts.StateDir == "" {
		opts.StateDir = f.stateDir
	}
	if opts.StorageRoot == "" {
		opts.StorageRoot = f.storageRoot
	}
	if opts.SQLitePath == "" {
		opts.SQLitePath = f.dbPath
	}
	archive := filepath.Join(f.dir, "backup.zip")
	if _, err := Backup(context.Background(), opts, archive); err != nil {
		t.Fatalf("backup: %v", err)
	}
	return archive
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	src := t.TempDir()
	f := newFixture(t, src)
	f.space("sp1", "local-main")
	f.seedDoc("sp1", "d1", "notes/one.md", "# one\nhello")
	f.seedDoc("sp1", "d2", "notes/two.md", "# two\nworld")
	f.putContent("sp1", "notes/one.md", "# one\nhello")
	f.putContent("sp1", "notes/two.md", "# two\nworld")
	archive := f.backup(t, Options{})

	target := filepath.Join(t.TempDir(), "restored")
	report, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if report.Accounts != 1 || report.Documents != 2 || report.Revisions != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.ContentVerified != 2 || report.HistoryVerified != 2 {
		t.Fatalf("verification counts: %+v", report)
	}
	if report.Sessions != 1 || report.APIKeys != 1 {
		t.Fatalf("credentials not restored: %+v", report)
	}
	if len(report.BindingRewrites) != 0 {
		t.Fatalf("local-main restore should not rewrite bindings: %v", report.BindingRewrites)
	}
	for _, p := range []string{
		filepath.Join(target, "database", "netsidian.db"),
		filepath.Join(target, "secrets", "auth_secret"),
		filepath.Join(target, "content", "spaces", "sp1", "notes", "one.md"),
		filepath.Join(target, "history", "sp1", "d1", "d1-rev.md"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("restored file missing %s: %v", p, err)
		}
	}
	// Restore must refuse to overwrite an existing instance.
	if _, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target}); err == nil {
		t.Fatal("second restore into the same target must fail")
	}
}

// tamperArchive rewrites one entry's bytes while keeping the manifest.
func tamperArchive(t *testing.T, archive, entryPath string, newBytes []byte) {
	t.Helper()
	r, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	tmp := archive + ".tampered"
	w, err := os.Create(tmp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(w)
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		hdr := f.FileHeader
		hdr.Method = zip.Deflate
		out, err := zw.CreateHeader(&hdr)
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == entryPath {
			if _, err := out.Write(newBytes); err != nil {
				t.Fatal(err)
			}
		} else {
			b, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := out.Write(b); err != nil {
				t.Fatal(err)
			}
		}
		rc.Close()
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	w.Close()
	if err := os.Rename(tmp, archive); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreDetectsTamperedEntry(t *testing.T) {
	src := t.TempDir()
	f := newFixture(t, src)
	f.space("sp1", "local-main")
	f.seedDoc("sp1", "d1", "notes/one.md", "# one\nhello")
	f.putContent("sp1", "notes/one.md", "# one\nhello")
	archive := f.backup(t, Options{})
	tamperArchive(t, archive, "content/spaces/sp1/notes/one.md", []byte("# one\nHELL!"))

	target := filepath.Join(t.TempDir(), "restored")
	_, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected sha256 mismatch, got: %v", err)
	}
}

func TestRestoreRejectsUnsafeAndUndeclaredEntries(t *testing.T) {
	build := func(t *testing.T, entries map[string]string, manifestEntries []Entry) string {
		t.Helper()
		archive := filepath.Join(t.TempDir(), "evil.zip")
		w, _ := os.Create(archive)
		zw := zip.NewWriter(w)
		man := Manifest{Format: Format, Entries: manifestEntries}
		mj, _ := json.Marshal(man)
		mw, _ := zw.Create("manifest.json")
		mw.Write(mj)
		for name, body := range entries {
			ew, err := zw.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			ew.Write([]byte(body))
		}
		zw.Close()
		w.Close()
		return archive
	}
	// Path traversal must be rejected even when declared by the manifest.
	arch := build(t, map[string]string{"../evil.md": "x"},
		[]Entry{{Path: "../evil.md", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("x"))), Bytes: 1}})
	if _, err := Restore(context.Background(), RestoreOptions{Archive: arch, TargetStateDir: filepath.Join(t.TempDir(), "r")}); err == nil ||
		!strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("expected unsafe path rejection, got: %v", err)
	}
	// An entry missing from the manifest fails closed.
	arch = build(t, map[string]string{"content/undeclared.md": "x"}, nil)
	if _, err := Restore(context.Background(), RestoreOptions{Archive: arch, TargetStateDir: filepath.Join(t.TempDir(), "r")}); err == nil ||
		!strings.Contains(err.Error(), "not declared") {
		t.Fatalf("expected undeclared entry rejection, got: %v", err)
	}
}

func TestAttachedFolderVaultRestoresIntoInstance(t *testing.T) {
	src := t.TempDir()
	f := newFixture(t, src)
	attached := filepath.Join(src, "outside-folder")
	if err := os.MkdirAll(attached, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attached, "note.md"), []byte("# attached"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.space("spA", "local-folder:"+attached)
	f.seedDoc("spA", "dA", "note.md", "# attached")

	archive := f.backup(t, Options{})
	target := filepath.Join(t.TempDir(), "restored")
	report, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(report.BindingRewrites) != 1 || !strings.Contains(report.BindingRewrites[0], "attached/spA") {
		t.Fatalf("binding not rewritten: %v", report.BindingRewrites)
	}
	if report.ContentVerified != 1 {
		t.Fatalf("attached content not verified: %+v", report)
	}
	restoredPath := filepath.Join(target, "attached", "spA", "note.md")
	if b, err := os.ReadFile(restoredPath); err != nil || string(b) != "# attached" {
		t.Fatalf("attached file not restored: %v", err)
	}
	db, err := sqlite.Open(filepath.Join(target, "database", "netsidian.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	var binding string
	if err := db.SQL.QueryRow(`SELECT storage_binding FROM spaces WHERE id='spA'`).Scan(&binding); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(binding, "local-folder:") || !strings.Contains(binding, filepath.Join("attached", "spA")) {
		t.Fatalf("binding in restored DB = %q, want rewritten under %s", binding, target)
	}
}

type fakeRemote struct{ files map[string][]byte }

func (s fakeRemote) Keys(ctx context.Context) ([]string, error) {
	out := make([]string, 0, len(s.files))
	for k := range s.files {
		out = append(out, k)
	}
	return out, nil
}
func (s fakeRemote) Read(ctx context.Context, key string) ([]byte, error) {
	b, ok := s.files[key]
	if !ok {
		return nil, fmt.Errorf("no such key %s", key)
	}
	return b, nil
}

func TestRemoteVaultRestoresAsManagedContent(t *testing.T) {
	src := t.TempDir()
	f := newFixture(t, src)
	f.space("spR", "s3-vault")
	f.seedDoc("spR", "dR", "notes/remote.md", "# remote bytes")

	// Seal a provider profile with the instance key, exactly as Configure does.
	base, err := local.NewContent(f.storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := providers.New(base, f.db, f.secret)
	if err != nil {
		t.Fatal(err)
	}
	cfg := ports.StorageConfig{Provider: "s3", Bucket: "b", Region: "us-east-1",
		Endpoint: "https://s3.example.test", Prefix: "cs", Auth: "keys", AccessKey: "ak", SecretKey: "sk"}
	sealed, err := mgr.Seal("vault:spR", struct {
		Config ports.StorageConfig
		Owner  string
	}{cfg, "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.SQL.Exec(`INSERT INTO vault_storage (space_id, sealed) VALUES (?,?)`, "spR", sealed); err != nil {
		t.Fatal(err)
	}

	content := "# remote bytes"
	archive := f.backup(t, Options{
		VaultConfig: func(ctx context.Context, spaceID, binding string) (*ports.StorageConfig, error) {
			return mgr.BackupProfile(ctx, spaceID)
		},
		RemoteSourceFor: func(ctx context.Context, cfg *ports.StorageConfig, spaceID string) (RemoteSource, error) {
			return fakeRemote{files: map[string][]byte{"notes/remote.md": []byte(content)}}, nil
		},
	})

	target := filepath.Join(t.TempDir(), "restored")
	report, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !report.SealedVerified || report.SealedProfiles != 1 {
		t.Fatalf("sealed profile not verified: %+v", report)
	}
	if len(report.BindingRewrites) != 1 || !strings.Contains(report.BindingRewrites[0], "local-main") {
		t.Fatalf("remote binding not rewritten: %v", report.BindingRewrites)
	}
	if report.ContentVerified != 1 {
		t.Fatalf("remote content not verified as managed content: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(target, "content", "spaces", "spR", "notes", "remote.md")); err != nil {
		t.Fatalf("remote bytes not restored as managed content: %v", err)
	}
	db, err := sqlite.Open(filepath.Join(target, "database", "netsidian.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	var n int
	if err := db.SQL.QueryRow(`SELECT count(*) FROM vault_storage`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rewritten remote vault kept its sealed profile: %d rows", n)
	}
	var binding string
	if err := db.SQL.QueryRow(`SELECT storage_binding FROM spaces WHERE id='spR'`).Scan(&binding); err != nil {
		t.Fatal(err)
	}
	if binding != "local-main" {
		t.Fatalf("binding = %q, want local-main", binding)
	}
}

func TestSealedSecondFactorSurvivesRestore(t *testing.T) {
	src := t.TempDir()
	f := newFixture(t, src)

	base, err := local.NewContent(f.storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := providers.New(base, f.db, f.secret)
	if err != nil {
		t.Fatal(err)
	}
	// Seal a second-factor secret with the instance key, exactly as enrollment does.
	secretB64 := "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	sealed, err := mgr.Seal("totp:u1", struct {
		Secret string `json:"secret"`
	}{secretB64})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := f.db.SQL.Exec(`INSERT INTO totp_enrollments (user_id, sealed_secret, confirmed, created_at, updated_at) VALUES (?,?,1,?,?)`,
		"u1", sealed, now, now); err != nil {
		t.Fatal(err)
	}

	for _, stmt := range []string{
		`INSERT INTO totp_recovery_codes(id,user_id,code_hash,used_at,created_at) VALUES('spent','u1','spent-hash','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		`INSERT INTO totp_recovery_codes(id,user_id,code_hash,created_at) VALUES('unused','u1','unused-hash','2026-01-01T00:00:00Z')`,
		`INSERT INTO totp_used_steps(user_id,step,used_at) VALUES('u1',12345,'2026-01-01T00:00:00Z')`,
		`INSERT INTO totp_attempt_budgets(user_id,window_started_at,attempts) VALUES('u1',123456,4)`,
	} {
		if _, err := f.db.SQL.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	archive := f.backup(t, Options{})
	target := filepath.Join(t.TempDir(), "restored")
	report, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !report.SealedVerified || report.SealedProfiles != 1 {
		t.Fatalf("sealed second-factor secret not verified: %+v", report)
	}

	// The restored instance key must open the sealed secret again.
	db, err := sqlite.Open(filepath.Join(target, "database", "netsidian.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	base2, err := local.NewContent(filepath.Join(target, "content"))
	if err != nil {
		t.Fatal(err)
	}
	mgr2, err := providers.New(base2, db, f.secret)
	if err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]int{
		`SELECT count(*) FROM totp_recovery_codes WHERE user_id='u1' AND used_at IS NULL`:     1,
		`SELECT count(*) FROM totp_recovery_codes WHERE user_id='u1' AND used_at IS NOT NULL`: 1,
		`SELECT step FROM totp_used_steps WHERE user_id='u1'`:                                 12345,
		`SELECT attempts FROM totp_attempt_budgets WHERE user_id='u1'`:                        4,
	} {
		var got int
		if err := db.SQL.QueryRow(query).Scan(&got); err != nil || got != want {
			t.Fatalf("factor state restore: %s got=%d want=%d err=%v", query, got, want, err)
		}
	}

	var blob []byte
	if err := db.SQL.QueryRow(`SELECT sealed_secret FROM totp_enrollments WHERE user_id='u1'`).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	var sec struct {
		Secret string `json:"secret"`
	}
	if err := mgr2.Open("totp:u1", blob, &sec); err != nil || sec.Secret != secretB64 {
		t.Fatalf("sealed secret did not survive the round trip: %+v err=%v", sec, err)
	}
}

func TestRestoreFailsOnIndexGap(t *testing.T) {
	src := t.TempDir()
	f := newFixture(t, src)
	f.space("sp1", "local-main")
	f.seedDoc("sp1", "d1", "notes/one.md", "# one")
	f.putContent("sp1", "notes/one.md", "# one")
	if _, err := f.db.SQL.Exec(`DELETE FROM search_index`); err != nil {
		t.Fatal(err)
	}
	archive := f.backup(t, Options{})
	target := filepath.Join(t.TempDir(), "restored")
	_, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target})
	if err == nil || !strings.Contains(err.Error(), "search index") {
		t.Fatalf("expected index gap failure, got: %v", err)
	}
}

func TestRestoreFailsOnDanglingLinkIndex(t *testing.T) {
	src := t.TempDir()
	f := newFixture(t, src)
	f.space("sp1", "local-main")
	f.seedDoc("sp1", "d1", "notes/one.md", "# one")
	f.putContent("sp1", "notes/one.md", "# one")
	if _, err := f.db.SQL.Exec(`INSERT INTO link_index (id, space_id, source_document_id, source_revision, target_kind, resolved_document_id, state) VALUES (?,?,?,?,?,?,?)`,
		"l1", "sp1", "d1", "d1-rev", "path", "ghost-doc", "resolved"); err != nil {
		t.Fatal(err)
	}
	archive := f.backup(t, Options{})
	target := filepath.Join(t.TempDir(), "restored")
	_, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target})
	if err == nil || !strings.Contains(err.Error(), "link index") {
		t.Fatalf("expected dangling link failure, got: %v", err)
	}
}

func TestBackupRefusesUnavailableStorage(t *testing.T) {
	f := newFixture(t, t.TempDir())
	f.space("missing", "local-folder:"+filepath.Join(f.dir, "gone"))
	archive := filepath.Join(f.dir, "backup.zip")
	_, err := Backup(context.Background(), Options{StateDir: f.stateDir, StorageRoot: f.storageRoot, SQLitePath: f.dbPath}, archive)
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected unavailable storage error: %v", err)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatal("incomplete backup was published")
	}
}

func TestBackupRejectsExternallyChangedCurrentContent(t *testing.T) {
	f := newFixture(t, t.TempDir())
	f.space("sp1", "local-main")
	f.seedDoc("sp1", "d1", "note.md", "# indexed")
	f.putContent("sp1", "note.md", "# external edit")
	archive := filepath.Join(f.dir, "backup.zip")
	if _, err := Backup(context.Background(), Options{StateDir: f.stateDir, StorageRoot: f.storageRoot, SQLitePath: f.dbPath}, archive); err == nil {
		t.Fatal("externally changed content produced successful backup")
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatal("inconsistent archive published")
	}
}

func TestBackupPreservesAttachmentsEmptyFoldersAndRelocatedHistory(t *testing.T) {
	f := newFixture(t, t.TempDir())
	f.space("sp1", "local-main")
	f.seedDoc("sp1", "d1", "note.md", "# note")
	f.putContent("sp1", "note.md", "# note")
	f.putContent("sp1", "files/picture.png", "binary picture bytes")
	if err := os.MkdirAll(filepath.Join(f.storageRoot, "spaces", "sp1", "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.stateDir, "desktop-settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := f.backup(t, Options{})
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("archive exposes secrets: %o", info.Mode().Perm())
	}
	target := filepath.Join(t.TempDir(), "restored")
	r, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target})
	if err != nil {
		t.Fatal(err)
	}
	if r.Attachments != 1 {
		t.Fatalf("attachments=%d", r.Attachments)
	}
	for _, path := range []string{"content/spaces/sp1/empty", "desktop-settings.json", "content/spaces/sp1/files/picture.png"} {
		if _, err := os.Stat(filepath.Join(target, path)); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sqlite.Open(filepath.Join(target, "database", "netsidian.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	var location string
	if err := db.SQL.QueryRow(`SELECT history_location FROM revisions WHERE id='d1-rev'`).Scan(&location); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(location, target+string(filepath.Separator)) {
		t.Fatal("history still points at source", location)
	}
	if b, err := os.ReadFile(location); err != nil || string(b) != "# note" {
		t.Fatalf("relocated history unreadable: %q %v", b, err)
	}
}

func TestRestorePreservesExistingTargetsAndCleansFailure(t *testing.T) {
	f := newFixture(t, t.TempDir())
	f.space("sp1", "local-main")
	archive := f.backup(t, Options{})
	target := t.TempDir()
	sentinel := filepath.Join(target, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target}); err == nil {
		t.Fatal("nonempty target accepted")
	}
	if b, err := os.ReadFile(sentinel); err != nil || string(b) != "keep" {
		t.Fatal("existing files changed")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: link}); err == nil {
		t.Fatal("symlink target accepted")
	}
	fresh := filepath.Join(t.TempDir(), "fresh")
	if _, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: fresh, MaxBytes: 1}); err == nil {
		t.Fatal("decompression bound ignored")
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatal("failed restore target not cleaned")
	}
	if _, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: fresh}); err != nil {
		t.Fatal("safe restart failed", err)
	}
}

func TestBackupRefusesOverwriteRemoteOmissionsAndSymlinks(t *testing.T) {
	for _, scenario := range []string{"remote", "symlink", "existing"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t, t.TempDir())
			f.space("sp1", "local-main")
			archive := filepath.Join(f.dir, "backup.zip")
			switch scenario {
			case "remote":
				f.space("remote", "s3-vault")
			case "symlink":
				if err := os.MkdirAll(f.storageRoot, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(f.stateDir, "secrets", "auth_secret"), filepath.Join(f.storageRoot, "link")); err != nil {
					t.Fatal(err)
				}
			case "existing":
				if err := os.WriteFile(archive, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Backup(context.Background(), Options{StateDir: f.stateDir, StorageRoot: f.storageRoot, SQLitePath: f.dbPath}, archive)
			if err == nil {
				t.Fatal("unsafe or incomplete backup accepted")
			}
			if scenario == "existing" {
				if b, _ := os.ReadFile(archive); string(b) != "keep" {
					t.Fatal("existing archive overwritten")
				}
			} else if _, err := os.Stat(archive); !os.IsNotExist(err) {
				t.Fatal("incomplete archive published")
			}
		})
	}
}

func rewriteArchive(t *testing.T, archive string, mutateManifest func(*Manifest), mutateFiles func([]*zip.File) []*zip.File) {
	t.Helper()
	r, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var m Manifest
	var files []*zip.File
	for _, f := range r.File {
		if f.Name == "manifest.json" {
			rc, _ := f.Open()
			err = json.NewDecoder(rc).Decode(&m)
			rc.Close()
			if err != nil {
				t.Fatal(err)
			}
		} else {
			files = append(files, f)
		}
	}
	if mutateManifest != nil {
		mutateManifest(&m)
	}
	if mutateFiles != nil {
		files = mutateFiles(files)
	}
	out, err := os.Create(archive + ".rewrite")
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(out)
	for _, f := range files {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		header := f.FileHeader
		dst, err := w.CreateHeader(&header)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(dst, rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	dst, err := w.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(dst).Encode(m); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(archive+".rewrite", archive); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsIncompleteAndDuplicateInventory(t *testing.T) {
	for _, scenario := range []string{"missing", "duplicate", "manifest-duplicate", "counts"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t, t.TempDir())
			f.space("sp1", "local-main")
			f.putContent("sp1", "file.png", "picture")
			archive := f.backup(t, Options{})
			switch scenario {
			case "missing":
				rewriteArchive(t, archive, nil, func(files []*zip.File) []*zip.File {
					for i, f := range files {
						if f.Name == "content/spaces/sp1/file.png" {
							return append(files[:i], files[i+1:]...)
						}
					}
					return files
				})
			case "duplicate":
				rewriteArchive(t, archive, nil, func(files []*zip.File) []*zip.File { return append(files, files[0]) })
			case "manifest-duplicate":
				rewriteArchive(t, archive, func(m *Manifest) { m.Entries = append(m.Entries, m.Entries[0]) }, nil)
			case "counts":
				rewriteArchive(t, archive, func(m *Manifest) { m.Counts.Users++ }, nil)
			}
			target := filepath.Join(t.TempDir(), "restored")
			if _, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target}); err == nil {
				t.Fatal("invalid inventory accepted")
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("failed restore published target")
			}
		})
	}
}

func TestAttachmentTrashSurvivesBackupRestore(t *testing.T) {
	f := newFixture(t, t.TempDir())
	f.space("sp1", "local-main")
	content, err := local.NewContent(f.storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("%PDF-1.4\noriginal bytes\n%%EOF")
	if err := content.WriteAtomic("sp1", "Docs/sample.pdf", original); err != nil {
		t.Fatal(err)
	}
	if err := content.StashAttachment("sp1", "Docs/sample.pdf", "ftr_pdf"); err != nil {
		t.Fatal(err)
	}
	archive := f.backup(t, Options{})
	target := filepath.Join(t.TempDir(), "restored")
	if _, err := Restore(context.Background(), RestoreOptions{Archive: archive, TargetStateDir: target}); err != nil {
		t.Fatal(err)
	}
	restored, err := local.NewContent(filepath.Join(target, "content"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := restored.StashedFolders("sp1")
	if err != nil || len(entries) != 1 || entries[0].Kind != "attachment" {
		t.Fatalf("trash catalog lost: %+v %v", entries, err)
	}
	if _, conflicts, err := restored.UnstashFolder("sp1", "ftr_pdf"); err != nil || len(conflicts) > 0 {
		t.Fatalf("trash restore failed: %v %v", conflicts, err)
	}
	got, err := restored.Read("sp1", "Docs/sample.pdf")
	if err != nil || string(got) != string(original) {
		t.Fatalf("trashed PDF bytes lost: %v", err)
	}
}

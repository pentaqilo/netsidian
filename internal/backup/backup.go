// Package backup implements quiesced, storage-aware backup and verified
// restore for a Netsidian instance. The archive covers everything an
// instance needs to come back on a different machine: the SQLite state
// snapshot, secrets, journal and revision history, managed local content,
// attached-folder vaults and remote (S3) vault content. A manifest records
// per-entry SHA-256 checksums and the vault inventory so a restore can
// verify the result instead of assuming it.
//
// Quiesced means the server must not be running while the backup is taken,
// and external writers (editors syncing attached folders) must also be
// quiesced. Online application backup is separate, uncommitted work.
package backup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
)

const Format = "netsidian-backup/v1"

// Entry is one archived file with its integrity record.
type Entry struct {
	Path      string `json:"path"`   // archive-relative, forward slashes
	SHA256    string `json:"sha256"` // hex sha256 of the archived bytes
	Bytes     int64  `json:"bytes"`
	Directory bool   `json:"directory,omitempty"`
}

// Vault records one vault and where its bytes came from.
type Vault struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Binding string `json:"binding"`
	// Source: content (managed local), attached (local-folder binding),
	// remote (provider bytes captured). Unavailable sources fail the backup.
	Source string `json:"source"`
	Files  int    `json:"files"`
}

// Counts summarizes the state snapshot so a restore can compare.
type Counts struct {
	Users       int `json:"users"`
	Sessions    int `json:"sessions"`
	APIKeys     int `json:"apiKeys"`
	Documents   int `json:"documents"`
	Attachments int `json:"attachments"`
	Revisions   int `json:"revisions"`
}

// Manifest is the archive's own inventory, stored as manifest.json.
// The unexported fields stage where each entry's bytes live during the
// write; they are never serialized.
type Manifest struct {
	Format    string    `json:"format"`
	CreatedAt time.Time `json:"createdAt"`
	Version   string    `json:"version"`
	AuthMode  string    `json:"authMode"`
	Counts    Counts    `json:"counts"`
	Vaults    []Vault   `json:"vaults"`
	Entries   []Entry   `json:"entries"`
	Warnings  []string  `json:"warnings,omitempty"`

	srcState    string            // state dir the state/* entries came from
	srcDB       string            // staged VACUUM INTO snapshot for the db entry
	srcContent  string            // managed content root for content/* entries
	srcAttached map[string]string // space id -> attached folder root
	remoteFiles map[string]string // archive path -> captured remote bytes
}

// RemoteSource reads one vault's bytes from a remote provider.
type RemoteSource interface {
	Keys(ctx context.Context) ([]string, error)
	Read(ctx context.Context, key string) ([]byte, error)
}

// Options configures a backup run. StateDir/StorageRoot/SQLitePath mirror
// the instance configuration.
type Options struct {
	StateDir    string
	StorageRoot string
	SQLitePath  string
	Version     string
	AuthMode    string
	// VaultConfig returns the full provider configuration for a remote-backed
	// vault (sealed per-vault profile or the instance S3 configuration), or
	// nil when the vault has no recoverable provider configuration.
	VaultConfig func(ctx context.Context, spaceID, binding string) (*ports.StorageConfig, error)
	// RemoteSourceFor builds a reader for one remote vault. When nil, remote
	// vaults fail the backup rather than silently omit their content.
	RemoteSourceFor func(ctx context.Context, cfg *ports.StorageConfig, spaceID string) (RemoteSource, error)
}

// Backup writes a quiesced archive to archivePath and returns the manifest.
func Backup(ctx context.Context, opts Options, archivePath string) (*Manifest, error) {
	if opts.StateDir == "" || opts.StorageRoot == "" || opts.SQLitePath == "" {
		return nil, fmt.Errorf("backup requires state dir, storage root and sqlite path")
	}
	if _, err := os.Lstat(archivePath); !os.IsNotExist(err) {
		return nil, fmt.Errorf("archive output must not exist")
	}
	for _, root := range []string{opts.StateDir, opts.StorageRoot} {
		if info, err := os.Lstat(root); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("source storage root must not be a symlink")
		}
		if inside(root, archivePath) {
			return nil, fmt.Errorf("archive output must be outside source storage")
		}
	}
	if _, err := os.Stat(opts.SQLitePath); err != nil {
		return nil, fmt.Errorf("existing instance database required: %w", err)
	}
	if _, err := readHexSecret(filepath.Join(opts.StateDir, "secrets", "auth_secret")); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp("", "netsidian-backup-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	db, err := sqlite.OpenReadOnly(opts.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("open state database: %w", err)
	}
	defer db.SQL.Close()

	man := &Manifest{
		Format: Format, CreatedAt: time.Now().UTC(),
		Version: opts.Version, AuthMode: opts.AuthMode,
		srcState: opts.StateDir, srcContent: opts.StorageRoot,
		srcAttached: map[string]string{}, remoteFiles: map[string]string{},
	}

	// 1. Consistent single-file database snapshot (VACUUM INTO folds the WAL
	// in and compacts; safe because the instance is quiesced).
	tmpPath := filepath.Join(staging, "snapshot.db")
	if _, err := db.SQL.ExecContext(ctx, `VACUUM INTO ?`, tmpPath); err != nil {
		return nil, fmt.Errorf("database snapshot: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return nil, err
	}
	snapshot, err := sql.Open("sqlite", "file:"+filepath.ToSlash(tmpPath)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer snapshot.Close()
	man.srcDB = tmpPath
	defer os.Remove(tmpPath)
	if err := man.addFile(tmpPath, "state/database/netsidian.db"); err != nil {
		return nil, err
	}

	// Preserve instance state outside managed content and the separately
	// snapshotted SQLite database (including desktop configuration).
	if err := filepath.WalkDir(opts.StateDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == opts.StateDir {
			return nil
		}
		if inside(opts.StorageRoot, p) || inside(filepath.Join(opts.StateDir, "content"), p) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		for _, dbPath := range []string{opts.SQLitePath, filepath.Join(opts.StateDir, "database", "netsidian.db")} {
			for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
				absP, _ := filepath.Abs(p)
				absDB, _ := filepath.Abs(dbPath + suffix)
				if absP == absDB {
					return nil
				}
			}
		}
		rel, err := filepath.Rel(opts.StateDir, p)
		if err != nil {
			return err
		}
		return man.addPath(p, "state/"+filepath.ToSlash(rel), d)
	}); err != nil {
		return nil, err
	}
	// 3. Managed local content.
	if err := man.addTree(opts.StorageRoot, "content"); err != nil {
		return nil, err
	}

	// 4. Storage-aware vault inventory: attached folders and remote content.
	if err := man.collectVaults(ctx, snapshot, opts); err != nil {
		return nil, err
	}

	var pending int
	if err := snapshot.QueryRowContext(ctx, `SELECT count(*) FROM operations WHERE phase IN ('pending','storage_written')`).Scan(&pending); err != nil {
		return nil, err
	}
	if pending != 0 {
		return nil, fmt.Errorf("reconcile interrupted operations before backup")
	}
	// 5. State counts from the snapshot connection.
	if err := man.collectCounts(ctx, snapshot); err != nil {
		return nil, err
	}
	if err := man.verifyRevisionInventory(ctx, snapshot); err != nil {
		return nil, err
	}

	for _, root := range man.srcAttached {
		if inside(root, archivePath) {
			return nil, fmt.Errorf("archive output must be outside attached storage")
		}
	}
	// 6. Write the archive: payload entries, then the manifest.
	f, err := os.CreateTemp(filepath.Dir(archivePath), ".netsidian-archive-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	seen := map[string]bool{}
	for _, e := range man.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := safeArchivePath(e.Path); err != nil {
			return nil, err
		}
		if seen[e.Path] {
			return nil, fmt.Errorf("duplicate source path %s", e.Path)
		}
		seen[e.Path] = true
		header := &zip.FileHeader{Name: e.Path, Method: zip.Deflate}
		if e.Directory {
			header.SetMode(os.ModeDir | 0o700)
		} else {
			header.SetMode(0o600)
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if e.Directory {
			continue
		}
		src, err := man.openEntry(e.Path)
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(w, h), src)
		closeErr := src.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if n != e.Bytes || hex.EncodeToString(h.Sum(nil)) != e.SHA256 {
			return nil, fmt.Errorf("source changed during backup: %s; quiesce all writers", e.Path)
		}
	}
	mj, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return nil, err
	}
	w, err := zw.Create("manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(mj); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	// Atomic publication without overwriting an existing archive.
	if err := os.Link(f.Name(), archivePath); err != nil {
		return nil, err
	}
	return man, nil
}

func (m *Manifest) collectVaults(ctx context.Context, db *sql.DB, opts Options) error {
	rows, err := db.QueryContext(ctx, `SELECT id, name, kind, storage_binding FROM spaces ORDER BY id`)
	if err != nil {
		return err
	}
	type spaceRow struct {
		id, name, kind, binding string
	}
	var spaces []spaceRow
	for rows.Next() {
		var s spaceRow
		if err := rows.Scan(&s.id, &s.name, &s.kind, &s.binding); err != nil {
			rows.Close()
			return err
		}
		spaces = append(spaces, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range spaces {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := safeArchivePath("content/spaces/" + s.id); err != nil {
			return err
		}
		v := Vault{ID: s.id, Name: s.name, Kind: s.kind, Binding: s.binding}
		switch {
		case s.binding == "local-main":
			v.Source = "content"
			v.Files = m.countPrefix("content/spaces/" + s.id + "/")
		case strings.HasPrefix(s.binding, "local-folder:"):
			dir := strings.TrimPrefix(s.binding, "local-folder:")
			if inside(dir, opts.SQLitePath) {
				return fmt.Errorf("attached folder overlaps instance database")
			}
			v.Source = "attached"
			if _, err := os.Stat(dir); err != nil {
				return fmt.Errorf("vault %s attached folder is unavailable: %w", s.id, err)
			} else {
				m.srcAttached[s.id] = dir
				if err := m.addTree(dir, "attached/"+s.id); err != nil {
					return err
				}
			}
			v.Files = m.countPrefix("attached/" + s.id + "/")
		case s.binding == "s3-main" || s.binding == "s3-vault":
			cfg := (*ports.StorageConfig)(nil)
			if opts.VaultConfig != nil {
				var err error
				cfg, err = opts.VaultConfig(ctx, s.id, s.binding)
				if err != nil {
					return fmt.Errorf("vault %s provider configuration: %w", s.id, err)
				}
			}
			if cfg == nil || opts.RemoteSourceFor == nil {
				return fmt.Errorf("vault %s (%s) content cannot be captured: provider credentials/resolver required", s.id, s.binding)
			}
			src, err := opts.RemoteSourceFor(ctx, cfg, s.id)
			if err != nil {
				return fmt.Errorf("vault %s remote source: %w", s.id, err)
			}
			keys, err := src.Keys(ctx)
			if err != nil {
				return fmt.Errorf("vault %s remote listing: %w", s.id, err)
			}
			v.Source = "remote"
			for _, k := range keys {
				b, err := src.Read(ctx, k)
				if err != nil {
					return fmt.Errorf("vault %s remote read %s: %w", s.id, k, err)
				}
				ap := "remote/" + s.id + "/" + k
				if err := safeArchivePath(ap); err != nil {
					return fmt.Errorf("unsafe remote key: %w", err)
				}
				if err := m.addBytes(b, ap); err != nil {
					return err
				}
				spool, err := os.CreateTemp(filepath.Dir(m.srcDB), "remote-*")
				if err != nil {
					return err
				}
				_, err = spool.Write(b)
				closeErr := spool.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
				m.remoteFiles[ap] = spool.Name()
				v.Files++
			}
		default:
			return fmt.Errorf("vault %s has unknown storage binding %q", s.id, s.binding)
		}
		if err := safeArchivePath("content/spaces/" + s.id); err != nil {
			return err
		}
		m.Vaults = append(m.Vaults, v)
	}
	return nil
}

func (m *Manifest) collectCounts(ctx context.Context, db *sql.DB) error {
	scans := []struct {
		q    string
		dest *int
	}{
		{`SELECT count(*) FROM users`, &m.Counts.Users},
		{`SELECT count(*) FROM sessions WHERE revoked_at IS NULL`, &m.Counts.Sessions},
		{`SELECT count(*) FROM api_keys WHERE revoked_at IS NULL`, &m.Counts.APIKeys},
		{`SELECT count(*) FROM documents WHERE tombstone_at IS NULL`, &m.Counts.Documents},

		{`SELECT count(*) FROM revisions`, &m.Counts.Revisions},
	}
	for _, s := range scans {
		if err := db.QueryRowContext(ctx, s.q).Scan(s.dest); err != nil {
			return err
		}
	}
	m.Counts.Attachments = attachmentCount(m.Entries)
	return nil
}

// addTree archives every regular file under dir as prefix/<relative>.
// Symlinks and special files fail the backup; no bytes are silently omitted.
func (m *Manifest) addTree(dir, prefix string) error {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		ap := prefix
		if rel != "." {
			ap += "/" + filepath.ToSlash(rel)
		}
		return m.addPath(p, ap, d)
	})
}

func (m *Manifest) addFile(path, archivePath string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	m.Entries = append(m.Entries, Entry{Path: archivePath, SHA256: hex.EncodeToString(h.Sum(nil)), Bytes: n})
	return nil
}

func (m *Manifest) addBytes(b []byte, archivePath string) error {
	h := sha256.Sum256(b)
	m.Entries = append(m.Entries, Entry{Path: archivePath, SHA256: hex.EncodeToString(h[:]), Bytes: int64(len(b))})
	return nil
}

func (m *Manifest) countPrefix(prefix string) int {
	n := 0
	for _, e := range m.Entries {
		if strings.HasPrefix(e.Path, prefix) {
			n++
		}
	}
	return n
}

// openEntry resolves an archive path back to its staged source.
func (m *Manifest) openEntry(archivePath string) (io.ReadCloser, error) {
	switch {
	case archivePath == "state/database/netsidian.db":
		return os.Open(m.srcDB)
	case strings.HasPrefix(archivePath, "state/"):
		return os.Open(filepath.Join(m.srcState, archivePath[len("state/"):]))
	case strings.HasPrefix(archivePath, "content/"):
		return os.Open(filepath.Join(m.srcContent, archivePath[len("content/"):]))
	case strings.HasPrefix(archivePath, "attached/"):
		spaceID, rest, ok := strings.Cut(archivePath[len("attached/"):], "/")
		if !ok {
			return nil, fmt.Errorf("malformed attached entry %q", archivePath)
		}
		root, ok := m.srcAttached[spaceID]
		if !ok {
			return nil, fmt.Errorf("attached entry %q has no source folder", archivePath)
		}
		return os.Open(filepath.Join(root, filepath.FromSlash(rest)))
	case strings.HasPrefix(archivePath, "remote/"):
		p, ok := m.remoteFiles[archivePath]
		if !ok {
			return nil, fmt.Errorf("remote entry %q was not captured", archivePath)
		}
		return os.Open(p)
	default:
		return nil, fmt.Errorf("unresolvable archive entry %q", archivePath)
	}
}

func (m *Manifest) addPath(p, ap string, d fs.DirEntry) error {
	if d.Type()&fs.ModeSymlink != 0 {
		return fmt.Errorf("symlink in backup source: %s", p)
	}
	if d.IsDir() {
		m.Entries = append(m.Entries, Entry{Path: ap, Directory: true})
		return nil
	}
	info, err := d.Info()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported file in backup source: %s", p)
	}
	return m.addFile(p, ap)
}
func inside(root, name string) bool {
	root, _ = filepath.Abs(root)
	name, _ = filepath.Abs(name)
	if canonical, err := filepath.EvalSymlinks(root); err == nil {
		root = canonical
	}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(name)); err == nil {
		name = filepath.Join(parent, filepath.Base(name))
	}
	rel, err := filepath.Rel(root, name)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func attachmentCount(entries []Entry) int {
	n := 0
	for _, e := range entries {
		if e.Directory || !(strings.HasPrefix(e.Path, "content/spaces/") || strings.HasPrefix(e.Path, "attached/") || strings.HasPrefix(e.Path, "remote/")) {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Path)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".pdf", ".mp4", ".puml", ".plantuml", ".mmd":
			n++
		}
	}
	return n
}

// Detect unavailable or externally changed revision bytes before publishing.
func (m *Manifest) verifyRevisionInventory(ctx context.Context, db *sql.DB) error {
	entries := map[string]Entry{}
	vaults := map[string]Vault{}
	for _, e := range m.Entries {
		entries[e.Path] = e
	}
	for _, v := range m.Vaults {
		vaults[v.ID] = v
	}
	rows, err := db.QueryContext(ctx, `SELECT r.id,r.sha256,r.history_location,d.id,d.space_id,d.path,d.current_revision,d.tombstone_at FROM revisions r JOIN documents d ON d.id=r.document_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, hash, location, doc, space, path, current string
		var tombstone sql.NullString
		if err := rows.Scan(&id, &hash, &location, &doc, &space, &path, &current, &tombstone); err != nil {
			return err
		}
		paths := []string{}
		if location != "" || id != current || tombstone.Valid {
			paths = append(paths, "state/history/"+space+"/"+doc+"/"+id+".md")
		}
		if id == current && !tombstone.Valid {
			v := vaults[space]
			var ap string
			switch v.Source {
			case "content":
				ap = "content/spaces/" + space + "/" + path
			case "attached":
				ap = "attached/" + space + "/" + path
			case "remote":
				ap = "remote/" + space + "/" + path
			default:
				return fmt.Errorf("missing vault source for revision %s", id)
			}
			paths = append(paths, ap)
		}
		for _, ap := range paths {
			e, ok := entries[ap]
			if !ok || e.Directory || e.SHA256 != hash {
				return fmt.Errorf("revision %s content missing or changed; reconcile before backup", id)
			}
		}
	}
	return rows.Err()
}

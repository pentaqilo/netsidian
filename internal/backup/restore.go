package backup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/storage/local"
	"github.com/pentaqilo/netsidian/internal/storage/providers"
)

// RestoreOptions configures a restore into an isolated instance.
type RestoreOptions struct {
	Archive        string
	TargetStateDir string
	// StorageRoot is where managed content lands; defaults to
	// <TargetStateDir>/content (the instance default layout).
	StorageRoot  string
	MaxBytes     int64
	MaxFileBytes int64
	MaxEntries   int
}

// Report is the evidence a restore produced. Restore returns an error when
// any verification fails; the report details what was checked.
type Report struct {
	Accounts        int      `json:"accounts"`
	Sessions        int      `json:"sessions"`
	APIKeys         int      `json:"apiKeys"`
	Documents       int      `json:"documents"`
	Attachments     int      `json:"attachments"`
	Revisions       int      `json:"revisions"`
	HistoryVerified int      `json:"historyVerified"`
	ContentVerified int      `json:"contentVerified"`
	IndexMetadata   int      `json:"indexMetadata"`
	IndexSearch     int      `json:"indexSearch"`
	SealedProfiles  int      `json:"sealedProfiles"`
	SealedVerified  bool     `json:"sealedVerified"`
	BindingRewrites []string `json:"bindingRewrites"`
	Warnings        []string `json:"warnings,omitempty"`
}

// Restore extracts the archive into an empty target instance, rewrites
// storage bindings to the restored locations, and verifies accounts,
// sealed secrets, document/attachment content, revision history and index
// consistency against the manifest and the restored database.
func Restore(ctx context.Context, opts RestoreOptions) (*Report, error) {
	if opts.TargetStateDir == "" {
		return nil, fmt.Errorf("restore requires a target state dir")
	}
	finalTarget, err := filepath.Abs(opts.TargetStateDir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(finalTarget); !os.IsNotExist(err) {
		return nil, fmt.Errorf("restore target must not exist; choose a new isolated directory")
	}
	if opts.StorageRoot != "" {
		root, err := filepath.Abs(opts.StorageRoot)
		if err != nil || root != filepath.Join(finalTarget, "content") {
			return nil, fmt.Errorf("restore storage root must be <state-dir>/content")
		}
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = 1 << 40
	}
	if opts.MaxFileBytes == 0 {
		opts.MaxFileBytes = 64 << 30
	}
	if opts.MaxEntries == 0 {
		opts.MaxEntries = 1000000
	}
	if opts.MaxBytes < 1 || opts.MaxFileBytes < 1 || opts.MaxEntries < 1 {
		return nil, fmt.Errorf("invalid restore limits")
	}
	// Exclusively reserve the destination, then keep extraction private until
	// verification. A failed run removes only the directory it created.
	if err := os.Mkdir(finalTarget, 0o700); err != nil {
		return nil, err
	}
	finished := false
	defer func() {
		if !finished {
			os.RemoveAll(finalTarget)
		}
	}()
	marker := filepath.Join(finalTarget, ".restore-in-progress")
	if err := os.WriteFile(marker, []byte("Restore incomplete; do not start this instance.\n"), 0o600); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(finalTarget, ".restore-staging-")
	if err != nil {
		return nil, err
	}
	targetDB := filepath.Join(stage, "database", "netsidian.db")
	storageRoot := filepath.Join(stage, "content")
	zr, err := zip.OpenReader(opts.Archive)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer zr.Close()

	if len(zr.File) > opts.MaxEntries+1 {
		return nil, fmt.Errorf("archive entry limit exceeded")
	}
	var manifest *Manifest
	fileEntries := []*zip.File{}
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			if manifest != nil {
				return nil, fmt.Errorf("duplicate manifest.json")
			}
			if f.UncompressedSize64 > 64<<20 {
				return nil, fmt.Errorf("manifest size limit exceeded")
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			var m Manifest
			if err := json.NewDecoder(io.LimitReader(rc, (64<<20)+1)).Decode(&m); err != nil {
				rc.Close()
				return nil, fmt.Errorf("manifest.json: %w", err)
			}
			rc.Close()
			manifest = &m
			continue
		}
		fileEntries = append(fileEntries, f)
	}
	if manifest == nil {
		return nil, fmt.Errorf("archive has no manifest.json")
	}
	if manifest.Format != Format {
		return nil, fmt.Errorf("unsupported archive format %q", manifest.Format)
	}
	manifestByPath := map[string]Entry{}
	var total int64
	for _, e := range manifest.Entries {
		if err := safeArchivePath(e.Path); err != nil {
			return nil, fmt.Errorf("unsafe path in manifest: %w", err)
		}
		if _, exists := manifestByPath[e.Path]; exists {
			return nil, fmt.Errorf("duplicate manifest entry %s", e.Path)
		}
		if e.Bytes < 0 || e.Bytes > opts.MaxFileBytes || e.Bytes > opts.MaxBytes-total {
			return nil, fmt.Errorf("archive decompression limit exceeded")
		}
		if e.Directory {
			if e.Bytes != 0 {
				return nil, fmt.Errorf("directory has payload")
			}
		} else if b, err := hex.DecodeString(e.SHA256); err != nil || len(b) != 32 {
			return nil, fmt.Errorf("invalid checksum")
		}
		total += e.Bytes
		manifestByPath[e.Path] = e
	}
	seen, destinations := map[string]bool{}, map[string]bool{}
	for _, f := range fileEntries {
		if err := safeArchivePath(f.Name); err != nil {
			return nil, fmt.Errorf("unsafe path: %w", err)
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("duplicate archive entry %s", f.Name)
		}
		seen[f.Name] = true
		e, ok := manifestByPath[f.Name]
		if !ok {
			return nil, fmt.Errorf("archive entry %s is not declared", f.Name)
		}
		if f.Mode()&fs.ModeSymlink != 0 || (!f.Mode().IsRegular() && !f.Mode().IsDir()) || f.Mode().IsDir() != e.Directory {
			return nil, fmt.Errorf("unsupported archive file type")
		}
		if f.UncompressedSize64 != uint64(e.Bytes) {
			return nil, fmt.Errorf("size mismatch: %s", f.Name)
		}
		dst, err := restoreTarget(f.Name, stage, storageRoot)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(dst)
		if destinations[key] {
			return nil, fmt.Errorf("colliding restore path %s", f.Name)
		}
		destinations[key] = true
	}
	if len(seen) != len(manifestByPath) {
		return nil, fmt.Errorf("manifest declares missing archive entries")
	}
	for _, required := range []string{"state/database/netsidian.db", "state/secrets/auth_secret"} {
		if e, ok := manifestByPath[required]; !ok || e.Directory {
			return nil, fmt.Errorf("required archive entry missing: %s", required)
		}
	}
	// Extract with integrity + path safety. Every payload entry must be
	// declared by the manifest; undeclared entries fail the restore.
	for _, f := range fileEntries {
		if err := safeArchivePath(f.Name); err != nil {
			return nil, fmt.Errorf("archive entry %q: %w", f.Name, err)
		}
		want, ok := manifestByPath[f.Name]
		if !ok {
			return nil, fmt.Errorf("archive entry %q is not declared in the manifest", f.Name)
		}
		dst, err := restoreTarget(f.Name, stage, storageRoot)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if want.Directory {
			if err := os.MkdirAll(dst, 0o700); err != nil {
				return nil, err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		if err := writeVerified(rc, dst, want); err != nil {
			rc.Close()
			return nil, fmt.Errorf("entry %q: %w", f.Name, err)
		}
		rc.Close()
	}

	// Instance key for sealed-secret verification comes from the restored
	// secrets, never from the operator's environment.
	secret, err := readHexSecret(filepath.Join(stage, "secrets", "auth_secret"))
	if err != nil {
		return nil, fmt.Errorf("restored instance key: %w", err)
	}

	db, err := sqlite.Open(targetDB, 5000)
	if err != nil {
		return nil, fmt.Errorf("open restored database: %w", err)
	}
	defer db.SQL.Close()

	report := &Report{}

	// Sealed provider profiles must decrypt with the restored key before any
	// binding rewrite; this proves the secrets survived the round trip.
	base, err := local.NewContent(storageRoot)
	if err != nil {
		return nil, err
	}
	mgr, err := providers.New(base, db, secret)
	if err != nil {
		return nil, fmt.Errorf("sealed verification setup: %w", err)
	}
	rows, err := db.SQL.QueryContext(ctx, `SELECT space_id FROM vault_storage ORDER BY space_id`)
	if err != nil {
		return nil, err
	}
	type sealedRow struct{ spaceID string }
	var sealed []sealedRow
	for rows.Next() {
		var s sealedRow
		if err := rows.Scan(&s.spaceID); err != nil {
			rows.Close()
			return nil, err
		}
		sealed = append(sealed, s)
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	for _, s := range sealed {
		if _, err := mgr.BackupProfile(ctx, s.spaceID); err != nil {
			return nil, fmt.Errorf("sealed storage profile for vault %s does not decrypt with the restored instance key: %w", s.spaceID, err)
		}
		report.SealedProfiles++
	}
	// OAuth refresh grants are sealed by the same instance key.
	grants, err := db.SQL.QueryContext(ctx, `SELECT user_id,sealed FROM oauth_storage_tokens`)
	if err != nil {
		return nil, err
	}
	for grants.Next() {
		var user string
		var blob []byte
		var value json.RawMessage
		if err := grants.Scan(&user, &blob); err != nil {
			grants.Close()
			return nil, err
		}
		if err := mgr.Open("oauth:"+user, blob, &value); err != nil {
			grants.Close()
			return nil, fmt.Errorf("sealed OAuth grant cannot be restored: %w", err)
		}
		report.SealedProfiles++
	}
	err = grants.Err()
	grants.Close()
	if err != nil {
		return nil, err
	}
	// Second-factor secrets are sealed with the same instance key; a restored
	// instance must be able to open them, otherwise the enrolled factor would
	// be silently unusable after a restore.
	totpRows, err := db.SQL.QueryContext(ctx, `SELECT user_id,sealed_secret FROM totp_enrollments ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	for totpRows.Next() {
		var userID string
		var blob []byte
		if err := totpRows.Scan(&userID, &blob); err != nil {
			totpRows.Close()
			return nil, err
		}
		var sec struct {
			Secret string `json:"secret"`
		}
		if err := mgr.Open("totp:"+userID, blob, &sec); err != nil || sec.Secret == "" {
			totpRows.Close()
			return nil, fmt.Errorf("sealed second-factor secret for user %s cannot be restored: %w", userID, err)
		}
		report.SealedProfiles++
	}
	if err := totpRows.Err(); err != nil {
		totpRows.Close()
		return nil, err
	}
	totpRows.Close()
	report.SealedVerified = true

	// Binding rewrite: attached folders land under the target instance and
	// remote vaults are restored as managed local content (the operator can
	// re-point them to the provider afterwards). Sealed profiles for
	// rewritten remote vaults are removed so the restored instance never
	// silently reaches for the old provider.
	var spaceCount int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM spaces`).Scan(&spaceCount); err != nil {
		return nil, err
	}
	if spaceCount != len(manifest.Vaults) {
		return nil, fmt.Errorf("vault inventory mismatch")
	}
	seenVaults := map[string]bool{}
	for _, v := range manifest.Vaults {
		if seenVaults[v.ID] {
			return nil, fmt.Errorf("duplicate vault inventory")
		}
		seenVaults[v.ID] = true
		var binding string
		if err := db.SQL.QueryRowContext(ctx, `SELECT storage_binding FROM spaces WHERE id=?`, v.ID).Scan(&binding); err != nil || binding != v.Binding {
			return nil, fmt.Errorf("vault inventory mismatch")
		}
		if err := safeArchivePath("content/spaces/" + v.ID); err != nil {
			return nil, err
		}
		if v.Source != "content" && v.Source != "attached" && v.Source != "remote" {
			return nil, fmt.Errorf("incomplete backup: vault %s", v.ID)
		}
		if (v.Binding == "local-main" && v.Source != "content") || (strings.HasPrefix(v.Binding, "local-folder:") && v.Source != "attached") || ((v.Binding == "s3-main" || v.Binding == "s3-vault") && v.Source != "remote") {
			return nil, fmt.Errorf("vault source does not match its binding")
		}

		switch {
		case strings.HasPrefix(v.Binding, "local-folder:"):
			newBinding := "local-folder:" + filepath.Join(finalTarget, "attached", v.ID)
			if _, err := db.SQL.ExecContext(ctx, `UPDATE spaces SET storage_binding=? WHERE id=?`, newBinding, v.ID); err != nil {
				return nil, err
			}
			report.BindingRewrites = append(report.BindingRewrites, fmt.Sprintf("%s: %s -> %s", v.ID, v.Binding, newBinding))
		case v.Binding == "s3-main" || v.Binding == "s3-vault":
			if _, err := db.SQL.ExecContext(ctx, `UPDATE spaces SET storage_binding='local-main' WHERE id=?`, v.ID); err != nil {
				return nil, err
			}
			if _, err := db.SQL.ExecContext(ctx, `DELETE FROM vault_storage WHERE space_id=?`, v.ID); err != nil {
				return nil, err
			}
			report.BindingRewrites = append(report.BindingRewrites, fmt.Sprintf("%s: %s -> local-main (restored as managed content)", v.ID, v.Binding))
		}
		if v.Source == "remote" || v.Source == "content" {
			if err := os.MkdirAll(filepath.Join(storageRoot, "spaces", v.ID), 0o700); err != nil {
				return nil, err
			}
		}
	}

	if err := verify(ctx, db, stage, storageRoot, report); err != nil {
		return nil, fmt.Errorf("restore verification failed: %w", err)
	}
	report.Attachments = attachmentCount(manifest.Entries)
	actual := Counts{Users: report.Accounts, Sessions: report.Sessions, APIKeys: report.APIKeys, Documents: report.Documents, Attachments: report.Attachments, Revisions: report.Revisions}
	if actual != manifest.Counts {
		return nil, fmt.Errorf("snapshot counts do not match manifest")
	}
	if err := relocateState(ctx, db, finalTarget); err != nil {
		return nil, err
	}
	if err := db.SQL.Close(); err != nil {
		return nil, err
	}
	// Publish the database last. The marker prevents startup after interruption.
	children, err := os.ReadDir(stage)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(children, func(i, j int) bool { return children[i].Name() != "database" && children[j].Name() == "database" })
	for _, child := range children {
		if err := os.Rename(filepath.Join(stage, child.Name()), filepath.Join(finalTarget, child.Name())); err != nil {
			return nil, err
		}
	}
	if err := os.Remove(stage); err != nil {
		return nil, err
	}
	if err := os.Remove(marker); err != nil {
		return nil, err
	}
	finished = true
	return report, nil
}

// safeArchivePath rejects absolute paths, traversal and odd separators.
func safeArchivePath(name string) error {
	if strings.Contains(name, ".restore-") || name == "" || strings.ContainsRune(name, 0) || strings.ContainsRune(name, '\\') {
		return fmt.Errorf("invalid path")
	}
	if strings.HasPrefix(name, "/") || filepath.IsAbs(name) || (len(name) > 1 && name[1] == ':') {
		return fmt.Errorf("absolute path")
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == ".." || seg == "." || strings.Contains(seg, ":") {
			return fmt.Errorf("unsafe path segment")
		}
	}
	switch {
	case name == "manifest.json", name == "state", name == "content", name == "attached", name == "remote",
		strings.HasPrefix(name, "state/"),
		strings.HasPrefix(name, "content/"),
		strings.HasPrefix(name, "attached/"),
		strings.HasPrefix(name, "remote/"):
		return nil
	default:
		return fmt.Errorf("entry outside the archive layout")
	}
}

func restoreTarget(archivePath, targetStateDir, storageRoot string) (string, error) {
	switch {
	case archivePath == "state":
		return targetStateDir, nil
	case archivePath == "content":
		return storageRoot, nil
	case archivePath == "attached":
		return filepath.Join(targetStateDir, "attached"), nil
	case archivePath == "remote":
		return filepath.Join(storageRoot, "spaces"), nil
	case strings.HasPrefix(archivePath, "state/"):
		return filepath.Join(targetStateDir, filepath.FromSlash(strings.TrimPrefix(archivePath, "state/"))), nil
	case strings.HasPrefix(archivePath, "content/"):
		return filepath.Join(storageRoot, filepath.FromSlash(strings.TrimPrefix(archivePath, "content/"))), nil
	case strings.HasPrefix(archivePath, "attached/"):
		return filepath.Join(targetStateDir, "attached", filepath.FromSlash(strings.TrimPrefix(archivePath, "attached/"))), nil
	case strings.HasPrefix(archivePath, "remote/"):
		// Remote vault bytes restore as managed local content.
		return filepath.Join(storageRoot, "spaces", filepath.FromSlash(strings.TrimPrefix(archivePath, "remote/"))), nil
	}
	return "", fmt.Errorf("entry %q has no restore target", archivePath)
}

func writeVerified(rc io.Reader, dst string, want Entry) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	tmp := dst + ".netsidian-restore.tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(rc, want.Bytes+1))
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if n != want.Bytes {
		os.Remove(tmp)
		return fmt.Errorf("size mismatch: archive %d bytes, manifest %d", n, want.Bytes)
	}
	if subtle.ConstantTimeCompare(h.Sum(nil), unhex(want.SHA256)) != 1 {
		os.Remove(tmp)
		return fmt.Errorf("sha256 mismatch")
	}
	return os.Rename(tmp, dst)
}

func readHexSecret(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 129))
	if len(b) > 128 {
		return nil, fmt.Errorf("invalid instance key size")
	}
	if err != nil {
		return nil, err
	}
	s, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, fmt.Errorf("secret is not hex")
	}
	if len(s) != 32 {
		return nil, fmt.Errorf("instance key must be 32 bytes")
	}
	return s, nil
}

func unhex(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

// verify proves the restored instance is coherent: accounts present, every
// live document's current revision resolves to bytes with the recorded
// sha256, every revision's history file exists with the recorded sha256,
// and the derived indexes cover exactly the live document set.
func verify(ctx context.Context, db *sqlite.DB, targetStateDir, storageRoot string, r *Report) error {
	foreignKeys, err := db.SQL.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	broken := foreignKeys.Next()
	err = foreignKeys.Err()
	foreignKeys.Close()
	if err != nil {
		return err
	}
	if broken {
		return fmt.Errorf("database foreign key integrity failed")
	}
	var pending int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM operations WHERE phase IN ('pending','storage_written')`).Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return fmt.Errorf("backup contains unreconciled operations")
	}
	counts := []struct {
		q    string
		dest *int
	}{
		{`SELECT count(*) FROM users`, &r.Accounts},
		{`SELECT count(*) FROM sessions WHERE revoked_at IS NULL`, &r.Sessions},
		{`SELECT count(*) FROM api_keys WHERE revoked_at IS NULL`, &r.APIKeys},
		{`SELECT count(*) FROM documents WHERE tombstone_at IS NULL`, &r.Documents},
		{`SELECT count(*) FROM documents WHERE tombstone_at IS NULL AND kind='attachment'`, &r.Attachments},
		{`SELECT count(*) FROM revisions`, &r.Revisions},
		{`SELECT count(*) FROM metadata_index`, &r.IndexMetadata},
		{`SELECT count(*) FROM search_index`, &r.IndexSearch},
	}
	for _, c := range counts {
		if err := db.SQL.QueryRowContext(ctx, c.q).Scan(c.dest); err != nil {
			return err
		}
	}
	if r.Accounts == 0 {
		return fmt.Errorf("restored instance has no accounts")
	}

	// Content: every live document's current revision bytes must exist at
	// the restored location for its (rewritten) binding, with matching hash.
	type docRow struct {
		spaceID, path, kind, binding, revID, sha string
	}
	rows, err := db.SQL.QueryContext(ctx, `
		SELECT d.space_id, d.path, d.kind, s.storage_binding, d.current_revision, r.sha256
		FROM documents d
		JOIN spaces s ON s.id = d.space_id
		JOIN revisions r ON r.id = d.current_revision
		WHERE d.tombstone_at IS NULL`)
	if err != nil {
		return err
	}
	var docs []docRow
	for rows.Next() {
		var d docRow
		if err := rows.Scan(&d.spaceID, &d.path, &d.kind, &d.binding, &d.revID, &d.sha); err != nil {
			rows.Close()
			return err
		}
		docs = append(docs, d)
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return rowErr
	}
	if len(docs) != r.Documents {
		return fmt.Errorf("%d live documents but %d have a resolvable current revision", r.Documents, len(docs))
	}
	for _, d := range docs {
		var root string
		switch {
		case strings.HasPrefix(d.binding, "local-folder:"):
			root = filepath.Join(targetStateDir, "attached", d.spaceID)
		default:
			root = filepath.Join(storageRoot, "spaces", d.spaceID)
		}
		if err := safeArchivePath("content/" + d.spaceID + "/" + d.path); err != nil {
			return err
		}
		full := filepath.Join(root, filepath.FromSlash(d.path))
		sum, err := fileHash(full)
		if err != nil {
			return fmt.Errorf("document %s (%s) content missing: %w", d.path, d.revID, err)
		}
		if subtle.ConstantTimeCompare(sum, unhex(d.sha)) != 1 {
			return fmt.Errorf("document %s content hash mismatch", d.path)
		}
		r.ContentVerified++
	}

	// History: historical revisions must have their immutable history files.
	hrows, err := db.SQL.QueryContext(ctx, `
		SELECT r.id, r.sha256, d.space_id, d.id, r.history_location
		FROM revisions r JOIN documents d ON d.id = r.document_id`)
	if err != nil {
		return err
	}
	type revRow struct{ revID, sha, spaceID, docID, location string }
	var revs []revRow
	for hrows.Next() {
		var v revRow
		if err := hrows.Scan(&v.revID, &v.sha, &v.spaceID, &v.docID, &v.location); err != nil {
			hrows.Close()
			return err
		}
		revs = append(revs, v)
	}
	rowErr = hrows.Err()
	hrows.Close()
	if rowErr != nil {
		return rowErr
	}
	historyRoot := filepath.Join(targetStateDir, "history")
	for _, v := range revs {
		// Saves keep the current revision in live content until the next save.
		// Only revisions with stored history must have a history file.
		if v.location == "" {
			currentVerified := false
			for _, d := range docs {
				if d.revID == v.revID && d.spaceID == v.spaceID {
					currentVerified = true
					break
				}
			}
			if currentVerified {
				continue
			}
			return fmt.Errorf("revision %s has neither live content nor history", v.revID)
		}

		if err := safeArchivePath("state/history/" + v.spaceID + "/" + v.docID + "/" + v.revID + ".md"); err != nil {
			return err
		}
		full := filepath.Join(historyRoot, v.spaceID, v.docID, v.revID+".md")
		sum, err := fileHash(full)
		if err != nil {
			return fmt.Errorf("revision %s history file missing: %w", v.revID, err)
		}
		if subtle.ConstantTimeCompare(sum, unhex(v.sha)) != 1 {
			return fmt.Errorf("revision %s history hash mismatch", v.revID)
		}
		r.HistoryVerified++
	}

	// Derived indexes must cover exactly the live document set. Search and
	// metadata must cover each live document at its current revision.
	// Full semantic reindex validation remains separate work.
	for _, table := range []string{"metadata_index", "search_index"} {
		var missing int
		q := `SELECT count(*) FROM documents d WHERE d.tombstone_at IS NULL AND NOT EXISTS(SELECT 1 FROM ` + table + ` i WHERE i.document_id=d.id AND i.space_id=d.space_id AND i.revision=d.current_revision)`
		if err := db.SQL.QueryRowContext(ctx, q).Scan(&missing); err != nil {
			return err
		}
		if missing > 0 {
			return fmt.Errorf("%s does not cover %d live document revisions", strings.ReplaceAll(table, "_", " "), missing)
		}
	}
	// Graph index integrity: every resolved link must point at a live
	// document in the restored instance.
	var dangling int
	if err := db.SQL.QueryRowContext(ctx, `
		SELECT count(*) FROM link_index li
		LEFT JOIN documents d ON d.id = li.resolved_document_id AND d.tombstone_at IS NULL
		WHERE li.resolved_document_id <> '' AND d.id IS NULL`).Scan(&dangling); err != nil {
		return err
	}
	if dangling > 0 {
		return fmt.Errorf("link index has %d resolved links pointing at missing documents", dangling)
	}
	return nil
}

// Persisted absolute history locations must refer to the new instance.
func relocateState(ctx context.Context, db *sqlite.DB, target string) error {
	rows, err := db.SQL.QueryContext(ctx, `SELECT r.id,d.space_id,d.id FROM revisions r JOIN documents d ON d.id=r.document_id WHERE r.history_location<>''`)
	if err != nil {
		return err
	}
	type rev struct{ id, space, doc string }
	var revisions []rev
	for rows.Next() {
		var r rev
		if err := rows.Scan(&r.id, &r.space, &r.doc); err != nil {
			rows.Close()
			return err
		}
		revisions = append(revisions, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range revisions {
		if _, err := db.SQL.ExecContext(ctx, `UPDATE revisions SET history_location=? WHERE id=?`, filepath.Join(target, "history", r.space, r.doc, r.id+".md"), r.id); err != nil {
			return err
		}
	}
	return nil
}

func fileHash(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

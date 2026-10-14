package bootstrap

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pentaqilo/netsidian/internal/backup"
	"github.com/pentaqilo/netsidian/internal/config"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/service"
	"github.com/pentaqilo/netsidian/internal/storage/local"
	"github.com/pentaqilo/netsidian/internal/storage/providers"
	"github.com/pentaqilo/netsidian/internal/storage/s3store"
)

// Operator CLI for quiesced backup and verified restore. The instance must
// not be running while either command executes.

func BackupCLI(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	archive := fs.String("archive", "", "path of the .zip archive to write (required)")
	stateDir := fs.String("state-dir", "", "instance state dir (default NETSIDIAN_STATE_DIR)")
	storageRoot := fs.String("storage-root", "", "managed content root (default NETSIDIAN_STORAGE_ROOT)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected backup arguments")
	}
	if *archive == "" {
		return fmt.Errorf("backup requires --archive <path.zip>")
	}
	cfg, err := operatorConfig(*stateDir, *storageRoot)
	if err != nil {
		return err
	}
	db, err := sqlite.OpenReadOnly(cfg.SQLitePath)
	if err != nil {
		return fmt.Errorf("open state database: %w", err)
	}
	defer db.SQL.Close()
	base, err := local.NewContent(cfg.StorageRoot)
	if err != nil {
		return err
	}
	mgr, err := providers.New(base, db, cfg.AuthSecret)
	if err != nil {
		return err
	}
	readLimit := cfg.MaxNoteBytes
	if cfg.MaxAttachBytes > readLimit {
		readLimit = cfg.MaxAttachBytes
	}
	opts := backup.Options{
		StateDir: cfg.StateDir, StorageRoot: cfg.StorageRoot, SQLitePath: cfg.SQLitePath,
		Version: service.Version, AuthMode: cfg.AuthMode,
		VaultConfig: func(ctx context.Context, spaceID, binding string) (*ports.StorageConfig, error) {
			switch binding {
			case "s3-vault":
				return mgr.BackupProfile(ctx, spaceID)
			case "s3-main":
				if cfg.StorageDriver != "s3" {
					return nil, nil
				}
				return &ports.StorageConfig{Provider: "s3", Bucket: cfg.S3Bucket, Region: cfg.S3Region,
					Endpoint: cfg.S3Endpoint, Prefix: cfg.S3Prefix, PathStyle: cfg.S3PathStyle}, nil
			}
			return nil, nil
		},
		RemoteSourceFor: func(ctx context.Context, v *ports.StorageConfig, spaceID string) (backup.RemoteSource, error) {
			if v.Auth == "" {
				// Instance bucket: same default credential chain the runtime uses.
				c, err := s3store.New(ctx, v.Bucket, v.Region, v.Endpoint, v.Prefix, v.PathStyle, nil)
				if err != nil {
					return nil, err
				}
				c.SetReadLimit(readLimit)
				return backup.S3StoreSource(c, spaceID), nil
			}
			return backup.S3RemoteSource(v, cfg.StorageEndpoints, cfg.STSEndpoints, nil, readLimit, spaceID)
		},
	}
	man, err := backup.Backup(context.Background(), opts, *archive)
	if err != nil {
		return err
	}
	fmt.Printf("backup written to %s: %d entries, %d vaults, %d documents, %d revisions\n",
		*archive, len(man.Entries), len(man.Vaults), man.Counts.Documents, man.Counts.Revisions)
	for _, w := range man.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	return nil
}

func RestoreCLI(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	archive := fs.String("archive", "", "archive to restore (required)")
	stateDir := fs.String("state-dir", "", "new nonexistent target state dir (required)")
	storageRoot := fs.String("storage-root", "", "target managed content root (default <state-dir>/content)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected restore arguments")
	}
	if *archive == "" || *stateDir == "" {
		return fmt.Errorf("restore requires --archive <path.zip> and --state-dir <new dir>")
	}
	report, err := backup.Restore(context.Background(), backup.RestoreOptions{
		Archive: *archive, TargetStateDir: *stateDir, StorageRoot: *storageRoot,
	})
	if err != nil {
		return err
	}
	fmt.Printf("restored into %s: %d accounts, %d sessions, %d api keys, %d documents (%d attachments), %d revisions\n",
		*stateDir, report.Accounts, report.Sessions, report.APIKeys, report.Documents, report.Attachments, report.Revisions)
	fmt.Printf("verified: %d document contents, %d history files, sealed profiles=%d verified=%v\n",
		report.ContentVerified, report.HistoryVerified, report.SealedProfiles, report.SealedVerified)
	for _, b := range report.BindingRewrites {
		fmt.Println("binding rewrite:", b)
	}
	for _, w := range report.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	return nil
}

// operatorConfig resolves config for the CLI with explicit overrides, and
// refuses to run against a state dir that has no instance database (so a
// typo never silently creates a fresh instance via secret creation).
func operatorConfig(stateDir, storageRoot string) (*config.Config, error) {
	if stateDir != "" {
		if err := os.Setenv("NETSIDIAN_STATE_DIR", stateDir); err != nil {
			return nil, err
		}
	}
	if storageRoot != "" {
		if err := os.Setenv("NETSIDIAN_STORAGE_ROOT", storageRoot); err != nil {
			return nil, err
		}
	}
	// Check before LoadFor can bootstrap secrets for a misspelled directory.
	root := os.Getenv("NETSIDIAN_STATE_DIR")
	if root == "" {
		base := os.Getenv("XDG_STATE_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			base = filepath.Join(home, ".local", "state")
		}
		root = filepath.Join(base, "netsidian")
	}
	database := os.Getenv("NETSIDIAN_SQLITE_PATH")
	if database == "" {
		database = filepath.Join(root, "database", "netsidian.db")
	}
	for _, p := range []string{database, filepath.Join(root, "secrets", "auth_secret")} {
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("existing instance state required at %s: %w", p, err)
		}
	}
	probe, err := config.LoadFor(runtimeKind)
	if err != nil {
		return nil, err
	}
	dbPath := probe.SQLitePath
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("no instance database at %s — backup/restore must target an existing quiesced instance", dbPath)
	}
	return probe, nil
}

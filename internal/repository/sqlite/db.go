package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB wraps the shared SQL connection pool. Single-writer SQLite: WAL +
// FULL synchronous + bounded busy timeout + serialized application
// mutations via per-document operation locks in the journal.
type DB struct {
	SQL *sql.DB
}

func Open(path string, busyTimeoutMS int) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)&_txlock=immediate",
		path, busyTimeoutMS)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// The pool has four connections. Immediate transactions take the write
	// lock before reading, reserving the writer up front; WAL readers on the
	// other connections remain concurrent.
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetConnMaxLifetime(time.Hour)
	// Verify the database is usable before serving traffic; never
	// silently start against an inaccessible existing database.
	if f, statErr := os.Stat(path); statErr == nil && f.Size() > 0 {
		probe := sqlDB.QueryRow(`PRAGMA integrity_check`)
		var ok string
		if err := probe.Scan(&ok); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("existing SQLite database unreadable: %w", err)
		}
		if ok != "ok" {
			sqlDB.Close()
			return nil, fmt.Errorf("existing SQLite database failed integrity check: %s", ok)
		}
	}
	if err := ensureFTS5(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}
	db := &DB{SQL: sqlDB}
	if err := db.migrate(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func ensureFTS5(sqlDB *sql.DB) error {
	// Virtual tables cannot be TEMP; they live in the temp schema instead.
	if _, err := sqlDB.Exec(`CREATE VIRTUAL TABLE temp._fts_probe USING fts5(x, tokenize='unicode61')`); err != nil {
		return fmt.Errorf("SQLite FTS5 unavailable in this build: %w", err)
	}
	sqlDB.Exec(`DROP TABLE temp._fts_probe`)
	return nil
}

func (db *DB) migrate() error {
	_, err := db.SQL.Exec(`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
	if err != nil {
		return err
	}
	var version int
	row := db.SQL.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`)
	if err := row.Scan(&version); err != nil {
		version = 0
	}
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name())
	}
	sort.Strings(names)
	for i, name := range names {
		if i < version {
			continue
		}
		content, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.SQL.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(string(content)) {
			if os.Getenv("NETSIDIAN_MIG_DEBUG") != "" {
				first := strings.SplitN(strings.TrimSpace(stmt), "\n", 2)[0]
				fmt.Printf("MIG> %.70s\n", first)
			}
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %s: %w", name, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO meta(key,value) VALUES('schema_version',?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprint(i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// splitStatements splits SQL on semicolons outside string literals and
// comments (migrations are static and reviewed).
func splitStatements(s string) []string {
	var out []string
	var cur strings.Builder
	inStr := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case inStr && ch == '\'':
			if i+1 < len(s) && s[i+1] == '\'' {
				cur.WriteByte('\'')
				i++
				continue
			}
			inStr = false
			cur.WriteByte(ch)
		case ch == '\'':
			inStr = true
			cur.WriteByte(ch)
		case ch == ';' && !inStr:
			if t := strings.TrimSpace(cur.String()); t != "" {
				out = append(out, t)
			}
			cur.Reset()
		default:
			if !inStr && ch == '-' && i+1 < len(s) && s[i+1] == '-' {
				// skip line comment
				for i < len(s) && s[i] != '\n' {
					i++
				}
				continue
			}
			cur.WriteByte(ch)
		}
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		out = append(out, t)
	}
	return out
}

func nowText() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanNullTime(s sql.NullString) *time.Time {
	if !s.Valid {
		return nil
	}
	t, err := parseTime(s.String)
	if err != nil {
		return nil
	}
	return &t
}

func textOf(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// Ping reports database reachability for the health endpoint.
func (db *DB) Ping(ctx context.Context) error {
	return db.SQL.PingContext(ctx)
}

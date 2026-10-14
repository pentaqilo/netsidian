package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

// A read-then-write transaction must reserve the writer before its first read.
// Otherwise a competing writer can invalidate its WAL snapshot and cause an
// immediate SQLITE_BUSY on upgrade despite the configured busy timeout.
func TestOpenReservesWriterBeforeReading(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "locking.db"), 25)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	ctx := context.Background()
	if _, err = db.SQL.ExecContext(ctx, `CREATE TABLE locking_probe(value INTEGER); INSERT INTO locking_probe VALUES(0)`); err != nil {
		t.Fatal(err)
	}
	first, err := db.SQL.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := db.SQL.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	tx, err := first.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// WAL readers remain usable while an immediate transaction holds the writer.
	var value int
	if err = second.QueryRowContext(ctx, `SELECT value FROM locking_probe`).Scan(&value); err != nil || value != 0 {
		t.Fatalf("concurrent reader: value=%d err=%v", value, err)
	}
	competing, err := second.BeginTx(ctx, nil)
	if err == nil {
		competing.Rollback()
		t.Fatal("second writer acquired a deferred transaction while first writer was reserved")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE locking_probe SET value=1`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	next, err := second.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("writer reservation not released after commit: %v", err)
	}
	defer next.Rollback()
	if err = next.QueryRowContext(ctx, `SELECT value FROM locking_probe`).Scan(&value); err != nil || value != 1 {
		t.Fatalf("next writer did not read committed state: value=%d err=%v", value, err)
	}
	if _, err = next.ExecContext(ctx, `UPDATE locking_probe SET value=2`); err != nil {
		t.Fatal(err)
	}
	if err = next.Commit(); err != nil {
		t.Fatal(err)
	}
}

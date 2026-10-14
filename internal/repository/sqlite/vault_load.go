package sqlite

import (
	"context"
	"database/sql"
	"github.com/pentaqilo/netsidian/internal/ports"
	"time"
)

func (db *DB) SaveVaultLoad(ctx context.Context, l *ports.VaultLoad) error {
	_, err := db.SQL.ExecContext(ctx, `INSERT INTO vault_loads(space_id,user_id,phase,completed,total,skipped,error,updated_at) VALUES(?,?,?,?,?,?,?,?)
 ON CONFLICT(space_id) DO UPDATE SET phase=excluded.phase,completed=excluded.completed,total=excluded.total,skipped=excluded.skipped,error=excluded.error,updated_at=excluded.updated_at`,
		l.SpaceID, l.UserID, l.Phase, l.Completed, l.Total, l.Skipped, l.Error, l.UpdatedAt.Format(time.RFC3339Nano))
	return err
}
func scanVaultLoad(row interface{ Scan(...any) error }) (*ports.VaultLoad, error) {
	var l ports.VaultLoad
	var updated string
	err := row.Scan(&l.SpaceID, &l.UserID, &l.Phase, &l.Completed, &l.Total, &l.Skipped, &l.Error, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	l.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &l, err
}
func (db *DB) VaultLoad(ctx context.Context, space string) (*ports.VaultLoad, error) {
	return scanVaultLoad(db.SQL.QueryRowContext(ctx, `SELECT space_id,user_id,phase,completed,total,skipped,error,updated_at FROM vault_loads WHERE space_id=?`, space))
}
func (db *DB) PendingVaultLoads(ctx context.Context) ([]*ports.VaultLoad, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT space_id,user_id,phase,completed,total,skipped,error,updated_at FROM vault_loads WHERE phase IN ('queued','scanning','indexing','resolving')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ports.VaultLoad
	for rows.Next() {
		l, err := scanVaultLoad(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

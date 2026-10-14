package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/pentaqilo/netsidian/internal/domain"
)

func (db *DB) VaultStorage(ctx context.Context, id string) ([]byte, error) {
	var b []byte
	var binding string
	err := db.SQL.QueryRowContext(ctx, `SELECT s.storage_binding,v.sealed FROM spaces s LEFT JOIN vault_storage v ON v.space_id=s.id WHERE s.id=?`, id).Scan(&binding, &b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err == nil && b == nil && domain.IsProviderStorageBinding(binding) {
		return nil, fmt.Errorf("vault storage profile is missing")
	}
	return b, err
}
func (db *DB) SaveVaultStorage(ctx context.Context, id string, b []byte, binding string) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO vault_storage(space_id,sealed) VALUES(?,?) ON CONFLICT(space_id) DO UPDATE SET sealed=excluded.sealed`, id, b); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE spaces SET storage_binding=? WHERE id=?`, binding, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (db *DB) OAuthStorageToken(ctx context.Context, id string) ([]byte, error) {
	var b []byte
	err := db.SQL.QueryRowContext(ctx, `SELECT sealed FROM oauth_storage_tokens WHERE user_id=?`, id).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}
func (db *DB) SaveOAuthStorageToken(ctx context.Context, id string, b []byte) error {
	_, err := db.SQL.ExecContext(ctx, `INSERT INTO oauth_storage_tokens(user_id,sealed) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET sealed=excluded.sealed`, id, b)
	return err
}

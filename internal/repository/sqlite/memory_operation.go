package sqlite

import (
	"context"
	"fmt"
)

func (db *DB) CommitMemoryOperation(ctx context.Context, spaceID, operationID, journal string) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE operations SET phase='committed',error=?,updated_at=? WHERE id=? AND space_id=? AND kind='memory_consolidate' AND phase='pending'`, journal, nowText(), operationID, spaceID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("memory operation is not pending in this vault")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox(space_id,event_type,document_id,revision_id,operation_id,created_at) VALUES(?,'memory.changed','','',?,?)`, spaceID, operationID, nowText())
	if err != nil {
		return err
	}
	return tx.Commit()
}

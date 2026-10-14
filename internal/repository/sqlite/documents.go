package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

const docCols = `id, space_id, path, norm_key, kind, current_revision, tombstone_at, purge_after, created_at, updated_at`

func scanDoc(row interface{ Scan(...any) error }) (*domain.Document, error) {
	var d domain.Document
	var tomb, purge sql.NullString
	var created, updated string
	if err := row.Scan(&d.ID, &d.SpaceID, &d.Path, &d.NormKey, &d.Kind, &d.CurrentRevision,
		&tomb, &purge, &created, &updated); err != nil {
		return nil, err
	}
	d.TombstoneAt = scanNullTime(tomb)
	d.PurgeAfter = scanNullTime(purge)
	d.CreatedAt, _ = parseTime(created)
	d.UpdatedAt, _ = parseTime(updated)
	return &d, nil
}

func (db *DB) InsertDocument(ctx context.Context, d *domain.Document, rev *domain.Revision) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertDocumentTx(ctx, tx, d, rev); err != nil {
		return err
	}
	return tx.Commit()
}

func insertDocumentTx(ctx context.Context, tx *sql.Tx, d *domain.Document, rev *domain.Revision) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO documents(id,space_id,path,norm_key,kind,current_revision,tombstone_at,purge_after,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.SpaceID, d.Path, d.NormKey, d.Kind, rev.ID,
		fmtTime(d.TombstoneAt), fmtTime(d.PurgeAfter),
		d.CreatedAt.UTC().Format(tRFC), d.UpdatedAt.UTC().Format(tRFC)); err != nil {
		return err
	}
	if err := insertRevisionTx(ctx, tx, rev); err != nil {
		return err
	}
	return nil
}

// tRFC stores timestamps with explicit offset for portability.
const tRFC = "2006-01-02T15:04:05.999999999Z"

func insertRevisionTx(ctx context.Context, tx *sql.Tx, r *domain.Revision) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO revisions(id,document_id,predecessor,sha256,byte_length,creator,operation_id,history_location,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.DocumentID, r.Predecessor, r.SHA256, r.ByteLength, r.Creator, r.OperationID, r.HistoryLocation,
		r.CreatedAt.UTC().Format(tRFC))
	return err
}

func (db *DB) InsertRevision(ctx context.Context, r *domain.Revision) error {
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO revisions(id,document_id,predecessor,sha256,byte_length,creator,operation_id,history_location,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.DocumentID, r.Predecessor, r.SHA256, r.ByteLength, r.Creator, r.OperationID, r.HistoryLocation,
		r.CreatedAt.UTC().Format(tRFC))
	return err
}

func (db *DB) Document(ctx context.Context, id string) (*domain.Document, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+docCols+` FROM documents WHERE id=?`, id)
	d, err := scanDoc(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func (db *DB) DocumentByNormKey(ctx context.Context, spaceID, normKey string) (*domain.Document, error) {
	row := db.SQL.QueryRowContext(ctx,
		`SELECT `+docCols+` FROM documents WHERE space_id=? AND norm_key=? AND tombstone_at IS NULL`, spaceID, normKey)
	d, err := scanDoc(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func (db *DB) DocumentsInSpace(ctx context.Context, spaceID string, includeTrashed bool) ([]*domain.Document, error) {
	q := `SELECT ` + docCols + ` FROM documents WHERE space_id=?`
	if !includeTrashed {
		q += ` AND tombstone_at IS NULL`
	}
	q += ` ORDER BY path`
	rows, err := db.SQL.QueryContext(ctx, q, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Document
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (db *DB) Revision(ctx context.Context, id string) (*domain.Revision, error) {
	row := db.SQL.QueryRowContext(ctx,
		`SELECT id, document_id, predecessor, sha256, byte_length, creator, operation_id, history_location, created_at
		 FROM revisions WHERE id=?`, id)
	r, err := scanRev(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func scanRev(row interface{ Scan(...any) error }) (*domain.Revision, error) {
	var r domain.Revision
	var created string
	if err := row.Scan(&r.ID, &r.DocumentID, &r.Predecessor, &r.SHA256, &r.ByteLength,
		&r.Creator, &r.OperationID, &r.HistoryLocation, &created); err != nil {
		return nil, err
	}
	r.CreatedAt, _ = parseTime(created)
	return &r, nil
}

func (db *DB) RevisionsForDocument(ctx context.Context, docID string) ([]*domain.Revision, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT id, document_id, predecessor, sha256, byte_length, creator, operation_id, history_location, created_at
		 FROM revisions WHERE document_id=? ORDER BY id DESC`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Revision
	for rows.Next() {
		r, err := scanRev(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) UpdateRevisionHistory(ctx context.Context, revID, location string) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE revisions SET history_location=? WHERE id=?`, location, revID)
	return err
}

func (db *DB) RevisionsForOperation(ctx context.Context, opID string) ([]*domain.Revision, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT id, document_id, predecessor, sha256, byte_length, creator, operation_id, history_location, created_at
		 FROM revisions WHERE operation_id=? ORDER BY id`, opID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Revision
	for rows.Next() {
		r, err := scanRev(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) UpdateDocumentCurrent(ctx context.Context, docID, revID string, updatedAt time.Time) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE documents SET current_revision=?, updated_at=? WHERE id=?`,
		revID, updatedAt.UTC().Format(tRFC), docID)
	return err
}

func (db *DB) SetDocumentPath(ctx context.Context, docID, path, normKey string, updatedAt time.Time) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE documents SET path=?, norm_key=?, updated_at=? WHERE id=?`,
		path, normKey, updatedAt.UTC().Format(tRFC), docID)
	return err
}

func (db *DB) SetDocumentTrash(ctx context.Context, docID string, tomb, purgeAfter *time.Time) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE documents SET tombstone_at=?, purge_after=?, updated_at=? WHERE id=?`,
		fmtTime(tomb), fmtTime(purgeAfter), nowText(), docID)
	return err
}

// ---------- Operation journal ----------

const opCols = `id, space_id, document_id, principal_id, kind, phase, expected_revision, old_revision, new_sha256, pending_path, target_path, origin_path, request_hash, idempotency_key, error, created_at, updated_at`

func scanOp(row interface{ Scan(...any) error }) (*domain.Operation, error) {
	var o domain.Operation
	var created, updated string
	if err := row.Scan(&o.ID, &o.SpaceID, &o.DocumentID, &o.Principal, &o.Kind, &o.Phase,
		&o.ExpectedRevision, &o.OldRevision, &o.NewSHA256, &o.PendingPath, &o.TargetPath, &o.OriginPath,
		&o.RequestHash, &o.IdempotencyKey, &o.Error, &created, &updated); err != nil {
		return nil, err
	}
	o.CreatedAt, _ = parseTime(created)
	o.UpdatedAt, _ = parseTime(updated)
	return &o, nil
}

func (db *DB) InsertOperation(ctx context.Context, op *domain.Operation) error {
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO operations(id,space_id,document_id,principal_id,kind,phase,expected_revision,old_revision,new_sha256,pending_path,target_path,origin_path,request_hash,idempotency_key,error,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		op.ID, op.SpaceID, op.DocumentID, op.Principal, string(op.Kind), string(op.Phase),
		op.ExpectedRevision, op.OldRevision, op.NewSHA256, op.PendingPath, op.TargetPath, op.OriginPath,
		op.RequestHash, op.IdempotencyKey, op.Error,
		op.CreatedAt.UTC().Format(tRFC), op.UpdatedAt.UTC().Format(tRFC))
	return err
}

func (db *DB) Operation(ctx context.Context, id string) (*domain.Operation, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+opCols+` FROM operations WHERE id=?`, id)
	o, err := scanOp(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return o, err
}

func (db *DB) SetOperationPhase(ctx context.Context, id string, phase domain.OperationPhase, errMsg string) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE operations SET phase=?, error=?, updated_at=? WHERE id=?`,
		string(phase), errMsg, nowText(), id)
	return err
}

func (db *DB) SetOperationDocument(ctx context.Context, id, docID string, at time.Time) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE operations SET document_id=?, updated_at=? WHERE id=?`,
		docID, at.UTC().Format(time.RFC3339Nano), id)
	return err
}

func (db *DB) FindOperationByIdempotency(ctx context.Context, spaceID, principal, key string) (*domain.Operation, error) {
	row := db.SQL.QueryRowContext(ctx,
		`SELECT `+opCols+` FROM operations WHERE space_id=? AND principal_id=? AND idempotency_key=?`,
		spaceID, principal, key)
	o, err := scanOp(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return o, err
}

// OpenOperations returns journaled operations that never reached a final
// phase; startup reconciliation replays or blocks them.
func (db *DB) OpenOperations(ctx context.Context) ([]*domain.Operation, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT `+opCols+` FROM operations WHERE phase IN ('pending','storage_written') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Operation
	for rows.Next() {
		o, err := scanOp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (db *DB) DeleteOperation(ctx context.Context, id string) error {
	_, err := db.SQL.ExecContext(ctx, `DELETE FROM operations WHERE id=?`, id)
	return err
}

// ---------- Outbox events ----------

func (db *DB) AppendEvent(ctx context.Context, e *domain.Event) (int64, error) {
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO outbox(space_id,event_type,document_id,revision_id,operation_id,created_at) VALUES(?,?,?,?,?,?)`,
		e.SpaceID, e.EventType, e.DocumentID, e.RevisionID, e.OperationID, nowText())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) MaxEventSeq(ctx context.Context, spaceID string) (int64, error) {
	var seq int64
	err := db.SQL.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM outbox WHERE space_id=?`, spaceID).Scan(&seq)
	return seq, err
}

func (db *DB) EventsAfter(ctx context.Context, spaceID string, seq int64, limit int) ([]*domain.Event, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT seq, space_id, event_type, document_id, revision_id, operation_id, created_at
		 FROM outbox WHERE space_id=? AND seq>? ORDER BY seq LIMIT ?`, spaceID, seq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Event
	for rows.Next() {
		var e domain.Event
		var created string
		if err := rows.Scan(&e.Seq, &e.SpaceID, &e.EventType, &e.DocumentID, &e.RevisionID, &e.OperationID, &created); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = parseTime(created)
		out = append(out, &e)
	}
	return out, rows.Err()
}

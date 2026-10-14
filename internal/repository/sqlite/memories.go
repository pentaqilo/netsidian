package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/pentaqilo/netsidian/internal/ports"
)

const memCols = `id, space_id, document_id, kind, title, status, approved_revision, expires_at, supersedes, created_by, reviewed_by, created_at, updated_at`

func scanMem(row interface{ Scan(...any) error }) (*ports.MemoryRecord, error) {
	var m ports.MemoryRecord
	var supersedes string
	var expires sql.NullString
	var created, updated string
	if err := row.Scan(&m.ID, &m.SpaceID, &m.DocumentID, &m.Kind, &m.Title, &m.Status,
		&m.ApprovedRevision, &expires, &supersedes, &m.CreatedBy, &m.ReviewedBy, &created, &updated); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(supersedes), &m.Supersedes)
	m.ExpiresAt = scanNullTime(expires)
	m.CreatedAt, _ = parseTime(created)
	m.UpdatedAt, _ = parseTime(updated)
	return &m, nil
}

// UpsertMemory stores or refreshes the trusted lifecycle record for a
// memory document. Content edits to an accepted record move status back
// to proposed (the caller computes that; the repository persists it).
func (db *DB) UpsertMemory(ctx context.Context, m *ports.MemoryRecord, sources []ports.MemorySource) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	supersedes, _ := json.Marshal(m.Supersedes)
	if supersedes == nil {
		supersedes = []byte("[]")
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE memories SET kind=?, title=?, status=?, approved_revision=?, expires_at=?, supersedes=?, reviewed_by=?, updated_at=? WHERE id=?`,
		m.Kind, m.Title, m.Status, m.ApprovedRevision, fmtTime(m.ExpiresAt), string(supersedes),
		m.ReviewedBy, m.UpdatedAt.UTC().Format(time.RFC3339Nano), m.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memories(id,space_id,document_id,kind,title,status,approved_revision,expires_at,supersedes,created_by,reviewed_by,created_at,updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.SpaceID, m.DocumentID, m.Kind, m.Title, m.Status, m.ApprovedRevision,
			fmtTime(m.ExpiresAt), string(supersedes), m.CreatedBy, m.ReviewedBy,
			m.CreatedAt.UTC().Format(time.RFC3339Nano), m.UpdatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_sources WHERE memory_id=?`, m.ID); err != nil {
		return err
	}
	for _, s := range sources {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memory_sources(id,memory_id,source_document_id,source_revision,locator) VALUES(?,?,?,?,?)`,
			s.ID, m.ID, s.SourceDocumentID, s.SourceRevision, s.Locator); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) Memory(ctx context.Context, id string) (*ports.MemoryRecord, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+memCols+` FROM memories WHERE id=?`, id)
	m, err := scanMem(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (db *DB) MemoryByDocument(ctx context.Context, docID string) (*ports.MemoryRecord, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+memCols+` FROM memories WHERE document_id=?`, docID)
	m, err := scanMem(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (db *DB) MemoriesInSpace(ctx context.Context, spaceID string, statuses []string) ([]*ports.MemoryRecord, error) {
	q := `SELECT ` + memCols + ` FROM memories WHERE space_id=? AND status<>'deleted'`
	args := []any{spaceID}
	if len(statuses) > 0 {
		q += ` AND status IN (`
		for i, s := range statuses {
			if i > 0 {
				q += ","
			}
			q += "?"
			args = append(args, s)
		}
		q += ")"
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ports.MemoryRecord
	for rows.Next() {
		m, err := scanMem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) MemorySources(ctx context.Context, memoryID string) ([]ports.MemorySource, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT id, memory_id, source_document_id, source_revision, locator FROM memory_sources WHERE memory_id=?`, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ports.MemorySource
	for rows.Next() {
		var s ports.MemorySource
		if err := rows.Scan(&s.ID, &s.MemoryID, &s.SourceDocumentID, &s.SourceRevision, &s.Locator); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (db *DB) UpdateMemoryStatus(ctx context.Context, id, status, approvedRevision, reviewedBy string, updatedAt time.Time) error {
	_, err := db.SQL.ExecContext(ctx,
		`UPDATE memories SET status=?, approved_revision=?, reviewed_by=?, updated_at=? WHERE id=?`,
		status, approvedRevision, reviewedBy, updatedAt.UTC().Format(time.RFC3339Nano), id)
	return err
}

// ---------- Audit ----------

func (db *DB) MemoryAudit(ctx context.Context, a *ports.AuditEvent) error {
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO audit_events(id,actor,action,space_id,resource,outcome,correlation,details,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Actor, a.Action, a.SpaceID, a.Resource, a.Outcome, a.Corr, a.Details,
		a.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// AuditForResource lists audit entries for one resource (memory id, etc.).
// Bodies are never stored, only actor/action/outcome metadata.
func (db *DB) AuditForResource(ctx context.Context, resource string) ([]*ports.AuditEvent, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT id, actor, action, space_id, resource, outcome, correlation, details, created_at
		 FROM audit_events WHERE resource=? ORDER BY created_at DESC LIMIT 100`, resource)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ports.AuditEvent
	for rows.Next() {
		var a ports.AuditEvent
		var created string
		if err := rows.Scan(&a.ID, &a.Actor, &a.Action, &a.SpaceID, &a.Resource, &a.Outcome, &a.Corr, &a.Details, &created); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = parseTime(created)
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (db *DB) AuditForMemory(ctx context.Context, memoryID string) ([]*ports.AuditEvent, error) {
	return db.AuditForResource(ctx, memoryID)
}

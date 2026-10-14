package sqlite

import (
	"context"
	"encoding/json"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"time"
)

func (db *DB) DocumentsNeedingIndex(ctx context.Context, space string) ([]*domain.Document, error) {
	rows, err := db.SQL.QueryContext(ctx, `WITH search_revisions AS MATERIALIZED (SELECT document_id,revision FROM search_index WHERE space_id=?)
	SELECT d.id,d.space_id,d.path,d.norm_key,d.kind,d.current_revision,d.tombstone_at,d.purge_after,d.created_at,d.updated_at
	FROM documents d LEFT JOIN metadata_index m ON m.document_id=d.id LEFT JOIN search_revisions s ON s.document_id=d.id
	WHERE d.space_id=? AND d.tombstone_at IS NULL AND (m.revision IS NULL OR m.revision<>d.current_revision OR s.revision IS NULL OR s.revision<>d.current_revision)`, space, space)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var docs []*domain.Document
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}
func (db *DB) DocumentMetadataPage(ctx context.Context, space string, trashed bool, cursor string, limit int) (*ports.DocumentPage, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	filter := `d.space_id=?`
	if !trashed {
		filter += ` AND d.tombstone_at IS NULL`
	}
	result := &ports.DocumentPage{Documents: []*ports.DocumentListEntry{}}
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM documents d WHERE `+filter, space).Scan(&result.Total); err != nil {
		return nil, err
	}
	rows, err := db.SQL.QueryContext(ctx, `SELECT d.id,d.path,d.kind,d.current_revision,d.updated_at,d.tombstone_at,COALESCE(m.title,''),COALESCE(m.tags,'[]')
 FROM documents d LEFT JOIN metadata_index m ON m.document_id=d.id WHERE `+filter+` AND d.path>? ORDER BY d.path LIMIT ?`, space, cursor, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d ports.DocumentListEntry
		var updated string
		var tomb *string
		var tags string
		if err := rows.Scan(&d.ID, &d.Path, &d.Kind, &d.CurrentRevision, &updated, &tomb, &d.Title, &tags); err != nil {
			return nil, err
		}
		d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		if tomb != nil {
			t, _ := time.Parse(time.RFC3339Nano, *tomb)
			d.TombstoneAt = &t
		}
		_ = json.Unmarshal([]byte(tags), &d.Tags)
		if d.Title == "" {
			d.Title = domain.BaseName(d.Path)
		}
		if len(result.Documents) == limit {
			result.NextCursor = result.Documents[limit-1].Path
			break
		}
		result.Documents = append(result.Documents, &d)
	}
	return result, rows.Err()
}

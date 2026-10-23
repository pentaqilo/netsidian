package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

const derivedSelect = `SELECT d.id,d.path,d.current_revision,m.revision,m.frontmatter
 FROM metadata_index m JOIN documents d ON d.id=m.document_id
 WHERE m.space_id=? AND d.space_id=? AND d.tombstone_at IS NULL`

func decodeDerived(scan interface{ Scan(...any) error }) (*ports.DerivedMetadata, error) {
	var out ports.DerivedMetadata
	var raw string
	if err := scan.Scan(&out.DocumentID, &out.Path, &out.Revision, &out.IndexedRevision, &raw); err != nil {
		return nil, err
	}
	if len(raw) > 65536 {
		return nil, domain.TooLarge("derived frontmatter exceeds 64 KiB; reduce metadata before status retrieval")
	}
	if err := json.Unmarshal([]byte(raw), &out.Frontmatter); err != nil {
		return nil, err
	}
	return &out, nil
}
func (db *DB) DerivedMetadataPage(ctx context.Context, space, cursor string, limit int) ([]ports.DerivedMetadata, bool, error) {
	if limit < 1 || limit > 50 {
		return nil, false, domain.Invalid("derived status limit must be 1..50")
	}
	rows, err := db.SQL.QueryContext(ctx, derivedSelect+` AND json_type(m.frontmatter,'$.netsidian.derived')='object' AND m.document_id>? ORDER BY m.document_id LIMIT ?`, space, space, cursor, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := []ports.DerivedMetadata{}
	for rows.Next() {
		if len(result) == limit {
			return result, true, nil
		}
		row, err := decodeDerived(rows)
		if err != nil {
			return nil, false, err
		}
		result = append(result, *row)
	}
	return result, false, rows.Err()
}
func (db *DB) DerivedMetadata(ctx context.Context, space, id string) (*ports.DerivedMetadata, error) {
	out, err := decodeDerived(db.SQL.QueryRowContext(ctx, derivedSelect+` AND d.id=?`, space, space, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return out, err
}

var _ ports.DerivedIndexReader = (*DB)(nil)

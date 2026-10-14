package sqlite

import (
	"context"
	"encoding/json"
	"github.com/pentaqilo/netsidian/internal/ports"
	"strings"
)

// Filter before pagination. A memo is an indexed, tagged document anywhere in
// the vault, never every note sharing a capture folder.
func (db *DB) ListMemos(ctx context.Context, q *ports.MemoQuery) (*ports.MemoResult, error) {
	where := []string{`d.space_id=?`, `d.tombstone_at IS NULL`, `d.kind IN ('note','memory')`, `EXISTS(SELECT 1 FROM document_tags t WHERE t.document_id=d.id AND t.tag='memo')`}
	args := []any{q.SpaceID}
	if q.Folder != nil {
		if *q.Folder == "" {
			where = append(where, `instr(d.path,'/')=0`)
		} else {
			prefix := *q.Folder + "/"
			where = append(where, `substr(d.path,1,?)=?`)
			args = append(args, len([]rune(prefix)), prefix)
		}
	}
	if q.Tag != "" {
		where = append(where, `EXISTS(SELECT 1 FROM document_tags t WHERE t.document_id=d.id AND (t.tag=? OR substr(t.tag,1,?)=?))`)
		args = append(args, q.Tag, len([]rune(q.Tag))+1, q.Tag+"/")
	}
	if q.Text != "" {
		where = append(where, `(instr(lower(d.path||' '||COALESCE(m.title,'')),lower(?))>0 OR EXISTS(SELECT 1 FROM search_index f WHERE f.document_id=d.id AND instr(lower(f.body),lower(?))>0))`)
		args = append(args, q.Text, q.Text)
	}
	if !q.From.IsZero() {
		where = append(where, `julianday(d.created_at)>=julianday(?)`)
		args = append(args, q.From.UTC().Format(tRFC))
	}
	if !q.Before.IsZero() {
		where = append(where, `julianday(d.created_at)<julianday(?)`)
		args = append(args, q.Before.UTC().Format(tRFC))
	}
	join := ` FROM documents d LEFT JOIN metadata_index m ON m.document_id=d.id WHERE `
	out := &ports.MemoResult{Memos: []ports.MemoEntry{}, Tags: []ports.TagCount{}}
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*)`+join+strings.Join(where, " AND "), args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	if !q.BeforeCreated.IsZero() {
		where = append(where, `(d.created_at<? OR (d.created_at=? AND d.id<?))`)
		args = append(args, q.BeforeCreated.UTC().Format(tRFC), q.BeforeCreated.UTC().Format(tRFC), q.BeforeID)
	}
	rows, err := db.SQL.QueryContext(ctx, `SELECT d.id,d.path,COALESCE(m.title,''),d.current_revision,d.created_at,d.updated_at,COALESCE(m.tags,'[]'),COALESCE((SELECT substr(f.body,1,240) FROM search_index f WHERE f.document_id=d.id),'')`+join+strings.Join(where, " AND ")+` ORDER BY d.created_at DESC,d.id DESC LIMIT ?`, append(args, q.Limit+1)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var entry ports.MemoEntry
		var tags, created, updated string
		if err := rows.Scan(&entry.ID, &entry.Path, &entry.Title, &entry.Revision, &created, &updated, &tags, &entry.Preview); err != nil {
			rows.Close()
			return nil, err
		}
		entry.CreatedAt, _ = parseTime(created)
		entry.UpdatedAt, _ = parseTime(updated)
		entry.Tags = []string{}
		_ = json.Unmarshal([]byte(tags), &entry.Tags)
		out.Memos = append(out.Memos, entry)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tags, err := db.SQL.QueryContext(ctx, `SELECT t.tag,count(*) FROM document_tags t JOIN documents d ON d.id=t.document_id WHERE d.space_id=? AND d.tombstone_at IS NULL AND t.tag<>'memo' AND EXISTS(SELECT 1 FROM document_tags marker WHERE marker.document_id=d.id AND marker.tag='memo') GROUP BY t.tag ORDER BY count(*) DESC,t.tag`, q.SpaceID)
	if err != nil {
		return nil, err
	}
	defer tags.Close()
	for tags.Next() {
		var tag ports.TagCount
		if err := tags.Scan(&tag.Tag, &tag.Count); err != nil {
			return nil, err
		}
		out.Tags = append(out.Tags, tag)
	}
	return out, tags.Err()
}

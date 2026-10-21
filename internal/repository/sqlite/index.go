package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

var stableDocumentIDPattern = regexp.MustCompile(`^doc_[a-z0-9]{26}$`)

// ReplaceDocumentIndex rewrites all derived rows for one document.
// Jobs call this with the generation they indexed; a stale job overwriting
// a newer generation is prevented by the revision guard.
func (db *DB) ReplaceDocumentIndex(ctx context.Context, spaceID string, doc *domain.Document, rev string, idx *ports.IndexData) error {
	return db.replaceDocumentIndex(ctx, spaceID, doc, rev, idx, false)
}

// Bulk import inserts unresolved links, then resolves the vault once after all
// documents exist. This avoids building a full path map for every imported file.
func (db *DB) ReplaceDocumentIndexDeferred(ctx context.Context, spaceID string, doc *domain.Document, rev string, idx *ports.IndexData) error {
	return db.replaceDocumentIndex(ctx, spaceID, doc, rev, idx, true)
}

func (db *DB) replaceDocumentIndex(ctx context.Context, spaceID string, doc *domain.Document, rev string, idx *ports.IndexData, deferred bool) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := replaceDocumentIndexTx(ctx, tx, spaceID, doc, rev, idx, deferred, false); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceDocumentIndexTx(ctx context.Context, tx *sql.Tx, spaceID string, doc *domain.Document, rev string, idx *ports.IndexData, deferred, fresh bool) error {

	// Guard: do not let a late job index an older generation.
	var current string
	err := tx.QueryRowContext(ctx, `SELECT current_revision FROM documents WHERE id=?`, doc.ID).Scan(&current)
	if err == sql.ErrNoRows {
		return nil // document deleted; drop stale work
	}
	if err != nil {
		return err
	}
	if current != rev {
		return nil // newer generation exists; ignore stale publish
	}

	if !fresh {
		if _, err := tx.ExecContext(ctx, `DELETE FROM link_index WHERE source_document_id=?`, doc.ID); err != nil {
			return err
		}
	}
	// Resolve link targets against the space's active documents.
	// Rule: explicit path first, then unique basename; several basename
	// candidates are ambiguous and must not be silently selected.
	type docRef struct{ id, key string }
	byKey := map[string]docRef{}
	byBase := map[string][]docRef{}
	byID := map[string]docRef{}
	if len(idx.Wikilinks) > 0 && !deferred {

		drows, err := tx.QueryContext(ctx,
			`SELECT id, norm_key FROM documents WHERE space_id=? AND tombstone_at IS NULL AND kind IN ('note','memory')`, spaceID)
		if err != nil {
			return err
		}
		for drows.Next() {
			var id, key string
			if err := drows.Scan(&id, &key); err != nil {
				drows.Close()
				return err
			}
			// norm_key is a composite "lower\x00fold" value; link targets
			// are plain lowercased paths, so compare against that part only.
			plain, _, _ := strings.Cut(key, "\x00")
			ref := docRef{id: id, key: plain}
			byKey[plain] = ref
			byID[id] = ref
			base := plain
			if i := strings.LastIndex(base, "/"); i >= 0 {
				base = base[i+1:]
			}
			byBase[strings.TrimSuffix(base, ".md")] = append(byBase[strings.TrimSuffix(base, ".md")], ref)
		}
		drows.Close()
		if err := drows.Err(); err != nil {
			return err
		}
	}

	for _, l := range idx.Wikilinks {
		kind := "basename"
		targetPath := ""
		targetBase := l.Target
		if i := strings.LastIndex(targetBase, "/"); i >= 0 {
			targetPath = l.Target
			kind = "path"
			targetBase = targetBase[i+1:]
		}
		targetBase = strings.TrimSuffix(strings.ToLower(targetBase), ".md")
		state := "unresolved"
		resolved := ""
		switch {
		case l.IsURL:
			kind, state = "url", "external"
		case stableDocumentIDPattern.MatchString(l.Target):
			kind, targetPath = "id", l.Target
			if ref, ok := byID[l.Target]; ok {
				state, resolved = "resolved", ref.id
			}
		default:
			key := targetBase + ".md"
			for _, key = range linkKeys(l.Target, doc.Path) {
				if ref, ok := byKey[key]; ok {
					state, resolved = "resolved", ref.id
					break
				}
			}
			if resolved == "" && !strings.HasPrefix(l.Target, "/") {
				refs := byBase[targetBase]
				switch len(refs) {
				case 1:
					state, resolved = "resolved", refs[0].id
				case 0:
					state = "unresolved"
				default:
					state = "ambiguous"
				}
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO link_index(id,space_id,source_document_id,source_revision,target_kind,target_path,target_basename,fragment,resolved_document_id,state)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`,
			domain.NewID("lnk"), spaceID, doc.ID, rev, kind, targetPath, targetBase, l.Fragment, resolved, state); err != nil {
			return err
		}
	}

	tagsJSON, _ := json.Marshal(idx.Tags)
	headJSON, _ := json.Marshal(idx.Headings)
	fmJSON, _ := json.Marshal(idx.Frontmatter)
	diagJSON, _ := json.Marshal(idx.Diagnostics)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO metadata_index(document_id,space_id,revision,title,tags,headings,frontmatter,tasks_done,tasks_total,diagnostics)
		 VALUES(?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(document_id) DO UPDATE SET revision=excluded.revision, title=excluded.title,
		   tags=excluded.tags, headings=excluded.headings, frontmatter=excluded.frontmatter,
		   tasks_done=excluded.tasks_done, tasks_total=excluded.tasks_total, diagnostics=excluded.diagnostics`,
		doc.ID, spaceID, rev, idx.Title, string(tagsJSON), string(headJSON), string(fmJSON),
		idx.TasksDone, idx.TasksTotal, string(diagJSON)); err != nil {
		return err
	}

	// Normalized tag rows (migration 0003): exact indexed tag filtering and
	// counting; the JSON column above stays authoritative for display.
	if !fresh {
		if _, err := tx.ExecContext(ctx, `DELETE FROM document_tags WHERE document_id=?`, doc.ID); err != nil {
			return err
		}
	}
	seenTag := map[string]bool{}
	for _, t := range idx.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seenTag[t] {
			continue
		}
		seenTag[t] = true
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO document_tags(document_id, space_id, tag) VALUES(?,?,?)`,
			doc.ID, spaceID, t); err != nil {
			return err
		}
	}

	if !fresh {
		if _, err := tx.ExecContext(ctx, `DELETE FROM search_index WHERE document_id=?`, doc.ID); err != nil {
			return err
		}
	}
	body := idx.BodyPlain
	if len(body) > 262144 {
		body = body[:262144]
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO search_index(document_id,space_id,revision,title,body) VALUES(?,?,?,?,?)`,
		doc.ID, spaceID, rev, idx.Title, body); err != nil {
		return err
	}
	return nil
}

func (db *DB) ImportDocumentBatch(ctx context.Context, space string, documents []ports.ImportedDocument) error {
	if len(documents) > 64 {
		return domain.Invalid("import batch exceeds 64 documents")
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range documents {
		if item.Revision != nil {
			if err := insertDocumentTx(ctx, tx, item.Document, item.Revision); err != nil {
				return err
			}
		}
		if err := replaceDocumentIndexTx(ctx, tx, space, item.Document, item.Document.CurrentRevision, item.Index, true, item.Revision != nil); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) RemoveDocumentIndex(ctx context.Context, docID string) error {
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM link_index WHERE source_document_id=?`, docID); err != nil {
		return err
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM document_tags WHERE document_id=?`, docID); err != nil {
		return err
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM metadata_index WHERE document_id=?`, docID); err != nil {
		return err
	}
	_, err := db.SQL.ExecContext(ctx, `DELETE FROM search_index WHERE document_id=?`, docID)
	return err
}

func (db *DB) Search(ctx context.Context, q *ports.SearchQuery) ([]*ports.SearchHit, error) {
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args := []any{q.SpaceID}
	where := `s.space_id = ?`
	text := strings.TrimSpace(q.Text)
	if text != "" {
		// Build an FTS5 OR-prefix query from sanitized words.
		words := ftsWords(text)
		if len(words) > 0 {
			where += ` AND search_index MATCH ?`
			args = append(args, strings.Join(words, " OR "))
		} else {
			where += " AND 0"
		}
	}
	var rows *sql.Rows
	var err error
	base := `SELECT s.document_id, d.path, COALESCE(m.title, ''),
		snippet(search_index, 4, '<mark>', '</mark>', '…', 18),
		bm25(search_index)`
	var having []string
	for _, tag := range q.Tags {
		// normalized tag rows: match the exact tag or a nested child ("tag" or "tag/...")
		t := strings.ToLower(tag)
		if q.TagMode == "exact" {
			having = append(having, `EXISTS(SELECT 1 FROM document_tags dt WHERE dt.document_id=d.id AND dt.tag=?)`)
			args = append(args, t)
		} else {
			having = append(having, `EXISTS(SELECT 1 FROM document_tags dt WHERE dt.document_id=d.id AND (dt.tag=? OR substr(dt.tag,1,?)=?))`)
			args = append(args, t, len([]rune(t))+1, t+"/")
		}
	}
	sqlq := base + ` FROM search_index s
		JOIN documents d ON d.id = s.document_id AND d.tombstone_at IS NULL
		JOIN metadata_index m ON m.document_id = d.id
		WHERE ` + where
	for i := range having {
		sqlq += ` AND ` + having[i]
		_ = i
	}
	if q.PathHas != "" {
		sqlq += ` AND lower(d.path) LIKE ?`
		args = append(args, "%"+strings.ToLower(q.PathHas)+"%")
	}
	if q.FolderPrefix != "" {
		sqlq += ` AND substr(d.path,1,?)=?`
		prefix := strings.TrimSuffix(q.FolderPrefix, "/") + "/"
		args = append(args, len([]rune(prefix)), prefix)
	}
	if !q.UpdatedAfter.IsZero() {
		sqlq += ` AND d.updated_at>?`
		args = append(args, q.UpdatedAfter.UTC().Format(time.RFC3339Nano))
	}
	if len(q.Kinds) > 0 {
		sqlq += ` AND d.kind IN (` + strings.TrimSuffix(strings.Repeat("?,", len(q.Kinds)), ",") + `)`
		for _, v := range q.Kinds {
			args = append(args, v)
		}
	}
	if len(q.ExcludeIDs) > 0 {
		sqlq += ` AND d.id NOT IN (` + strings.TrimSuffix(strings.Repeat("?,", len(q.ExcludeIDs)), ",") + `)`
		for _, v := range q.ExcludeIDs {
			args = append(args, v)
		}
	}
	sqlq += ` ORDER BY bm25(search_index), d.id LIMIT ? OFFSET ?`
	args = append(args, limit, q.Offset)
	rows, err = db.SQL.QueryContext(ctx, sqlq, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ports.SearchHit
	for rows.Next() {
		var h ports.SearchHit
		if err := rows.Scan(&h.DocumentID, &h.Path, &h.Title, &h.Snippet, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, &h)
	}
	return out, rows.Err()
}

// ftsWords extracts alphanumeric words for an FTS5 OR-prefix query.
func ftsWords(text string) []string {
	var words []string
	cur := strings.Builder{}
	for _, r := range text {
		if r == '_' || r == '-' {
			cur.WriteRune(r)
			continue
		}
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r >= 0x80 {
			cur.WriteRune(r)
			continue
		}
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	out := make([]string, 0, len(words))
	for _, w := range words {
		if len([]rune(w)) > 32 {
			w = string([]rune(w)[:32])
		}
		out = append(out, "\""+strings.ToLower(w)+"\"*")
	}
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

func (db *DB) Backlinks(ctx context.Context, spaceID, docID string) ([]*ports.Backlink, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT DISTINCT l.source_document_id, d.path, COALESCE(m.title, ''),
			COALESCE((SELECT body FROM search_index WHERE document_id = l.source_document_id), '')
		 FROM link_index l
		 JOIN documents d ON d.id = l.source_document_id AND d.tombstone_at IS NULL
		 LEFT JOIN metadata_index m ON m.document_id = d.id
		 WHERE l.space_id=? AND l.resolved_document_id=? AND l.source_document_id <> ?`,
		spaceID, docID, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ports.Backlink
	for rows.Next() {
		var b ports.Backlink
		var body string
		if err := rows.Scan(&b.DocumentID, &b.Path, &b.Title, &body); err != nil {
			return nil, err
		}
		b.Snippet = makeSnippet(body, 140)
		out = append(out, &b)
	}
	return out, rows.Err()
}

func makeSnippet(body string, n int) string {
	body = strings.Join(strings.Fields(strings.ReplaceAll(body, "\n", " ")), " ")
	runes := []rune(body)
	if len(runes) <= n {
		return body
	}
	return string(runes[:n]) + "…"
}

func (db *DB) Graph(ctx context.Context, spaceID string, limit int) (*ports.GraphData, error) {
	if limit <= 0 || limit > 2000 {
		limit = 600
	}
	g := &ports.GraphData{Nodes: []ports.GraphNode{}, Edges: []ports.GraphEdge{}}
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT d.id, d.path, COALESCE(m.title,''), COALESCE(m.tags,'[]'), d.kind
		 FROM documents d LEFT JOIN metadata_index m ON m.document_id=d.id
		 WHERE d.space_id=? AND d.tombstone_at IS NULL AND d.kind IN ('note','memory')
		 ORDER BY d.id LIMIT ?`, spaceID, limit+1)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for rows.Next() {
		var id, path, title, tagsRaw, kind string
		if err := rows.Scan(&id, &path, &title, &tagsRaw, &kind); err != nil {
			rows.Close()
			return nil, err
		}
		if len(g.Nodes) >= limit {
			g.Truncated = true
			break
		}
		var tags []string
		_ = json.Unmarshal([]byte(tagsRaw), &tags)
		g.Nodes = append(g.Nodes, ports.GraphNode{
			ID: id, Path: path, Title: title, Tags: tags, Memory: kind == "memory",
		})
		ids[id] = true
	}
	rows.Close()
	var total int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT count(*) FROM documents WHERE space_id=? AND tombstone_at IS NULL AND kind IN ('note','memory')`,
		spaceID).Scan(&total); err != nil {
		return nil, err
	}
	g.Total = total
	erows, err := db.SQL.QueryContext(ctx,
		`SELECT source_document_id, resolved_document_id FROM link_index
		 WHERE space_id=? AND state='resolved' AND resolved_document_id<>'' AND source_document_id<>resolved_document_id`, spaceID)
	if err != nil {
		return nil, err
	}
	defer erows.Close()
	for erows.Next() {
		var s, t string
		if err := erows.Scan(&s, &t); err != nil {
			return nil, err
		}
		if ids[s] && ids[t] {
			g.Edges = append(g.Edges, ports.GraphEdge{Source: s, Target: t})
		}
	}
	return g, nil
}

func (db *DB) LinksForSpace(ctx context.Context, spaceID string) ([]ports.LinkRow, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT source_document_id, source_revision, state, target_path, target_basename, fragment, resolved_document_id
		 FROM link_index WHERE space_id=? AND state != 'external'`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.LinkRow{}
	for rows.Next() {
		var r ports.LinkRow
		if err := rows.Scan(&r.SourceID, &r.SourceRevision, &r.State, &r.TargetPath, &r.TargetBasename, &r.Fragment, &r.ResolvedID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) DocsLite(ctx context.Context, spaceID string) ([]ports.DocLite, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT d.id, d.path, d.current_revision, COALESCE(m.title,''), COALESCE(m.tags,'[]'), d.kind
		 FROM documents d LEFT JOIN metadata_index m ON m.document_id=d.id
		 WHERE d.space_id=? AND d.tombstone_at IS NULL AND d.kind IN ('note','memory')
		 ORDER BY d.id`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.DocLite{}
	for rows.Next() {
		var d ports.DocLite
		var tagsRaw, kind string
		if err := rows.Scan(&d.ID, &d.Path, &d.Revision, &d.Title, &tagsRaw, &kind); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsRaw), &d.Tags)
		d.Memory = kind == "memory"
		out = append(out, d)
	}
	return out, rows.Err()
}

func (db *DB) TagsInSpace(ctx context.Context, spaceID string) ([]ports.TagCount, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT dt.tag, COUNT(*) FROM document_tags dt
		 JOIN documents d ON d.id = dt.document_id AND d.tombstone_at IS NULL
		 WHERE dt.space_id = ? GROUP BY dt.tag ORDER BY dt.tag`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ports.TagCount
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		out = append(out, ports.TagCount{Tag: t, Count: c})
	}
	return out, rows.Err()
}

// Adjacency performs bounded BFS over resolved links using the composite
// (space, source/target, state) indexes. Seeds and discovered endpoints are
// restricted to active documents; the edge count is hard-capped.
func (db *DB) Adjacency(ctx context.Context, spaceID string, seeds []string, depth, maxEdges int) ([]ports.AdjEdge, error) {
	if len(seeds) == 0 || depth <= 0 {
		return nil, nil
	}
	if depth > 2 {
		depth = 2
	}
	if maxEdges <= 0 || maxEdges > 500 {
		maxEdges = 200
	}
	active := func(id string) bool {
		var one string
		err := db.SQL.QueryRowContext(ctx,
			`SELECT id FROM documents WHERE id=? AND space_id=? AND tombstone_at IS NULL AND kind IN ('note','memory')`,
			id, spaceID).Scan(&one)
		return err == nil
	}
	seen := map[string]bool{}
	for _, s := range seeds {
		if !active(s) {
			continue
		}
		seen[s] = true
	}
	frontier := make([]string, 0, len(seen))
	for s := range seen {
		frontier = append(frontier, s)
	}
	var out []ports.AdjEdge
	dup := map[string]bool{}
	for hop := 0; hop < depth && len(frontier) > 0 && len(out) < maxEdges; hop++ {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(frontier)), ",")
		args := []any{spaceID}
		for _, f := range frontier {
			args = append(args, f)
		}
		rows, err := db.SQL.QueryContext(ctx,
			`SELECT source_document_id, resolved_document_id, source_revision, fragment, 'out'
			 FROM link_index WHERE space_id=? AND state='resolved' AND source_document_id IN (`+ph+`)
			 UNION ALL
			 SELECT source_document_id, resolved_document_id, source_revision, fragment, 'in'
			 FROM link_index WHERE space_id=? AND state='resolved' AND resolved_document_id IN (`+ph+`)`,
			append(args, args...)...)
		if err != nil {
			return nil, err
		}
		var next []string
		for rows.Next() && len(out) < maxEdges {
			var e ports.AdjEdge
			if err := rows.Scan(&e.SourceID, &e.TargetID, &e.SourceRevision, &e.Fragment, &e.Direction); err != nil {
				rows.Close()
				return nil, err
			}
			if e.SourceID == e.TargetID {
				continue
			}
			key := e.Direction + "\x00" + e.SourceID + "\x00" + e.TargetID
			if dup[key] {
				continue
			}
			dup[key] = true
			if !active(e.SourceID) || !active(e.TargetID) {
				continue
			}
			out = append(out, e)
			far := e.TargetID
			if e.Direction == "in" {
				far = e.SourceID
			}
			if !seen[far] {
				seen[far] = true
				next = append(next, far)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		frontier = next
	}
	return out, nil
}

func (db *DB) AllBasenames(ctx context.Context, spaceID string) (map[string]string, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT id, path FROM documents WHERE space_id=? AND tombstone_at IS NULL AND kind IN ('note','memory')`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	dup := map[string]bool{}
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		base := strings.ToLower(domain.BaseName(path))
		if prev, ok := out[base]; ok && prev != id {
			dup[base] = true
		}
		if !dup[base] {
			out[base] = id
		}
	}
	for b := range dup {
		out[b] = "" // ambiguous
	}
	return out, rows.Err()
}

func (db *DB) SpaceTitles(ctx context.Context, spaceID string) (map[string]string, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT m.document_id,m.title,d.path,CASE WHEN EXISTS(SELECT 1 FROM document_tags t WHERE t.document_id=m.document_id AND t.tag='memo') THEN COALESCE((SELECT substr(f.body,1,240) FROM search_index f WHERE f.document_id=m.document_id),'') ELSE '' END FROM metadata_index m JOIN documents d ON d.id=m.document_id WHERE m.space_id=?`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, title, docPath, body string
		if err := rows.Scan(&id, &title, &docPath, &body); err != nil {
			return nil, err
		}
		if body != "" && (title == "" || title == path.Base(docPath) || title == strings.TrimSuffix(path.Base(docPath), ".md")) {
			title = strings.TrimSpace(strings.SplitN(body, "\n", 2)[0])
			chars := []rune(title)
			if len(chars) > 80 {
				title = string(chars[:77]) + "…"
			}
		}
		out[id] = title
	}
	return out, rows.Err()
}

func (db *DB) TitleForDocument(ctx context.Context, docID string) (string, error) {
	var title string
	err := db.SQL.QueryRowContext(ctx, `SELECT title FROM metadata_index WHERE document_id=?`, docID).Scan(&title)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return title, err
}

// ResolveLinks re-evaluates unresolved/ambiguous/outbound rows against
// the current active documents of a space. It runs after creates,
// renames, trash and restores so wikilinks that become resolvable (or
// dangling) are corrected without re-parsing bodies.
func (db *DB) ResolveLinks(ctx context.Context, spaceID string) error {
	byBase := map[string][]string{}
	byKey := map[string]string{}
	drows, err := db.SQL.QueryContext(ctx,
		`SELECT id, norm_key FROM documents WHERE space_id=? AND tombstone_at IS NULL AND kind IN ('note','memory')`, spaceID)
	if err != nil {
		return err
	}
	active := map[string]bool{}
	for drows.Next() {
		var id, key string
		if err := drows.Scan(&id, &key); err != nil {
			drows.Close()
			return err
		}
		active[id] = true
		plain, _, _ := strings.Cut(key, "\x00")
		byKey[plain] = id
		base := plain
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		b := strings.TrimSuffix(base, ".md")
		byBase[b] = append(byBase[b], id)
	}
	drows.Close()
	if err := drows.Err(); err != nil {
		return err
	}

	type row struct{ id, kind, targetPath, base, resolved, sourcePath, state string }
	var rows []*row
	lrows, err := db.SQL.QueryContext(ctx,
		`SELECT l.id, l.target_kind, l.target_path, l.target_basename, l.resolved_document_id, d.path, l.state
 FROM link_index l JOIN documents d ON d.id=l.source_document_id WHERE l.space_id=? AND l.state != 'external'`, spaceID)
	if err != nil {
		return err
	}
	for lrows.Next() {
		var r row
		if err := lrows.Scan(&r.id, &r.kind, &r.targetPath, &r.base, &r.resolved, &r.sourcePath, &r.state); err != nil {
			lrows.Close()
			return err
		}
		rows = append(rows, &r)
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return err
	}

	var tx *sql.Tx
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	updates := 0
	for _, r := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		// already-resolved rows only need a liveness check
		if r.resolved != "" {
			if active[r.resolved] {
				continue
			}
		}
		state, resolved := "unresolved", ""
		target := r.targetPath
		if target == "" {
			target = r.base
		}
		// Stable-ID links (memory refs/details) resolve directly against
		// live documents and survive renames and moves untouched.
		if stableDocumentIDPattern.MatchString(target) {
			if active[target] {
				state, resolved = "resolved", target
			}
		} else {
			for _, key := range linkKeys(target, r.sourcePath) {
				if id, ok := byKey[key]; ok {
					state, resolved = "resolved", id
					break
				}
			}
			if resolved == "" && !strings.HasPrefix(target, "/") {
				switch refs := byBase[r.base]; len(refs) {
				case 1:
					state, resolved = "resolved", refs[0]
				case 0:
					state = "unresolved"
				default:
					state = "ambiguous"
				}
			}
		}
		if r.state == state && r.resolved == resolved {
			continue
		}
		if tx == nil {
			tx, err = db.SQL.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE link_index SET state=?, resolved_document_id=? WHERE id=?`, state, resolved, r.id); err != nil {
			return err
		}
		updates++
		// Bound write transactions so other vaults get a turn while resolving.
		if updates%250 == 0 {
			if err := tx.Commit(); err != nil {
				return err
			}
			tx = nil
		}
	}
	if tx != nil {
		return tx.Commit()
	}
	return nil
}

// Explicit / paths are anchored at the vault root; other links prefer their source folder.
func linkKeys(target, source string) []string {
	target = strings.ToLower(target)
	if !strings.HasSuffix(target, ".md") {
		target += ".md"
	}
	root := path.Clean(strings.TrimPrefix(target, "/"))
	if strings.HasPrefix(target, "/") {
		return []string{root}
	}
	return []string{path.Clean(path.Join(path.Dir(strings.ToLower(source)), target)), root}
}

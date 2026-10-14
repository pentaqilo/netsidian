-- Agent surface (agent-memory PLAN.md, phase B): normalized tag index and
-- retrieval-support composite indexes. All derived; rebuildable via ReindexAll.

CREATE TABLE document_tags (
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  space_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  PRIMARY KEY (document_id, tag)
);
CREATE INDEX ix_dtags_lookup ON document_tags(space_id, tag, document_id);

-- Backfill from the JSON tags arrays already in metadata_index.
INSERT OR IGNORE INTO document_tags(document_id, space_id, tag)
  SELECT m.document_id, m.space_id, lower(trim(j.value))
  FROM metadata_index m, json_each(m.tags) j;

-- Bounded adjacency scans (agent context + note/selection graph scopes).
CREATE INDEX ix_links_space_source ON link_index(space_id, source_document_id, state);
CREATE INDEX ix_links_space_resolved ON link_index(space_id, resolved_document_id, state);

-- Change-cursor scans ordered by recency within one vault.
CREATE INDEX ix_doc_space_updated ON documents(space_id, updated_at);

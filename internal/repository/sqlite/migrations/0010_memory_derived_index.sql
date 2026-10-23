-- Rebuildable access path over frontmatter already projected by normal document
-- indexing/imports. This contains no second authoritative memory facts.
CREATE INDEX ix_memory_derived_space_document
ON metadata_index(space_id, document_id)
WHERE json_type(frontmatter, '$.netsidian.derived') = 'object';

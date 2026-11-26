-- Keep existing search results available until startup repair re-parses the
-- Markdown. Only the derived index stamp changes, not the document revision.
UPDATE metadata_index SET revision = '';

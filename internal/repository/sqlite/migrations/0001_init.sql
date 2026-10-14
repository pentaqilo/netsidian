-- Netsidian SQLite schema v1

CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  email TEXT UNIQUE,
  password_hash TEXT NOT NULL DEFAULT '',
  disabled INTEGER NOT NULL DEFAULT 0,
  is_operator INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE TABLE identities (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  issuer TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL,
  UNIQUE (provider, issuer, subject)
);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX ix_sessions_user ON sessions(user_id);

CREATE TABLE api_keys (
  id TEXT PRIMARY KEY,
  label TEXT NOT NULL,
  principal_id TEXT NOT NULL REFERENCES users(id),
  secret_hash TEXT NOT NULL UNIQUE,
  scopes TEXT NOT NULL,           -- JSON array
  space_ids TEXT NOT NULL,        -- JSON array
  expires_at TEXT,
  last_used_at TEXT,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE spaces (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'shared',
  storage_binding TEXT NOT NULL DEFAULT 'local-main',
  archived_at TEXT,
  permission_version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);

CREATE TABLE memberships (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  UNIQUE (space_id, user_id)
);
CREATE INDEX ix_memberships_user ON memberships(user_id);

CREATE TABLE invitations (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  email TEXT NOT NULL,
  role TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  issued_by TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  consumed_at TEXT,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE documents (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  norm_key TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'note',
  current_revision TEXT NOT NULL DEFAULT '',
  tombstone_at TEXT,
  purge_after TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX ux_doc_path ON documents(space_id, norm_key) WHERE tombstone_at IS NULL;
CREATE INDEX ix_doc_space ON documents(space_id);

CREATE TABLE revisions (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  predecessor TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL,
  byte_length INTEGER NOT NULL,
  creator TEXT NOT NULL,
  operation_id TEXT NOT NULL DEFAULT '',
  history_location TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX ix_rev_doc ON revisions(document_id);

CREATE TABLE operations (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL,
  document_id TEXT NOT NULL,
  principal_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  phase TEXT NOT NULL,
  expected_revision TEXT NOT NULL DEFAULT '',
  old_revision TEXT NOT NULL DEFAULT '',
  new_sha256 TEXT NOT NULL DEFAULT '',
  pending_path TEXT NOT NULL DEFAULT '',
  target_path TEXT NOT NULL DEFAULT '',
  request_hash TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX ix_ops_open ON operations(phase) WHERE phase IN ('pending','storage_written');
CREATE UNIQUE INDEX ux_ops_idem ON operations(space_id, principal_id, idempotency_key)
  WHERE idempotency_key <> '';

CREATE TABLE outbox (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  space_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  document_id TEXT NOT NULL DEFAULT '',
  revision_id TEXT NOT NULL DEFAULT '',
  operation_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX ix_outbox_space ON outbox(space_id, seq);

CREATE TABLE jobs (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  space_id TEXT NOT NULL,
  document_id TEXT NOT NULL DEFAULT '',
  generation INTEGER NOT NULL DEFAULT 1,
  state TEXT NOT NULL DEFAULT 'pending',
  attempt INTEGER NOT NULL DEFAULT 0,
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_expires_at TEXT,
  retry_at TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX ix_jobs_claim ON jobs(state, retry_at);

CREATE TABLE link_index (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL,
  source_document_id TEXT NOT NULL,
  source_revision TEXT NOT NULL,
  target_kind TEXT NOT NULL,      -- path | basename | url
  target_path TEXT NOT NULL DEFAULT '',
  target_basename TEXT NOT NULL DEFAULT '',
  fragment TEXT NOT NULL DEFAULT '',
  resolved_document_id TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL             -- resolved | unresolved | ambiguous | external
);
CREATE INDEX ix_links_target ON link_index(space_id, resolved_document_id);
CREATE INDEX ix_links_source ON link_index(source_document_id);

CREATE TABLE metadata_index (
  document_id TEXT PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
  space_id TEXT NOT NULL,
  revision TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  tags TEXT NOT NULL DEFAULT '[]',        -- JSON array of lowercase tags
  headings TEXT NOT NULL DEFAULT '[]',    -- JSON
  frontmatter TEXT NOT NULL DEFAULT '{}', -- JSON
  tasks_done INTEGER NOT NULL DEFAULT 0,
  tasks_total INTEGER NOT NULL DEFAULT 0,
  diagnostics TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX ix_meta_space ON metadata_index(space_id);

CREATE VIRTUAL TABLE search_index USING fts5(
  document_id UNINDEXED,
  space_id UNINDEXED,
  revision UNINDEXED,
  title,
  body,
  tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TABLE memories (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  kind TEXT NOT NULL DEFAULT 'fact',
  title TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'proposed',
  approved_revision TEXT NOT NULL DEFAULT '',
  expires_at TEXT,
  supersedes TEXT NOT NULL DEFAULT '[]',  -- JSON array
  created_by TEXT NOT NULL,
  reviewed_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX ix_mem_space ON memories(space_id, status);
CREATE UNIQUE INDEX ux_mem_doc ON memories(document_id);

CREATE TABLE memory_sources (
  id TEXT PRIMARY KEY,
  memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  source_document_id TEXT NOT NULL,
  source_revision TEXT NOT NULL DEFAULT '',
  locator TEXT NOT NULL DEFAULT ''
);

CREATE TABLE audit_events (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  space_id TEXT NOT NULL DEFAULT '',
  resource TEXT NOT NULL DEFAULT '',
  outcome TEXT NOT NULL DEFAULT '',
  correlation TEXT NOT NULL DEFAULT '',
  details TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX ix_audit_res ON audit_events(resource);

CREATE TABLE user_preferences (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  prefs TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL
);

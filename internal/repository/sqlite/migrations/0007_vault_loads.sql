CREATE TABLE vault_loads (
 space_id TEXT PRIMARY KEY REFERENCES spaces(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL,
 phase TEXT NOT NULL,
 completed INTEGER NOT NULL DEFAULT 0,
 total INTEGER NOT NULL DEFAULT 0,
 skipped INTEGER NOT NULL DEFAULT 0,
 error TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL
);
CREATE INDEX documents_tree_page ON documents(space_id,path);

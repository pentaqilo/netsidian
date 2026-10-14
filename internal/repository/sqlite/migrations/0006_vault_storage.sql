CREATE TABLE vault_storage (
 space_id TEXT PRIMARY KEY REFERENCES spaces(id) ON DELETE CASCADE,
 sealed BLOB NOT NULL
);
CREATE TABLE oauth_storage_tokens (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 sealed BLOB NOT NULL
);

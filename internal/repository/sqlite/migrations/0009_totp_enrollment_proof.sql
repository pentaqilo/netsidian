-- Bind pending enrollment to the password actually verified at its creation.
-- Existing confirmed factors remain valid; old pending enrollment needs restart.
ALTER TABLE totp_enrollments ADD COLUMN expected_hash TEXT NOT NULL DEFAULT '';

-- Account-wide budget survives new challenges, restarts and IP changes.
CREATE TABLE totp_attempt_budgets (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 window_started_at INTEGER NOT NULL,
 attempts INTEGER NOT NULL DEFAULT 0
);

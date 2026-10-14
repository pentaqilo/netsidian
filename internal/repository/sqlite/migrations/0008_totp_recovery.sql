-- Phase A account security: optional confirmed TOTP second factor, hashed
-- single-use recovery codes, TOTP time-step replay prevention, and expiring
-- pending-login challenges. All secrets are sealed at rest; only hashes and
-- sealed blobs are stored here.
CREATE TABLE totp_enrollments (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  sealed_secret BLOB NOT NULL,
  confirmed INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE totp_recovery_codes (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash TEXT NOT NULL,
  used_at TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX ix_recovery_user ON totp_recovery_codes(user_id, used_at);

-- One row per spent TOTP time step. The primary key is the atomic claim that
-- makes a replayed code fail.
CREATE TABLE totp_used_steps (
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  step INTEGER NOT NULL,
  used_at TEXT NOT NULL,
  PRIMARY KEY (user_id, step)
);

-- Pending second-factor challenges. token_hash is the only lookup key the
-- client ever sees; status transitions are compare-and-swap so a challenge
-- can be consumed exactly once.
CREATE TABLE login_challenges (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expected_hash TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX ix_challenges_user ON login_challenges(user_id, status);

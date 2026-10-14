ALTER TABLE external_identities ADD COLUMN verified_claims TEXT NOT NULL DEFAULT '{}';
ALTER TABLE external_identities ADD COLUMN claims_expires_at TEXT NOT NULL DEFAULT '';

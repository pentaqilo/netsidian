package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func factorAudit(ctx context.Context, tx *sql.Tx, actor, target, action, corr string) error {
	details, _ := json.Marshal(map[string]any{"targetUserId": target})
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_events(id,actor,action,resource,outcome,correlation,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, domain.NewID("aud"), actor, action, target, "success", corr, string(details), nowText())
	return err
}

func factorCredentials(ctx context.Context, tx *sql.Tx, user, expectedHash string) error {
	var hash string
	var disabled bool
	if err := tx.QueryRowContext(ctx, `SELECT password_hash,disabled FROM users WHERE id=?`, user).Scan(&hash, &disabled); err != nil {
		return err
	}
	if disabled || hash == "" || hash != expectedHash {
		return domain.Unauthenticated("account credentials changed; sign in again")
	}
	return nil
}

func factorEnrollment(ctx context.Context, tx *sql.Tx, user string, proof ports.FactorProof, confirmed bool) error {
	var sealed []byte
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT sealed_secret,confirmed FROM totp_enrollments WHERE user_id=?`, user).Scan(&sealed, &enabled); err != nil {
		return domain.Unauthenticated("second factor unavailable")
	}
	if enabled != confirmed || !bytes.Equal(sealed, proof.SealedSecret) {
		return domain.Conflict("second factor changed; retry authentication")
	}
	return nil
}

func spendFactor(ctx context.Context, tx *sql.Tx, user string, proof ports.FactorProof) error {
	if proof.RecoveryHash != "" {
		r, err := tx.ExecContext(ctx, `UPDATE totp_recovery_codes SET used_at=? WHERE user_id=? AND code_hash=? AND used_at IS NULL`, nowText(), user, proof.RecoveryHash)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return domain.Unauthenticated("invalid or used recovery code")
		}
		return nil
	}
	now := time.Now().Unix() / 30
	if proof.Step < now-1 || proof.Step > now+1 {
		return domain.Unauthenticated("authenticator code expired")
	}
	var last sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT max(step) FROM totp_used_steps WHERE user_id=?`, user).Scan(&last); err != nil {
		return err
	}
	if last.Valid && proof.Step <= last.Int64 {
		return domain.Unauthenticated("code already used; wait for a new code")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO totp_used_steps(user_id,step,used_at) VALUES(?,?,?)`, user, proof.Step, nowText()); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM totp_used_steps WHERE user_id=? AND step<?`, user, proof.Step)
	return err
}

func revokeFactorCredentials(ctx context.Context, tx *sql.Tx, user string) error {
	for _, query := range []string{`UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, `UPDATE api_keys SET revoked_at=? WHERE principal_id=? AND revoked_at IS NULL`, `UPDATE login_challenges SET status='expired' WHERE user_id=? AND status='pending'`} {
		var err error
		if query == `UPDATE login_challenges SET status='expired' WHERE user_id=? AND status='pending'` {
			_, err = tx.ExecContext(ctx, query, user)
		} else {
			_, err = tx.ExecContext(ctx, query, nowText(), user)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func writeRecoveryHashes(ctx context.Context, tx *sql.Tx, user string, hashes []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM totp_recovery_codes WHERE user_id=?`, user); err != nil {
		return err
	}
	for _, hash := range hashes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO totp_recovery_codes(id,user_id,code_hash,created_at) VALUES(?,?,?,?)`, domain.NewID("rc"), user, hash, nowText()); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) StartTOTPBound(ctx context.Context, user, expectedHash string, sealed []byte, hashes []string, corr string) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := factorCredentials(ctx, tx, user, expectedHash); err != nil {
		return err
	}
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT confirmed FROM totp_enrollments WHERE user_id=?`, user).Scan(&active)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if active {
		return domain.Conflict("second factor already enrolled")
	}
	now := nowText()
	if _, err := tx.ExecContext(ctx, `INSERT INTO totp_enrollments(user_id,sealed_secret,confirmed,created_at,updated_at,expected_hash) VALUES(?,?,0,?,?,?) ON CONFLICT(user_id) DO UPDATE SET sealed_secret=excluded.sealed_secret,confirmed=0,created_at=excluded.created_at,updated_at=excluded.updated_at,expected_hash=excluded.expected_hash`, user, sealed, now, now, expectedHash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM totp_used_steps WHERE user_id=?`, user); err != nil {
		return err
	}
	if err := writeRecoveryHashes(ctx, tx, user, hashes); err != nil {
		return err
	}
	if err := factorAudit(ctx, tx, user, user, "totp.enroll", corr); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) ConfirmTOTPBound(ctx context.Context, user string, proof ports.FactorProof, corr string) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := factorEnrollment(ctx, tx, user, proof, false); err != nil {
		return err
	}
	var hash, created string
	if err := tx.QueryRowContext(ctx, `SELECT expected_hash,created_at FROM totp_enrollments WHERE user_id=?`, user).Scan(&hash, &created); err != nil {
		return err
	}
	started, err := parseTime(created)
	if err != nil {
		return err
	}
	if time.Since(started) > 10*time.Minute {
		return domain.Unauthenticated("enrollment expired; start again")
	}
	if err := factorCredentials(ctx, tx, user, hash); err != nil {
		return err
	}
	if err := spendFactor(ctx, tx, user, proof); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE totp_enrollments SET confirmed=1,updated_at=? WHERE user_id=?`, nowText(), user); err != nil {
		return err
	}
	if err := revokeFactorCredentials(ctx, tx, user); err != nil {
		return err
	}
	if err := factorAudit(ctx, tx, user, user, "totp.confirm", corr); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) ChangeTOTPBound(ctx context.Context, user, hash string, proof ports.FactorProof, action string, hashes []string, corr string) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := factorCredentials(ctx, tx, user, hash); err != nil {
		return err
	}
	if err := factorEnrollment(ctx, tx, user, proof, true); err != nil {
		return err
	}
	if err := spendFactor(ctx, tx, user, proof); err != nil {
		return err
	}
	switch action {
	case "totp.disable":
		if err := removeFactor(ctx, tx, user); err != nil {
			return err
		}
	case "totp.recovery":
		if err := writeRecoveryHashes(ctx, tx, user, hashes); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE login_challenges SET status='expired' WHERE user_id=? AND status='pending'`, user); err != nil {
			return err
		}
	default:
		return domain.Invalid("invalid second-factor operation")
	}
	if err := factorAudit(ctx, tx, user, user, action, corr); err != nil {
		return err
	}
	return tx.Commit()
}

func removeFactor(ctx context.Context, tx *sql.Tx, user string) error {
	for _, table := range []string{"totp_enrollments", "totp_recovery_codes", "totp_used_steps"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE user_id=?`, user); err != nil {
			return err
		}
	}
	return revokeFactorCredentials(ctx, tx, user)
}

func (db *DB) CompleteChallengeBound(ctx context.Context, ch *ports.LoginChallenge, proof ports.FactorProof, session, token string, expiry time.Time) (bool, error) {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := factorCredentials(ctx, tx, ch.UserID, ch.ExpectedHash); err != nil {
		return false, err
	}
	if err := factorEnrollment(ctx, tx, ch.UserID, proof, true); err != nil {
		return false, err
	}

	// Parse timestamps rather than compare RFC3339Nano strings: fractional
	// seconds have variable width and lexical ordering is wrong at boundaries.
	var challengeExpiry string
	if err := tx.QueryRowContext(ctx, `SELECT expires_at FROM login_challenges WHERE id=? AND user_id=? AND expected_hash=?`, ch.ID, ch.UserID, ch.ExpectedHash).Scan(&challengeExpiry); err != nil {
		return false, err
	}
	expires, err := parseTime(challengeExpiry)
	if err != nil {
		return false, err
	}
	if !expires.After(time.Now()) {
		return false, nil
	}
	r, err := tx.ExecContext(ctx, `UPDATE login_challenges SET status='consumed' WHERE id=? AND user_id=? AND expected_hash=? AND status='pending' AND attempts<=max_attempts`, ch.ID, ch.UserID, ch.ExpectedHash)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return false, err
	}
	if n != 1 {
		return false, nil
	}
	if err := spendFactor(ctx, tx, ch.UserID, proof); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,expires_at,created_at) VALUES(?,?,?,?,?)`, session, ch.UserID, token, expiry.UTC().Format(time.RFC3339Nano), nowText()); err != nil {
		return false, err
	}
	if err := factorAudit(ctx, tx, ch.UserID, ch.UserID, "totp.challenge", ""); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (db *DB) ResetTOTPBound(ctx context.Context, actor, actorHash string, proof ports.FactorProof, target string, targetSealed []byte, corr string) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := factorCredentials(ctx, tx, actor, actorHash); err != nil {
		return err
	}
	var operator bool
	if err := tx.QueryRowContext(ctx, `SELECT is_operator FROM users WHERE id=?`, actor).Scan(&operator); err != nil {
		return err
	}
	if !operator {
		return domain.Forbidden("administrator required")
	}
	var hasFactor int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM totp_enrollments WHERE user_id=? AND confirmed=1`, actor).Scan(&hasFactor); err != nil {
		return err
	}
	if hasFactor != 0 {
		if err := factorEnrollment(ctx, tx, actor, proof, true); err != nil {
			return err
		}
		if err := spendFactor(ctx, tx, actor, proof); err != nil {
			return err
		}
	} else if len(proof.SealedSecret) != 0 {
		return domain.Conflict("administrator second factor changed")
	}
	var sealed []byte
	if err := tx.QueryRowContext(ctx, `SELECT sealed_secret FROM totp_enrollments WHERE user_id=?`, target).Scan(&sealed); err != nil {
		return err
	}
	if !bytes.Equal(sealed, targetSealed) {
		return domain.Conflict("target second factor changed")
	}
	if err := removeFactor(ctx, tx, target); err != nil {
		return err
	}
	if err := factorAudit(ctx, tx, actor, target, "totp.reset", corr); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) ChangePasswordWithFactor(ctx context.Context, user, oldHash, newHash string, proof ports.FactorProof, corr string) (int, int, error) {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	if err := factorCredentials(ctx, tx, user, oldHash); err != nil {
		return 0, 0, err
	}
	if err := factorEnrollment(ctx, tx, user, proof, true); err != nil {
		return 0, 0, err
	}
	if err := spendFactor(ctx, tx, user, proof); err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, newHash, user); err != nil {
		return 0, 0, err
	}
	r, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, nowText(), user)
	if err != nil {
		return 0, 0, err
	}
	sessions, err := r.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	r, err = tx.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE principal_id=? AND revoked_at IS NULL`, nowText(), user)
	if err != nil {
		return 0, 0, err
	}
	keys, err := r.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE login_challenges SET status='expired' WHERE user_id=? AND status='pending'`, user); err != nil {
		return 0, 0, err
	}
	details, _ := json.Marshal(map[string]any{"sessionsRevoked": sessions, "keysRevoked": keys, "secondFactor": true})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(id,actor,action,resource,outcome,correlation,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, domain.NewID("aud"), user, "password.change", user, "success", corr, string(details), nowText()); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return int(sessions), int(keys), nil
}

package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/pentaqilo/netsidian/internal/ports"
)

func (db *DB) GetTOTPEnrollment(ctx context.Context, userID string) (*ports.TOTPEnrollment, error) {
	var e ports.TOTPEnrollment
	var confirmed int
	var created, updated string
	err := db.SQL.QueryRowContext(ctx,
		`SELECT user_id, sealed_secret, confirmed, created_at, updated_at FROM totp_enrollments WHERE user_id=?`, userID,
	).Scan(&e.UserID, &e.SealedSecret, &confirmed, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Confirmed = confirmed != 0
	e.CreatedAt, _ = parseTime(created)
	e.UpdatedAt, _ = parseTime(updated)
	return &e, nil
}

func (db *DB) CountRecoveryCodes(ctx context.Context, userID string) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM totp_recovery_codes WHERE user_id=? AND used_at IS NULL`, userID).Scan(&n)
	return n, err
}

func (db *DB) CreateLoginChallenge(ctx context.Context, id, userID, expectedHash, tokenHash string, expiresAt time.Time, maxAttempts int) error {
	_, err := db.SQL.ExecContext(ctx, `INSERT INTO login_challenges(id,user_id,token_hash,expected_hash,status,attempts,max_attempts,expires_at,created_at) VALUES(?,?,?,?,'pending',0,?,?,?)`,
		id, userID, tokenHash, expectedHash, maxAttempts, expiresAt.UTC().Format(time.RFC3339Nano), nowText())
	return err
}

// BeginLoginChallengeAttempt loads a pending challenge and counts this attempt
// atomically. Expired or exhausted challenges are transitioned to a terminal
// status so later attempts see them as spent.
func (db *DB) BeginLoginChallengeAttempt(ctx context.Context, tokenHash string) (*ports.LoginChallenge, bool, error) {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var c ports.LoginChallenge
	var status, expires string
	err = tx.QueryRowContext(ctx, `SELECT id,user_id,expected_hash,status,attempts,max_attempts,expires_at FROM login_challenges WHERE token_hash=?`, tokenHash).
		Scan(&c.ID, &c.UserID, &c.ExpectedHash, &status, &c.Attempts, &c.MaxAttempts, &expires)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	c.Status = status
	c.ExpiresAt, _ = parseTime(expires)
	if status != "pending" {
		return nil, false, nil
	}
	if time.Now().UTC().After(c.ExpiresAt) {
		if _, err = tx.ExecContext(ctx, `UPDATE login_challenges SET status='expired' WHERE id=? AND status='pending'`, c.ID); err != nil {
			return nil, false, err
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	if c.Attempts >= c.MaxAttempts {
		if _, err = tx.ExecContext(ctx, `UPDATE login_challenges SET status='failed' WHERE id=? AND status='pending'`, c.ID); err != nil {
			return nil, false, err
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}

	allowed, err := reserveFactorAttempt(ctx, tx, c.UserID)
	if err != nil {
		return nil, false, err
	}
	if !allowed {
		return nil, false, nil
	}
	res, err := tx.ExecContext(ctx, `UPDATE login_challenges SET attempts=attempts+1 WHERE id=? AND status='pending'`, c.ID)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n != 1 {
		return nil, false, nil
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	c.Attempts++
	return &c, true, nil
}

func reserveFactorAttempt(ctx context.Context, tx *sql.Tx, user string) (bool, error) {
	nowUnix := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO totp_attempt_budgets(user_id,window_started_at,attempts) VALUES(?,?,0) ON CONFLICT(user_id) DO NOTHING`, user, nowUnix); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE totp_attempt_budgets SET window_started_at=?,attempts=0 WHERE user_id=? AND window_started_at<=?`, nowUnix, user, nowUnix-300); err != nil {
		return false, err
	}
	r, err := tx.ExecContext(ctx, `UPDATE totp_attempt_budgets SET attempts=attempts+1 WHERE user_id=? AND attempts<10`, user)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
func (db *DB) ReserveTOTPAttempt(ctx context.Context, user string) (bool, error) {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	allowed, err := reserveFactorAttempt(ctx, tx, user)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return allowed, nil
}

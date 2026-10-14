package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/ports"
)

func factorDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "state.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.SQL.Close() })
	if _, err := db.SQL.Exec(`INSERT INTO users(id,display_name,password_hash,created_at) VALUES('u','User','password-proof',?)`, nowText()); err != nil {
		t.Fatal(err)
	}
	return db
}
func pendingFactor(t *testing.T, db *DB) ports.FactorProof {
	t.Helper()
	if err := db.StartTOTPBound(context.Background(), "u", "password-proof", []byte("sealed"), []string{"recovery"}, ""); err != nil {
		t.Fatal(err)
	}
	return ports.FactorProof{SealedSecret: []byte("sealed"), Step: time.Now().Unix() / 30}
}
func countSQL(t *testing.T, db *DB, q string) int {
	t.Helper()
	var n int
	if err := db.SQL.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestFactorConfirmationRollbackAndRevocation(t *testing.T) {
	db := factorDB(t)
	ctx := context.Background()
	proof := pendingFactor(t, db)
	if err := db.CreatePasswordSession(ctx, "old", "u", "password-proof", "old-token", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`CREATE TRIGGER fail_factor_audit BEFORE INSERT ON audit_events WHEN NEW.action='totp.confirm' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.ConfirmTOTPBound(ctx, "u", proof, ""); err == nil {
		t.Fatal("audit failure accepted")
	}
	if countSQL(t, db, `SELECT confirmed FROM totp_enrollments WHERE user_id='u'`) != 0 || countSQL(t, db, `SELECT count(*) FROM totp_used_steps`) != 0 || countSQL(t, db, `SELECT count(*) FROM sessions WHERE revoked_at IS NULL`) != 1 {
		t.Fatal("failed confirmation had partial effects")
	}
	db.SQL.Exec(`DROP TRIGGER fail_factor_audit`)
	if err := db.ConfirmTOTPBound(ctx, "u", proof, ""); err != nil {
		t.Fatal(err)
	}
	if countSQL(t, db, `SELECT count(*) FROM sessions WHERE revoked_at IS NULL`) != 0 {
		t.Fatal("pre-factor session survived")
	}
	if err := db.CreatePasswordSession(ctx, "bypass", "u", "password-proof", "bypass-token", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("stale password-only login bypassed MFA")
	}
}
func TestPendingEnrollmentRejectsStaleProof(t *testing.T) {
	for _, mode := range []string{"expired", "password-changed", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			db := factorDB(t)
			proof := pendingFactor(t, db)
			switch mode {
			case "expired":
				db.SQL.Exec(`UPDATE totp_enrollments SET created_at=?`, time.Now().Add(-11*time.Minute).UTC().Format(time.RFC3339Nano))
			case "password-changed":
				db.SQL.Exec(`UPDATE users SET password_hash='new'`)
			case "replaced":
				if err := db.StartTOTPBound(context.Background(), "u", "password-proof", []byte("replacement"), []string{"new-code"}, ""); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.ConfirmTOTPBound(context.Background(), "u", proof, ""); err == nil {
				t.Fatal("stale enrollment proof accepted")
			}
			if countSQL(t, db, `SELECT confirmed FROM totp_enrollments WHERE user_id='u'`) != 0 {
				t.Fatal("stale proof enabled MFA")
			}
		})
	}
}
func TestRecoveryCompletionRollbackSingleUseAndExpiry(t *testing.T) {
	db := factorDB(t)
	ctx := context.Background()
	proof := pendingFactor(t, db)
	if err := db.ConfirmTOTPBound(ctx, "u", proof, ""); err != nil {
		t.Fatal(err)
	}
	proof.RecoveryHash = "recovery"
	if err := db.CreateLoginChallenge(ctx, "c", "u", "password-proof", "token", time.Now().Add(time.Minute), 5); err != nil {
		t.Fatal(err)
	}
	ch, ok, err := db.BeginLoginChallengeAttempt(ctx, "token")
	if err != nil || !ok {
		t.Fatalf("begin: %v", err)
	}
	db.SQL.Exec(`CREATE TRIGGER fail_factor_audit BEFORE INSERT ON audit_events WHEN NEW.action='totp.challenge' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`)
	if _, err := db.CompleteChallengeBound(ctx, ch, proof, "session", "session-token", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("failed audit accepted")
	}
	if countSQL(t, db, `SELECT count(*) FROM sessions`) != 0 || countSQL(t, db, `SELECT count(*) FROM totp_recovery_codes WHERE used_at IS NULL`) != 1 || countSQL(t, db, `SELECT count(*) FROM login_challenges WHERE status='pending'`) != 1 {
		t.Fatal("recovery was spent without session")
	}
	db.SQL.Exec(`DROP TRIGGER fail_factor_audit`)
	db.SQL.Exec(`UPDATE login_challenges SET expires_at=?`, time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano))
	if ok, err := db.CompleteChallengeBound(ctx, ch, proof, "expired", "expired-token", time.Now().Add(time.Hour)); err != nil || ok {
		t.Fatalf("expired completion: %v %v", ok, err)
	}
	db.SQL.Exec(`UPDATE login_challenges SET expires_at=?`, time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano))
	if ok, err := db.CompleteChallengeBound(ctx, ch, proof, "session", "session-token", time.Now().Add(time.Hour)); err != nil || !ok {
		t.Fatalf("retry: %v %v", ok, err)
	}
	if ok, err := db.CompleteChallengeBound(ctx, ch, proof, "second", "second-token", time.Now().Add(time.Hour)); err != nil || ok {
		t.Fatalf("challenge replay: %v %v", ok, err)
	}
	if err := db.CreateLoginChallenge(ctx, "c2", "u", "password-proof", "token2", time.Now().Add(time.Minute), 5); err != nil {
		t.Fatal(err)
	}
	ch, _, _ = db.BeginLoginChallengeAttempt(ctx, "token2")
	if _, err := db.CompleteChallengeBound(ctx, ch, proof, "third", "third-token", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("recovery reused across challenges")
	}
}
func TestFactorPasswordChangeRollsBackSpentProof(t *testing.T) {
	db := factorDB(t)
	ctx := context.Background()
	proof := pendingFactor(t, db)
	if err := db.ConfirmTOTPBound(ctx, "u", proof, ""); err != nil {
		t.Fatal(err)
	}
	proof.RecoveryHash = "recovery"
	db.SQL.Exec(`CREATE TRIGGER fail_factor_audit BEFORE INSERT ON audit_events WHEN NEW.action='password.change' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`)
	if _, _, err := db.ChangePasswordWithFactor(ctx, "u", "password-proof", "new", proof, ""); err == nil {
		t.Fatal("failed audit accepted")
	}
	if countSQL(t, db, `SELECT count(*) FROM users WHERE password_hash='password-proof'`) != 1 || countSQL(t, db, `SELECT count(*) FROM totp_recovery_codes WHERE used_at IS NULL`) != 1 {
		t.Fatal("password or recovery changed after failure")
	}
	db.SQL.Exec(`DROP TRIGGER fail_factor_audit`)
	if _, _, err := db.ChangePasswordWithFactor(ctx, "u", "password-proof", "new", proof, ""); err != nil {
		t.Fatal(err)
	}
}
func TestFactorAttemptBudgetSurvivesNewChallenges(t *testing.T) {
	db := factorDB(t)
	ctx := context.Background()
	for i := 0; i < 11; i++ {
		id := fmt.Sprint(i)
		if err := db.CreateLoginChallenge(ctx, id, "u", "password-proof", id, time.Now().Add(time.Minute), 5); err != nil {
			t.Fatal(err)
		}
		_, ok, err := db.BeginLoginChallengeAttempt(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if ok != (i < 10) {
			t.Fatalf("attempt %d accepted=%v", i, ok)
		}
	}
	db.SQL.Exec(`UPDATE totp_attempt_budgets SET window_started_at=?`, time.Now().Unix()-301)
	if ok, err := db.ReserveTOTPAttempt(ctx, "u"); err != nil || !ok {
		t.Fatalf("budget did not expire: %v", err)
	}
}
func TestSchemaSevenUpgradePreservesAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	raw.Exec(`CREATE TABLE meta(key TEXT PRIMARY KEY,value TEXT NOT NULL)`)
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files[:7] {
		data, _ := migrationFS.ReadFile("migrations/" + f.Name())
		for _, stmt := range splitStatements(string(data)) {
			if _, err := raw.Exec(stmt); err != nil {
				t.Fatal(err)
			}
		}
	}
	raw.Exec(`INSERT INTO meta VALUES('schema_version','7')`)
	if _, err := raw.Exec(`INSERT INTO users(id,display_name,password_hash,created_at) VALUES('original','Existing','old-proof',?)`, nowText()); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	for i := 0; i < 2; i++ {
		db, err := Open(path, 5000)
		if err != nil {
			t.Fatal(err)
		}
		if countSQL(t, db, `SELECT CAST(value AS INTEGER) FROM meta WHERE key='schema_version'`) != 9 || countSQL(t, db, `SELECT count(*) FROM users WHERE id='original' AND password_hash='old-proof'`) != 1 {
			t.Fatal("upgrade lost account or schema version")
		}
		if err := db.StartTOTPBound(context.Background(), "original", "old-proof", []byte("sealed"), []string{"code"}, ""); err != nil {
			t.Fatal(err)
		}
		db.SQL.Close()
	}
}

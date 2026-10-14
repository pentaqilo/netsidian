package service

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/storage/local"
)

// Password change tests (roadmap Phase A): current-password verification,
// local-account eligibility, human-only access, and the atomic revocation
// policy (all sessions + all personal API keys) recorded in the audit feed.

func newLocalHarness(t *testing.T) (*Services, *sqlite.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "state", "database", "netsidian.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.SQL.Close() })
	content, err := local.NewContent(filepath.Join(dir, "state", "content"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := local.NewState(filepath.Join(dir, "state", "journal"), filepath.Join(dir, "state", "history"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewServices(Config{
		AuthMode: "local", MaxNoteBytes: 1 << 20, TrashRetentionDays: 30,
	}, db, db, db, db, db, db, db, content, state)
	return svc, db
}

func TestChangePasswordRotatesAndRevokesAllCredentials(t *testing.T) {
	svc, _ := newLocalHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}

	session, _, err := svc.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := svc.ResolveSession(ctx, session); err != nil || !ok {
		t.Fatal("session should resolve before the change")
	}
	keyRecord, keySecret, err := svc.CreateKey(ctx, a, "agent", nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AuthenticateKey(ctx, keySecret); err != nil {
		t.Fatal("key should authenticate before the change")
	}

	res, err := svc.ChangePassword(ctx, a, "0123456789ab", "brandnewpass10", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionsRevoked != 1 || res.KeysRevoked != 1 {
		t.Fatalf("expected 1 session + 1 key revoked, got %+v", res)
	}

	// old session and old key must no longer authenticate
	if _, ok, err := svc.ResolveSession(ctx, session); err != nil || ok {
		t.Fatalf("old session must be revoked (ok=%v err=%v)", ok, err)
	}
	if _, _, err := svc.AuthenticateKey(ctx, keySecret); err == nil {
		t.Fatal("old key must be revoked")
	}
	if k, err := svc.Users.KeyByID(ctx, keyRecord.ID); err != nil || k == nil || k.RevokedAt == nil {
		t.Fatalf("key record must show revocation: %+v err=%v", k, err)
	}

	// old password no longer logs in; new password does
	if _, err := svc.Login(ctx, "op@x.io", "0123456789ab"); err == nil {
		t.Fatal("old password must not log in")
	}
	if _, err := svc.Login(ctx, "op@x.io", "brandnewpass10"); err != nil {
		t.Fatalf("new password must log in: %v", err)
	}

	// the change is audited and visible to operators
	events, err := svc.ListAdminAudit(ctx, a, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range events {
		if e.Action == "password.change" && e.Actor == u.ID && e.Outcome == "success" {
			found = true
		}
	}
	if !found {
		t.Fatalf("password.change audit event missing: %+v", events)
	}
}

func TestChangePasswordWrongCurrentChangesNothing(t *testing.T) {
	svc, _ := newLocalHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	session, _, err := svc.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, keySecret, err := svc.CreateKey(ctx, a, "agent", nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	err = runChange(svc, ctx, a, "wrongcurrent1", "brandnewpass10")
	if de, ok := err.(*domain.Error); !ok || de.Code != domain.ErrCodeUnauthenticated {
		t.Fatalf("expected unauthenticated, got %v", err)
	}

	// failed verification must leave every credential intact
	if _, ok, err := svc.ResolveSession(ctx, session); err != nil || !ok {
		t.Fatal("session must survive a failed change")
	}
	if _, _, err := svc.AuthenticateKey(ctx, keySecret); err != nil {
		t.Fatal("key must survive a failed change")
	}
	if _, err := svc.Login(ctx, "op@x.io", "0123456789ab"); err != nil {
		t.Fatal("old password must still log in after a failed change")
	}
}

func TestChangePasswordRejectsKeyPrincipalsAndPolicyViolations(t *testing.T) {
	svc, _ := newLocalHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	human := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	keyRecord, keySecret, err := svc.CreateKey(ctx, human, "agent", nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AuthenticateKey(ctx, keySecret); err != nil {
		t.Fatal(err)
	}

	// API-key principals cannot rotate passwords (same rule as key management)
	keyActor := &Actor{UserID: u.ID, Key: keyRecord}
	if err := runChange(svc, ctx, keyActor, "0123456789ab", "brandnewpass10"); err == nil {
		t.Fatal("key principals must be forbidden from changing passwords")
	} else if de, ok := err.(*domain.Error); !ok || de.Code != domain.ErrCodeForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}

	// length policy matches account creation; reuse is rejected
	if err := runChange(svc, ctx, human, "0123456789ab", "short9ch"); err == nil {
		t.Fatal("short new password must be rejected")
	}
	if err := runChange(svc, ctx, human, "0123456789ab", "0123456789ab"); err == nil {
		t.Fatal("reusing the current password must be rejected")
	}
	// and none of the rejections changed anything
	if _, err := svc.Login(ctx, "op@x.io", "0123456789ab"); err != nil {
		t.Fatal("password must be unchanged after rejected attempts")
	}
}

func TestChangePasswordProviderManagedAccountConflict(t *testing.T) {
	svc, db := newLocalHarness(t)
	ctx := context.Background()
	ext := &domain.User{ID: "usr_external_test", DisplayName: "Ext", CreatedAt: time.Now()}
	if err := db.CreateExternalUser(ctx, "https://idp.example", "subject-1", ext); err != nil {
		t.Fatal(err)
	}
	err := runChange(svc, ctx, &Actor{UserID: ext.ID, Local: true}, "whatever12345", "brandnewpass10")
	if de, ok := err.(*domain.Error); !ok || de.Code != domain.ErrCodeConflict {
		t.Fatalf("expected conflict for provider-managed account, got %v", err)
	}
}

func runChange(svc *Services, ctx context.Context, a *Actor, current, next string) error {
	_, err := svc.ChangePassword(ctx, a, current, next, "")
	return err
}

func TestPasswordRotationRollbackAndStaleProof(t *testing.T) {
	svc, db := newLocalHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@example.test", "old-password-123", "Op")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := svc.CreateKey(ctx, &Actor{UserID: u.ID}, "agent", nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`CREATE TRIGGER fail_password_audit BEFORE INSERT ON audit_events WHEN NEW.action='password.change' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ChangePassword(ctx, &Actor{UserID: u.ID}, "old-password-123", "new-password-123", ""); err == nil {
		t.Fatal("audit failure must roll back")
	}
	if _, err := svc.Login(ctx, u.Email, "old-password-123"); err != nil {
		t.Fatal("password changed despite rollback", err)
	}
	if _, ok, err := svc.ResolveSession(ctx, token); err != nil || !ok {
		t.Fatal("session revoked despite rollback")
	}
	if _, _, err := svc.AuthenticateKey(ctx, secret); err != nil {
		t.Fatal("key revoked despite rollback", err)
	}
	if _, err := db.SQL.Exec(`DROP TRIGGER fail_password_audit`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ChangePassword(ctx, &Actor{UserID: u.ID}, "old-password-123", "new-password-123", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.ChangePassword(ctx, u.ID, u.PasswordHash, "stale-hash", ""); err == nil {
		t.Fatal("stale verified hash overwrote new password")
	}
	if err := db.CreatePasswordSession(ctx, "stale-session", u.ID, u.PasswordHash, "stale-token", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("stale login proof created a session")
	}
	if _, _, _, err := svc.LoginSession(ctx, u.Email, "new-password-123"); err != nil {
		t.Fatal("new login failed", err)
	}
}

func TestConcurrentPasswordChangesHaveOneWinner(t *testing.T) {
	svc, db := newLocalHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@example.test", "old-password-123", "Op")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, hash := range []string{"first-new-hash", "second-new-hash"} {
		wg.Add(1)
		go func(hash string) {
			defer wg.Done()
			<-start
			_, _, err := db.ChangePassword(ctx, u.ID, u.PasswordHash, hash, "")
			results <- err
		}(hash)
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("%d concurrent changes succeeded", wins)
	}
	var audits int
	if err := db.SQL.QueryRow(`SELECT count(*) FROM audit_events WHERE action='password.change'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}
}

func TestPasswordChangeModesAndOtherAccounts(t *testing.T) {
	svc, _ := newLocalHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@example.test", "old-password-123", "Op")
	if err != nil {
		t.Fatal(err)
	}
	svc.Cfg.SignupEnabled = true
	other, err := svc.SignupAccount(ctx, "other@example.test", "other-password-123", "Other")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.NewSession(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"none", "external"} {
		svc.Cfg.AuthMode = mode
		if _, err := svc.ChangePassword(ctx, &Actor{UserID: u.ID}, "old-password-123", "new-password-123", ""); err == nil {
			t.Fatal("change accepted in mode", mode)
		}
	}
	svc.Cfg.AuthMode = "mixed"
	if _, err := svc.ChangePassword(ctx, &Actor{UserID: u.ID}, "old-password-123", "new-password-123", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := svc.ResolveSession(ctx, token); err != nil || !ok {
		t.Fatal("other account session was revoked")
	}
	if _, err := svc.ChangePassword(ctx, nil, "old-password-123", "new-password-123", ""); err == nil {
		t.Fatal("missing actor accepted")
	}
}

func TestExistingLongPasswordRemainsUsable(t *testing.T) {
	svc, _ := newLocalHarness(t)
	ctx := context.Background()
	old := strings.Repeat("x", 1500)
	u, err := svc.SetupAccount(ctx, "old@example.test", old, "Op")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := svc.LoginSession(ctx, u.Email, old); err != nil {
		t.Fatal("existing long-password login broken", err)
	}
	if _, err := svc.ChangePassword(ctx, &Actor{UserID: u.ID}, old, "new-password-123", ""); err != nil {
		t.Fatal("existing long password cannot be changed", err)
	}
}

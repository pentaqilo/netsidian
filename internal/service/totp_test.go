package service

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/storage/local"
	"github.com/pentaqilo/netsidian/internal/storage/providers"
)

// Second-factor tests (roadmap Phase A): enrollment that only enforces after
// confirmation, sealed-at-rest secrets, expiring attempt-limited challenges
// that never authorize ordinary operations, atomic replay prevention, and
// single-use recovery codes.

func newFactorHarness(t *testing.T) (*Services, *sqlite.DB) {
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
	mgr, err := providers.New(content, db, []byte("test-instance-secret-32-bytes!!!"))
	if err != nil {
		t.Fatal(err)
	}
	svc.Seal = mgr
	return svc, db
}

func currentTOTP(t *testing.T, secret string, offset int64) string {
	t.Helper()
	step := time.Now().Unix() / int64(totpStep/time.Second)
	code, err := totpCode(secret, step+offset)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func enrollAndConfirm(t *testing.T, svc *Services, ctx context.Context, a *Actor, password string) *TOTPEnrollmentView {
	t.Helper()
	view, err := svc.StartTOTPEnrollment(ctx, a, password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmTOTPEnrollment(ctx, a, currentTOTP(t, view.Secret, 0)); err != nil {
		t.Fatalf("confirm failed: %v", err)
	}
	return view
}

func TestTOTPEnrollmentEnforcesOnlyAfterConfirmation(t *testing.T) {
	svc, _ := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}

	view, err := svc.StartTOTPEnrollment(ctx, a, "0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.RecoveryCodes) != totpRecovery || view.Secret == "" {
		t.Fatalf("enrollment view incomplete: %+v", view)
	}
	st, err := svc.TOTPStatus(ctx, u.ID)
	if err != nil || !st.Pending || st.Enabled {
		t.Fatalf("a started enrollment must be pending, not enforced: %+v err=%v", st, err)
	}

	// Before confirmation, login still issues a normal session (no challenge).
	out, err := svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out.ChallengeToken != "" || out.SessionToken == "" {
		t.Fatalf("unconfirmed factor must not challenge login: %+v err=%v", out, err)
	}

	// A wrong code must not confirm the factor.
	if _, err := svc.ConfirmTOTPEnrollment(ctx, a, "000000"); err == nil {
		t.Fatal("invalid code must not confirm enrollment")
	}
	if st, _ := svc.TOTPStatus(ctx, u.ID); st.Enabled {
		t.Fatal("factor must remain unenforced after a failed confirmation")
	}

	// A valid code confirms it; login now yields a challenge, not a session.
	if _, err := svc.ConfirmTOTPEnrollment(ctx, a, currentTOTP(t, view.Secret, 0)); err != nil {
		t.Fatal(err)
	}
	if st, _ := svc.TOTPStatus(ctx, u.ID); !st.Enabled || st.Pending {
		t.Fatalf("factor must be enforced after confirmation: %+v", st)
	}
	out, err = svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out.ChallengeToken == "" || out.SessionToken != "" {
		t.Fatalf("confirmed factor must challenge login: %+v err=%v", out, err)
	}
}

func TestTOTPSecretIsSealedAtRest(t *testing.T) {
	svc, db := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	view, err := svc.StartTOTPEnrollment(ctx, a, "0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	var blob []byte
	if err := db.SQL.QueryRow(`SELECT sealed_secret FROM totp_enrollments WHERE user_id=?`, u.ID).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), view.Secret) {
		t.Fatal("shared secret stored in plaintext")
	}
	// It must round-trip through the instance key.
	got, err := svc.openTOTPSecret(u.ID, blob)
	if err != nil || got != view.Secret {
		t.Fatalf("sealed secret did not round-trip: %v", err)
	}
}

func TestTOTPLoginChallengeCompletesAndReplayFails(t *testing.T) {
	svc, _ := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	view := enrollAndConfirm(t, svc, ctx, a, "0123456789ab")

	out, err := svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out.ChallengeToken == "" {
		t.Fatalf("expected challenge: %+v err=%v", out, err)
	}
	// The challenge token is not a session and cannot authorize ordinary use.
	if _, ok, err := svc.ResolveSession(ctx, out.ChallengeToken); err != nil || ok {
		t.Fatalf("challenge token must not resolve as a session (ok=%v err=%v)", ok, err)
	}

	code := currentTOTP(t, view.Secret, 1)
	done, err := svc.CompleteChallengeWithTOTP(ctx, out.ChallengeToken, code)
	if err != nil || done.SessionToken == "" {
		t.Fatalf("challenge completion failed: %+v err=%v", done, err)
	}
	if _, ok, err := svc.ResolveSession(ctx, done.SessionToken); err != nil || !ok {
		t.Fatalf("completed challenge must issue a real session (ok=%v err=%v)", ok, err)
	}

	// Replaying the same code (same time step) must fail.
	if _, err := svc.CompleteChallengeWithTOTP(ctx, out.ChallengeToken, code); err == nil {
		t.Fatal("replayed code must be rejected")
	}
}

func TestTOTPChallengeAttemptLimit(t *testing.T) {
	svc, _ := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	enrollAndConfirm(t, svc, ctx, a, "0123456789ab")

	out, err := svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out.ChallengeToken == "" {
		t.Fatal("expected challenge")
	}
	var lastErr error
	for i := 0; i <= loginChallengeMaxAttempt; i++ {
		_, lastErr = svc.CompleteChallengeWithTOTP(ctx, out.ChallengeToken, "123456")
	}
	if lastErr == nil {
		t.Fatal("challenge must stop accepting attempts after the limit")
	}
	if de, ok := lastErr.(*domain.Error); !ok || de.Code != domain.ErrCodeUnauthenticated {
		t.Fatalf("expected unauthenticated after limit, got %v", lastErr)
	}
}

func TestTOTPChallengeConsumedOnceUnderConcurrency(t *testing.T) {
	svc, _ := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	view := enrollAndConfirm(t, svc, ctx, a, "0123456789ab")

	out, err := svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out.ChallengeToken == "" {
		t.Fatal("expected challenge")
	}
	// Enrollment consumed the current step. Both contenders use the next
	// accepted step; +2 is outside the configured +/-1 window.
	code := currentTOTP(t, view.Secret, 1)
	codes := []string{code, code}
	var wg sync.WaitGroup
	results := make(chan error, len(codes))
	start := make(chan struct{})
	for _, c := range codes {
		wg.Add(1)
		go func(code string) {
			defer wg.Done()
			<-start
			_, err := svc.CompleteChallengeWithTOTP(ctx, out.ChallengeToken, code)
			results <- err
		}(c)
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else {
			if de, ok := err.(*domain.Error); !ok || de.Code != domain.ErrCodeUnauthenticated {
				t.Errorf("losing completion must reject authentication, not fail storage: %v", err)
			}
		}
	}
	if wins != 1 {
		t.Fatalf("%d concurrent completions succeeded; a challenge must be consumed once", wins)
	}
}

// Different recovery codes isolate challenge consumption from factor replay:
// both proofs are valid and unused, but only one may create a session.
func TestTOTPChallengeDistinctRecoveryCodesUnderConcurrency(t *testing.T) {
	svc, db := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	view := enrollAndConfirm(t, svc, ctx, &Actor{UserID: u.ID, IsOperator: true, Local: true}, "0123456789ab")
	out, err := svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out.ChallengeToken == "" {
		t.Fatalf("challenge: %v", err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, code := range view.RecoveryCodes[:2] {
		go func(code string) {
			<-start
			_, err := svc.CompleteChallengeWithRecovery(ctx, out.ChallengeToken, code)
			results <- err
		}(code)
	}
	close(start)
	wins := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			wins++
		} else if de, ok := err.(*domain.Error); !ok || de.Code != domain.ErrCodeUnauthenticated {
			t.Errorf("losing completion must reject authentication, not fail storage: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d completions succeeded; distinct proofs must consume the challenge once", wins)
	}
	status, err := svc.TOTPStatus(ctx, u.ID)
	if err != nil || status.RecoveryCodesRemaining != totpRecovery-1 {
		t.Fatalf("only the winning proof may be spent: %+v err=%v", status, err)
	}
	var sessions int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE user_id=? AND revoked_at IS NULL`, u.ID).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("only the winning completion may create a session: count=%d err=%v", sessions, err)
	}
}

func TestTOTPRecoveryCodeIsSingleUse(t *testing.T) {
	svc, _ := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	view := enrollAndConfirm(t, svc, ctx, a, "0123456789ab")

	out, err := svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out.ChallengeToken == "" {
		t.Fatal("expected challenge")
	}
	code := view.RecoveryCodes[0]
	done, err := svc.CompleteChallengeWithRecovery(ctx, out.ChallengeToken, code)
	if err != nil || done.SessionToken == "" {
		t.Fatalf("recovery completion failed: %+v err=%v", done, err)
	}
	if st, _ := svc.TOTPStatus(ctx, u.ID); st.RecoveryCodesRemaining != totpRecovery-1 {
		t.Fatalf("recovery code was not consumed: %+v", st)
	}

	// The same code cannot be spent again on a fresh challenge.
	out2, err := svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out2.ChallengeToken == "" {
		t.Fatal("expected second challenge")
	}
	if _, err := svc.CompleteChallengeWithRecovery(ctx, out2.ChallengeToken, code); err == nil {
		t.Fatal("a spent recovery code must not work again")
	}
}

func TestTOTPRecoveryCodeSingleUseUnderConcurrency(t *testing.T) {
	svc, _ := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	view := enrollAndConfirm(t, svc, ctx, a, "0123456789ab")

	out, err := svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out.ChallengeToken == "" {
		t.Fatal("expected challenge")
	}
	code := view.RecoveryCodes[1]
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.CompleteChallengeWithRecovery(ctx, out.ChallengeToken, code)
			results <- err == nil
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for ok := range results {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("%d concurrent recovery completions succeeded", wins)
	}
}

func TestTOTPDisableRequiresPasswordAndFactor(t *testing.T) {
	svc, _ := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	view := enrollAndConfirm(t, svc, ctx, a, "0123456789ab")

	if err := svc.DisableTOTP(ctx, a, "wrongpassword1", currentTOTP(t, view.Secret, 0)); err == nil {
		t.Fatal("disable must require the current password")
	}
	if err := svc.DisableTOTP(ctx, a, "0123456789ab", "000000"); err == nil {
		t.Fatal("disable must require a valid factor code")
	}
	if err := svc.DisableTOTP(ctx, a, "0123456789ab", currentTOTP(t, view.Secret, 1)); err != nil {
		t.Fatalf("valid disable failed: %v", err)
	}
	if st, _ := svc.TOTPStatus(ctx, u.ID); st.Enabled || st.Pending {
		t.Fatalf("factor must be gone after disable: %+v", st)
	}
	out, err := svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out.ChallengeToken != "" || out.SessionToken == "" {
		t.Fatalf("login must be single-factor again after disable: %+v err=%v", out, err)
	}
}

func TestTOTPRegenerateRecoveryCodesInvalidatesOld(t *testing.T) {
	svc, _ := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	view := enrollAndConfirm(t, svc, ctx, a, "0123456789ab")

	newCodes, err := svc.RegenerateRecoveryCodes(ctx, a, "0123456789ab", currentTOTP(t, view.Secret, 1))
	if err != nil || len(newCodes) != totpRecovery {
		t.Fatalf("regeneration failed: %v", err)
	}

	out, err := svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out.ChallengeToken == "" {
		t.Fatal("expected challenge")
	}
	if _, err := svc.CompleteChallengeWithRecovery(ctx, out.ChallengeToken, view.RecoveryCodes[2]); err == nil {
		t.Fatal("an old recovery code must not work after regeneration")
	}
	out2, err := svc.LoginWithFactor(ctx, "op@x.io", "0123456789ab")
	if err != nil || out2.ChallengeToken == "" {
		t.Fatal("expected challenge")
	}
	if _, err := svc.CompleteChallengeWithRecovery(ctx, out2.ChallengeToken, newCodes[0]); err != nil {
		t.Fatalf("new recovery code failed: %v", err)
	}
}

func TestResetUserTOTPIsOperatorOnlyRecovery(t *testing.T) {
	svc, _ := newFactorHarness(t)
	ctx := context.Background()
	svc.Cfg.SignupEnabled = true
	op, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	target, err := svc.SignupAccount(ctx, "user@x.io", "userpassword1", "User")
	if err != nil {
		t.Fatal(err)
	}
	targetActor := &Actor{UserID: target.ID}
	enrollAndConfirm(t, svc, ctx, targetActor, "userpassword1")

	// A non-operator cannot reset someone else's factor.
	if err := svc.ResetUserTOTP(ctx, &Actor{UserID: op.ID}, target.ID); err == nil {
		t.Fatal("non-operator reset must be forbidden")
	}
	// The operator can, and the user then signs in with only their password.
	opActor := &Actor{UserID: op.ID, IsOperator: true}
	if err := svc.ResetUserTOTP(ctx, opActor, target.ID, "0123456789ab"); err != nil {
		t.Fatal(err)
	}
	if st, _ := svc.TOTPStatus(ctx, target.ID); st.Enabled || st.Pending {
		t.Fatalf("factor must be cleared by operator reset: %+v", st)
	}
	out, err := svc.LoginWithFactor(ctx, "user@x.io", "userpassword1")
	if err != nil || out.ChallengeToken != "" || out.SessionToken == "" {
		t.Fatalf("recovered user must sign in with password only: %+v err=%v", out, err)
	}
	// The reset is audited for operators.
	events, err := svc.ListAdminAudit(ctx, opActor, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range events {
		if e.Action == "totp.reset" && e.Resource == target.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("totp.reset audit event missing: %+v", events)
	}
}

func TestTOTPRequiresSealerAndLocalAccount(t *testing.T) {
	svc, db := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}

	// Without a sealer, enrollment is refused rather than storing plaintext.
	svc.Seal = nil
	if _, err := svc.StartTOTPEnrollment(ctx, a, "0123456789ab"); err == nil {
		t.Fatal("enrollment must require a sealer")
	}
	svc.Seal = mustSealer(t, db)

	// A provider-managed account (no local password) cannot enroll.
	ext := &domain.User{ID: "usr_ext", DisplayName: "Ext", CreatedAt: time.Now()}
	if err := db.CreateExternalUser(ctx, "https://idp.example", "sub-1", ext); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartTOTPEnrollment(ctx, &Actor{UserID: ext.ID, Local: true}, "whatever12345"); err == nil {
		t.Fatal("provider-managed account must not enroll")
	}

	// API-key principals cannot manage the factor.
	keyRec, keySecret, err := svc.CreateKey(ctx, a, "agent", nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = keySecret
	if _, err := svc.StartTOTPEnrollment(ctx, &Actor{UserID: u.ID, Key: keyRec}, "0123456789ab"); err == nil {
		t.Fatal("key principals must not manage the factor")
	}
}

func mustSealer(t *testing.T, db *sqlite.DB) ports.Sealer {
	t.Helper()
	content, err := local.NewContent(filepath.Join(t.TempDir(), "content"))
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := providers.New(content, db, []byte("test-instance-secret-32-bytes!!!"))
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

func TestPasswordChangeRequiresFactorWhenEnrolled(t *testing.T) {
	svc, _ := newFactorHarness(t)
	ctx := context.Background()
	u, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	a := &Actor{UserID: u.ID, IsOperator: true, Local: true}
	view := enrollAndConfirm(t, svc, ctx, a, "0123456789ab")

	// Step-up: a confirmed factor must present a valid current code, so a
	// stolen password alone cannot rotate the credential and revoke sessions.
	if _, err := svc.ChangePassword(ctx, a, "0123456789ab", "brandnewpass10", ""); err == nil {
		t.Fatal("password change must require the second factor when enrolled")
	}
	if _, err := svc.ChangePassword(ctx, a, "0123456789ab", "brandnewpass10", "000000"); err == nil {
		t.Fatal("an invalid code must not authorize a password change")
	}
	if _, err := svc.ChangePassword(ctx, a, "0123456789ab", "brandnewpass10", currentTOTP(t, view.Secret, 1)); err != nil {
		t.Fatalf("a valid factor should authorize the change: %v", err)
	}
	// The new password now works and still challenges.
	if out, err := svc.LoginWithFactor(ctx, "op@x.io", "brandnewpass10"); err != nil || out.ChallengeToken == "" {
		t.Fatalf("login after change should still challenge: %+v err=%v", out, err)
	}
}

func TestTOTPRFC6238Vectors(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for _, v := range []struct {
		seconds int64
		code    string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		got, err := totpCode(secret, v.seconds/30)
		if err != nil || got != v.code {
			t.Fatalf("RFC time %d: %s %v", v.seconds, got, err)
		}
	}
}
func TestRecoveryCodeNormalizationAndSetupURL(t *testing.T) {
	if hashRecoveryCode("abcd-efgh-ijkl-mnop-qrst") != hashRecoveryCode("ABCDEFGHIJKLMNOPQRST") {
		t.Fatal("lowercase recovery rejected")
	}
	u, err := url.Parse(otpauthURL("a+b?issuer=evil@example.com", "SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("issuer") != "Netsidian" || u.Query().Get("secret") != "SECRET" || u.Path != "/Netsidian:a+b?issuer=evil@example.com" {
		t.Fatalf("unsafe setup URL: %s", u)
	}
}

func TestAdministratorResetRequiresFreshOwnFactorAndAuditsActor(t *testing.T) {
	svc, db := newFactorHarness(t)
	ctx := context.Background()
	svc.Cfg.SignupEnabled = true
	op, err := svc.SetupAccount(ctx, "op@x.io", "0123456789ab", "Op")
	if err != nil {
		t.Fatal(err)
	}
	target, err := svc.SignupAccount(ctx, "user@x.io", "userpassword1", "User")
	if err != nil {
		t.Fatal(err)
	}
	actor := &Actor{UserID: op.ID, IsOperator: true}
	own := enrollAndConfirm(t, svc, ctx, actor, "0123456789ab")
	enrollAndConfirm(t, svc, ctx, &Actor{UserID: target.ID}, "userpassword1")
	for _, credentials := range [][]string{{}, {"wrong"}, {"0123456789ab"}, {"0123456789ab", "invalid"}} {
		if err := svc.ResetUserTOTP(ctx, actor, target.ID, credentials...); err == nil {
			t.Fatalf("accepted insufficient credentials %d", len(credentials))
		}
	}
	if err := svc.ResetUserTOTP(ctx, actor, target.ID, "0123456789ab", strings.ToLower(own.RecoveryCodes[0])); err != nil {
		t.Fatal(err)
	}
	var auditActor, auditTarget string
	if err := db.SQL.QueryRow(`SELECT actor,resource FROM audit_events WHERE action='totp.reset'`).Scan(&auditActor, &auditTarget); err != nil {
		t.Fatal(err)
	}
	if auditActor != op.ID || auditTarget != target.ID {
		t.Fatalf("incorrect actor/target: %s %s", auditActor, auditTarget)
	}
	if n, err := db.CountRecoveryCodes(ctx, op.ID); err != nil || n != 9 {
		t.Fatalf("operator proof unspent: %d %v", n, err)
	}
}

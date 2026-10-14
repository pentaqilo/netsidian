package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// RFC 6238 TOTP defaults. Kept as constants so there is no required
// configuration; the values match what standard authenticator apps expect.
const (
	totpStep      = 30 * time.Second
	totpDigits    = 6
	totpWindow    = 1 // accept the current step plus one step either side
	totpSecretLen = 20
	totpRecovery  = 10

	loginChallengeTTL        = 5 * time.Minute
	loginChallengeMaxAttempt = 5
)

// totpSecret is the base32 shared secret, stored only sealed at rest.
type totpSecret struct {
	Secret string `json:"secret"`
}

func newTOTPSecret() (string, error) {
	b := make([]byte, totpSecretLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// totpCode computes the RFC 6238 code for a given time step.
func totpCode(secret string, step int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", domain.Invalid("second-factor secret is malformed")
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (int64(sum[offset]&0x7f) << 24) |
		(int64(sum[offset+1]&0xff) << 16) |
		(int64(sum[offset+2]&0xff) << 8) |
		int64(sum[offset+3]&0xff)
	code := value % 1000000
	return fmt.Sprintf("%06d", code), nil
}

// totpMatch checks a code against the current step plus the allowed window
// and returns the matched step (for replay prevention) when it is valid.
func totpMatch(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	step := now.Unix() / int64(totpStep/time.Second)
	for d := -totpWindow; d <= totpWindow; d++ {
		c, err := totpCode(secret, step+int64(d))
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(c), []byte(code)) == 1 {
			return step + int64(d), true
		}
	}
	return 0, false
}

// newRecoveryCodes mints human-friendly single-use codes (XXXX-XXXX-XXXX-XXXX-XXXX).
func newRecoveryCodes(n int) ([]string, error) {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		b := make([]byte, 15) // Displaying 20 Base32 characters retains 100 bits of entropy.
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b) // 24 chars
		out = append(out, group4(raw[:20]))
	}
	return out, nil
}

func group4(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// normalizeRecoveryCode strips separators/case so user input matches the
// stored form before hashing.
func normalizeRecoveryCode(code string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return -1
		}
	}, strings.ToUpper(code)))
}

func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRecoveryCode(code)))
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
}

func otpauthURL(email, secret string) string {
	label := "Netsidian:" + email
	if email == "" {
		label = "Netsidian"
	}
	params := url.Values{"secret": {secret}, "issuer": {"Netsidian"}, "algorithm": {"SHA1"}, "digits": {fmt.Sprint(totpDigits)}, "period": {fmt.Sprint(int(totpStep / time.Second))}}
	u := url.URL{Scheme: "otpauth", Host: "totp", Path: "/" + label, RawQuery: params.Encode()}
	return u.String()
}

// totpRepo resolves the second-factor persistence capability, or reports the
// storage backend does not support it.
func (s *Services) totpRepo() (ports.TOTPRepo, bool) {
	r, ok := s.Users.(ports.TOTPRepo)
	return r, ok
}

// TOTPStatus is the account-facing view of second-factor state.
type TOTPStatus struct {
	Enabled                bool `json:"enabled"`
	Pending                bool `json:"pending"`
	RecoveryCodesRemaining int  `json:"recoveryCodesRemaining"`
}

func (s *Services) TOTPStatus(ctx context.Context, userID string) (*TOTPStatus, error) {
	repo, ok := s.totpRepo()
	if !ok {
		return &TOTPStatus{}, nil
	}
	enr, err := repo.GetTOTPEnrollment(ctx, userID)
	if err != nil {
		return nil, err
	}
	st := &TOTPStatus{}
	if enr != nil {
		st.Enabled = enr.Confirmed
		st.Pending = !enr.Confirmed
		n, err := repo.CountRecoveryCodes(ctx, userID)
		if err != nil {
			return nil, err
		}
		st.RecoveryCodesRemaining = n
	}
	return st, nil
}

// TOTPEnrollmentView is returned once, at enrollment: the shared secret (for
// the authenticator app) and the recovery codes. Neither is retrievable later.
type TOTPEnrollmentView struct {
	Secret        string   `json:"secret"`
	OTPAuthURL    string   `json:"otpauthUrl"`
	RecoveryCodes []string `json:"recoveryCodes"`
}

// requireLocalFactorAccount enforces the preconditions shared by every
// security-changing operation: a human session, a local/mixed instance, a
// local password account, and a freshly verified current password.
func (s *Services) requireLocalFactorAccount(ctx context.Context, a *Actor, password string) (*domain.User, ports.TOTPRepo, error) {
	if a == nil || !a.Human() {
		return nil, nil, domain.Forbidden("second-factor management requires a human session")
	}
	if s.Cfg.AuthMode != "local" && s.Cfg.AuthMode != "mixed" {
		return nil, nil, domain.Unsupported("second factor requires local or mixed authentication")
	}
	if s.Seal == nil {
		return nil, nil, domain.Unsupported("secret sealing is unavailable")
	}
	repo, ok := s.totpRepo()
	if !ok {
		return nil, nil, domain.Unsupported("second factor is not supported by this storage backend")
	}
	u, err := s.Users.UserByID(ctx, a.UserID)
	if err != nil {
		return nil, nil, err
	}
	if u == nil || u.Disabled {
		return nil, nil, domain.Unauthenticated("account is unavailable")
	}
	if u.PasswordHash == "" {
		return nil, nil, domain.Conflict("this account signs in through the identity provider and has no local password")
	}
	if len(password) > 1<<20 {
		return nil, nil, domain.Invalid("password exceeds the request limit")
	}
	if !verifyPassword(password, u.PasswordHash) {
		return nil, nil, domain.Unauthenticated("current password is incorrect")
	}
	return u, repo, nil
}

// StartTOTPEnrollment begins enrollment: it verifies the current password,
// generates a fresh secret and recovery codes, seals the secret, and stores a
// pending (unconfirmed) enrollment. A pending enrollment never enables MFA.
func (s *Services) StartTOTPEnrollment(ctx context.Context, a *Actor, password string) (*TOTPEnrollmentView, error) {
	u, repo, err := s.requireLocalFactorAccount(ctx, a, password)
	if err != nil {
		return nil, err
	}
	enr, err := repo.GetTOTPEnrollment(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if enr != nil && enr.Confirmed {
		return nil, domain.Conflict("second factor already enrolled; disable it first")
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return nil, err
	}
	sealed, err := s.Seal.Seal("totp:"+u.ID, totpSecret{Secret: secret})
	if err != nil {
		return nil, err
	}
	codes, err := newRecoveryCodes(totpRecovery)
	if err != nil {
		return nil, err
	}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = hashRecoveryCode(c)
	}
	atomic, err := s.atomicFactorRepo()
	if err != nil {
		return nil, err
	}
	if err := atomic.StartTOTPBound(ctx, u.ID, u.PasswordHash, sealed, hashes, a.Corr); err != nil {
		return nil, err
	}
	return &TOTPEnrollmentView{Secret: secret, OTPAuthURL: otpauthURL(u.Email, secret), RecoveryCodes: codes}, nil
}

// ConfirmTOTPEnrollment verifies a valid code against the pending enrollment
// and only then flips it to confirmed. A wrong code leaves MFA disabled.
func (s *Services) ConfirmTOTPEnrollment(ctx context.Context, a *Actor, code string) (*TOTPStatus, error) {
	if a == nil || !a.Human() {
		return nil, domain.Forbidden("second-factor management requires a human session")
	}
	repo, ok := s.totpRepo()
	if !ok {
		return nil, domain.Unsupported("second factor is not supported by this storage backend")
	}
	enr, err := repo.GetTOTPEnrollment(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	if enr == nil {
		return nil, domain.NotFound("no pending second-factor enrollment")
	}
	if enr.Confirmed {
		return nil, domain.Conflict("second factor already enrolled")
	}
	if err := reserveFactorVerification(ctx, repo, a.UserID); err != nil {
		return nil, err
	}
	secret, err := s.openTOTPSecret(a.UserID, enr.SealedSecret)
	if err != nil {
		return nil, err
	}
	step, ok := totpMatch(secret, code, time.Now())
	if !ok {
		return nil, domain.Unauthenticated("invalid code")
	}
	atomic, err := s.atomicFactorRepo()
	if err != nil {
		return nil, err
	}
	if s.Cfg.AuthMode != "local" && s.Cfg.AuthMode != "mixed" {
		return nil, domain.Unsupported("local password authentication required")
	}
	if err := atomic.ConfirmTOTPBound(ctx, a.UserID, ports.FactorProof{SealedSecret: enr.SealedSecret, Step: step}, a.Corr); err != nil {
		return nil, err
	}
	return s.TOTPStatus(ctx, a.UserID)
}

// DisableTOTP removes the factor. It requires both the current password and a
// valid current code, so a stolen session cannot silently drop MFA.
func (s *Services) DisableTOTP(ctx context.Context, a *Actor, password, code string) error {
	u, repo, err := s.requireLocalFactorAccount(ctx, a, password)
	if err != nil {
		return err
	}
	proof, err := s.factorProof(ctx, repo, u.ID, code)
	if err != nil {
		return err
	}
	atomic, err := s.atomicFactorRepo()
	if err != nil {
		return err
	}
	return atomic.ChangeTOTPBound(ctx, u.ID, u.PasswordHash, proof, "totp.disable", nil, a.Corr)
}

// RegenerateRecoveryCodes issues a fresh set of recovery codes (invalidating
// the old ones). It requires the current password and a valid current code.
func (s *Services) RegenerateRecoveryCodes(ctx context.Context, a *Actor, password, code string) ([]string, error) {
	u, repo, err := s.requireLocalFactorAccount(ctx, a, password)
	if err != nil {
		return nil, err
	}
	proof, err := s.factorProof(ctx, repo, u.ID, code)
	if err != nil {
		return nil, err
	}
	atomic, err := s.atomicFactorRepo()
	if err != nil {
		return nil, err
	}
	codes, err := newRecoveryCodes(totpRecovery)
	if err != nil {
		return nil, err
	}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = hashRecoveryCode(c)
	}
	if err := atomic.ChangeTOTPBound(ctx, u.ID, u.PasswordHash, proof, "totp.recovery", hashes, a.Corr); err != nil {
		return nil, err
	}
	return codes, nil
}

// ResetUserTOTP is the documented operator-recovery path: an operator removes
// a user's second factor so the user can sign in with their password and
// re-enroll. It never issues a session as the user and is audited.
func (s *Services) ResetUserTOTP(ctx context.Context, a *Actor, targetUserID string, credentials ...string) error {
	if a == nil || !a.Human() || !a.IsOperator {
		return domain.Forbidden("operator session required")
	}
	if err := s.RequireAdministrator(ctx, a); err != nil {
		return err
	}
	if len(credentials) < 1 {
		return domain.Forbidden("fresh administrator password required")
	}
	code := ""
	if len(credentials) > 1 {
		code = credentials[1]
	}
	operator, repo, err := s.requireLocalFactorAccount(ctx, a, credentials[0])
	if err != nil {
		return err
	}
	var proof ports.FactorProof
	enrollment, err := repo.GetTOTPEnrollment(ctx, a.UserID)
	if err != nil {
		return err
	}
	if enrollment != nil && enrollment.Confirmed {
		proof, err = s.factorProof(ctx, repo, a.UserID, code)
		if err != nil {
			return err
		}
	}
	target, err := repo.GetTOTPEnrollment(ctx, targetUserID)
	if err != nil {
		return err
	}
	if target == nil {
		return domain.NotFound("no second factor enrolled")
	}
	atomic, err := s.atomicFactorRepo()
	if err != nil {
		return err
	}
	return atomic.ResetTOTPBound(ctx, a.UserID, operator.PasswordHash, proof, targetUserID, target.SealedSecret, a.Corr)
}

// openTOTPSecret decrypts a sealed secret with the instance key.
func (s *Services) openTOTPSecret(userID string, sealed []byte) (string, error) {
	if s.Seal == nil {
		return "", domain.Unsupported("secret sealing is unavailable")
	}
	var sec totpSecret
	if err := s.Seal.Open("totp:"+userID, sealed, &sec); err != nil {
		return "", domain.Unauthenticated("second-factor secret could not be read")
	}
	return sec.Secret, nil
}

func (s *Services) atomicFactorRepo() (ports.TOTPAtomicRepo, error) {
	r, ok := s.Users.(ports.TOTPAtomicRepo)
	if !ok {
		return nil, domain.Unsupported("atomic second-factor operations unavailable")
	}
	return r, nil
}

// Verify in services; defer consumption until the effect can commit atomically.
func (s *Services) factorProof(ctx context.Context, repo ports.TOTPRepo, user, code string) (ports.FactorProof, error) {
	if len(code) > 128 {
		return ports.FactorProof{}, domain.Unauthenticated("invalid code")
	}
	enr, err := repo.GetTOTPEnrollment(ctx, user)
	if err != nil {
		return ports.FactorProof{}, err
	}
	if enr == nil || !enr.Confirmed {
		return ports.FactorProof{}, domain.Conflict("second factor is not enrolled")
	}
	if err := reserveFactorVerification(ctx, repo, user); err != nil {
		return ports.FactorProof{}, err
	}
	proof := ports.FactorProof{SealedSecret: enr.SealedSecret}
	if len(strings.TrimSpace(code)) == 6 {
		secret, err := s.openTOTPSecret(user, enr.SealedSecret)
		if err != nil {
			return proof, err
		}
		step, ok := totpMatch(secret, code, time.Now())
		if !ok {
			return proof, domain.Unauthenticated("invalid code")
		}
		proof.Step = step
		return proof, nil
	}
	if len(normalizeRecoveryCode(code)) != 20 {
		return proof, domain.Unauthenticated("invalid code")
	}
	proof.RecoveryHash = hashRecoveryCode(code)
	return proof, nil
}

// LoginOutcome is the result of a password login: either a ready session or a
// pending second-factor challenge. Exactly one of SessionToken/ChallengeToken
// is set.
type LoginOutcome struct {
	User           *domain.User
	SessionToken   string
	SessionExpires time.Time
	ChallengeToken string
}

// LoginWithFactor verifies the password, then either issues a session (no
// confirmed factor) or creates an expiring, attempt-limited challenge (factor
// confirmed). The challenge is not a session and cannot authorize API calls.
func (s *Services) LoginWithFactor(ctx context.Context, email, password string) (*LoginOutcome, error) {
	if s.Cfg.AuthMode != "local" && s.Cfg.AuthMode != "mixed" {
		return nil, domain.Unsupported("local password login unavailable")
	}
	u, err := s.Login(ctx, email, password)
	if err != nil {
		return nil, err
	}
	repo, ok := s.totpRepo()
	if !ok {
		return s.issuePasswordSession(ctx, u)
	}
	enr, err := repo.GetTOTPEnrollment(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if enr == nil || !enr.Confirmed {
		return s.issuePasswordSession(ctx, u)
	}
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	if err := repo.CreateLoginChallenge(ctx, domain.NewID("chal"), u.ID, u.PasswordHash, hashToken(token), time.Now().Add(loginChallengeTTL), loginChallengeMaxAttempt); err != nil {
		return nil, err
	}
	return &LoginOutcome{User: u, ChallengeToken: token}, nil
}

func (s *Services) issuePasswordSession(ctx context.Context, u *domain.User) (*LoginOutcome, error) {
	repo, ok := s.Users.(ports.PasswordSessionRepo)
	if !ok {
		return nil, domain.Unsupported("password sessions unavailable")
	}
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	exp, err := s.sessionExpiry(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if err := repo.CreatePasswordSession(ctx, domain.NewID("ses"), u.ID, u.PasswordHash, hashToken(token), exp); err != nil {
		return nil, err
	}
	return &LoginOutcome{User: u, SessionToken: token, SessionExpires: exp}, nil
}

// CompleteChallengeWithTOTP finishes a pending challenge with a valid current
// code and issues the session. Attempts are counted before verification; factor consumption, challenge
// completion, session issuance and audit commit together.
func (s *Services) CompleteChallengeWithTOTP(ctx context.Context, challengeToken, code string) (*LoginOutcome, error) {
	repo, ok := s.totpRepo()
	if !ok {
		return nil, domain.Unsupported("second factor is not supported by this storage backend")
	}
	ch, ok, err := repo.BeginLoginChallengeAttempt(ctx, hashToken(challengeToken))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domain.Unauthenticated("challenge expired or attempts exhausted")
	}
	enr, err := repo.GetTOTPEnrollment(ctx, ch.UserID)
	if err != nil {
		return nil, err
	}
	if enr == nil || !enr.Confirmed {
		return nil, domain.Unauthenticated("second factor unavailable")
	}
	secret, err := s.openTOTPSecret(ch.UserID, enr.SealedSecret)
	if err != nil {
		return nil, err
	}
	step, ok := totpMatch(secret, code, time.Now())
	if !ok {
		return nil, domain.Unauthenticated("invalid code")
	}
	return s.finishChallenge(ctx, ch, ports.FactorProof{SealedSecret: enr.SealedSecret, Step: step})
}

// CompleteChallengeWithRecovery finishes a pending challenge with a single-use
// recovery code. The code, challenge, session and audit commit atomically.
func (s *Services) CompleteChallengeWithRecovery(ctx context.Context, challengeToken, recoveryCode string) (*LoginOutcome, error) {
	repo, ok := s.totpRepo()
	if !ok {
		return nil, domain.Unsupported("second factor is not supported by this storage backend")
	}
	ch, ok, err := repo.BeginLoginChallengeAttempt(ctx, hashToken(challengeToken))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domain.Unauthenticated("challenge expired or attempts exhausted")
	}
	if len(recoveryCode) > 128 || len(normalizeRecoveryCode(recoveryCode)) != 20 {
		return nil, domain.Unauthenticated("invalid recovery code")
	}
	enr, err := repo.GetTOTPEnrollment(ctx, ch.UserID)
	if err != nil {
		return nil, err
	}
	if enr == nil || !enr.Confirmed {
		return nil, domain.Unauthenticated("second factor unavailable")
	}
	return s.finishChallenge(ctx, ch, ports.FactorProof{SealedSecret: enr.SealedSecret, RecoveryHash: hashRecoveryCode(recoveryCode)})
}

func (s *Services) finishChallenge(ctx context.Context, ch *ports.LoginChallenge, proof ports.FactorProof) (*LoginOutcome, error) {
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	exp, err := s.sessionExpiry(ctx, ch.UserID)
	if err != nil {
		return nil, err
	}
	atomic, err := s.atomicFactorRepo()
	if err != nil {
		return nil, err
	}
	ok, err := atomic.CompleteChallengeBound(ctx, ch, proof, domain.NewID("ses"), hashToken(token), exp)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domain.Unauthenticated("challenge already completed")
	}
	u, err := s.Users.UserByID(ctx, ch.UserID)
	if err != nil {
		return nil, err
	}
	return &LoginOutcome{User: u, SessionToken: token, SessionExpires: exp}, nil
}

func reserveFactorVerification(ctx context.Context, repo ports.TOTPRepo, user string) error {
	allowed, err := repo.ReserveTOTPAttempt(ctx, user)
	if err != nil {
		return err
	}
	if !allowed {
		return domain.Unauthenticated("too many factor attempts; retry in five minutes")
	}
	return nil
}

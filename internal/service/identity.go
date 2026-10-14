package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

const sessionTTL = 30 * 24 * time.Hour

// LoginSession binds issuance to the hash actually verified by Login.
func (s *Services) LoginSession(ctx context.Context, email, password string) (*domain.User, string, time.Time, error) {
	u, err := s.Login(ctx, email, password)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	repo, ok := s.Users.(ports.PasswordSessionRepo)
	if !ok {
		return nil, "", time.Time{}, domain.Unsupported("password sessions unavailable")
	}
	token, err := randomToken(32)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	exp, err := s.sessionExpiry(ctx, u.ID)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	if err := repo.CreatePasswordSession(ctx, domain.NewID("ses"), u.ID, u.PasswordHash, hashToken(token), exp); err != nil {
		return nil, "", time.Time{}, err
	}
	return u, token, exp, nil
}

// Password hashing via the reviewed argon2id library with conservative
// parameters. There is no default admin password: local mode starts with
// an operator setup form and no-auth mode resolves one local principal.
func hashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

func verifyPassword(password, hash string) bool {
	if hash == "" {
		return false
	}
	ok, err := argon2id.ComparePasswordAndHash(password, hash)
	return err == nil && ok
}

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// randomHex renders n random bytes as lowercase hex; safe to embed in
// the "nsd_<keyid>_<secret>" format because hex never contains "_".
func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// NewSession issues an opaque revocable session for the user.
func (s *Services) NewSession(ctx context.Context, userID string) (token string, expiresAt time.Time, err error) {
	token, err = randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt, err = s.sessionExpiry(ctx, userID)
	if err != nil {
		return "", time.Time{}, err
	}
	if err := s.Users.CreateSession(ctx, domain.NewID("ses"), userID, hashToken(token), expiresAt); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (s *Services) ResolveSession(ctx context.Context, token string) (string, bool, error) {
	userID, _, ok, err := s.Users.SessionByTokenHash(ctx, hashToken(token))
	if err != nil || !ok {
		return "", false, err
	}
	return userID, true, nil
}

func (s *Services) Logout(ctx context.Context, token string) error {
	return s.Users.RevokeSessionByHash(ctx, hashToken(token))
}

// SetupAccount performs the first-run local bootstrap: with zero users,
// create the operator account. Never installs a default password.
func (s *Services) SetupAccount(ctx context.Context, email, password, displayName string) (*domain.User, error) {
	lock := s.docLock("instance-setup")
	lock.Lock()
	defer lock.Unlock()
	n, err := s.Users.UserCount(ctx)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, domain.Conflict("instance already configured")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return nil, domain.Invalid("a valid email is required")
	}
	if len(password) < 10 {
		return nil, domain.Invalid("password must be at least 10 characters")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &domain.User{
		ID: domain.NewID("usr"), DisplayName: displayName, Email: email,
		PasswordHash: hash, IsOperator: true, CreatedAt: time.Now(),
	}
	if err := s.Users.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// SignupAccount registers a regular (non-operator) local account while
// signups are open. The first account always goes through SetupAccount so
// the instance keeps exactly one bootstrap operator.
func (s *Services) SignupAccount(ctx context.Context, email, password, displayName string) (*domain.User, error) {
	if !s.Cfg.SignupEnabled {
		return nil, domain.Forbidden("signups are disabled on this instance")
	}
	lock := s.docLock("instance-setup")
	lock.Lock()
	defer lock.Unlock()
	n, err := s.Users.UserCount(ctx)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, domain.Conflict("instance setup required before signups")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return nil, domain.Invalid("a valid email is required")
	}
	if len(password) < 10 {
		return nil, domain.Invalid("password must be at least 10 characters")
	}
	if existing, err := s.Users.UserByEmail(ctx, email); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, domain.Conflict("an account with this email already exists")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &domain.User{
		ID: domain.NewID("usr"), DisplayName: displayName, Email: email,
		PasswordHash: hash, IsOperator: false, CreatedAt: time.Now(),
	}
	if err := s.Users.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	return s.ensureExternalVault(ctx, u)
}

// Login validates local credentials.
func (s *Services) Login(ctx context.Context, email, password string) (*domain.User, error) {
	u, err := s.Users.UserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return nil, err
	}
	// constant-ish failure shape; never reveal which half failed
	if u == nil || u.Disabled || !verifyPassword(password, u.PasswordHash) {
		return nil, domain.Unauthenticated("invalid email or password")
	}
	return u, nil
}

// EnsureNoAuthPrincipal creates (once) the sole principal used by
// explicit no-auth mode. It is not an isolation mechanism; see the
// Permission Matrix note on no-auth.
func (s *Services) EnsureNoAuthPrincipal(ctx context.Context) (*domain.User, error) {
	u, err := s.Users.SolePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if u != nil {
		return u, nil
	}
	u = &domain.User{
		ID: domain.NewID("usr"), DisplayName: "Local", Email: "",
		IsOperator: true, CreatedAt: time.Now(),
	}
	if err := s.Users.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	// give the sole principal a personal space
	sp, err := s.CreateSpace(ctx, &Actor{UserID: u.ID, Local: true}, "Personal", "personal")
	if err != nil {
		return nil, err
	}
	_ = sp
	return u, nil
}

// ---------- API keys ----------

// CreateKey mints a scoped key for the caller. The secret is returned
// exactly once and only its SHA-256 hash persists.
func (s *Services) CreateKey(ctx context.Context, a *Actor, label string, spaceIDs, scopes []string, expiresInDays int) (record *ports.APIKeyRecord, secret string, err error) {
	if a.Key != nil {
		return nil, "", domain.Forbidden("key management requires a human session")
	}
	label = strings.TrimSpace(label)
	if len(label) > 200 || expiresInDays < 0 || expiresInDays > 365 {
		return nil, "", domain.Invalid("label must be at most 200 characters and expiry between 1 and 365 days")
	}
	if label == "" {
		return nil, "", domain.Invalid("key label required")
	}
	userSpaces, _, err := s.Spaces.SpacesForUser(ctx, a.UserID)
	if err != nil {
		return nil, "", err
	}
	allowed := map[string]bool{}
	for _, sp := range userSpaces {
		allowed[sp.ID] = true
	}
	if len(spaceIDs) == 0 {
		for id := range allowed {
			spaceIDs = append(spaceIDs, id)
		}
	}
	for _, id := range spaceIDs {
		if !allowed[id] {
			return nil, "", domain.Forbidden("key may only be bound to spaces you belong to")
		}
	}
	for _, sc := range scopes {
		if !domain.ValidScope(sc) {
			return nil, "", domain.Invalid("unknown scope " + sc)
		}
	}
	secretBytes, err := randomHex(32)
	if err != nil {
		return nil, "", err
	}
	k := &ports.APIKeyRecord{
		ID: domain.NewID("key"), Label: label, PrincipalID: a.UserID,
		SecretHash: hashToken(secretBytes), Scopes: scopes, SpaceIDs: spaceIDs,
		CreatedAt: time.Now(),
	}
	if expiresInDays > 0 {
		exp := time.Now().AddDate(0, 0, expiresInDays)
		k.ExpiresAt = &exp
	} else {
		exp := time.Now().AddDate(0, 0, 90) // proposed default expiry
		k.ExpiresAt = &exp
	}
	if err := s.Users.CreateKey(ctx, k); err != nil {
		return nil, "", err
	}
	// nsd_<keyid>_<secret>: id is non-secret for routing, secret is entropy
	return k, "nsd_" + k.ID + "_" + secretBytes, nil
}

// AuthenticateKey resolves a bearer secret to its principal + grants.
func (s *Services) AuthenticateKey(ctx context.Context, bearer string) (*ports.APIKeyRecord, *domain.User, error) {
	bearer = strings.TrimSpace(bearer)
	if !strings.HasPrefix(bearer, "nsd_") {
		return nil, nil, domain.Unauthenticated("invalid key")
	}
	rest := strings.TrimPrefix(bearer, "nsd_")
	// the key id itself contains underscores (key_<ulid>); the secret is
	// hex, so the final underscore is the separator
	idx := strings.LastIndex(rest, "_")
	if idx <= 0 {
		return nil, nil, domain.Unauthenticated("invalid key")
	}
	keyID, secret := rest[:idx], rest[idx+1:]
	k, err := s.Users.KeyByID(ctx, keyID)
	if err != nil {
		return nil, nil, err
	}
	if k == nil || k.RevokedAt != nil || (k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt)) {
		return nil, nil, domain.Unauthenticated("key revoked or expired")
	}
	if subtle.ConstantTimeCompare([]byte(hashToken(secret)), []byte(k.SecretHash)) != 1 {
		return nil, nil, domain.Unauthenticated("invalid key")
	}
	u, err := s.Users.UserByID(ctx, k.PrincipalID)
	if err != nil || u == nil || u.Disabled {
		return nil, nil, domain.Unauthenticated("principal unavailable")
	}
	if k.LastUsedAt == nil || time.Since(*k.LastUsedAt) >= time.Minute {
		_ = s.Users.TouchKey(ctx, k.ID, time.Now())
	}
	return k, u, nil
}

func (s *Services) sessionExpiry(ctx context.Context, userID string) (time.Time, error) {
	expiry := time.Now().Add(sessionTTL)
	if repo, ok := s.Users.(ExternalIdentityRepo); ok {
		proof, e := repo.ExternalAuth(ctx, userID)
		if e != nil {
			return time.Time{}, e
		}
		if proof != nil {
			if !proof.ExpiresAt.After(time.Now()) {
				return time.Time{}, domain.Unauthenticated("sign in again")
			}
			if proof.ExpiresAt.Before(expiry) {
				expiry = proof.ExpiresAt
			}
		}
	}

	return expiry, nil
}

package ports

import (
	"context"
	"time"
)

// Sealer encrypts a secret at rest with the instance key and decrypts it
// with the same key. providers.Manager implements it; the service layer uses
// it as a capability so the sealing rules never leak into transport handlers.
type Sealer interface {
	Seal(id string, v any) ([]byte, error)
	Open(id string, b []byte, v any) error
}

// TOTPEnrollment is the stored second-factor state for one account. The
// shared secret is sealed; Confirmed=false means enrollment was started but
// the factor is not yet enforced, so a failed or abandoned enrollment never
// enables MFA.
type TOTPEnrollment struct {
	UserID       string
	SealedSecret []byte
	Confirmed    bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// LoginChallenge is an expiring, attempt-limited pending second-factor
// challenge created after a correct password. It authorizes only the
// challenge-completion endpoints: it is never a session cookie or an API
// credential, so it cannot authorize ordinary API operations.
type LoginChallenge struct {
	ID           string
	UserID       string
	ExpectedHash string // the verified password hash, for compare-and-swap
	Status       string // pending|consumed|failed|expired
	Attempts     int
	MaxAttempts  int
	ExpiresAt    time.Time
}

// TOTPRepo provides reads and independently counted login attempts.
// Factor consumption and authorized effects use TOTPAtomicRepo transactions.
type TOTPRepo interface {
	ReserveTOTPAttempt(ctx context.Context, userID string) (bool, error)
	GetTOTPEnrollment(ctx context.Context, userID string) (*TOTPEnrollment, error)
	CountRecoveryCodes(ctx context.Context, userID string) (int, error)
	CreateLoginChallenge(ctx context.Context, id, userID, expectedHash, tokenHash string, expiresAt time.Time, maxAttempts int) error
	BeginLoginChallengeAttempt(ctx context.Context, tokenHash string) (*LoginChallenge, bool, error)
}

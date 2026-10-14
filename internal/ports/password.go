package ports

import (
	"context"
	"time"
)

// PasswordChangeRepo compares the verified hash and commits password rotation,
// credential revocation and audit together. No changes commit on stale proof.
type PasswordChangeRepo interface {
	ChangePassword(ctx context.Context, userID, expectedHash, newHash, corr string) (sessionsRevoked, keysRevoked int, err error)
}

// PasswordSessionRepo prevents a stale password proof from issuing a session
// after rotation, including across separate service instances.
type PasswordSessionRepo interface {
	CreatePasswordSession(ctx context.Context, id, userID, expectedHash, tokenHash string, expiresAt time.Time) error
}

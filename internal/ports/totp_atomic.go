package ports

import (
	"context"
	"time"
)

// FactorProof binds a service-verified factor to its exact enrollment. The
// repository consumes the step/code in the same transaction as its effect.
type FactorProof struct {
	SealedSecret []byte
	Step         int64
	RecoveryHash string
}

type TOTPAtomicRepo interface {
	StartTOTPBound(ctx context.Context, userID, expectedHash string, sealedSecret []byte, codeHashes []string, correlation string) error
	ConfirmTOTPBound(ctx context.Context, userID string, proof FactorProof, correlation string) error
	ChangeTOTPBound(ctx context.Context, userID, expectedHash string, proof FactorProof, action string, codeHashes []string, correlation string) error
	CompleteChallengeBound(ctx context.Context, challenge *LoginChallenge, proof FactorProof, sessionID, tokenHash string, expiry time.Time) (bool, error)
	ResetTOTPBound(ctx context.Context, actorID, expectedActorHash string, actorProof FactorProof, targetID string, targetSealedSecret []byte, correlation string) error
}

type PasswordFactorRepo interface {
	ChangePasswordWithFactor(ctx context.Context, userID, expectedHash, newHash string, proof FactorProof, correlation string) (int, int, error)
}

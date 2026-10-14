package ports

import (
	"context"
	"time"
)

type VaultLoad struct {
	SpaceID   string    `json:"spaceId"`
	UserID    string    `json:"-"`
	Phase     string    `json:"phase"`
	Completed int       `json:"completed"`
	Total     int       `json:"total"`
	Skipped   int       `json:"skipped"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (l *VaultLoad) Active() bool {
	return l != nil && (l.Phase == "queued" || l.Phase == "scanning" || l.Phase == "indexing" || l.Phase == "resolving")
}

type VaultLoadRepo interface {
	SaveVaultLoad(context.Context, *VaultLoad) error
	VaultLoad(context.Context, string) (*VaultLoad, error)
	PendingVaultLoads(context.Context) ([]*VaultLoad, error)
}

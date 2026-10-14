package ports

import "time"

// Administration responses deliberately exclude password and token material.
type AdminUser struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	Email       string    `json:"email,omitempty"`
	Disabled    bool      `json:"disabled"`
	IsOperator  bool      `json:"isOperator"`
	RoleManaged bool      `json:"roleManaged"`
	CreatedAt   time.Time `json:"createdAt"`
}

type ExternalAuth struct {
	Issuer    string
	Claims    map[string]any
	ExpiresAt time.Time
}

type AdminChange struct {
	Disabled       *bool `json:"disabled,omitempty"`
	IsOperator     *bool `json:"isOperator,omitempty"`
	RevokeSessions bool  `json:"revokeSessions,omitempty"`
}

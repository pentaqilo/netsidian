package ports

import "context"

// StorageConfig describes a vault provider. Secret fields are accepted on writes
// only; adapters must redact them before returning configuration to a client.
type StorageConfig struct {
	Provider     string `json:"provider"`
	Bucket       string `json:"bucket,omitempty"`
	Region       string `json:"region,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
	Prefix       string `json:"prefix,omitempty"`
	PathStyle    bool   `json:"pathStyle"`
	Auth         string `json:"auth,omitempty"` // keys | session | oidc
	AccessKey    string `json:"accessKey,omitempty"`
	SecretKey    string `json:"secretKey,omitempty"`
	SessionToken string `json:"sessionToken,omitempty"`
	STSEndpoint  string `json:"stsEndpoint,omitempty"`
	RoleARN      string `json:"roleArn,omitempty"`
	OwnerID      string `json:"-"`
}

type StorageSecretRepo interface {
	VaultStorage(context.Context, string) ([]byte, error)
	// SaveVaultStorage commits the sealed profile and binding together.
	SaveVaultStorage(context.Context, string, []byte, string) error
	OAuthStorageToken(context.Context, string) ([]byte, error)
	SaveOAuthStorageToken(context.Context, string, []byte) error
}

type VaultStorageManager interface {
	Describe(context.Context, string) (*StorageConfig, error)
	Configure(context.Context, string, string, StorageConfig) error
	Providers() []string
}

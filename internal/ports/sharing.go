package ports

// SharingUser is the minimal account directory exposed to vault owners.
// Federated users enter this directory after their first successful login.
type SharingUser struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email,omitempty"`
}

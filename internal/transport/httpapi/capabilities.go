package httpapi

// RuntimeCapabilities is the common public contract for web and desktop UI.
// Capability flags describe configured behavior; authorization is still
// checked on every request. Credentials, folders and identity claims are absent.
type RuntimeCapabilities struct {
	Kind string `json:"kind"`
	Auth struct {
		PasswordLogin bool `json:"passwordLogin"`
		OAuth         bool `json:"oauth"`
		Logout        bool `json:"logout"`
	} `json:"auth"`
	Storage struct {
		DefaultProvider string   `json:"defaultProvider"`
		Providers       []string `json:"providers"`
		FolderPicker    string   `json:"folderPicker"`
	} `json:"storage"`
}

func (s *Server) executionTarget() string {
	if s.NativePick != nil {
		return "desktop"
	}
	return "web"
}
func (s *Server) runtimeCapabilities() RuntimeCapabilities {
	var v RuntimeCapabilities
	v.Kind = s.executionTarget()
	mode := s.Svc.Cfg.AuthMode
	v.Auth.PasswordLogin = mode == "local" || mode == "mixed"
	v.Auth.OAuth = s.OAuth != nil
	v.Auth.Logout = mode != "none"
	v.Storage.DefaultProvider = s.Svc.Cfg.StorageBinding
	if v.Storage.DefaultProvider == "" {
		v.Storage.DefaultProvider = "local-main"
	}
	v.Storage.Providers = s.Svc.StorageOptions()
	v.Storage.FolderPicker = "none"
	if s.NativePick != nil {
		v.Storage.FolderPicker = "native"
	} else if s.LocalFolderPick {
		v.Storage.FolderPicker = "server"
	}
	return v
}

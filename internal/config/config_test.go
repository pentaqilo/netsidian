package config

import "testing"

func TestSecurityConfigurationFailsClosed(t *testing.T) {
	for _, test := range []struct{ name, value string }{
		{"NETSIDIAN_SIGNUP_ENABLED", "maybe"},
		{"NETSIDIAN_OIDC_ALLOW_ALL_USERS", "yes"},
		{"NETSIDIAN_OIDC_ALLOWED_CLAIMS", `{"/groups":[]}`},
		{"NETSIDIAN_OIDC_ADMIN_CLAIMS", `{"groups":["admin"]}`},
		{"NETSIDIAN_OIDC_EMAIL_CLAIM", "profile.email"},
		{"NETSIDIAN_OIDC_NAME_CLAIM", "/bad~2pointer"},
		{"NETSIDIAN_OIDC_EMAIL_VERIFIED_CLAIM", "verified"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("NETSIDIAN_STATE_DIR", t.TempDir())
			t.Setenv(test.name, test.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid security configuration accepted")
			}
		})
	}
}

func TestExternalModeRequiresExplicitAdmissionAndAdministrators(t *testing.T) {
	t.Setenv("NETSIDIAN_STATE_DIR", t.TempDir())
	t.Setenv("NETSIDIAN_AUTH_MODE", "external")
	t.Setenv("NETSIDIAN_OIDC_ISSUER", "https://issuer.example")
	t.Setenv("NETSIDIAN_OIDC_CLIENT_ID", "netsidian")
	t.Setenv("NETSIDIAN_PUBLIC_URL", "https://notes.example")
	t.Setenv("NETSIDIAN_OIDC_ALLOWED_CLAIMS", "")
	t.Setenv("NETSIDIAN_OIDC_ADMIN_CLAIMS", "")
	t.Setenv("NETSIDIAN_OIDC_ALLOW_ALL_USERS", "")
	if _, err := Load(); err == nil {
		t.Fatal("external mode without admission policy started")
	}
	t.Setenv("NETSIDIAN_OIDC_ALLOWED_CLAIMS", `{"/groups":["members","admins"]}`)
	if _, err := Load(); err == nil {
		t.Fatal("external mode without admin policy started")
	}
	t.Setenv("NETSIDIAN_OIDC_ADMIN_CLAIMS", `{"/groups":["admins"]}`)
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETSIDIAN_OIDC_ALLOW_ALL_USERS", "true")
	if _, err := Load(); err == nil {
		t.Fatal("ambiguous admission configuration accepted")
	}
}

func TestNativeRuntimeOwnsItsLocalProfile(t *testing.T) {
	t.Setenv("NETSIDIAN_STATE_DIR", t.TempDir())
	t.Setenv("NETSIDIAN_AUTH_MODE", "external")
	t.Setenv("NETSIDIAN_BIND", "0.0.0.0")
	t.Setenv("NETSIDIAN_STORAGE_DRIVER", "s3")
	t.Setenv("NETSIDIAN_SIGNUP_ENABLED", "invalid inherited value")
	t.Setenv("NETSIDIAN_OIDC_ALLOWED_CLAIMS", "invalid inherited value")
	t.Setenv("NETSIDIAN_TRUSTED_PROXY", "true")
	t.Setenv("NETSIDIAN_LOCAL_FOLDER_PICK", "true")
	c, err := LoadFor("native")
	if err != nil {
		t.Fatal(err)
	}
	if c.InstanceKind != "native" || c.AuthMode != "none" || c.StorageDriver != "local" || c.BindHost != "127.0.0.1" || c.LocalFolderPick || c.TrustedProxy || c.SignupEnabled || c.OIDCIssuer != "" {
		t.Fatalf("wrong native profile: auth=%s storage=%s", c.AuthMode, c.StorageDriver)
	}
	if _, err = LoadFor("browser-supplied"); err == nil {
		t.Fatal("unknown runtime accepted")
	}
}

func TestServerRuntimeRejectsUnsafeConfiguration(t *testing.T) {
	for _, test := range []struct{ key, value string }{{"NETSIDIAN_PORT", "0"}, {"NETSIDIAN_PORT", "65536"}, {"NETSIDIAN_TRUSTED_PROXY", "maybe"}, {"NETSIDIAN_LOCAL_FOLDER_PICK", "maybe"}, {"NETSIDIAN_BIND", "0.0.0.0"}} {
		t.Run(test.key+test.value, func(t *testing.T) {
			t.Setenv("NETSIDIAN_STATE_DIR", t.TempDir())
			t.Setenv("NETSIDIAN_AUTH_MODE", "none")
			t.Setenv(test.key, test.value)
			if _, err := LoadFor("server"); err == nil {
				t.Fatal("unsafe server configuration accepted")
			}
		})
	}
}

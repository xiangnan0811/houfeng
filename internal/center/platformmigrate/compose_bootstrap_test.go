package platformmigrate

import (
	"errors"
	"testing"
)

func TestComposeBootstrapConfigRejectsUnsafeOrAmbiguousInputs(t *testing.T) {
	t.Parallel()

	valid := composeBootstrapConfigFixture()
	tests := []struct {
		name   string
		mutate func(*ComposeBootstrapConfig)
	}{
		{name: "database", mutate: func(config *ComposeBootstrapConfig) { config.DatabaseName = "" }},
		{name: "bootstrap reuse", mutate: func(config *ComposeBootstrapConfig) { config.BootstrapRole = config.Roles.Migrator }},
		{name: "authority role", mutate: func(config *ComposeBootstrapConfig) { config.AuthorityRole = "" }},
		{name: "authority reuses bootstrap", mutate: func(config *ComposeBootstrapConfig) { config.AuthorityRole = config.BootstrapRole }},
		{name: "authority reuses runtime", mutate: func(config *ComposeBootstrapConfig) { config.AuthorityRole = config.Roles.CenterRuntime }},
		{name: "duplicate app roles", mutate: func(config *ComposeBootstrapConfig) { config.Roles.PlatformAdmin = config.Roles.CenterRuntime }},
		{name: "empty runtime password", mutate: func(config *ComposeBootstrapConfig) { config.Passwords.Runtime = "" }},
		{name: "runtime password control", mutate: func(config *ComposeBootstrapConfig) { config.Passwords.Runtime = "secret\n" }},
		{name: "admin password control", mutate: func(config *ComposeBootstrapConfig) { config.Passwords.PlatformAdmin = "secret\x00tail" }},
		{name: "migrator password invalid UTF-8", mutate: func(config *ComposeBootstrapConfig) { config.Passwords.Migrator = string([]byte{0xff}) }},
		{name: "authority password control", mutate: func(config *ComposeBootstrapConfig) { config.Passwords.Authority = "secret\n" }},
		{name: "runtime and admin password reuse", mutate: func(config *ComposeBootstrapConfig) { config.Passwords.PlatformAdmin = config.Passwords.Runtime }},
		{name: "runtime and migrator password reuse", mutate: func(config *ComposeBootstrapConfig) { config.Passwords.Migrator = config.Passwords.Runtime }},
		{name: "runtime and authority password reuse", mutate: func(config *ComposeBootstrapConfig) { config.Passwords.Authority = config.Passwords.Runtime }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := valid
			tt.mutate(&config)
			if err := config.Validate(); !errors.Is(err, ErrInvalidComposeBootstrapConfig) {
				t.Fatalf("ComposeBootstrapConfig.Validate() error = %v, want %v", err, ErrInvalidComposeBootstrapConfig)
			}
		})
	}
}

func TestComposeBootstrapIdentityRejectsUnsupportedPostgresOrBootstrapDrift(t *testing.T) {
	t.Parallel()

	config := composeBootstrapConfigFixture()
	valid := composeBootstrapIdentity{
		ServerMajor:      16,
		SessionUser:      config.BootstrapRole,
		CurrentUser:      config.BootstrapRole,
		CurrentUserOID:   10,
		CurrentUserSuper: true,
		DatabaseName:     config.DatabaseName,
	}
	tests := []struct {
		name   string
		mutate func(*composeBootstrapIdentity)
	}{
		{name: "PostgreSQL 15", mutate: func(identity *composeBootstrapIdentity) { identity.ServerMajor = 15 }},
		{name: "PostgreSQL 17", mutate: func(identity *composeBootstrapIdentity) { identity.ServerMajor = 17 }},
		{name: "session user", mutate: func(identity *composeBootstrapIdentity) { identity.SessionUser = "other" }},
		{name: "current user", mutate: func(identity *composeBootstrapIdentity) { identity.CurrentUser = "other" }},
		{name: "OID", mutate: func(identity *composeBootstrapIdentity) { identity.CurrentUserOID = 11 }},
		{name: "not superuser", mutate: func(identity *composeBootstrapIdentity) { identity.CurrentUserSuper = false }},
		{name: "database", mutate: func(identity *composeBootstrapIdentity) { identity.DatabaseName = "other" }},
	}
	if err := validateComposeBootstrapIdentity(config, valid); err != nil {
		t.Fatalf("valid Compose bootstrap identity error = %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := valid
			tt.mutate(&identity)
			if err := validateComposeBootstrapIdentity(config, identity); err == nil {
				t.Fatal("Compose bootstrap identity drift was accepted")
			}
		})
	}
}

func composeBootstrapConfigFixture() ComposeBootstrapConfig {
	return ComposeBootstrapConfig{
		DatabaseName:  "houfeng",
		BootstrapRole: "postgres",
		AuthorityRole: "houfeng_records_authority",
		Roles: AppRoleSetV2{
			CenterRuntime:  "houfeng_runtime",
			PlatformAdmin:  "houfeng_platform_admin",
			DeletionWorker: "houfeng_record_deletion_worker",
			Migrator:       "houfeng_migrator",
		},
		Passwords: ComposeRolePasswords{
			Runtime:        "runtime-secret",
			PlatformAdmin:  "admin-secret",
			DeletionWorker: "deletion-worker-secret",
			Migrator:       "migrator-secret",
			Authority:      "authority-secret",
		},
	}
}

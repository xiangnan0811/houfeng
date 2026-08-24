package platformmigrate

import (
	"errors"
	"reflect"
	"strings"
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

func TestComposeBootstrapIncludesIsolatedRecordsAuthorityRole(t *testing.T) {
	t.Parallel()

	config := composeBootstrapConfigFixture()
	if got, want := config.roleNames(), []string{
		"houfeng_runtime",
		"houfeng_platform_admin",
		"houfeng_migrator",
		"houfeng_records_authority",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Compose bootstrap roles = %q, want %q", got, want)
	}
	if got, want := config.rolePasswords(), map[string]string{
		"houfeng_runtime":           "runtime-secret",
		"houfeng_platform_admin":    "admin-secret",
		"houfeng_migrator":          "migrator-secret",
		"houfeng_records_authority": "authority-secret",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Compose bootstrap password roles = %q, want %q", got, want)
	}
}

func TestComposeBootstrapSQLPreservesPostgres16PreR1Contract(t *testing.T) {
	t.Parallel()

	contract := composePreR1ValidateSQL + composePreR1RevokeSQL + composePreR1VerifySQL
	for _, required := range []string{
		"procedure.pronargs = 0",
		"procedure.proowner = 10",
		"OUT pg_control_version integer, OUT catalog_version_no integer, OUT system_identifier bigint, OUT pg_control_last_modified timestamp with time zone",
		"REVOKE EXECUTE ON FUNCTION pg_catalog.pg_control_system() FROM PUBLIC",
		"pg_catalog.aclexplode(procedure.proacl)",
		"acl_grant.grantor = procedure.proowner",
		"acl_grant.grantee = procedure.proowner",
		"NOT acl_grant.is_grantable",
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("Compose bootstrap pre-R1 SQL must contain %q", required)
		}
	}
	if strings.Contains(contract, "pg_get_function_identity_arguments(procedure.oid) = ''") {
		t.Fatal("Compose bootstrap must select pg_control_system() by zero input arguments, not empty formatted arguments")
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
		Roles: AppRoleSetV1{
			CenterRuntime: "houfeng_runtime",
			PlatformAdmin: "houfeng_platform_admin",
			Migrator:      "houfeng_migrator",
		},
		Passwords: ComposeRolePasswords{
			Runtime:       "runtime-secret",
			PlatformAdmin: "admin-secret",
			Migrator:      "migrator-secret",
			Authority:     "authority-secret",
		},
	}
}

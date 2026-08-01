package platformmigrate

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestLoadAppScopeConfigRejectsUnsafeRoleNameWithoutLeakingValue(t *testing.T) {
	const unsafeRole = "runtime\x00secret"
	_, err := LoadAppScopeConfig(func(key string) (string, bool) {
		switch key {
		case AppMigratorDatabaseURLEnv:
			return "postgres://migrator:app-only-secret@example.invalid/houfeng", true
		case AppRuntimeRoleEnv:
			return unsafeRole, true
		case AppAdminRoleEnv:
			return "houfeng_platform_admin", true
		default:
			return "", false
		}
	})
	if !errors.Is(err, ErrInvalidAppScopeConfig) {
		t.Fatalf("LoadAppScopeConfig() error = %v, want ErrInvalidAppScopeConfig", err)
	}
	if strings.Contains(err.Error(), unsafeRole) {
		t.Fatalf("LoadAppScopeConfig() error leaked role value: %q", err)
	}
}

func TestLoadAppScopeConfigRejectsMissingOrRepeatedRequiredValuesWithoutLeakingValues(t *testing.T) {
	const (
		migratorDSN = "postgres://migrator:migrator-secret@example.invalid/houfeng"
		runtimeRole = "houfeng_runtime_secret"
		adminRole   = "houfeng_admin_secret"
		sharedRole  = "houfeng_shared_role_secret"
	)

	for _, tc := range []struct {
		name        string
		databaseURL string
		runtimeRole string
		adminRole   string
	}{
		{
			name:        "empty_migrator_database_url",
			runtimeRole: runtimeRole,
			adminRole:   adminRole,
		},
		{
			name:        "empty_runtime_role",
			databaseURL: migratorDSN,
			adminRole:   adminRole,
		},
		{
			name:        "empty_admin_role",
			databaseURL: migratorDSN,
			runtimeRole: runtimeRole,
		},
		{
			name:        "runtime_admin_role_reuse",
			databaseURL: migratorDSN,
			runtimeRole: sharedRole,
			adminRole:   sharedRole,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadAppScopeConfig(func(key string) (string, bool) {
				switch key {
				case AppMigratorDatabaseURLEnv:
					return tc.databaseURL, true
				case AppRuntimeRoleEnv:
					return tc.runtimeRole, true
				case AppAdminRoleEnv:
					return tc.adminRole, true
				default:
					t.Fatalf("LoadAppScopeConfig() queried forbidden environment key %q", key)
					return "", false
				}
			})
			if !errors.Is(err, ErrInvalidAppScopeConfig) {
				t.Fatalf("LoadAppScopeConfig() error = %v, want ErrInvalidAppScopeConfig", err)
			}
			for _, value := range []string{tc.databaseURL, tc.runtimeRole, tc.adminRole} {
				if value != "" && strings.Contains(err.Error(), value) {
					t.Fatalf("LoadAppScopeConfig() error leaked input value %q: %q", value, err)
				}
			}
		})
	}
}

func TestLoadAppScopeConfigOnlyLoadsAllowedEnvironmentKeys(t *testing.T) {
	const migratorDSN = "postgres://migrator:app-only-secret@example.invalid/houfeng"
	var lookedUp []string
	config, err := LoadAppScopeConfig(func(key string) (string, bool) {
		lookedUp = append(lookedUp, key)
		switch key {
		case AppMigratorDatabaseURLEnv:
			return migratorDSN, true
		case AppRuntimeRoleEnv:
			return "houfeng_center_runtime", true
		case AppAdminRoleEnv:
			return "houfeng_platform_admin", true
		default:
			t.Fatalf("LoadAppScopeConfig() queried forbidden environment key %q", key)
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("LoadAppScopeConfig() error = %v", err)
	}
	if config.MigratorDatabaseURL != migratorDSN || config.RuntimeRole != "houfeng_center_runtime" || config.AdminRole != "houfeng_platform_admin" {
		t.Fatalf("LoadAppScopeConfig() = %#v, want the three APP-only values", config)
	}
	wantKeys := []string{AppMigratorDatabaseURLEnv, AppRuntimeRoleEnv, AppAdminRoleEnv}
	if !reflect.DeepEqual(lookedUp, wantKeys) {
		t.Fatalf("LoadAppScopeConfig() looked up %#v, want %#v", lookedUp, wantKeys)
	}
}

package platformmigrate

import (
	"strings"
	"testing"
)

func TestAppRoleSetV1ValidateRejectsReuseAndInvalidNames(t *testing.T) {
	valid := AppRoleSetV1{
		CenterRuntime: "houfeng_center_runtime",
		PlatformAdmin: "houfeng_platform_admin",
		Migrator:      "houfeng_platform_migrator",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid role set Validate() error = %v", err)
	}

	for name, roles := range map[string]AppRoleSetV1{
		"runtime_admin_reuse": {
			CenterRuntime: "same_role",
			PlatformAdmin: "same_role",
			Migrator:      "migrator",
		},
		"migrator_reuse": {
			CenterRuntime: "runtime",
			PlatformAdmin: "admin",
			Migrator:      "runtime",
		},
		"empty_role": {
			CenterRuntime: "runtime",
			PlatformAdmin: "",
			Migrator:      "migrator",
		},
		"control_character": {
			CenterRuntime: "runtime\x00role",
			PlatformAdmin: "admin",
			Migrator:      "migrator",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := roles.Validate()
			if err == nil || !strings.Contains(err.Error(), "role") {
				t.Fatalf("Validate() error = %v, want role contract rejection", err)
			}
		})
	}
}

func TestValidateDirectRolePreflightRequiresDirectMigratorAndConstrainedRoles(t *testing.T) {
	roles := AppRoleSetV1{
		CenterRuntime: "houfeng_center_runtime",
		PlatformAdmin: "houfeng_platform_admin",
		Migrator:      "houfeng_platform_migrator",
	}
	attributes := map[string]appRoleAttributes{
		roles.CenterRuntime: {CanLogin: true},
		roles.PlatformAdmin: {CanLogin: true},
		roles.Migrator:      {CanLogin: true},
	}

	if err := validateDirectRolePreflight(roles, roles.Migrator, roles.Migrator, attributes, nil); err != nil {
		t.Fatalf("validateDirectRolePreflight() valid roles error = %v", err)
	}

	for _, tc := range []struct {
		name        string
		sessionUser string
		currentUser string
		mutate      func(map[string]appRoleAttributes)
		membership  []appRoleMembership
		want        string
	}{
		{
			name:        "session_user_mismatch",
			sessionUser: "member_login",
			currentUser: roles.Migrator,
			want:        "session user",
		},
		{
			name:        "current_user_mismatch",
			sessionUser: roles.Migrator,
			currentUser: "other_migrator",
			want:        "current user",
		},
		{
			name: "runtime_cannot_login",
			mutate: func(values map[string]appRoleAttributes) {
				value := values[roles.CenterRuntime]
				value.CanLogin = false
				values[roles.CenterRuntime] = value
			},
			want: "LOGIN",
		},
		{
			name: "admin_inherits",
			mutate: func(values map[string]appRoleAttributes) {
				value := values[roles.PlatformAdmin]
				value.Inherit = true
				values[roles.PlatformAdmin] = value
			},
			want: "NOINHERIT",
		},
		{
			name: "migrator_superuser",
			mutate: func(values map[string]appRoleAttributes) {
				value := values[roles.Migrator]
				value.Superuser = true
				values[roles.Migrator] = value
			},
			want: "NOSUPERUSER",
		},
		{
			name: "runtime_can_create_database",
			mutate: func(values map[string]appRoleAttributes) {
				value := values[roles.CenterRuntime]
				value.CreateDB = true
				values[roles.CenterRuntime] = value
			},
			want: "NOCREATEDB",
		},
		{
			name: "admin_can_create_role",
			mutate: func(values map[string]appRoleAttributes) {
				value := values[roles.PlatformAdmin]
				value.CreateRole = true
				values[roles.PlatformAdmin] = value
			},
			want: "NOCREATEROLE",
		},
		{
			name: "migrator_can_replicate",
			mutate: func(values map[string]appRoleAttributes) {
				value := values[roles.Migrator]
				value.Replication = true
				values[roles.Migrator] = value
			},
			want: "NOREPLICATION",
		},
		{
			name: "runtime_bypasses_rls",
			mutate: func(values map[string]appRoleAttributes) {
				value := values[roles.CenterRuntime]
				value.BypassRLS = true
				values[roles.CenterRuntime] = value
			},
			want: "NOBYPASSRLS",
		},
		{
			name: "recursive_membership_touching_migrator",
			membership: []appRoleMembership{{
				MemberRole: "member_login",
				ParentRole: roles.Migrator,
			}},
			want: "membership",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotAttributes := make(map[string]appRoleAttributes, len(attributes))
			for name, value := range attributes {
				gotAttributes[name] = value
			}
			if tc.mutate != nil {
				tc.mutate(gotAttributes)
			}
			sessionUser := tc.sessionUser
			if sessionUser == "" {
				sessionUser = roles.Migrator
			}
			currentUser := tc.currentUser
			if currentUser == "" {
				currentUser = roles.Migrator
			}

			err := validateDirectRolePreflight(roles, sessionUser, currentUser, gotAttributes, tc.membership)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateDirectRolePreflight() error = %v, want %q rejection", err, tc.want)
			}
		})
	}
}

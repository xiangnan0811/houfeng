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

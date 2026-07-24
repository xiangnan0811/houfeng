package platformmigrate

import (
	"errors"
	"strings"
)

const (
	AppMigratorDatabaseURLEnv = "HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL"
	AppRuntimeRoleEnv         = "HOUFENG_RECORD_PLATFORM_APP_RUNTIME_ROLE"
	AppAdminRoleEnv           = "HOUFENG_RECORD_PLATFORM_APP_ADMIN_ROLE"
)

var ErrInvalidAppScopeConfig = errors.New("app migration configuration is invalid")

// AppScopeConfig contains the only environment-derived values accepted by the
// APP-scoped migrator command.
type AppScopeConfig struct {
	MigratorDatabaseURL string
	RuntimeRole         string
	AdminRole           string
}

// LoadAppScopeConfig reads only the three APP migrator inputs. It deliberately
// does not use the shared center configuration loader because that loader can
// resolve unrelated domain configuration and secret files.
func LoadAppScopeConfig(lookup func(string) (string, bool)) (AppScopeConfig, error) {
	if lookup == nil {
		return AppScopeConfig{}, ErrInvalidAppScopeConfig
	}

	databaseURL, databaseURLOK := lookup(AppMigratorDatabaseURLEnv)
	runtimeRole, runtimeRoleOK := lookup(AppRuntimeRoleEnv)
	adminRole, adminRoleOK := lookup(AppAdminRoleEnv)

	config := AppScopeConfig{
		MigratorDatabaseURL: strings.TrimSpace(databaseURL),
		RuntimeRole:         strings.TrimSpace(runtimeRole),
		AdminRole:           strings.TrimSpace(adminRole),
	}
	if !databaseURLOK || !runtimeRoleOK || !adminRoleOK ||
		config.MigratorDatabaseURL == "" || config.RuntimeRole == "" || config.AdminRole == "" ||
		config.RuntimeRole == config.AdminRole ||
		!validAppRoleName(config.RuntimeRole) || !validAppRoleName(config.AdminRole) {
		return AppScopeConfig{}, ErrInvalidAppScopeConfig
	}
	return config, nil
}

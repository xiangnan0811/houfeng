package platformmigrate

import (
	"errors"
	"strings"
)

const (
	AppMigratorDatabaseURLEnv = "HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL"
	AppRuntimeRoleEnv         = "HOUFENG_RECORD_PLATFORM_APP_RUNTIME_ROLE"
	AppAdminRoleEnv           = "HOUFENG_RECORD_PLATFORM_APP_ADMIN_ROLE"
	AppDeletionWorkerRoleEnv  = "HOUFENG_RECORD_PLATFORM_APP_DELETION_WORKER_ROLE"
)

var ErrInvalidAppScopeConfig = errors.New("app migration configuration is invalid")

// AppScopeConfig contains the only environment-derived values accepted by the
// APP-scoped migrator command.
type AppScopeConfig struct {
	MigratorDatabaseURL string
	RuntimeRole         string
	AdminRole           string
	DeletionWorkerRole  string
}

// LoadAppScopeConfig reads only the four APP migrator inputs. It deliberately
// does not use the shared center configuration loader because that loader can
// resolve unrelated domain configuration and secret files.
func LoadAppScopeConfig(lookup func(string) (string, bool)) (AppScopeConfig, error) {
	if lookup == nil {
		return AppScopeConfig{}, ErrInvalidAppScopeConfig
	}

	databaseURL, databaseURLOK := lookup(AppMigratorDatabaseURLEnv)
	runtimeRole, runtimeRoleOK := lookup(AppRuntimeRoleEnv)
	adminRole, adminRoleOK := lookup(AppAdminRoleEnv)
	workerRole, workerRoleOK := lookup(AppDeletionWorkerRoleEnv)

	config := AppScopeConfig{
		MigratorDatabaseURL: strings.TrimSpace(databaseURL),
		RuntimeRole:         strings.TrimSpace(runtimeRole),
		AdminRole:           strings.TrimSpace(adminRole),
		DeletionWorkerRole:  strings.TrimSpace(workerRole),
	}
	if !databaseURLOK || !runtimeRoleOK || !adminRoleOK || !workerRoleOK ||
		config.MigratorDatabaseURL == "" || config.RuntimeRole == "" || config.AdminRole == "" || config.DeletionWorkerRole == "" ||
		config.RuntimeRole == config.AdminRole || config.DeletionWorkerRole == config.RuntimeRole || config.DeletionWorkerRole == config.AdminRole ||
		!validAppRoleName(config.RuntimeRole) || !validAppRoleName(config.AdminRole) || !validAppRoleName(config.DeletionWorkerRole) {
		return AppScopeConfig{}, ErrInvalidAppScopeConfig
	}
	return config, nil
}

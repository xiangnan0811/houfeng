package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
)

var ErrComposeSecretStage = errors.New("Compose secret staging failed")

// StageComposeSecrets verifies the current source credential set before publishing
// any of the supplied secrets to the private service staging directories.
func StageComposeSecrets(ctx context.Context, config ComposeInitConfig) error {
	if config.Validate() != nil {
		return ErrComposeSecretStage
	}
	pool, err := openComposePostgres(ctx, composePostgresEndpoint{
		Host: config.DatabaseHost, Port: config.DatabasePort, Database: config.DatabaseName,
		Role: config.BootstrapRole, Password: config.Passwords.Bootstrap,
	})
	if err != nil || pool == nil {
		if pool != nil {
			pool.Close()
		}
		return ErrComposeSecretStage
	}
	defer pool.Close()
	if err := WithSourceMaintenance(ctx, pool, SourceMaintenanceConfigPath, false, func(maintenance *SourceMaintenanceConfig) error {
		state, stateErr := recordauthority.LoadComposeState(config.AuthorityStateRoot)
		if stateErr != nil && !errors.Is(stateErr, recordauthority.ErrComposeStateAbsent) {
			return ErrComposeSecretStage
		}
		passwords := platformmigrate.ComposeRolePasswords{
			Runtime: config.Passwords.Runtime, PlatformAdmin: config.Passwords.PlatformAdmin,
			DeletionWorker: config.Passwords.DeletionWorker, Migrator: config.Passwords.Migrator,
		}
		if stateErr == nil {
			passwords.Authority = state.DatabasePassword()
		}
		if err := VerifySourceMaintenanceCredentials(ctx, pool, maintenance, config.Roles, config.AuthorityRole, passwords); err != nil {
			return ErrComposeSecretStage
		}
		files := []struct {
			path     string
			password string
			mode     os.FileMode
			group    int
		}{
			{"/state/secrets/db-init/postgres_bootstrap_password", config.Passwords.Bootstrap, 0400, 10002},
			{"/state/secrets/db-init/houfeng_runtime_password", config.Passwords.Runtime, 0400, 10002},
			{"/state/secrets/db-init/houfeng_platform_admin_password", config.Passwords.PlatformAdmin, 0400, 10002},
			{"/state/secrets/db-init/houfeng_record_deletion_worker_password", config.Passwords.DeletionWorker, 0400, 10002},
			{"/state/secrets/db-init/houfeng_migrator_password", config.Passwords.Migrator, 0400, 10002},
			{"/state/secrets/processor/houfeng_runtime_password", config.Passwords.Runtime, 0040, 10001},
		}
		for _, file := range files {
			if err := publishComposeSecret(file.path, file.password, file.mode, file.group); err != nil {
				return ErrComposeSecretStage
			}
		}
		return nil
	}); err != nil {
		return ErrComposeSecretStage
	}
	return nil
}

func publishComposeSecret(path, password string, mode os.FileMode, group int) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".secret-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	defer file.Close()
	if err := file.Chown(-1, group); err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.WriteString(password); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

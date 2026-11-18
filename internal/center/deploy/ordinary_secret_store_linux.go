//go:build linux

package deploy

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var ordinarySecretNames = [...]string{"postgres_bootstrap_password", "houfeng_runtime_password", "houfeng_platform_admin_password", "houfeng_record_deletion_worker_password", "houfeng_migrator_password", "houfeng_initial_password", "houfeng_session_hmac_key"}

// This is an explicit root-private persistent secret store, not an operation
// journal. It is retained while recreated containers may reference the files.
func ordinaryStageSecretFiles(intent ordinaryUpgradeIntent) (string, error) {
	if ordinaryValidateIntent(intent) != nil || os.Getuid() != 0 || os.Geteuid() != 0 {
		return "", errOrdinaryUpgrade
	}
	_, identities, err := ordinaryReadUpgradeCredentials(intent.Request)
	if err != nil {
		return "", errOrdinaryUpgrade
	}
	recipe, err := ordinaryCaptureComposeRecipe(intent.Request.WorkingDirectory)
	if err != nil || !ordinaryEnvironmentMetadataMatches(recipe.Environment, intent.Recipe.Environment) {
		return "", errOrdinaryUpgrade
	}
	commitment, err := ordinaryEnsureSecretCommitments(intent.Request, identities, recipe.Environment.SHA256, false)
	if err != nil || commitment != intent.SecretCommitmentSHA256 {
		return "", errOrdinaryUpgrade
	}
	maintenance := filepath.Join(intent.Request.WorkingDirectory, "maintenance")
	dirs, err := ordinaryOpenPrivateDirectories(maintenance, "ordinary-secrets", intent.Request.OperationID, true)
	if err != nil {
		return "", errOrdinaryUpgrade
	}
	defer dirs.Close()
	parent := dirs.operationFD
	store := filepath.Join(maintenance, "ordinary-secrets", intent.Request.OperationID)
	if !ordinarySecretStoreEntriesKnown(parent) {
		return "", errOrdinaryUpgrade
	}
	for i, name := range ordinarySecretNames {
		body, identity, err := readOrdinaryPrivateIdentity(intent.Secrets[i].Path, 64<<10)
		if err != nil || identity != identities[i] || !ordinarySecretMetadataMatches(identity, intent.Secrets[i]) {
			clear(body)
			return "", errOrdinaryUpgrade
		}
		err = ordinaryStageOneSecret(parent, name, body)
		clear(body)
		if err != nil || !dirs.unchanged() {
			return "", errOrdinaryUpgrade
		}
	}
	return store, nil
}

func ordinarySecretStoreEntriesKnown(parent int) bool {
	fd, err := unix.Openat(parent, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	file := os.NewFile(uintptr(fd), "ordinary secret store")
	names, err := file.Readdirnames(-1)
	_ = file.Close()
	if err != nil {
		return false
	}
	for _, name := range names {
		known := false
		if name == "secret-commitments.json" || name == ".pending-secret-commitments.json" {
			continue
		}
		for _, fixed := range ordinarySecretNames {
			if name == fixed || name == ".pending-"+fixed {
				known = true
				break
			}
		}
		if !known {
			return false
		}
	}
	return true
}

func ordinaryPrivateDirectoryMatches(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Uid == 0 && stat.Gid == 0 && stat.Mode&0o7777 == 0o700 && stat.Nlink != 0
}

func ordinaryPrivateMkdir(parent int, name string) error {
	created := false
	if err := unix.Mkdirat(parent, name, 0o700); err == nil {
		created = true
	} else if !errors.Is(err, unix.EEXIST) {
		return errOrdinaryUpgrade
	}
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(fd)
	if created && (unix.Fchown(fd, 0, 0) != nil || unix.Fchmod(fd, 0o700) != nil) {
		return errOrdinaryUpgrade
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || !ordinaryPrivateDirectoryMatches(stat) || unix.Fsync(fd) != nil || unix.Fsync(parent) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryStageOneSecret(parent int, name string, expected []byte) error {
	temp := ".pending-" + name
	// This exact private staging name is reserved for this fixed secret. An
	// interrupted partial cannot have been mounted by the Compose override.
	var stale unix.Stat_t
	if err := unix.Fstatat(parent, temp, &stale, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if stale.Mode&unix.S_IFMT != unix.S_IFREG || stale.Uid != 0 || stale.Gid != 0 || stale.Nlink != 1 || (stale.Mode&0o7777 != 0o444 && stale.Mode&0o7777&^0o600 != 0) || unix.Unlinkat(parent, temp, 0) != nil || unix.Fsync(parent) != nil {
			return errOrdinaryUpgrade
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return errOrdinaryUpgrade
	}
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
	if err == nil {
		file := os.NewFile(uintptr(fd), "ordinary staged secret")
		defer file.Close()
		var before, after, named unix.Stat_t
		if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0o7777 != 0o444 || before.Uid != 0 || before.Gid != 0 || before.Nlink != 1 || before.Size != int64(len(expected)) {
			return errOrdinaryUpgrade
		}
		body, err := io.ReadAll(io.LimitReader(file, int64(len(expected))+1))
		valid := err == nil && bytes.Equal(body, expected) && unix.Fstat(fd, &after) == nil && unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW) == nil && sameSourceFile(before, after) && sameSourceFile(before, named)
		clear(body)
		if !valid || unix.Fsync(parent) != nil {
			return errOrdinaryUpgrade
		}
		return nil
	}
	if !errors.Is(err, unix.ENOENT) {
		return errOrdinaryUpgrade
	}
	fd, err = unix.Openat2(parent, temp, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0o600, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary secret staging")
	defer file.Close()
	if n, err := file.Write(expected); err != nil || n != len(expected) || file.Sync() != nil || unix.Fchmod(fd, 0o444) != nil || file.Sync() != nil || unix.Fsync(parent) != nil || unix.Renameat2(parent, temp, parent, name, unix.RENAME_NOREPLACE) != nil || unix.Fsync(parent) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

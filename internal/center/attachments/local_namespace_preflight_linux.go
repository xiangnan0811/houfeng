//go:build linux

package attachments

import (
	"context"
	"errors"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// PreflightLocalNamespace applies the provisioning filesystem rules without
// creating a directory, lock, marker, database row, or namespace identity.
// Root is the actual host path; existing, when non-nil, must use that same path.
func PreflightLocalNamespace(ctx context.Context, request LocalProvisionRequest, existing *LocalNamespace) error {
	if ctx == nil || !validLocalUUID(request.NamespaceID) || !validLocalUUID(request.IncarnationID) || !validLocalUUID(request.RequestID) || request.SourceRecoveryID != nil {
		return ErrInvalidBlobStoreConfig
	}
	known := existing != nil
	if known && (existing.NamespaceID != request.NamespaceID || existing.IncarnationID != request.IncarnationID || existing.RequestID != request.RequestID || existing.RootPath != request.Root || existing.SourceRecoveryID != nil) {
		return ErrLocalIdentityChanged
	}
	files := emptyLocalDescriptors()
	defer files.close()
	root, err := openLocalRoot(request.Root, false, request.OwnerUID, request.OwnerGID)
	if errors.Is(err, unix.ENOENT) && !known {
		parent, openErr := unix.Openat2(unix.AT_FDCWD, filepath.Dir(request.Root), &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_NO_SYMLINKS})
		if openErr != nil {
			return openErr
		}
		defer unix.Close(parent)
		var stat unix.Stat_t
		if unix.Fstat(parent, &stat) != nil || stat.Mode&0o022 != 0 || (stat.Uid != request.OwnerUID && stat.Uid != 0) {
			return ErrLocalIdentityChanged
		}
		return nil
	}
	if err != nil {
		return err
	}
	files.root = root
	lock, lockErr := openLocalAt(root, localProvisionLockName, unix.O_RDWR, 0)
	if lockErr == nil {
		files.provision = lock
		if err := localFlock(ctx, lock, unix.LOCK_SH, nil); err != nil {
			return err
		}
		stat, err := localStat(lock, unix.S_IFREG, request.OwnerUID, request.OwnerGID, 0o600)
		if err != nil || stat.Nlink != 1 {
			return ErrLocalIdentityChanged
		}
		var named unix.Stat_t
		if unix.Fstatat(root, localProvisionLockName, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || named.Dev != stat.Dev || named.Ino != stat.Ino {
			return ErrLocalIdentityChanged
		}
		data, err := localReadFile(lock, 36)
		if err != nil || (len(data) != 0 && !validLocalUUID(string(data))) || (known && len(data) == 0) {
			return ErrLocalIdentityChanged
		}
	} else if !errors.Is(lockErr, unix.ENOENT) || known {
		return ErrLocalIdentityChanged
	}
	marker, _, _, markerErr := readLocalMarker(root, request.OwnerUID, request.OwnerGID)
	if errors.Is(markerErr, unix.ENOENT) {
		if known {
			return ErrLocalIdentityChanged
		}
		return localValidateUnboundEntries(root, false, true)
	}
	if markerErr != nil || marker.NamespaceID != request.NamespaceID || marker.IncarnationID != request.IncarnationID || marker.RequestID != request.RequestID || files.provision < 0 {
		return ErrLocalIdentityChanged
	}
	lockStat, err := localStat(files.provision, unix.S_IFREG, request.OwnerUID, request.OwnerGID, 0o600)
	lockNonce, nonceErr := localReadFile(files.provision, 36)
	if err != nil || nonceErr != nil || !validLocalUUID(string(lockNonce)) || lockStat.Ino != marker.ProvisionLockInode || string(lockNonce) != marker.ProvisionLockNonce {
		return ErrLocalIdentityChanged
	}
	if !known {
		if err := localValidateUnboundEntries(root, true, true); err != nil {
			return err
		}
	}
	for _, entry := range []struct {
		name string
		fd   *int
	}{{"sha256", &files.digest}, {".locks", &files.locks}} {
		fd, err := openLocalAt(root, entry.name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if errors.Is(err, unix.ENOENT) && !known {
			continue
		}
		if err != nil {
			return err
		}
		*entry.fd = fd
		if _, err := localStat(fd, unix.S_IFDIR, request.OwnerUID, request.OwnerGID, 0o700); err != nil {
			return err
		}
		if !known {
			names, err := localDirectoryNames(fd)
			if err != nil || len(names) != 0 {
				return errors.Join(ErrLocalNamespaceClosed, err)
			}
		}
	}
	if known {
		physical, err := inspectLocalNamespace(request.Root, files, request.OwnerUID, request.OwnerGID)
		if err != nil || !sameLocalNamespaceIdentity(*existing, physical) {
			return errors.Join(ErrLocalIdentityChanged, err)
		}
	}
	return nil
}

// FreshLocalNamespaceRoot checks the unbound root before any identity or lock
// is published. Its entry policy is shared with the real provisioner.
func FreshLocalNamespaceRoot(path string, uid, gid uint32) bool {
	fd, err := openLocalRoot(path, false, uid, gid)
	if errors.Is(err, unix.ENOENT) {
		return true
	}
	if err != nil {
		return false
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	return unix.Fstat(fd, &stat) == nil && stat.Nlink != 0 && localValidateUnboundEntries(fd, false, false) == nil
}

// ColdReleasedLocalNamespaceRoot requires the existing released empty skeleton.
// Fresh installations continue to use FreshLocalNamespaceRoot.
func ColdReleasedLocalNamespaceRoot(path string, uid, gid uint32) bool {
	fd, err := openLocalRoot(path, false, uid, gid)
	if err != nil {
		return false
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Nlink == 0 || localValidateUnboundEntries(fd, false, false) != nil {
		return false
	}
	names, err := localDirectoryNames(fd)
	return err == nil && len(names) == 1 && names[0] == "sha256"
}

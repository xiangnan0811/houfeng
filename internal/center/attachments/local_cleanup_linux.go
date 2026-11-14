//go:build linux

package attachments

import (
	"context"
	"encoding/hex"
	"errors"

	"golang.org/x/sys/unix"
)

func (store *LocalBlobStore) inspectCleanupFinal(ctx context.Context, guard *LocalKeyGuard, snapshot LocalCleanupSnapshot) (*string, uint64, uint64, error) {
	var version *string
	var device, inode uint64
	err := guard.run(snapshot.Task.Key, func() error {
		if guard.store != store {
			return ErrLocalGuardExpired
		}
		if err := store.validateIdentity(ctx); err != nil {
			return err
		}
		fd, err := openLocalAt(store.files.digest, snapshot.Task.Key[7:], unix.O_RDONLY, 0)
		if errors.Is(err, unix.ENOENT) {
			if snapshot.Referenced {
				return ErrBlobNotFound
			}
			return nil
		}
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		stat, err := localStat(fd, unix.S_IFREG, store.namespace.OwnerUID, store.namespace.OwnerGID, 0o600)
		if err != nil || stat.Dev != store.namespace.Device {
			return ErrLocalIdentityChanged
		}
		var attribute [128]byte
		n, err := unix.Fgetxattr(fd, localMaterializationAttribute, attribute[:])
		if err != nil {
			return errors.Join(ErrBlobVersionMismatch, err)
		}
		actual := string(attribute[:n])
		var materialization *WriteObligation
		for index := range snapshot.Obligations {
			o := &snapshot.Obligations[index]
			if "local-v2-"+o.ID == actual && o.DispatchedAt != nil {
				materialization = o
				break
			}
		}
		if materialization == nil {
			return ErrBlobVersionMismatch
		}
		digest, err := hex.DecodeString(materialization.SHA256)
		if err != nil || len(digest) != 32 {
			return ErrLocalIdentityChanged
		}
		expected := ObjectVersion{Key: snapshot.Task.Key, VersionID: actual, SizeBytes: materialization.Size}
		copy(expected.SHA256[:], digest)
		verified, _, err := store.openVerified(ctx, expected, true)
		if err != nil {
			return err
		}
		defer verified.Close()
		var checked unix.Stat_t
		if err := unix.Fstat(int(verified.Fd()), &checked); err != nil || stat.Ino != checked.Ino || stat.Dev != checked.Dev {
			return ErrLocalIdentityChanged
		}
		version, device, inode = &actual, stat.Dev, stat.Ino
		return nil
	})
	return version, device, inode, err
}

func (store *LocalBlobStore) cleanupFinal(ctx context.Context, guard *LocalKeyGuard, snapshot LocalCleanupSnapshot) (string, error) {
	var outcome string
	err := guard.run(snapshot.Task.Key, func() error {
		task := snapshot.Task
		if guard.store != store || !task.FinalIdentityBound || task.FinalWasPresent == nil {
			return ErrLocalIdentityChanged
		}
		if err := store.validateIdentity(ctx); err != nil {
			return err
		}
		if !*task.FinalWasPresent {
			var stat unix.Stat_t
			err := unix.Fstatat(store.files.digest, task.Key[7:], &stat, unix.AT_SYMLINK_NOFOLLOW)
			if !errors.Is(err, unix.ENOENT) {
				return errors.Join(ErrBlobVersionMismatch, err)
			}
			if snapshot.Referenced {
				return ErrBlobNotFound
			}
			outcome = "already_absent"
			return unix.Fsync(store.files.digest)
		}
		if task.Version == nil || task.Size == nil || task.FinalDevice == nil || task.FinalInode == nil {
			return ErrLocalIdentityChanged
		}
		digest, err := hex.DecodeString(task.Key[7:])
		if err != nil || len(digest) != 32 {
			return ErrLocalIdentityChanged
		}
		version := ObjectVersion{Key: task.Key, VersionID: *task.Version, SizeBytes: *task.Size}
		copy(version.SHA256[:], digest)
		file, _, err := store.openVerified(ctx, version, true)
		if errors.Is(err, ErrBlobNotFound) {
			if snapshot.Referenced {
				return err
			}
			outcome = "already_absent"
			return unix.Fsync(store.files.digest)
		}
		if err != nil {
			return err
		}
		defer file.Close()
		var held, named unix.Stat_t
		if err := unix.Fstat(int(file.Fd()), &held); err != nil {
			return err
		}
		if held.Dev != *task.FinalDevice || held.Ino != *task.FinalInode {
			return ErrLocalIdentityChanged
		}
		if err := unix.Fstatat(store.files.digest, task.Key[7:], &named, unix.AT_SYMLINK_NOFOLLOW); err != nil || named.Ino != held.Ino || named.Dev != held.Dev {
			return ErrLocalIdentityChanged
		}
		if snapshot.Referenced {
			outcome = "retained_referenced"
			return unix.Fsync(store.files.digest)
		}
		if err := unix.Unlinkat(store.files.digest, task.Key[7:], 0); err != nil {
			return err
		}
		if err := unix.Fsync(store.files.digest); err != nil {
			return err
		}
		if err := store.runCutpoint(localBlobAfterDelete); err != nil {
			return err
		}
		outcome = "deleted"
		return nil
	})
	return outcome, err
}

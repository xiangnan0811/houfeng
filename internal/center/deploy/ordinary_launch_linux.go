//go:build linux

package deploy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/recordbirth"
)

// replaceOrdinaryComposeBirthLaunchPair is only safe while the launch registry
// is closed and the caller holds the exclusive ordinary management guard. Both
// predecessor and target must already be bound into the durable intent.
// A cut between the two renames is replayed using the same pair of bindings.
func replaceOrdinaryComposeBirthLaunchPair(directory string, predecessor, target composeBirthLaunchPair) error {
	if os.Getuid() != composeLaunchPublisherUID || os.Geteuid() != composeLaunchPublisherUID {
		return errComposeBirthLaunch
	}
	directories, err := openComposeLaunchDirectories(directory)
	if err != nil {
		return errComposeBirthLaunch
	}
	defer func() {
		for _, dir := range directories {
			_ = unix.Close(dir.fd)
		}
	}()
	type item struct {
		name, kind            string
		old, next             recordbirth.LaunchBinding
		body                  []byte
		oldDigest, nextDigest [32]byte
	}
	items := []item{
		{composeCenterBirthLaunchFile, "center", predecessor.center, target.center, target.centerBytes, predecessor.centerDigest, target.centerDigest},
		{composeProcessorBirthLaunchFile, "content-processor", predecessor.processor, target.processor, target.processorBytes, predecessor.processorDigest, target.processorDigest},
	}
	// Reject unknown state for either member before changing either pathname.
	for _, v := range items {
		if _, err := ordinaryLaunchState(directories, directory, v.name, v.kind, v.old, v.next, v.oldDigest, v.nextDigest); err != nil {
			return errComposeBirthLaunch
		}
		encoded, err := recordbirth.EncodeLaunchBinding(v.next)
		if err != nil || string(encoded) != string(v.body) || sha256.Sum256(encoded) != v.nextDigest {
			return errComposeBirthLaunch
		}
	}
	for _, v := range items {
		isTarget, err := ordinaryLaunchState(directories, directory, v.name, v.kind, v.old, v.next, v.oldDigest, v.nextDigest)
		if err != nil {
			return errComposeBirthLaunch
		}
		if isTarget {
			continue
		}
		if err := replaceOrdinaryLaunchFile(directories, directory, v.name, v.kind, v.old, v.next, v.body, v.oldDigest, v.nextDigest); err != nil {
			return errComposeBirthLaunch
		}
	}
	if unix.Fsync(directories[len(directories)-1].fd) != nil || !composeLaunchDirectoriesUnchanged(directories) {
		return errComposeBirthLaunch
	}
	return nil
}

func ordinaryLaunchState(directories []composeLaunchDirectory, directory, name, kind string, old, next recordbirth.LaunchBinding, oldDigest, nextDigest [32]byte) (bool, error) {
	if !composeLaunchDirectoriesUnchanged(directories) {
		return false, errComposeBirthLaunch
	}
	value, digest, err := recordbirth.ReadPublishedLaunchBinding(filepath.Join(directory, name), kind)
	if err != nil || !composeLaunchDirectoriesUnchanged(directories) {
		return false, errComposeBirthLaunch
	}
	if value == next && digest == nextDigest {
		return true, nil
	}
	if value == old && digest == oldDigest {
		return false, nil
	}
	return false, errComposeBirthLaunch
}

func replaceOrdinaryLaunchFile(directories []composeLaunchDirectory, directory, name, kind string, old, next recordbirth.LaunchBinding, body []byte, oldDigest, nextDigest [32]byte) error {
	parent := directories[len(directories)-1].fd
	var original unix.Stat_t
	if unix.Fstatat(parent, name, &original, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return errComposeBirthLaunch
	}
	// Reuse the existing protected publisher to create an independently checked
	// temporary pathname, then atomically exchange it with the old binding.
	temp, err := ordinaryLaunchTempName()
	if err != nil {
		return errComposeBirthLaunch
	}
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = unix.Unlinkat(parent, temp, 0)
		}
	}()
	if err := publishComposeBirthLaunchFile(directories, directory, temp, kind, next, body, nextDigest); err != nil {
		return errComposeBirthLaunch
	}
	isTarget, err := ordinaryLaunchState(directories, directory, name, kind, old, next, oldDigest, nextDigest)
	if err != nil || isTarget {
		return errComposeBirthLaunch
	}
	var current unix.Stat_t
	if unix.Fstatat(parent, name, &current, unix.AT_SYMLINK_NOFOLLOW) != nil || current.Dev != original.Dev || current.Ino != original.Ino || !composeLaunchDirectoriesUnchanged(directories) {
		return errComposeBirthLaunch
	}
	if err := unix.Renameat2(parent, temp, parent, name, unix.RENAME_EXCHANGE); err != nil {
		return errComposeBirthLaunch
	}
	removeTemp = false
	// The exchanged predecessor is removed only when it is the original inode.
	var exchanged unix.Stat_t
	if unix.Fstatat(parent, temp, &exchanged, unix.AT_SYMLINK_NOFOLLOW) != nil || exchanged.Dev != original.Dev || exchanged.Ino != original.Ino || unix.Fsync(parent) != nil {
		return errComposeBirthLaunch
	}
	if err := unix.Unlinkat(parent, temp, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return errComposeBirthLaunch
	}
	if unix.Fsync(parent) != nil {
		return errComposeBirthLaunch
	}
	isTarget, err = ordinaryLaunchState(directories, directory, name, kind, old, next, oldDigest, nextDigest)
	if err != nil || !isTarget {
		return errComposeBirthLaunch
	}
	return nil
}

func ordinaryLaunchTempName() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return ".ordinary-launch-" + hex.EncodeToString(nonce[:]), nil
}

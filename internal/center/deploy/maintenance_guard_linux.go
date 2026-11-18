//go:build linux

package deploy

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const maintenanceGuardFilename = "maintenance.lock"

// ProvisionMaintenanceGuard explicitly creates a private directory (its parent
// must exist) and the permanent maintenance.lock if absent. It never unlinks,
// replaces, truncates, or repairs a nonempty guard. An empty file can be the
// result of interrupted provisioning; only this explicit call initializes it.
func ProvisionMaintenanceGuard(directory string) (MaintenanceGuardIdentity, error) {
	var identity MaintenanceGuardIdentity
	path, dirFD, err := openMaintenanceGuardDirectory(directory, true)
	if err != nil {
		return identity, err
	}
	defer unix.Close(dirFD)
	fd, err := openMaintenanceGuardFile(dirFD, true)
	if err != nil {
		return identity, err
	}
	defer unix.Close(fd)
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		return identity, maintenanceGuardError("lock provisioning file", err)
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	stat, err := maintenanceGuardFileStat(fd)
	if err != nil {
		return identity, err
	}
	if stat.Size == 0 {
		var nonce [32]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return identity, fmt.Errorf("generate maintenance guard nonce: %w", err)
		}
		written := 0
		for written < len(nonce) {
			n, err := unix.Pwrite(fd, nonce[written:], int64(written))
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil {
				return identity, maintenanceGuardError("write provisioning nonce", err)
			}
			if n == 0 {
				return identity, maintenanceGuardError("write provisioning nonce", io.ErrShortWrite)
			}
			written += n
		}
	}
	// Sync even on exact replay: an earlier provisioner may have written the
	// complete nonce but died before syncing the file or its directory entry.
	if err := unix.Fsync(fd); err != nil {
		return identity, maintenanceGuardError("sync provisioning file", err)
	}
	if err := unix.Fsync(dirFD); err != nil {
		return identity, maintenanceGuardError("sync provisioning directory", err)
	}
	identity, err = readMaintenanceGuardIdentity(dirFD, fd)
	if err != nil {
		return MaintenanceGuardIdentity{}, err
	}
	if err := validateMaintenanceGuardPath(path, dirFD, fd, identity); err != nil {
		return MaintenanceGuardIdentity{}, err
	}
	return identity, nil
}

// WithMaintenanceGuard holds a shared normal-operation/heartbeat lock or an
// exclusive recovery lock throughout fn. Runtime never provisions anything.
// Cancellation interrupts waiting, not an already executing callback; fn owns
// cancellation of its own work. Each call opens a distinct file description so
// concurrent calls cannot release or convert one another's locks.
func WithMaintenanceGuard(ctx context.Context, directory string, expected MaintenanceGuardIdentity, exclusive bool, fn func() error) error {
	if fn == nil {
		return maintenanceGuardError("invalid invocation", unix.EINVAL)
	}
	return withMaintenanceGuardFD(ctx, directory, expected, exclusive, func(int) error { return fn() })
}

// withMaintenanceGuardFD exposes the held description only to the trusted
// maintenance coordinator. A supervised child may retain a duplicate; each
// holder closes its descriptor without unlocking the shared description.
func withMaintenanceGuardFD(ctx context.Context, directory string, expected MaintenanceGuardIdentity, exclusive bool, fn func(int) error) error {
	if ctx == nil || fn == nil {
		return maintenanceGuardError("invalid invocation", unix.EINVAL)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path, dirFD, err := openMaintenanceGuardDirectory(directory, false)
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	fd, err := openMaintenanceGuardFile(dirFD, false)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := lockMaintenanceGuard(ctx, fd, exclusive); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateMaintenanceGuardPath(path, dirFD, fd, expected); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(fd)
}

func maintenanceGuardError(action string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrMaintenanceGuardMismatch, action, err)
}

const maintenanceGuardResolve = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV

func openMaintenanceGuardDirectory(directory string, provision bool) (string, int, error) {
	if directory == "" || strings.ContainsRune(directory, '\x00') {
		return "", -1, maintenanceGuardError("invalid directory", unix.EINVAL)
	}
	path, err := filepath.Abs(directory)
	if err != nil || path == string(filepath.Separator) {
		return "", -1, maintenanceGuardError("invalid directory", unix.EINVAL)
	}
	// The containing filesystem may itself be mounted (e.g. /var or /tmp).
	// Resolve its parent without symlinks, then prohibit mount crossings in
	// both the controlled directory and the lock entry beneath that anchor.
	parentFD, err := unix.Openat2(unix.AT_FDCWD, filepath.Dir(path), &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return "", -1, maintenanceGuardError("open parent directory", err)
	}
	defer unix.Close(parentFD)
	name := filepath.Base(path)
	fd, err := unix.Openat2(parentFD, name, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC),
		Resolve: maintenanceGuardResolve,
	})
	if errors.Is(err, unix.ENOENT) && provision {
		if err := unix.Mkdirat(parentFD, name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return "", -1, maintenanceGuardError("create directory", err)
		}
		if err := unix.Fsync(parentFD); err != nil {
			return "", -1, maintenanceGuardError("sync parent directory", err)
		}
		fd, err = unix.Openat2(parentFD, name, &unix.OpenHow{
			Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC),
			Resolve: maintenanceGuardResolve,
		})
	}
	if err != nil {
		return "", -1, maintenanceGuardError("open directory", err)
	}
	if _, err := maintenanceGuardDirectoryStat(fd); err != nil {
		_ = unix.Close(fd)
		return "", -1, err
	}
	return path, fd, nil
}

func openMaintenanceGuardFile(dirFD int, provision bool) (int, error) {
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
	mode := uint64(0)
	if provision {
		flags = unix.O_RDWR | unix.O_CREAT | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
		mode = 0o600
	}
	fd, err := unix.Openat2(dirFD, maintenanceGuardFilename, &unix.OpenHow{
		Flags: uint64(flags), Mode: mode, Resolve: maintenanceGuardResolve,
	})
	if err != nil {
		return -1, maintenanceGuardError("open lock file", err)
	}
	if _, err := maintenanceGuardFileStat(fd); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func maintenanceGuardDirectoryStat(fd int) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return stat, maintenanceGuardError("stat directory", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o022 != 0 || stat.Nlink == 0 {
		return stat, maintenanceGuardError("unsafe directory", unix.EPERM)
	}
	return stat, nil
}

func maintenanceGuardFileStat(fd int) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return stat, maintenanceGuardError("stat lock file", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o7777 != 0o600 || stat.Nlink != 1 {
		return stat, maintenanceGuardError("unsafe lock file", unix.EPERM)
	}
	return stat, nil
}

func readMaintenanceGuardIdentity(dirFD, fd int) (MaintenanceGuardIdentity, error) {
	var identity MaintenanceGuardIdentity
	dir, err := maintenanceGuardDirectoryStat(dirFD)
	if err != nil {
		return identity, err
	}
	file, err := maintenanceGuardFileStat(fd)
	if err != nil {
		return identity, err
	}
	if file.Size != int64(len(identity.Nonce)) {
		return identity, maintenanceGuardError("invalid nonce length", unix.EINVAL)
	}
	// Read one extra byte so a concurrent extension cannot masquerade as a
	// valid nonce. Controlled owners must also honor the flock protocol.
	var data [33]byte
	n, err := unix.Pread(fd, data[:], 0)
	for errors.Is(err, unix.EINTR) {
		n, err = unix.Pread(fd, data[:], 0)
	}
	if err != nil || n != len(identity.Nonce) {
		return identity, maintenanceGuardError("read nonce", io.ErrUnexpectedEOF)
	}
	identity.DirectoryDevice = uint64(dir.Dev)
	identity.DirectoryInode = dir.Ino
	identity.FileDevice = uint64(file.Dev)
	identity.FileInode = file.Ino
	copy(identity.Nonce[:], data[:len(identity.Nonce)])
	return identity, nil
}

func validateMaintenanceGuardPath(path string, dirFD, fd int, expected MaintenanceGuardIdentity) error {
	actual, err := readMaintenanceGuardIdentity(dirFD, fd)
	if err != nil {
		return err
	}
	if actual != expected {
		return maintenanceGuardError("anchored identity changed", unix.ESTALE)
	}
	_, currentDirFD, err := openMaintenanceGuardDirectory(path, false)
	if err != nil {
		return err
	}
	defer unix.Close(currentDirFD)
	currentFD, err := openMaintenanceGuardFile(currentDirFD, false)
	if err != nil {
		return err
	}
	defer unix.Close(currentFD)
	current, err := readMaintenanceGuardIdentity(currentDirFD, currentFD)
	if err != nil {
		return err
	}
	if current != expected {
		return maintenanceGuardError("pathname identity changed", unix.ESTALE)
	}
	return nil
}

func lockMaintenanceGuard(ctx context.Context, fd int, exclusive bool) error {
	operation := unix.LOCK_SH | unix.LOCK_NB
	if exclusive {
		operation = unix.LOCK_EX | unix.LOCK_NB
	}
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := unix.Flock(fd, operation)
		if err == nil {
			return nil
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return maintenanceGuardError("acquire lock", err)
		}
		if timer == nil {
			timer = time.NewTimer(10 * time.Millisecond)
		} else {
			timer.Reset(10 * time.Millisecond)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}

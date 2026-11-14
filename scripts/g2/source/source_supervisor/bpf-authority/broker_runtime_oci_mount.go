//go:build linux && amd64 && !cgo

package main

import (
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/sys/unix"
)

// runtimeMountModeStage classifies only the checked path's shape and mode bits.
func runtimeMountModeStage(path string, mode uint32) string {
	const volumePrefix = runtimeDockerDataRoot + "/volumes/"
	const containerPrefix = runtimeDockerDataRoot + "/containers/"
	if strings.HasPrefix(path, volumePrefix) {
		rest := path[len(volumePrefix):]
		if len(rest) == 64+len("/_data") && rest[64:] == "/_data" && runtimeContainerID(rest[:64]) {
			if mode&07777 == 01777 {
				return "ADMIT_MOUNT_MODE_VOLUME_01777"
			}
			return "ADMIT_MOUNT_MODE_VOLUME_OTHER_WRITABLE"
		}
	}
	if strings.HasPrefix(path, containerPrefix) {
		rest := path[len(containerPrefix):]
		if len(rest) > 64 && runtimeContainerID(rest[:64]) {
			switch rest[64:] {
			case "/hostname":
				return "ADMIT_MOUNT_MODE_HOSTNAME"
			case "/hosts":
				return "ADMIT_MOUNT_MODE_HOSTS"
			case "/resolv.conf":
				return "ADMIT_MOUNT_MODE_RESOLVCONF"
			}
		}
	}
	return "ADMIT_MOUNT_MODE"
}

// runtimeMountIdentity checks the named object through no-follow traversal.
// The requested bind sources have already been fixed by the admitted template.
func runtimeMountIdentity(path string, diagnostic *admitDiagnostic) error {
	if !runtimePath(path) {
		diagnostic.reject("ADMIT_MOUNT_PATH", errGuardProtocol)
		return errGuardProtocol
	}
	parent := filepath.Dir(path)
	dir, err := runtimeDirectory(parent, false)
	if err != nil {
		diagnostic.reject("ADMIT_MOUNT_PARENT", err)
		return err
	}
	defer unix.Close(dir)
	leaf := filepath.Base(path)
	fd, err := unix.Openat2(dir, leaf, &unix.OpenHow{Flags: unix.O_PATH | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		diagnostic.reject("ADMIT_MOUNT_OPEN", err)
		return err
	}
	defer unix.Close(fd)
	var held, named unix.Stat_t
	if err := unix.Fstat(fd, &held); err != nil {
		diagnostic.reject("ADMIT_MOUNT_FSTAT", err)
		return errGuardProtocol
	}
	if err := unix.Fstatat(dir, leaf, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		diagnostic.reject("ADMIT_MOUNT_FSTATAT", err)
		return errGuardProtocol
	}
	if !sameTrustedStat(held, named) {
		diagnostic.reject("ADMIT_MOUNT_IDENTITY", errGuardProtocol)
		return errGuardProtocol
	}
	if held.Mode&unix.S_IFMT != unix.S_IFDIR && held.Mode&unix.S_IFMT != unix.S_IFREG {
		diagnostic.reject("ADMIT_MOUNT_TYPE", errGuardProtocol)
		return errGuardProtocol
	}
	if held.Mode&0022 != 0 {
		if diagnostic != nil && diagnostic.observed == nil {
			diagnostic.reject(runtimeMountModeStage(path, held.Mode), errGuardProtocol)
		}
		return errGuardProtocol
	}
	return nil
}

func runtimeMounts(spec map[string]any, grant *brokerGrant) error {
	expected, err := expectedRuntimeSpec(nil, grant)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(spec["mounts"], expected["mounts"]) {
		return errGuardProtocol
	}
	return verifyMountAndRootfsIdentities(grant.facts, nil)
}

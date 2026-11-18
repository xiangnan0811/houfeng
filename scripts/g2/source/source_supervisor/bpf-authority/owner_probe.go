//go:build linux && amd64 && !cgo

package main

import (
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const postgresImageID = "sha256:23d19d971a56051f2509bb6e20efb9a96e225a336ce8174ad78c3e5d050a3610"
const ownerProbeSourcePath = "/run/g2/postgres-owner-probe"
const ownerProbeTargetPath = "/var/lib/postgresql/data"

type ownerProbeSourceIdentity struct {
	stat    unix.Stat_t
	mountID int
}

func ownerProbeSourceStatValid(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFDIR && st.Uid == 0 && st.Gid == 0 && st.Mode&07777 == 0500
}

func readOwnerProbeSourceIdentity() (ownerProbeSourceIdentity, error) {
	var identity ownerProbeSourceIdentity
	parent, err := runtimeDirectory("/run/g2", false)
	if err != nil {
		return identity, err
	}
	defer unix.Close(parent)
	parentMount, err := fdMountID(parent)
	if err != nil || parentMount <= 0 {
		return identity, errGuardProtocol
	}
	how := &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	}
	name := filepath.Base(ownerProbeSourcePath)
	fd, err := unix.Openat2(parent, name, how)
	if err != nil {
		return identity, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return identity, errGuardProtocol
	}
	defer file.Close()
	if err = unix.Fstat(fd, &identity.stat); err != nil {
		return ownerProbeSourceIdentity{}, err
	}
	identity.mountID, err = fdMountID(fd)
	if err != nil || identity.mountID <= 0 || identity.mountID != parentMount || !ownerProbeSourceStatValid(identity.stat) {
		return ownerProbeSourceIdentity{}, errGuardProtocol
	}
	entries, err := file.ReadDir(1)
	if err != io.EOF || len(entries) != 0 {
		return ownerProbeSourceIdentity{}, errGuardProtocol
	}
	var after unix.Stat_t
	if err = unix.Fstat(fd, &after); err != nil {
		return ownerProbeSourceIdentity{}, err
	}
	afterMount, err := fdMountID(fd)
	if err != nil || afterMount != identity.mountID || !sameTrustedStat(identity.stat, after) {
		return ownerProbeSourceIdentity{}, errGuardProtocol
	}
	named, err := unix.Openat2(parent, name, how)
	if err != nil {
		return ownerProbeSourceIdentity{}, err
	}
	defer unix.Close(named)
	var namedStat unix.Stat_t
	if err = unix.Fstat(named, &namedStat); err != nil {
		return ownerProbeSourceIdentity{}, err
	}
	namedMount, err := fdMountID(named)
	if err != nil || namedMount != identity.mountID || !sameTrustedStat(identity.stat, namedStat) {
		return ownerProbeSourceIdentity{}, errGuardProtocol
	}
	parentAfter, err := fdMountID(parent)
	if err != nil || parentAfter != parentMount {
		return ownerProbeSourceIdentity{}, errGuardProtocol
	}
	return identity, nil
}

// ownerProbeConfig returns the only authorized create request for the two
// fixed image-owner probes. Attach is opened before start because the pinned
// daemon uses the none logging driver.
func ownerProbeConfig(purpose string) (map[string]any, error) {
	flag := ""
	switch purpose {
	case "postgres-uid":
		flag = "-u"
	case "postgres-gid":
		flag = "-g"
	default:
		return nil, errControlFrame
	}
	return map[string]any{
		"Image":        postgresImageID,
		"Entrypoint":   []any{"/usr/bin/id"},
		"Cmd":          []any{flag, "postgres"},
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          false,
		"HostConfig": map[string]any{
			"NetworkMode":    "none",
			"ReadonlyRootfs": true,
			"Privileged":     false,
			"CapDrop":        []any{"ALL"},
			"SecurityOpt":    []any{"no-new-privileges"},
			"RestartPolicy":  map[string]any{"Name": "no"},
			"LogConfig":      map[string]any{"Type": "none", "Config": map[string]any{}},
			"Mounts":         []any{bind(ownerProbeSourcePath, ownerProbeTargetPath, true)},
		},
	}, nil
}

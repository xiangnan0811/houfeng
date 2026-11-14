//go:build linux && amd64 && !cgo

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

type bootstrapExecutable struct{ Package, Version, Path, SHA256 string }
type bootstrapManifest struct {
	SourceInputsSHA256, KernelSHA256, OriginalInitrdSHA256   string
	BrokerSHA256, GuardSHA256, PolicySHA256, GeneratorSHA256 string
	ObserverBindingsSHA256                                   string
	Scripts                                                  map[string]string
	Executables                                              map[string]bootstrapExecutable
}

func exactKeys(m map[string]any, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := m[key]; !ok {
			return false
		}
	}
	return true
}

func digestString(v any) (string, bool) {
	s, ok := v.(string)
	if !ok || len(s) != 64 || s == strings.Repeat("0", 64) {
		return "", false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", false
		}
	}
	return s, true
}

func manifestText(v any, max int) (string, bool) {
	s, ok := v.(string)
	if !ok || len(s) == 0 || len(s) > max {
		return "", false
	}
	for _, c := range s {
		if c < 0x21 || c > 0x7e {
			return "", false
		}
	}
	return s, true
}

func parseBootstrapManifest(raw []byte) (bootstrapManifest, error) {
	var out bootstrapManifest
	m, err := parseCanonicalControl(raw)
	if err != nil {
		return out, err
	}
	if !exactKeys(m, "version", "kernel_release", "source_inputs_sha256", "kernel_sha256", "original_initrd_sha256", "broker_sha256", "guard_sha256", "policy_sha256", "generator_sha256", "observer_bindings_sha256", "scripts", "package_executables") || m["version"] != json.Number("1") || m["kernel_release"] != "6.8.0-147-generic" {
		return out, errControlFrame
	}
	destinations := []*string{&out.SourceInputsSHA256, &out.KernelSHA256, &out.OriginalInitrdSHA256, &out.BrokerSHA256, &out.GuardSHA256, &out.PolicySHA256, &out.GeneratorSHA256, &out.ObserverBindingsSHA256}
	keys := []string{"source_inputs_sha256", "kernel_sha256", "original_initrd_sha256", "broker_sha256", "guard_sha256", "policy_sha256", "generator_sha256", "observer_bindings_sha256"}
	for i, key := range keys {
		value, ok := digestString(m[key])
		if !ok {
			return bootstrapManifest{}, errControlFrame
		}
		*destinations[i] = value
	}
	scripts, ok := m["scripts"].(map[string]any)
	if !ok || !exactKeys(scripts, "source_guest", "reboot_observer") {
		return bootstrapManifest{}, errControlFrame
	}
	out.Scripts = make(map[string]string, 2)
	for k, v := range scripts {
		value, ok := digestString(v)
		if !ok {
			return bootstrapManifest{}, errControlFrame
		}
		out.Scripts[k] = value
	}
	programs, ok := m["package_executables"].(map[string]any)
	if !ok || !exactKeys(programs, "python", "runc", "dockerd", "containerd", "shim", "systemd") {
		return bootstrapManifest{}, errControlFrame
	}
	out.Executables = make(map[string]bootstrapExecutable, len(programs))
	for role, item := range programs {
		p, ok := item.(map[string]any)
		if !ok || !exactKeys(p, "package", "version", "path", "sha256") {
			return bootstrapManifest{}, errControlFrame
		}
		pkg, pkgOK := manifestText(p["package"], 256)
		version, verOK := manifestText(p["version"], 256)
		executable, pathOK := manifestText(p["path"], 4096)
		hash, hashOK := digestString(p["sha256"])
		if !pkgOK || !verOK || !pathOK || !hashOK || !strings.HasPrefix(executable, "/usr/") || path.Clean(executable) != executable {
			return bootstrapManifest{}, errControlFrame
		}
		out.Executables[role] = bootstrapExecutable{pkg, version, executable, hash}
	}
	if systemd := out.Executables["systemd"]; systemd.Package != "systemd" || systemd.Path != "/usr/lib/systemd/systemd" {
		return bootstrapManifest{}, errControlFrame
	}
	return out, nil
}

// openTrustedRegular resolves every path component without following symlinks
// and holds the exact regular file while it is hashed and used. Root ownership
// and write restrictions are enforced on the file and on each ancestor.
func openTrustedRegular(name string, maxBytes int64) (*os.File, string, error) {
	root, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", err
	}
	defer unix.Close(root)
	return openTrustedRegularAt(root, name, maxBytes)
}

// openTrustedRegularAt resolves the absolute name beneath an already held root.
// The caller retains ownership of rootFD.
func openTrustedRegularAt(rootFD int, name string, maxBytes int64) (*os.File, string, error) {
	if rootFD < 0 || !strings.HasPrefix(name, "/") || path.Clean(name) != name || maxBytes <= 0 {
		return nil, "", errGuardProtocol
	}
	root, err := unix.FcntlInt(uintptr(rootFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, "", err
	}
	var rootStat unix.Stat_t
	if err = unix.Fstat(root, &rootStat); err != nil || rootStat.Uid != 0 || rootStat.Gid != 0 || rootStat.Mode&0022 != 0 || rootStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(root)
		return nil, "", errGuardProtocol
	}
	type ancestor struct {
		parent, fd int
		name       string
		stat       unix.Stat_t
	}
	var ancestors []ancestor
	defer func() {
		for _, entry := range ancestors {
			_ = unix.Close(entry.fd)
		}
		_ = unix.Close(root)
	}()
	parts := strings.Split(strings.TrimPrefix(name, "/"), "/")
	current := root
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, "", errGuardProtocol
		}
		flags := uint64(unix.O_PATH | unix.O_NOFOLLOW | unix.O_CLOEXEC)
		if i != len(parts)-1 {
			flags |= unix.O_DIRECTORY
		} else {
			flags = unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC
		}
		fd, e := unix.Openat2(current, part, &unix.OpenHow{Flags: flags, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
		if e != nil {
			return nil, "", e
		}
		var st unix.Stat_t
		if e = unix.Fstat(fd, &st); e != nil || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 || (i != len(parts)-1 && st.Mode&unix.S_IFMT != unix.S_IFDIR) {
			_ = unix.Close(fd)
			if e != nil {
				return nil, "", e
			}
			return nil, "", errGuardProtocol
		}
		if i != len(parts)-1 {
			ancestors = append(ancestors, ancestor{current, fd, part, st})
			current = fd
			continue
		}
		if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size <= 0 || st.Size > maxBytes {
			_ = unix.Close(fd)
			return nil, "", errGuardProtocol
		}
		file := os.NewFile(uintptr(fd), name)
		if file == nil {
			_ = unix.Close(fd)
			return nil, "", errGuardProtocol
		}
		h := sha256.New()
		n, e := io.Copy(h, io.LimitReader(file, maxBytes+1))
		var after, named unix.Stat_t
		if e != nil || n != st.Size || unix.Fstat(fd, &after) != nil || unix.Fstatat(current, part, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(st, after) || !sameTrustedStat(st, named) {
			_ = file.Close()
			return nil, "", fmt.Errorf("BPF_AUTH_UNAVAILABLE: executable identity changed")
		}
		var rootAfter unix.Stat_t
		if unix.Fstat(root, &rootAfter) != nil || !sameTrustedStat(rootStat, rootAfter) {
			_ = file.Close()
			return nil, "", errGuardProtocol
		}
		for _, entry := range ancestors {
			var held, namedDir unix.Stat_t
			if unix.Fstat(entry.fd, &held) != nil || unix.Fstatat(entry.parent, entry.name, &namedDir, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(entry.stat, held) || !sameTrustedStat(entry.stat, namedDir) {
				_ = file.Close()
				return nil, "", errGuardProtocol
			}
		}
		if _, e = file.Seek(0, io.SeekStart); e != nil {
			_ = file.Close()
			return nil, "", e
		}
		return file, hex.EncodeToString(h.Sum(nil)), nil
	}
	return nil, "", errGuardProtocol
}

func sameTrustedStat(a, b unix.Stat_t) bool {
	return trustedStatDifference(a, b) == runtimeStatSame
}

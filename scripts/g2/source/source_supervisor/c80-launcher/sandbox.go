//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func consumePermit(dir int, permit launchPermit) error {
	name := fmt.Sprintf("launch-used-%020d", permit.PhaseGeneration)
	fd, err := unix.Openat(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return denied
	}
	defer unix.Close(fd)
	// The launcher starts with GID 10002; explicitly make the marker root:root.
	if unix.Fchown(fd, 0, 0) != nil || unix.Fchmod(fd, 0600) != nil {
		return denied
	}
	body, err := json.Marshal(struct {
		ConfigDigest    string `json:"config_digest"`
		CoordinatorPID  int    `json:"coordinator_pid"`
		LauncherPID     int    `json:"launcher_pid"`
		PhaseGeneration uint64 `json:"phase_generation"`
		RunToken        string `json:"run_token"`
		Version         int    `json:"version"`
	}{permit.ConfigDigest, permit.Coordinator.PID, os.Getpid(), permit.PhaseGeneration, runToken, 1})
	if err != nil || len(body) == 0 || len(body) > maxFrame {
		return denied
	}
	for offset := 0; offset < len(body); {
		n, err := unix.Write(fd, body[offset:])
		if err == unix.EINTR {
			continue
		}
		if err != nil || n <= 0 {
			return denied
		}
		offset += n
	}
	if unix.Fsync(fd) != nil || unix.Fsync(dir) != nil {
		return denied
	}
	check, err := readPrivateAt(dir, name, 0, 0)
	if err != nil || string(check) != string(body) {
		return denied
	}
	return nil
}

func joinChildCgroup(pin cgroupIdentity) error {
	dir, err := openDirectory(childCgroupPath, true)
	if err != nil {
		return denied
	}
	defer unix.Close(dir)
	var info unix.Stat_t
	if unix.Fstat(dir, &info) != nil || info.Uid != 0 || info.Gid != 0 || uint64(info.Dev) != pin.Dev || info.Ino != pin.Ino {
		return denied
	}
	fd, err := unix.Openat(dir, "cgroup.procs", unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return denied
	}
	body := []byte(strconv.Itoa(os.Getpid()) + "\n")
	n, writeErr := unix.Write(fd, body)
	closeErr := unix.Close(fd)
	if writeErr != nil || n != len(body) || closeErr != nil {
		return denied
	}
	return verifyChildCgroup(pin)
}

func verifyChildCgroup(pin cgroupIdentity) error {
	dir, err := openDirectory(childCgroupPath, true)
	if err != nil {
		return denied
	}
	defer unix.Close(dir)
	var info unix.Stat_t
	if unix.Fstat(dir, &info) != nil || info.Uid != 0 || info.Gid != 0 || uint64(info.Dev) != pin.Dev || info.Ino != pin.Ino {
		return denied
	}
	body, err := readProc(childCgroupPath+"/cgroup.procs", 65536)
	if err != nil || strings.TrimSpace(string(body)) != strconv.Itoa(os.Getpid()) {
		return denied
	}
	membership, err := readProc("/proc/self/cgroup", 4096)
	if err != nil || strings.TrimSpace(string(membership)) != "0::/g2-c80-predecessor" {
		return denied
	}
	return nil
}

func mountSandbox() error {
	// Mount namespaces are per-thread; the caller remains locked to this thread
	// all the way through exec. The parent and root supervisor keep their own.
	previous, err := os.Readlink("/proc/thread-self/ns/mnt")
	if err != nil {
		return denied
	}
	if unix.Unshare(unix.CLONE_NEWNS) != nil || unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, "") != nil {
		return denied
	}
	current, err := os.Readlink("/proc/thread-self/ns/mnt")
	if err != nil || current == previous {
		return denied
	}
	var socket unix.Stat_t
	if unix.Lstat("/run/docker.sock", &socket) != nil || socket.Mode&unix.S_IFMT != unix.S_IFSOCK || socket.Uid != 0 || socket.Gid != serviceGID {
		return denied
	}
	if unix.Mount("/dev/null", "/run/docker.sock", "", unix.MS_BIND, "") != nil {
		return denied
	}
	var masked, null unix.Stat_t
	if unix.Stat("/run/docker.sock", &masked) != nil || unix.Stat("/dev/null", &null) != nil || masked.Mode&unix.S_IFMT != unix.S_IFCHR || masked.Dev != null.Dev || masked.Ino != null.Ino || masked.Rdev != null.Rdev {
		return denied
	}
	for _, path := range []string{"/var/lib/g2", "/run/g2", "/run/g2-docker", "/opt/g2/realm-supervisor"} {
		var before unix.Stat_t
		if unix.Lstat(path, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFDIR || before.Uid != 0 || before.Mode&0022 != 0 {
			return denied
		}
		if unix.Mount("tmpfs", path, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "size=4096,mode=000,uid=0,gid=0") != nil {
			return denied
		}
		var after unix.Stat_t
		if unix.Lstat(path, &after) != nil || after.Mode&unix.S_IFMT != unix.S_IFDIR || after.Uid != 0 || after.Gid != 0 || after.Mode&07777 != 0 || before.Dev == after.Dev && before.Ino == after.Ino {
			return denied
		}
	}
	return nil
}

func verifyMasks() error {
	for _, path := range []string{"/var/lib/g2", "/run/g2", "/run/g2-docker", "/opt/g2/realm-supervisor"} {
		var st unix.Stat_t
		if unix.Lstat(path, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&07777 != 0 {
			return denied
		}
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err == nil {
			unix.Close(fd)
			return denied
		}
		if err != unix.EACCES {
			return denied
		}
	}
	var masked, null unix.Stat_t
	if unix.Stat("/run/docker.sock", &masked) != nil || unix.Stat("/dev/null", &null) != nil || masked.Mode&unix.S_IFMT != unix.S_IFCHR || masked.Dev != null.Dev || masked.Ino != null.Ino || masked.Rdev != null.Rdev {
		return denied
	}
	return nil
}

func allThreads(trap, a1, a2, a3 uintptr) error {
	_, _, errno := syscall.AllThreadsSyscall(trap, a1, a2, a3)
	if errno != 0 {
		return denied
	}
	return nil
}

func dropCredentials() error {
	bits, err := unix.PrctlRetInt(unix.PR_GET_SECUREBITS, 0, 0, 0, 0)
	if err != nil || bits != 0 {
		return denied
	}
	if allThreads(unix.SYS_PRCTL, unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0) != nil {
		return denied
	}
	var header unix.CapUserHeader
	header.Version = unix.LINUX_CAPABILITY_VERSION_3
	var caps [2]unix.CapUserData
	if unix.Capget(&header, &caps[0]) != nil {
		return denied
	}
	caps[0].Inheritable, caps[1].Inheritable = 0, 0
	if allThreads(unix.SYS_CAPSET, uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&caps[0])), 0) != nil {
		return denied
	}
	// x/sys/unix.Setgroups is per-thread on Linux; use Go's all-thread syscall
	// facility explicitly, as with capabilities and no_new_privs.
	if allThreads(unix.SYS_SETGROUPS, 0, 0, 0) != nil || syscall.Setresgid(serviceGID, serviceGID, serviceGID) != nil || syscall.Setresuid(serviceUID, serviceUID, serviceUID) != nil {
		return denied
	}
	if allThreads(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0) != nil {
		return denied
	}
	return verifyAllThreads()
}

func verifyAllThreads() error {
	for range 3 {
		before, err := os.ReadDir("/proc/self/task")
		if err != nil || len(before) == 0 {
			return denied
		}
		names := make([]string, 0, len(before))
		valid := true
		for _, entry := range before {
			name := entry.Name()
			if _, err := strconv.Atoi(name); err != nil {
				return denied
			}
			names = append(names, name)
			body, err := readProc("/proc/self/task/"+name+"/status", 65536)
			if err != nil {
				valid = false
				break
			}
			wanted := map[string]bool{"Uid": true, "Gid": true, "Groups": true, "NoNewPrivs": true, "CapEff": true, "CapPrm": true, "CapInh": true, "CapAmb": true}
			values := make(map[string]string, len(wanted))
			for _, line := range strings.Split(string(body), "\n") {
				key, value, ok := strings.Cut(line, ":")
				if !ok || !wanted[key] {
					continue
				}
				if _, exists := values[key]; exists {
					return denied
				}
				values[key] = strings.TrimSpace(value)
			}
			if len(values) != len(wanted) || strings.Join(strings.Fields(values["Uid"]), " ") != "10002 10002 10002 10002" || strings.Join(strings.Fields(values["Gid"]), " ") != "10002 10002 10002 10002" || values["Groups"] != "" || values["NoNewPrivs"] != "1" {
				return denied
			}
			for _, key := range []string{"CapEff", "CapPrm", "CapInh", "CapAmb"} {
				v, err := strconv.ParseUint(values[key], 16, 64)
				if err != nil || v != 0 {
					return denied
				}
			}
		}
		if !valid {
			continue
		}
		after, err := os.ReadDir("/proc/self/task")
		if err != nil {
			return denied
		}
		current := make([]string, 0, len(after))
		for _, entry := range after {
			current = append(current, entry.Name())
		}
		sort.Strings(names)
		sort.Strings(current)
		if strings.Join(names, ",") == strings.Join(current, ",") {
			return nil
		}
	}
	return denied
}

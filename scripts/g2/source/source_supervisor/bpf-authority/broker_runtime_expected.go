//go:build linux && amd64 && !cgo

package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/moby/profiles/seccomp"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

const (
	defaultUnixPathEnv   = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	defaultShmSize       = int64(67108864)
	defaultAppArmorName  = "docker-default"
	unconfinedAppArmor   = "unconfined"
	defaultSeccompName   = "builtin"
	unconfinedSeccomp    = "unconfined"
	minPasswdOrGroupID   = 0
	maxPasswdOrGroupID   = 1<<31 - 1
	maxUserDatabaseBytes = 10 << 20
)

type runtimeSysInfo struct {
	cgroupUnified      bool
	memoryLimit        bool
	swapLimit          bool
	memoryReservation  bool
	oomKillDisable     bool
	memorySwappiness   bool
	cpuShares          bool
	cpuCfs             bool
	cpuRealtime        bool
	cpuset             bool
	pidsLimit          bool
	blkioWeight        bool
	blkioWeightDevice  bool
	blkioReadBps       bool
	blkioWriteBps      bool
	blkioReadIOps      bool
	blkioWriteIOps     bool
	seccomp            bool
	cgroupNamespaces   bool
	timeNamespaces     bool
	appArmorSupported  bool
	rroSupported       bool
	pingGroupRange     bool
	unprivPortStart    bool
	possibleCPUs       []int
	thermalThrottleCPU []int
	mountInfo          []runtimeMountInfoEntry
}

type runtimeMountInfoEntry struct {
	mountpoint string
	optional   string
}

type runtimeUserEntry struct {
	name  string
	pass  string
	uid   int
	gid   int
	gecos string
	home  string
	shell string
}

type runtimeGroupEntry struct {
	name string
	pass string
	gid  int
	list []string
}

func checkDaemonConfigAbsent(diagnostic *admitDiagnostic) error {
	var st unix.Stat_t
	err := unix.Lstat("/etc/docker/daemon.json", &st)
	if err == nil || !errors.Is(err, unix.ENOENT) {
		if err != nil {
			diagnostic.reject("ADMIT_DAEMON_CONFIG_STAT", err)
		} else {
			diagnostic.reject("ADMIT_DAEMON_CONFIG_PRESENT", errGuardProtocol)
		}
		return errGuardProtocol
	}
	return nil
}

func daemonNamespaceStages(ns string) (string, string, string) {
	switch ns {
	case "user":
		return "ADMIT_NS_USER_INIT", "ADMIT_NS_USER_DAEMON", "ADMIT_NS_USER_IDENTITY"
	case "pid":
		return "ADMIT_NS_PID_INIT", "ADMIT_NS_PID_DAEMON", "ADMIT_NS_PID_IDENTITY"
	case "net":
		return "ADMIT_NS_NET_INIT", "ADMIT_NS_NET_DAEMON", "ADMIT_NS_NET_IDENTITY"
	case "mnt":
		return "ADMIT_NS_MNT_INIT", "ADMIT_NS_MNT_DAEMON", "ADMIT_NS_MNT_IDENTITY"
	case "ipc":
		return "ADMIT_NS_IPC_INIT", "ADMIT_NS_IPC_DAEMON", "ADMIT_NS_IPC_IDENTITY"
	case "uts":
		return "ADMIT_NS_UTS_INIT", "ADMIT_NS_UTS_DAEMON", "ADMIT_NS_UTS_IDENTITY"
	case "cgroup":
		return "ADMIT_NS_CGROUP_INIT", "ADMIT_NS_CGROUP_DAEMON", "ADMIT_NS_CGROUP_IDENTITY"
	}
	return "ADMIT_NS_INIT", "ADMIT_NS_DAEMON", "ADMIT_NS_IDENTITY"
}

func checkDaemonNamespacesAndRlimits(pid uint32, diagnostic *admitDiagnostic) error {
	if pid <= 1 {
		diagnostic.reject("ADMIT_DAEMON_PID", errGuardProtocol)
		return errGuardProtocol
	}
	for _, ns := range []string{"user", "pid", "net", "mnt", "ipc", "uts", "cgroup"} {
		var initStat, daemonStat unix.Stat_t
		if err := unix.Stat("/proc/1/ns/"+ns, &initStat); err != nil {
			stage, _, _ := daemonNamespaceStages(ns)
			diagnostic.reject(stage, err)
			return errGuardProtocol
		}
		if err := unix.Stat(fmt.Sprintf("/proc/%d/ns/%s", pid, ns), &daemonStat); err != nil {
			_, stage, _ := daemonNamespaceStages(ns)
			diagnostic.reject(stage, err)
			return errGuardProtocol
		}
		if initStat.Dev != daemonStat.Dev || initStat.Ino != daemonStat.Ino {
			_, _, stage := daemonNamespaceStages(ns)
			diagnostic.reject(stage, errGuardProtocol)
			return errGuardProtocol
		}
	}
	rawLimits, err := readHeldProc(fmt.Sprintf("%d/limits", pid), 65536)
	if err != nil || len(rawLimits) == 0 {
		if err != nil {
			diagnostic.reject("ADMIT_DAEMON_LIMITS_READ", err)
		} else {
			diagnostic.reject("ADMIT_DAEMON_LIMITS_EMPTY", errGuardProtocol)
		}
		return errGuardProtocol
	}
	return nil
}

func parsePossibleCPURanges(content string) ([]int, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errGuardProtocol
	}
	var cpus []int
	for _, r := range strings.Split(content, ",") {
		if rStart, rEnd, ok := strings.Cut(r, "-"); !ok {
			cpu, err := strconv.Atoi(rStart)
			if err != nil || cpu < 0 || cpu > 8192 {
				return nil, errGuardProtocol
			}
			cpus = append(cpus, cpu)
		} else {
			start, err1 := strconv.Atoi(rStart)
			end, err2 := strconv.Atoi(rEnd)
			if err1 != nil || err2 != nil || start < 0 || end < start || end > 8192 {
				return nil, errGuardProtocol
			}
			for i := start; i <= end; i++ {
				cpus = append(cpus, i)
			}
		}
	}
	if len(cpus) == 0 {
		return nil, errGuardProtocol
	}
	return cpus, nil
}

func parseProcMountInfo(raw []byte) ([]runtimeMountInfoEntry, error) {
	if len(raw) == 0 || len(raw) > 4<<20 {
		return nil, errGuardProtocol
	}
	var entries []runtimeMountInfoEntry
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		left, _, ok := strings.Cut(line, " - ")
		if !ok {
			return nil, errGuardProtocol
		}
		fields := strings.Fields(left)
		if len(fields) < 6 {
			return nil, errGuardProtocol
		}
		optional := ""
		if len(fields) > 6 {
			optional = strings.Join(fields[6:], " ")
		}
		entries = append(entries, runtimeMountInfoEntry{
			mountpoint: fields[4],
			optional:   optional,
		})
	}
	if sc.Err() != nil || len(entries) == 0 {
		return nil, errGuardProtocol
	}
	return entries, nil
}

func statExistsStrict(path string) (bool, error) {
	var st unix.Stat_t
	err := unix.Stat(path, &st)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	return false, errGuardProtocol
}

func probeRuntimeSysInfo(daemonPID uint32, diagnostic *admitDiagnostic) (runtimeSysInfo, error) {
	var info runtimeSysInfo
	if err := checkDaemonConfigAbsent(diagnostic); err != nil {
		diagnostic.reject("ADMIT_SYSINFO_DAEMON_CONFIG", err)
		return info, err
	}
	if daemonPID > 1 {
		if err := checkDaemonNamespacesAndRlimits(daemonPID, diagnostic); err != nil {
			diagnostic.reject("ADMIT_SYSINFO_DAEMON_STATE", err)
			return info, err
		}
	}
	var cgfs unix.Statfs_t
	if err := unix.Statfs("/sys/fs/cgroup", &cgfs); err != nil || int64(cgfs.Type) != unix.CGROUP2_SUPER_MAGIC {
		if err != nil {
			diagnostic.reject("ADMIT_SYSINFO_CGROUP_STATFS", err)
		} else {
			diagnostic.reject("ADMIT_SYSINFO_CGROUP_TYPE", errGuardProtocol)
		}
		return info, errGuardProtocol
	}
	info.cgroupUnified = true
	controllersRaw, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	if err != nil || len(controllersRaw) > 65536 {
		if err != nil {
			diagnostic.reject("ADMIT_SYSINFO_CONTROLLERS_READ", err)
		} else {
			diagnostic.reject("ADMIT_SYSINFO_CONTROLLERS_SIZE", errGuardProtocol)
		}
		return info, errGuardProtocol
	}
	controllers := make(map[string]bool)
	for _, c := range strings.Fields(string(controllersRaw)) {
		controllers[c] = true
	}
	info.memoryLimit = controllers["memory"]
	info.memoryReservation = controllers["memory"]
	if controllers["memory"] {
		cgBytes, e := readHeldProc("self/cgroup", 65536)
		if e != nil {
			diagnostic.reject("ADMIT_SYSINFO_SELF_CGROUP", e)
			return info, errGuardProtocol
		}
		cgGroup := ""
		for _, line := range strings.Split(strings.TrimSpace(string(cgBytes)), "\n") {
			if strings.HasPrefix(line, "0::") {
				cgGroup = strings.TrimPrefix(line, "0::")
				break
			}
		}
		if cgGroup != "" {
			swapExists, e := statExistsStrict(filepath.Join("/sys/fs/cgroup", cgGroup, "memory.swap.max"))
			if e != nil {
				diagnostic.reject("ADMIT_SYSINFO_SWAP_STAT", e)
				return info, e
			}
			info.swapLimit = swapExists
		}
	}
	info.cpuShares = controllers["cpu"]
	info.cpuCfs = controllers["cpu"]
	info.cpuset = controllers["cpuset"]
	info.pidsLimit = controllers["pids"]
	info.blkioWeight = controllers["io"]
	info.blkioWeightDevice = controllers["io"]
	info.blkioReadBps = controllers["io"]
	info.blkioWriteBps = controllers["io"]
	info.blkioReadIOps = controllers["io"]
	info.blkioWriteIOps = controllers["io"]
	if !info.memoryLimit || !info.swapLimit || !info.cpuCfs || !info.pidsLimit {
		diagnostic.reject("ADMIT_SYSINFO_REQUIRED_CONTROLLERS", errGuardProtocol)
		return info, errGuardProtocol
	}
	var prctlRet uintptr
	_, _, errno := unix.Syscall6(unix.SYS_PRCTL, unix.PR_GET_SECCOMP, 0, 0, 0, 0, 0)
	if errno != 0 && errno != unix.EINVAL {
		_ = prctlRet
	}
	act := uint32(unix.SECCOMP_RET_ALLOW)
	_, _, errno = unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_GET_ACTION_AVAIL, 0, 0)
	_ = act
	info.seccomp = errno != unix.ENOSYS

	cgnsExists, err := statExistsStrict("/proc/self/ns/cgroup")
	if err != nil {
		diagnostic.reject("ADMIT_SYSINFO_CGROUP_NS_STAT", err)
		return info, err
	}
	info.cgroupNamespaces = cgnsExists

	timensExists, err := statExistsStrict("/proc/self/ns/time")
	if err != nil {
		diagnostic.reject("ADMIT_SYSINFO_TIME_NS_STAT", err)
		return info, err
	}
	info.timeNamespaces = timensExists

	pingExists, err := statExistsStrict("/proc/sys/net/ipv4/ping_group_range")
	if err != nil {
		diagnostic.reject("ADMIT_SYSINFO_PING_STAT", err)
		return info, err
	}
	info.pingGroupRange = pingExists

	portExists, err := statExistsStrict("/proc/sys/net/ipv4/ip_unprivileged_port_start")
	if err != nil {
		diagnostic.reject("ADMIT_SYSINFO_PORT_STAT", err)
		return info, err
	}
	info.unprivPortStart = portExists

	aaDir, err := statExistsStrict("/sys/kernel/security/apparmor")
	if err != nil {
		diagnostic.reject("ADMIT_SYSINFO_APPARMOR_DIR_STAT", err)
		return info, err
	}
	aaParser, err := statExistsStrict("/sbin/apparmor_parser")
	if err != nil {
		diagnostic.reject("ADMIT_SYSINFO_APPARMOR_PARSER_STAT", err)
		return info, err
	}
	if aaDir && aaParser && os.Getenv("container") == "" {
		buf, e := os.ReadFile("/sys/module/apparmor/parameters/enabled")
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			diagnostic.reject("ADMIT_SYSINFO_APPARMOR_ENABLED_READ", e)
			return info, errGuardProtocol
		}
		info.appArmorSupported = e == nil && len(buf) > 1 && buf[0] == 'Y'
	}

	possibleRaw, err := os.ReadFile("/sys/devices/system/cpu/possible")
	if err != nil || len(possibleRaw) > 65536 {
		if err != nil {
			diagnostic.reject("ADMIT_SYSINFO_CPU_POSSIBLE_READ", err)
		} else {
			diagnostic.reject("ADMIT_SYSINFO_CPU_POSSIBLE_SIZE", errGuardProtocol)
		}
		return info, errGuardProtocol
	}
	cpus, err := parsePossibleCPURanges(string(possibleRaw))
	if err != nil {
		diagnostic.reject("ADMIT_SYSINFO_CPU_POSSIBLE_PARSE", err)
		return info, err
	}
	info.possibleCPUs = cpus
	for _, cpu := range cpus {
		p := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/thermal_throttle", cpu)
		exists, e := statExistsStrict(p)
		if e != nil {
			diagnostic.reject("ADMIT_SYSINFO_THERMAL_STAT", e)
			return info, e
		}
		if exists {
			info.thermalThrottleCPU = append(info.thermalThrottleCPU, cpu)
		}
	}

	mountRaw, err := readHeldProc("self/mountinfo", 4<<20)
	if err != nil {
		diagnostic.reject("ADMIT_SYSINFO_MOUNTINFO_READ", err)
		return info, err
	}
	mountInfo, err := parseProcMountInfo(mountRaw)
	if err != nil {
		diagnostic.reject("ADMIT_SYSINFO_MOUNTINFO_PARSE", err)
		return info, err
	}
	info.mountInfo = mountInfo
	// Pinned runc v1.2+/containerd.io 2.3.6 features include "rro" and kernel 6.8 supports mount_setattr AT_RECURSIVE.
	info.rroSupported = true
	return info, nil
}

func readRootfsOptionalFile(rootfs string, rel string, diagnostic *admitDiagnostic) ([]byte, bool, error) {
	if !runtimePath(rootfs) || rel != "etc/passwd" && rel != "etc/group" {
		diagnostic.reject("ADMIT_OPTIONAL_INPUT", errGuardProtocol)
		return nil, false, errGuardProtocol
	}
	rootFD, err := runtimeDirectory(rootfs, false)
	if err != nil {
		diagnostic.reject("ADMIT_OPTIONAL_ROOT_DIRECTORY", err)
		return nil, false, err
	}
	defer unix.Close(rootFD)
	etcFD, err := unix.Openat2(rootFD, "etc", &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, false, nil
		}
		diagnostic.reject("ADMIT_OPTIONAL_ETC_OPEN", err)
		return nil, false, errGuardProtocol
	}
	defer unix.Close(etcFD)
	var etcHeld, etcNamed unix.Stat_t
	if err := unix.Fstat(etcFD, &etcHeld); err != nil {
		diagnostic.reject("ADMIT_OPTIONAL_ETC_FSTAT", err)
		return nil, false, errGuardProtocol
	}
	if err := unix.Fstatat(rootFD, "etc", &etcNamed, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		diagnostic.reject("ADMIT_OPTIONAL_ETC_FSTATAT", err)
		return nil, false, errGuardProtocol
	}
	if !sameTrustedStat(etcHeld, etcNamed) {
		diagnostic.reject("ADMIT_OPTIONAL_ETC_IDENTITY", errGuardProtocol)
		return nil, false, errGuardProtocol
	}
	if etcHeld.Mode&unix.S_IFMT != unix.S_IFDIR || etcHeld.Uid != 0 || etcHeld.Gid != 0 || etcHeld.Mode&0022 != 0 {
		diagnostic.reject("ADMIT_OPTIONAL_ETC_METADATA", errGuardProtocol)
		return nil, false, errGuardProtocol
	}
	leaf := filepath.Base(rel)
	fd, err := unix.Openat2(etcFD, leaf, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, false, nil
		}
		diagnostic.reject("ADMIT_OPTIONAL_FILE_OPEN", err)
		return nil, false, errGuardProtocol
	}
	file := os.NewFile(uintptr(fd), rel)
	if file == nil {
		_ = unix.Close(fd)
		diagnostic.reject("ADMIT_OPTIONAL_FILE_HANDLE", errGuardProtocol)
		return nil, false, errGuardProtocol
	}
	defer file.Close()
	var before, after, named unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		diagnostic.reject("ADMIT_OPTIONAL_FILE_FSTAT", err)
		return nil, false, errGuardProtocol
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Gid != 0 || before.Mode&0022 != 0 || before.Size < 0 || before.Size > maxUserDatabaseBytes {
		diagnostic.reject("ADMIT_OPTIONAL_FILE_METADATA", errGuardProtocol)
		return nil, false, errGuardProtocol
	}
	data, err := io.ReadAll(io.LimitReader(file, maxUserDatabaseBytes+1))
	if err != nil {
		diagnostic.reject("ADMIT_OPTIONAL_FILE_READ", err)
		return nil, false, errGuardProtocol
	}
	if int64(len(data)) != before.Size {
		diagnostic.reject("ADMIT_OPTIONAL_FILE_SIZE", errGuardProtocol)
		return nil, false, errGuardProtocol
	}
	if err := unix.Fstat(fd, &after); err != nil {
		diagnostic.reject("ADMIT_OPTIONAL_FILE_RESTAT", err)
		return nil, false, errGuardProtocol
	}
	if err := unix.Fstatat(etcFD, leaf, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		diagnostic.reject("ADMIT_OPTIONAL_FILE_FSTATAT", err)
		return nil, false, errGuardProtocol
	}
	if !sameTrustedStat(before, after) || !sameTrustedStat(before, named) {
		diagnostic.reject("ADMIT_OPTIONAL_FILE_IDENTITY", errGuardProtocol)
		return nil, false, errGuardProtocol
	}
	return data, true, nil
}

func parseUserColonParts(line []byte, targets ...any) {
	parts := bytes.Split(line, []byte(":"))
	for i, p := range parts {
		if len(targets) <= i {
			break
		}
		switch dst := targets[i].(type) {
		case *string:
			*dst = string(p)
		case *int:
			*dst, _ = strconv.Atoi(string(p))
		case *[]string:
			if len(p) != 0 {
				*dst = strings.Split(string(p), ",")
			} else {
				*dst = []string{}
			}
		}
	}
}

func parsePasswdBytes(raw []byte, hasFile bool, filter func(runtimeUserEntry) bool) ([]runtimeUserEntry, error) {
	if !hasFile {
		return nil, errors.New("nil source for passwd-formatted data")
	}
	var out []runtimeUserEntry
	s := bufio.NewScanner(bytes.NewReader(raw))
	for s.Scan() {
		line := bytes.TrimSpace(s.Bytes())
		if len(line) == 0 {
			continue
		}
		var u runtimeUserEntry
		parseUserColonParts(line, &u.name, &u.pass, &u.uid, &u.gid, &u.gecos, &u.home, &u.shell)
		if filter == nil || filter(u) {
			out = append(out, u)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseGroupBytes(raw []byte, hasFile bool, filter func(runtimeGroupEntry) bool) ([]runtimeGroupEntry, error) {
	if !hasFile {
		return nil, errors.New("nil source for group-formatted data")
	}
	var out []runtimeGroupEntry
	s := bufio.NewScanner(bytes.NewReader(raw))
	for s.Scan() {
		line := bytes.TrimSpace(s.Bytes())
		if len(line) == 0 {
			continue
		}
		var g runtimeGroupEntry
		parseUserColonParts(line, &g.name, &g.pass, &g.gid, &g.list)
		if filter == nil || filter(g) {
			out = append(out, g)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseNumericUserID(val string) (int, bool, error) {
	if val == "" {
		return 0, false, nil
	}
	id, err := strconv.Atoi(val)
	if err != nil {
		if errors.Is(err, strconv.ErrSyntax) {
			return 0, false, nil
		}
		return 0, true, errGuardProtocol
	}
	if id < minPasswdOrGroupID || id > maxPasswdOrGroupID {
		return 0, true, errGuardProtocol
	}
	return id, true, nil
}

func resolveContainerUser(userSpec string, passwdRaw []byte, hasPasswd bool, groupRaw []byte, hasGroup bool) (specs.User, error) {
	var out specs.User
	var userArg, groupArg string
	parseUserColonParts([]byte(userSpec), &userArg, &groupArg)
	uidArg, isUID, err := parseNumericUserID(userArg)
	if err != nil {
		return out, err
	}
	gidArg, isGID, err := parseNumericUserID(groupArg)
	if err != nil {
		return out, err
	}
	uid, gid := 0, 0
	sgids := []int{}
	users, err := parsePasswdBytes(passwdRaw, hasPasswd, func(u runtimeUserEntry) bool {
		if userArg == "" {
			return u.uid == uid
		}
		if isUID {
			return uidArg == u.uid
		}
		return u.name == userArg
	})
	if err != nil && hasPasswd {
		return out, errGuardProtocol
	}
	var matchedUserName string
	if len(users) > 0 {
		matchedUserName = users[0].name
		uid = users[0].uid
		gid = users[0].gid
	} else if userArg != "" {
		if !isUID {
			return out, errGuardProtocol
		}
		uid = uidArg
	}
	if groupArg != "" || matchedUserName != "" {
		groups, gerr := parseGroupBytes(groupRaw, hasGroup, func(g runtimeGroupEntry) bool {
			if groupArg == "" {
				return slices.Contains(g.list, matchedUserName)
			}
			if isGID {
				return gidArg == g.gid
			}
			return g.name == groupArg
		})
		if gerr != nil && hasGroup {
			return out, errGuardProtocol
		}
		if groupArg != "" {
			if len(groups) > 0 {
				gid = groups[0].gid
			} else {
				if !isGID {
					return out, errGuardProtocol
				}
				gid = gidArg
			}
		} else if len(groups) > 0 {
			sgids = make([]int, len(groups))
			for i, grp := range groups {
				sgids[i] = grp.gid
			}
		}
	}
	if uid < 0 || gid < 0 {
		return out, errGuardProtocol
	}
	out.UID = uint32(uid)
	out.GID = uint32(gid)
	out.AdditionalGids = []uint32{out.GID}
	for _, g := range sgids {
		if g < 0 {
			return out, errGuardProtocol
		}
		out.AdditionalGids = append(out.AdditionalGids, uint32(g))
	}
	return out, nil
}

func replaceOrAppendEnvValues(defaults, overrides []string) []string {
	cache := make(map[string]int, len(defaults))
	for i, e := range defaults {
		before, _, _ := strings.Cut(e, "=")
		cache[before] = i
	}
	for _, value := range overrides {
		before, _, ok := strings.Cut(value, "=")
		if !ok {
			if i, exists := cache[value]; exists {
				defaults[i] = ""
			}
			continue
		}
		if i, exists := cache[before]; exists {
			defaults[i] = value
		} else {
			defaults = append(defaults, value)
		}
	}
	ret := make([]string, 0, len(defaults))
	for _, e := range defaults {
		if e != "" {
			ret = append(ret, e)
		}
	}
	return ret
}

func createDaemonEnvironment(tty bool, hostname string, linkedEnv, configEnv []string) []string {
	env := make([]string, 0, 2+len(linkedEnv)+len(configEnv))
	env = append(env, "PATH="+defaultUnixPathEnv)
	env = append(env, "HOSTNAME="+hostname)
	if tty {
		env = append(env, "TERM=xterm")
	}
	env = append(env, linkedEnv...)
	return replaceOrAppendEnvValues(env, configEnv)
}

func compareMountPaths(a, b string) int {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	aParts := strings.Count(a, string(os.PathSeparator))
	bParts := strings.Count(b, string(os.PathSeparator))
	if c := cmp.Compare(aParts, bParts); c != 0 {
		return c
	}
	return cmp.Compare(a, b)
}

func mergeTmpfsMountOptions(options []string) ([]string, error) {
	flagTable := map[string]int{
		"ro":            unix.MS_RDONLY,
		"rw":            unix.MS_RDONLY,
		"suid":          unix.MS_NOSUID,
		"nosuid":        unix.MS_NOSUID,
		"dev":           unix.MS_NODEV,
		"nodev":         unix.MS_NODEV,
		"exec":          unix.MS_NOEXEC,
		"noexec":        unix.MS_NOEXEC,
		"sync":          unix.MS_SYNCHRONOUS,
		"async":         unix.MS_SYNCHRONOUS,
		"dirsync":       unix.MS_DIRSYNC,
		"remount":       unix.MS_REMOUNT,
		"mand":          unix.MS_MANDLOCK,
		"nomand":        unix.MS_MANDLOCK,
		"atime":         unix.MS_NOATIME,
		"noatime":       unix.MS_NOATIME,
		"diratime":      unix.MS_NODIRATIME,
		"nodiratime":    unix.MS_NODIRATIME,
		"bind":          -1,
		"rbind":         -1,
		"unbindable":    -1,
		"runbindable":   -1,
		"private":       -1,
		"rprivate":      -1,
		"shared":        -1,
		"rshared":       -1,
		"slave":         -1,
		"rslave":        -1,
		"relatime":      unix.MS_RELATIME,
		"norelatime":    unix.MS_RELATIME,
		"strictatime":   unix.MS_STRICTATIME,
		"nostrictatime": unix.MS_STRICTATIME,
	}
	validData := map[string]bool{
		"":          true,
		"size":      true,
		"mode":      true,
		"uid":       true,
		"gid":       true,
		"nr_inodes": true,
		"nr_blocks": true,
		"mpol":      true,
	}
	flagCollisions := map[int]bool{}
	dataCollisions := map[string]bool{}
	var newOptions []string
	for i := len(options) - 1; i >= 0; i-- {
		option := options[i]
		if option == "defaults" {
			continue
		}
		if key, ok := flagTable[option]; ok {
			if !flagCollisions[key] {
				newOptions = append([]string{option}, newOptions...)
				flagCollisions[key] = true
			}
			continue
		}
		opt, _, ok := strings.Cut(option, "=")
		if !ok || !validData[opt] {
			return nil, errGuardProtocol
		}
		if !dataCollisions[opt] {
			newOptions = append([]string{option}, newOptions...)
			dataCollisions[opt] = true
		}
	}
	return newOptions, nil
}

func defaultLinuxMaskedPathsFromSysInfo(sys runtimeSysInfo) []string {
	masked := []string{
		"/proc/acpi",
		"/proc/asound",
		"/proc/interrupts",
		"/proc/kcore",
		"/proc/keys",
		"/proc/latency_stats",
		"/proc/sched_debug",
		"/proc/scsi",
		"/proc/timer_list",
		"/proc/timer_stats",
		"/sys/devices/virtual/powercap",
		"/sys/firmware",
	}
	for _, cpu := range sys.thermalThrottleCPU {
		masked = append(masked, fmt.Sprintf("/sys/devices/system/cpu/cpu%d/thermal_throttle", cpu))
	}
	return masked
}

func defaultLinuxReadonlyPaths() []string {
	return []string{
		"/proc/bus",
		"/proc/fs",
		"/proc/irq",
		"/proc/sys",
		"/proc/sysrq-trigger",
	}
}

func defaultLinuxCapabilities() []string {
	return []string{
		"CAP_CHOWN",
		"CAP_DAC_OVERRIDE",
		"CAP_FSETID",
		"CAP_FOWNER",
		"CAP_MKNOD",
		"CAP_NET_RAW",
		"CAP_SETGID",
		"CAP_SETUID",
		"CAP_SETFCAP",
		"CAP_SETPCAP",
		"CAP_NET_BIND_SERVICE",
		"CAP_SYS_CHROOT",
		"CAP_KILL",
		"CAP_AUDIT_WRITE",
	}
}

func defaultLinuxDeviceCgroupRules() []specs.LinuxDeviceCgroup {
	return []specs.LinuxDeviceCgroup{
		{Allow: false, Access: "rwm"},
		{Allow: true, Type: "c", Major: new(int64(1)), Minor: new(int64(5)), Access: "rwm"},
		{Allow: true, Type: "c", Major: new(int64(1)), Minor: new(int64(3)), Access: "rwm"},
		{Allow: true, Type: "c", Major: new(int64(1)), Minor: new(int64(9)), Access: "rwm"},
		{Allow: true, Type: "c", Major: new(int64(1)), Minor: new(int64(8)), Access: "rwm"},
		{Allow: true, Type: "c", Major: new(int64(5)), Minor: new(int64(0)), Access: "rwm"},
		{Allow: true, Type: "c", Major: new(int64(5)), Minor: new(int64(1)), Access: "rwm"},
		{Allow: false, Type: "c", Major: new(int64(10)), Minor: new(int64(229)), Access: "rwm"},
	}
}

func defaultLinuxSpecFromSysInfo(sys runtimeSysInfo) specs.Spec {
	return specs.Spec{
		Version: specs.Version,
		Process: &specs.Process{
			Capabilities: &specs.LinuxCapabilities{
				Bounding:  defaultLinuxCapabilities(),
				Permitted: defaultLinuxCapabilities(),
				Effective: defaultLinuxCapabilities(),
			},
		},
		Root: &specs.Root{},
		Mounts: []specs.Mount{
			{
				Destination: "/proc",
				Type:        "proc",
				Source:      "proc",
				Options:     []string{"nosuid", "noexec", "nodev"},
			},
			{
				Destination: "/dev",
				Type:        "tmpfs",
				Source:      "tmpfs",
				Options:     []string{"nosuid", "strictatime", "mode=755", "size=65536k"},
			},
			{
				Destination: "/dev/pts",
				Type:        "devpts",
				Source:      "devpts",
				Options:     []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620", "gid=5"},
			},
			{
				Destination: "/sys",
				Type:        "sysfs",
				Source:      "sysfs",
				Options:     []string{"nosuid", "noexec", "nodev", "ro"},
			},
			{
				Destination: "/sys/fs/cgroup",
				Type:        "cgroup",
				Source:      "cgroup",
				Options:     []string{"ro", "nosuid", "noexec", "nodev"},
			},
			{
				Destination: "/dev/mqueue",
				Type:        "mqueue",
				Source:      "mqueue",
				Options:     []string{"nosuid", "noexec", "nodev"},
			},
			{
				Destination: "/dev/shm",
				Type:        "tmpfs",
				Source:      "shm",
				Options:     []string{"nosuid", "noexec", "nodev", "mode=1777"},
			},
		},
		Linux: &specs.Linux{
			MaskedPaths:   defaultLinuxMaskedPathsFromSysInfo(sys),
			ReadonlyPaths: defaultLinuxReadonlyPaths(),
			Namespaces: []specs.LinuxNamespace{
				{Type: specs.MountNamespace},
				{Type: specs.NetworkNamespace},
				{Type: specs.TimeNamespace},
				{Type: specs.UTSNamespace},
				{Type: specs.PIDNamespace},
				{Type: specs.IPCNamespace},
			},
			Devices: []specs.LinuxDevice{},
			Resources: &specs.LinuxResources{
				Devices: defaultLinuxDeviceCgroupRules(),
			},
		},
	}
}

type mobyContainerMount struct {
	source                 string
	destination            string
	writable               bool
	data                   string
	propagation            string
	nonRecursive           bool
	readOnlyNonRecursive   bool
	readOnlyForceRecursive bool
}

func sortMobyMounts(m []mobyContainerMount) {
	slices.SortStableFunc(m, func(a, b mobyContainerMount) int {
		return compareMountPaths(a.destination, b.destination)
	})
}

func setOCINamespace(s *specs.Spec, ns specs.LinuxNamespace) {
	if s.Linux == nil {
		s.Linux = &specs.Linux{}
	}
	for i, n := range s.Linux.Namespaces {
		if n.Type == ns.Type {
			s.Linux.Namespaces[i] = ns
			return
		}
	}
	s.Linux.Namespaces = append(s.Linux.Namespaces, ns)
}

func removeOCINamespace(s *specs.Spec, nsType specs.LinuxNamespaceType) {
	if s.Linux == nil {
		return
	}
	for i, n := range s.Linux.Namespaces {
		if n.Type == nsType {
			s.Linux.Namespaces = append(s.Linux.Namespaces[:i], s.Linux.Namespaces[i+1:]...)
			return
		}
	}
}

// allowContainerSelftestBPF adds the one fixed probe syscall to Docker's
// unconditional allow group; the sealed LSM still enforces BPF authority.
func allowContainerSelftestBPF(profile *specs.LinuxSeccomp) error {
	if profile == nil {
		return errGuardProtocol
	}
	for _, rule := range profile.Syscalls {
		for _, name := range rule.Names {
			if name == "bpf" {
				return errGuardProtocol
			}
		}
	}
	for i := range profile.Syscalls {
		rule := &profile.Syscalls[i]
		if rule.Action == specs.ActAllow && len(rule.Args) == 0 && len(rule.Names) != 0 {
			rule.Names = append(rule.Names, "bpf")
			return nil
		}
	}
	return errGuardProtocol
}

// expectedRuntimeSpec reconstructs the complete non-secret OCI specification
// from the fixed Moby v29.8.2 (commit 8af9fe3a36bab3e039862a2ab1cef1880c9b4d03)
// generator order and the independently authenticated container/daemon facts.
func expectedRuntimeSpec(b *authorityBroker, grant *brokerGrant) (map[string]any, error) {
	spec, err := buildExpectedRuntimeSpecStruct(b, grant)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, errGuardProtocol
	}
	out, err := runtimeJSON(raw)
	if err != nil {
		return nil, errGuardProtocol
	}
	return out, nil
}

func buildExpectedRuntimeSpecStruct(b *authorityBroker, grant *brokerGrant) (*specs.Spec, error) {
	if grant == nil || grant.facts == nil || !grant.facts.runtimeReady || grant.config == nil || !runtimeContainerID(grant.containerID) {
		return nil, errGuardProtocol
	}
	facts := grant.facts
	if !facts.sysInfoValid || !runtimePath(facts.rootfs) || facts.hostname == "" || facts.path == "" || facts.cwd == "" {
		return nil, errGuardProtocol
	}
	if b != nil {
		if err := checkDaemonConfigAbsent(nil); err != nil {
			return nil, err
		}
	}
	if b != nil && len(b.daemons) > 0 {
		ownerID := grant.owner
		if p := b.principals[grant.owner]; p != nil && p.Role == "REALM_ROOT" {
			ownerID = b.bootOwner
		}
		if d := b.daemons[ownerID]; d != nil {
			if !livePIDFD(d.pidfd) || checkDaemonNamespacesAndRlimits(d.pid, nil) != nil {
				return nil, errGuardProtocol
			}
		}
	}
	sys := facts.sysInfo
	if !sys.cgroupUnified || !sys.memoryLimit || !sys.swapLimit || !sys.cpuCfs || !sys.pidsLimit || !sys.seccomp || !sys.cgroupNamespaces {
		return nil, errGuardProtocol
	}
	wantHost, ok := grant.config["HostConfig"].(map[string]any)
	if !ok {
		return nil, errGuardProtocol
	}
	var wantMem, wantSwap, wantNanoCPUs, wantPids int64
	var hasPids bool
	if rawMem, exists := wantHost["Memory"]; exists && rawMem != nil {
		v, ok := exactNumber(rawMem)
		if !ok || v != 805306368 || grant.purpose != "browser-negative" {
			return nil, errGuardProtocol
		}
		wantMem = int64(v)
		wantSwap = wantMem * 2
	}
	if rawCPU, exists := wantHost["NanoCpus"]; exists && rawCPU != nil {
		v, ok := exactNumber(rawCPU)
		if !ok || v != 1000000000 || grant.purpose != "browser-negative" {
			return nil, errGuardProtocol
		}
		wantNanoCPUs = int64(v)
	}
	if rawPids, exists := wantHost["PidsLimit"]; exists && rawPids != nil {
		v, ok := exactNumber(rawPids)
		if !ok || v == 0 || v > 4096 {
			return nil, errGuardProtocol
		}
		wantPids = int64(v)
		hasPids = true
	}
	switch grant.purpose {
	case "browser-negative":
		if wantMem != 805306368 || wantSwap != 1610612736 || wantNanoCPUs != 1000000000 || !hasPids || wantPids != 128 {
			return nil, errGuardProtocol
		}
	case "dns-preflight", "container-bpf-selftest":
		if wantMem != 0 || wantSwap != 0 || wantNanoCPUs != 0 || !hasPids || wantPids != 32 {
			return nil, errGuardProtocol
		}
	case "postgres-uid", "postgres-gid":
		if wantMem != 0 || wantSwap != 0 || wantNanoCPUs != 0 || hasPids {
			return nil, errGuardProtocol
		}
	default:
		if !strings.HasPrefix(grant.purpose, "postgres-init-") && !strings.HasPrefix(grant.purpose, "postgres-live-") && !strings.HasPrefix(grant.purpose, "recovery-") {
			return nil, errGuardProtocol
		}
		if wantMem != 0 || wantSwap != 0 || wantNanoCPUs != 0 || !hasPids || wantPids != 256 {
			return nil, errGuardProtocol
		}
	}
	if facts.memory != wantMem || facts.memorySwap != wantSwap || facts.nanoCPUs != wantNanoCPUs || facts.hasPidsLimit != hasPids || facts.pidsLimit != wantPids {
		return nil, errGuardProtocol
	}
	if facts.shmSize != defaultShmSize || facts.ipcMode != "private" || facts.cgroupnsMode != "private" || facts.oomScoreAdj != 0 || facts.privileged || !facts.readonlyRootfs || !facts.noNewPrivileges {
		return nil, errGuardProtocol
	}

	s := defaultLinuxSpecFromSysInfo(sys)

	// 1. withCommonOptions
	s.Root = &specs.Root{
		Path:     facts.rootfs,
		Readonly: true,
	}
	args := make([]string, 0, 1+len(facts.args))
	args = append(args, facts.path)
	for _, rawArg := range facts.args {
		str, ok := rawArg.(string)
		if !ok || strings.ContainsRune(str, 0) {
			return nil, errGuardProtocol
		}
		args = append(args, str)
	}
	s.Process.Args = args
	s.Process.Cwd = facts.cwd
	s.Process.Env = createDaemonEnvironment(false, facts.hostname, nil, facts.configEnv)
	s.Process.Terminal = false
	s.Hostname = facts.hostname
	if s.Linux.Sysctl == nil {
		s.Linux.Sysctl = make(map[string]string)
	}
	if facts.domainname != "" {
		return nil, errGuardProtocol
	}
	// NetworkMode.IsPrivate() is true for both "none" and custom bridge networks;
	// only "host" or "container:<id>" is non-private.
	if facts.networkMode == "" || facts.networkMode == "host" || strings.HasPrefix(facts.networkMode, "container:") {
		return nil, errGuardProtocol
	}
	if sys.pingGroupRange {
		s.Linux.Sysctl["net.ipv4.ping_group_range"] = "0 2147483647"
	}
	if sys.unprivPortStart {
		s.Linux.Sysctl["net.ipv4.ip_unprivileged_port_start"] = "0"
	}

	// 2. withCgroups: fixed --exec-opt=native.cgroupdriver=cgroupfs and empty CgroupParent
	s.Linux.CgroupsPath = filepath.Join("/docker", grant.containerID)

	// 3. WithResources
	var memoryRes *specs.LinuxMemory
	if wantMem > 0 || wantSwap > 0 {
		mem := specs.LinuxMemory{}
		if wantMem > 0 {
			limit := wantMem
			mem.Limit = &limit
		}
		if wantSwap > 0 {
			swap := wantSwap
			mem.Swap = &swap
		}
		if mem != (specs.LinuxMemory{}) {
			memoryRes = &mem
		}
	}
	var cpuRes *specs.LinuxCPU
	if wantNanoCPUs > 0 {
		period := uint64(100000)
		quota := wantNanoCPUs * int64(period) / 1e9
		cpuRes = &specs.LinuxCPU{
			Period: &period,
			Quota:  &quota,
		}
	}
	var pidsRes *specs.LinuxPids
	if hasPids {
		limit := wantPids
		pidsRes = &specs.LinuxPids{Limit: &limit}
	}
	s.Linux.Resources.Memory = memoryRes
	s.Linux.Resources.CPU = cpuRes
	s.Linux.Resources.BlockIO = &specs.LinuxBlockIO{}
	s.Linux.Resources.Pids = pidsRes

	// 4. WithSysctls: no custom HostConfig.Sysctls are permitted
	// 5. WithUser
	resolvedUser, err := resolveContainerUser(facts.user, facts.passwdRaw, facts.hasPasswd, facts.groupRaw, facts.hasGroup)
	if err != nil {
		return nil, err
	}
	s.Process.User.UID = resolvedUser.UID
	s.Process.User.GID = resolvedUser.GID
	s.Process.User.AdditionalGids = resolvedUser.AdditionalGids

	// 6. WithUmask: nil
	// 7. WithDevices: non-privileged with no custom devices appends default permissions to themselves (16 rules)
	devPermissions := append([]specs.LinuxDeviceCgroup(nil), s.Linux.Resources.Devices...)
	s.Linux.Devices = append(s.Linux.Devices, ([]specs.LinuxDevice)(nil)...)
	s.Linux.Resources.Devices = append(s.Linux.Resources.Devices, devPermissions...)

	// 8. withRlimits: both host and daemon Ulimits are empty -> nil slice (omitted)
	s.Process.Rlimits = nil

	// 9. WithNamespaces
	setOCINamespace(&s, specs.LinuxNamespace{Type: specs.NetworkNamespace})
	if !sys.timeNamespaces {
		removeOCINamespace(&s, specs.TimeNamespace)
	}
	setOCINamespace(&s, specs.LinuxNamespace{Type: specs.IPCNamespace})
	setOCINamespace(&s, specs.LinuxNamespace{Type: specs.PIDNamespace})
	setOCINamespace(&s, specs.LinuxNamespace{Type: specs.CgroupNamespace})

	// 10. WithCapabilities: CapDrop=["ALL"], CapAdd=nil, Privileged=false -> empty slices -> nil Capabilities slices
	s.Process.Capabilities.Bounding = nil
	s.Process.Capabilities.Effective = nil
	s.Process.Capabilities.Permitted = nil
	s.Process.Capabilities.Inheritable = nil
	s.Process.Capabilities.Ambient = nil

	// 11. WithSeccomp: GetDefaultProfile(&s) after capabilities are cleared
	seccompProfile, err := seccomp.GetDefaultProfile(&s)
	if err != nil || seccompProfile == nil {
		return nil, errGuardProtocol
	}
	if grant.purpose == "container-bpf-selftest" {
		if err = allowContainerSelftestBPF(seccompProfile); err != nil {
			return nil, err
		}
	}
	s.Linux.Seccomp = seccompProfile

	// 12. withMounts: setupContainerDirs + setupMounts
	var mnts []mobyContainerMount
	if tmpfsRaw, exists := wantHost["Tmpfs"]; exists && tmpfsRaw != nil {
		items, ok := tmpfsRaw.(map[string]any)
		if !ok || len(items) != 1 || grant.purpose != "browser-negative" {
			return nil, errGuardProtocol
		}
		data, ok := items["/tmp"].(string)
		if !ok || data != "rw,nosuid,nodev,noexec,size=134217728,mode=1777" {
			return nil, errGuardProtocol
		}
		mnts = append(mnts, mobyContainerMount{
			source:      "tmpfs",
			destination: "/tmp",
			data:        data,
		})
	}
	var userMnts []mobyContainerMount
	for _, mf := range facts.mounts {
		propagation := mf.propagation
		if propagation == "" && mf.typ == "bind" {
			propagation = "rprivate"
		}
		userMnts = append(userMnts, mobyContainerMount{
			source:               mf.source,
			destination:          mf.destination,
			writable:             !mf.readonly,
			propagation:          propagation,
			nonRecursive:         mf.nonRecursive,
			readOnlyNonRecursive: mf.readOnlyNonRecursive,
		})
	}
	sortMobyMounts(userMnts)
	if !runtimePath(facts.resolvPath) || !runtimePath(facts.hostnamePath) || !runtimePath(facts.hostsPath) {
		return nil, errGuardProtocol
	}
	netMnts := []mobyContainerMount{
		{source: facts.resolvPath, destination: "/etc/resolv.conf", writable: false, propagation: "rprivate"},
		{source: facts.hostnamePath, destination: "/etc/hostname", writable: false, propagation: "rprivate"},
		{source: facts.hostsPath, destination: "/etc/hosts", writable: false, propagation: "rprivate"},
	}
	mnts = append(mnts, userMnts...)
	mnts = append(mnts, netMnts...)
	sortMobyMounts(mnts)

	userMountsMap := make(map[string]struct{}, len(mnts))
	for _, m := range mnts {
		userMountsMap[m.destination] = struct{}{}
	}
	defaultMounts := s.Mounts[:0]
	_, mountDev := userMountsMap["/dev"]
	for _, m := range s.Mounts {
		if _, ok := userMountsMap[m.Destination]; ok {
			continue
		}
		if mountDev && strings.HasPrefix(m.Destination, "/dev/") {
			continue
		}
		if m.Destination == "/dev/shm" {
			sizeOpt := "size=" + strconv.FormatInt(facts.shmSize, 10)
			m.Options = append(append([]string(nil), m.Options...), sizeOpt)
		}
		defaultMounts = append(defaultMounts, m)
	}
	s.Mounts = defaultMounts
	for _, m := range mnts {
		if m.source == "tmpfs" {
			options := []string{"noexec", "nosuid", "nodev", "rprivate"}
			if m.data != "" {
				options = append(options, strings.Split(m.data, ",")...)
			}
			merged, e := mergeTmpfsMountOptions(options)
			if e != nil {
				return nil, e
			}
			s.Mounts = append(s.Mounts, specs.Mount{
				Destination: m.destination,
				Source:      m.source,
				Type:        "tmpfs",
				Options:     merged,
			})
			continue
		}
		mt := specs.Mount{
			Destination: m.destination,
			Source:      m.source,
			Type:        "bind",
		}
		bindMode := "rbind"
		if m.nonRecursive {
			bindMode = "bind"
		}
		opts := []string{bindMode}
		if !m.writable {
			rro := !m.readOnlyNonRecursive && sys.rroSupported
			if rro {
				opts = append(opts, "rro")
			} else {
				opts = append(opts, "ro")
			}
		}
		if m.propagation != "" {
			if m.propagation != "rprivate" && m.propagation != "private" {
				return nil, errGuardProtocol
			}
			opts = append(opts, m.propagation)
		}
		mt.Options = opts
		s.Mounts = append(s.Mounts, mt)
	}
	if s.Root.Readonly {
		for i, m := range s.Mounts {
			switch m.Destination {
			case "/proc", "/dev/pts", "/dev/shm", "/dev/mqueue", "/dev":
				continue
			}
			if _, ok := userMountsMap[m.Destination]; !ok {
				if !slices.Contains(m.Options, "ro") {
					s.Mounts[i].Options = append(s.Mounts[i].Options, "ro")
				}
			}
		}
	}

	// 13. WithApparmor
	if sys.appArmorSupported {
		if facts.appArmorProfile != defaultAppArmorName {
			return nil, errGuardProtocol
		}
		s.Process.ApparmorProfile = defaultAppArmorName
	} else {
		if facts.appArmorProfile != "" {
			return nil, errGuardProtocol
		}
		s.Process.ApparmorProfile = ""
	}

	// 14. WithSelinux
	if facts.processLabel != "" || facts.mountLabel != "" {
		return nil, errGuardProtocol
	}
	s.Process.SelinuxLabel = ""
	s.Linux.MountLabel = ""

	// 15. WithOOMScore
	oomScore := 0
	s.Process.OOMScoreAdj = &oomScore

	// 16. WithAnnotations (empty map, omitted by json.Marshal)
	s.Annotations = make(map[string]string)

	// 17. WithNoNewPrivileges
	s.Process.NoNewPrivileges = true

	// 18. WithMaskedPaths / WithReadonlyPaths from HostConfig (populated with default spec paths at containerCreate)
	s.Linux.MaskedPaths = defaultLinuxMaskedPathsFromSysInfo(sys)
	s.Linux.ReadonlyPaths = defaultLinuxReadonlyPaths()

	return &s, nil
}

func verifyMountAndRootfsIdentities(facts *runtimeContainerFacts, diagnostic *admitDiagnostic) error {
	if facts == nil || !runtimePath(facts.rootfs) {
		diagnostic.reject("ADMIT_IDENTITIES_INPUT", errGuardProtocol)
		return errGuardProtocol
	}
	if err := runtimeMountIdentity(facts.rootfs, diagnostic); err != nil {
		diagnostic.reject("ADMIT_ROOTFS_IDENTITY", err)
		return err
	}
	for _, p := range []string{facts.hostnamePath, facts.hostsPath, facts.resolvPath} {
		if err := runtimeMountIdentity(p, diagnostic); err != nil {
			diagnostic.reject("ADMIT_SYSTEM_PATH_IDENTITY", err)
			return err
		}
	}
	return verifyRuntimeMountSources(facts, diagnostic)
}

func verifyRuntimeMountSources(facts *runtimeContainerFacts, diagnostic *admitDiagnostic) error {
	if facts == nil {
		return errGuardProtocol
	}
	for _, m := range facts.mounts {
		if err := runtimeMountIdentity(m.source, diagnostic); err != nil {
			diagnostic.reject("ADMIT_SOURCE_MOUNT_IDENTITY", err)
			return err
		}
	}
	foundOwnerSource := false
	for _, m := range facts.mounts {
		if m.source != ownerProbeSourcePath {
			continue
		}
		if foundOwnerSource || len(facts.mounts) != 1 || m.typ != "bind" || m.destination != ownerProbeTargetPath || !m.readonly || !facts.hasOwnerProbeSource {
			diagnostic.reject("ADMIT_SOURCE_MOUNT_IDENTITY", errGuardProtocol)
			return errGuardProtocol
		}
		foundOwnerSource = true
		identity, err := readOwnerProbeSourceIdentity()
		if err != nil {
			diagnostic.reject("ADMIT_SOURCE_MOUNT_IDENTITY", err)
			return err
		}
		if identity.mountID != facts.ownerProbeSource.mountID || !sameTrustedStat(identity.stat, facts.ownerProbeSource.stat) {
			diagnostic.reject("ADMIT_SOURCE_MOUNT_IDENTITY", errGuardProtocol)
			return errGuardProtocol
		}
	}
	if foundOwnerSource != facts.hasOwnerProbeSource {
		diagnostic.reject("ADMIT_SOURCE_MOUNT_IDENTITY", errGuardProtocol)
		return errGuardProtocol
	}
	return nil
}

func materializeRuntimeFacts(admitted *runtimeContainerFacts) (*runtimeContainerFacts, error) {
	if admitted == nil || admitted.runtimeReady {
		return nil, errGuardProtocol
	}
	candidate := *admitted
	if err := verifyMountAndRootfsIdentities(&candidate, nil); err != nil {
		return nil, errGuardProtocol
	}
	passwd, hasPasswd, err := readRootfsOptionalFile(candidate.rootfs, "etc/passwd", nil)
	if err != nil {
		return nil, errGuardProtocol
	}
	group, hasGroup, err := readRootfsOptionalFile(candidate.rootfs, "etc/group", nil)
	if err != nil {
		return nil, errGuardProtocol
	}
	candidate.passwdRaw, candidate.hasPasswd = passwd, hasPasswd
	candidate.groupRaw, candidate.hasGroup = group, hasGroup
	candidate.runtimeReady = true
	return &candidate, nil
}

func verifyRuntimeUserFiles(facts *runtimeContainerFacts) error {
	if facts == nil || !facts.runtimeReady {
		return errGuardProtocol
	}
	passwd, hasPasswd, err := readRootfsOptionalFile(facts.rootfs, "etc/passwd", nil)
	if err != nil || hasPasswd != facts.hasPasswd || !bytes.Equal(passwd, facts.passwdRaw) {
		return errGuardProtocol
	}
	group, hasGroup, err := readRootfsOptionalFile(facts.rootfs, "etc/group", nil)
	if err != nil || hasGroup != facts.hasGroup || !bytes.Equal(group, facts.groupRaw) {
		return errGuardProtocol
	}
	return nil
}

// verifyRuntimeBundle validates the held bundle directory and config.json
// against the complete source-derived OCI policy for the grant.
func verifyRuntimeBundle(b *authorityBroker, grant *brokerGrant, path string, directoryFD, configFD int, digest string) (*runtimeContainerFacts, unix.Stat_t, ownerProbeRuntimeDiagnostic, error) {
	var zero unix.Stat_t
	if grant == nil || grant.phase != "admitted" || grant.createBound || grant.facts == nil || grant.facts.runtimeReady || directoryFD < 0 || configFD < 0 || directoryFD == configFD || !runtimePath(path) || filepath.Base(path) != grant.containerID {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleInput}, errGuardProtocol
	}
	if _, ok := digestString(digest); !ok {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleInput}, errGuardProtocol
	}
	if b != nil {
		if err := checkDaemonConfigAbsent(nil); err != nil {
			return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleInput}, err
		}
	}
	namedDir, err := runtimeDirectory(path, false)
	if err != nil {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleDir}, errGuardProtocol
	}
	defer unix.Close(namedDir)
	var heldDirStat, namedDirStat unix.Stat_t
	if unix.Fstat(directoryFD, &heldDirStat) != nil || unix.Fstat(namedDir, &namedDirStat) != nil || !sameTrustedStat(heldDirStat, namedDirStat) {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleDir}, errGuardProtocol
	}
	var heldCfgStat unix.Stat_t
	if unix.Fstat(configFD, &heldCfgStat) != nil {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleConfig}, errGuardProtocol
	}
	dup, err := unix.FcntlInt(uintptr(configFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleConfig}, errGuardProtocol
	}
	cfgFile := os.NewFile(uintptr(dup), "config.json")
	if cfgFile == nil {
		_ = unix.Close(dup)
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleConfig}, errGuardProtocol
	}
	defer cfgFile.Close()
	if err := runtimeRecheckInput(directoryFD, path, "config.json", cfgFile, digest, heldCfgStat, false); err != nil {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleConfig}, errGuardProtocol
	}
	raw, err := readHeldRegular(configFD, runtimeInputLimit)
	if err != nil || hashBytes(raw) != digest {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleConfig}, errGuardProtocol
	}
	actual, err := runtimeJSON(raw)
	if err != nil {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleJSON}, errGuardProtocol
	}
	candidate, err := materializeRuntimeFacts(grant.facts)
	if err != nil {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleMaterialize}, err
	}
	prospectiveGrant := *grant
	prospectiveGrant.facts = candidate
	expected, err := expectedRuntimeSpec(b, &prospectiveGrant)
	if err != nil {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleExpected}, errGuardProtocol
	}
	if !reflect.DeepEqual(actual, expected) {
		return nil, zero, classifyRuntimeSpecMismatch(actual, expected), errGuardProtocol
	}
	if linuxSection, ok := actual["linux"].(map[string]any); !ok || validateRuntimeDeviceRules(linuxSection) != nil {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleDeviceRules}, errGuardProtocol
	}
	if err := verifyMountAndRootfsIdentities(candidate, nil); err != nil {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleIdentities}, errGuardProtocol
	}
	if err := verifyRuntimeUserFiles(candidate); err != nil {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleUserFiles}, errGuardProtocol
	}
	if err := runtimeRecheckInput(directoryFD, path, "config.json", cfgFile, digest, heldCfgStat, false); err != nil {
		return nil, zero, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleRecheck}, errGuardProtocol
	}
	return candidate, heldCfgStat, ownerProbeRuntimeDiagnostic{}, nil
}

//go:build linux && amd64 && !cgo

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type genesisFacts struct {
	RunToken                   string            `json:"run_token"`
	BootID                     string            `json:"boot_id"`
	SupervisorPID              int               `json:"supervisor_pid"`
	SupervisorStarttime        uint64            `json:"supervisor_starttime"`
	SupervisorExecutableSHA256 string            `json:"supervisor_executable_sha256"`
	SupervisorManifestSHA256   string            `json:"supervisor_manifest_sha256"`
	Programs                   map[string]string `json:"programs"`
	ChildCgroup                cgroupIdentity    `json:"child_cgroup"`
}

type appFacts struct {
	RunToken     string      `json:"run_token"`
	Application  application `json:"application"`
	DeploymentID string      `json:"deployment_id"`
	ProjectID    string      `json:"project_id"`
	Guard        struct {
		Device uint64 `json:"guard_device"`
		Inode  uint64 `json:"guard_inode"`
	} `json:"guard"`
}

type supervisorManifest struct {
	Version  uint64            `json:"version"`
	RunToken string            `json:"run_token"`
	Files    map[string]string `json:"files"`
}

func realmDigest(genesis, app []byte) string {
	h := sha256.New()
	for _, part := range [][]byte{[]byte("HOUFENG-C80-REALM-V1"), genesis, app} {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(part)))
		_, _ = h.Write(size[:])
		_, _ = h.Write(part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func checkedRootFile(path, expected string, requiredMode uint32) error {
	fd, err := openAbsolute(path, unix.O_RDONLY, true)
	if err != nil {
		return denied
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 || (requiredMode != 0 && st.Mode&07777 != requiredMode) || st.Mode&0022 != 0 {
		return denied
	}
	digest, err := hashFD(fd)
	if err != nil || digest != expected {
		return denied
	}
	return nil
}

func verifyRealm(dir int, permit launchPermit, config serviceConfig) (int, string, error) {
	var directory unix.Stat_t
	if unix.Fstat(dir, &directory) != nil || directory.Mode&unix.S_IFMT != unix.S_IFDIR || directory.Uid != 0 || directory.Gid != 0 || directory.Mode&07777 != 0700 {
		return -1, "", denied
	}
	genesisRaw, err := readGenesisAt(dir)
	if err != nil {
		return -1, "", denied
	}
	appRaw, err := readPrivateAt(dir, "realm-app.json", 0, 0)
	if err != nil || realmDigest(genesisRaw, appRaw) != permit.RealmDigest {
		return -1, "", denied
	}
	var genesis genesisFacts
	var app appFacts
	if genesisDocument(genesisRaw, &genesis) != nil || realmDocument(appRaw, &app) != nil || genesis.RunToken != runToken || app.RunToken != runToken || genesis.BootID != permit.BootID || genesis.SupervisorPID != permit.Supervisor.PID || genesis.SupervisorStarttime != permit.Supervisor.Starttime || genesis.ChildCgroup != permit.ChildCgroup || genesis.Programs["c80-launcher"] != permit.LauncherSHA256 || genesis.Programs["c80-predecessor"] != permit.PredecessorSHA256 || !validDigest(genesis.Programs["houfeng-record-platform-admin"]) || !validDigest(genesis.SupervisorExecutableSHA256) || !validDigest(genesis.SupervisorManifestSHA256) || app.Application != permit.Application || app.DeploymentID != config.DeploymentID || app.ProjectID != config.ProjectID || app.Guard.Device != permit.Guard.Dev || app.Guard.Inode != permit.Guard.Ino {
		return -1, "", denied
	}
	currentBoot, err := bootID()
	if err != nil || currentBoot != permit.BootID {
		return -1, "", denied
	}
	manifestFD, err := openAbsolute("/opt/g2/realm-supervisor/manifest.json", unix.O_RDONLY, true)
	if err != nil {
		return -1, "", denied
	}
	manifestRaw, err := boundedFD(manifestFD, 0, 0, 0400, maxFrame)
	unix.Close(manifestFD)
	if err != nil || sha(manifestRaw) != genesis.SupervisorManifestSHA256 {
		return -1, "", denied
	}
	var manifest supervisorManifest
	fields, err := exactObject(manifestRaw, "version", "run_token", "files")
	if err != nil || json.Unmarshal(manifestRaw, &manifest) != nil || fields["files"] == nil || manifest.Version != 1 || manifest.RunToken != runToken || manifest.Files["source_guest.py"] != permit.Supervisor.ProgramSHA256 || checkedRootFile("/opt/g2/realm-supervisor/source_guest.py", permit.Supervisor.ProgramSHA256, 0400) != nil || checkedRootFile("/run/g2/source_guest.py", permit.Supervisor.ProgramSHA256, 0) != nil {
		return -1, "", denied
	}
	pidfd, err := openLive(permit.Supervisor.PID, permit.Supervisor.Starttime)
	if err != nil {
		return -1, "", denied
	}
	status, err := processStatus(permit.Supervisor.PID, "Uid", "Gid")
	exe, exeErr := os.Readlink(fmt.Sprintf("/proc/%d/exe", permit.Supervisor.PID))
	cmdline, cmdErr := readProc(fmt.Sprintf("/proc/%d/cmdline", permit.Supervisor.PID), 4096)
	if err != nil || status["Uid"] != "0\t0\t0\t0" && strings.Join(strings.Fields(status["Uid"]), " ") != "0 0 0 0" || strings.Join(strings.Fields(status["Gid"]), " ") != "0 0 0 0" || exeErr != nil || !strings.HasPrefix(exe, "/usr/bin/python3") || strings.Contains(exe, " (deleted)") || procExecutable(permit.Supervisor.PID, exe, genesis.SupervisorExecutableSHA256) != nil || cmdErr != nil || string(cmdline) != "/usr/bin/python3\x00-B\x00/run/g2/source_guest.py\x00" || !live(pidfd) {
		unix.Close(pidfd)
		return -1, "", denied
	}
	return pidfd, genesis.Programs["houfeng-record-platform-admin"], nil
}

func ensureInheritable(fd int) bool {
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	return err == nil && flags&unix.FD_CLOEXEC == 0
}

func fstatType(fd int, kind uint32) (unix.Stat_t, error) {
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != kind || !ensureInheritable(fd) {
		return st, denied
	}
	return st, nil
}

func parentHasReadEnd(parent int, target unix.Stat_t) bool {
	dir := fmt.Sprintf("/proc/%d/fd", parent)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 65536 {
		return false
	}
	for _, entry := range entries {
		number, err := strconv.Atoi(entry.Name())
		if err != nil || number < 0 {
			return false
		}
		var st unix.Stat_t
		if unix.Stat(fmt.Sprintf("%s/%d", dir, number), &st) != nil || st.Dev != target.Dev || st.Ino != target.Ino || st.Mode&unix.S_IFMT != unix.S_IFIFO {
			continue
		}
		body, err := readProc(fmt.Sprintf("/proc/%d/fdinfo/%d", parent, number), 4096)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(body), "\n") {
			if !strings.HasPrefix(line, "flags:") {
				continue
			}
			flags, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "flags:")), 8, 32)
			if err == nil && flags&unix.O_ACCMODE == unix.O_RDONLY {
				return true
			}
		}
	}
	return false
}

func verifyStandardFDs(parent int) error {
	in, err := fstatType(0, unix.S_IFCHR)
	if err != nil {
		return denied
	}
	var null unix.Stat_t
	if unix.Stat("/dev/null", &null) != nil || null.Mode&unix.S_IFMT != unix.S_IFCHR || in.Rdev != null.Rdev || in.Dev != null.Dev || in.Ino != null.Ino {
		return denied
	}
	flags, err := unix.FcntlInt(0, unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
		return denied
	}
	out, err := fstatType(1, unix.S_IFIFO)
	if err != nil {
		return denied
	}
	errout, err := fstatType(2, unix.S_IFIFO)
	if err != nil || out.Dev == errout.Dev && out.Ino == errout.Ino {
		return denied
	}
	for fd, st := range map[int]unix.Stat_t{1: out, 2: errout} {
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil || flags&unix.O_ACCMODE != unix.O_WRONLY || !parentHasReadEnd(parent, st) {
			return denied
		}
	}
	return nil
}

func verifyConfigFD(parent int, parentFD int, config serviceConfig, digest string) error {
	link, err := os.Readlink("/proc/self/fd/4")
	if err != nil || !strings.HasPrefix(link, "/memfd:") || !strings.HasSuffix(link, " (deleted)") || !ensureInheritable(4) {
		return denied
	}
	var st unix.Stat_t
	if unix.Fstat(4, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size <= 0 || st.Size > maxFrame {
		return denied
	}
	seals, err := unix.FcntlInt(4, unix.F_GET_SEALS, 0)
	if err != nil || seals&(unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL) != unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL {
		return denied
	}
	if pos, err := unix.Seek(4, 0, 1); err != nil || pos != 0 {
		return denied
	}
	body := make([]byte, int(st.Size))
	for offset := 0; offset < len(body); {
		n, err := unix.Pread(4, body[offset:], int64(offset))
		if err == unix.EINTR {
			continue
		}
		if err != nil || n <= 0 {
			return denied
		}
		offset += n
	}
	actual, err := parseConfig(body)
	if err != nil || actual != config || sha(body) != digest {
		return denied
	}
	path := fmt.Sprintf("/proc/%d/fd/%d", parent, parentFD)
	parentLink, err := os.Readlink(path)
	if err != nil || parentLink != link {
		return denied
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return denied
	}
	defer unix.Close(fd)
	if !sameFD(fd, 4) {
		return denied
	}
	return nil
}

func verifyGuard(pin fileIdentity, parent, parentPIDFD int) error {
	inherited, err := fstatType(3, unix.S_IFREG)
	if err != nil || inherited.Uid != serviceUID || inherited.Gid != serviceGID || inherited.Mode&07777 != 0600 || inherited.Nlink != 1 || uint64(inherited.Dev) != pin.Dev || inherited.Ino != pin.Ino {
		return denied
	}
	fd, err := openAbsolute(guardPath, unix.O_RDONLY, false)
	if err != nil {
		return denied
	}
	defer unix.Close(fd)
	if !sameFD(fd, 3) {
		return denied
	}
	// The independent description must conflict first. Acquiring through FD3
	// must then succeed, proving that FD3 is the locking description rather
	// than merely another open of the same inode. Never unlock either one.
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK && err != unix.EAGAIN {
		return denied
	}
	if err := unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB); err != unix.EWOULDBLOCK && err != unix.EAGAIN {
		return denied
	}
	if err := unix.Flock(3, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return denied
	}
	if err := unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB); err != unix.EWOULDBLOCK && err != unix.EAGAIN {
		return denied
	}
	// The pinned coordinator must still retain a descriptor for this guard.
	// The conflicting independent open and successful FD3 reassertion above
	// distinguish its held locking description from a separate open.
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", parent))
	if err != nil || len(entries) > 65536 {
		return denied
	}
	for _, entry := range entries {
		number, err := strconv.Atoi(entry.Name())
		if err != nil || number < 0 {
			return denied
		}
		var candidate unix.Stat_t
		if unix.Stat(fmt.Sprintf("/proc/%d/fd/%d", parent, number), &candidate) == nil && candidate.Dev == inherited.Dev && candidate.Ino == inherited.Ino && candidate.Mode&unix.S_IFMT == unix.S_IFREG && live(parentPIDFD) && os.Getppid() == parent {
			return nil
		}
	}
	return denied
}

func verifySocket(parent int) error {
	if _, err := fstatType(6, unix.S_IFSOCK); err != nil {
		return denied
	}
	typ, err := unix.GetsockoptInt(6, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || typ != unix.SOCK_SEQPACKET {
		return denied
	}
	family, err := unix.GetsockoptInt(6, unix.SOL_SOCKET, unix.SO_DOMAIN)
	if err != nil || family != unix.AF_UNIX {
		return denied
	}
	peer, err := unix.GetsockoptUcred(6, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer == nil || int(peer.Pid) != parent || peer.Uid != serviceUID || peer.Gid != serviceGID {
		return denied
	}
	return nil
}

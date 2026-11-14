//go:build linux && amd64 && !cgo

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func configFromFD() (serviceConfig, error) {
	var st unix.Stat_t
	if unix.Fstat(4, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size <= 0 || st.Size > maxFrame {
		return serviceConfig{}, denied
	}
	body := make([]byte, int(st.Size))
	for offset := 0; offset < len(body); {
		n, err := unix.Pread(4, body[offset:], int64(offset))
		if err == unix.EINTR {
			continue
		}
		if err != nil || n <= 0 {
			return serviceConfig{}, denied
		}
		offset += n
	}
	return parseConfig(body)
}

func sameExecutable(fd int, pid int) bool {
	procfd, err := unix.Open(fmt.Sprintf("/proc/%d/exe", pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	defer unix.Close(procfd)
	return sameFD(fd, procfd)
}

func emptyCgroup(pin cgroupIdentity) error {
	dir, err := openDirectory(childCgroupPath, true)
	if err != nil {
		return denied
	}
	defer unix.Close(dir)
	var st unix.Stat_t
	if unix.Fstat(dir, &st) != nil || st.Uid != 0 || st.Gid != 0 || uint64(st.Dev) != pin.Dev || st.Ino != pin.Ino {
		return denied
	}
	procs, err := readProc(childCgroupPath+"/cgroup.procs", 65536)
	if err != nil || len(strings.TrimSpace(string(procs))) != 0 {
		return denied
	}
	events, err := readProc(childCgroupPath+"/cgroup.events", 4096)
	if err != nil {
		return denied
	}
	found := false
	for _, line := range strings.Split(string(events), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "populated" {
			if found || fields[1] != "0" {
				return denied
			}
			found = true
		}
	}
	if !found {
		return denied
	}
	return nil
}

func run() error {
	// Unshare and the final exec must run on the same OS thread. This also
	// makes the parent's death signal apply to the actual executing thread.
	runtime.LockOSThread()
	if len(os.Args) != 2 || os.Args[0] != launcherPath || os.Args[1] != "service-preview-execute" {
		return denied
	}
	// A setuid Go runtime synthesizes GOTRACEBACK=none in os.Environ even
	// for an empty exec environment. Procfs exposes the original exec bytes.
	inherited, err := readProc("/proc/self/environ", maxFrame)
	if err != nil || len(inherited) != 0 {
		return denied
	}
	ruid, euid, suid := unix.Getresuid()
	rgid, egid, sgid := unix.Getresgid()
	if ruid != serviceUID || euid != 0 || suid != 0 || rgid != serviceGID || egid != serviceGID || sgid != serviceGID {
		return denied
	}
	parent := os.Getppid()
	if parent <= 1 || unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0) != nil || os.Getppid() != parent {
		return denied
	}
	if unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}) != nil {
		return denied
	}
	// A cwd inherited inside a later-masked tree would retain a bypass to it.
	if unix.Chdir("/") != nil {
		return denied
	}
	// CLOEXEC is applied instead of closing arbitrary FDs while Go's runtime is
	// still running: they may include its poller. Exec closes every descriptor
	// except the six fixed channels; no descriptor becomes child authority.
	if unix.CloseRange(5, 5, unix.CLOSE_RANGE_CLOEXEC) != nil || unix.CloseRange(7, ^uint(0), unix.CLOSE_RANGE_CLOEXEC) != nil {
		return denied
	}
	config, err := configFromFD()
	if err != nil {
		return denied
	}
	realm, err := openDirectory(realmPath, true)
	if err != nil {
		return denied
	}
	defer unix.Close(realm)
	raw, err := readPrivateAt(realm, fmt.Sprintf("launch-authority-%020d.json", config.PhaseGeneration), 0, 0)
	if err != nil {
		return denied
	}
	intent, permit, err := parseIntent(raw, true)
	if err != nil || intent.PhaseGeneration != config.PhaseGeneration || intent.ActorUserID != config.ActorUserID || intent.RecordID != config.RecordID || intent.Coordinator.PID != parent {
		return denied
	}
	supervisorFD, adminSHA, err := verifyRealm(realm, permit, config)
	if err != nil {
		return denied
	}
	defer unix.Close(supervisorFD)
	parentFD, err := coordinatorIdentity(intent.Coordinator, adminSHA)
	if err != nil {
		return denied
	}
	defer unix.Close(parentFD)
	adminFD, err := checkedProgram(adminPath, adminSHA, 0550, serviceGID)
	if err != nil {
		return denied
	}
	if !sameExecutable(adminFD, parent) {
		unix.Close(adminFD)
		return denied
	}
	unix.Close(adminFD)
	selfFD, err := checkedProgram(launcherPath, intent.LauncherSHA256, 04750, serviceGID)
	if err != nil {
		return denied
	}
	if !sameExecutable(selfFD, os.Getpid()) {
		unix.Close(selfFD)
		return denied
	}
	unix.Close(selfFD)
	predecessorFD, err := checkedProgram(predecessorPath, intent.PredecessorSHA256, 0550, serviceGID)
	if err != nil {
		return denied
	}
	defer unix.Close(predecessorFD)
	ledger, err := openDirectory(ledgerPath, false)
	if err != nil {
		return denied
	}
	defer unix.Close(ledger)
	if verifyPhaseChain(ledger, intent) != nil || verifyConfigFD(parent, intent.ConfigFD, config, intent.ConfigDigest) != nil || verifyGuard(intent.Guard, parent, parentFD) != nil || verifySocket(parent) != nil || verifyStandardFDs(parent) != nil || emptyCgroup(intent.ChildCgroup) != nil || verifyRootJournal(realm, intent) != nil || !live(parentFD) || !live(supervisorFD) {
		return denied
	}
	// This durable O_EXCL marker permanently consumes this phase even if any
	// subsequent sandbox setup or exec fails. Recovery must refence first.
	if consumePermit(realm, permit) != nil || joinChildCgroup(intent.ChildCgroup) != nil || mountSandbox() != nil || dropCredentials() != nil {
		return denied
	}
	if verifyMasks() != nil || verifyPhaseChain(ledger, intent) != nil || verifyChildCgroup(intent.ChildCgroup) != nil || !live(parentFD) || !live(supervisorFD) || os.Getppid() != parent {
		return denied
	}
	if actual, err := processStart(parent); err != nil || actual != intent.Coordinator.Starttime || !coordinatorProtected(parent) || coordinatorThreads(parent) != nil {
		return denied
	}
	if actual, err := processStart(permit.Supervisor.PID); err != nil || actual != permit.Supervisor.Starttime {
		return denied
	}
	lastFD, err := checkedProgram(predecessorPath, intent.PredecessorSHA256, 0550, serviceGID)
	if err != nil {
		return denied
	}
	if !sameFD(predecessorFD, lastFD) {
		unix.Close(lastFD)
		return denied
	}
	unix.Close(lastFD)
	if unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0) != nil || os.Getppid() != parent || !live(parentFD) || !live(supervisorFD) {
		return denied
	}
	if actual, err := processStart(parent); err != nil || actual != intent.Coordinator.Starttime {
		return denied
	}
	if !ensureInheritable(3) || !ensureInheritable(4) || !ensureInheritable(6) || unix.CloseRange(5, 5, unix.CLOSE_RANGE_CLOEXEC) != nil || unix.CloseRange(7, ^uint(0), unix.CLOSE_RANGE_CLOEXEC) != nil {
		return denied
	}
	return syscall.Exec(predecessorPath, []string{predecessorPath, "service-preview-execute"}, []string{"LANG=C", "LC_ALL=C"})
}

func main() {
	if run() != nil {
		_, _ = unix.Write(2, []byte("launch denied\n"))
		os.Exit(1)
	}
}

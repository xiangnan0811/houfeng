//go:build linux && amd64 && !cgo

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type observedProcess struct {
	PID, PPID, Tracer                          uint32
	StartTicks                                 uint64
	State                                      byte
	UID, GID                                   uint32
	Cgroup                                     string
	Argv                                       []byte
	ArgvSHA256, ExecutableSHA256, CgroupSHA256 string
}

func boundedProc(pid uint32, name string, limit int64) ([]byte, error) {
	if pid == 0 || name == "" || strings.Contains(name, "/") || limit <= 0 {
		return nil, errGuardProtocol
	}
	return readHeldProc(fmt.Sprintf("%d/%s", pid, name), limit)
}

func observeProcess(pid uint32, pidfd int) (observedProcess, error) {
	return observeProcessCore(pid, pidfd, -1)
}

// observeProcessWithHeldExecutable is only for the authenticated PID1 startup
// witness: PID1 acquired this descriptor from the original observer's proc exe.
func observeProcessWithHeldExecutable(pid uint32, pidfd, executableFD int) (observedProcess, error) {
	if executableFD < 0 || !pidfdSelects(pidfd, pid) {
		return observedProcess{}, errGuardProtocol
	}
	out, err := observeProcessCore(pid, pidfd, executableFD)
	if err != nil {
		return out, err
	}
	if !pidfdSelects(pidfd, pid) {
		return observedProcess{}, errGuardProtocol
	}
	return out, nil
}

func observeProcessCore(pid uint32, pidfd, executableFD int) (observedProcess, error) {
	var out observedProcess
	if pid == 0 || !livePIDFD(pidfd) {
		return out, errGuardProtocol
	}
	start, err := procStartTicks(int(pid))
	if err != nil {
		return out, err
	}
	status, err := boundedProc(pid, "status", 65536)
	if err != nil {
		if g2d1.role == 'R' {
			g2d1Fail("PROCESS_STATUS", err)
		}
		return out, err
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(status), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if _, exists := values[key]; exists {
			return out, errGuardProtocol
		}
		values[key] = strings.TrimSpace(value)
	}
	parse := func(key string) (uint32, error) {
		fields := strings.Fields(values[key])
		if len(fields) == 0 {
			return 0, errGuardProtocol
		}
		n, e := strconv.ParseUint(fields[0], 10, 32)
		if e != nil {
			return 0, errGuardProtocol
		}
		return uint32(n), nil
	}
	observedPID, err := parse("Tgid")
	if err != nil || observedPID != pid {
		return out, errGuardProtocol
	}
	out.PID = pid
	if out.PPID, err = parse("PPid"); err != nil {
		return observedProcess{}, err
	}
	if out.Tracer, err = parse("TracerPid"); err != nil {
		return observedProcess{}, err
	}
	if out.UID, err = parse("Uid"); err != nil {
		return observedProcess{}, err
	}
	if out.GID, err = parse("Gid"); err != nil {
		return observedProcess{}, err
	}
	for _, key := range []string{"Uid", "Gid"} {
		ids := strings.Fields(values[key])
		if len(ids) != 4 {
			return observedProcess{}, errGuardProtocol
		}
		for _, id := range ids {
			if id != "0" {
				return observedProcess{}, errGuardProtocol
			}
		}
	}
	state := values["State"]
	if state == "" {
		return observedProcess{}, errGuardProtocol
	}
	out.State = state[0]
	argv, err := boundedProc(pid, "cmdline", 65536)
	if err != nil {
		if g2d1.role == 'R' {
			g2d1Fail("PROCESS_CMDLINE", err)
		}
		return observedProcess{}, err
	}
	cgroup, err := boundedProc(pid, "cgroup", 65536)
	if err != nil {
		if g2d1.role == 'R' {
			g2d1Fail("PROCESS_CGROUP", err)
		}
		return observedProcess{}, err
	}
	text := strings.TrimSuffix(string(cgroup), "\n")
	if !strings.HasPrefix(text, "0::/") || strings.Contains(text, "\n") {
		return observedProcess{}, errGuardProtocol
	}
	out.Cgroup = strings.TrimPrefix(text, "0::")
	out.Argv = argv
	h := sha256.Sum256(argv)
	out.ArgvSHA256 = hex.EncodeToString(h[:])
	h = sha256.Sum256(cgroup)
	out.CgroupSHA256 = hex.EncodeToString(h[:])
	if executableFD < 0 {
		out.ExecutableSHA256, err = procExecutableHash(int(pid))
	} else {
		out.ExecutableSHA256, _, err = heldRegularDigest(executableFD, 128<<20)
	}
	if err != nil {
		if g2d1.role == 'R' {
			g2d1Fail("PROCESS_EXECUTABLE", err)
		}
		return observedProcess{}, err
	}
	again, err := procStartTicks(int(pid))
	if err != nil || again != start || !livePIDFD(pidfd) {
		return observedProcess{}, errGuardProtocol
	}
	out.StartTicks = start
	return out, nil
}

func pidfdSelects(fd int, pid uint32) bool {
	selected, _ := pidfdSelectsObserved(fd, pid, nil)
	return selected
}

func pidfdSelectsObserved(fd int, pid uint32, observation *selectorObservation) (bool, error) {
	if fd < 0 {
		if observation != nil {
			*observation = selectorObservation{reason: selectorInvalidFD}
		}
		return false, nil
	}
	if pid == 0 {
		if observation != nil {
			*observation = selectorObservation{reason: selectorZeroPID}
		}
		return false, nil
	}
	if selected, pollErr := livePIDFDObserved(fd, observation, selectorPollPre); !selected {
		return false, pollErr
	}
	raw, err := readHeldProc(fmt.Sprintf("self/fdinfo/%d", fd), 65536)
	if err != nil {
		if observation != nil {
			*observation = selectorObservation{reason: selectorFDInfoRead, err: err}
		}
		return false, nil
	}
	found := false
	for _, line := range strings.Split(string(raw), "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok || key != "Pid" {
			continue
		}
		if found {
			if observation != nil {
				*observation = selectorObservation{reason: selectorPIDDuplicate}
			}
			return false, nil
		}
		found = true
		n, err := strconv.ParseUint(strings.TrimSpace(val), 10, 32)
		if err != nil {
			if observation != nil {
				*observation = selectorObservation{reason: selectorPIDParse}
			}
			return false, nil
		}
		if uint32(n) != pid {
			if observation != nil {
				*observation = selectorObservation{reason: selectorPIDMismatch}
			}
			return false, nil
		}
	}
	if !found {
		if observation != nil {
			*observation = selectorObservation{reason: selectorPIDMissing}
		}
		return false, nil
	}
	return livePIDFDObserved(fd, observation, selectorPollPost)
}

func heldRegularDigest(fd int, limit int64) (string, unix.Stat_t, error) {
	return heldRegularDigestWithDiagnostic(fd, limit, nil)
}

func heldRegularDigestWithDiagnostic(fd int, limit int64, d *runtimeInputDiagnostic) (string, unix.Stat_t, error) {
	var before, after unix.Stat_t
	if fd < 0 {
		d.reject(ownerProbeStageWrapperPrepareDigestFD)
		return "", before, errGuardProtocol
	}
	if unix.Fstat(fd, &before) != nil {
		d.reject(ownerProbeStageWrapperPrepareDigestStat)
		return "", before, errGuardProtocol
	}
	if stage := heldRegularDigestMetadataStage(before, limit); stage != 0 {
		d.reject(stage)
		return "", before, errGuardProtocol
	}
	h := sha256.New()
	data := make([]byte, 128*1024)
	var offset int64
	for offset < before.Size {
		want := len(data)
		if int64(want) > before.Size-offset {
			want = int(before.Size - offset)
		}
		n, err := unix.Pread(fd, data[:want], offset)
		if err != nil {
			d.reject(ownerProbeStageWrapperPrepareDigestRead)
			return "", before, errGuardProtocol
		}
		if n <= 0 {
			d.reject(ownerProbeStageWrapperPrepareDigestCount)
			return "", before, errGuardProtocol
		}
		_, _ = h.Write(data[:n])
		offset += int64(n)
	}
	if unix.Fstat(fd, &after) != nil {
		d.reject(ownerProbeStageWrapperPrepareDigestAfterStat)
		return "", before, errGuardProtocol
	}
	if difference := trustedStatDifference(before, after); difference != runtimeStatSame {
		d.rejectDifference(ownerProbeStageWrapperPrepareDigestAfterDev, difference)
		return "", before, errGuardProtocol
	}
	return hex.EncodeToString(h.Sum(nil)), before, nil
}

func verifyHeldScript(fd int, name, expected string) error {
	digest, identity, err := heldRegularDigest(fd, 16<<20)
	if err != nil || digest != expected {
		return errGuardProtocol
	}
	named, actual, err := openTrustedRegular(name, 16<<20)
	if err != nil {
		return err
	}
	defer named.Close()
	var st unix.Stat_t
	if unix.Fstat(int(named.Fd()), &st) != nil || !sameTrustedStat(identity, st) || actual != expected {
		return errGuardProtocol
	}
	return nil
}

func verifyProcessExecutable(pid uint32, name, expected string) error {
	program, digest, err := openTrustedRegular(name, 128<<20)
	if err != nil {
		return err
	}
	defer program.Close()
	if digest != expected {
		return errGuardProtocol
	}
	var named, actual unix.Stat_t
	fd, err := unix.Openat(heldProcDirectory, fmt.Sprintf("%d/exe", pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if unix.Fstat(int(program.Fd()), &named) != nil || unix.Fstat(fd, &actual) != nil || !sameTrustedStat(named, actual) {
		return errGuardProtocol
	}
	return nil
}

func verifyLaunchProcessPair(manifest bootstrapManifest, kind string, cred *unix.Ucred, launcherPIDFD, childPIDFD int, childPID uint32, childStart uint64, scriptFD int) (observedProcess, observedProcess, error) {
	var empty observedProcess
	if cred == nil || cred.Pid <= 1 || cred.Uid != 0 || cred.Gid != 0 || !pidfdSelects(launcherPIDFD, uint32(cred.Pid)) || !pidfdSelects(childPIDFD, childPID) {
		return empty, empty, errGuardProtocol
	}
	parent, err := observeProcess(uint32(cred.Pid), launcherPIDFD)
	if err != nil {
		return empty, empty, err
	}
	child, err := observeProcess(childPID, childPIDFD)
	if err != nil {
		return empty, empty, err
	}
	launcherPath, scriptPath, cgroup, role := "/opt/g2-launch/g2-bpf-authority", "/run/g2/source_guest.py", "/system.slice/cloud-final.service", "source_guest"
	if kind == "reboot" {
		launcherPath, scriptPath, cgroup, role = "/opt/g2/g2-bpf-authority", "/opt/g2/realm-supervisor/reboot_observer.py", "/system.slice/g2-reboot-observer.service", "reboot_observer"
	} else if kind != "source" {
		return empty, empty, errGuardProtocol
	}
	if parent.UID != 0 || parent.GID != 0 || parent.ExecutableSHA256 != manifest.BrokerSHA256 || parent.Cgroup != cgroup || !exactArgumentVector(parent.Argv, []string{launcherPath, "launch-" + kind}) {
		return empty, empty, errGuardProtocol
	}
	if child.UID != 0 || child.GID != 0 || child.PPID != parent.PID || child.Tracer != parent.PID || child.State != 't' || child.StartTicks != childStart || child.Cgroup != parent.Cgroup || child.ExecutableSHA256 != manifest.Executables["python"].SHA256 || !exactArgumentVector(child.Argv, []string{"/usr/bin/python3", "-B", scriptPath}) {
		return empty, empty, errGuardProtocol
	}
	if err = verifyProcessExecutable(parent.PID, launcherPath, manifest.BrokerSHA256); err != nil {
		return empty, empty, err
	}
	if err = verifyProcessExecutable(child.PID, manifest.Executables["python"].Path, manifest.Executables["python"].SHA256); err != nil {
		return empty, empty, err
	}
	if err = verifyHeldScript(scriptFD, scriptPath, manifest.Scripts[role]); err != nil {
		return empty, empty, err
	}
	if !livePIDFD(launcherPIDFD) || !livePIDFD(childPIDFD) {
		return empty, empty, errGuardProtocol
	}
	return parent, child, nil
}

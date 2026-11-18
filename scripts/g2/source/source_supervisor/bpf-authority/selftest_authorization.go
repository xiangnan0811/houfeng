//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

func asmProbeBarrierRead(fd int64, bufPtr uintptr) int64
func asmProbeBPF(cmd int64, attrPtr uintptr, size uintptr) int64
func asmProbeDup2AndBPF(oldfd int64, newfd int64, cmd int64, attrPtr uintptr, size uintptr) int64
func asmProbeNonleaderThread(flags uintptr, childStack uintptr, attrPtr uintptr, results uintptr, unshareFiles uintptr) int64
func asmProbeLeaderExit(flags uintptr, childStack uintptr, attrPtr uintptr, results uintptr) int64
func asmProbeForkExit() int64
func asmProbeClone3SetTID(clArgsPtr uintptr, clArgsSize uintptr, cmd int64, attrPtr uintptr, size uintptr, resultPtr uintptr) int64

// waitProbeBarrierOnFD8 blocks on FD 8 if FD 8 is a pipe, allowing the parent
// to register the stopped/blocked probe actor via REGISTER_SELFTEST_PROBE before
// the first syscall executes.
func waitProbeBarrierOnFD8() error {
	var st unix.Stat_t
	if err := unix.Fstat(8, &st); err != nil {
		return nil
	}
	if st.Mode&unix.S_IFMT != unix.S_IFIFO {
		return nil
	}
	var b [1]byte
	for {
		n := asmProbeBarrierRead(8, uintptr(unsafe.Pointer(&b[0])))
		if n == -int64(unix.EINTR) {
			continue
		}
		if n != 1 || b[0] != 0x5a {
			return errGuardProtocol
		}
		return nil
	}
}

// runAuthoritySelftest is an unprivileged requester of fixed denied BPF calls
// or assembly thread/FD probes. It receives no broker token and is never a
// second BPF executor.
func runAuthoritySelftest(which string) error {
	if which == "container-raw-denial" {
		return runContainerRawDenialSelftest()
	}
	switch which {
	case "bpf-authorization", "raw-denial", "post-death-raw":
	case "fd-slot-reuse", "nonleader-files", "shared-files", "leader-exit", "tid-reuse", "ring-overflow-filler", "seal-detach-denied", "seal-unload-denied", "extra-fd-delays-removal", "audit-overflow-bind-filler":
		return runAssemblySelftestProbe(which)
	default:
		return fmt.Errorf("BPF_AUTH_INCOMPATIBLE: unsupported selftest")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if len(os.Args) != 3 || os.Geteuid() != 0 || unix.Gettid() != os.Getpid() {
		return errGuardProtocol
	}
	var uts unix.Utsname
	if unix.Uname(&uts) != nil || unix.ByteSliceToString(uts.Release[:]) != "6.8.0-147-generic" {
		return errGuardProtocol
	}
	caseRaw, err := readHeldRegular(4, 4096)
	if err != nil {
		return err
	}
	caseData, err := readCanonicalLine(caseRaw)
	if err != nil || caseData["case"] != "realm-negative" || (caseData["scenario"] != "bpf-authorization" && caseData["scenario"] != "bpf-broker-kill") || caseData["run_token"] != authorityRunToken {
		return errGuardProtocol
	}
	manifestObject, ok := caseData["bpf_bootstrap"].(map[string]any)
	if !ok {
		return errGuardProtocol
	}
	manifestRaw, err := canonicalControl(manifestObject)
	if err != nil {
		return err
	}
	manifest, err := parseBootstrapManifest(manifestRaw)
	if err != nil {
		return err
	}
	facts, err := verifySourceCarrier(3, 4, manifest)
	if err != nil || facts["case"] != "realm-negative" || (facts["scenario"] != "bpf-authorization" && facts["scenario"] != "bpf-broker-kill") || facts["run_token"] != authorityRunToken {
		return errGuardProtocol
	}
	if err = verifyHeldScript(5, "/run/g2/source_guest.py", manifest.Scripts["source_guest"]); err != nil {
		return err
	}
	if err = verifyProcessExecutable(uint32(os.Getpid()), "/opt/g2/g2-bpf-authority", manifest.BrokerSHA256); err != nil {
		return err
	}
	parentPID := uint32(os.Getppid())
	if parentPID <= 1 || !pidfdSelects(6, parentPID) {
		return errGuardProtocol
	}
	parent, err := observeProcess(parentPID, 6)
	if err != nil || parent.UID != 0 || parent.GID != 0 || parent.Tracer != 0 || parent.Cgroup != "/system.slice/cloud-final.service" || parent.ExecutableSHA256 != manifest.Executables["python"].SHA256 || !exactArgumentVector(parent.Argv, []string{"/usr/bin/python3", "-B", "/run/g2/source_guest.py"}) {
		return errGuardProtocol
	}
	parentDigest, parentHeld, err := heldRegularDigest(7, 128<<20)
	var parentExe unix.Stat_t
	if err != nil || parentDigest != parent.ExecutableSHA256 || unix.Stat(fmt.Sprintf("/proc/%d/exe", parentPID), &parentExe) != nil || !sameTrustedStat(parentHeld, parentExe) || verifyProcessExecutable(parentPID, manifest.Executables["python"].Path, manifest.Executables["python"].SHA256) != nil {
		return errGuardProtocol
	}
	launcherFD, err := unix.PidfdOpen(int(parent.PPID), 0)
	if err != nil {
		return err
	}
	defer unix.Close(launcherFD)
	launcher, err := observeProcess(parent.PPID, launcherFD)
	if err != nil || launcher.UID != 0 || launcher.GID != 0 || launcher.Cgroup != parent.Cgroup || launcher.ExecutableSHA256 != manifest.BrokerSHA256 || !exactArgumentVector(launcher.Argv, []string{"/opt/g2-launch/g2-bpf-authority", "launch-source"}) || verifyProcessExecutable(parent.PPID, "/opt/g2-launch/g2-bpf-authority", manifest.BrokerSHA256) != nil {
		return errGuardProtocol
	}
	var moduleStat, guardStat unix.Stat_t
	if unix.Stat("/sys/module/g2_bpf_guard", &moduleStat) != nil || moduleStat.Mode&unix.S_IFMT != unix.S_IFDIR || unix.Stat("/dev/g2_bpf_guard", &guardStat) != nil || guardStat.Mode&unix.S_IFMT != unix.S_IFCHR || guardStat.Uid != 0 || guardStat.Gid != 0 || guardStat.Mode&07777 != 0600 {
		return errGuardProtocol
	}
	lsm, err := os.ReadFile("/sys/kernel/security/lsm")
	if err != nil || len(lsm) == 0 || len(lsm) > 1024 {
		return errGuardProtocol
	}
	bpfLSM := false
	for _, item := range strings.Split(strings.TrimSpace(string(lsm)), ",") {
		if item == "bpf" {
			bpfLSM = true
		}
	}
	if !bpfLSM {
		return errGuardProtocol
	}
	selfPIDFD, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		return err
	}
	defer unix.Close(selfPIDFD)
	self, err := observeProcess(uint32(os.Getpid()), selfPIDFD)
	if err != nil || self.PPID != parentPID || self.Tracer != 0 || self.StartTicks == 0 || !pidfdSelects(6, parentPID) {
		return errGuardProtocol
	}
	if err = waitProbeBarrierOnFD8(); err != nil {
		return err
	}
	actor := map[string]any{"tgid": jsonU64(uint64(os.Getpid())), "tid": jsonU64(uint64(unix.Gettid())), "start_ticks": jsonU64(self.StartTicks), "parent_tgid": jsonU64(uint64(parentPID))}
	mapAttr := make([]byte, 72)
	guardPut32(mapAttr, 0, unix.BPF_MAP_TYPE_ARRAY)
	guardPut32(mapAttr, 4, 4)
	guardPut32(mapAttr, 8, 4)
	guardPut32(mapAttr, 12, 1)
	copy(mapAttr[28:44], "feature_test")
	program, err := probeProgram()
	if err != nil || len(program) != 16 {
		return errGuardProtocol
	}
	license := []byte{'G', 'P', 'L', 0}
	programAttr := make([]byte, 72)
	guardPut32(programAttr, 0, unix.BPF_PROG_TYPE_SOCKET_FILTER)
	guardPut32(programAttr, 4, uint32(len(program)/8))
	attempts := make([]any, 0, 2)
	failed := false
	for _, attempt := range []struct {
		command          uint32
		name             string
		attr             []byte
		program, license []byte
	}{
		{unix.BPF_MAP_CREATE, "MAP_CREATE", mapAttr, nil, nil},
		{unix.BPF_PROG_LOAD, "PROG_LOAD", programAttr, program, license},
	} {
		if !pidfdSelects(6, parentPID) {
			return errGuardProtocol
		}
		actual := append([]byte(nil), attempt.attr...)
		var programValue, licenseValue any
		if attempt.program != nil {
			guardPut64(actual, 8, uint64(uintptr(unsafe.Pointer(&attempt.program[0]))))
			guardPut64(actual, 16, uint64(uintptr(unsafe.Pointer(&attempt.license[0]))))
			programValue, licenseValue = hex.EncodeToString(attempt.program), "GPL"
		}
		result, _, errno := unix.Syscall(unix.SYS_BPF, uintptr(attempt.command), uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
		runtime.KeepAlive(actual)
		runtime.KeepAlive(attempt.program)
		runtime.KeepAlive(attempt.license)
		signed := int64(result)
		var closeResult any
		if errno != 0 {
			signed = -int64(errno)
		} else {
			failed = true
			if result > uintptr(^uint32(0)>>1) {
				return errGuardProtocol
			}
			closeErr := unix.Close(int(result))
			closeResult = jsonI64(0)
			if closeErr != nil {
				var closeErrno unix.Errno
				if !errors.As(closeErr, &closeErrno) || closeErrno == 0 {
					return errGuardProtocol
				}
				closeResult = jsonI64(-int64(closeErrno))
			}
		}
		if errno != unix.EPERM {
			failed = true
		}
		attempts = append(attempts, map[string]any{"command": attempt.name, "attributes": map[string]any{"attr_size": jsonU64(uint64(len(attempt.attr))), "attr_hex": hex.EncodeToString(attempt.attr)}, "program_bytes": programValue, "license": licenseValue, "result": jsonI64(signed), "close_result": closeResult})
	}
	body := map[string]any{"version": jsonU64(1), "case": "bpf-authorization", "actor": actor, "attempts": attempts}
	encoded, err := canonicalControl(body)
	if err != nil || len(encoded)+1 > maxControlFrame {
		return errGuardProtocol
	}
	encoded = append(encoded, '\n')
	n, err := os.Stdout.Write(encoded)
	if err != nil {
		return err
	}
	if n != len(encoded) {
		return errGuardProtocol
	}
	if failed {
		return errGuardProtocol
	}
	return nil
}

// writeSelftestFrame emits only canonical, bounded observations of actual calls.
func writeSelftestFrame(body map[string]any) error {
	encoded, err := canonicalControl(body)
	if err != nil || len(encoded)+1 > maxControlFrame {
		return errGuardProtocol
	}
	encoded = append(encoded, '\n')
	n, err := os.Stdout.Write(encoded)
	if err != nil {
		return err
	}
	if n != len(encoded) {
		return errGuardProtocol
	}
	return nil
}

func runContainerRawDenialSelftest() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if len(os.Args) != 3 || os.Getuid() != 10002 || os.Geteuid() != 10002 || os.Getgid() != 10002 || os.Getegid() != 10002 {
		return errGuardProtocol
	}
	rows, err := taskCapabilityRows(os.Getpid())
	if err != nil {
		return err
	}
	for _, row := range rows {
		for _, key := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
			if row[key] != "0000000000000000" {
				return errGuardProtocol
			}
		}
	}
	fd, err := unix.Open("/run/g2/container-bpf.fifo", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFIFO || st.Uid != 0 || st.Gid != 10002 || st.Mode&07777 != 0640 {
		return errGuardProtocol
	}
	var gate [1]byte
	for {
		n := asmProbeBarrierRead(int64(fd), uintptr(unsafe.Pointer(&gate[0])))
		if n == -int64(unix.EINTR) {
			continue
		}
		if n != 1 || gate[0] != 0x5a {
			return errGuardProtocol
		}
		break
	}
	var mapAttr [72]byte
	guardPut32(mapAttr[:], 0, unix.BPF_MAP_TYPE_ARRAY)
	guardPut32(mapAttr[:], 4, 4)
	guardPut32(mapAttr[:], 8, 4)
	guardPut32(mapAttr[:], 12, 1)
	copy(mapAttr[28:44], "feature_test")
	program, err := probeProgram()
	if err != nil || len(program) != 16 {
		return errGuardProtocol
	}
	license := []byte{'G', 'P', 'L', 0}
	var progAttr [72]byte
	guardPut32(progAttr[:], 0, unix.BPF_PROG_TYPE_SOCKET_FILTER)
	guardPut32(progAttr[:], 4, uint32(len(program)/8))
	guardPut64(progAttr[:], 8, uint64(uintptr(unsafe.Pointer(&program[0]))))
	guardPut64(progAttr[:], 16, uint64(uintptr(unsafe.Pointer(&license[0]))))
	mapResult := asmProbeBPF(unix.BPF_MAP_CREATE, uintptr(unsafe.Pointer(&mapAttr[0])), uintptr(len(mapAttr)))
	programResult := asmProbeBPF(unix.BPF_PROG_LOAD, uintptr(unsafe.Pointer(&progAttr[0])), uintptr(len(progAttr)))
	runtime.KeepAlive(program)
	runtime.KeepAlive(license)
	if mapResult >= 0 {
		_ = unix.Close(int(mapResult))
	}
	if programResult >= 0 {
		_ = unix.Close(int(programResult))
	}
	if mapResult != -int64(unix.EPERM) || programResult != -int64(unix.EPERM) {
		return errGuardProtocol
	}
	return nil
}

// Fixed child protocol: FD 8 carries one-byte 0x5a permits. FD 10 is the
// supplied link or extra program descriptor; stdout carries observed results.
func runFixedSelftestChild(mode string) error {
	if unix.Gettid() != os.Getpid() {
		return errGuardProtocol
	}
	var control unix.Stat_t
	if unix.Fstat(8, &control) != nil || control.Mode&unix.S_IFMT != unix.S_IFIFO {
		return errGuardProtocol
	}
	if len(os.Args) != 3 {
		return errGuardProtocol
	}
	if mode == "audit-overflow-bind-filler" {
		for {
			var permit [1]byte
			n := asmProbeBarrierRead(8, uintptr(unsafe.Pointer(&permit[0])))
			if n == 0 {
				return nil
			}
			if n == -int64(unix.EINTR) {
				continue
			}
			if n != 1 || permit[0] != 0x5a {
				return errGuardProtocol
			}
			fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				return err
			}
			bindErr := unix.Bind(fd, &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}})
			closeErr := unix.Close(fd)
			bindResult, closeResult := int64(0), int64(0)
			if bindErr != nil {
				var errno unix.Errno
				if !errors.As(bindErr, &errno) {
					return bindErr
				}
				bindResult = -int64(errno)
			}
			if closeErr != nil {
				var errno unix.Errno
				if !errors.As(closeErr, &errno) {
					return closeErr
				}
				closeResult = -int64(errno)
			}
			if err := writeSelftestFrame(map[string]any{"mode": mode, "tid": jsonI64(int64(unix.Gettid())), "socket_fd": jsonI64(int64(fd)), "bind_result": jsonI64(bindResult), "close_result": jsonI64(closeResult)}); err != nil {
				return err
			}
			if bindErr != nil {
				return bindErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	if mode == "extra-fd-delays-removal" {
		var socketStat unix.Stat_t
		if unix.Fstat(9, &socketStat) != nil || socketStat.Mode&unix.S_IFMT != unix.S_IFSOCK {
			return errGuardProtocol
		}
		listener, err := installRuntimeListener()
		if err != nil {
			return err
		}
		if err = sendControl(9, []byte(`{"ready":true}`), []int{listener}); err != nil {
			_ = unix.Close(listener)
			return err
		}
		_ = unix.Close(listener)
		_ = unix.Close(9)
		if err = waitProbeBarrierOnFD8(); err != nil {
			return err
		}
		var attr [16]byte
		result := asmProbeBPF(unix.BPF_MAP_CREATE, uintptr(unsafe.Pointer(&attr[0])), uintptr(len(attr)))
		runtime.KeepAlive(attr)
		if result < 0 || result > int64(^uint32(0)>>1) {
			return errGuardProtocol
		}
		fd := int(result)
		var st unix.Stat_t
		if err = unix.Fstat(fd, &st); err != nil {
			_ = unix.Close(fd)
			return err
		}
		if err = writeSelftestFrame(map[string]any{"mode": mode, "phase": "held", "tid": jsonI64(int64(unix.Gettid())), "fd": jsonI64(result)}); err != nil {
			_ = unix.Close(fd)
			return err
		}
		if err = waitProbeBarrierOnFD8(); err != nil {
			_ = unix.Close(fd)
			return err
		}
		if err = unix.Close(fd); err != nil {
			return err
		}
		return writeSelftestFrame(map[string]any{"mode": mode, "phase": "closed", "tid": jsonI64(int64(unix.Gettid())), "fd": jsonI64(result), "close_result": jsonI64(0)})
	}
	if mode == "seal-detach-denied" {
		if err := unix.SetsockoptInt(9, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
			return err
		}
		payload, cred, rights, err := receiveControl(9, 1)
		if err != nil {
			return err
		}
		if string(payload) != `{"fd":"link"}` || cred == nil || cred.Uid != 0 || cred.Pid <= 1 {
			_ = unix.Close(rights[0])
			return errGuardProtocol
		}
		fd := rights[0]
		if fd != 10 {
			if err = unix.Dup3(fd, 10, unix.O_CLOEXEC); err != nil {
				_ = unix.Close(fd)
				return err
			}
			if err = unix.Close(fd); err != nil {
				return err
			}
		}
		if err = unix.Close(9); err != nil {
			_ = unix.Close(10)
			return err
		}
	}
	if err := waitProbeBarrierOnFD8(); err != nil {
		return err
	}
	switch mode {
	case "seal-detach-denied":
		var attr [4]byte
		guardPut32(attr[:], 0, 10)
		result := asmProbeBPF(unix.BPF_LINK_DETACH, uintptr(unsafe.Pointer(&attr[0])), uintptr(len(attr)))
		if err := unix.Close(10); err != nil {
			return err
		}
		if err := writeSelftestFrame(map[string]any{"mode": mode, "tid": jsonI64(int64(unix.Gettid())), "fd": jsonI64(10), "result": jsonI64(result)}); err != nil {
			return err
		}
		if result != -int64(unix.EPERM) {
			return errGuardProtocol
		}
		return nil
	case "seal-unload-denied":
		name := []byte("g2_bpf_guard\x00")
		result, _, errno := unix.RawSyscall(unix.SYS_DELETE_MODULE, uintptr(unsafe.Pointer(&name[0])), uintptr(unix.O_NONBLOCK), 0)
		runtime.KeepAlive(name)
		signed := int64(result)
		if errno != 0 {
			signed = -int64(errno)
		}
		if err := writeSelftestFrame(map[string]any{"mode": mode, "tid": jsonI64(int64(unix.Gettid())), "result": jsonI64(signed)}); err != nil {
			return err
		}
		if errno == 0 || errno == unix.ENOENT {
			return errGuardProtocol
		}
		return nil
	}
	return errGuardProtocol
}

type probeThreadResults struct {
	childTID               int32
	_                      int32
	tid, unshare, dup, bpf int64
}

func waitProbeThread(results *probeThreadResults) error {
	for {
		tid := atomic.LoadInt32(&results.childTID)
		if tid == 0 {
			return nil
		}
		_, _, errno := unix.Syscall6(unix.SYS_FUTEX, uintptr(unsafe.Pointer(&results.childTID)), 0 /* FUTEX_WAIT */, uintptr(uint32(tid)), 0, 0, 0)
		if errno != 0 && errno != unix.EINTR && errno != unix.EAGAIN {
			return errno
		}
	}
}

func stopProbeChild(pid int64) {
	if pid <= 0 {
		return
	}
	_ = unix.Kill(int(pid), unix.SIGKILL)
	var status unix.WaitStatus
	for {
		_, err := unix.Wait4(int(pid), &status, 0, nil)
		if err != unix.EINTR {
			return
		}
	}
}

func runAssemblySelftestProbe(mode string) error {
	runtime.LockOSThread()
	if mode == "seal-detach-denied" || mode == "seal-unload-denied" || mode == "extra-fd-delays-removal" || mode == "audit-overflow-bind-filler" {
		return runFixedSelftestChild(mode)
	}
	var barrierStat unix.Stat_t
	if unix.Fstat(8, &barrierStat) != nil || barrierStat.Mode&unix.S_IFMT != unix.S_IFIFO {
		return errGuardProtocol
	}
	if mode == "ring-overflow-filler" {
		if err := waitProbeBarrierOnFD8(); err != nil {
			return err
		}
		for range 2100 {
			pid, _, errno := unix.RawSyscall(unix.SYS_FORK, 0, 0, 0)
			if errno != 0 {
				return errno
			}
			if pid == 0 {
				unix.RawSyscall(unix.SYS_EXIT_GROUP, 0, 0, 0)
			}
			var wstatus unix.WaitStatus
			waited, err := unix.Wait4(int(pid), &wstatus, 0, nil)
			if err != nil || waited != int(pid) || !wstatus.Exited() || wstatus.ExitStatus() != 0 {
				return errGuardProtocol
			}
		}
		return writeSelftestFrame(map[string]any{"mode": mode, "tid": jsonI64(int64(unix.Gettid())), "children_reaped": jsonI64(2100)})
	}
	// FD 9 is the seqpacket socket to send the newly installed seccomp listener FD back to REALM_ROOT
	// before blocking on the FD 8 barrier. FD 10 = fixtureA, FD 11 = fixtureB.
	var st9 unix.Stat_t
	if err := unix.Fstat(9, &st9); err != nil || st9.Mode&unix.S_IFMT != unix.S_IFSOCK {
		return errGuardProtocol
	}
	listenerFD, err := installRuntimeListener()
	if err != nil {
		return err
	}
	if err := sendControl(9, []byte(`{"ready":true}`), []int{listenerFD}); err != nil {
		_ = unix.Close(listenerFD)
		return err
	}
	_ = unix.Close(listenerFD)
	_ = unix.Close(9)
	if err := waitProbeBarrierOnFD8(); err != nil {
		return err
	}
	attr, err := unix.Mmap(-1, 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		return err
	}
	defer unix.Munmap(attr)
	const targetSlot = 10
	guardPut32(attr[:16], 0, targetSlot)
	guardPut32(attr[:16], 4, 88)

	switch mode {
	case "fd-slot-reuse":
		dupErr := unix.Dup2(11, targetSlot)
		if dupErr != nil {
			return dupErr
		}
		res := asmProbeBPF(unix.BPF_OBJ_GET_INFO_BY_FD, uintptr(unsafe.Pointer(&attr[0])), 16)
		if err := writeSelftestFrame(map[string]any{"mode": mode, "tid": jsonI64(int64(unix.Gettid())), "dup_result": jsonI64(10), "bpf_result": jsonI64(res)}); err != nil {
			return err
		}
		if res != -int64(unix.EPERM) {
			return errGuardProtocol
		}
	case "nonleader-files", "shared-files", "leader-exit":
		var original, fixture unix.Stat_t
		if unix.Fstat(10, &original) != nil || unix.Fstat(11, &fixture) != nil {
			return errGuardProtocol
		}
		if original.Dev == fixture.Dev && original.Ino == fixture.Ino {
			return errGuardProtocol
		}
		stack, err := unix.Mmap(-1, 0, 65536, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
		if err != nil {
			return err
		}
		defer unix.Munmap(stack)
		resultMemory, err := unix.Mmap(-1, 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_ANON)
		if err != nil {
			return err
		}
		defer unix.Munmap(resultMemory)
		results := (*probeThreadResults)(unsafe.Pointer(&resultMemory[0]))
		results.childTID = -1
		flags := uintptr(unix.CLONE_VM | unix.CLONE_FS | unix.CLONE_SIGHAND | unix.CLONE_THREAD | unix.CLONE_CHILD_SETTID | unix.CLONE_CHILD_CLEARTID)
		if mode != "nonleader-files" {
			flags |= unix.CLONE_FILES
		}
		stackTop := uintptr(unsafe.Pointer(&stack[len(stack)-1])) + 1
		if mode == "leader-exit" {
			if unix.Gettid() != os.Getpid() {
				return errGuardProtocol
			}
			dupResult, _, dupErr := unix.RawSyscall(unix.SYS_DUP2, 11, targetSlot, 0)
			if dupErr != 0 || dupResult != targetSlot {
				return errGuardProtocol
			}
			ready := make(chan struct{})
			go func() {
				close(ready)
				err := waitProbeThread(results)
				if err == nil {
					err = writeSelftestFrame(map[string]any{"mode": mode, "leader_tid": jsonI64(int64(os.Getpid())), "tid": jsonI64(results.tid), "dup_result": jsonI64(int64(dupResult)), "barrier_result": jsonI64(results.unshare), "bpf_result": jsonI64(results.bpf)})
				}
				runtime.KeepAlive(stack)
				runtime.KeepAlive(attr)
				if err != nil || results.tid <= 0 || results.unshare != 1 || results.bpf != -int64(unix.EPERM) {
					os.Exit(1)
				}
				os.Exit(0)
			}()
			<-ready
			_ = asmProbeLeaderExit(flags, stackTop, uintptr(unsafe.Pointer(&attr[0])), uintptr(unsafe.Pointer(results)))
			return errGuardProtocol
		}
		unshare := uintptr(0)
		if mode == "nonleader-files" {
			unshare = 1
		}
		tid := asmProbeNonleaderThread(flags, stackTop, uintptr(unsafe.Pointer(&attr[0])), uintptr(unsafe.Pointer(results)), unshare)
		if tid <= 0 {
			return errGuardProtocol
		}
		if err := waitProbeThread(results); err != nil {
			return err
		}
		runtime.KeepAlive(stack)
		runtime.KeepAlive(attr)
		var after unix.Stat_t
		if err := unix.Fstat(10, &after); err != nil {
			return err
		}
		leaderIsA := after.Dev == original.Dev && after.Ino == original.Ino
		leaderIsB := after.Dev == fixture.Dev && after.Ino == fixture.Ino
		if err := writeSelftestFrame(map[string]any{"mode": mode, "leader_tid": jsonI64(int64(unix.Gettid())), "tid": jsonI64(results.tid), "clone_result": jsonI64(tid), "unshare_result": jsonI64(results.unshare), "dup_result": jsonI64(results.dup), "bpf_result": jsonI64(results.bpf), "leader_is_a": leaderIsA, "leader_is_b": leaderIsB}); err != nil {
			return err
		}
		if results.tid != tid || results.unshare != 0 || results.dup != 10 || results.bpf != -int64(unix.EPERM) || (mode == "nonleader-files" && !leaderIsA) || (mode == "shared-files" && !leaderIsB) {
			return errGuardProtocol
		}
	case "tid-reuse":
		tid := asmProbeForkExit()
		if tid <= 0 {
			return errGuardProtocol
		}
		if err := writeSelftestFrame(map[string]any{"mode": mode, "phase": "first-child-started", "first_tid": jsonI64(tid)}); err != nil {
			stopProbeChild(tid)
			return err
		}
		var firstStatus unix.WaitStatus
		waited, err := unix.Wait4(int(tid), &firstStatus, 0, nil)
		if err != nil || waited != int(tid) || !firstStatus.Exited() || firstStatus.ExitStatus() != 0 {
			return errGuardProtocol
		}
		type cloneArgs struct{ flags, pidfd, childTID, parentTID, exitSignal, stack, stackSize, tls, setTID, setTIDSize, cgroup uint64 }
		setTIDVal := uint32(tid)
		cl := cloneArgs{exitSignal: uint64(unix.SIGCHLD), setTID: uint64(uintptr(unsafe.Pointer(&setTIDVal))), setTIDSize: 1}
		if err := unix.Dup2(11, targetSlot); err != nil {
			return err
		}
		shared, err := unix.Mmap(-1, 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_ANON)
		if err != nil {
			return err
		}
		defer unix.Munmap(shared)
		cpid := asmProbeClone3SetTID(uintptr(unsafe.Pointer(&cl)), unsafe.Sizeof(cl), int64(unix.BPF_OBJ_GET_INFO_BY_FD), uintptr(unsafe.Pointer(&attr[0])), 16, uintptr(unsafe.Pointer(&shared[0])))
		runtime.KeepAlive(cl)
		runtime.KeepAlive(setTIDVal)
		if cpid <= 0 || cpid != tid {
			if cpid > 0 {
				stopProbeChild(cpid)
			}
			_ = writeSelftestFrame(map[string]any{"mode": mode, "first_tid": jsonI64(tid), "first_wait_status": jsonI64(int64(firstStatus)), "clone_result": jsonI64(cpid)})
			return errGuardProtocol
		}
		if err := writeSelftestFrame(map[string]any{"mode": mode, "phase": "reused-child-started", "first_tid": jsonI64(tid), "new_tid": jsonI64(cpid), "first_wait_status": jsonI64(int64(firstStatus))}); err != nil {
			stopProbeChild(cpid)
			return err
		}
		var status unix.WaitStatus
		waited, err = unix.Wait4(int(cpid), &status, 0, nil)
		result := *(*int64)(unsafe.Pointer(&shared[0]))
		if err != nil || waited != int(cpid) {
			return errGuardProtocol
		}
		if err := writeSelftestFrame(map[string]any{"mode": mode, "first_tid": jsonI64(tid), "first_wait_status": jsonI64(int64(firstStatus)), "clone_result": jsonI64(cpid), "wait_status": jsonI64(int64(status)), "dup_result": jsonI64(10), "bpf_result": jsonI64(result)}); err != nil {
			return err
		}
		if !status.Exited() || status.ExitStatus() != 0 || result != -int64(unix.EPERM) {
			return errGuardProtocol
		}
	default:
		return errGuardProtocol
	}
	return nil
}

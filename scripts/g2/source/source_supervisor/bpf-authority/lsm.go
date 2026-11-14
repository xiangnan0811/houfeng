//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

// sealedAuthority retains the original kernel links and registry maps for the
// broker's entire life. The guard independently holds link references after
// SEAL, so loss of these descriptors cannot detach the three hooks.
type sealedAuthority struct {
	guard              guardDevice
	maps               map[string]bootstrapMapHandle
	programs           map[string]bootstrapProgramHandle
	links              [3]int // bpf, inode_rmdir, inode_mkdir
	btfFD, brokerPIDFD int
	recorder           *bootstrapRecorder
	guardCursor        uint64
	seal               guardSealResult
	brokerCookie       uint64
}

func checkPolicySpec(spec *ebpf.CollectionSpec) error {
	if spec == nil || len(spec.Maps) != 4 || len(spec.Programs) != 3 {
		return errGuardProtocol
	}
	type mapShape struct {
		typ                    ebpf.MapType
		key, value, max, flags uint32
	}
	shapes := map[string]mapShape{
		"broker_token":   {ebpf.TaskStorage, 4, 8, 0, unix.BPF_F_NO_PREALLOC},
		"guard_liveness": {ebpf.Array, 4, 8, 1, 0},
		"realm_targets":  {ebpf.Hash, 16, 24, 128, 0},
		"retire_tasks":   {ebpf.Hash, 32, 40, 4096, 0},
	}
	for name, want := range shapes {
		m := spec.Maps[name]
		if m == nil || m.Type != want.typ || m.KeySize != want.key || m.ValueSize != want.value || m.MaxEntries != want.max || m.Flags != want.flags || len(m.Contents) != 0 || m.InnerMap != nil || m.Pinning != ebpf.PinNone {
			return errGuardProtocol
		}
	}
	for name, attach := range map[string]string{"g2_bpf": "bpf", "g2_inode_rmdir": "inode_rmdir", "g2_inode_mkdir": "inode_mkdir"} {
		p := spec.Programs[name]
		if p == nil || p.Type != ebpf.LSM || p.AttachType != ebpf.AttachLSMMac || p.AttachTo != attach || p.License != "GPL" {
			return errGuardProtocol
		}
	}
	return nil
}

// noExecBroker is applied to the token-bearing OS thread before attaching the
// bpf hook. It is deliberately not a general capability or module filter.
func noExecBroker() error {
	const (
		ld  = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
		jeq = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
		ret = unix.BPF_RET | unix.BPF_K
	)
	ins := []unix.SockFilter{
		{Code: ld, K: 4},
		{Code: jeq, Jt: 0, Jf: 7, K: auditArchX8664},
		{Code: ld, K: 0},
		{Code: jeq, Jt: 3, K: unix.SYS_EXECVE},
		{Code: jeq, Jt: 2, K: unix.SYS_EXECVEAT},
		{Code: jeq, Jt: 1, K: x32SyscallBit | 520},
		{Code: jeq, Jt: 0, Jf: 1, K: x32SyscallBit | 545},
		{Code: ret, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: ret, K: unix.SECCOMP_RET_ALLOW},
		{Code: jeq, Jt: 0, Jf: 4, K: auditArchI386},
		{Code: ld, K: 0},
		{Code: jeq, Jt: 2, K: 11},  // i386 execve
		{Code: jeq, Jt: 1, K: 358}, // i386 execveat
		{Code: ret, K: unix.SECCOMP_RET_ALLOW},
		{Code: ret, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
	}
	program := unix.SockFprog{Len: uint16(len(ins)), Filter: &ins[0]}
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, 0, uintptr(unsafe.Pointer(&program)))
	if errno != 0 {
		return errno
	}
	return nil
}

// loadAndSealPolicy must be called on the broker's original locked main thread
// after finit_module. The guard validates the actual hook/program references;
// no successful seal can be constructed from these userspace checks alone.
func loadAndSealPolicy(manifest bootstrapManifest, manifestSHA string, heldGuard guardDevice, beforeEffects func(*bootstrapRecorder) error, onSealStage func(string) error) (_ *sealedAuthority, err error) {
	runtime.LockOSThread()
	if unix.Gettid() != os.Getpid() {
		g2d1Fail("POLICY_THREAD", nil)
		return nil, fmt.Errorf("BPF_AUTH_UNAVAILABLE: broker is not the initial task")
	}
	decoded, e := hex.DecodeString(manifestSHA)
	if e != nil || len(decoded) != 32 || guardZero(decoded) {
		g2d1Fail("POLICY_MANIFEST", e)
		return nil, errGuardProtocol
	}
	var digest [32]byte
	copy(digest[:], decoded)
	fd := heldGuard.fd
	if fd < 0 {
		g2d1Fail("POLICY_GUARD_FD", nil)
		return nil, errGuardProtocol
	}
	result := &sealedAuthority{guard: heldGuard, maps: make(map[string]bootstrapMapHandle), programs: make(map[string]bootstrapProgramHandle), links: [3]int{-1, -1, -1}, btfFD: -1, brokerPIDFD: -1}
	defer func() {
		if err != nil {
			for _, linkFD := range result.links {
				if linkFD >= 0 {
					_ = unix.Close(linkFD)
				}
			}
			for _, program := range result.programs {
				if program.fd >= 0 {
					_ = unix.Close(program.fd)
				}
			}
			for _, item := range result.maps {
				if item.fd >= 0 {
					_ = unix.Close(item.fd)
				}
			}
			if result.btfFD >= 0 {
				_ = unix.Close(result.btfFD)
			}
			if result.brokerPIDFD >= 0 {
				_ = unix.Close(result.brokerPIDFD)
			}
			_ = unix.Close(fd)
		}
	}()
	status, e := result.guard.status()
	if e != nil || status.State != 0 || status.BrokerCookie == 0 || status.Sticky != 0 || status.Lost != 0 || status.ProbeMissed != 0 {
		g2d1Fail("POLICY_GUARD_UNSEALED", e)
		return nil, fmt.Errorf("BPF_AUTH_UNAVAILABLE: guard is not clean and unsealed")
	}
	result.brokerCookie = status.BrokerCookie
	boot, e := fixedBootID()
	if e != nil {
		g2d1Fail("POLICY_BOOT_ID", e)
		return nil, e
	}
	result.recorder = &bootstrapRecorder{bootID: boot, executor: status.BrokerCookie, baseSequence: 1}
	if beforeEffects == nil {
		g2d1Fail("POLICY_RECORD_SETUP", nil)
		return nil, errGuardProtocol
	}
	if e = beforeEffects(result.recorder); e != nil {
		g2d1Fail("POLICY_RECORD_SETUP", e)
		return nil, e
	}
	policy, policyHash, e := openTrustedRegular("/g2-policy.o", 32<<20)
	if e != nil {
		g2d1Fail("POLICY_ARTIFACT", e)
		return nil, e
	}
	defer policy.Close()
	if policyHash != manifest.PolicySHA256 {
		g2d1Fail("POLICY_ARTIFACT", nil)
		return nil, fmt.Errorf("BPF_AUTH_UNAVAILABLE: policy artifact changed")
	}
	spec, e := loadBootstrapCollectionSpec(policy)
	if e != nil {
		g2d1Fail("POLICY_SPEC", e)
		return nil, e
	}
	kernel, e := readPinnedKernelBTF()
	if e != nil {
		g2d1Fail("POLICY_BTF_READ", e)
		return nil, e
	}
	image, e := prepareBootstrapPolicy(spec, kernel)
	if e != nil {
		g2d1Fail("POLICY_PREPARE", e)
		return nil, e
	}
	result.btfFD, e = loadBootstrapBTF(result.recorder, image.btfBytes)
	if e != nil {
		g2d1Fail("POLICY_BTF_LOAD", e)
		return nil, e
	}
	for _, name := range []string{"broker_token", "guard_liveness", "realm_targets", "retire_tasks"} {
		item, e := createBootstrapMap(result.guard, result.recorder, &result.guardCursor, image.maps[name], result.btfFD)
		if e != nil {
			g2d1Fail("POLICY_MAP_CREATE", e)
			return nil, e
		}
		result.maps[name] = item
	}
	result.brokerPIDFD, e = unix.PidfdOpen(os.Getpid(), 0)
	if e != nil {
		g2d1Fail("POLICY_TOKEN_PIDFD", e)
		return nil, e
	}
	if e = installBrokerToken(result.recorder, result.maps["broker_token"], result.brokerPIDFD, result.brokerCookie); e != nil {
		g2d1Fail("POLICY_TOKEN_INSTALL", e)
		return nil, e
	}
	if e = installGuardLiveness(result.recorder, result.maps["guard_liveness"]); e != nil {
		g2d1Fail("POLICY_LIVENESS_INSTALL", e)
		return nil, e
	}
	if e = noExecBroker(); e != nil {
		g2d1Fail("POLICY_NOEXEC", e)
		return nil, e
	}
	for _, name := range []string{"g2_inode_rmdir", "g2_inode_mkdir", "g2_bpf"} {
		item, e := loadBootstrapProgram(result.guard, result.recorder, &result.guardCursor, image.programs[name], result.btfFD, result.maps)
		if e != nil {
			g2d1Fail("POLICY_PROGRAM_LOAD", e)
			return nil, e
		}
		result.programs[name] = item
	}
	var observedLinkIDs [3]uint32
	for _, item := range []struct {
		index         int
		hook, program string
	}{{1, "inode_rmdir", "g2_inode_rmdir"}, {2, "inode_mkdir", "g2_inode_mkdir"}, {0, "bpf", "g2_bpf"}} {
		linkFD, linkID, e := attachBootstrapProgram(result.recorder, item.hook, result.programs[item.program])
		if e != nil {
			g2d1Fail("POLICY_LINK_ATTACH", e)
			return nil, e
		}
		result.links[item.index], observedLinkIDs[item.index] = linkFD, linkID
		if item.index == 2 && onSealStage != nil {
			if e = onSealStage("seal-inode-links"); e != nil {
				g2d1Fail("POLICY_INODE_STAGE", e)
				return nil, e
			}
		}
		if item.index == 0 && onSealStage != nil {
			if e = onSealStage("seal-bpf-link"); e != nil {
				g2d1Fail("POLICY_BPF_STAGE", e)
				return nil, e
			}
		}
	}
	sealed, e := result.guard.seal(result.links, digest)
	if e != nil {
		g2d1Fail("POLICY_SEAL_IOCTL", e)
		return nil, e
	}
	result.seal = sealed
	if sealed.LinkIDs != observedLinkIDs || uint64(sealed.ProgramIDs[0]) != result.programs["g2_bpf"].kernelID || uint64(sealed.ProgramIDs[1]) != result.programs["g2_inode_rmdir"].kernelID || uint64(sealed.ProgramIDs[2]) != result.programs["g2_inode_mkdir"].kernelID {
		g2d1Fail("POLICY_SEAL_IDS", nil)
		return nil, errGuardProtocol
	}
	if onSealStage != nil {
		if e = onSealStage("seal-sealed"); e != nil {
			g2d1Fail("POLICY_SEALED_STAGE", e)
			return nil, e
		}
	}
	events, e := bootstrapDrainRecords(result.guard, result.recorder, &result.guardCursor)
	if e != nil {
		g2d1Fail("POLICY_SEALED_DRAIN", e)
		return nil, e
	}
	for _, event := range events {
		if event.Kind == 1 || event.Kind == 3 {
			g2d1Fail("POLICY_SEALED_DRAIN", nil)
			return nil, errGuardProtocol
		}
	}
	verified, e := result.guard.status()
	if e != nil || verified.State != 1 || verified.BrokerCookie != result.brokerCookie || verified.Epoch != sealed.Epoch || verified.LinkIDs != sealed.LinkIDs || verified.Sticky != 0 || verified.Lost != 0 || verified.ProbeMissed != 0 {
		g2d1Fail("POLICY_SEALED_STATUS", e)
		return nil, fmt.Errorf("BPF_AUTH_UNAVAILABLE: guard seal readback failed")
	}
	return result, nil
}

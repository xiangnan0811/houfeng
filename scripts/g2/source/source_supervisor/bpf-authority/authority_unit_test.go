//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/cilium/ebpf/btf"
	"golang.org/x/sys/unix"
)

// The filter is installed in a separate process and synchronized to all its
// threads. A positive-control child proves SYS_BPF actually kills the process,
// including when the host would otherwise reject the feature probe itself.
func TestBootstrapPolicyZeroSyscallParse(t *testing.T) {
	mode := os.Getenv("G2_BOOTSTRAP_POLICY_TEST_CHILD")
	if mode != "" {
		var raw []byte
		var cases map[string][]byte
		if mode == "parse" {
			var err error
			raw, err = os.ReadFile(os.Getenv("G2_BOOTSTRAP_POLICY_TEST_ELF"))
			if err != nil {
				t.Fatal(err)
			}
			cases = bootstrapPolicyMalformedFixtures(t, raw)
		} else if mode != "probe" {
			t.Fatalf("unknown child mode %q", mode)
		}
		runtime.LockOSThread()
		if err := bootstrapPolicyNoBPFFilter(); err != nil {
			t.Fatalf("install BPF syscall filter: %v", err)
		}
		if mode == "probe" {
			_, _, _ = unix.Syscall(unix.SYS_BPF, 0, 0, 0)
			t.Fatal("SYS_BPF survived the filter")
		}
		spec, err := loadBootstrapCollectionSpec(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("parse policy: %v", err)
		}
		if err := checkPolicySpec(spec); err != nil {
			t.Fatalf("policy shape: %v", err)
		}
		for name, want := range map[string]map[string]int{
			"g2_bpf":         {"broker_token": 1, "guard_liveness": 1},
			"g2_inode_mkdir": {"realm_targets": 1},
			"g2_inode_rmdir": {"realm_targets": 1, "retire_tasks": 1},
		} {
			p := spec.Programs[name]
			if p.Instructions[0].Symbol() != name || btf.FuncMetadata(&p.Instructions[0]) == nil {
				t.Errorf("%s missing function symbol or BTF metadata", name)
			}
			refs := make(map[string]int)
			core := 0
			iter := p.Instructions.Iterate()
			for iter.Next() {
				if iter.Ins.IsLoadFromMap() {
					refs[iter.Ins.Reference()]++
				}
				if btf.CORERelocationMetadata(iter.Ins) != nil {
					core++
				}
			}
			if !reflect.DeepEqual(refs, want) || (name != "g2_bpf" && core == 0) {
				t.Errorf("%s map references %v, CO-RE relocations %d", name, refs, core)
			}
		}
		for name, fixture := range cases {
			if _, err := loadBootstrapCollectionSpec(bytes.NewReader(fixture)); err != errGuardProtocol {
				t.Errorf("%s: expected protocol error, got %v", name, err)
			}
		}
		return
	}

	policy := filepath.Join("native", "g2-policy.o")
	if _, err := os.Stat(policy); os.IsNotExist(err) {
		policy = filepath.Join(t.TempDir(), "g2-policy.o")
		cmd := exec.Command("/usr/bin/clang", "-target", "bpfel", "-O2", "-g", "-D__EXPORTED_HEADERS__", "-c", filepath.Join("native", "policy.c"), "-o", policy)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("compile policy fixture: %v\n%s", err, output)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	policy, err := filepath.Abs(policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, childMode := range []string{"probe", "parse"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBootstrapPolicyZeroSyscallParse$", "-test.v")
		cmd.Env = append(os.Environ(), "G2_BOOTSTRAP_POLICY_TEST_CHILD="+childMode, "G2_BOOTSTRAP_POLICY_TEST_ELF="+policy)
		output, err := cmd.CombinedOutput()
		if childMode == "probe" {
			exit, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("filter positive control did not die from SIGSYS: %v\n%s", err, output)
			}
			status, ok := exit.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != syscall.SIGSYS {
				t.Fatalf("filter positive control exited unexpectedly: %v\n%s", err, output)
			}
		} else if err != nil {
			t.Fatalf("filtered policy parser failed: %v\n%s", err, output)
		}
	}
}

func bootstrapPolicyNoBPFFilter() error {
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	const load = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
	const eq = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
	const ret = unix.BPF_RET | unix.BPF_K
	ins := []unix.SockFilter{
		{Code: load, K: 4},
		{Code: eq, K: auditArchX8664, Jt: 1},
		{Code: ret, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: load, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, K: x32SyscallBit, Jf: 1},
		{Code: ret, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: eq, K: unix.SYS_BPF, Jf: 1},
		{Code: ret, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: ret, K: unix.SECCOMP_RET_ALLOW},
	}
	prog := unix.SockFprog{Len: uint16(len(ins)), Filter: &ins[0]}
	result, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&prog)))
	runtime.KeepAlive(ins)
	if errno != 0 {
		return errno
	}
	if result != 0 {
		return fmt.Errorf("seccomp synchronization failed on thread %d", result)
	}
	return nil
}

func bootstrapPolicyMalformedFixtures(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	f, err := elf.NewFile(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	section := func(name string) *elf.Section {
		t.Helper()
		s := f.Section(name)
		if s == nil || s.Offset > uint64(len(raw)) || s.Size > uint64(len(raw))-s.Offset {
			t.Fatalf("fixture section %s missing or out of bounds", name)
		}
		return s
	}
	clone := func() []byte { return append([]byte(nil), raw...) }
	cases := make(map[string][]byte)
	badLicense := clone()
	badLicense[section("license").Offset] = 'X'
	cases["license"] = badLicense
	badMapData := clone()
	badMapData[section(".maps").Offset] = 1
	cases["map initialization"] = badMapData

	// Change both the ELF and BTF names, so the BTF layout fixup succeeds and
	// the parser itself must reject the otherwise matching invalid name.
	badMapName := clone()
	symtab := section(".symtab")
	if int(symtab.Link) >= len(f.Sections) {
		t.Fatal("fixture symbol string table missing")
	}
	for _, sec := range []*elf.Section{f.Sections[symtab.Link], section(".BTF")} {
		if sec.Offset > uint64(len(raw)) || sec.Size > uint64(len(raw))-sec.Offset {
			t.Fatal("fixture name section out of bounds")
		}
		data := raw[sec.Offset : sec.Offset+sec.Size]
		nameOffset := bytes.Index(data, []byte("broker_token\x00"))
		if nameOffset < 0 {
			t.Fatal("fixture map name missing")
		}
		badMapName[sec.Offset+uint64(nameOffset)+6] = '.'
	}
	cases["map name"] = badMapName

	badSection := clone()
	shoff := binary.LittleEndian.Uint64(raw[40:48])
	shentsize := binary.LittleEndian.Uint16(raw[58:60])
	shstrndx := binary.LittleEndian.Uint16(raw[62:64])
	if int(shstrndx) >= len(f.Sections) || shentsize < 64 {
		t.Fatal("fixture section string table missing")
	}
	for i, sec := range f.Sections {
		if sec.Name != "lsm/bpf" {
			continue
		}
		header := shoff + uint64(i)*uint64(shentsize)
		if header > uint64(len(raw)) || uint64(len(raw))-header < 4 {
			t.Fatal("fixture section header out of bounds")
		}
		nameOffset := binary.LittleEndian.Uint32(raw[header : header+4])
		strtab := f.Sections[shstrndx]
		if uint64(nameOffset) >= strtab.Size || strtab.Offset+uint64(nameOffset) >= uint64(len(raw)) {
			t.Fatal("fixture section name out of bounds")
		}
		badSection[strtab.Offset+uint64(nameOffset)] = 'x'
	}
	cases["program section"] = badSection

	rel := section(".rellsm/bpf")
	if rel.Size < 32 {
		t.Fatal("fixture has too few map relocations")
	}
	badRelSymbol := clone()
	binary.LittleEndian.PutUint64(badRelSymbol[rel.Offset+8:rel.Offset+16], 1)
	cases["zero relocation symbol"] = badRelSymbol
	badRelType := clone()
	info := binary.LittleEndian.Uint64(badRelType[rel.Offset+8 : rel.Offset+16])
	binary.LittleEndian.PutUint64(badRelType[rel.Offset+8:rel.Offset+16], info&^uint64(0xffffffff)|2)
	cases["relocation type"] = badRelType
	badRelOffset := clone()
	binary.LittleEndian.PutUint64(badRelOffset[rel.Offset:rel.Offset+8], 4)
	cases["relocation offset"] = badRelOffset
	duplicateRel := clone()
	copy(duplicateRel[rel.Offset+16:rel.Offset+32], duplicateRel[rel.Offset:rel.Offset+16])
	cases["duplicate relocation"] = duplicateRel
	return cases
}

func TestRuntimeSpecPolicy(t *testing.T) {
	facts := &runtimeContainerFacts{
		runtimeReady:    true,
		rootfs:          "/run/g2/rootfs",
		hostname:        "g2-container",
		path:            "/usr/lib/postgresql/16/bin/postgres",
		cwd:             "/var/lib/postgresql/data",
		user:            "999:999",
		args:            []any{"-D", "/var/lib/postgresql/data"},
		configEnv:       []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/lib/postgresql/16/bin", "LANG=C.UTF-8"},
		hostnamePath:    "/run/g2/hostname",
		hostsPath:       "/run/g2/hosts",
		resolvPath:      "/run/g2/resolv.conf",
		shmPath:         "/run/g2/shm",
		sandboxPath:     "/run/g2/netns",
		networkMode:     "g2-initial-app",
		ipcMode:         "private",
		cgroupnsMode:    "private",
		appArmorProfile: "docker-default",
		shmSize:         defaultShmSize,
		pidsLimit:       256,
		hasPidsLimit:    true,
		readonlyRootfs:  true,
		noNewPrivileges: true,
		sysInfoValid:    true,
		sysInfo: runtimeSysInfo{
			cgroupUnified:     true,
			memoryLimit:       true,
			swapLimit:         true,
			cpuCfs:            true,
			pidsLimit:         true,
			seccomp:           true,
			cgroupNamespaces:  true,
			appArmorSupported: true,
		},
		mounts: []runtimeMountFact{
			{typ: "bind", source: "/var/lib/g2/app/pgdata", destination: "/var/lib/postgresql/data", propagation: "rprivate", readonly: false},
		},
	}
	grant := &brokerGrant{
		id:          1,
		owner:       1,
		purpose:     "postgres-live-app",
		containerID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		facts:       facts,
		config: map[string]any{
			"HostConfig": map[string]any{
				"NetworkMode": "g2-initial-app",
				"PidsLimit":   json.Number("256"),
			},
		},
	}
	specMap, err := expectedRuntimeSpec(nil, grant)
	if err != nil {
		t.Fatalf("expectedRuntimeSpec failed: %v", err)
	}
	if err := runtimeProcess(specMap, grant); err != nil {
		t.Fatalf("runtimeProcess failed on expectedRuntimeSpec output: %v", err)
	}
	linuxSection, ok := specMap["linux"].(map[string]any)
	if !ok {
		t.Fatalf("missing linux section in expectedRuntimeSpec output")
	}
	if err := runtimeNamespaces(linuxSection, grant); err != nil {
		t.Fatalf("runtimeNamespaces failed on expectedRuntimeSpec output: %v", err)
	}
	if err := validateRuntimeDeviceRules(linuxSection); err != nil {
		t.Fatalf("validateRuntimeDeviceRules failed on expectedRuntimeSpec output: %v", err)
	}

	// Mutating process.noNewPrivileges must fail runtimeProcess
	procCopy := make(map[string]any)
	for k, v := range specMap["process"].(map[string]any) {
		procCopy[k] = v
	}
	procCopy["noNewPrivileges"] = false
	badSpec := make(map[string]any, len(specMap))
	for k, v := range specMap {
		badSpec[k] = v
	}
	badSpec["process"] = procCopy
	if err := runtimeProcess(badSpec, grant); err == nil {
		t.Fatalf("runtimeProcess accepted modified process.noNewPrivileges")
	}
}

func TestExecAdmissionLifecycle(t *testing.T) {
	// Test expectedExecArgs for all 4 allowed observation classes and reject unknown
	for _, tc := range []struct {
		obs      string
		ip       string
		wantPass bool
		wantErr  bool
	}{
		{"negative_pg_dump", "172.30.80.2", true, false},
		{"app_rollback_dump", "172.30.80.2", true, false},
		{"app_rollback_restore", "172.30.80.2", true, false},
		{"pg_controldata", "", false, false},
		{"unknown_class", "172.30.80.2", false, true},
		{"negative_pg_dump", "", false, true},
	} {
		args, needsPass, err := expectedExecArgs(tc.obs, tc.ip)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("expectedExecArgs(%q, %q) unexpectedly succeeded", tc.obs, tc.ip)
			}
			continue
		}
		if err != nil {
			t.Fatalf("expectedExecArgs(%q, %q) failed: %v", tc.obs, tc.ip, err)
		}
		if needsPass != tc.wantPass || len(args) == 0 {
			t.Fatalf("expectedExecArgs(%q, %q) = (%v, %v), wantPass=%v", tc.obs, tc.ip, args, needsPass, tc.wantPass)
		}
	}

	// Test secret commitment and zeroization on postgresServiceSecretRecord
	pw := strings.Repeat("a1", 32)
	sum := sha256.Sum256([]byte(pw))
	rec := &postgresServiceSecretRecord{
		service:        "app",
		uid:            999,
		gid:            999,
		pgdataDirFD:    -1,
		liveGrantID:    1,
		liveGeneration: 1,
		passwordSHA256: hex.EncodeToString(sum[:]),
		handedOver:     true,
	}
	rec.close()
	if rec.passwordSHA256 != "" {
		t.Fatalf("postgresServiceSecretRecord.close() did not clear passwordSHA256")
	}

	// Test verifyRuntimeExec rejects unconsumed/untracked or wrong-directory paths
	grant := &brokerGrant{
		id:         1,
		purpose:    "postgres-live-app",
		generation: 1,
		facts:      &runtimeContainerFacts{rootfs: "/run/g2/rootfs"},
		exec: &brokerExecAdmission{
			id:           1,
			grantID:      1,
			observation:  "pg_controldata",
			state:        "admitted",
			dockerExecID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
	}
	tmpFile := filepath.Join(t.TempDir(), "process.json")
	if err := os.WriteFile(tmpFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	f, err := os.Open(tmpFile)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	if err := verifyRuntimeExec(nil, grant, tmpFile, int(f.Fd()), strings.Repeat("0", 64)); err == nil {
		t.Fatalf("verifyRuntimeExec accepted non-/tmp path or invalid digest")
	}
}

func TestProgramLoadAttributeFields(t *testing.T) {
	type change struct {
		offset int
		value  byte
	}
	type testCase struct {
		name               string
		size               int
		typ                uint32
		funcSize, lineSize byte
		changes            []change
		want               bool
	}
	cases := []testCase{
		{name: "device-zero-24", size: 24, typ: 15, want: true},
		{name: "device-zero-40", size: 40, typ: 15, want: true},
		{name: "device-zero-144", size: 144, typ: 15, want: true},
		{name: "device-zero-4096", size: 4096, typ: 15, want: true},
		{name: "device-metadata-108", size: 108, typ: 15, funcSize: 8, lineSize: 16, want: true},
		{name: "device-metadata-144", size: 144, typ: 15, funcSize: 8, lineSize: 16, want: true},
		{name: "device-metadata-152", size: 152, typ: 15, funcSize: 8, lineSize: 16, want: true},
		{name: "device-metadata-4096", size: 4096, typ: 15, funcSize: 8, lineSize: 16, want: true},
		{name: "socket-probe", size: 144, typ: 1, changes: []change{{72, 255}, {73, 255}, {74, 255}, {75, 255}, {88, 1}}, want: true},
		{name: "socket-metadata", size: 144, typ: 1, funcSize: 8, lineSize: 16, changes: []change{{72, 255}, {73, 255}, {74, 255}, {75, 255}, {88, 1}}},
		{name: "socket-wrong-btf", size: 144, typ: 1, changes: []change{{72, 254}, {73, 255}, {74, 255}, {75, 255}, {88, 1}}},
		{name: "socket-wrong-count", size: 144, typ: 1, changes: []change{{72, 255}, {73, 255}, {74, 255}, {75, 255}, {88, 2}}},
		{name: "device-socket-markers", size: 144, typ: 15, changes: []change{{72, 255}, {73, 255}, {74, 255}, {75, 255}, {88, 1}}},
		{name: "unknown-type", size: 144, typ: 99},
	}
	for _, size := range []int{93, 95, 96, 104, 107} {
		cases = append(cases, testCase{name: fmt.Sprintf("device-metadata-short-%d", size), size: size, typ: 15, funcSize: 8, lineSize: 16})
	}
	for _, pair := range [][2]byte{{8, 0}, {0, 16}, {16, 8}, {7, 16}, {8, 15}} {
		cases = append(cases, testCase{name: fmt.Sprintf("device-pair-%d-%d", pair[0], pair[1]), size: 144, typ: 15, funcSize: pair[0], lineSize: pair[1]})
	}
	for _, pair := range [][2]byte{{0, 0}, {8, 16}} {
		for _, offset := range []int{40, 44, 48, 72, 80, 84, 88, 96, 100, 104, 108, 4095} {
			size := 144
			if offset == 4095 {
				size = 4096
			}
			cases = append(cases, testCase{
				name: fmt.Sprintf("device-%d-%d-forbidden-byte-%d", pair[0], pair[1], offset),
				size: size, typ: 15, funcSize: pair[0], lineSize: pair[1],
				changes: []change{{offset, 1}},
			})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := make([]byte, tc.size)
			binary.LittleEndian.PutUint32(raw, tc.typ)
			binary.LittleEndian.PutUint32(raw[4:], 1)
			for _, field := range []change{{76, tc.funcSize}, {92, tc.lineSize}} {
				if field.offset < len(raw) {
					raw[field.offset] = field.value
				}
			}
			for _, field := range tc.changes {
				raw[field.offset] = field.value
			}
			full := make([]byte, 4096)
			copy(full, raw)
			before := append([]byte(nil), full...)
			if got := programLoadAttributeFieldsValid(full, len(raw)); got != tc.want {
				t.Fatalf("programLoadAttributeFieldsValid() = %v, want %v", got, tc.want)
			}
			if !bytes.Equal(full, before) {
				t.Fatal("attribute input changed")
			}
		})
	}
	for _, tc := range []struct {
		name     string
		fullSize int
		attrSize int
	}{
		{"short-full", 4095, 24},
		{"long-full", 4097, 24},
		{"short-attr", 4096, 23},
		{"long-attr", 4096, 4097},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if programLoadAttributeFieldsValid(make([]byte, tc.fullSize), tc.attrSize) {
				t.Fatal("invalid buffer or attribute size accepted")
			}
		})
	}
}

func TestDeviceLoadRetryBounds(t *testing.T) {
	inv := &runtimeInvocation{
		id:      1,
		command: "create",
		grant:   &brokerGrant{id: 1},
	}

	makeCaptured := func(logSize int, trueLogSize uint32) *capturedRuntimeEffect {
		outer := make([]byte, 144)
		binary.LittleEndian.PutUint32(outer[140:144], trueLogSize)
		return &capturedRuntimeEffect{
			command:     unix.BPF_PROG_LOAD,
			programKind: "device",
			program: &programInput{
				log:           make([]byte, logSize),
				returnedOuter: outer,
			},
		}
	}

	// 1. Initial load with non-zero log buffer must fail closed
	if err := authorizeCreationCapture(inv, makeCaptured(131072, 0)); err == nil {
		t.Fatalf("authorizeCreationCapture accepted initial load with 131072 log buffer")
	}

	// 2. Initial load with 0 log buffer succeeds commitment
	cap1 := makeCaptured(0, 0)
	if err := noteCreationCommitted(inv, cap1); err != nil {
		t.Fatalf("noteCreationCommitted failed on initial level-0 load: %v", err)
	}
	// Record failed level-0 result with log_true_size == 0
	if err := noteCreationResult(inv, cap1, bpfEffect{FD: -1, Errno: unix.EINVAL}); err != nil {
		t.Fatalf("noteCreationResult failed: %v", err)
	}

	// 3. Second load with old 65536 log buffer must fail closed
	if err := authorizeCreationCapture(inv, makeCaptured(65536, 0)); err == nil {
		t.Fatalf("authorizeCreationCapture accepted 65536 log buffer on retry")
	}

	// 4. Second load with 131072 log buffer succeeds commitment
	cap2 := makeCaptured(131072, 200)
	if err := noteCreationCommitted(inv, cap2); err != nil {
		t.Fatalf("noteCreationCommitted failed on valid 131072 retry: %v", err)
	}
	if err := noteCreationResult(inv, cap2, bpfEffect{FD: 5, Errno: 0}); err != nil {
		t.Fatalf("noteCreationResult failed on retry success: %v", err)
	}

	// 5. Third load must fail closed
	if err := authorizeCreationCapture(inv, makeCaptured(131072, 0)); err == nil {
		t.Fatalf("authorizeCreationCapture accepted third device load")
	}
}

func TestTerminalObserverProtocol(t *testing.T) {
	t.Run("seqpacket-rights", testTerminalObserverSeqpacket)
	tr := newObserverTranscript("00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32))
	seq1, h1, err := tr.append("guard", map[string]any{"kind": jsonU64(14), "operation": jsonU64(2)})
	if err != nil || seq1 != 1 || len(h1) != 64 {
		t.Fatalf("observerTranscript.append(1) failed: seq=%d h=%q err=%v", seq1, h1, err)
	}
	seq2, h2, err := tr.append("guard", map[string]any{"kind": jsonU64(15), "exit_code": jsonU64(9)})
	if err != nil || seq2 != 2 || len(h2) != 64 || h1 == h2 {
		t.Fatalf("observerTranscript.append(2) failed: seq=%d h=%q err=%v", seq2, h2, err)
	}
	recs, next, done, err := tr.recordsAfter(0, 64)
	if err != nil || len(recs) != 2 || next != 2 || !done {
		t.Fatalf("observerTranscript.recordsAfter failed: len=%d next=%d done=%v err=%v", len(recs), next, done, err)
	}

	st := guardStatus{
		State:             2, // dead
		Sticky:            0,
		AuthorityPhase:    2, // revoked
		Sequence:          10,
		AckCursor:         8,
		ObserverAckCursor: 10,
		BrokerCookie:      1,
		ObserverCookie:    2,
		DeathSequence:     9,
		ModuleInstance:    1,
		ModuleDev:         1,
		ModuleIno:         2,
		ModuleSize:        4096,
	}
	proj := projectGuardStatus(st)
	if !reflect.DeepEqual(proj["state"], "dead") || !reflect.DeepEqual(proj["authority_phase"], jsonU64(2)) || !reflect.DeepEqual(proj["death_sequence"], jsonU64(9)) {
		t.Fatalf("projectGuardStatus unexpected output: %+v", proj)
	}
}

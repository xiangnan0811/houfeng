//go:build linux

package deploy

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	ledgerMigrationProgram = "/opt/g2/c81-migrator"
	ledgerMigrationCgroup  = "/sys/fs/cgroup/g2-c81-migration"
)

type ledgerMigrationConfig struct {
	Version         uint64                          `json:"version"`
	PhaseGeneration uint64                          `json:"phase_generation"`
	MigratorURL     string                          `json:"migrator_url"`
	RuntimeRole     string                          `json:"runtime_role"`
	AdminRole       string                          `json:"admin_role"`
	WorkerRole      string                          `json:"worker_role"`
	Application     RecordLedgerApplicationIdentity `json:"application"`
}

// The root supervisor owns the entire scope, including the coordinator. A
// persistent inode and boot ID, rather than a recycled PID, identify a scope
// across a coordinator restart.
func ledgerMigrationScope(expected ledgerPredecessorGroup, members ...int) error {
	var st unix.Stat_t
	if expected.Path != ledgerMigrationCgroup || unix.Lstat(expected.Path, &st) != nil ||
		st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 ||
		st.Dev != expected.Dev || st.Ino != expected.Ino {
		return ErrRecordLedgerFence
	}
	var fs unix.Statfs_t
	if unix.Statfs(expected.Path, &fs) != nil || uint64(fs.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) {
		return ErrRecordLedgerFence
	}
	for _, name := range []string{"cgroup.procs", "cgroup.threads", "cgroup.subtree_control"} {
		var control unix.Stat_t
		if unix.Lstat(expected.Path+"/"+name, &control) != nil || control.Mode&unix.S_IFMT != unix.S_IFREG ||
			control.Uid != 0 || control.Gid != 0 || control.Mode&0022 != 0 {
			return ErrRecordLedgerFence
		}
	}
	typ, err := os.ReadFile(expected.Path + "/cgroup.type")
	if err != nil || string(typ) != "domain\n" {
		return ErrRecordLedgerFence
	}
	entries, err := os.ReadDir(expected.Path)
	if err != nil || len(entries) > 256 {
		return ErrRecordLedgerFence
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return ErrRecordLedgerFence
		}
	}
	body, err := os.ReadFile(expected.Path + "/cgroup.procs")
	if err != nil || len(body) > 4096 {
		return ErrRecordLedgerFence
	}
	actual := strings.Fields(string(body))
	wanted := make([]string, 0, len(members))
	for _, member := range members {
		if member <= 0 {
			return ErrRecordLedgerFence
		}
		wanted = append(wanted, strconv.Itoa(member))
	}
	sort.Strings(actual)
	sort.Strings(wanted)
	if len(actual) != len(wanted) {
		return ErrRecordLedgerFence
	}
	for i := range wanted {
		if actual[i] != wanted[i] || (i > 0 && wanted[i-1] == wanted[i]) {
			return ErrRecordLedgerFence
		}
	}
	return nil
}

func ledgerMigrationBootID() (string, error) {
	body, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || len(body) != 37 || body[36] != '\n' {
		return "", ErrRecordLedgerFence
	}
	value := string(body[:36])
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return "", ErrRecordLedgerFence
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", ErrRecordLedgerFence
		}
	}
	return value, nil
}

// F_SEAL_WRITE prevents writes but does not make the memfd descriptor readonly.
// Reopen it before inheritance and verify both descriptors denote that same
// sealed inode. The write-capable descriptor is closed before child startup.
func ledgerMigrationConfigFile(config ledgerMigrationConfig) (*os.File, error) {
	body, err := json.Marshal(config)
	if err != nil || len(body) == 0 || len(body) > ledgerCallbackFrameLimit {
		return nil, ErrRecordLedgerFence
	}
	defer clear(body)
	writable, err := ledgerPredecessorMemfd(body)
	if err != nil {
		return nil, err
	}
	defer writable.Close()
	readonly, err := os.Open("/proc/self/fd/" + strconv.FormatUint(uint64(writable.Fd()), 10))
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	var a, b unix.Stat_t
	seals, e1 := unix.FcntlInt(readonly.Fd(), unix.F_GET_SEALS, 0)
	flags, e2 := unix.FcntlInt(readonly.Fd(), unix.F_GETFL, 0)
	if unix.Fstat(int(writable.Fd()), &a) != nil || unix.Fstat(int(readonly.Fd()), &b) != nil ||
		a.Dev != b.Dev || a.Ino != b.Ino || a.Size != b.Size || b.Mode&unix.S_IFMT != unix.S_IFREG ||
		e1 != nil || seals&(unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL) != (unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL) ||
		e2 != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
		readonly.Close()
		return nil, ErrRecordLedgerFence
	}
	return readonly, nil
}

// The child is a direct child of this coordinator; the pidfd is obtained as
// part of clone and cmd.Wait is invoked exactly once by the owning coordinator.
func ledgerLaunchMigration(ctx context.Context, guard int, config *os.File, digest string) (*ledgerPredecessorChild, error) {
	observed, err := ledgerPredecessorProgram(ledgerMigrationProgram, 0550)
	if err != nil || observed != digest {
		return nil, ErrRecordLedgerFence
	}
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	defer unix.Close(sockets[0])
	remote := os.NewFile(uintptr(sockets[1]), "c81-child-channel")
	defer remote.Close()
	if unix.SetsockoptInt(sockets[0], unix.SOL_SOCKET, unix.SO_PASSCRED, 1) != nil {
		return nil, ErrRecordLedgerFence
	}
	null, err := os.OpenFile("/dev/null", os.O_RDONLY, 0)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	defer null.Close()
	guardFD, err := unix.FcntlInt(uintptr(guard), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	held := os.NewFile(uintptr(guardFD), "c81-guard")
	defer held.Close()
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		outRead.Close()
		outWrite.Close()
		return nil, ErrRecordLedgerFence
	}
	defer outWrite.Close()
	defer errWrite.Close()
	lifetime, cancel := context.WithCancel(ctx)
	child := &ledgerPredecessorChild{pidfd: -1, output: make(chan ledgerPredecessorIO, 8), waited: make(chan error, 1), cancel: cancel, pipes: []*os.File{outRead, errRead}}
	command := exec.Command(ledgerMigrationProgram)
	command.Env = []string{}
	command.Stdin, command.Stdout, command.Stderr = null, outWrite, errWrite
	command.ExtraFiles = []*os.File{held, config, nil, remote}
	command.SysProcAttr = &syscall.SysProcAttr{PidFD: &child.pidfd, Pdeathsig: syscall.SIGKILL}
	child.cmd = command
	if command.Start() != nil {
		cancel()
		outRead.Close()
		errRead.Close()
		return nil, ErrRecordLedgerFence
	}
	child.process.PID = command.Process.Pid
	child.process.ExecutableSHA256 = digest
	go func() { child.waited <- command.Wait() }()
	if child.pidfd < 0 {
		return child, ErrRecordLedgerFence
	}
	child.process.Starttime, err = ledgerPredecessorStart(child.process.PID)
	if err == nil {
		child.channel, err = ledgerCallbackSocket(sockets[0], unix.Ucred{Pid: int32(child.process.PID), Uid: 10002, Gid: 10002})
	}
	if err != nil {
		return child, ErrRecordLedgerFence
	}
	pump := func(stream string, file *os.File) {
		defer child.readers.Done()
		var total int
		for {
			buffer := make([]byte, 4096)
			n, err := file.Read(buffer)
			total += n
			if total > ledgerCallbackFrameLimit {
				err = ErrRecordLedgerFence
				n = 0
			}
			select {
			case child.output <- ledgerPredecessorIO{Stream: stream, Body: buffer[:n], Err: err}:
			case <-lifetime.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}
	child.readers.Add(3)
	go pump("stdout", outRead)
	go pump("stderr", errRead)
	go func() {
		defer child.readers.Done()
		for {
			body, err := child.channel.receive(lifetime)
			select {
			case child.output <- ledgerPredecessorIO{Stream: "ipc", Body: body, Err: err}:
			case <-lifetime.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return child, nil
}

func ledgerMigrationJoin(ctx context.Context, child *ledgerPredecessorChild) error {
	if child == nil {
		return nil
	}
	if child.pidfd >= 0 {
		return ledgerPredecessorJoin(ctx, child)
	}
	// No numeric-PID signal is permitted if pidfd acquisition failed. The
	// closed IPC endpoint makes the fixed child fail; still reap its direct wait
	// within the original deadline, and keep the callback unresolved.
	if !child.waitSeen {
		select {
		case err := <-child.waited:
			child.waitSeen, child.waitError = true, err
		case <-ctx.Done():
			return ErrRecordLedgerFence
		}
	}
	return ErrRecordLedgerFence
}

func ledgerVerifyMigrationChild(child *ledgerPredecessorChild, scope ledgerPredecessorGroup) error {
	if child == nil || !ledgerHeldProcessLive(child.pidfd) || ledgerMigrationScope(scope, os.Getpid(), child.process.PID) != nil {
		return ErrRecordLedgerFence
	}
	started, err := ledgerPredecessorStart(child.process.PID)
	if err != nil || started != child.process.Starttime {
		return ErrRecordLedgerFence
	}
	proc := "/proc/" + strconv.Itoa(child.process.PID)
	// The child is deliberately nondumpable. Its executable was pinned before
	// direct exec; the root supervisor observes the live executable separately.
	stat, err := os.ReadFile(proc + "/status")
	if err != nil || len(stat) > 65536 {
		return ErrRecordLedgerFence
	}
	parent, uid, gid := false, false, false
	for _, line := range strings.Split(string(stat), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "PPid:" {
			parent = fields[1] == strconv.Itoa(os.Getpid())
		}
		if len(fields) == 5 && fields[0] == "Uid:" {
			uid = fields[1] == "10002" && fields[2] == "10002" && fields[3] == "10002" && fields[4] == "10002"
		}
		if len(fields) == 5 && fields[0] == "Gid:" {
			gid = fields[1] == "10002" && fields[2] == "10002" && fields[3] == "10002" && fields[4] == "10002"
		}
	}
	if !parent || !uid || !gid {
		return ErrRecordLedgerFence
	}
	startAgain, err := ledgerPredecessorStart(child.process.PID)
	if err != nil || startAgain != started || !ledgerHeldProcessLive(child.pidfd) {
		return ErrRecordLedgerFence
	}
	return nil
}

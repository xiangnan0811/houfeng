//go:build linux && !cgo

package main

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	privateLimit = 16 << 10
	serviceUID   = 10002
	serviceGID   = 10002
)

var denied = errors.New("migration unavailable")

// PR_SET_PDEATHSIG belongs to the thread that execs this program. Keep the
// startup goroutine on that OS thread through the entire one-shot operation.
func init() {
	runtime.LockOSThread()
}

func main() {
	// Do not let pgx consult inherited PG*, TLS, service, or home settings.
	os.Clearenv()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	watchdog := time.AfterFunc(60*time.Second, func() { os.Exit(1) })
	ok := false
	defer func() {
		watchdog.Stop()
		cancel()
		if recover() != nil || !ok {
			os.Exit(1)
		}
	}()
	if run(ctx) != nil {
		return
	}
	ok = true
}

func run(ctx context.Context) error {
	// Go may initialize its CLOEXEC epoll/eventfd before main. No other
	// descriptor besides the fixed channels may have survived exec.
	if len(os.Args) != 1 || os.Args[0] != "/opt/g2/c81-migrator" || checkInheritedFDs() != nil || checkProcessSecurity() != nil || checkStandardFDs() != nil {
		return denied
	}
	parent, err := openParent(ctx)
	if err != nil {
		return denied
	}
	defer parent.close()
	// Closing the guard, rather than unlocking it, is the only allowed release.
	defer unix.Close(3)
	if checkGuard() != nil || parent.alive(ctx) != nil {
		return denied
	}
	input, err := readConfig()
	if err != nil || parent.alive(ctx) != nil {
		return denied
	}
	pool, identity, err := openMigrator(ctx, input)
	if err != nil {
		return denied
	}
	defer pool.Close()
	if parent.exchangeReady(ctx, input.phase, identity) != nil {
		return denied
	}

	// While the actual transaction runs, loss of the original parent or an
	// unsolicited packet cancels it. This never substitutes for the parent's
	// independent settlement of an uncertain commit.
	migrationCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopWatch, err := parent.watchDuringMigration(migrationCtx, cancel)
	if err != nil {
		return denied
	}
	manifest, migrationErr := converge(migrationCtx, pool, input)
	watchErr := stopWatch()
	if migrationErr != nil || watchErr != nil || manifest.MigratorCatalogRole != identity.role || parent.alive(ctx) != nil {
		return denied
	}

	// Hold this exact connection, and FD3, until the result ACK has been
	// authenticated. The readback is the persisted manifest head, not a value
	// synthesized from the requested migration.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return denied
	}
	defer conn.Release()
	if checkBackend(ctx, conn.Conn(), input, identity) != nil || readManifest(ctx, conn.Conn(), manifest) != nil {
		return denied
	}
	if parent.exchangeResult(ctx, input.phase, manifest.ManifestDigest) != nil {
		return denied
	}
	return nil
}

func checkInheritedFDs() error {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil || len(entries) > 128 {
		return denied
	}
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil || fd < 0 {
			return denied
		}
		if fd <= 4 || fd == 6 {
			continue
		}
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err == unix.EBADF {
			// The descriptor used to enumerate /proc/self/fd is already closed.
			continue
		}
		if err != nil || flags&unix.FD_CLOEXEC == 0 {
			return denied
		}
		target, err := os.Readlink("/proc/self/fd/" + entry.Name())
		if err != nil || (target != "anon_inode:[eventpoll]" && target != "anon_inode:[eventfd]") {
			return denied
		}
	}
	return nil
}

func checkProcessSecurity() error {
	ruid, euid, suid := unix.Getresuid()
	rgid, egid, sgid := unix.Getresgid()
	groups, err := unix.Getgroups()
	if err != nil || ruid != serviceUID || euid != serviceUID || suid != serviceUID ||
		rgid != serviceGID || egid != serviceGID || sgid != serviceGID || len(groups) != 0 {
		return denied
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var caps [2]unix.CapUserData
	if unix.Capget(&header, &caps[0]) != nil || caps[0].Effective != 0 || caps[1].Effective != 0 ||
		caps[0].Permitted != 0 || caps[1].Permitted != 0 || caps[0].Inheritable != 0 || caps[1].Inheritable != 0 {
		return denied
	}
	var signal int32
	if unix.Prctl(unix.PR_GET_PDEATHSIG, uintptr(unsafe.Pointer(&signal)), 0, 0, 0) != nil || signal != int32(unix.SIGKILL) {
		return denied
	}
	if unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil ||
		unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}) != nil {
		return denied
	}
	// Go may already have created runtime threads by the time main starts.
	// Setting this only on the current thread would leave the others exposed.
	if _, _, errno := syscall.AllThreadsSyscall(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0); errno != 0 {
		return denied
	}
	dumpable, dumpErr := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	nnp, nnpErr := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	var limit unix.Rlimit
	if dumpErr != nil || dumpable != 0 || nnpErr != nil || nnp != 1 ||
		unix.Getrlimit(unix.RLIMIT_CORE, &limit) != nil || limit.Cur != 0 || limit.Max != 0 {
		return denied
	}
	return nil
}

func checkStandardFDs() error {
	var input, null, out, stderr unix.Stat_t
	if unix.Fstat(0, &input) != nil || unix.Stat("/dev/null", &null) != nil ||
		input.Mode&unix.S_IFMT != unix.S_IFCHR || null.Mode&unix.S_IFMT != unix.S_IFCHR ||
		input.Dev != null.Dev || input.Ino != null.Ino || input.Rdev != null.Rdev ||
		unix.Fstat(1, &out) != nil || unix.Fstat(2, &stderr) != nil ||
		out.Mode&unix.S_IFMT != unix.S_IFIFO || stderr.Mode&unix.S_IFMT != unix.S_IFIFO ||
		(out.Dev == stderr.Dev && out.Ino == stderr.Ino) {
		return denied
	}
	for fd, access := range map[int]int{0: unix.O_RDONLY, 1: unix.O_WRONLY, 2: unix.O_WRONLY} {
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil || flags&unix.O_ACCMODE != access {
			return denied
		}
	}
	return nil
}

func checkGuard() error {
	var held, other unix.Stat_t
	if unix.Fstat(3, &held) != nil || held.Mode&unix.S_IFMT != unix.S_IFREG ||
		held.Uid != serviceUID || held.Gid != serviceGID || held.Mode&07777 != 0600 || held.Nlink != 1 {
		return denied
	}
	independent, err := unix.Open("/proc/self/fd/3", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return denied
	}
	defer unix.Close(independent)
	if unix.Fstat(independent, &other) != nil || other.Dev != held.Dev || other.Ino != held.Ino ||
		!lockContended(independent, unix.LOCK_EX) || !lockContended(independent, unix.LOCK_SH) ||
		unix.Flock(3, unix.LOCK_EX|unix.LOCK_NB) != nil || !lockContended(independent, unix.LOCK_SH) {
		return denied
	}
	return nil
}

func lockContended(fd int, kind int) bool {
	err := unix.Flock(fd, kind|unix.LOCK_NB)
	return err == unix.EWOULDBLOCK || err == unix.EAGAIN
}

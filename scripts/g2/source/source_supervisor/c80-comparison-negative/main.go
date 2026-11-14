package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

type stageError string

func (err stageError) Error() string { return string(err) }

// The supervisor observes FD5 until this process exits, including while the
// final stdout frame is emitted. Keep the socket reachable until os.Exit.
var processRoot *rootChannel

func main() {
	os.Exit(mainStatus())
}

func mainStatus() (status int) {
	defer func() {
		if recover() != nil {
			_, _ = fmt.Fprintln(os.Stderr, "c80 comparison negative: internal_unavailable")
			status = 1
		}
	}()
	// Prevent pgx and the keyring from consulting inherited environment values.
	os.Clearenv()
	out, err := run()
	if err != nil {
		code, ok := err.(stageError)
		if !ok {
			code = "internal_unavailable"
		}
		_, _ = fmt.Fprintln(os.Stderr, "c80 comparison negative:", code)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "c80 comparison negative: output_unavailable")
		return 1
	}
	return 0
}

func run() (out result, runErr error) {
	if len(os.Args) != 2 || os.Args[1] != "produce" || os.Getuid() != 10002 || os.Geteuid() != 10002 ||
		os.Getgid() != 10002 || os.Getegid() != 10002 {
		return result{}, stageError("invocation_unavailable")
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 {
		return result{}, stageError("invocation_unavailable")
	}
	// Linux attaches PDEATHSIG to the current thread. Keep that thread alive
	// until process exit and close the parent-death race before using FD5.
	runtime.LockOSThread()
	parent := os.Getppid()
	if parent <= 1 || unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0) != nil || os.Getppid() != parent {
		return result{}, stageError("parent_unavailable")
	}
	input, endpoint, err := readInput()
	if err != nil {
		return result{}, stageError("private_input_unavailable")
	}
	root, err := openRootChannel()
	if err != nil {
		return result{}, stageError("parent_unavailable")
	}
	processRoot = root
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := openRuntimePool(ctx, endpoint, root)
	if err != nil {
		return result{}, stageError("backend_unavailable")
	}
	defer pool.Close()
	signer, cleanup, err := openSigner()
	if err != nil {
		return result{}, stageError("signer_unavailable")
	}
	defer func() {
		if cleanup() != nil {
			out = result{}
			runErr = stageError("signer_cleanup_unavailable")
		}
	}()
	return produce(ctx, pool, input, signer)
}

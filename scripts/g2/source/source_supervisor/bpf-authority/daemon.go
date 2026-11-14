//go:build linux && amd64 && !cgo

package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

const runtimeDockerDataRoot = "/var/lib/g2/docker"

var fixedDockerArgs = []string{
	"/usr/bin/dockerd", "--host=unix:///var/run/docker.sock", "--group=g2coord",
	"--data-root=" + runtimeDockerDataRoot, "--exec-root=/run/g2-docker", "--storage-driver=overlay2",
	"--bridge=none", "--iptables=false", "--ip6tables=false", "--ip-forward=false",
	"--ip-masq=false", "--userland-proxy=false", "--log-driver=none",
	"--default-address-pool=base=172.29.0.0/16,size=24",
	"--add-runtime=g2-bpf=/opt/g2/g2-bpf-runtime", "--default-runtime=g2-bpf",
	"--exec-opt=native.cgroupdriver=cgroupfs",
}

// runDaemonChild is the actual stopped child of the source or reboot owner.
// The parent registers and WATCHes it before SIGCONT; after that the same PID
// execs the held package file, without an intermediate shell or fork.
func runDaemonChild() error {
	runtime.LockOSThread()
	if len(os.Args) != 2 || os.Args[0] != "/opt/g2/g2-bpf-authority" || os.Args[1] != "daemon-child" || os.Geteuid() != 0 || unix.Gettid() != os.Getpid() {
		return errGuardProtocol
	}
	if err := managedContainerdPreflight(); err != nil {
		return err
	}
	environment := os.Environ()
	if err := replaceExactPath(environment, baseGuestPath, managedDockerPath); err != nil {
		return err
	}
	program, _, err := openTrustedRegular("/usr/bin/dockerd", 128<<20)
	if err != nil {
		return err
	}
	defer program.Close()
	if err = unix.Kill(os.Getpid(), unix.SIGSTOP); err != nil {
		return err
	}
	// The broker verifies this held inode against its package-pinned digest via
	// the actual sched_process_exec event. /proc/self/fd makes replacement of
	// the named path after the stop unable to change the executed file.
	execPath := "/proc/self/fd/" + strconv.Itoa(int(program.Fd()))
	if err = syscall.Exec(execPath, fixedDockerArgs, environment); err != nil {
		return fmt.Errorf("BPF_AUTH_UNAVAILABLE: dockerd exec: %w", err)
	}
	return errGuardProtocol
}

//go:build linux

package main

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/deploy"
)

var errOrdinaryPrivateInvocation = errors.New("ordinary private invocation rejected")

func runOrdinaryTargetLauncher(args []string) error {
	if len(args) != 3 || args[0] != "_ordinary-launch-target" {
		return errOrdinaryPrivateInvocation
	}
	return deploy.RunOrdinaryTargetLauncher(args[1], args[2])
}

// The probe runs the extracted target admin itself, never the host admin.
// No credentials, arbitrary paths, Docker access or executable overrides enter
// this mode. All output is the embedded compiler contract.
func runOrdinaryTargetContract(args []string) error {
	if len(args) != 1 || args[0] != "_ordinary-target-contract" || os.Getuid() != 10002 || os.Geteuid() != 10002 || os.Getgid() != 10001 || os.Getegid() != 10001 {
		return errOrdinaryPrivateInvocation
	}
	if err := deploy.PrepareOrdinaryTargetProcess(); err != nil {
		return errOrdinaryPrivateInvocation
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 {
		return errOrdinaryPrivateInvocation
	}
	if len(os.Environ()) != 0 {
		return errOrdinaryPrivateInvocation
	}
	noNewPrivileges, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil || noNewPrivileges != 1 {
		return errOrdinaryPrivateInvocation
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil || len(status) > 64<<10 {
		return errOrdinaryPrivateInvocation
	}
	for _, field := range []string{"CapEff:\t0000000000000000", "CapPrm:\t0000000000000000", "CapAmb:\t0000000000000000"} {
		if !strings.Contains("\n"+string(status), "\n"+field+"\n") {
			return errOrdinaryPrivateInvocation
		}
	}
	if err := unix.Access("/var/run/docker.sock", unix.W_OK); err == nil || (!errors.Is(err, unix.EACCES) && !errors.Is(err, unix.ENOENT) && !errors.Is(err, unix.EPERM)) {
		return errOrdinaryPrivateInvocation
	}
	return deploy.WriteOrdinaryTargetContract(os.Stdout)
}

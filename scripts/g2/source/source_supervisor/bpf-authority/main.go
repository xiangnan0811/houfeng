//go:build linux && amd64 && !cgo

package main

import (
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

func main() {
	runtime.LockOSThread()
	var wrapperStage ownerProbeRuntimeStage
	status, err := authorityMain(&wrapperStage)
	if err != nil {
		if wrapperStage != 0 && len(os.Args) > 0 && os.Args[0] == "/opt/g2/g2-bpf-runtime" {
			reportOwnerProbeWrapperFailure(os.Args[1:], wrapperStage, err)
		}
		_, _ = fmt.Fprintln(os.Stderr, "BPF_AUTH_UNAVAILABLE")
		if status == 0 {
			status = 1
		}
	}
	if os.Getpid() == 1 {
		// Only the isolated direct-boot entry can be PID1. Failure never execs
		// the original init; a failed bootstrap must not start services.
		_ = unix.Reboot(unix.LINUX_REBOOT_CMD_POWER_OFF)
		for {
			_ = unix.Pause()
		}
	}
	os.Exit(status)
}

func authorityMain(wrapperStage *ownerProbeRuntimeStage) (status int, err error) {
	if len(os.Args) == 2 && os.Args[0] == "/opt/g2-launch/g2-bpf-authority" && os.Args[1] == "launch-source" {
		g2d1Open('L')
		g2d1Mark("ENTRY")
	} else if len(os.Args) == 2 && os.Args[0] == "/g2-bpf-authority" && os.Args[1] == "broker" {
		g2d1Open('B')
	} else if role := g2d1HelperRole(os.Args); role != 0 {
		g2d1.fd, g2d1.role = 2, role
	} else if os.Getpid() == 1 && len(os.Args) == 1 && os.Args[0] == "/g2-early-init" {
		g2d1.fd, g2d1.role = 2, 'E'
	}
	defer func() {
		if err != nil {
			if g2d1.role == 'E' {
				g2d1Fail("EARLY_ENTRY", err)
			} else {
				g2d1TerminalFail(err)
			}
		}
		g2d1Close()
	}()
	if len(os.Args) == 0 || unix.Gettid() != os.Getpid() {
		if len(os.Args) > 0 && os.Args[0] == "/opt/g2/g2-bpf-runtime" {
			*wrapperStage = ownerProbeStageWrapperEntry
		}
		return 1, errGuardProtocol
	}
	if os.Args[0] == "/g2-early-init" && len(os.Args) == 1 {
		return 1, runEarlyInit()
	}
	if err := pinProcDirectory(); err != nil {
		if os.Args[0] == "/opt/g2/g2-bpf-runtime" {
			*wrapperStage = ownerProbeStageWrapperProc
		}
		return 1, err
	}
	if os.Args[0] == containerdLauncherPath {
		return 1, runManagedContainerdLauncher()
	}
	if os.Args[0] == "/opt/g2/g2-bpf-runtime" {
		stage, wrapperErr := runRuntimeWrapper(os.Args[1:])
		*wrapperStage = stage
		return 1, wrapperErr
	}
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "launch-source":
			return runSourceLauncher("source")
		case "launch-reboot":
			return runSourceLauncher("reboot")
		case "daemon-child":
			return 1, runDaemonChild()
		case "audit-reader":
			if os.Args[0] != "/g2-bpf-authority" {
				return 1, errGuardProtocol
			}
			return 0, runAuditReader(3, 4, 5, 6)
		case "terminal-observer":
			if os.Args[0] != "/g2-bpf-authority" {
				return 1, errGuardProtocol
			}
			return 0, runTerminalObserver(3, 4, 5, 6, 7, 8, 9, 10)
		case "broker":
			if os.Args[0] != "/g2-bpf-authority" {
				return 1, errGuardProtocol
			}
			return 0, runAuthorityBroker(3)
		}
	}
	if len(os.Args) == 3 && os.Args[1] == "selftest" {
		if os.Args[2] == "early-seccomp-probe" && os.Args[0] == "/g2-bpf-authority" {
			return 0, runEarlySeccompProbeChild()
		}
		return 0, runAuthoritySelftest(os.Args[2])
	}
	return 1, errGuardProtocol
}

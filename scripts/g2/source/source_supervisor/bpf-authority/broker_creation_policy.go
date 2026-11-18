//go:build linux && amd64 && !cgo

package main

import "golang.org/x/sys/unix"

// authorizeCreationCapture is called after exact byte capture and before
// commitment. Counts are per invocation, not inherited by a later runc task.
func authorizeCreationCapture(inv *runtimeInvocation, captured *capturedRuntimeEffect) error {
	if inv == nil || inv.grant == nil || inv.command != "create" || captured == nil {
		return errGuardProtocol
	}
	switch captured.command {
	case unix.BPF_MAP_CREATE:
		if inv.featureMaps != 0 || !featureMapAttribute(captured.attribute) {
			return errGuardProtocol
		}
	case unix.BPF_PROG_LOAD:
		if captured.program == nil {
			return errGuardProtocol
		}
		switch captured.programKind {
		case "extended_info_probe":
			if inv.extendedProbes != 0 || len(captured.program.log) != 0 {
				return errGuardProtocol
			}
		case "replace_probe":
			if inv.replaceProbes != 0 {
				return errGuardProtocol
			}
		case "device":
			if inv.devicePending || inv.deviceSucceeded || inv.deviceLoads >= 2 {
				return errGuardProtocol
			}
			nextSize := uint32(len(captured.program.log))
			if inv.deviceLoads == 0 {
				if nextSize != 0 {
					return errGuardProtocol
				}
			} else {
				if inv.deviceLastResult >= 0 || inv.deviceLastLogSize != 0 || inv.deviceLastLogTrueSize != 0 || nextSize != 131072 {
					return errGuardProtocol
				}
			}
		default:
			return errGuardProtocol
		}
	default:
		return errGuardProtocol
	}
	return nil
}

// noteCreationCommitted consumes one permitted probe only after the original
// notification has passed all checks and its transaction is committed.
func noteCreationCommitted(inv *runtimeInvocation, captured *capturedRuntimeEffect) error {
	if err := authorizeCreationCapture(inv, captured); err != nil {
		return err
	}
	if captured.command == unix.BPF_MAP_CREATE {
		inv.featureMaps++
		return nil
	}
	switch captured.programKind {
	case "extended_info_probe":
		inv.extendedProbes++
	case "replace_probe":
		inv.replaceProbes++
	case "device":
		inv.deviceLoads++
		inv.devicePending = true
		inv.deviceLastLogSize = uint32(len(captured.program.log))
	}
	return nil
}

// noteCreationResult consumes the actual syscall outcome; it never derives
// success from a request, notification delivery, or expected probe errno.
func noteCreationResult(inv *runtimeInvocation, captured *capturedRuntimeEffect, effect bpfEffect) error {
	if inv == nil || captured == nil || (effect.FD >= 0) == (effect.Errno != 0) {
		return errGuardProtocol
	}
	if captured.command != unix.BPF_PROG_LOAD {
		return nil
	}
	if captured.programKind == "extended_info_probe" && (effect.Errno != unix.EBADF || effect.FD >= 0) {
		return errGuardProtocol
	}
	if captured.programKind == "device" {
		if !inv.devicePending {
			return errGuardProtocol
		}
		inv.devicePending = false
		var logTrueSize uint32
		if captured.program != nil && len(captured.program.returnedOuter) >= 144 {
			logTrueSize = guardU32(captured.program.returnedOuter, 140)
		}
		inv.deviceLastLogTrueSize = logTrueSize
		if effect.Errno != 0 {
			inv.deviceLastResult = -int64(effect.Errno)
		} else {
			inv.deviceLastResult = int64(effect.FD)
			inv.deviceSucceeded = true
		}
	}
	return nil
}

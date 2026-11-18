//go:build linux && amd64 && !cgo

package main

import (
	"bytes"

	"golang.org/x/sys/unix"
)

// capturedRuntimeEffect is constructed exclusively from the actual held
// notifying task's mm. Pointer fields are removed from the public attribute
// copy, while the validated buffers remain private to the broker.
type capturedRuntimeEffect struct {
	command               uint32
	attribute, normalized []byte
	program               *programInput
	query                 *queryInput
	attach                *attachInput
	getID                 *getProgramIDInput
	programKind           string
}

func captureRuntimeEffect(inv *runtimeInvocation, h *heldNotification, logScratch []byte) (*capturedRuntimeEffect, error) {
	if inv == nil || inv.grant == nil || h == nil || h.memory == nil || h.notice.Args[0] > uint64(^uint32(0)) {
		return nil, errGuardProtocol
	}
	command := uint32(h.notice.Args[0])
	if inv.command != "create" && inv.command != "delete" {
		return nil, errGuardProtocol
	}
	if inv.command == "delete" && command != unix.BPF_PROG_DETACH {
		return nil, errGuardProtocol
	}
	raw, err := capturedAttribute(h.memory, h.notice)
	if err != nil {
		return nil, err
	}
	out := &capturedRuntimeEffect{command: command, attribute: raw}
	switch command {
	case unix.BPF_MAP_CREATE:
		if !featureMapAttribute(raw) {
			return nil, errGuardProtocol
		}
		out.normalized = append([]byte(nil), raw...)
	case unix.BPF_PROG_LOAD:
		out.program, err = programLoadInput(h.memory, h.notice, raw, logScratch)
		if err != nil {
			return nil, err
		}
		out.normalized = out.program.normalized
		device, e := deviceProgram()
		if e != nil {
			return nil, e
		}
		if bytes.Equal(out.program.instructions, device) {
			out.programKind = "device"
		} else if guardU32(out.program.outer, 0) == unix.BPF_PROG_TYPE_SOCKET_FILTER {
			out.programKind = "extended_info_probe"
		} else {
			out.programKind = "replace_probe"
		}
	case unix.BPF_PROG_QUERY:
		out.query, err = parseQueryInput(h.notice, raw)
		if err != nil {
			return nil, err
		}
		out.normalized = out.query.normalized
		if out.query.capacity > 0 {
			if _, err = h.memory.copyIn(out.query.idsAddress, int(out.query.capacity)*4); err != nil {
				return nil, err
			}
			if out.query.flagsAddress != 0 {
				if _, err = h.memory.copyIn(out.query.flagsAddress, int(out.query.capacity)*4); err != nil {
					return nil, err
				}
			}
		}
	case unix.BPF_PROG_ATTACH, unix.BPF_PROG_DETACH:
		out.attach, err = captureAttach(h.notice, raw)
		if err != nil {
			return nil, err
		}
		out.normalized = out.attach.normalized
	case unix.BPF_PROG_GET_FD_BY_ID:
		out.getID, err = captureGetProgramID(h.notice, raw)
		if err != nil {
			return nil, err
		}
		out.normalized = out.getID.normalized
	case unix.BPF_OBJ_GET_INFO_BY_FD:
		// The object class is determined only after importing the actual fd
		// and matching it to an owned incarnation. captureScalarInfo then
		// snapshots the class-specific nested output buffer.
		if len(raw) < 16 || !guardZero(raw[16:]) {
			return nil, errGuardProtocol
		}
	default:
		return nil, errGuardProtocol
	}
	return out, nil
}

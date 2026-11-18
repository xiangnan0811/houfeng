//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/cilium/ebpf/asm"
	"golang.org/x/sys/unix"
)

// deviceRule is a clean, ordered rule, rather than an OCI input rule. The
// complete list is fixed independently of anything captured from runc.
type deviceRule struct {
	deviceType   int32
	major, minor int32 // -1 means wildcard
	access       int32
}

var fixedDeviceRules = [...]deviceRule{
	{unix.BPF_DEVCG_DEV_BLOCK, -1, -1, unix.BPF_DEVCG_ACC_MKNOD},
	{unix.BPF_DEVCG_DEV_CHAR, -1, -1, unix.BPF_DEVCG_ACC_MKNOD},
	{unix.BPF_DEVCG_DEV_CHAR, 1, 3, 7},
	{unix.BPF_DEVCG_DEV_CHAR, 1, 5, 7},
	{unix.BPF_DEVCG_DEV_CHAR, 1, 7, 7},
	{unix.BPF_DEVCG_DEV_CHAR, 1, 8, 7},
	{unix.BPF_DEVCG_DEV_CHAR, 1, 9, 7},
	{unix.BPF_DEVCG_DEV_CHAR, 5, 0, 7},
	{unix.BPF_DEVCG_DEV_CHAR, 5, 1, 7},
	{unix.BPF_DEVCG_DEV_CHAR, 5, 2, 7},
	{unix.BPF_DEVCG_DEV_CHAR, 10, 200, 7},
	{unix.BPF_DEVCG_DEV_CHAR, 136, -1, 7},
}

// deviceProgram generates the exact instruction sequence of the pinned runc
// clean-rules generator, including its ALU32 instructions and jump labels.
// Equivalence of a different instruction stream is intentionally insufficient.
func deviceProgram() ([]byte, error) {
	ins := asm.Instructions{
		asm.LoadMem(asm.R2, asm.R1, 0, asm.Word),
		asm.And.Imm32(asm.R2, 0xffff),
		asm.LoadMem(asm.R3, asm.R1, 0, asm.Word),
		asm.RSh.Imm32(asm.R3, 16),
		asm.LoadMem(asm.R4, asm.R1, 4, asm.Word),
		asm.LoadMem(asm.R5, asm.R1, 8, asm.Word),
	}
	for i, rule := range fixedDeviceRules {
		next := "block-" + strconv.Itoa(i+1)
		first := len(ins)
		ins = append(ins, asm.JNE.Imm(asm.R2, rule.deviceType, next))
		if rule.access != 7 {
			ins = append(ins, asm.Mov.Reg32(asm.R1, asm.R3), asm.And.Imm32(asm.R1, rule.access), asm.JNE.Reg(asm.R1, asm.R3, next))
		}
		if rule.major >= 0 {
			ins = append(ins, asm.JNE.Imm(asm.R4, rule.major, next))
		}
		if rule.minor >= 0 {
			ins = append(ins, asm.JNE.Imm(asm.R5, rule.minor, next))
		}
		ins = append(ins, asm.Mov.Imm32(asm.R0, 1), asm.Return())
		ins[first] = ins[first].WithSymbol("block-" + strconv.Itoa(i))
	}
	ins = append(ins, asm.Mov.Imm32(asm.R0, 0).WithSymbol("block-"+strconv.Itoa(len(fixedDeviceRules))), asm.Return())
	return marshalInstructions(ins)
}

func probeProgram() ([]byte, error) {
	return marshalInstructions(asm.Instructions{asm.Mov.Imm(asm.R0, 0), asm.Return()})
}

func marshalInstructions(ins asm.Instructions) ([]byte, error) {
	if ins.Size() > 4096*8 {
		return nil, fmt.Errorf("BPF_AUTH_INCOMPATIBLE: instruction limit")
	}
	var buf bytes.Buffer
	if err := ins.Marshal(&buf, binary.LittleEndian); err != nil {
		return nil, err
	}
	if buf.Len() == 0 || buf.Len()%8 != 0 {
		return nil, errGuardProtocol
	}
	return buf.Bytes(), nil
}

//go:build linux && amd64 && !cgo

#include "textflag.h"

// func asmProbeBarrierRead(fd int64, bufPtr uintptr) int64
TEXT ·asmProbeBarrierRead(SB), NOSPLIT, $0-24
	MOVQ	fd+0(FP), DI
	MOVQ	bufPtr+8(FP), SI
	MOVQ	$1, DX
	MOVQ	$0, AX		// SYS_read
	SYSCALL
	MOVQ	AX, ret+16(FP)
	RET

// func asmProbeBPF(cmd int64, attrPtr uintptr, size uintptr) int64
TEXT ·asmProbeBPF(SB), NOSPLIT, $0-32
	MOVQ	cmd+0(FP), DI
	MOVQ	attrPtr+8(FP), SI
	MOVQ	size+16(FP), DX
	MOVQ	$321, AX	// SYS_bpf
	SYSCALL
	MOVQ	AX, ret+24(FP)
	RET

// func asmProbeDup2AndBPF(oldfd int64, newfd int64, cmd int64, attrPtr uintptr, size uintptr) int64
TEXT ·asmProbeDup2AndBPF(SB), NOSPLIT, $0-48
	MOVQ	oldfd+0(FP), DI
	MOVQ	newfd+8(FP), SI
	MOVQ	$33, AX		// SYS_dup2
	SYSCALL
	CMPQ	AX, $0
	JL	dup_err
	MOVQ	cmd+16(FP), DI
	MOVQ	attrPtr+24(FP), SI
	MOVQ	size+32(FP), DX
	MOVQ	$321, AX	// SYS_bpf
	SYSCALL
	MOVQ	AX, ret+40(FP)
	RET
dup_err:
	MOVQ	AX, ret+40(FP)
	RET

// The child runs only raw syscalls. results[0] is the kernel-cleared child TID;
// subsequent int64 fields contain the actual TID, unshare, dup2 and BPF results.
TEXT ·asmProbeNonleaderThread(SB), NOSPLIT, $0-48
	MOVQ flags+0(FP), DI
	MOVQ childStack+8(FP), SI
	MOVQ attrPtr+16(FP), R12
	MOVQ results+24(FP), R13
	MOVQ unshareFiles+32(FP), R14
	XORQ DX, DX
	MOVQ R13, R10
	XORQ R8, R8
	MOVQ $56, AX
	SYSCALL
	CMPQ AX, $0
	JEQ probe_thread
	MOVQ AX, ret+40(FP)
	RET
probe_thread:
	MOVQ $186, AX // gettid
	SYSCALL
	MOVQ AX, 8(R13)
	CMPQ R14, $0
	JEQ probe_dup
	MOVQ $0x400, DI // CLONE_FILES
	MOVQ $272, AX // unshare
	SYSCALL
	MOVQ AX, 16(R13)
	CMPQ AX, $0
	JNE probe_exit
probe_dup:
	MOVQ $11, DI
	MOVQ $10, SI
	MOVQ $33, AX // dup2
	SYSCALL
	MOVQ AX, 24(R13)
	CMPQ AX, $10
	JNE probe_exit
	MOVQ $15, DI // BPF_OBJ_GET_INFO_BY_FD
	MOVQ R12, SI
	MOVQ $16, DX
	MOVQ $321, AX
	SYSCALL
	MOVQ AX, 32(R13)
probe_exit:
	XORQ DI, DI
	MOVQ $60, AX // exit; kernel clears child TID and wakes futex waiters
	SYSCALL
	JMP probe_exit

// The original leader exits via SYS_exit; the raw child waits for FD 8.
TEXT ·asmProbeLeaderExit(SB), NOSPLIT, $0-40
	MOVQ flags+0(FP), DI
	MOVQ childStack+8(FP), SI
	MOVQ attrPtr+16(FP), R12
	MOVQ results+24(FP), R13
	MOVQ $2, R14
	XORQ DX, DX
	MOVQ R13, R10
	XORQ R8, R8
	MOVQ $56, AX
	SYSCALL
	CMPQ AX, $0
	JEQ leader_child
	JL leader_clone_failed
	XORQ DI, DI
	MOVQ $60, AX
	SYSCALL
leader_clone_failed:
	MOVQ AX, ret+32(FP)
	RET
leader_child:
	MOVQ $186, AX
	SYSCALL
	MOVQ AX, 8(R13)
	SUBQ $8, SP
	MOVQ $8, DI
	MOVQ SP, SI
	MOVQ $1, DX
	XORQ AX, AX
	SYSCALL
	MOVQ AX, 16(R13)
	CMPQ AX, $1
	JNE leader_child_exit
	CMPB 0(SP), $0x5a
	JNE leader_child_exit
	MOVQ $15, DI
	MOVQ R12, SI
	MOVQ $16, DX
	MOVQ $321, AX
	SYSCALL
	MOVQ AX, 32(R13)
leader_child_exit:
	XORQ DI, DI
	MOVQ $60, AX
	SYSCALL
	JMP leader_child_exit

// Fork a child that waits for a permit before exiting without entering Go.
TEXT ·asmProbeForkExit(SB), NOSPLIT, $0-8
	MOVQ $57, AX
	SYSCALL
	CMPQ AX, $0
	JEQ probe_fork_child
	MOVQ AX, ret+0(FP)
	RET
probe_fork_child:
	SUBQ $8, SP
	MOVQ $8, DI
	MOVQ SP, SI
	MOVQ $1, DX
	XORQ AX, AX
	SYSCALL
	CMPQ AX, $1
	JNE probe_fork_failed
	CMPB 0(SP), $0x5a
	JNE probe_fork_failed
	XORQ DI, DI
	MOVQ $60, AX
	SYSCALL
probe_fork_failed:
	MOVQ $1, DI
	MOVQ $60, AX
	SYSCALL
	JMP probe_fork_failed

// func asmProbeClone3SetTID(clArgsPtr uintptr, clArgsSize uintptr, cmd int64, attrPtr uintptr, size uintptr, resultPtr uintptr) int64
TEXT ·asmProbeClone3SetTID(SB), NOSPLIT, $0-56
	MOVQ clArgsPtr+0(FP), DI
	MOVQ clArgsSize+8(FP), SI
	MOVQ cmd+16(FP), R12
	MOVQ attrPtr+24(FP), R13
	MOVQ size+32(FP), R14
	MOVQ resultPtr+40(FP), R15
	MOVQ $435, AX // SYS_clone3
	SYSCALL
	CMPQ AX, $0
	JEQ clone3_child
	MOVQ AX, ret+48(FP)
	RET
clone3_child:
	SUBQ $8, SP
	MOVQ $8, DI
	MOVQ SP, SI
	MOVQ $1, DX
	XORQ AX, AX
	SYSCALL
	MOVQ AX, 8(R15)
	CMPQ AX, $1
	JNE clone3_bad_gate
	CMPB 0(SP), $0x5a
	JNE clone3_bad_gate
	MOVQ	R12, DI
	MOVQ	R13, SI
	MOVQ	R14, DX
	MOVQ	$321, AX	// SYS_bpf
	SYSCALL
	MOVQ AX, 0(R15)
	XORQ	DI, DI
	MOVQ	$231, AX	// SYS_exit_group
	SYSCALL
	JMP clone3_child
clone3_bad_gate:
	MOVQ $1, DI
	MOVQ $231, AX
	SYSCALL
	JMP clone3_bad_gate

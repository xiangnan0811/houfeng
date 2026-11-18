//go:build linux && amd64

#include "textflag.h"

// This function is terminal. All referenced memory is pinned and strongly
// rooted before entry; no Go runtime transition occurs in this suffix.
TEXT ·ordinaryExecBridge(SB), NOSPLIT|NOFRAME, $0-48
	MOVQ $2, DI                 // SIG_SETMASK
	MOVQ mask+40(FP), SI
	XORQ DX, DX
	MOVQ $8, R10                // kernel sigset size
	MOVQ $14, AX                // rt_sigprocmask
	SYSCALL
	TESTQ AX, AX
	JS failure

	MOVQ first+8(FP), DI
	MOVL $0xffffffff, SI
	MOVQ $6, DX                 // CLOSE_RANGE_UNSHARE | CLOSE_RANGE_CLOEXEC
	MOVQ $436, AX               // close_range
	SYSCALL
	TESTQ AX, AX
	JS failure

	MOVQ targetFD+0(FP), DI
	MOVQ path+16(FP), SI
	MOVQ argv+24(FP), DX
	MOVQ env+32(FP), R10
	MOVQ $4096, R8              // AT_EMPTY_PATH
	MOVQ $322, AX               // execveat
	SYSCALL

failure:
	MOVQ $127, DI
	MOVQ $231, AX               // exit_group
	SYSCALL
	JMP failure

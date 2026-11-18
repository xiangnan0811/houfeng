//go:build linux && arm64

#include "textflag.h"

// This function is terminal. All referenced memory is pinned and strongly
// rooted before entry; no Go runtime transition occurs in this suffix.
TEXT ·ordinaryExecBridge(SB), NOSPLIT|NOFRAME, $0-48
	MOVD $2, R0                 // SIG_SETMASK
	MOVD mask+40(FP), R1
	MOVD $0, R2
	MOVD $8, R3                 // kernel sigset size
	MOVD $135, R8               // rt_sigprocmask
	SVC
	CMP $0, R0
	BLT failure

	MOVD first+8(FP), R0
	MOVD $0xffffffff, R1
	MOVD $6, R2                 // CLOSE_RANGE_UNSHARE | CLOSE_RANGE_CLOEXEC
	MOVD $436, R8               // close_range
	SVC
	CMP $0, R0
	BLT failure

	MOVD targetFD+0(FP), R0
	MOVD path+16(FP), R1
	MOVD argv+24(FP), R2
	MOVD env+32(FP), R3
	MOVD $4096, R4              // AT_EMPTY_PATH
	MOVD $281, R8               // execveat
	SVC

failure:
	MOVD $127, R0
	MOVD $94, R8                // exit_group
	SVC
	JMP failure

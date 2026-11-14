//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// These identifiers select templates frozen into the candidate. They never
// authorize caller-provided argv or a caller-defined device policy.
func allowedPurpose(role, purpose string) bool {
	switch role {
	case "BOOT_OWNER":
		switch purpose {
		case "postgres-uid", "postgres-gid", "dns-preflight":
			return true
		}
	case "REALM_ROOT":
		switch purpose {
		case "postgres-init-app", "postgres-init-w1", "postgres-init-w2", "postgres-init-control", "postgres-live-app", "postgres-live-w1", "postgres-live-w2", "postgres-live-control", "browser-negative":
			return true
		}
	case "RECOVERY_OBSERVER":
		switch purpose {
		case "recovery-app", "recovery-w1", "recovery-w2", "recovery-control":
			return true
		}
	}
	return false
}

func allowedObservation(role, observation string) bool {
	switch role {
	case "REALM_ROOT":
		return observation == "app_rollback_dump" || observation == "app_rollback_restore" || observation == "negative_pg_dump"
	case "RECOVERY_OBSERVER":
		return observation == "pg_controldata"
	}
	return false
}

func allowedOperation(role, op string) bool {
	switch role {
	case "BOOT_OWNER":
		switch op {
		case "REGISTER_DAEMON", "REGISTER_REALM", "ADMIT", "SETTLE", "READ_STATUS", "READ_EVIDENCE", "QUIESCE", "RELEASE":
			return true
		}
	case "REALM_ROOT":
		switch op {
		case "ADMIT", "ADMIT_EXEC", "TRACK_EXEC", "CANCEL_EXEC", "BEGIN_ENDPOINT_TRANSITION", "END_ENDPOINT_TRANSITION", "SETTLE", "READ_STATUS", "READ_EVIDENCE", "QUIESCE", "RELEASE", "AUDIT_CUT", "SELFTEST_FD_ARM", "REGISTER_SELFTEST_PROBE", "SELFTEST_RUNTIME_ARM", "SELFTEST_BASELINE_STEP", "SELFTEST_REVOKE_AND_CLOSE", "SELFTEST_OBSERVATION_FAULT":
			return true
		}
	case "RECOVERY_PENDING":
		return op == "READ_STATUS" || op == "REGISTER_RECOVERY"
	case "RECOVERY_OBSERVER":
		switch op {
		case "REGISTER_DAEMON", "ADMIT", "ADMIT_EXEC", "TRACK_EXEC", "CANCEL_EXEC", "SETTLE", "READ_STATUS", "READ_EVIDENCE", "QUIESCE", "RELEASE", "AUDIT_CUT":
			return true
		}
	}
	return false
}

// Control credentials identify a process through its held leader and TGID;
// SCM_CREDENTIALS cannot identify which thread sent a frame. This process
// identity is distinct from the actual TID carried by each seccomp BPF
// notification, which must be pinned and checked independently.
type controlPrincipal struct {
	ID, TaskCookie, StartNS                    uint64
	Role                                       string
	PIDFD                                      int
	PID, UID, GID                              uint32
	ExecutableSHA256, ArgvSHA256, CgroupSHA256 string
	NextRequest                                uint64
	Terminal                                   bool
	Transferred                                bool
	TransferRequest                            uint64
	TransferChildPIDFD                         int
	Replay                                     map[uint64]controlReplay
}

type controlReplay struct {
	Request, Ack []byte
	HeldFDs      []int
}

func exactNumber(value any) (uint64, bool) {
	v, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(string(v), 10, 64)
	return n, err == nil
}

func livePIDFD(fd int) bool {
	selected, _ := livePIDFDObserved(fd, nil, selectorPollPre)
	return selected
}

func livePIDFDObserved(fd int, observation *selectorObservation, phase selectorPollPhase) (bool, error) {
	if fd < 0 {
		if observation != nil {
			*observation = selectorObservation{reason: selectorInvalidFD}
		}
		return false, nil
	}
	p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(p, 0)
	if err == nil && n == 0 && p[0].Revents == 0 {
		return true, nil
	}
	if observation != nil {
		if err != nil {
			reason := selectorPrePollError
			if phase == selectorPollPost {
				reason = selectorPostPollError
			}
			*observation = selectorObservation{reason: reason, err: err}
		} else {
			reason := selectorPrePollRejected
			if phase == selectorPollPost {
				reason = selectorPostPollRejected
			}
			*observation = selectorObservation{reason: reason, pollFacts: normalizeSelectorPoll(n, p[0].Revents)}
		}
	}
	return false, err
}

// sameHeldDescription compares the exact open file description already
// imported into this process. An unsupported/denied comparison is failure,
// never an inode-only approximation.
func sameHeldDescription(a, b int) bool {
	if a < 0 || b < 0 {
		return false
	}
	pid := uintptr(os.Getpid())
	r, _, errno := unix.Syscall6(unix.SYS_KCMP, pid, pid, 0 /* KCMP_FILE */, uintptr(a), uintptr(b), 0)
	return errno == 0 && r == 0
}

func (p *controlPrincipal) authenticate(m map[string]any, raw []byte, cred *unix.Ucred, actual guardTask, bootID, epoch, runToken string, fds []int) (*controlReplay, error) {
	if p == nil || p.Terminal || p.ID == 0 || p.TaskCookie == 0 || p.StartNS == 0 || p.NextRequest == 0 || p.PID == 0 || p.UID != 0 || cred == nil || cred.Pid <= 0 || uint32(cred.Pid) != p.PID || cred.Uid != p.UID || cred.Gid != p.GID || actual.Cookie != p.TaskCookie || actual.TGID != p.PID || actual.StartBoottimeNS != p.StartNS || !livePIDFD(p.PIDFD) {
		return nil, errControlFrame
	}
	process, err := observeProcess(p.PID, p.PIDFD)
	if err != nil || process.ExecutableSHA256 != p.ExecutableSHA256 || process.ArgvSHA256 != p.ArgvSHA256 || process.CgroupSHA256 != p.CgroupSHA256 {
		return nil, errControlFrame
	}
	if !exactKeys(m, "version", "boot_id", "epoch", "run_token", "principal_id", "request_id", "operation", "body") || m["version"] != json.Number("1") || m["boot_id"] != bootID || m["epoch"] != epoch || m["run_token"] != runToken {
		return nil, errControlFrame
	}
	id, ok := exactNumber(m["principal_id"])
	if !ok || id != p.ID {
		return nil, errControlFrame
	}
	requestID, ok := exactNumber(m["request_id"])
	if !ok || requestID == 0 {
		return nil, errControlFrame
	}
	operation, ok := m["operation"].(string)
	if !ok {
		return nil, errControlFrame
	}
	if _, ok := m["body"].(map[string]any); !ok {
		return nil, errControlFrame
	}
	if requestID < p.NextRequest {
		if p.Transferred && (requestID != p.TransferRequest || !livePIDFD(p.TransferChildPIDFD)) {
			return nil, errControlFrame
		}
		old, exists := p.Replay[requestID]
		if !exists || !bytes.Equal(old.Request, raw) || len(old.HeldFDs) != len(fds) {
			return nil, errControlFrame
		}
		for i := range fds {
			if !sameHeldDescription(old.HeldFDs[i], fds[i]) {
				return nil, errControlFrame
			}
		}
		return &old, nil
	}
	if p.Transferred || requestID != p.NextRequest || requestID == math.MaxUint64 || !allowedOperation(p.Role, operation) {
		return nil, errControlFrame
	}
	return nil, nil
}

// commitReply records the exact request and descriptor references before
// sending an ACK. The caller transfers ownership of heldFDs; they remain open
// for the live principal so a replay cannot exchange a same-inode file.
func (p *controlPrincipal) commitReply(requestID uint64, request, ack []byte, heldFDs []int) error {
	if p == nil || p.Terminal || p.Transferred || requestID != p.NextRequest || requestID == math.MaxUint64 || len(request) == 0 || len(request) > maxControlFrame || len(ack) == 0 || len(ack) > maxControlFrame {
		return errControlFrame
	}
	if p.Replay == nil {
		p.Replay = make(map[uint64]controlReplay)
	}
	if _, exists := p.Replay[requestID]; exists {
		return errControlFrame
	}
	for _, fd := range heldFDs {
		if fd < 0 {
			return errControlFrame
		}
	}
	p.Replay[requestID] = controlReplay{append([]byte(nil), request...), append([]byte(nil), ack...), append([]int(nil), heldFDs...)}
	p.NextRequest++
	return nil
}

// commitTransferredFDArmDenial advances only the explicitly rejected fixed
// selftest request. It never restores operation authority to a transferred owner.
func (p *controlPrincipal) commitTransferredFDArmDenial(requestID uint64, request, ack []byte) error {
	if p == nil || p.Terminal || !p.Transferred || p.Role != "BOOT_OWNER" || requestID != p.NextRequest || requestID == math.MaxUint64 || !livePIDFD(p.TransferChildPIDFD) || len(request) == 0 || len(request) > maxControlFrame || len(ack) == 0 || len(ack) > maxControlFrame {
		return errControlFrame
	}
	parsed, err := parseCanonicalControl(request)
	if err != nil || parsed["operation"] != "SELFTEST_FD_ARM" || parsed["principal_id"] != jsonU64(p.ID) || parsed["request_id"] != jsonU64(requestID) || !recordableFDArm(parsed) {
		return errControlFrame
	}
	if p.Replay == nil {
		p.Replay = make(map[uint64]controlReplay)
	}
	if _, exists := p.Replay[requestID]; exists {
		return errControlFrame
	}
	p.Replay[requestID] = controlReplay{append([]byte(nil), request...), append([]byte(nil), ack...), nil}
	p.NextRequest++
	return nil
}

func (p *controlPrincipal) retire() error {
	if p == nil || p.Terminal {
		return errControlFrame
	}
	p.Terminal = true
	var errs []error
	for _, replay := range p.Replay {
		for _, fd := range replay.HeldFDs {
			if err := unix.Close(fd); err != nil && !errors.Is(err, unix.EINTR) {
				errs = append(errs, err)
			}
		}
	}
	p.Replay = nil
	return errors.Join(errs...)
}

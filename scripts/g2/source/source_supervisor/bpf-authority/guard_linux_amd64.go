//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"errors"
	"math"
	"unsafe"

	"golang.org/x/sys/unix"
)

// guardDevice is used only by the broker's original locked OS thread. In
// particular, the kernel authenticates the calling task, not its thread group.
type guardDevice struct {
	fd         int
	diagnostic *ownerProbeEvidenceState
}

var errGuardProtocol = errors.New("BPF_AUTH_UNAVAILABLE: invalid guard response")

const (
	guardVersion    = 5
	guardRecordSize = 192
	guardMaxRecords = 64
	guardEventsSize = 12344
)

func guardMessage(size int) []byte {
	b := make([]byte, size)
	binary.LittleEndian.PutUint32(b, guardVersion)
	return b
}

func guardIOCTL(nr, size uintptr) uintptr {
	return uintptr(3)<<30 | size<<16 | uintptr(0xb7)<<8 | nr
}

func (g guardDevice) call(nr uintptr, b []byte) error {
	if g.fd < 0 || len(b) < 8 || len(b) > 16383 || binary.LittleEndian.Uint32(b) != guardVersion || binary.LittleEndian.Uint32(b[4:]) != 0 {
		return errGuardProtocol
	}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(g.fd), guardIOCTL(nr, uintptr(len(b))), uintptr(unsafe.Pointer(&b[0])))
	if errno != 0 {
		// In particular EFAULT is not evidence that the operation had no
		// effect: the module may have completed it before copyout failed.
		return errno
	}
	if binary.LittleEndian.Uint32(b) != guardVersion || binary.LittleEndian.Uint32(b[4:]) != 0 {
		return errGuardProtocol
	}
	return nil
}

func guardU32(b []byte, off int) uint32      { return binary.LittleEndian.Uint32(b[off:]) }
func guardU64(b []byte, off int) uint64      { return binary.LittleEndian.Uint64(b[off:]) }
func guardPut32(b []byte, off int, v uint32) { binary.LittleEndian.PutUint32(b[off:], v) }
func guardPut64(b []byte, off int, v uint64) { binary.LittleEndian.PutUint64(b[off:], v) }

func guardZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

type guardSealResult struct {
	Epoch      [16]byte
	LinkIDs    [3]uint32
	ProgramIDs [3]uint32
}

func (g guardDevice) seal(fds [3]int, digest [32]byte) (guardSealResult, error) {
	var out guardSealResult
	if guardZero(digest[:]) {
		return out, errGuardProtocol
	}
	b := guardMessage(96)
	guardPut32(b, 8, 3)
	for i, fd := range fds {
		if fd < 0 || int64(fd) > math.MaxInt32 {
			return out, errGuardProtocol
		}
		guardPut32(b, 12+i*4, uint32(fd))
	}
	copy(b[24:56], digest[:])
	if err := g.call(1, b); err != nil {
		return out, err
	}
	if guardU32(b, 8) != 3 || !equalBytes(b[24:56], digest[:]) {
		return out, errGuardProtocol
	}
	for i, fd := range fds {
		if guardU32(b, 12+i*4) != uint32(fd) {
			return out, errGuardProtocol
		}
	}
	copy(out.Epoch[:], b[56:72])
	if guardZero(out.Epoch[:]) {
		return guardSealResult{}, errGuardProtocol
	}
	for i := range 3 {
		out.LinkIDs[i] = guardU32(b, 72+i*4)
		out.ProgramIDs[i] = guardU32(b, 84+i*4)
		if out.LinkIDs[i] == 0 || out.ProgramIDs[i] == 0 {
			return guardSealResult{}, errGuardProtocol
		}
	}
	return out, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type guardStatus struct {
	Epoch                  [16]byte
	State, Sticky          uint32
	AuthorityPhase         uint32
	Sequence, AckCursor    uint64
	ObserverAckCursor      uint64
	Lost, ProbeMissed      uint64
	BrokerCookie           uint64
	ObserverCookie         uint64
	DeathSequence          uint64
	ModuleInstance         uint64
	ModuleDev, ModuleIno   uint64
	ModuleSize             uint64
	LinkIDs                [3]uint32
	ProgramIDs             [3]uint32
	LinkAttached           [3]uint32
	BootstrapDigest        [32]byte
	ObserverBindingsSHA256 [32]byte
}

func (g guardDevice) status() (guardStatus, error) {
	b := guardMessage(240)
	if err := g.call(2, b); err != nil {
		return guardStatus{}, err
	}
	return decodeGuardStatus(b)
}

func (g guardDevice) statusObserved(source uint64) (guardStatus, error) {
	if g.diagnostic == nil {
		return g.status()
	}
	before := evidenceNow()
	status, err := g.status()
	after := evidenceNow()
	g.diagnostic.status(source, status, err, before, after)
	return status, err
}

// The seal identity is populated atomically by SEAL. Release can instead
// mark an unsealed guard DEAD, leaving the entire identity absent.
func decodeGuardStatus(b []byte) (guardStatus, error) {
	var out guardStatus
	if len(b) != 240 || guardU32(b, 0) != guardVersion || guardU32(b, 4) != 0 {
		return out, errGuardProtocol
	}
	copy(out.Epoch[:], b[8:24])
	out.State, out.Sticky = guardU32(b, 24), guardU32(b, 28)
	out.AuthorityPhase = guardU32(b, 32)
	if guardU32(b, 36) != 0 {
		return guardStatus{}, errGuardProtocol
	}
	out.Sequence = guardU64(b, 40)
	out.AckCursor = guardU64(b, 48)
	out.ObserverAckCursor = guardU64(b, 56)
	out.Lost = guardU64(b, 64)
	out.ProbeMissed = guardU64(b, 72)
	out.BrokerCookie = guardU64(b, 80)
	out.ObserverCookie = guardU64(b, 88)
	out.DeathSequence = guardU64(b, 96)
	out.ModuleInstance = guardU64(b, 104)
	out.ModuleDev = guardU64(b, 112)
	out.ModuleIno = guardU64(b, 120)
	out.ModuleSize = guardU64(b, 128)
	for i := range 3 {
		out.LinkIDs[i] = guardU32(b, 136+i*4)
		out.ProgramIDs[i] = guardU32(b, 148+i*4)
		out.LinkAttached[i] = guardU32(b, 160+i*4)
		if out.LinkAttached[i] > 1 {
			return guardStatus{}, errGuardProtocol
		}
	}
	if guardU32(b, 172) != 0 {
		return guardStatus{}, errGuardProtocol
	}
	copy(out.BootstrapDigest[:], b[176:208])
	copy(out.ObserverBindingsSHA256[:], b[208:240])
	if out.State > 2 || out.AuthorityPhase > 2 || out.AckCursor > out.Sequence || out.ObserverAckCursor > out.Sequence {
		return guardStatus{}, errGuardProtocol
	}
	if out.ModuleInstance == 0 || out.ModuleIno == 0 || out.ModuleSize == 0 || guardZero(out.ObserverBindingsSHA256[:]) {
		return guardStatus{}, errGuardProtocol
	}
	completeSeal := !guardZero(out.Epoch[:]) && !guardZero(out.BootstrapDigest[:])
	absentSeal := guardZero(out.Epoch[:]) && guardZero(out.BootstrapDigest[:])
	for i := range 3 {
		completeSeal = completeSeal && out.LinkIDs[i] != 0 && out.ProgramIDs[i] != 0
		absentSeal = absentSeal && out.LinkIDs[i] == 0 && out.ProgramIDs[i] == 0
	}
	if (out.State == 0 && !absentSeal) || (out.State == 1 && !completeSeal) ||
		(out.State == 2 && !absentSeal && !completeSeal) ||
		(out.State != 0 && (out.BrokerCookie == 0 || out.ObserverCookie == 0)) {
		return guardStatus{}, errGuardProtocol
	}
	for _, attached := range out.LinkAttached {
		if (absentSeal && attached != 0) || (out.State == 1 && attached != 1) {
			return guardStatus{}, errGuardProtocol
		}
	}
	return out, nil
}

type guardTask struct {
	Cookie          uint64
	TGID, TID       uint32
	StartBoottimeNS uint64
}

func (g guardDevice) pinTask(tid uint32) (guardTask, error) {
	var out guardTask
	if tid == 0 {
		return out, errGuardProtocol
	}
	b := guardMessage(40)
	guardPut32(b, 8, tid)
	if err := g.call(3, b); err != nil {
		return out, err
	}
	out = guardTask{guardU64(b, 16), guardU32(b, 24), guardU32(b, 28), guardU64(b, 32)}
	if guardU32(b, 8) != tid || guardU32(b, 12) != 0 || out.Cookie == 0 || out.TGID == 0 || out.TID != tid || out.StartBoottimeNS == 0 {
		return guardTask{}, errGuardProtocol
	}
	return out, nil
}

func (g guardDevice) importFD(cookie uint64, fd uint32) (int, error) {
	if cookie == 0 {
		return -1, errGuardProtocol
	}
	b := guardMessage(32)
	guardPut64(b, 8, cookie)
	guardPut32(b, 16, fd)
	if err := g.call(4, b); err != nil {
		return -1, err
	}
	installed := int32(guardU32(b, 24))
	if guardU64(b, 8) != cookie || guardU32(b, 16) != fd || guardU32(b, 20) != 0 || guardU32(b, 28) != 0 || installed < 0 {
		if installed >= 0 {
			_ = unix.Close(int(installed))
		}
		return -1, errGuardProtocol
	}
	return int(installed), nil
}

func (g guardDevice) dropTask(cookie uint64) error {
	if cookie == 0 {
		return errGuardProtocol
	}
	b := guardMessage(16)
	guardPut64(b, 8, cookie)
	if err := g.call(5, b); err != nil {
		return err
	}
	if guardU64(b, 8) != cookie {
		return errGuardProtocol
	}
	return nil
}

type guardEnrollment struct{ RootCookie, KernfsID, Enrollment uint64 }

func (g guardDevice) enrollCgroup(fd int, grant uint64) (guardEnrollment, error) {
	var out guardEnrollment
	if fd < 0 || int64(fd) > math.MaxInt32 || grant == 0 {
		return out, errGuardProtocol
	}
	b := guardMessage(48)
	guardPut32(b, 8, uint32(fd))
	guardPut64(b, 16, grant)
	if err := g.call(6, b); err != nil {
		return out, err
	}
	out = guardEnrollment{guardU64(b, 24), guardU64(b, 32), guardU64(b, 40)}
	if guardU32(b, 8) != uint32(fd) || guardU32(b, 12) != 0 || guardU64(b, 16) != grant || out.RootCookie == 0 || out.KernfsID == 0 || out.Enrollment == 0 {
		return guardEnrollment{}, errGuardProtocol
	}
	return out, nil
}

type guardRetirement struct {
	State    uint32
	Sequence uint64
}

func (g guardDevice) retireCgroup(enrollment uint64) (guardRetirement, error) {
	var out guardRetirement
	if enrollment == 0 {
		return out, errGuardProtocol
	}
	b := guardMessage(32)
	guardPut64(b, 8, enrollment)
	if err := g.call(7, b); err != nil {
		return out, err
	}
	out = guardRetirement{guardU32(b, 16), guardU64(b, 24)}
	if guardU64(b, 8) != enrollment || guardU32(b, 20) != 0 || out.State > 2 || out.Sequence == 0 {
		return guardRetirement{}, errGuardProtocol
	}
	return out, nil
}

type guardRecord struct {
	Sequence                                                                           uint64
	Kind, Flags                                                                        uint32
	ObjectCookie, KernelID, RootCookie, CgroupID, TaskCookie, TaskStartNS, AuditSerial uint64
	Operation                                                                          uint32
	Result                                                                             int32
	ParentCookie                                                                       uint64
	Detail                                                                             [40]byte
	Snapshot                                                                           [64]byte
	// Raw is the original record returned by the guard, before decoding.
	Raw [guardRecordSize]byte
}

func (r guardRecord) auditKey() (auditEventKey, error) {
	if r.Kind != 8 || r.AuditSerial == 0 || r.AuditSerial > math.MaxUint32 ||
		guardU64(r.Detail[:], 0) > math.MaxInt64 || guardU32(r.Detail[:], 8) >= 1000000000 ||
		!guardZero(r.Detail[12:]) {
		return auditEventKey{}, errGuardProtocol
	}
	return auditEventKey{guardU64(r.Detail[:], 0), guardU32(r.Detail[:], 8) / 1000000, r.AuditSerial}, nil
}

type guardEventBatch struct {
	First, Last uint64
	Sticky      uint32
	Records     []guardRecord
}

func (g guardDevice) readEvents(max uint32, after, ack uint64) (guardEventBatch, error) {
	var out guardEventBatch
	if max == 0 || max > guardMaxRecords || ack > after {
		return out, errGuardProtocol
	}
	b := guardMessage(guardEventsSize)
	guardPut32(b, 8, max)
	guardPut64(b, 16, after)
	guardPut64(b, 24, ack)
	if err := g.call(8, b); err != nil {
		if g.diagnostic != nil {
			g.diagnostic.batchError = 1
			if errors.Is(err, errGuardProtocol) {
				g.diagnostic.batchError = 2
			}
		}
		return out, err
	}
	return g.decodeEvents(b, max, after, ack)
}

// decodeEvents consumes the exact buffer returned by READ_EVENTS. Keeping the
// decoder separate permits exercising partial decode without a guard device.
func (g guardDevice) decodeEvents(b []byte, max uint32, after, ack uint64) (guardEventBatch, error) {
	var out guardEventBatch
	if len(b) != guardEventsSize || max == 0 || max > guardMaxRecords || ack > after {
		return out, errGuardProtocol
	}
	out.First, out.Last, out.Sticky = guardU64(b, 32), guardU64(b, 40), guardU32(b, 52)
	count := guardU32(b, 48)
	if guardU32(b, 8) != max || guardU32(b, 12) != 0 || guardU64(b, 16) != after || guardU64(b, 24) != ack || count > max || count > guardMaxRecords || !guardZero(b[56+int(count)*guardRecordSize:]) {
		if g.diagnostic != nil {
			g.diagnostic.batchError = 2
		}
		return guardEventBatch{}, errGuardProtocol
	}
	if count == 0 {
		if out.First != 0 || out.Last != 0 {
			if g.diagnostic != nil {
				g.diagnostic.batchError = 2
			}
			return guardEventBatch{}, errGuardProtocol
		}
		return out, nil
	}
	if after == math.MaxUint64 || out.First != after+1 || out.Last < out.First || out.Last-out.First != uint64(count-1) {
		if g.diagnostic != nil {
			g.diagnostic.batchError = 2
		}
		return guardEventBatch{}, errGuardProtocol
	}
	out.Records = make([]guardRecord, 0, count)
	for i := range count {
		start := 56 + int(i)*guardRecordSize
		v, err := decodeGuardRecord(b[start:start+guardRecordSize], out.First+uint64(i))
		if err != nil {
			if g.diagnostic != nil {
				g.diagnostic.batchError = 2
			}
			return guardEventBatch{}, err
		}
		if g.diagnostic != nil {
			g.diagnostic.decoded(&v)
		}
		out.Records = append(out.Records, v)
	}
	return out, nil
}

// decodeGuardRecord validates an exact wire record from the guard.
func decodeGuardRecord(r []byte, expectedSequence uint64) (guardRecord, error) {
	if len(r) != guardRecordSize {
		return guardRecord{}, errGuardProtocol
	}
	v := guardRecord{Sequence: guardU64(r, 0), Kind: guardU32(r, 8), Flags: guardU32(r, 12), ObjectCookie: guardU64(r, 16), KernelID: guardU64(r, 24), RootCookie: guardU64(r, 32), CgroupID: guardU64(r, 40), TaskCookie: guardU64(r, 48), TaskStartNS: guardU64(r, 56), AuditSerial: guardU64(r, 64), Operation: guardU32(r, 72), Result: int32(guardU32(r, 76)), ParentCookie: guardU64(r, 80)}
	copy(v.Raw[:], r)
	copy(v.Detail[:], r[88:128])
	copy(v.Snapshot[:], r[128:192])
	if _, err := v.objectSnapshot(); err != nil {
		return guardRecord{}, err
	}
	if v.Sequence != expectedSequence || v.Kind < 1 || v.Kind > 16 || v.Flags & ^uint32(7) != 0 || !validLifecycleDetail(v.Kind, v.Detail[:]) || (v.Kind != 8 && v.Flags&1 != 0) || (v.Kind == 7 && v.AuditSerial != 0) {
		return guardRecord{}, errGuardProtocol
	}
	if v.Kind == 8 {
		if _, err := v.auditKey(); err != nil {
			return guardRecord{}, err
		}
	}
	if v.Kind >= 9 && v.Kind <= 12 && (v.Flags != 0 || v.ObjectCookie != 0 || v.KernelID != 0 || v.RootCookie != 0 || v.CgroupID != 0 || v.AuditSerial != 0 || v.Operation != 0 || v.Result != 0 || guardU32(v.Detail[:], 36) != 0 || (v.Kind != 11 && guardU32(v.Detail[:], 32) != 0)) {
		return guardRecord{}, errGuardProtocol
	}
	if v.Kind == 13 {
		if v.Flags != 0 || v.ObjectCookie != 0 || v.KernelID != 0 || v.RootCookie != 0 || v.CgroupID != 0 || v.ParentCookie != 0 || v.TaskCookie == 0 || v.TaskStartNS == 0 || v.AuditSerial == 0 || v.AuditSerial > math.MaxUint32 || guardU64(v.Detail[:], 8) > math.MaxInt64 || guardU32(v.Detail[:], 16) >= 1000000000 || guardU32(v.Detail[:], 20) != 0 || guardU32(v.Detail[:], 0) == 0 || guardU32(v.Detail[:], 4) == 0 || guardU64(v.Detail[:], 32) == 0 {
			return guardRecord{}, errGuardProtocol
		}
	}
	if v.Kind == 14 {
		if v.Flags != 0 || v.ObjectCookie != 0 || v.KernelID != 0 || v.RootCookie != 0 || v.CgroupID != 0 || v.AuditSerial != 0 || v.Result != 0 || v.ParentCookie != 0 || v.TaskCookie == 0 || v.TaskStartNS == 0 || (v.Operation != 2 && v.Operation != 0) || !guardZero(v.Detail[:]) {
			return guardRecord{}, errGuardProtocol
		}
	}
	if v.Kind == 15 {
		if v.Flags != 0 || v.ObjectCookie != 0 || v.KernelID != 0 || v.RootCookie != 0 || v.CgroupID != 0 || v.AuditSerial != 0 || v.Operation != 0 || v.ParentCookie != 0 || v.TaskCookie == 0 || v.TaskStartNS == 0 || guardU32(v.Detail[:], 36) != 0 || int32(guardU32(v.Detail[:], 32)) != v.Result || guardU32(v.Detail[:], 0) == 0 || guardU32(v.Detail[:], 4) == 0 {
			return guardRecord{}, errGuardProtocol
		}
	}
	if v.Kind == 16 && v.validateIDCursor() != nil {
		return guardRecord{}, errGuardProtocol
	}
	return v, nil
}

func validLifecycleDetail(kind uint32, detail []byte) bool {
	if kind == 2 || kind == 4 || kind == 5 || kind == 6 {
		return guardU64(detail, 0) != 0 && guardZero(detail[8:])
	}
	return kind >= 8 || guardZero(detail)
}

func (g guardDevice) watchDaemon(cookie uint64) (uint64, error) {
	if cookie == 0 {
		return 0, errGuardProtocol
	}
	b := guardMessage(24)
	guardPut64(b, 8, cookie)
	if err := g.call(9, b); err != nil {
		return 0, err
	}
	seq := guardU64(b, 16)
	if guardU64(b, 8) != cookie || seq == 0 {
		return 0, errGuardProtocol
	}
	return seq, nil
}
func (g guardDevice) bindObserver(cookie uint64) (int, error) {
	if cookie == 0 {
		return -1, errGuardProtocol
	}
	b := guardMessage(24)
	guardPut64(b, 8, cookie)
	if err := g.call(10, b); err != nil {
		return -1, err
	}
	installed := int32(guardU32(b, 16))
	if guardU64(b, 8) != cookie || guardU32(b, 20) != 0 || installed < 0 {
		if installed >= 0 {
			_ = unix.Close(int(installed))
		}
		return -1, errGuardProtocol
	}
	return int(installed), nil
}

func (g guardDevice) revoke() error {
	b := guardMessage(8)
	return g.call(11, b)
}

func (g guardDevice) watchProbe(cookie uint64, probeKind uint32) (uint64, error) {
	if cookie == 0 || (probeKind != 1 && probeKind != 2) {
		return 0, errGuardProtocol
	}
	b := guardMessage(32)
	guardPut64(b, 8, cookie)
	guardPut32(b, 16, probeKind)
	if err := g.call(12, b); err != nil {
		return 0, err
	}
	seq := guardU64(b, 24)
	if guardU64(b, 8) != cookie || guardU32(b, 16) != probeKind || guardU32(b, 20) != 0 || seq == 0 {
		return 0, errGuardProtocol
	}
	return seq, nil
}

func (g guardDevice) selftestObservationFault(variant uint32) error {
	if variant < 1 || variant > 3 {
		return errGuardProtocol
	}
	b := guardMessage(16)
	guardPut32(b, 8, variant)
	if err := g.call(13, b); err != nil {
		return err
	}
	if guardU32(b, 8) != variant || guardU32(b, 12) != 0 {
		return errGuardProtocol
	}
	return nil
}

func guardIDReuseMessage(firstObjectCookie, firstRemovalSeq, nextTransactionID uint64, nonce [16]byte) ([]byte, error) {
	if firstObjectCookie == 0 || firstRemovalSeq == 0 || nextTransactionID == 0 || guardZero(nonce[:]) {
		return nil, errGuardProtocol
	}
	b := guardMessage(48)
	guardPut64(b, 8, firstObjectCookie)
	guardPut64(b, 16, firstRemovalSeq)
	guardPut64(b, 24, nextTransactionID)
	copy(b[32:], nonce[:])
	return b, nil
}

func (g guardDevice) selftestIDReuse(firstObjectCookie, firstRemovalSeq, nextTransactionID uint64, nonce [16]byte) error {
	b, err := guardIDReuseMessage(firstObjectCookie, firstRemovalSeq, nextTransactionID, nonce)
	if err != nil {
		return err
	}
	if err := g.call(14, b); err != nil {
		return err
	}
	if guardU64(b, 8) != firstObjectCookie || guardU64(b, 16) != firstRemovalSeq || guardU64(b, 24) != nextTransactionID || !equalBytes(b[32:], nonce[:]) {
		return errGuardProtocol
	}
	return nil
}

func (r guardRecord) validateIDCursor() error {
	if r.Kind != 16 || r.Flags != 0 || r.ObjectCookie == 0 || r.KernelID == 0 || r.KernelID >= math.MaxInt32 ||
		r.RootCookie != 0 || r.CgroupID != 0 || r.TaskCookie == 0 || r.TaskStartNS == 0 || r.AuditSerial != 0 ||
		r.ParentCookie != 0 || !guardZero(r.Snapshot[:]) || guardU64(r.Detail[:], 0) == 0 ||
		guardU64(r.Detail[:], 8) == 0 || uint64(guardU32(r.Detail[:], 20)) != r.KernelID || guardU64(r.Detail[:], 32) != 0 {
		return errGuardProtocol
	}
	saved, result, after := guardU32(r.Detail[:], 16), int32(guardU32(r.Detail[:], 24)), guardU32(r.Detail[:], 28)
	switch r.Operation {
	case 1:
		if saved != 0 || result != 0 || after != 0 || r.Result != 0 {
			return errGuardProtocol
		}
	case 2:
		if result != 0 || uint64(after) != r.KernelID || r.Result != 0 {
			return errGuardProtocol
		}
	case 3:
		if after != saved || r.Result != result {
			return errGuardProtocol
		}
	default:
		return errGuardProtocol
	}
	return nil
}

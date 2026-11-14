//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func confineTerminalObserver() error {
	lastRaw, err := os.ReadFile("/proc/sys/kernel/cap_last_cap")
	if err != nil || len(lastRaw) > 32 {
		return errGuardProtocol
	}
	last, err := strconv.ParseUint(strings.TrimSpace(string(lastRaw)), 10, 32)
	if err != nil || last < unix.CAP_AUDIT_READ || last < unix.CAP_AUDIT_CONTROL || last > 63 {
		return errGuardProtocol
	}
	all := func(trap, a, b, c uintptr) error {
		_, _, e := syscall.AllThreadsSyscall(trap, a, b, c)
		if e != 0 {
			return e
		}
		return nil
	}
	for cap := uint64(0); cap <= last; cap++ {
		if cap != unix.CAP_AUDIT_CONTROL && cap != unix.CAP_AUDIT_READ {
			if err = all(unix.SYS_PRCTL, unix.PR_CAPBSET_DROP, uintptr(cap), 0); err != nil {
				return err
			}
		}
	}
	if err = all(unix.SYS_PRCTL, unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0); err != nil {
		return err
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	caps := [2]unix.CapUserData{}
	mask := (uint64(1) << unix.CAP_AUDIT_CONTROL) | (uint64(1) << unix.CAP_AUDIT_READ)
	caps[0].Effective = uint32(mask)
	caps[0].Permitted = uint32(mask)
	caps[1].Effective = uint32(mask >> 32)
	caps[1].Permitted = uint32(mask >> 32)
	if err = all(unix.SYS_CAPSET, uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&caps[0])), 0); err != nil {
		return err
	}
	if err = all(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0); err != nil {
		return err
	}
	if err = terminalObserverThreads(false); err != nil {
		return err
	}
	if err = auditReaderFilter(); err != nil {
		return err
	}
	return terminalObserverThreads(true)
}

func terminalObserverThreads(filtered bool) error {
	return verifyTerminalObserverConfinementPID(os.Getpid(), filtered)
}

func verifyTerminalObserverConfinement(pid int) error {
	return verifyTerminalObserverConfinementPID(pid, true)
}

func verifyTerminalObserverConfinementPID(pid int, filtered bool) error {
	if pid <= 1 {
		return errGuardProtocol
	}
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil || len(entries) == 0 || len(entries) > 4096 {
		return errGuardProtocol
	}
	expectedMask := (uint64(1) << unix.CAP_AUDIT_CONTROL) | (uint64(1) << unix.CAP_AUDIT_READ)
	for _, entry := range entries {
		if _, err = strconv.ParseUint(entry.Name(), 10, 32); err != nil {
			return errGuardProtocol
		}
		f, e := os.Open(fmt.Sprintf("/proc/%d/task/%s/status", pid, entry.Name()))
		if e != nil {
			return e
		}
		raw, e := io.ReadAll(io.LimitReader(f, 65537))
		_ = f.Close()
		if e != nil || len(raw) > 65536 {
			return errGuardProtocol
		}
		values := map[string]string{}
		for _, line := range strings.Split(string(raw), "\n") {
			key, val, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch key {
			case "Uid", "Gid", "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb", "NoNewPrivs", "Seccomp":
				if _, exists := values[key]; exists {
					return errGuardProtocol
				}
				values[key] = strings.TrimSpace(val)
			}
		}
		if len(values) != 9 || values["NoNewPrivs"] != "1" || strings.Join(strings.Fields(values["Uid"]), " ") != "0 0 0 0" || strings.Join(strings.Fields(values["Gid"]), " ") != "0 0 0 0" {
			return errGuardProtocol
		}
		for _, key := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
			value, e := strconv.ParseUint(values[key], 16, 64)
			if e != nil {
				return errGuardProtocol
			}
			expected := uint64(0)
			if key == "CapPrm" || key == "CapEff" || key == "CapBnd" {
				expected = expectedMask
			}
			if value != expected {
				return errGuardProtocol
			}
		}
		if filtered && values["Seccomp"] != "2" {
			return errGuardProtocol
		}
	}
	return nil
}

func openObserverMulticast() (*auditMulticast, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_AUDIT)
	if err != nil {
		g2d1Fail("MULTICAST_SOCKET", err)
		return nil, err
	}
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, 8<<20)
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 8<<20)
	if err = unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: 1}); err != nil {
		g2d1Fail("MULTICAST_BIND", err)
		_ = unix.Close(fd)
		return nil, err
	}
	return &auditMulticast{fd: fd}, nil
}

func observerTranscriptHash(record map[string]any) (string, error) {
	raw, err := canonicalControl(record)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("HOUFENG-G2-BPF-OBS-V1\x00"), raw...))
	return hex.EncodeToString(sum[:]), nil
}

type observerRecordEntry struct {
	record map[string]any
	raw    []byte
	hash   string
}

const maxObserverAuditWireIdentities = 18432

type observerTranscript struct {
	bootID, epoch string
	entries       []observerRecordEntry
	tail          string
	total         int
	outputLoss    bool
}

func newObserverTranscript(bootID, epoch string) *observerTranscript {
	return &observerTranscript{
		bootID: bootID,
		epoch:  epoch,
		tail:   strings.Repeat("0", 64),
	}
}

func (t *observerTranscript) append(kind string, body map[string]any) (uint64, string, error) {
	if t == nil || kind == "" || body == nil || uint64(len(t.entries)) == math.MaxUint64 {
		if t != nil {
			t.outputLoss = true
		}
		return 0, "", errControlFrame
	}
	epochVal := any(nil)
	if t.epoch != "" {
		epochVal = t.epoch
	}
	seq := uint64(len(t.entries)) + 1
	rec := map[string]any{
		"version":  json.Number("1"),
		"boot_id":  t.bootID,
		"epoch":    epochVal,
		"sequence": jsonU64(seq),
		"previous": t.tail,
		"kind":     kind,
		"body":     body,
	}
	raw, err := canonicalControl(rec)
	if err != nil {
		t.outputLoss = true
		return 0, "", err
	}
	if len(raw)+1 > maxAuthorityTranscriptBytes-t.total {
		t.outputLoss = true
		return 0, "", errControlFrame
	}
	h, err := observerTranscriptHash(rec)
	if err != nil {
		t.outputLoss = true
		return 0, "", err
	}
	t.entries = append(t.entries, observerRecordEntry{record: rec, raw: raw, hash: h})
	t.total += len(raw) + 1
	t.tail = h
	return seq, h, nil
}

func (t *observerTranscript) recordsAfter(after uint64, max uint32) ([]any, uint64, bool, error) {
	if t == nil || max == 0 || max > 64 || after > uint64(len(t.entries)) {
		return nil, 0, false, errControlFrame
	}
	records := make([]any, 0, max)
	next := after
	for next < uint64(len(t.entries)) && len(records) < int(max) {
		candidate := append(records, t.entries[next].record)
		if _, err := canonicalControl(candidate); err != nil {
			if len(records) == 0 {
				return nil, 0, false, err
			}
			break
		}
		records = candidate
		next++
	}
	return records, next, next == uint64(len(t.entries)), nil
}

func projectGuardStatus(st guardStatus) map[string]any {
	states := [...]string{"unsealed", "sealed", "dead"}
	stateStr := "dead"
	if st.State < uint32(len(states)) {
		stateStr = states[st.State]
	}
	var epochVal any
	if !guardZero(st.Epoch[:]) {
		epochVal = hex.EncodeToString(st.Epoch[:])
	}
	var bootstrapVal any
	if !guardZero(st.BootstrapDigest[:]) {
		bootstrapVal = hex.EncodeToString(st.BootstrapDigest[:])
	}
	linkIDs := make([]any, 3)
	progIDs := make([]any, 3)
	attached := make([]any, 3)
	for i := range 3 {
		if st.LinkIDs[i] != 0 {
			linkIDs[i] = jsonU64(uint64(st.LinkIDs[i]))
		}
		if st.ProgramIDs[i] != 0 {
			progIDs[i] = jsonU64(uint64(st.ProgramIDs[i]))
		}
		attached[i] = st.LinkAttached[i] != 0
	}
	var deathSeq any
	if st.DeathSequence != 0 {
		deathSeq = jsonU64(st.DeathSequence)
	}
	return map[string]any{
		"state":                    stateStr,
		"sticky":                   jsonU64(uint64(st.Sticky)),
		"authority_phase":          jsonU64(uint64(st.AuthorityPhase)),
		"sequence":                 jsonU64(st.Sequence),
		"ack_cursor":               jsonU64(st.AckCursor),
		"observer_ack_cursor":      jsonU64(st.ObserverAckCursor),
		"lost":                     jsonU64(st.Lost),
		"probe_missed":             jsonU64(st.ProbeMissed),
		"broker_cookie":            jsonU64(st.BrokerCookie),
		"observer_cookie":          jsonU64(st.ObserverCookie),
		"death_sequence":           deathSeq,
		"module_instance":          jsonU64(st.ModuleInstance),
		"module_dev":               jsonU64(st.ModuleDev),
		"module_ino":               jsonU64(st.ModuleIno),
		"module_size":              jsonU64(st.ModuleSize),
		"epoch":                    epochVal,
		"link_ids":                 linkIDs,
		"program_ids":              progIDs,
		"link_attached":            attached,
		"bootstrap_digest":         bootstrapVal,
		"observer_bindings_sha256": hex.EncodeToString(st.ObserverBindingsSHA256[:]),
	}
}

type observerRawSyscallPending struct {
	record guardRecord
}

type observerAuditSerialPending struct {
	syscall           *auditFact
	syscallOccurrence uint64
	eoe               *auditFact
	eoeOccurrence     uint64
	invalid           bool
	joined            bool
}

type observerPeerState struct {
	role            string // "PID1", "REALM_ROOT", "EXPORTER"
	pid             uint32
	pidfd           int
	taskCookie      uint64
	startNS         uint64
	exeSHA256       string
	scriptSHA256    string
	caseSHA256      string
	nextRequest     uint64
	seenNonces      map[string]bool
	lastRequestID   uint64
	lastRequestRaw  []byte
	lastResponseRaw []byte
}

func (p *observerPeerState) close() {
	if p == nil {
		return
	}
	if p.pidfd >= 0 {
		_ = unix.Close(p.pidfd)
		p.pidfd = -1
	}
}

type observerWitnessPages struct {
	next, count uint64
	total       uint64
	sha         string
	data        []byte
}

type terminalObserver struct {
	bootID                  string
	epoch                   string
	moduleSHA256            string
	observerBindingsSHA256  string
	moduleMemfd             int
	moduleDev               uint64
	moduleIno               uint64
	moduleSize              uint64
	startupFD               int
	brokerFD                int
	brokerPID               uint32
	brokerPIDFD             int
	brokerExeFD             int
	witnessFD               int
	readerPID               uint32
	readerPIDFD             int
	readerWitnessArmID      uint64
	readerWitnessNonce      string
	readerWitnessHoldFD     int
	readerDeathHoldObserved bool
	armedCaseSHA256         string
	faultEvidence           map[string]any
	brokerObservation       map[string]any
	brokerSnapshotPages     map[string][]byte
	snapshotOriginals       map[uint64][guardRecordSize]byte
	snapshotSequences       map[uint64]uint64
	brokerCredentialSend    map[string]any
	brokerWitnessSequences  []any
	readerWitnessSequences  []any
	readerEvent             map[string]any
	readerRequestID         uint64
	readerRequestNonce      string
	readerPages             map[string]*observerWitnessPages
	readerPageBytes         uint64
	readerLiveAtEvent       bool
	readerLiveAtBroker      bool
	fdFaultPhase2           uint64
	fdFaultPhase0           uint64
	fdFaultExit             uint64
	faultGuardSequences     []any
	guard                   guardDevice
	guardCursor             uint64
	audit                   *auditMulticast
	queryFD                 int
	queryPort               uint32
	querySeq                uint32
	transcript              *observerTranscript
	rawSyscalls             map[auditEventKey]observerRawSyscallPending
	rawSeen                 map[auditEventKey]struct{}
	auditSerials            map[auditEventKey]*observerAuditSerialPending
	eoeOrder                []auditEventKey
	wireCounts              map[auditWireKey]uint64
	auditLost               bool
	nativeLost              bool
	armed                   bool
	armedVariant            string
	armedByRole             string
	beginSequence           uint64
	endSequence             uint64
	authorityBefore         map[string]any
	authorityAfter          map[string]any
	finished                bool
	terminalSHA256          string
	outcome                 string
	pid1Peer                *observerPeerState
	rootPeer                *observerPeerState
	exporterPeer            *observerPeerState
}

func (o *terminalObserver) drainGuard() error {
	if o.guard.fd < 0 || o.transcript == nil {
		return errGuardProtocol
	}
	for {
		batch, err := o.guard.readEvents(64, o.guardCursor, o.guardCursor)
		if err != nil {
			o.nativeLost = true
			return err
		}
		if batch.Sticky != 0 {
			o.nativeLost = true
		}
		if len(batch.Records) == 0 {
			return nil
		}
		for _, rec := range batch.Records {
			if o.epoch == "" {
				if st, e := o.guard.status(); e == nil && !guardZero(st.Epoch[:]) {
					o.epoch = hex.EncodeToString(st.Epoch[:])
					o.transcript.epoch = o.epoch
				}
			}
			proj, err := rec.projection()
			if err != nil {
				o.nativeLost = true
				return err
			}
			witnessSeq, _, err := o.transcript.append("guard", map[string]any{"record": proj})
			if err != nil {
				return err
			}
			if o.armed && (o.armedVariant == "snapshot-missing" || o.armedVariant == "snapshot-bad-field") && rec.Kind == 3 && guardU32(rec.Snapshot[:], 0) == unix.BPF_MAP_TYPE_ARRAY && string(bytes.TrimRight(rec.Snapshot[8:24], "\x00")) == "feature_test" {
				if o.snapshotOriginals == nil {
					o.snapshotOriginals = make(map[uint64][guardRecordSize]byte)
					o.snapshotSequences = make(map[uint64]uint64)
				}
				if len(o.snapshotOriginals) >= 384 {
					o.transcript.outputLoss = true
					return errGuardProtocol
				}
				o.snapshotOriginals[rec.Sequence], o.snapshotSequences[rec.Sequence] = rec.Raw, witnessSeq
			}
			if o.armed && (isObservationFaultVariant(o.armedVariant) || o.armedVariant == "fd-fill-failure" || o.armedVariant == "fd-restore-failure") {
				cookie, ok := exactNumber(o.authorityBefore["broker_cookie"])
				if ok && cookie != 0 && rec.TaskCookie == cookie {
					switch {
					case rec.Kind == 14 && rec.Operation == 2 && o.fdFaultPhase2 == 0:
						o.faultGuardSequences = append(o.faultGuardSequences, jsonU64(witnessSeq))
						o.fdFaultPhase2 = rec.Sequence
					case rec.Kind == 14 && rec.Operation == 0 && o.fdFaultPhase2 != 0 && rec.Sequence > o.fdFaultPhase2:
						o.faultGuardSequences = append(o.faultGuardSequences, jsonU64(witnessSeq))
						o.fdFaultPhase0 = rec.Sequence
					case rec.Kind == 15:
						o.faultGuardSequences = append(o.faultGuardSequences, jsonU64(witnessSeq))
						o.fdFaultExit = rec.Sequence
					}
				}
			}
			o.guardCursor = rec.Sequence
			if rec.Kind == 13 {
				o.observeRawGuard(rec)
			}
		}
	}
}

func (o *terminalObserver) drainAudit() error {
	if o.audit == nil || o.transcript == nil {
		return errGuardProtocol
	}
	for {
		facts, err := o.audit.receive()
		if auditReceiveAgain(err) {
			break
		}
		if err != nil {
			o.auditLost = true
			return nil
		}
		for _, f := range facts {
			if err := o.observeAuditFact(f); err != nil {
				return err
			}
		}
	}
	o.joinRawSyscalls()
	return nil
}

// Guard and multicast delivery are independent; both are retained until the
// complete, unique join is possible, including the actual EOE wire fact.
func (o *terminalObserver) observeRawGuard(r guardRecord) {
	key, err := rawGuardAuditKey(r)
	if err != nil || len(o.rawSeen) >= 4096 {
		o.nativeLost = true
		return
	}
	if _, exists := o.rawSeen[key]; exists {
		o.nativeLost = true
		return
	}
	o.rawSeen[key] = struct{}{}
	o.rawSyscalls[key] = observerRawSyscallPending{record: r}
}

func (o *terminalObserver) observeAuditFact(f auditFact) error {
	if f.Serial == 0 || f.Key.Serial != f.Serial || f.PayloadSHA256 == "" {
		o.auditLost = true
		return nil
	}
	wire := auditWireKey{f.Key, f.Kind, f.PayloadSHA256}
	if len(o.wireCounts) >= maxObserverAuditWireIdentities && o.wireCounts[wire] == 0 || o.wireCounts[wire] == ^uint64(0) {
		o.auditLost = true
		return nil
	}
	o.wireCounts[wire]++
	if _, _, err := o.transcript.append("audit_wire", map[string]any{
		"stamp": f.Key.stamp(), "serial": jsonU64(f.Serial), "record_type": jsonU64(uint64(f.Kind)),
		"payload_sha256": f.PayloadSHA256, "occurrence": jsonU64(o.wireCounts[wire]),
	}); err != nil {
		return err
	}
	if f.Kind != auditSyscallRecord && f.Kind != auditEOERecord {
		return nil
	}
	entry := o.auditSerials[f.Key]
	if entry == nil {
		if len(o.auditSerials) >= maxObserverAuditWireIdentities {
			o.auditLost = true
			return nil
		}
		entry = &observerAuditSerialPending{}
		o.auditSerials[f.Key] = entry
	}
	if entry.joined {
		o.auditLost = true
		return nil
	}
	if f.Kind == auditSyscallRecord {
		if entry.syscall != nil || entry.eoe != nil {
			entry.invalid = true
			return nil
		}
		entry.syscall = &f
		entry.syscallOccurrence = o.wireCounts[wire]
	} else {
		if entry.eoe != nil {
			entry.invalid = true
			return nil
		}
		if entry.syscall == nil {
			entry.invalid = true
		}
		entry.eoe = &f
		entry.eoeOccurrence = o.wireCounts[wire]
		o.eoeOrder = append(o.eoeOrder, f.Key)
	}
	return nil
}

func (o *terminalObserver) joinRawSyscalls() {
	remaining := make([]auditEventKey, 0, len(o.eoeOrder))
	for _, key := range o.eoeOrder {
		entry := o.auditSerials[key]
		raw, ok := o.rawSyscalls[key]
		if entry == nil || entry.eoe == nil || entry.syscall == nil || !ok {
			remaining = append(remaining, key)
			continue
		}
		s := entry.syscall
		r := raw.record
		evidence, err := rawAuditEvidence(*s, entry.syscallOccurrence, entry.eoe, entry.eoeOccurrence, r)
		if entry.invalid || err != nil {
			o.auditLost = true
			delete(o.rawSyscalls, key)
			entry.joined = true
			continue
		}
		_, _, err = o.transcript.append("audit", map[string]any{
			"stamp": s.Key.stamp(), "serial": jsonU64(s.Serial), "record_type": "SYSCALL", "context": "syscall",
			"object_cookie": nil, "transaction_id": nil, "task_cookie": jsonU64(r.TaskCookie),
			"command": nullableName(bpfCommand(uint64(r.Operation))), "result": jsonI64(s.Result), "operation": nil, "raw": evidence,
		})
		if err != nil {
			o.auditLost = true
			remaining = append(remaining, key)
			continue
		}
		delete(o.rawSyscalls, key)
		entry.joined = true
	}
	o.eoeOrder = remaining
}

func isObservationFaultVariant(variant string) bool {
	switch variant {
	case "snapshot-missing", "snapshot-bad-field", "audit-loss", "audit-duplicate", "audit-overflow", "reader-death", "reader-endpoint", "reader-timeout", "reader-replay", "reader-credential", "reader-extra-fd", "reader-frame", "reader-done":
		return true
	default:
		return false
	}
}

func (o *terminalObserver) finishOutcome() string {
	if len(o.rawSyscalls) != 0 {
		o.auditLost = true
	}
	if o.armedByRole == "PID1" {
		return "early_no_handoff"
	}
	if o.faultEvidence != nil && o.fdFaultPhase2 != 0 && o.fdFaultPhase0 > o.fdFaultPhase2 && o.fdFaultExit != 0 && !livePIDFD(o.brokerPIDFD) && o.authorityAfter["authority_phase"] == jsonU64(0) {
		return "closure_on_fault"
	}
	if o.nativeLost || o.auditLost || o.transcript.outputLoss {
		return "closure_on_fault"
	}
	return "denial_proved"
}

func (o *terminalObserver) performAuditReadback(check string, nonce string) (observedAuditStatus, error) {
	var empty observedAuditStatus
	deadline, err := monoNowNS()
	if err != nil {
		return empty, err
	}
	deadline += uint64((5 * time.Second).Nanoseconds())
	kinds := []uint16{auditGetType}
	if check == "configuration" {
		kinds = []uint16{auditGetType, auditListRulesType, auditGetType}
	}
	queries := make([]any, 0, len(kinds))
	for _, k := range kinds {
		if o.querySeq == 0 || o.querySeq == math.MaxUint32 {
			return empty, errGuardProtocol
		}
		seq := o.querySeq
		o.querySeq++
		q, e := readerQuery(o.queryFD, -1, o.queryPort, seq, k, deadline)
		if e != nil {
			o.auditLost = true
			return empty, e
		}
		queries = append(queries, q)
	}
	st, err := validateAuditQueriesPhase(queries, check, true)
	if err != nil {
		o.auditLost = true
		return empty, err
	}
	if st.lost != 0 {
		o.auditLost = true
	}
	stGuard, e := o.guard.status()
	var obsCookie uint64 = 1
	if e == nil && stGuard.ObserverCookie != 0 {
		obsCookie = stGuard.ObserverCookie
	}
	_, _, err = o.transcript.append("audit_readback", map[string]any{
		"reader_cookie": jsonU64(obsCookie),
		"request_id":    jsonU64(uint64(o.querySeq)),
		"nonce":         nonce,
		"check":         check,
		"queries":       queries,
	})
	return st, err
}

func observerDetectorMatches(variant, source, detector string) bool {
	switch variant {
	case "snapshot-missing":
		return source == "broker_guard_copy" && detector == "guard_snapshot_missing"
	case "snapshot-bad-field":
		return source == "broker_guard_copy" && detector == "guard_snapshot_mismatch"
	case "audit-loss":
		return source == "broker_audit_delivery" && detector == "audit_incomplete"
	case "audit-duplicate":
		return source == "broker_audit_delivery" && detector == "audit_duplicate"
	case "audit-overflow":
		return source == "broker_audit_delivery" && detector == "audit_wire_capacity"
	case "reader-death":
		return source == "broker_reader" && detector == "reader_pidfd_exited"
	case "reader-endpoint":
		return source == "broker_reader" && detector == "reader_endpoint_closed"
	case "reader-timeout":
		return source == "broker_reader" && detector == "reader_deadline"
	case "reader-replay":
		return source == "broker_reader" && detector == "reader_response_identity"
	case "reader-credential":
		return source == "broker_reader" && detector == "reader_message_credentials"
	case "reader-extra-fd":
		return source == "broker_reader" && detector == "reader_unexpected_rights"
	case "reader-frame":
		return source == "broker_reader" && detector == "reader_noncanonical_frame"
	case "reader-done":
		return source == "broker_reader" && detector == "reader_missing_done"
	case "fd-fill-failure":
		return source == "broker_fd_stage" && detector == "fd_fill_failure"
	case "fd-restore-failure":
		return source == "broker_fd_stage" && detector == "fd_restore_failure"
	}
	return false
}

// observeReaderDeath releases the held duplicate only after this observer's
// own pidfd becomes readable, and records that independent observation.
func (o *terminalObserver) observeReaderDeath() error {
	if o == nil || o.armedVariant != "reader-death" || o.readerWitnessHoldFD < 0 || o.readerDeathHoldObserved {
		return nil
	}
	poll := []unix.PollFd{{Fd: int32(o.readerPIDFD), Events: unix.POLLIN}}
	n, err := unix.Poll(poll, 0)
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if poll[0].Revents&unix.POLLIN == 0 {
		return errGuardProtocol
	}
	if err = unix.Close(o.readerWitnessHoldFD); err != nil {
		return err
	}
	o.readerWitnessHoldFD = -1
	seq, _, err := o.transcript.append("reader_witness", map[string]any{"kind": "reader_pidfd_exit", "variant": "reader-death", "reader_pid": jsonU64(uint64(o.readerPID)), "pidfd_exited": true, "query_dup_closed_after_exit": true})
	if err != nil {
		return err
	}
	o.readerWitnessSequences = append(o.readerWitnessSequences, jsonU64(seq))
	o.readerDeathHoldObserved = true
	return nil
}

func (o *terminalObserver) handleBrokerMessage() error {
	raw, cred, rights, err := receiveControl(o.brokerFD, -1)
	if err != nil {
		for _, fd := range rights {
			_ = unix.Close(fd)
		}
		return err
	}
	keepRights := false
	defer func() {
		if !keepRights {
			for _, fd := range rights {
				_ = unix.Close(fd)
			}
		}
	}()
	if cred == nil || uint32(cred.Pid) != o.brokerPID || cred.Uid != 0 || cred.Gid != 0 {
		return errGuardProtocol
	}
	frame, err := parseCanonicalControl(raw)
	if err != nil {
		return err
	}
	if frame["version"] != json.Number("1") || frame["boot_id"] != o.bootID {
		return errGuardProtocol
	}
	kind, _ := frame["kind"].(string)
	switch kind {
	case "bind_exporter":
		if len(rights) != 1 || !exactKeys(frame, "version", "boot_id", "kind", "pid", "task_cookie", "start_ns", "exe_sha256", "case_sha256", "epoch") {
			return errGuardProtocol
		}
		pidN, ok1 := exactNumber(frame["pid"])
		cookie, ok2 := exactNumber(frame["task_cookie"])
		startNS, ok3 := exactNumber(frame["start_ns"])
		exeSHA, ok4 := digestString(frame["exe_sha256"])
		caseSHA, ok5 := digestString(frame["case_sha256"])
		ep, ok6 := frame["epoch"].(string)
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !pidfdSelects(rights[0], uint32(pidN)) {
			return errGuardProtocol
		}
		if o.epoch == "" && len(ep) == 32 {
			o.epoch = ep
			o.transcript.epoch = ep
		}
		if o.exporterPeer != nil {
			o.exporterPeer.close()
		}
		o.exporterPeer = &observerPeerState{
			role:        "EXPORTER",
			pid:         uint32(pidN),
			pidfd:       rights[0],
			taskCookie:  cookie,
			startNS:     startNS,
			exeSHA256:   exeSHA,
			caseSHA256:  caseSHA,
			nextRequest: 1,
			seenNonces:  make(map[string]bool),
		}
		keepRights = true
		return nil
	case "bind_realm_root":
		if len(rights) != 1 || !exactKeys(frame, "version", "boot_id", "kind", "pid", "task_cookie", "start_ns", "exe_sha256", "script_sha256", "case_sha256", "epoch") {
			return errGuardProtocol
		}
		pidN, ok1 := exactNumber(frame["pid"])
		cookie, ok2 := exactNumber(frame["task_cookie"])
		startNS, ok3 := exactNumber(frame["start_ns"])
		exeSHA, ok4 := digestString(frame["exe_sha256"])
		scriptSHA, ok5 := digestString(frame["script_sha256"])
		caseSHA, ok6 := digestString(frame["case_sha256"])
		ep, ok7 := frame["epoch"].(string)
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !ok7 || !pidfdSelects(rights[0], uint32(pidN)) {
			return errGuardProtocol
		}
		if o.epoch == "" && len(ep) == 32 {
			o.epoch = ep
			o.transcript.epoch = ep
		}
		if o.rootPeer != nil {
			o.rootPeer.close()
		}
		o.rootPeer = &observerPeerState{
			role:         "REALM_ROOT",
			pid:          uint32(pidN),
			pidfd:        rights[0],
			taskCookie:   cookie,
			startNS:      startNS,
			exeSHA256:    exeSHA,
			scriptSHA256: scriptSHA,
			caseSHA256:   caseSHA,
			nextRequest:  1,
			seenNonces:   make(map[string]bool),
		}
		keepRights = true
		return nil
	case "broker_witness_chunk":
		if len(rights) != 0 || !pidfdSelects(o.brokerPIDFD, o.brokerPID) || !o.armed || (o.armedVariant != "snapshot-missing" && o.armedVariant != "snapshot-bad-field") || frame["variant"] != o.armedVariant || o.brokerObservation != nil || !exactKeys(frame, "version", "boot_id", "kind", "variant", "request_id", "nonce", "role", "page_index", "page_count", "total_bytes", "total_sha256", "data_b64") || frame["request_id"] != nil || frame["nonce"] != nil || frame["page_index"] != jsonU64(0) || frame["page_count"] != jsonU64(1) || frame["total_bytes"] != jsonU64(guardRecordSize) {
			return errGuardProtocol
		}
		role, roleOK := frame["role"].(string)
		text, textOK := frame["data_b64"].(string)
		sha, shaOK := digestString(frame["total_sha256"])
		data, e := base64.StdEncoding.Strict().DecodeString(text)
		if !roleOK || role != "original" && role != "modified" || !textOK || !shaOK || e != nil || len(data) != guardRecordSize || base64.StdEncoding.EncodeToString(data) != text || hashBytes(data) != sha {
			return errGuardProtocol
		}
		if o.brokerSnapshotPages == nil {
			o.brokerSnapshotPages = make(map[string][]byte)
		}
		if o.brokerSnapshotPages[role] != nil {
			return errGuardProtocol
		}
		seq, _, e := o.transcript.append("broker_witness", frame)
		if e != nil {
			return e
		}
		o.brokerSnapshotPages[role] = data
		o.brokerWitnessSequences = append(o.brokerWitnessSequences, jsonU64(seq))
		return nil
	case "broker_credential_send":
		if len(rights) != 0 || !pidfdSelects(o.brokerPIDFD, o.brokerPID) || o.armedVariant != "reader-credential" || o.readerEvent == nil || o.brokerCredentialSend != nil || !exactKeys(frame, "version", "boot_id", "kind", "variant", "request_id", "nonce", "input_sha256", "input_bytes", "send_result") || frame["variant"] != o.armedVariant || frame["request_id"] != o.readerEvent["request_id"] || frame["nonce"] != o.readerEvent["nonce"] || frame["input_sha256"] != o.readerEvent["sent_reply_sha256"] || frame["input_bytes"] != o.readerEvent["sent_reply_bytes"] || frame["send_result"] != jsonU64(0) {
			return errGuardProtocol
		}
		seq, _, e := o.transcript.append("broker_witness", frame)
		if e != nil {
			return e
		}
		o.brokerCredentialSend = frame
		o.brokerWitnessSequences = append(o.brokerWitnessSequences, jsonU64(seq))
		return nil
	case "broker_receive_observation":
		if len(rights) != 0 || !pidfdSelects(o.brokerPIDFD, o.brokerPID) || !o.armed || o.brokerObservation != nil || !exactKeys(frame, "version", "boot_id", "kind", "variant", "source", "detector", "request_id", "nonce", "input_sha256", "input_bytes", "original_sha256", "expected_actor", "observed_actor", "deadline_ns", "observed_ns", "capacity", "count_before", "rejected_wire", "guard_sequence", "receive_observation") {
			return errGuardProtocol
		}
		variant, ok := frame["variant"].(string)
		source, sourceOK := frame["source"].(string)
		detector, detectorOK := frame["detector"].(string)
		if !ok || !sourceOK || !detectorOK || variant != o.armedVariant || !observerDetectorMatches(variant, source, detector) {
			return errGuardProtocol
		}
		seq, _, e := o.transcript.append("broker_witness", frame)
		if e != nil {
			return e
		}
		o.brokerObservation = frame
		o.readerLiveAtBroker = livePIDFD(o.readerPIDFD)
		if variant == "reader-death" && !o.readerLiveAtBroker {
			if e := o.observeReaderDeath(); e != nil {
				return e
			}
		}
		o.brokerWitnessSequences = append(o.brokerWitnessSequences, jsonU64(seq))
		if source == "broker_fd_stage" {
			if _, e := o.sourceFaultEvidence(); e != nil {
				return e
			}
			deadline, e := guestDeadline(5 * time.Second)
			if e != nil {
				return e
			}
			return earlyProbeSend(o.brokerFD, map[string]any{"version": jsonU64(1), "boot_id": o.bootID, "kind": "broker_fd_witness_ack", "variant": variant, "detector": detector, "count_before": frame["count_before"]}, nil, deadline)
		}
		return nil
	case "broker_observation_fault":
		// An asserted label is not evidence of an observation or stream loss.
		return errGuardProtocol
	default:
		return errGuardProtocol
	}
}

func (o *terminalObserver) readerWitnessRequest(frame map[string]any) bool {
	id, ok := exactNumber(frame["request_id"])
	nonce, nonceOK := frame["nonce"].(string)
	if !ok || id == 0 || !nonceOK || !validCheckpointNonce(nonce) || frame["variant"] != o.armedVariant || o.readerWitnessArmID == 0 {
		return false
	}
	if o.readerRequestID == 0 {
		o.readerRequestID, o.readerRequestNonce = id, nonce
	}
	return o.readerRequestID == id && o.readerRequestNonce == nonce
}

func (o *terminalObserver) receiveReaderChunk(frame map[string]any) error {
	if !exactKeys(frame, "version", "boot_id", "kind", "variant", "request_id", "nonce", "role", "page_index", "page_count", "total_bytes", "total_sha256", "data_b64") || !o.readerWitnessRequest(frame) || o.readerEvent != nil {
		return errGuardProtocol
	}
	role, roleOK := frame["role"].(string)
	index, indexOK := exactNumber(frame["page_index"])
	count, countOK := exactNumber(frame["page_count"])
	total, totalOK := exactNumber(frame["total_bytes"])
	sha, shaOK := digestString(frame["total_sha256"])
	encoded, encodedOK := frame["data_b64"].(string)
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if !roleOK || role != "original_reply" && role != "sent_reply" || !indexOK || !countOK || !totalOK || !shaOK || !encodedOK || err != nil || len(data) == 0 || len(data) > 8192 || total == 0 || total > 32768 || count != (total+8191)/8192 || index >= count || o.readerPageBytes+uint64(len(data)) > 32768 {
		return errGuardProtocol
	}
	if o.readerPages == nil {
		o.readerPages = make(map[string]*observerWitnessPages)
	}
	page := o.readerPages[role]
	if page == nil {
		if index != 0 {
			return errGuardProtocol
		}
		page = &observerWitnessPages{count: count, total: total, sha: sha}
		o.readerPages[role] = page
	}
	if page.next != index || page.count != count || page.total != total || page.sha != sha || uint64(len(page.data)+len(data)) > total || index+1 < count && len(data) != 8192 {
		return errGuardProtocol
	}
	page.data = append(page.data, data...)
	page.next++
	o.readerPageBytes += uint64(len(data))
	if page.next == count && (uint64(len(page.data)) != total || hashBytes(page.data) != sha) {
		return errGuardProtocol
	}
	seq, _, err := o.transcript.append("reader_witness", frame)
	if err != nil {
		return err
	}
	o.readerWitnessSequences = append(o.readerWitnessSequences, jsonU64(seq))
	return nil
}

func (o *terminalObserver) receiveReaderEvent(frame map[string]any) error {
	if !exactKeys(frame, "version", "boot_id", "kind", "variant", "request_id", "nonce", "check", "request_sha256", "request_bytes", "original_reply_sha256", "original_reply_bytes", "sent_reply_sha256", "sent_reply_bytes", "send_result", "sent_rights", "endpoint_closed", "queries") || !o.readerWitnessRequest(frame) || o.readerEvent != nil {
		return errGuardProtocol
	}
	check, ok := frame["check"].(string)
	_, shaOK := digestString(frame["request_sha256"])
	requestBytes, bytesOK := exactNumber(frame["request_bytes"])
	rights, rightsOK := exactNumber(frame["sent_rights"])
	if !ok || check != "status" && check != "configuration" || !shaOK || !bytesOK || requestBytes == 0 || requestBytes > maxControlFrame || !rightsOK || rights > 1 {
		return errGuardProtocol
	}
	if frame["queries"] != nil {
		if _, err := validateAuditQueries(frame["queries"], check); err != nil {
			return err
		}
	}
	for _, item := range []struct{ role, sha, size string }{{"original_reply", "original_reply_sha256", "original_reply_bytes"}, {"sent_reply", "sent_reply_sha256", "sent_reply_bytes"}} {
		page := o.readerPages[item.role]
		if page != nil {
			n, valid := exactNumber(frame[item.size])
			if !valid || page.next != page.count || n != page.total || frame[item.sha] != page.sha {
				return errGuardProtocol
			}
		}
	}
	if o.armedVariant == "reader-frame" || o.armedVariant == "reader-done" || o.armedVariant == "reader-replay" || o.armedVariant == "reader-credential" {
		if o.readerPages["original_reply"] == nil || o.readerPages["sent_reply"] == nil {
			return errGuardProtocol
		}
	}
	seq, _, err := o.transcript.append("reader_witness", frame)
	if err != nil {
		return err
	}
	o.readerWitnessSequences = append(o.readerWitnessSequences, jsonU64(seq))
	o.readerEvent = frame
	o.readerLiveAtEvent = true
	if o.armedVariant == "reader-credential" {
		original, sent := o.readerPages["original_reply"], o.readerPages["sent_reply"]
		expected, e := canonicalControl(map[string]any{"version": json.Number("1"), "boot_id": o.bootID, "request_id": frame["request_id"], "nonce": frame["nonce"], "check": frame["check"], "queries": frame["queries"]})
		if e != nil || frame["queries"] == nil || original == nil || sent == nil || !bytes.Equal(original.data, expected) || !bytes.Equal(sent.data, expected) || frame["send_result"] != nil || frame["sent_rights"] != jsonU64(0) {
			return errGuardProtocol
		}
		deadline, e := guestDeadline(5 * time.Second)
		if e != nil {
			return e
		}
		count := (len(expected) + 8191) / 8192
		for i := range count {
			end := (i + 1) * 8192
			if end > len(expected) {
				end = len(expected)
			}
			page := map[string]any{"version": jsonU64(1), "boot_id": o.bootID, "kind": "observer_credential_chunk", "request_id": frame["request_id"], "nonce": frame["nonce"], "page_index": jsonU64(uint64(i)), "page_count": jsonU64(uint64(count)), "total_bytes": jsonU64(uint64(len(expected))), "total_sha256": hashBytes(expected), "data_b64": base64.StdEncoding.EncodeToString(expected[i*8192 : end])}
			if e = earlyProbeSend(o.brokerFD, page, nil, deadline); e != nil {
				return e
			}
		}
	}
	return nil
}

// expectedReaderExit uses only the originally held pidfds. A missing or
// invalid pidfd is never evidence of exit.
func (o *terminalObserver) expectedReaderExit() bool {
	exited, err := pidfdGroupExited(o.readerPIDFD)
	if err != nil || !exited || o.readerPID == 0 {
		return false
	}
	if o.armed && o.armedVariant == "reader-death" {
		if o.readerWitnessArmID == 0 || o.observeReaderDeath() != nil {
			return false
		}
		return o.readerDeathHoldObserved && o.readerWitnessHoldFD < 0
	}
	brokerExited, err := pidfdGroupExited(o.brokerPIDFD)
	return err == nil && brokerExited && o.brokerPID != 0
}

// serviceReaderWitness consumes one packet per poll iteration, including on
// HUP, so queued packets are validated before a subsequent actual EOF.
func (o *terminalObserver) serviceReaderWitness(revents int16) bool {
	if revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
		o.transcript.outputLoss = true
		return true
	}
	if revents&(unix.POLLIN|unix.POLLHUP) == 0 {
		return false
	}
	eof, err := o.handleReaderWitness()
	if err != nil || eof && !o.expectedReaderExit() {
		o.transcript.outputLoss = true
	}
	return err != nil || eof
}

// A queued credential can still be matched to the held, exited reader while
// its pidfd identifies the unreaped task. After reap, numeric PID reuse makes
// such a packet ambiguous, so it must remain output loss.
func (o *terminalObserver) readerWitnessSenderMatches(cred *unix.Ucred) bool {
	if cred == nil || cred.Pid <= 1 || uint32(cred.Pid) != o.readerPID || cred.Uid != 0 || cred.Gid != 0 {
		return false
	}
	if pidfdSelects(o.readerPIDFD, o.readerPID) {
		return true
	}
	info, err := readHeldProc(fmt.Sprintf("self/fdinfo/%d", o.readerPIDFD), 65536)
	if err != nil || len(info) > 65536 {
		return false
	}
	pid := ""
	for _, line := range strings.Split(string(info), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && key == "Pid" {
			if pid != "" {
				return false
			}
			pid = strings.TrimSpace(value)
		}
	}
	return pid == strconv.FormatUint(uint64(o.readerPID), 10) && o.expectedReaderExit()
}

// handleReaderWitness accepts only records sent by the held reader task.
func (o *terminalObserver) handleReaderWitness() (bool, error) {
	raw, cred, rights, observed, err := receiveControlObserved(o.witnessFD, -1)
	if err != nil {
		// A queued empty packet carries sender credentials under SO_PASSCRED;
		// EOF has no credentials or rights. Truncation and recvmsg errors
		// are never evidence of EOF.
		eof := errors.Is(err, errControlFrame) && observed.Stage == "validation" && observed.Bytes == 0 && observed.Errno == 0 && !observed.Truncated && !observed.ControlTruncated && observed.RightsCount == 0 && observed.PID == nil && observed.UID == nil && observed.GID == nil
		if eof {
			return true, nil
		}
		return false, err
	}
	return false, o.acceptReaderWitness(raw, cred, rights)
}

func (o *terminalObserver) acceptReaderWitness(raw []byte, cred *unix.Ucred, rights []int) error {
	keep := false
	defer func() {
		if !keep {
			for _, fd := range rights {
				_ = unix.Close(fd)
			}
		}
	}()
	if !o.readerWitnessSenderMatches(cred) {
		return errGuardProtocol
	}
	frame, err := parseCanonicalControl(raw)
	if err != nil || frame["version"] != json.Number("1") || frame["boot_id"] != o.bootID {
		return errGuardProtocol
	}
	if frame["kind"] == "reader_witness_chunk" && len(rights) == 0 {
		return o.receiveReaderChunk(frame)
	}
	if frame["kind"] == "reader_witness_event" && len(rights) == 0 {
		return o.receiveReaderEvent(frame)
	}
	if frame["kind"] != "reader_witness_arm" || !exactKeys(frame, "version", "boot_id", "kind", "request_id", "nonce", "variant", "case_sha256") {
		return errGuardProtocol
	}
	id, ok := exactNumber(frame["request_id"])
	nonce, nonceOK := frame["nonce"].(string)
	variant, variantOK := frame["variant"].(string)
	caseSHA, shaOK := digestString(frame["case_sha256"])
	if !ok || id == 0 || !nonceOK || !validCheckpointNonce(nonce) || !variantOK || !shaOK || !o.armed || o.armedVariant != variant || o.armedCaseSHA256 != caseSHA || o.readerWitnessArmID != 0 || (variant == "reader-death" && len(rights) != 1) || (variant != "reader-death" && len(rights) != 0) {
		return errGuardProtocol
	}
	seq, _, err := o.transcript.append("reader_witness", frame)
	if err != nil {
		return err
	}
	o.readerWitnessSequences = append(o.readerWitnessSequences, jsonU64(seq))
	if len(rights) == 1 {
		o.readerWitnessHoldFD = rights[0]
		keep = true
	}
	o.readerWitnessArmID, o.readerWitnessNonce = id, nonce
	reply, err := canonicalControl(map[string]any{"version": json.Number("1"), "boot_id": o.bootID, "kind": "reader_witness_arm_ack", "request_id": jsonU64(id), "nonce": nonce, "variant": variant, "case_sha256": caseSHA, "status": "OK"})
	if err != nil {
		return err
	}
	return sendControl(o.witnessFD, reply, nil)
}

// readerFaultEvidence joins independently received sender and receiver facts.
// Missing or inconsistent inputs never produce a terminal fault proof.
func (o *terminalObserver) readerFaultEvidence() (map[string]any, error) {
	b := o.brokerObservation
	if b == nil || b["source"] != "broker_reader" || len(o.readerWitnessSequences) == 0 || len(o.brokerWitnessSequences) == 0 || b["expected_actor"] != jsonU64(uint64(o.readerPID)) {
		return nil, errGuardProtocol
	}
	detector, ok := b["detector"].(string)
	if !ok || b["variant"] != o.armedVariant || !observerDetectorMatches(o.armedVariant, "broker_reader", detector) {
		return nil, errGuardProtocol
	}
	variant := o.armedVariant
	var inputSHA, inputBytes, requestID, nonce any
	if variant == "reader-death" {
		if o.readerLiveAtBroker || !o.readerDeathHoldObserved || o.readerWitnessArmID == 0 {
			return nil, errGuardProtocol
		}
	} else {
		e := o.readerEvent
		if e == nil || !o.readerLiveAtEvent || !o.readerLiveAtBroker || e["request_id"] != b["request_id"] || e["nonce"] != b["nonce"] {
			return nil, errGuardProtocol
		}
		requestID, nonce = e["request_id"], e["nonce"]
		switch variant {
		case "reader-endpoint":
			if e["endpoint_closed"] != true || e["send_result"] != nil || e["queries"] != nil {
				return nil, errGuardProtocol
			}
		case "reader-timeout":
			deadline, ok1 := exactNumber(b["deadline_ns"])
			observed, ok2 := exactNumber(b["observed_ns"])
			now, err := monoNowNS()
			if !ok1 || !ok2 || err != nil || deadline == 0 || observed < deadline || now < deadline || e["check"] != "status" || e["send_result"] != nil || e["queries"] != nil {
				return nil, errGuardProtocol
			}
		case "reader-credential":
			recv, ok := b["receive_observation"].(map[string]any)
			original, sent := o.readerPages["original_reply"], o.readerPages["sent_reply"]
			if !ok || o.brokerCredentialSend == nil || original == nil || sent == nil || e["send_result"] != nil || e["sent_rights"] != jsonU64(0) || e["sent_reply_sha256"] != b["input_sha256"] || e["sent_reply_bytes"] != b["input_bytes"] || b["observed_actor"] != jsonU64(uint64(o.brokerPID)) || recv["pid"] != jsonU64(uint64(o.brokerPID)) || recv["uid"] != jsonU64(0) || recv["gid"] != jsonU64(0) || recv["rights_count"] != jsonU64(0) || recv["packet_sha256"] != b["input_sha256"] || recv["bytes"] != b["input_bytes"] || recv["msg_trunc"] != false || recv["msg_ctrunc"] != false || !bytes.Equal(original.data, sent.data) {
				return nil, errGuardProtocol
			}
			inputSHA, inputBytes = b["input_sha256"], b["input_bytes"]
		case "reader-extra-fd", "reader-frame", "reader-done", "reader-replay":
			if e["send_result"] != jsonU64(0) || e["sent_reply_sha256"] != b["input_sha256"] || e["sent_reply_bytes"] != b["input_bytes"] || b["observed_actor"] != jsonU64(uint64(o.readerPID)) {
				return nil, errGuardProtocol
			}
			recv, ok := b["receive_observation"].(map[string]any)
			if !ok || recv["pid"] != jsonU64(uint64(o.readerPID)) || recv["uid"] != jsonU64(0) || recv["gid"] != jsonU64(0) || recv["packet_sha256"] != b["input_sha256"] || recv["bytes"] != b["input_bytes"] || recv["msg_trunc"] != false || recv["msg_ctrunc"] != false {
				return nil, errGuardProtocol
			}
			original, err := canonicalControl(map[string]any{"version": json.Number("1"), "boot_id": o.bootID, "request_id": e["request_id"], "nonce": e["nonce"], "check": e["check"], "queries": e["queries"]})
			if err != nil || e["queries"] == nil || e["original_reply_sha256"] != hashBytes(original) || e["original_reply_bytes"] != jsonU64(uint64(len(original))) {
				return nil, errGuardProtocol
			}
			if variant == "reader-extra-fd" {
				if e["sent_rights"] != jsonU64(1) || recv["rights_count"] != jsonU64(1) || recv["closed_count"] != jsonU64(1) || e["sent_reply_sha256"] != hashBytes(original) {
					return nil, errGuardProtocol
				}
			} else {
				originalPage, sentPage := o.readerPages["original_reply"], o.readerPages["sent_reply"]
				if e["sent_rights"] != jsonU64(0) || recv["rights_count"] != jsonU64(0) || originalPage == nil || sentPage == nil || !bytes.Equal(originalPage.data, original) {
					return nil, errGuardProtocol
				}
				if variant == "reader-frame" {
					if !bytes.Equal(sentPage.data, append(append([]byte(nil), original...), ' ')) {
						return nil, errGuardProtocol
					}
				} else if variant == "reader-replay" {
					old, err := parseCanonicalControl(sentPage.data)
					currentID, currentOK := exactNumber(e["request_id"])
					oldID, oldOK := exactNumber(old["request_id"])
					if err != nil || !currentOK || !oldOK || currentID < 2 || oldID != currentID-1 || old["boot_id"] != o.bootID || old["nonce"] == e["nonce"] || !exactKeys(old, "version", "boot_id", "request_id", "nonce", "check", "queries") {
						return nil, errGuardProtocol
					}
				} else {
					queries, ok := e["queries"].([]any)
					if !ok {
						return nil, errGuardProtocol
					}
					modified, err := readerWithoutDone(queries)
					if err != nil {
						return nil, err
					}
					if _, err = validateAuditQueries(modified, "configuration"); !errors.Is(err, errAuditMissingDone) {
						return nil, errGuardProtocol
					}
					modifiedRaw, err := canonicalControl(map[string]any{"version": json.Number("1"), "boot_id": o.bootID, "request_id": e["request_id"], "nonce": e["nonce"], "check": e["check"], "queries": modified})
					if err != nil || !bytes.Equal(sentPage.data, modifiedRaw) {
						return nil, errGuardProtocol
					}
				}
			}
			inputSHA, inputBytes = b["input_sha256"], b["input_bytes"]
		default:
			return nil, errGuardProtocol
		}
	}
	return map[string]any{"variant": variant, "source": b["source"], "detector": b["detector"], "request_id": requestID, "nonce": nonce, "input_sha256": inputSHA, "input_bytes": inputBytes, "expected_actor": b["expected_actor"], "observed_actor": b["observed_actor"], "deadline_ns": b["deadline_ns"], "observed_ns": b["observed_ns"], "capacity": nil, "count_before": nil, "rejected_wire": nil, "guard_sequences": append([]any{}, o.faultGuardSequences...), "reader_witness_sequences": append([]any{}, o.readerWitnessSequences...), "broker_witness_sequences": append([]any{}, o.brokerWitnessSequences...)}, nil
}

func (o *terminalObserver) sourceFaultEvidence() (map[string]any, error) {
	b := o.brokerObservation
	if b == nil || len(o.brokerWitnessSequences) == 0 || b["variant"] != o.armedVariant {
		return nil, errGuardProtocol
	}
	source, sourceOK := b["source"].(string)
	detector, detectorOK := b["detector"].(string)
	if !sourceOK || !detectorOK || !observerDetectorMatches(o.armedVariant, source, detector) {
		return nil, errGuardProtocol
	}
	guards := append([]any{}, o.faultGuardSequences...)
	switch b["source"] {
	case "broker_guard_copy":
		seq, ok := exactNumber(b["guard_sequence"])
		observed, found := o.snapshotOriginals[seq]
		original, modified := o.brokerSnapshotPages["original"], o.brokerSnapshotPages["modified"]
		if !ok || !found || len(original) != guardRecordSize || len(modified) != guardRecordSize || !bytes.Equal(observed[:], original) || b["original_sha256"] != hashBytes(original) || b["input_sha256"] != hashBytes(modified) || b["input_bytes"] != jsonU64(guardRecordSize) {
			return nil, errGuardProtocol
		}
		before, err := decodeGuardRecord(original, seq)
		if err != nil || before.Kind != 3 || guardU32(before.Snapshot[:], 0) != unix.BPF_MAP_TYPE_ARRAY || guardU32(before.Snapshot[:], 28) != 4 {
			return nil, errGuardProtocol
		}
		expected := append([]byte(nil), original...)
		if o.armedVariant == "snapshot-missing" {
			clear(expected[128:192])
			if _, err = decodeGuardRecord(modified, seq); err == nil {
				return nil, errGuardProtocol
			}
		} else if o.armedVariant == "snapshot-bad-field" {
			guardPut32(expected, 156, 8)
			if _, err = decodeGuardRecord(modified, seq); err != nil {
				return nil, err
			}
		} else {
			return nil, errGuardProtocol
		}
		if !bytes.Equal(expected, modified) || o.snapshotSequences[seq] == 0 {
			return nil, errGuardProtocol
		}
		guards = append([]any{jsonU64(o.snapshotSequences[seq])}, guards...)
	case "broker_audit_delivery":
		wire, ok := b["rejected_wire"].(map[string]any)
		if !ok || !exactKeys(wire, "stamp", "serial", "record_type", "payload_sha256", "occurrence") {
			return nil, errGuardProtocol
		}
		stamp, ok := wire["stamp"].(map[string]any)
		if !ok || !exactKeys(stamp, "seconds", "milliseconds") {
			return nil, errGuardProtocol
		}
		seconds, a := exactNumber(stamp["seconds"])
		milliseconds, c := exactNumber(stamp["milliseconds"])
		serial, d := exactNumber(wire["serial"])
		kind, e := exactNumber(wire["record_type"])
		sha, f := digestString(wire["payload_sha256"])
		if !a || !c || !d || !e || !f || milliseconds >= 1000 || serial == 0 || kind > math.MaxUint16 || b["input_sha256"] != sha {
			return nil, errGuardProtocol
		}
		key := auditEventKey{Seconds: seconds, Milliseconds: uint32(milliseconds), Serial: serial}
		if o.wireCounts[auditWireKey{key, uint16(kind), sha}] != 1 {
			return nil, errGuardProtocol
		}
		if o.armedVariant == "audit-overflow" {
			if b["capacity"] != jsonU64(maxAuditWireIdentities) || b["count_before"] != jsonU64(maxAuditWireIdentities) || wire["occurrence"] != jsonU64(1) {
				return nil, errGuardProtocol
			}
		} else {
			entry := o.auditSerials[key]
			if entry == nil || entry.syscall == nil || entry.eoe == nil || entry.invalid || entry.syscall.Syscall != unix.SYS_BPF || entry.syscall.Argument0 != unix.BPF_MAP_CREATE || entry.syscall.Success || b["expected_actor"] != jsonU64(entry.syscall.PID) {
				return nil, errGuardProtocol
			}
			if o.armedVariant == "audit-loss" && entry.eoe.PayloadSHA256 != sha || o.armedVariant == "audit-duplicate" && entry.syscall.PayloadSHA256 != sha {
				return nil, errGuardProtocol
			}
			if o.armedVariant == "audit-loss" && (kind != auditEOERecord || wire["occurrence"] != jsonU64(1)) || o.armedVariant == "audit-duplicate" && (kind != auditSyscallRecord || wire["occurrence"] != jsonU64(2)) {
				return nil, errGuardProtocol
			}
		}
	case "broker_fd_stage":
		count, ok := exactNumber(b["count_before"])
		soft, softOK := exactNumber(b["input_bytes"])
		observed, observedOK := exactNumber(b["observed_ns"])
		if !ok || !softOK || soft < 256 || !observedOK || observed == 0 || b["capacity"] != jsonU64(256) || b["input_sha256"] != nil || b["original_sha256"] != nil || b["expected_actor"] != nil || b["observed_actor"] != nil || b["deadline_ns"] != nil || b["request_id"] != nil || b["nonce"] != nil || b["rejected_wire"] != nil || b["guard_sequence"] != nil || b["receive_observation"] != nil {
			return nil, errGuardProtocol
		}
		if o.armedVariant == "fd-fill-failure" && count >= 256 || o.armedVariant == "fd-restore-failure" && count != 256 {
			return nil, errGuardProtocol
		}
	default:
		return nil, errGuardProtocol
	}
	return map[string]any{"variant": o.armedVariant, "source": b["source"], "detector": b["detector"], "request_id": nil, "nonce": nil, "input_sha256": b["input_sha256"], "input_bytes": b["input_bytes"], "expected_actor": b["expected_actor"], "observed_actor": b["observed_actor"], "deadline_ns": nil, "observed_ns": b["observed_ns"], "capacity": b["capacity"], "count_before": b["count_before"], "rejected_wire": b["rejected_wire"], "guard_sequences": guards, "reader_witness_sequences": []any{}, "broker_witness_sequences": append([]any{}, o.brokerWitnessSequences...)}, nil
}

func (o *terminalObserver) makeObserverReply(reqID uint64, nonce string, status string, body map[string]any) ([]byte, error) {
	epochVal := any(nil)
	if o.epoch != "" {
		epochVal = o.epoch
	}
	resp := map[string]any{
		"version":    json.Number("1"),
		"boot_id":    o.bootID,
		"epoch":      epochVal,
		"request_id": jsonU64(reqID),
		"nonce":      nonce,
		"status":     status,
		"body":       body,
	}
	return canonicalControl(resp)
}

func (o *terminalObserver) handleClientRequest(fd int, peer *observerPeerState) error {
	raw, cred, rights, err := receiveControl(fd, 0)
	for _, rfd := range rights {
		_ = unix.Close(rfd)
	}
	if err != nil {
		return err
	}
	if len(rights) != 0 || cred == nil || peer == nil || uint32(cred.Pid) != peer.pid || cred.Uid != 0 || cred.Gid != 0 || !pidfdSelects(peer.pidfd, peer.pid) {
		return errControlFrame
	}
	ucred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || ucred == nil || uint32(ucred.Pid) != peer.pid || ucred.Uid != 0 || ucred.Gid != 0 {
		return errControlFrame
	}
	req, err := parseCanonicalControl(raw)
	if err != nil {
		return err
	}
	if !exactKeys(req, "version", "boot_id", "epoch", "request_id", "nonce", "operation", "body") || req["version"] != json.Number("1") || req["boot_id"] != o.bootID {
		return errControlFrame
	}
	if o.epoch != "" {
		if req["epoch"] != o.epoch && !(peer.role == "PID1" && req["epoch"] == nil) {
			return errControlFrame
		}
	} else if req["epoch"] != nil {
		return errControlFrame
	}
	reqID, ok := exactNumber(req["request_id"])
	if !ok || reqID == 0 || reqID == math.MaxUint64 {
		return errControlFrame
	}
	nonce, ok := req["nonce"].(string)
	if !ok || !validCheckpointNonce(nonce) {
		return errControlFrame
	}
	if peer.lastRequestID != 0 && reqID == peer.lastRequestID {
		if string(raw) != string(peer.lastRequestRaw) {
			return errControlFrame
		}
		return sendControl(fd, peer.lastResponseRaw, nil)
	}
	if reqID != peer.nextRequest || peer.seenNonces[nonce] {
		return errControlFrame
	}
	op, ok := req["operation"].(string)
	body, bodyOK := req["body"].(map[string]any)
	if !ok || !bodyOK {
		return errControlFrame
	}
	_ = o.drainGuard()
	_ = o.drainAudit()

	var respBody map[string]any
	statusStr := "OK"
	switch op {
	case "ARM":
		if peer.role != "REALM_ROOT" && peer.role != "PID1" {
			respBody = map[string]any{"code": "BPF_AUTH_DENIED", "sequence": jsonU64(uint64(len(o.transcript.entries)))}
			statusStr = "BPF_AUTH_DENIED"
			break
		}
		if o.armed {
			respBody = map[string]any{"code": "BPF_AUTH_DENIED", "sequence": jsonU64(uint64(len(o.transcript.entries)))}
			statusStr = "BPF_AUTH_DENIED"
			break
		}
		if peer.role == "PID1" {
			if !exactKeys(body, "variant", "case_sha256") {
				return errControlFrame
			}
			csha, ok := digestString(body["case_sha256"])
			if !ok {
				return errControlFrame
			}
			peer.caseSHA256 = csha
		} else if !exactKeys(body, "variant") && !exactKeys(body, "variant", "case_sha256") {
			return errControlFrame
		}
		variant, ok := body["variant"].(string)
		if !ok || variant == "" {
			return errControlFrame
		}
		st, err := o.guard.status()
		if err != nil {
			respBody = map[string]any{"code": "BPF_AUTH_UNAVAILABLE", "sequence": jsonU64(uint64(len(o.transcript.entries)))}
			statusStr = "BPF_AUTH_UNAVAILABLE"
			break
		}
		caseSHA := peer.caseSHA256
		if supplied, present := body["case_sha256"]; present {
			var valid bool
			caseSHA, valid = digestString(supplied)
			if !valid || (peer.caseSHA256 != "" && peer.caseSHA256 != caseSHA) {
				return errControlFrame
			}
		}
		o.armedCaseSHA256 = caseSHA
		o.armed = true
		o.armedVariant = variant
		o.armedByRole = peer.role
		o.beginSequence = uint64(len(o.transcript.entries))
		o.authorityBefore = projectGuardStatus(st)
		respBody = map[string]any{
			"armed":            true,
			"begin_sequence":   jsonU64(o.beginSequence),
			"authority_before": o.authorityBefore,
		}
	case "READ":
		if !o.armed || !exactKeys(body, "after", "max_records") {
			return errControlFrame
		}
		after, ok1 := exactNumber(body["after"])
		maxRec, ok2 := exactNumber(body["max_records"])
		if !ok1 || !ok2 || maxRec == 0 || maxRec > 64 || after > uint64(len(o.transcript.entries)) {
			return errControlFrame
		}
		records, next, complete, err := o.transcript.recordsAfter(after, uint32(maxRec))
		if err != nil {
			return err
		}
		for {
			candidateBody := map[string]any{
				"records":  records,
				"next":     jsonU64(next),
				"complete": complete,
			}
			if _, e := o.makeObserverReply(reqID, nonce, "OK", candidateBody); e == nil {
				respBody = candidateBody
				break
			}
			if len(records) <= 1 {
				return errControlFrame
			}
			records = records[:len(records)-1]
			next--
			complete = false
		}
	case "FINISH":
		if !o.armed || !exactKeys(body) {
			return errControlFrame
		}
		if !o.finished {
			_, _ = o.performAuditReadback("configuration", nonce)
			_ = o.drainGuard()
			_ = o.drainAudit()
			st, err := o.guard.status()
			if err != nil {
				o.nativeLost = true
			} else {
				o.authorityAfter = projectGuardStatus(st)
				if st.Sticky != 0 || st.Lost != 0 || st.ProbeMissed != 0 {
					o.nativeLost = true
				}
			}
			if isObservationFaultVariant(o.armedVariant) {
				links, ok := o.authorityAfter["link_attached"].([]any)
				if !ok || len(links) != 3 || links[0] != true || links[1] != true || links[2] != true {
					return errControlFrame
				}
				if o.fdFaultPhase2 == 0 || o.fdFaultPhase0 <= o.fdFaultPhase2 || o.fdFaultExit == 0 || livePIDFD(o.brokerPIDFD) || o.authorityAfter["authority_phase"] != jsonU64(0) || o.nativeLost || o.auditLost || o.transcript.outputLoss {
					return errControlFrame
				}
				if o.brokerObservation != nil && o.brokerObservation["source"] == "broker_reader" {
					o.faultEvidence, err = o.readerFaultEvidence()
				} else {
					o.faultEvidence, err = o.sourceFaultEvidence()
				}
				if err != nil {
					return err
				}
			}
			if (o.armedVariant == "fd-fill-failure" || o.armedVariant == "fd-restore-failure") && (o.fdFaultPhase2 == 0 || o.fdFaultPhase0 <= o.fdFaultPhase2 || o.fdFaultExit == 0 || livePIDFD(o.brokerPIDFD) || o.authorityAfter["authority_phase"] != jsonU64(0)) {
				return errControlFrame
			}
			if o.armedVariant == "fd-fill-failure" || o.armedVariant == "fd-restore-failure" {
				o.faultEvidence, err = o.sourceFaultEvidence()
				if err != nil {
					return err
				}
			}
			o.endSequence = uint64(len(o.transcript.entries))
			o.terminalSHA256 = o.transcript.tail
			o.outcome = o.finishOutcome()
			o.finished = true
		}
		var faultEvidence any
		if o.faultEvidence != nil {
			faultEvidence = o.faultEvidence
		}
		respBody = map[string]any{
			"end_sequence":    jsonU64(o.endSequence),
			"terminal_sha256": o.terminalSHA256,
			"authority_after": o.authorityAfter,
			"losses": map[string]any{
				"native": o.nativeLost,
				"audit":  o.auditLost,
				"output": o.transcript.outputLoss,
			},
			"outcome":        o.outcome,
			"fault_evidence": faultEvidence,
		}
	default:
		return errControlFrame
	}
	replyRaw, err := o.makeObserverReply(reqID, nonce, statusStr, respBody)
	if err != nil {
		return err
	}
	peer.seenNonces[nonce] = true
	peer.lastRequestID = reqID
	peer.lastRequestRaw = append([]byte(nil), raw...)
	peer.lastResponseRaw = append([]byte(nil), replyRaw...)
	peer.nextRequest++
	return sendControl(fd, replyRaw, nil)
}

func openObserverAbstractListener(bootID string) (int, error) {
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		g2d1Fail("LISTENER_SOCKET", err)
		return -1, err
	}
	if err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		g2d1Fail("LISTENER_CREDENTIALS", err)
		_ = unix.Close(fd)
		return -1, err
	}
	if err = unix.Bind(fd, &unix.SockaddrUnix{Name: "\x00g2-bpf-terminal-" + bootID}); err != nil {
		g2d1Fail("LISTENER_BIND", err)
		_ = unix.Close(fd)
		return -1, err
	}
	if err = unix.Listen(fd, 16); err != nil {
		g2d1Fail("LISTENER_LISTEN", err)
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

// runTerminalObserver owns the broker and reader identity descriptors and the
// private credentialled reader witness socket until termination.
func runTerminalObserver(startupFD, brokerFD, brokerPIDFD, brokerExeFD, moduleMemfd, readerPIDFD, readerExeFD, witnessFD int) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		if startupFD >= 0 {
			_ = unix.Close(startupFD)
		}
		if brokerFD >= 0 {
			_ = unix.Close(brokerFD)
		}
		if brokerPIDFD >= 0 {
			_ = unix.Close(brokerPIDFD)
		}
		if brokerExeFD >= 0 {
			_ = unix.Close(brokerExeFD)
		}
		if moduleMemfd >= 0 {
			_ = unix.Close(moduleMemfd)
		}
		if readerPIDFD >= 0 {
			_ = unix.Close(readerPIDFD)
		}
		if readerExeFD >= 0 {
			_ = unix.Close(readerExeFD)
		}
		if witnessFD >= 0 {
			_ = unix.Close(witnessFD)
		}
	}()
	if os.Getppid() != 1 || unix.Gettid() != os.Getpid() || startupFD < 0 || brokerFD < 0 || brokerPIDFD < 0 || brokerExeFD < 0 || moduleMemfd < 0 || readerPIDFD < 0 || readerExeFD < 0 || witnessFD < 0 {
		return errGuardProtocol
	}
	for _, name := range []string{"user", "pid", "net"} {
		var initial, own unix.Stat_t
		if unix.Stat("/proc/1/ns/"+name, &initial) != nil || unix.Stat("/proc/self/ns/"+name, &own) != nil || initial.Dev != own.Dev || initial.Ino != own.Ino {
			return errGuardProtocol
		}
	}
	boot, err := fixedBootID()
	if err != nil {
		return err
	}
	brokerPID, err := auditReaderPID(brokerPIDFD)
	if err != nil {
		return err
	}
	selfSHA, err := procExecutableHash(os.Getpid())
	if err != nil {
		return err
	}
	heldBrokerSHA, _, err := heldRegularDigest(brokerExeFD, 128<<20)
	if err != nil || heldBrokerSHA != selfSHA {
		return errGuardProtocol
	}
	brokerProc, err := observeProcess(brokerPID, brokerPIDFD)
	if err != nil || brokerProc.PPID != 1 || brokerProc.ExecutableSHA256 != selfSHA {
		return errGuardProtocol
	}
	readerPID, err := auditReaderPID(readerPIDFD)
	if err != nil {
		return err
	}
	readerSHA, _, err := heldRegularDigest(readerExeFD, 128<<20)
	readerProc, procErr := observeProcess(readerPID, readerPIDFD)
	if err != nil || procErr != nil || readerSHA != selfSHA || readerProc.PPID != 1 || readerProc.UID != 0 || readerProc.GID != 0 || readerProc.ExecutableSHA256 != readerSHA || !exactArgumentVector(readerProc.Argv, []string{"/g2-bpf-authority", "audit-reader"}) || verifyAuditReaderConfinement(int(readerPID)) != nil {
		return errGuardProtocol
	}
	uniqueReader, uniqueFD, err := discoverBootstrapReader(bootstrapManifest{BrokerSHA256: selfSHA})
	if err != nil {
		return err
	}
	_ = unix.Close(uniqueFD)
	if uniqueReader.PID != readerPID || uniqueReader.StartTicks != readerProc.StartTicks || !pidfdSelects(readerPIDFD, readerPID) {
		return errGuardProtocol
	}
	parentSHA, err := procExecutableHash(1)
	if err != nil || parentSHA != selfSHA {
		return errGuardProtocol
	}
	for _, sfd := range []int{startupFD, brokerFD, witnessFD} {
		typ, e := unix.GetsockoptInt(sfd, unix.SOL_SOCKET, unix.SO_TYPE)
		if e != nil || typ != unix.SOCK_SEQPACKET {
			return errGuardProtocol
		}
		if e = unix.SetsockoptInt(sfd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); e != nil {
			return e
		}
		if e = unix.SetNonblock(sfd, true); e != nil {
			return e
		}
	}
	var stStartup, stBroker unix.Stat_t
	if unix.Fstat(startupFD, &stStartup) != nil || unix.Fstat(brokerFD, &stBroker) != nil || (stStartup.Dev == stBroker.Dev && stStartup.Ino == stBroker.Ino) {
		return errGuardProtocol
	}
	deadline, err := guestDeadline(30 * time.Second)
	if err != nil {
		return err
	}
	initFrame, cred, _, err := earlyProbeReceive(startupFD, 0, deadline)
	if err != nil || cred == nil || cred.Pid != 1 || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(initFrame, "version", "boot_id", "kind", "nonce", "module_sha256", "observer_bindings_sha256") || initFrame["version"] != json.Number("1") || initFrame["boot_id"] != boot || initFrame["kind"] != "observer_init" {
		return errGuardProtocol
	}
	nonce, ok1 := initFrame["nonce"].(string)
	modSHA, ok2 := digestString(initFrame["module_sha256"])
	bindSHA, ok3 := digestString(initFrame["observer_bindings_sha256"])
	if !ok1 || !validCheckpointNonce(nonce) || !ok2 || !ok3 {
		return errGuardProtocol
	}
	modDev, modIno, modSize, err := verifySealedModuleMemfd(moduleMemfd, modSHA, 0, 0, 0)
	if err != nil {
		return err
	}
	if err = confineTerminalObserver(); err != nil {
		return err
	}
	auditMC, err := openObserverMulticast()
	if err != nil {
		g2d1Fail("MULTICAST_INIT", err)
		return err
	}
	defer auditMC.close()
	queryFD, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_AUDIT)
	if err != nil {
		g2d1Fail("NETLINK_SOCKET", err)
		return err
	}
	defer unix.Close(queryFD)
	if err = unix.Bind(queryFD, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		g2d1Fail("NETLINK_BIND", err)
		return err
	}
	boundAddr, err := unix.Getsockname(queryFD)
	if err != nil {
		g2d1Fail("NETLINK_ADDRESS", err)
		return err
	}
	nlAddr, ok := boundAddr.(*unix.SockaddrNetlink)
	if !ok || nlAddr.Pid == 0 || nlAddr.Groups != 0 {
		g2d1Fail("NETLINK_ADDRESS", nil)
		return errGuardProtocol
	}
	obs := &terminalObserver{
		bootID:                 boot,
		moduleSHA256:           modSHA,
		observerBindingsSHA256: bindSHA,
		moduleMemfd:            moduleMemfd,
		moduleDev:              modDev,
		moduleIno:              modIno,
		moduleSize:             modSize,
		startupFD:              startupFD,
		brokerFD:               brokerFD,
		brokerPID:              brokerPID,
		brokerPIDFD:            brokerPIDFD,
		brokerExeFD:            brokerExeFD,
		witnessFD:              witnessFD,
		readerPID:              readerPID,
		readerPIDFD:            readerPIDFD,
		readerWitnessHoldFD:    -1,
		guard:                  guardDevice{fd: -1},
		audit:                  auditMC,
		queryFD:                queryFD,
		queryPort:              nlAddr.Pid,
		querySeq:               1,
		transcript:             newObserverTranscript(boot, ""),
		rawSyscalls:            make(map[auditEventKey]observerRawSyscallPending),
		rawSeen:                make(map[auditEventKey]struct{}),
		auditSerials:           make(map[auditEventKey]*observerAuditSerialPending),
		wireCounts:             make(map[auditWireKey]uint64),
	}
	if _, err = obs.performAuditReadback("configuration", nonce); err != nil {
		g2d1Fail("NETLINK_QUERY", err)
		return err
	}
	if err = earlyProbeSend(startupFD, map[string]any{
		"version": json.Number("1"),
		"boot_id": boot,
		"kind":    "observer_ready",
		"nonce":   nonce,
	}, nil, deadline); err != nil {
		g2d1Fail("READY_SEND", err)
		return err
	}
	guardFrame, brokerCred, rights, err := earlyProbeReceive(brokerFD, 1, deadline)
	if err != nil {
		g2d1Fail("GUARD_RECEIVE", err)
		return err
	}
	if brokerCred == nil || uint32(brokerCred.Pid) != brokerPID || brokerCred.Uid != 0 || brokerCred.Gid != 0 || len(rights) != 1 || !exactKeys(guardFrame, "version", "boot_id", "kind", "nonce") || guardFrame["version"] != json.Number("1") || guardFrame["boot_id"] != boot || guardFrame["kind"] != "observer_guard" || guardFrame["nonce"] != nonce {
		g2d1Fail("GUARD_FRAME_RIGHT_CREDENTIALS", nil)
		for _, rfd := range rights {
			_ = unix.Close(rfd)
		}
		return errGuardProtocol
	}
	obs.guard = guardDevice{fd: rights[0]}
	defer unix.Close(obs.guard.fd)
	st, err := obs.guard.status()
	if err != nil || st.ObserverCookie == 0 || st.BrokerCookie == 0 || st.ModuleInstance == 0 || st.ModuleDev != modDev || st.ModuleIno != modIno || st.ModuleSize != modSize || hex.EncodeToString(st.ObserverBindingsSHA256[:]) != bindSHA {
		g2d1Fail("GUARD_STATUS_IDENTITY", err)
		return errGuardProtocol
	}
	g2d1.scope = "MODULE_RECHECK"
	_, _, _, err = verifySealedModuleMemfd(moduleMemfd, modSHA, modDev, modIno, modSize)
	g2d1.scope = ""
	if err != nil {
		g2d1Fail("MODULE_RECHECK", err)
		return err
	}
	if err = earlyProbeSend(brokerFD, map[string]any{
		"version": json.Number("1"),
		"boot_id": boot,
		"kind":    "observer_guard_ready",
		"nonce":   nonce,
	}, nil, deadline); err != nil {
		g2d1Fail("GUARD_ACK", err)
		return err
	}
	pid1FD, err := unix.PidfdOpen(1, 0)
	if err != nil {
		g2d1Fail("PID1_PIDFD", err)
		return err
	}
	obs.pid1Peer = &observerPeerState{
		role:        "PID1",
		pid:         1,
		pidfd:       pid1FD,
		nextRequest: 1,
		seenNonces:  make(map[string]bool),
	}
	defer obs.pid1Peer.close()

	listener, err := openObserverAbstractListener(boot)
	if err != nil {
		g2d1Fail("LISTENER_INIT", err)
		return err
	}
	defer unix.Close(listener)

	clients := make(map[int]*observerPeerState)
	defer func() {
		for cfd := range clients {
			_ = unix.Close(cfd)
		}
	}()
	defer func() {
		if obs.readerWitnessHoldFD >= 0 {
			_ = unix.Close(obs.readerWitnessHoldFD)
		}
	}()

	for {
		_ = obs.drainGuard()
		_ = obs.drainAudit()

		poll := []unix.PollFd{
			{Fd: int32(obs.guard.fd), Events: unix.POLLIN},
			{Fd: int32(obs.audit.fd), Events: unix.POLLIN},
			{Fd: int32(listener), Events: unix.POLLIN},
		}
		startupIdx := -1
		if startupFD >= 0 {
			startupIdx = len(poll)
			poll = append(poll, unix.PollFd{Fd: int32(startupFD), Events: unix.POLLIN})
		}
		brokerIdx := -1
		if brokerFD >= 0 {
			brokerIdx = len(poll)
			poll = append(poll, unix.PollFd{Fd: int32(brokerFD), Events: unix.POLLIN})
		}
		witnessIdx := -1
		if witnessFD >= 0 {
			witnessIdx = len(poll)
			poll = append(poll, unix.PollFd{Fd: int32(witnessFD), Events: unix.POLLIN})
		}
		clientFDs := make([]int, 0, len(clients))
		for cfd := range clients {
			clientFDs = append(clientFDs, cfd)
			poll = append(poll, unix.PollFd{Fd: int32(cfd), Events: unix.POLLIN})
		}
		_, err := unix.Poll(poll, 50)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return err
		}
		if startupIdx >= 0 {
			rev := poll[startupIdx].Revents
			if rev&unix.POLLIN != 0 {
				if e := obs.handleClientRequest(startupFD, obs.pid1Peer); e != nil {
					_ = unix.Close(startupFD)
					startupFD = -1
				}
			} else if rev&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
				_ = unix.Close(startupFD)
				startupFD = -1
			}
		}
		if brokerIdx >= 0 {
			rev := poll[brokerIdx].Revents
			if rev&unix.POLLIN != 0 {
				if e := obs.handleBrokerMessage(); e != nil {
					_ = unix.Close(brokerFD)
					brokerFD = -1
				}
			} else if rev&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
				_ = unix.Close(brokerFD)
				brokerFD = -1
			}
		}
		if witnessIdx >= 0 && obs.serviceReaderWitness(poll[witnessIdx].Revents) {
			_ = unix.Close(witnessFD)
			witnessFD = -1
		}
		if poll[2].Revents&unix.POLLIN != 0 && len(clients) < 32 {
			cfd, _, e := unix.Accept4(listener, unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK)
			if e == nil {
				if unix.SetsockoptInt(cfd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1) != nil {
					_ = unix.Close(cfd)
				} else {
					ucred, ce := unix.GetsockoptUcred(cfd, unix.SOL_SOCKET, unix.SO_PEERCRED)
					if ce != nil || ucred == nil || ucred.Uid != 0 || ucred.Gid != 0 {
						_ = unix.Close(cfd)
					} else if obs.rootPeer != nil && uint32(ucred.Pid) == obs.rootPeer.pid && pidfdSelects(obs.rootPeer.pidfd, obs.rootPeer.pid) {
						clients[cfd] = obs.rootPeer
					} else if obs.exporterPeer != nil && uint32(ucred.Pid) == obs.exporterPeer.pid && pidfdSelects(obs.exporterPeer.pidfd, obs.exporterPeer.pid) {
						clients[cfd] = obs.exporterPeer
					} else {
						_ = unix.Close(cfd)
					}
				}
			}
		}
		baseClientIdx := 3
		if startupIdx >= 0 {
			baseClientIdx++
		}
		if brokerIdx >= 0 {
			baseClientIdx++
		}
		if witnessIdx >= 0 {
			baseClientIdx++
		}
		for i, cfd := range clientFDs {
			rev := poll[baseClientIdx+i].Revents
			if rev&unix.POLLIN != 0 {
				if e := obs.handleClientRequest(cfd, clients[cfd]); e != nil {
					_ = unix.Close(cfd)
					delete(clients, cfd)
				}
			} else if rev&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
				_ = unix.Close(cfd)
				delete(clients, cfd)
			}
		}
	}
}

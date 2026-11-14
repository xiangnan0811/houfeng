//go:build linux && amd64 && !cgo

package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

type auditBinding struct {
	TaskCookie    uint64
	TransactionID uint64 // zero represents null
	Command       string // empty represents null
}

type auditScopeResolver interface {
	// Resolve a complete syscall from already held task and transaction
	// identities. A TGID alone is never a thread incarnation proof.
	resolveAuditSyscall(auditFact) (auditBinding, error)
	// Resolve a guard-matched lifecycle event to its authorized transaction.
	resolveAuditBPF(guardRecord) (auditBinding, error)
	// Record a guard-observed raw syscall (kind 13) for correlation.
	observeRawSyscall(guardRecord) error
	// Publication order is a guard-observed incarnation fact; the boolean
	// records preauthorized cleanup of that exact old incarnation.
	auditIncarnation(cookie, id uint64) (uint64, bool, error)
}

type auditWireKey struct {
	event auditEventKey
	kind  uint16
	hash  string
}
type pendingAuditCall struct{ enter guardRecord }
type pendingAuditSerial struct {
	syscall           *auditFact
	syscallOccurrence uint64
	eoe               bool
	eoeFact           *auditFact
	eoeOccurrence     uint64
	hasBPF            bool
	pendingBPF        int
}

type auditControlActor struct {
	TaskCookie, TGID, TID, StartNS uint64
	ExeSHA256                      string
}
type auditControlExpectation struct {
	Actor              auditControlActor
	Phase, Change      string
	OldValue, NewValue *uint32
	RuleList           []uint32
	RequireSyscall     bool
}
type auditControlPending struct {
	expectation auditControlExpectation
	fact        *auditFact
}

// observationFaultState is armed only by the fixed selftest and retains the
// actual rejected input, never an inferred kernel loss or fabricated serial.
type observationFaultState struct {
	variant            string
	detector           string
	actorPID           uint64
	guardSequence      uint64
	original, modified []byte
	audit              *auditFact
	countBefore        uint64
	omittedKey         *auditEventKey
	mapEligible        func(guardRecord) bool
	auditActor         func(uint64) bool
}

// auditReorder buffers exactly the two registered actors' original pairs.
type auditReorder struct {
	actors     [2]uint64
	seenActors [2]uint64
	keys       [2]auditEventKey
	nonce      string
	received   []auditFact
	identities []any
	positions  [4]int // actor A/B syscall, then actor A/B EOE; one-based
	complete   bool
}

type auditJoin struct {
	transcript      *authorityTranscript
	resolver        auditScopeResolver
	calls           map[uint64][]pendingAuditCall
	callCount       int
	ready           []guardRecord
	serials         map[auditEventKey]*pendingAuditSerial
	eoeOrder        []auditEventKey
	completed       map[auditEventKey]struct{}
	wireCounts      map[auditWireKey]uint64
	rawSyscalls     map[auditEventKey]guardRecord
	rawSeen         map[auditEventKey]struct{}
	wireCount       uint64
	controls        []auditControlPending
	bpfOrder        []auditFact
	coverageStarted bool
	invalid         error
	fault           *observationFaultState
	reorder         *auditReorder
}

func newAuditJoin(t *authorityTranscript, resolver auditScopeResolver) *auditJoin {
	return &auditJoin{transcript: t, resolver: resolver, calls: make(map[uint64][]pendingAuditCall), serials: make(map[auditEventKey]*pendingAuditSerial), completed: make(map[auditEventKey]struct{}), wireCounts: make(map[auditWireKey]uint64), rawSyscalls: make(map[auditEventKey]guardRecord), rawSeen: make(map[auditEventKey]struct{})}
}

func (j *auditJoin) fail(stage string) error {
	if j.invalid == nil {
		j.invalid = fmt.Errorf("BPF_AUTH_UNAVAILABLE: audit join is incomplete or ambiguous")
		if g2d1.role == 'B' {
			g2d1CaptureFailure(stage, j.invalid)
		}
	}
	return j.invalid
}

func (j *auditJoin) observeGuard(r guardRecord) error {
	if j.invalid != nil {
		return j.invalid
	}
	if r.Kind == 13 {
		key, err := rawGuardAuditKey(r)
		if err != nil || j.resolver == nil || len(j.rawSeen) >= 4096 {
			return j.fail("JOIN_GUARD_RAW_KEY_RESOLVER_OR_CAPACITY")
		}
		if _, exists := j.rawSeen[key]; exists {
			return j.fail("JOIN_GUARD_RAW_DUPLICATE")
		}
		if _, completed := j.completed[key]; completed {
			return j.fail("JOIN_GUARD_RAW_ALREADY_COMPLETED")
		}
		if err := j.resolver.observeRawSyscall(r); err != nil {
			return j.fail("JOIN_GUARD_RAW_RESOLVER")
		}
		j.rawSeen[key] = struct{}{}
		j.rawSyscalls[key] = r
		return nil
	}
	if r.Kind != 7 && r.Kind != 8 {
		return j.fail("JOIN_GUARD_UNEXPECTED_KIND")
	}
	if r.ObjectCookie == 0 || r.KernelID == 0 || r.TaskCookie == 0 || r.Operation > 1 {
		return j.fail("JOIN_GUARD_IDENTITY_OR_OPERATION")
	}
	stack := j.calls[r.TaskCookie]
	if r.Kind == 7 {
		if r.Flags&1 != 0 || r.AuditSerial != 0 || len(stack) >= 8 || j.callCount+len(j.ready) >= 4096 {
			return j.fail("JOIN_GUARD_ENTER_FLAGS_SERIAL_OR_CAPACITY")
		}
		j.calls[r.TaskCookie] = append(stack, pendingAuditCall{enter: r})
		j.callCount++
		return nil
	}
	if len(stack) == 0 {
		return j.fail("JOIN_GUARD_RETURN_WITHOUT_ENTER")
	}
	call := stack[len(stack)-1].enter
	if r.Sequence <= call.Sequence || r.KernelID != call.KernelID || r.ObjectCookie != call.ObjectCookie || r.Operation != call.Operation || r.TaskStartNS != call.TaskStartNS || (r.Flags&6) != (call.Flags&6) {
		return j.fail("JOIN_GUARD_RETURN_MISMATCH")
	}
	if _, err := r.auditKey(); err != nil {
		return j.fail("JOIN_GUARD_RETURN_AUDIT_KEY")
	}
	if len(stack) == 1 {
		delete(j.calls, r.TaskCookie)
	} else {
		j.calls[r.TaskCookie] = stack[:len(stack)-1]
	}
	j.callCount--
	j.ready = append(j.ready, r)
	return nil
}

func (j *auditJoin) beginCoverage() error {
	if j == nil || j.coverageStarted || !j.settled() {
		return errGuardProtocol
	}
	j.coverageStarted = true
	return nil
}

// The caller pins each actor and serializes the corresponding kernel operation.
// No expectation is installed for changes before the multicast subscription.
func (j *auditJoin) expectControl(e auditControlExpectation) error {
	if j == nil || j.invalid != nil || len(j.controls) >= 3 || e.Actor.TaskCookie == 0 || e.Actor.TGID == 0 || e.Actor.TID == 0 || e.Actor.StartNS == 0 || len(e.Actor.ExeSHA256) != 64 {
		return errGuardProtocol
	}
	nonzero := false
	for _, c := range e.Actor.ExeSHA256 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return errGuardProtocol
		}
		if c != '0' {
			nonzero = true
		}
	}
	if !nonzero {
		return errGuardProtocol
	}
	switch e.Phase {
	case "final_rules":
		if e.Actor.TGID != 1 || e.Actor.TID != 1 || e.RequireSyscall || e.Change != "add_rule" || e.OldValue != nil || e.NewValue != nil || len(e.RuleList) != 1 || e.RuleList[0] != 4 {
			return errGuardProtocol
		}
		e.RuleList = []uint32{4}
	case "final_lock":
		if e.Actor.TGID != 1 || e.Actor.TID != 1 || e.RequireSyscall || e.Change != "enabled" || e.OldValue == nil || e.NewValue == nil || *e.OldValue != 1 || *e.NewValue != 2 || e.RuleList != nil {
			return errGuardProtocol
		}
		old, next := *e.OldValue, *e.NewValue
		e.OldValue, e.NewValue = &old, &next
	case "unicast_claim":
		if e.Actor.TGID == 1 || e.Actor.TID != e.Actor.TGID || !e.RequireSyscall || e.Change != "audit_pid" || e.OldValue == nil || e.NewValue == nil || *e.OldValue != 0 || uint64(*e.NewValue) != e.Actor.TGID || e.RuleList != nil {
			return errGuardProtocol
		}
		old, next := *e.OldValue, *e.NewValue
		e.OldValue, e.NewValue = &old, &next
	default:
		return errGuardProtocol
	}
	j.controls = append(j.controls, auditControlPending{expectation: e})
	return nil
}

func (j *auditJoin) auditControlsSettled() bool {
	return j != nil && j.invalid == nil && len(j.controls) == 0
}

func (j *auditJoin) controlInFlight() bool {
	for _, control := range j.controls {
		if control.fact != nil {
			return true
		}
	}
	return false
}

func (j *auditJoin) controlBlocks(key auditEventKey, syscall *auditFact) bool {
	for _, control := range j.controls {
		if control.fact != nil && control.fact.Key == key {
			return true
		}
		if control.fact == nil && syscall.PID == control.expectation.Actor.TGID && syscall.Arch == auditArchX8664 && (syscall.Syscall == 44 || syscall.Syscall == 46) {
			return true
		}
	}
	return false
}

// completeControl follows the actual serialized operation and a socket drain.
// If a syscall is present, its actual EOE is mandatory before publication.
func (j *auditJoin) completeControl() error {
	if j == nil || j.invalid != nil || len(j.controls) == 0 || j.controls[0].fact == nil {
		return errGuardProtocol
	}
	p := j.controls[0]
	f := p.fact
	e := p.expectation
	entry := j.serials[f.Key]
	if e.RequireSyscall && (entry == nil || entry.syscall == nil || !entry.eoe) {
		return errGuardProtocol
	}
	if !e.RequireSyscall && entry != nil {
		return j.fail("JOIN_CONTROL_UNEXPECTED_SERIAL")
	}
	if entry != nil && entry.syscall != nil {
		s := entry.syscall
		if !entry.eoe || s.PID != e.Actor.TGID || s.Arch != auditArchX8664 || (s.Syscall != 44 && s.Syscall != 46) || !s.Success {
			return j.fail("JOIN_CONTROL_SYSCALL_OR_EOE_MISMATCH")
		}
		if len(j.eoeOrder) == 0 || j.eoeOrder[0] != f.Key || entry.pendingBPF != 0 {
			return j.fail("JOIN_CONTROL_EOE_ORDER_OR_PENDING_BPF")
		}
		if err := j.appendSyscall(*s, auditBinding{TaskCookie: e.Actor.TaskCookie}, nil); err != nil {
			return err
		}
	} else if entry != nil && entry.eoe {
		return j.fail("JOIN_CONTROL_EOE_WITHOUT_SYSCALL")
	}
	var old, newValue, list any
	if e.OldValue != nil {
		old = jsonU64(uint64(*e.OldValue))
		newValue = jsonU64(uint64(*e.NewValue))
	}
	if e.RuleList != nil {
		values := make([]any, len(e.RuleList))
		for i, value := range e.RuleList {
			values[i] = jsonU64(uint64(value))
		}
		list = values
	}
	actor := map[string]any{"task_cookie": jsonU64(e.Actor.TaskCookie), "tgid": jsonU64(e.Actor.TGID), "tid": jsonU64(e.Actor.TID), "start_ns": jsonU64(e.Actor.StartNS), "exe_sha256": e.Actor.ExeSHA256}
	_, _, err := j.transcript.append("audit_control", map[string]any{"stamp": f.Key.stamp(), "serial": jsonU64(f.Serial), "actor": actor, "phase": e.Phase, "change": e.Change, "old_value": old, "new_value": newValue, "rule_list": list, "result": jsonU64(1)})
	if err != nil {
		return j.fail("JOIN_CONTROL_APPEND")
	}
	if entry != nil && entry.syscall != nil {
		j.eoeOrder = j.eoeOrder[1:]
		delete(j.serials, f.Key)
		j.completed[f.Key] = struct{}{}
	}
	if !e.RequireSyscall {
		j.completed[f.Key] = struct{}{}
	}
	j.controls = j.controls[1:]
	return nil
}

func (j *auditJoin) observeControl(f auditFact) error {
	index := -1
	for i := range j.controls {
		if j.controls[i].fact == nil {
			index = i
			break
		}
	}
	if index < 0 {
		return j.fail("JOIN_CONFIG_NO_EXPECTATION")
	}
	e := j.controls[index].expectation
	allowed := map[string]bool{"op": true, "res": true, "auid": true, "ses": true, "subj": true}
	if e.Change == "add_rule" {
		allowed["key"] = true
		allowed["list"] = true
	} else {
		allowed["old"] = true
		allowed[map[string]string{"enabled": "audit_enabled", "failure": "audit_failure", "rate_limit": "audit_rate_limit", "backlog_limit": "audit_backlog_limit", "audit_pid": "audit_pid"}[e.Change]] = true
	}
	for key := range f.Fields {
		if !allowed[key] {
			return j.fail("JOIN_CONFIG_UNEXPECTED_FIELD")
		}
	}
	if _, ok := f.Fields["auid"]; !ok {
		return j.fail("JOIN_CONFIG_MISSING_AUID")
	}
	if _, ok := f.Fields["ses"]; !ok {
		return j.fail("JOIN_CONFIG_MISSING_SESSION")
	}
	if f.Fields["res"] != "1" {
		return j.fail("JOIN_CONFIG_RESULT")
	}
	if e.Change == "add_rule" {
		if f.Fields["op"] != "add_rule" || f.Fields["list"] != "4" || f.Fields["key"] != "(null)" {
			return j.fail("JOIN_CONFIG_ADD_RULE_VALUE")
		}
	} else {
		name := map[string]string{"enabled": "audit_enabled", "failure": "audit_failure", "rate_limit": "audit_rate_limit", "backlog_limit": "audit_backlog_limit", "audit_pid": "audit_pid"}[e.Change]
		if f.Fields["op"] != "set" {
			return j.fail("JOIN_CONFIG_SET_OPERATION")
		}
		old, err := auditNumber(f.Fields, "old", 10, 32)
		if err != nil || old != uint64(*e.OldValue) {
			return j.fail("JOIN_CONFIG_OLD_VALUE")
		}
		value, err := auditNumber(f.Fields, name, 10, 32)
		if err != nil || value != uint64(*e.NewValue) {
			return j.fail("JOIN_CONFIG_NEW_VALUE")
		}
	}
	j.controls[index].fact = &f
	return nil
}

func (j *auditJoin) observeFact(f auditFact) error {
	if j.invalid != nil {
		return j.invalid
	}
	r := j.reorder
	if r == nil || r.complete {
		return j.observeFactOrdered(f)
	}
	index := -1
	if f.Kind == auditSyscallRecord && f.Arch == auditArchX8664 && f.Syscall == unix.SYS_BPF && f.Argument0 == unix.BPF_MAP_CREATE && f.Result == -1 {
		for i, actor := range r.actors {
			if f.PID == actor {
				index = i
				if r.positions[i] != 0 {
					return j.fail("JOIN_REORDER_DUPLICATE_ACTOR")
				}
				r.keys[i], r.seenActors[i] = f.Key, f.PID
				break
			}
		}
	} else if f.Kind == auditEOERecord {
		for i := range r.actors {
			if r.positions[i] != 0 && f.Key == r.keys[i] {
				index = i + 2
				break
			}
		}
	}
	if index < 0 {
		return j.observeFactOrdered(f)
	}
	if r.positions[index] != 0 || len(r.received) >= 4 || f.Serial == 0 || f.Key.Serial != f.Serial || f.PayloadSHA256 == "" || j.transcript == nil {
		return j.fail("JOIN_REORDER_POSITION_CAPACITY_OR_IDENTITY")
	}
	r.received = append(r.received, f)
	wire := auditWireKey{f.Key, f.Kind, f.PayloadSHA256}
	r.identities = append(r.identities, map[string]any{"stamp": f.Key.stamp(), "serial": jsonU64(f.Serial), "record_type": jsonU64(uint64(f.Kind)), "payload_sha256": f.PayloadSHA256, "occurrence": jsonU64(j.wireCounts[wire] + 1)})
	r.positions[index] = len(r.received)
	if len(r.received) != 4 {
		return nil
	}
	if !validCheckpointNonce(r.nonce) || r.keys[0] == r.keys[1] {
		return j.fail("JOIN_REORDER_NONCE_OR_DUPLICATE_KEY")
	}
	order := make([]any, 4)
	for i, position := range r.positions {
		if position == 0 {
			return j.fail("JOIN_REORDER_MISSING_POSITION")
		}
		order[i] = jsonU64(uint64(position))
	}
	if _, _, err := j.transcript.append("selftest_audit_reorder", map[string]any{"name": "audit-interleaved", "nonce": r.nonce, "received": r.identities, "delivery_order": order}); err != nil {
		return j.fail("JOIN_REORDER_APPEND")
	}
	for _, position := range r.positions {
		if err := j.observeFactOrdered(r.received[position-1]); err != nil {
			return err
		}
	}
	r.complete = true
	return nil
}

const maxAuditWireIdentities = 16384

func (j *auditJoin) observeFactOrdered(f auditFact) error {
	if j.invalid != nil {
		return j.invalid
	}
	if f.Serial == 0 || f.Key.Serial != f.Serial || f.PayloadSHA256 == "" || j.transcript == nil {
		return j.fail("JOIN_FACT_IDENTITY_OR_TRANSCRIPT")
	}
	wire := auditWireKey{f.Key, f.Kind, f.PayloadSHA256}
	if len(j.wireCounts) >= maxAuditWireIdentities && j.wireCounts[wire] == 0 && j.fault != nil && j.fault.variant == "audit-overflow" && j.fault.detector == "" {
		copyFact := f
		j.fault.detector, j.fault.audit, j.fault.countBefore = "audit_wire_capacity", &copyFact, uint64(len(j.wireCounts))
	}
	if len(j.wireCounts) >= maxAuditWireIdentities && j.wireCounts[wire] == 0 {
		return j.fail("JOIN_WIRE_CAPACITY")
	}
	j.wireCounts[wire]++
	if _, _, err := j.transcript.append("audit_wire", map[string]any{"stamp": f.Key.stamp(), "serial": jsonU64(f.Serial), "record_type": jsonU64(uint64(f.Kind)), "payload_sha256": f.PayloadSHA256, "occurrence": jsonU64(j.wireCounts[wire])}); err != nil {
		return j.fail("JOIN_WIRE_APPEND")
	}
	if j.wireCount == ^uint64(0) {
		return j.fail("JOIN_WIRE_COUNTER_OVERFLOW")
	}
	j.wireCount++
	if f.Kind == auditConfigRecord {
		return j.observeControl(f)
	}
	if f.Kind == auditSockaddrRecord || f.Kind == auditExecveRecord {
		return nil
	}
	entry := j.serials[f.Key]
	if entry == nil {
		if len(j.serials) >= 4096 {
			return j.fail("JOIN_PENDING_SERIAL_CAPACITY")
		}
		if _, seen := j.completed[f.Key]; seen {
			return j.fail("JOIN_SERIAL_ALREADY_COMPLETED")
		}
		entry = &pendingAuditSerial{}
		j.serials[f.Key] = entry
	}
	switch f.Kind {
	case auditSyscallRecord:
		if entry.syscall != nil && j.fault != nil && j.fault.variant == "audit-duplicate" && j.fault.detector == "" && f.PID == j.fault.actorPID && entry.syscall.PayloadSHA256 == f.PayloadSHA256 {
			copyFact := f
			j.fault.detector, j.fault.audit = "audit_duplicate", &copyFact
		}
		if entry.syscall != nil {
			return j.fail("JOIN_SYSCALL_DUPLICATE")
		}
		if entry.eoe {
			return j.fail("JOIN_SYSCALL_AFTER_EOE")
		}
		entry.syscall = &f
		entry.syscallOccurrence = j.wireCounts[wire]
	case auditBPFRecord:
		entry.hasBPF = true
		if entry.eoe {
			return j.fail("JOIN_BPF_AFTER_EOE")
		}
		if len(j.bpfOrder) >= 4096 {
			return j.fail("JOIN_PENDING_BPF_CAPACITY")
		}
		entry.pendingBPF++
		j.bpfOrder = append(j.bpfOrder, f)
	case auditEOERecord:
		if entry.eoe {
			return j.fail("JOIN_EOE_DUPLICATE")
		}
		if entry.syscall == nil {
			return j.fail("JOIN_EOE_WITHOUT_SYSCALL")
		}
		entry.eoe = true
		entry.eoeFact = &f
		entry.eoeOccurrence = j.wireCounts[wire]
		j.eoeOrder = append(j.eoeOrder, f.Key)
	default:
		return j.fail("JOIN_FACT_UNEXPECTED_KIND")
	}
	return nil
}

func nullableID(id uint64) any {
	if id == 0 {
		return nil
	}
	return jsonU64(id)
}
func nullableName(name string) any {
	if name == "" {
		return nil
	}
	return name
}

// A raw guard's timestamp and TGID are independently observed kernel facts.
// The audit PID is a TGID, not a thread identity or task start time.
func rawGuardAuditKey(r guardRecord) (auditEventKey, error) {
	seconds := guardU64(r.Detail[:], 8)
	nanoseconds := guardU32(r.Detail[:], 16)
	if r.Kind != 13 || r.TaskCookie == 0 || r.TaskStartNS == 0 || r.AuditSerial == 0 || r.AuditSerial > 0xffffffff ||
		seconds > 0x7fffffffffffffff || nanoseconds >= 1000000000 || guardU32(r.Detail[:], 0) == 0 {
		return auditEventKey{}, errGuardProtocol
	}
	return auditEventKey{seconds, nanoseconds / 1000000, r.AuditSerial}, nil
}

func rawAuditEvidence(syscall auditFact, syscallOccurrence uint64, eoe *auditFact, eoeOccurrence uint64, r guardRecord) (map[string]any, error) {
	key, err := rawGuardAuditKey(r)
	if err != nil || syscall.Kind != auditSyscallRecord || syscall.Key != key || syscall.Serial != key.Serial ||
		syscall.PayloadSHA256 == "" || syscallOccurrence == 0 || syscall.Arch != auditArchX8664 || syscall.Syscall != 321 ||
		syscall.PID == 0 || syscall.PID != uint64(guardU32(r.Detail[:], 0)) ||
		syscall.Argument0 != uint64(r.Operation) || syscall.Result != int64(r.Result) || bpfCommand(uint64(r.Operation)) == "" ||
		eoe == nil || eoe.Kind != auditEOERecord || eoe.Key != key || eoe.Serial != key.Serial || eoe.PayloadSHA256 == "" || eoeOccurrence == 0 {
		return nil, errGuardProtocol
	}
	return map[string]any{
		"tgid": jsonU64(syscall.PID), "syscall_payload_sha256": syscall.PayloadSHA256,
		"syscall_occurrence": jsonU64(syscallOccurrence), "eoe_payload_sha256": eoe.PayloadSHA256,
		"eoe_occurrence": jsonU64(eoeOccurrence),
	}, nil
}

func (j *auditJoin) appendSyscall(f auditFact, binding auditBinding, raw any) error {
	if binding.TaskCookie == 0 {
		return j.fail("JOIN_SYSCALL_BINDING_TASK_MISSING")
	}
	body := map[string]any{
		"stamp": f.Key.stamp(), "serial": jsonU64(f.Serial), "record_type": "SYSCALL", "context": "syscall", "object_cookie": nil,
		"transaction_id": nullableID(binding.TransactionID), "task_cookie": jsonU64(binding.TaskCookie),
		"command": nullableName(binding.Command), "result": jsonI64(f.Result), "operation": nil, "raw": raw,
	}
	_, _, err := j.transcript.append("audit", body)
	if err != nil {
		return j.fail("JOIN_SYSCALL_APPEND")
	}
	return nil
}

func (j *auditJoin) appendBPF(f auditFact, r guardRecord, binding auditBinding) error {
	if r.ObjectCookie == 0 || f.ProgramID != r.KernelID || (f.Operation == "LOAD") != (r.Operation == 0) {
		return j.fail("JOIN_BPF_GUARD_MISMATCH")
	}
	context := "standalone"
	var task any
	if r.Flags&1 != 0 {
		context = "syscall"
		if binding.TaskCookie == 0 {
			return j.fail("JOIN_BPF_BINDING_TASK_MISSING")
		}
		task = jsonU64(binding.TaskCookie)
	}
	body := map[string]any{
		"stamp": f.Key.stamp(), "serial": jsonU64(f.Serial), "record_type": "BPF", "context": context, "object_cookie": jsonU64(r.ObjectCookie),
		"transaction_id": nullableID(binding.TransactionID), "task_cookie": task,
		"command": nullableName(binding.Command), "result": nil, "operation": f.Operation, "raw": nil,
	}
	_, _, err := j.transcript.append("audit", body)
	if err != nil {
		return j.fail("JOIN_BPF_APPEND")
	}
	return nil
}

// matchingGuard selects the uniquely oldest matching incarnation. A reused
// ID is valid only if each older matched incarnation has authorized cleanup;
// the removal return itself may arrive after the newer publication.
func (j *auditJoin) matchingGuard(f auditFact, syscall bool) (int, error) {
	match := -1
	var oldest uint64
	var oldestRetiring bool
	for i, r := range j.ready {
		if r.KernelID != f.ProgramID || (f.Operation == "LOAD") != (r.Operation == 0) || (r.Flags&1 != 0) != syscall {
			continue
		}
		key, err := r.auditKey()
		if err != nil {
			return -1, j.fail("JOIN_MATCH_GUARD_AUDIT_KEY")
		}
		if key != f.Key {
			continue
		}
		sequence, retiring, err := j.resolver.auditIncarnation(r.ObjectCookie, r.KernelID)
		if err != nil || sequence == 0 {
			return -1, j.fail("JOIN_MATCH_INCARNATION")
		}
		if match == -1 {
			match, oldest, oldestRetiring = i, sequence, retiring
			continue
		}
		if sequence == oldest {
			prior := j.ready[match]
			if prior.ObjectCookie != r.ObjectCookie || prior.TaskCookie != r.TaskCookie || prior.TaskStartNS != r.TaskStartNS {
				return -1, j.fail("JOIN_MATCH_INCARNATION_IDENTITY")
			}
			if r.Sequence < prior.Sequence {
				match = i
			}
			continue
		}
		if sequence < oldest {
			if !retiring {
				return -1, j.fail("JOIN_MATCH_OLDER_NOT_RETIRING")
			}
			match, oldest, oldestRetiring = i, sequence, retiring
		} else if !oldestRetiring {
			return -1, j.fail("JOIN_MATCH_PRIOR_NOT_RETIRING")
		}
	}
	return match, nil
}

func (j *auditJoin) consumeBPF(f auditFact, syscall bool) (bool, error) {
	i, err := j.matchingGuard(f, syscall)
	if err != nil {
		return false, err
	}
	if i < 0 {
		return false, nil
	}
	r := j.ready[i]
	binding, err := j.resolver.resolveAuditBPF(r)
	if err != nil {
		return false, j.fail("JOIN_BPF_RESOLVE")
	}
	if err = j.appendBPF(f, r, binding); err != nil {
		return false, err
	}
	j.ready = append(j.ready[:i], j.ready[i+1:]...)
	return true, nil
}

func (j *auditJoin) drain() error {
	if j.resolver == nil || j.transcript == nil {
		return j.fail("JOIN_DRAIN_RESOLVER_OR_TRANSCRIPT")
	}
	// Preserve the actual BPF multicast order across interleaved serials.
	// Syscall-context records wait for their own real EOE before publication.
	for len(j.bpfOrder) > 0 {
		fact := j.bpfOrder[0]
		entry := j.serials[fact.Key]
		if entry == nil || entry.pendingBPF <= 0 {
			return j.fail("JOIN_DRAIN_BPF_PENDING_STATE")
		}
		syscallMatch, err := j.matchingGuard(fact, true)
		if err != nil {
			return err
		}
		standaloneMatch, err := j.matchingGuard(fact, false)
		if err != nil {
			return err
		}
		if syscallMatch >= 0 && standaloneMatch >= 0 {
			return j.fail("JOIN_DRAIN_BPF_AMBIGUOUS_CONTEXT")
		}
		if syscallMatch < 0 && standaloneMatch < 0 {
			break
		}
		isSyscall := syscallMatch >= 0
		if isSyscall && !entry.eoe {
			break
		}
		consumed, err := j.consumeBPF(fact, isSyscall)
		if err != nil {
			return err
		}
		if !consumed {
			return j.fail("JOIN_DRAIN_BPF_NOT_CONSUMED")
		}
		entry.pendingBPF--
		j.bpfOrder = j.bpfOrder[1:]
		if !isSyscall && entry.pendingBPF == 0 {
			if entry.syscall != nil || entry.eoe {
				return j.fail("JOIN_DRAIN_STANDALONE_SYSCALL_OR_EOE")
			}
			delete(j.serials, fact.Key)
		}
	}
	for {
		progressed := false
		for len(j.controls) > 0 && j.controls[0].fact != nil {
			p := j.controls[0]
			if p.expectation.RequireSyscall {
				entry := j.serials[p.fact.Key]
				if entry == nil || entry.syscall == nil || !entry.eoe || len(j.eoeOrder) == 0 || j.eoeOrder[0] != p.fact.Key || entry.pendingBPF != 0 {
					break
				}
			}
			if err := j.completeControl(); err != nil {
				return err
			}
			progressed = true
		}
		for len(j.eoeOrder) > 0 {
			serial := j.eoeOrder[0]
			entry := j.serials[serial]
			if entry == nil || !entry.eoe || entry.syscall == nil {
				return j.fail("JOIN_DRAIN_EOE_PENDING_STATE")
			}
			if j.controlBlocks(serial, entry.syscall) {
				break
			}
			if entry.pendingBPF != 0 {
				break
			}
			syscall := entry.syscall
			isBPF := syscall.Arch == auditArchX8664 && syscall.Syscall == 321 || syscall.Arch == auditArchI386 && syscall.Syscall == 357
			if isBPF || entry.hasBPF {
				binding, err := j.resolver.resolveAuditSyscall(*syscall)
				if err != nil {
					break
				}
				var raw any
				if record, ok := j.rawSyscalls[serial]; ok {
					if record.TaskCookie != binding.TaskCookie || binding.TransactionID != 0 {
						return j.fail("JOIN_DRAIN_RAW_BINDING_MISMATCH")
					}
					evidence, err := rawAuditEvidence(*syscall, entry.syscallOccurrence, entry.eoeFact, entry.eoeOccurrence, record)
					if err != nil {
						return j.fail("JOIN_DRAIN_RAW_EVIDENCE")
					}
					raw = evidence
					delete(j.rawSyscalls, serial)
				} else if binding.TransactionID == 0 {
					return j.fail("JOIN_DRAIN_MISSING_RAW_OR_TRANSACTION")
				}
				if err = j.appendSyscall(*syscall, binding, raw); err != nil {
					return err
				}
			}
			delete(j.serials, serial)
			j.completed[serial] = struct{}{}
			j.eoeOrder = j.eoeOrder[1:]
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return nil
}

func (j *auditJoin) settled() bool {
	return j != nil && j.invalid == nil && (j.reorder == nil || j.reorder.complete) && !j.controlInFlight() && j.callCount == 0 && len(j.calls) == 0 && len(j.ready) == 0 && len(j.serials) == 0 && len(j.eoeOrder) == 0 && len(j.bpfOrder) == 0 && len(j.rawSyscalls) == 0
}

func (j *auditJoin) detectOmittedAudit() error {
	if j == nil || j.fault == nil || j.fault.variant != "audit-loss" || j.fault.detector != "" || j.fault.omittedKey == nil || j.fault.audit == nil {
		return nil
	}
	key := *j.fault.omittedKey
	entry := j.serials[key]
	_, rawPresent := j.rawSyscalls[key]
	if entry == nil || entry.syscall == nil || entry.syscall.PID != j.fault.actorPID || entry.eoe || !rawPresent {
		return nil
	}
	if _, err := j.auditCutReady(); err == nil || j.invalid != nil {
		return errGuardProtocol
	}
	j.fault.detector = "audit_incomplete"
	return errGuardProtocol
}

// auditCutReady does not block the broker's other event consumers.
func (j *auditJoin) auditCutReady() (uint64, error) {
	if j == nil || j.invalid != nil {
		return 0, errGuardProtocol
	}
	if err := j.drain(); err != nil {
		return 0, err
	}
	if !j.settled() || !j.auditControlsSettled() {
		return 0, errGuardProtocol
	}
	return j.wireCount, nil
}

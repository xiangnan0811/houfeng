//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	settlementEvidenceBytes    = 4096
	settlementEvidenceObjects  = 384
	settlementEvidenceEarly    = 8
	settlementGapCapacity      = 1 << 0
	settlementGapUnbound       = 1 << 1
	settlementGapContradiction = 1 << 2
	settlementGapSaturated     = 1 << 3
	settlementGapLoss          = 1 << 4
	settlementGapStatus        = 1 << 5
	settlementGapTail          = 1 << 6
	settlementGapExport        = 1 << 7
)

type settlementEvidenceFact struct {
	firstSeq, lastSeq, firstNative, lastNative     uint64
	firstBroker, lastBroker, count, value0, value1 uint64
	state, flags                                   uint32
}

func (f *settlementEvidenceFact) add(seq, native, broker, value0, value1 uint64) uint32 {
	if f.count == math.MaxUint64 {
		f.state, f.flags = 4, f.flags|4
		return settlementGapSaturated
	}
	var gap uint32
	if f.count == 0 {
		f.firstSeq, f.firstNative, f.firstBroker = seq, native, broker
		f.value0, f.value1, f.state = value0, value1, 1
	} else {
		if seq != 0 && seq == f.lastSeq {
			f.flags |= 1
			if native != 0 && f.lastNative != 0 && native != f.lastNative {
				f.flags |= 2
				f.state = 4
				gap |= settlementGapContradiction
				native = 0
			}
		}
		if f.flags&2 != 0 && seq == f.lastSeq {
			native = 0
		}
	}
	f.lastSeq, f.lastNative, f.lastBroker = seq, native, broker
	f.count++
	if f.count == math.MaxUint64 {
		f.state, f.flags = 4, f.flags|4
		return gap | settlementGapSaturated
	}
	return gap
}

type settlementEvidenceStatus struct {
	status        guardStatus
	before, after uint64
	source, code  uint64
}

type settlementEvidenceObject struct {
	object                           *objectIncarnation
	grant                            *brokerGrant
	cookie, id, creator, publication uint64
	class, objectType                uint32
	facts                            [32]settlementEvidenceFact
}

type settlementEvidenceGrant struct {
	grant                              *brokerGrant
	purpose                            uint8
	boot, epoch, nonce                 [16]byte
	cid                                [32]byte
	start, deadline, authorityDeadline uint64
	issuedAt                           uint64
	root, cgroup, enrollment           uint64
	facts                              [32]settlementEvidenceFact
	startSuccess, startAttempt         settlementEvidenceStatus
	target                             *settlementEvidenceObject
	gap                                uint32
	exportError                        uint64
	firstGuard, lastGuard              uint64
	known, pending                     uint64
	lastWait                           uint64
	snapshots                          [3][settlementEvidenceBytes]byte
	phases                             [3]uint8
	snapshotCount, sent                uint8
	final                              bool
}

type settlementEvidenceEarlyRecord struct {
	record guardRecord
	broker uint64
	grant  *brokerGrant
}

type ownerProbeEvidenceState struct {
	broker                                                                           *authorityBroker
	transport                                                                        *settlementEvidenceTransport
	moduleInstance                                                                   uint64
	grants                                                                           [2]settlementEvidenceGrant
	objects                                                                          [settlementEvidenceObjects]settlementEvidenceObject
	used                                                                             uint16
	early                                                                            [settlementEvidenceEarly]settlementEvidenceEarlyRecord
	earlyUsed                                                                        uint8
	latestSuccess, latestAttempt                                                     settlementEvidenceStatus
	decodedThrough, ringThrough, transcriptThrough, trackerThrough, lifecycleThrough uint64
	batchError                                                                       uint64
}

func evidenceNow() uint64 {
	now, _ := monoNowNS()
	return now
}

func (s *ownerProbeEvidenceState) grantFor(g *brokerGrant) *settlementEvidenceGrant {
	if s == nil || g == nil {
		return nil
	}
	for i := range s.grants {
		if s.grants[i].grant == g {
			return &s.grants[i]
		}
	}
	return nil
}

func (s *ownerProbeEvidenceState) issue(b *authorityBroker, g *brokerGrant) {
	if s == nil || b == nil || g == nil || g.owner != b.bootOwner || b.bootOwner == 0 {
		return
	}
	var index int
	switch g.purpose {
	case "postgres-uid":
		index = 0
	case "postgres-gid":
		index = 1
	default:
		return
	}
	e := &s.grants[index]
	if e.grant != nil {
		e.gap |= settlementGapContradiction
		return
	}
	e.grant, e.purpose = g, uint8(index+1)
	if _, err := hex.Decode(e.boot[:], []byte(strings.ReplaceAll(b.bootID, "-", ""))); err != nil {
		e.gap |= settlementGapUnbound
	}
	if _, err := hex.Decode(e.epoch[:], []byte(b.epoch)); err != nil {
		e.gap |= settlementGapUnbound
	}
	if _, err := hex.Decode(e.cid[:], []byte(g.containerID)); err != nil {
		e.gap |= settlementGapUnbound
	}
	if s.transport != nil {
		for i := range s.transport.connections {
			c := &s.transport.connections[i]
			if c.hello && c.purpose == e.purpose && c.principal != nil && c.principal.ID == g.owner {
				if c.fd < 0 {
					e.gap |= settlementGapExport
					e.exportError = 5
				}
				e.nonce = c.nonce
				if c.bound {
					if e.cid != c.cid {
						e.gap |= settlementGapContradiction
						s.transport.fail(c, s, 4)
					} else {
						e.start, e.deadline, e.authorityDeadline = c.start, c.deadline, c.authorityDeadline
					}
				}
				e.fact(13, 0, 0, c.registeredAt, g.owner, 0)
				break
			}
		}
	}
	if s.transport == nil {
		e.gap |= settlementGapExport
		e.exportError = 1
	}
	e.startSuccess, e.startAttempt = s.latestSuccess, s.latestAttempt
	e.issuedAt = evidenceNow()
	e.fact(0, 0, 0, e.issuedAt, g.id, 0)
	e.capture(s, 1, 0, 0)
}

func (e *settlementEvidenceGrant) fact(slot int, seq, native, broker, value0, value1 uint64) {
	if e == nil || e.final || slot < 0 || slot >= len(e.facts) {
		return
	}
	e.gap |= e.facts[slot].add(seq, native, broker, value0, value1)
}

func (s *ownerProbeEvidenceState) objectFor(o *objectIncarnation) *settlementEvidenceObject {
	if s == nil || o == nil || o.diagnosticIndex == 0 || int(o.diagnosticIndex) > int(s.used) {
		return nil
	}
	d := &s.objects[o.diagnosticIndex-1]
	if d.object != o || d.cookie != o.cookie || d.id != o.id || d.creator != o.ownerTransaction || d.publication != o.publicationSeq {
		if e := s.grantFor(d.grant); e != nil && !e.final {
			e.gap |= settlementGapContradiction
		}
		return nil
	}
	return d
}

func (s *ownerProbeEvidenceState) objectForRecord(r *guardRecord) *objectIncarnation {
	if s == nil || s.broker == nil || s.broker.effects == nil || s.broker.effects.objects == nil {
		return nil
	}
	o := s.broker.effects.objects.byCookie[r.ObjectCookie]
	if o != nil && o.cookie != r.ObjectCookie {
		if d := s.objectFor(o); d != nil {
			if e := s.grantFor(d.grant); e != nil && !e.final {
				e.gap |= settlementGapContradiction
			}
		}
		return nil
	}
	return o
}

func (s *ownerProbeEvidenceState) publish(g *brokerGrant, o *objectIncarnation, r guardRecord) {
	e := s.grantFor(g)
	if e == nil || e.final || o == nil {
		return
	}
	if o.diagnosticIndex != 0 {
		e.gap |= settlementGapContradiction
		return
	}
	if int(s.used) == len(s.objects) {
		e.gap |= settlementGapCapacity
		return
	}
	d := &s.objects[s.used]
	s.used++
	d.object, d.grant, d.cookie, d.id, d.creator, d.publication = o, g, o.cookie, o.id, o.ownerTransaction, o.publicationSeq
	d.objectType = o.objectType
	if o.class == "program" {
		d.class = 1
	} else if o.class == "map" {
		d.class = 2
	} else {
		e.gap |= settlementGapUnbound
	}
	o.diagnosticIndex = s.used
	now := evidenceNow()
	e.gap |= d.facts[17].add(r.Sequence, 0, now, r.Sequence, 0)
	remaining := uint8(0)
	for i := range int(s.earlyUsed) {
		x := s.early[i]
		if x.grant == g && x.record.ObjectCookie == o.cookie && x.record.KernelID == o.id {
			s.recordObject(e, d, &x.record, x.broker)
		} else {
			s.early[remaining] = x
			remaining++
		}
	}
	for i := int(remaining); i < int(s.earlyUsed); i++ {
		s.early[i] = settlementEvidenceEarlyRecord{}
	}
	s.earlyUsed = remaining
}

func (s *ownerProbeEvidenceState) recordObject(e *settlementEvidenceGrant, d *settlementEvidenceObject, r *guardRecord, now uint64) {
	if e == nil || d == nil || e.final {
		return
	}
	slot := 16
	native := uint64(0)
	if r.Kind == 2 || r.Kind == 4 {
		slot, native = 24, guardU64(r.Detail[:], 0)
	}
	e.gap |= d.facts[slot].add(r.Sequence, native, now, r.Sequence, 0)
	if e.firstGuard == 0 || r.Sequence < e.firstGuard {
		e.firstGuard = r.Sequence
	}
	if r.Sequence > e.lastGuard {
		e.lastGuard = r.Sequence
	}
}

func (s *ownerProbeEvidenceState) decoded(r *guardRecord) {
	if s == nil {
		return
	}
	s.decodedThrough = r.Sequence
	if s.broker == nil || s.broker.effects == nil {
		return
	}
	if r.Kind < 1 || r.Kind > 6 {
		return
	}
	b := s.broker
	now := evidenceNow()
	if r.Kind == 1 || r.Kind == 3 {
		if t := b.effects.pending; t != nil {
			g := b.grants[t.grantID]
			if e := s.grantFor(g); e != nil && !e.final {
				if int(s.earlyUsed) == len(s.early) {
					e.gap |= settlementGapCapacity
				} else {
					s.early[s.earlyUsed] = settlementEvidenceEarlyRecord{*r, now, g}
					s.earlyUsed++
				}
			}
		}
	} else if r.Kind == 2 || r.Kind == 4 {
		if b.effects.objects != nil {
			if d := s.objectFor(s.objectForRecord(r)); d != nil {
				if d.id == r.KernelID && (d.class == 1 && r.Kind == 2 || d.class == 2 && r.Kind == 4) {
					s.recordObject(s.grantFor(d.grant), d, r, now)
				} else if e := s.grantFor(d.grant); e != nil && !e.final {
					e.gap |= settlementGapContradiction
				}
				return
			}
		}
		for i := range int(s.earlyUsed) {
			x := s.early[i]
			if x.grant != nil && x.record.ObjectCookie == r.ObjectCookie && x.record.KernelID == r.KernelID {
				if int(s.earlyUsed) == len(s.early) {
					if e := s.grantFor(x.grant); e != nil {
						e.gap |= settlementGapCapacity
					}
				} else {
					s.early[s.earlyUsed] = settlementEvidenceEarlyRecord{*r, now, x.grant}
					s.earlyUsed++
				}
				break
			}
		}
	} else if r.Kind == 5 || r.Kind == 6 {
		var pending *settlementEvidenceGrant
		for i := range s.grants {
			e := &s.grants[i]
			if e.grant == nil || e.final {
				continue
			}
			if e.root == r.RootCookie && e.cgroup == r.CgroupID && e.enrollment != 0 {
				s.recordCgroup(e, r, now)
				return
			}
			if _, ok := b.effects.enrollmentPending[e.grant.id]; ok {
				if pending != nil {
					pending.gap |= settlementGapUnbound
					e.gap |= settlementGapUnbound
					return
				}
				pending = e
			}
		}
		if pending != nil {
			if int(s.earlyUsed) == len(s.early) {
				pending.gap |= settlementGapCapacity
			} else {
				s.early[s.earlyUsed] = settlementEvidenceEarlyRecord{*r, now, pending.grant}
				s.earlyUsed++
			}
		}
	}
}

func (s *ownerProbeEvidenceState) recordCgroup(e *settlementEvidenceGrant, r *guardRecord, now uint64) {
	if e == nil || e.final {
		return
	}
	slot := 2
	if r.Kind == 6 {
		slot = 3
	}
	e.fact(slot, r.Sequence, guardU64(r.Detail[:], 0), now, r.Sequence, 0)
	if e.firstGuard == 0 || r.Sequence < e.firstGuard {
		e.firstGuard = r.Sequence
	}
	if r.Sequence > e.lastGuard {
		e.lastGuard = r.Sequence
	}
}

func (s *ownerProbeEvidenceState) enroll(g *brokerGrant, group *liveCgroup) {
	e := s.grantFor(g)
	if e == nil || e.final || group == nil {
		return
	}
	e.root, e.cgroup, e.enrollment = group.RootCookie, group.KernfsID, group.Enrollment
	e.fact(1, 0, 0, evidenceNow(), group.Enrollment, 0)
	remaining := uint8(0)
	for i := range int(s.earlyUsed) {
		x := s.early[i]
		if x.grant == g && (x.record.Kind == 5 || x.record.Kind == 6) {
			if x.record.RootCookie == e.root && x.record.CgroupID == e.cgroup && e.enrollment != 0 {
				s.recordCgroup(e, &x.record, x.broker)
			} else {
				e.gap |= settlementGapUnbound
			}
		} else {
			s.early[remaining] = x
			remaining++
		}
	}
	for i := int(remaining); i < int(s.earlyUsed); i++ {
		s.early[i] = settlementEvidenceEarlyRecord{}
	}
	s.earlyUsed = remaining
}

func (s *ownerProbeEvidenceState) status(source uint64, status guardStatus, err error, before, after uint64) {
	if s == nil {
		return
	}
	code := uint64(1)
	if errors.Is(err, unix.EAGAIN) {
		code = 2
	} else if err != nil {
		code = 3
	}
	s.latestAttempt = settlementEvidenceStatus{before: before, after: after, source: source, code: code}
	if err == nil {
		s.latestSuccess = settlementEvidenceStatus{status: status, before: before, after: after, source: source, code: 1}
		for i := range s.grants {
			e := &s.grants[i]
			if e.grant == nil || e.final {
				continue
			}
			if s.moduleInstance != 0 && status.ModuleInstance != s.moduleInstance {
				e.gap |= settlementGapContradiction
			}
			if status.Lost != 0 || status.ProbeMissed != 0 || status.Sticky != 0 {
				e.gap |= settlementGapLoss
			}
			if status.AuthorityPhase != 1 {
				e.fact(12, status.Sequence, 0, after, uint64(status.AuthorityPhase), 0)
			}
		}
	}
}

func evidencePutFact(out []byte, f settlementEvidenceFact, cutoff uint64) {
	if cutoff != 0 && (f.firstNative > cutoff || f.lastNative > cutoff || f.firstBroker > cutoff || f.lastBroker > cutoff) {
		f.flags |= 8
	}
	values := [...]uint64{f.firstSeq, f.lastSeq, f.firstNative, f.lastNative, f.firstBroker, f.lastBroker, f.count, f.value0, f.value1}
	for i, value := range values {
		binary.LittleEndian.PutUint64(out[i*8:], value)
	}
	binary.LittleEndian.PutUint32(out[72:], f.state)
	binary.LittleEndian.PutUint32(out[76:], f.flags)
}

func evidencePutStatus(c *[64]uint64, offset int, success, attempt settlementEvidenceStatus) {
	if success.code == 1 {
		c[offset], c[2+offset], c[4+offset], c[6+offset] = 1, success.status.Sequence, success.status.AckCursor, success.status.ObserverAckCursor
		c[8+offset], c[10+offset], c[12+offset], c[14+offset] = success.status.Lost, success.status.ProbeMissed, uint64(success.status.Sticky), uint64(success.status.AuthorityPhase)
	}
	c[31+offset], c[33+offset*2], c[34+offset*2] = attempt.code, attempt.before, attempt.after
	c[37+offset*2], c[38+offset*2], c[41+offset], c[43+offset] = success.before, success.after, success.source, attempt.source
}

func (e *settlementEvidenceGrant) pin(o *settlementEvidenceObject) {
	if e == nil || e.final || o == nil || o.grant != e.grant {
		return
	}
	if e.target != nil && e.target != o {
		e.gap |= settlementGapContradiction
		return
	}
	if e.target == nil {
		e.target = o
		e.fact(31, 0, 0, evidenceNow(), o.cookie, o.publication)
	}
}

func (e *settlementEvidenceGrant) capture(s *ownerProbeEvidenceState, phase uint8, cutoff uint64, reason uint32) {
	if e == nil || s == nil || e.grant == nil || e.final || e.snapshotCount == 3 {
		return
	}
	if phase != 1 && phase != 2 && phase != 3 || e.snapshotCount != 0 && phase <= e.phases[e.snapshotCount-1] {
		return
	}
	if phase != 1 && e.target == nil {
		for i := range int(s.used) {
			o := &s.objects[i]
			if o.grant == e.grant && o.class == 1 && o.objectType == unix.BPF_PROG_TYPE_CGROUP_DEVICE && (e.target == nil || o.publication < e.target.publication) {
				e.target = o
			}
		}
		if e.target != nil {
			e.fact(31, 0, 0, evidenceNow(), e.target.cookie, e.target.publication)
		} else {
			e.gap |= settlementGapUnbound
		}
	}
	var unbound uint64
	for i := range int(s.earlyUsed) {
		if s.early[i].grant == e.grant {
			unbound++
		}
	}
	if phase != 1 && unbound != 0 {
		e.gap |= settlementGapUnbound
	}
	gap := e.gap | settlementGapTail
	if s.latestSuccess.code == 0 {
		gap |= settlementGapStatus
	}
	out := &e.snapshots[e.snapshotCount]
	copy(out[:8], "G2SEV1\x00\x00")
	binary.LittleEndian.PutUint16(out[8:], 1)
	out[10], out[11] = phase, e.purpose
	binary.LittleEndian.PutUint32(out[12:], gap)
	values := [15]uint64{e.grant.generation, e.grant.id, e.grant.owner, 0, 0, 0, 0, e.root, e.cgroup, e.enrollment, e.start, e.deadline, e.authorityDeadline, cutoff, evidenceNow()}
	if e.target != nil {
		values[3], values[4], values[5], values[6] = e.target.creator, e.target.cookie, e.target.id, e.target.publication
	}
	for i, value := range values {
		binary.LittleEndian.PutUint64(out[16+i*8:], value)
	}
	copy(out[136:152], e.boot[:])
	copy(out[152:168], e.epoch[:])
	copy(out[168:184], e.nonce[:])
	copy(out[184:216], e.cid[:])
	if e.target != nil {
		binary.LittleEndian.PutUint32(out[216:], e.target.class)
		binary.LittleEndian.PutUint32(out[220:], e.target.objectType)
	}
	binary.LittleEndian.PutUint64(out[224:], e.firstGuard)
	binary.LittleEndian.PutUint64(out[232:], e.lastGuard)
	binary.LittleEndian.PutUint64(out[240:], s.moduleInstance)
	binary.LittleEndian.PutUint32(out[248:], reason)
	for i := range e.facts {
		f := e.facts[i]
		if i >= 16 && i <= 30 && e.target != nil {
			f = e.target.facts[i]
		}
		evidencePutFact(out[256+i*80:], f, cutoff)
	}
	if e.target != nil {
		r := [8]uint64{e.grant.id, e.target.creator, e.target.cookie, e.target.id, e.target.publication, e.root, e.cgroup, e.enrollment}
		for i, v := range r {
			binary.LittleEndian.PutUint64(out[2816+i*8:], v)
		}
		if e.root != 0 && e.cgroup != 0 && e.enrollment != 0 {
			r = [8]uint64{e.grant.id, 0, 0, 0, 0, e.root, e.cgroup, e.enrollment}
			for i, v := range r {
				binary.LittleEndian.PutUint64(out[2880+i*8:], v)
			}
		}
	}
	var coverage [64]uint64
	evidencePutStatus(&coverage, 0, e.startSuccess, e.startAttempt)
	evidencePutStatus(&coverage, 1, s.latestSuccess, s.latestAttempt)
	coverage[16], coverage[17], coverage[18], coverage[19], coverage[20] = s.ringThrough, s.transcriptThrough, s.trackerThrough, s.lifecycleThrough, s.latestSuccess.status.Sequence
	if s.latestSuccess.code == 1 && s.latestSuccess.after >= e.issuedAt && e.issuedAt != 0 {
		coverage[21] |= 1
		watermark := s.latestSuccess.status.Sequence
		if s.ringThrough >= watermark {
			coverage[21] |= 2
		}
		if s.transcriptThrough >= watermark {
			coverage[21] |= 4
		}
		if s.trackerThrough >= watermark && s.lifecycleThrough >= watermark {
			coverage[21] |= 8
		}
	}
	coverage[22], coverage[23], coverage[24], coverage[25], coverage[26], coverage[28], coverage[29], coverage[30] = e.known, e.pending, uint64(gap), settlementEvidenceObjects, uint64(s.used), e.exportError, 5, e.lastWait
	coverage[27] = unbound
	coverage[45], coverage[46] = s.batchError, s.decodedThrough
	for i, value := range coverage {
		binary.LittleEndian.PutUint64(out[3328+i*8:], value)
	}
	e.phases[e.snapshotCount] = phase
	e.snapshotCount++
	if phase == 3 {
		e.final = true
	}
}

func (s *ownerProbeEvidenceState) objectFact(o *objectIncarnation, slot int, seq, native, v0, v1 uint64) {
	s.objectFactAt(o, slot, seq, native, evidenceNow(), v0, v1)
}

func (s *ownerProbeEvidenceState) objectFactAt(o *objectIncarnation, slot int, seq, native, now, v0, v1 uint64) {
	if s == nil {
		return
	}
	d := s.objectFor(o)
	if d == nil {
		return
	}
	e := s.grantFor(d.grant)
	if e == nil || e.final {
		return
	}
	e.gap |= d.facts[slot].add(seq, native, now, v0, v1)
}

func (s *ownerProbeEvidenceState) objectGap(o *objectIncarnation, slot int) {
	if s == nil || slot < 0 || slot >= 32 {
		return
	}
	d := s.objectFor(o)
	if d == nil {
		return
	}
	e := s.grantFor(d.grant)
	if e == nil || e.final {
		return
	}
	d.facts[slot].state = 4
	e.gap |= settlementGapUnbound
}

func (s *ownerProbeEvidenceState) pipeline(r *guardRecord, stage int) {
	if s == nil {
		return
	}
	switch stage {
	case 25:
		s.transcriptThrough = r.Sequence
	case 27:
		s.lifecycleThrough = r.Sequence
	}
	if stage != 25 || s.broker == nil || s.broker.effects == nil || s.broker.effects.objects == nil || (r.Kind != 2 && r.Kind != 4) {
		return
	}
	o := s.objectForRecord(r)
	d := s.objectFor(o)
	if d == nil || d.id != r.KernelID || d.class == 1 && r.Kind != 2 || d.class == 2 && r.Kind != 4 {
		return
	}
	s.objectFact(o, 25, r.Sequence, guardU64(r.Detail[:], 0), r.Sequence, 0)
}

func (s *ownerProbeEvidenceState) lifecycleDecision(r *guardRecord, decision, reason uint64) {
	if s == nil || s.broker == nil || s.broker.effects == nil || s.broker.effects.objects == nil {
		return
	}
	s.objectFact(s.objectForRecord(r), 27, r.Sequence, 0, decision, reason)
}

func (s *ownerProbeEvidenceState) terminal(e *settlementEvidenceGrant, reason uint32, now uint64, request uint64) {
	if s == nil || e == nil || e.grant == nil || e.final || now == 0 {
		return
	}
	actualReason := reason
	cutoff := now
	if e.deadline != 0 && e.deadline <= cutoff {
		cutoff, reason = e.deadline, 1
	}
	switch actualReason {
	case 2:
		e.fact(8, 0, 0, now, request, 0)
	case 3:
		e.fact(9, 0, 0, now, uint64(actualReason), 0)
	case 4:
		e.fact(10, 0, 0, now, uint64(actualReason), 0)
	case 5:
		e.fact(11, 0, 0, now, uint64(actualReason), 0)
	}
	e.capture(s, 3, cutoff, reason)
}

func (s *ownerProbeEvidenceState) deadlines(now uint64) {
	if s == nil || now == 0 {
		return
	}
	for i := range s.grants {
		e := &s.grants[i]
		if e.grant != nil && !e.final && e.deadline != 0 && now >= e.deadline {
			s.terminal(e, 1, now, 0)
		}
	}
}

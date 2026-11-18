//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOwnerProbeSettlementEvidence(t *testing.T) {
	newFixture := func() (*ownerProbeEvidenceState, *authorityBroker, *brokerGrant) {
		g := &brokerGrant{id: 3, owner: 7, generation: 2, purpose: "postgres-uid", containerID: strings.Repeat("a", 64)}
		b := &authorityBroker{bootOwner: 7, bootID: "00000000-0000-0000-0000-000000000001", epoch: strings.Repeat("b", 32), grants: map[uint64]*brokerGrant{3: g}, effects: &runtimeEffects{objects: &objectTracker{byCookie: make(map[uint64]*objectIncarnation)}, pending: &runtimeEffectTransaction{grantID: 3}}}
		s := &ownerProbeEvidenceState{broker: b}
		b.diagnostic = s
		s.issue(b, g)
		return s, b, g
	}
	read := func(raw *[settlementEvidenceBytes]byte, offset int) uint64 {
		return binary.LittleEndian.Uint64(raw[offset:])
	}
	t.Run("early-removal-and-frozen-cutoff", func(t *testing.T) {
		s, b, g := newFixture()
		e := s.grantFor(g)
		e.root, e.cgroup, e.enrollment = 4, 5, 6
		now := evidenceNow()
		if now == 0 {
			t.Fatal("monotonic clock unavailable")
		}
		pub := guardRecord{Sequence: 10, Kind: 1, ObjectCookie: 11, KernelID: 12}
		removal := guardRecord{Sequence: 11, Kind: 2, ObjectCookie: 11, KernelID: 12}
		guardPut64(removal.Detail[:], 0, now)
		s.decoded(&pub)
		s.decoded(&removal)
		o := &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 10, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE}
		b.effects.objects.byCookie[11] = o
		s.publish(g, o, pub)
		if s.earlyUsed != 0 {
			t.Fatal("early records were not bound")
		}
		s.pipeline(&removal, 25)
		s.objectFact(o, 26, 11, 0, 1, 0)
		e.pin(s.objectFor(o))
		e.known, e.pending = 1<<9, 1<<9
		e.fact(5, 0, 0, now, 10, 0)
		e.fact(6, 0, 0, now, e.known, e.pending)
		e.capture(s, 2, 0, 0)
		if e.snapshotCount != 2 || read(&e.snapshots[1], 48) != 11 || read(&e.snapshots[1], 56) != 12 {
			t.Fatal("target identity missing from WAIT")
		}
		s.status(1, guardStatus{Sequence: 11, AckCursor: 11, ObserverAckCursor: 10, AuthorityPhase: 1, ModuleInstance: 9}, nil, now, now)
		s.status(3, guardStatus{}, unix.EAGAIN, now, now)
		e.deadline = now + 1000000000
		s.terminal(e, 2, evidenceNow(), 99)
		if !e.final || e.snapshotCount != 3 || binary.LittleEndian.Uint32(e.snapshots[2][248:]) != 2 {
			t.Fatal("missing ACK terminal")
		}
		if got := read(&e.snapshots[2], 256+24*80+16); got != now {
			t.Fatalf("native removal time = %d", got)
		}
		if got := read(&e.snapshots[2], 3328+32*8); got != 2 {
			t.Fatalf("EAGAIN attempt code = %d", got)
		}
		if got := read(&e.snapshots[2], 3328+3*8); got != 11 {
			t.Fatalf("cached success sequence = %d", got)
		}
		frozen := e.snapshots[2]
		s.decoded(&removal)
		s.terminal(e, 5, evidenceNow(), 0)
		if e.snapshotCount != 3 || frozen != e.snapshots[2] {
			t.Fatal("FINAL changed after capture")
		}
	})
	t.Run("release-before-close-and-deadline", func(t *testing.T) {
		s, b, g := newFixture()
		e := s.grantFor(g)
		e.root, e.cgroup, e.enrollment = 4, 5, 6
		now := evidenceNow()
		release := guardRecord{Sequence: 8, Kind: 6, RootCookie: 4, CgroupID: 5}
		guardPut64(release.Detail[:], 0, now)
		s.decoded(&release)
		o := &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 7, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE}
		b.effects.objects.byCookie[o.cookie] = o
		s.publish(g, o, guardRecord{Sequence: 7, Kind: 1, ObjectCookie: 11, KernelID: 12})
		s.objectFact(o, 22, 0, 0, 0, 0)
		e.deadline = now
		s.terminal(e, 2, now, 33)
		if binary.LittleEndian.Uint32(e.snapshots[1][248:]) != 1 || read(&e.snapshots[1], 120) != now {
			t.Fatal("deadline did not win tie")
		}
		if got := read(&e.snapshots[1], 256+3*80+16); got != now {
			t.Fatalf("release time = %d", got)
		}
		if got := read(&e.snapshots[1], 256+22*80+48); got != 1 {
			t.Fatalf("close result count = %d", got)
		}
	})
	t.Run("ack-observed-after-deadline", func(t *testing.T) {
		s, _, g := newFixture()
		e := s.grantFor(g)
		now := evidenceNow()
		if now < 2 {
			t.Fatal("monotonic clock unavailable")
		}
		e.deadline = now - 1
		s.terminal(e, 2, now, 19)
		final := &e.snapshots[1]
		if binary.LittleEndian.Uint32(final[248:]) != 1 || binary.LittleEndian.Uint64(final[120:]) != e.deadline {
			t.Fatal("deadline was not retained as cutoff")
		}
		if binary.LittleEndian.Uint32(final[256+8*80+76:])&8 == 0 {
			t.Fatal("late ACK was not marked post-cutoff")
		}
	})
	t.Run("incarnations-and-capacity", func(t *testing.T) {
		s, _, g := newFixture()
		e := s.grantFor(g)
		var first *objectIncarnation
		for i := range settlementEvidenceObjects {
			o := &objectIncarnation{cookie: uint64(i + 1), id: 17, ownerTransaction: uint64(i + 2), publicationSeq: uint64(i + 3), class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE}
			if i == 0 {
				first = o
			}
			s.publish(g, o, guardRecord{Kind: 1, Sequence: o.publicationSeq, ObjectCookie: o.cookie, KernelID: o.id})
		}
		extra := &objectIncarnation{cookie: 999, id: 17, ownerTransaction: 999, publicationSeq: 999, class: "program"}
		s.publish(g, extra, guardRecord{Kind: 1, Sequence: 999, ObjectCookie: 999, KernelID: 17})
		if s.used != settlementEvidenceObjects || e.gap&settlementGapCapacity == 0 || extra.diagnosticIndex != 0 {
			t.Fatal("capacity overflow was not contained")
		}
		clone := *first
		if s.objectFor(&clone) != nil {
			t.Fatal("reused identity was joined to another incarnation")
		}
	})
	t.Run("early-overflow-and-unrelated-history", func(t *testing.T) {
		s, _, g := newFixture()
		e := s.grantFor(g)
		for i := range 5000 {
			r := guardRecord{Sequence: uint64(i + 1), Kind: 7}
			s.decoded(&r)
		}
		if e.gap&settlementGapCapacity != 0 || s.earlyUsed != 0 {
			t.Fatal("unrelated records consumed diagnostic capacity")
		}
		for i := range settlementEvidenceEarly + 1 {
			r := guardRecord{Sequence: uint64(5001 + i), Kind: 1, ObjectCookie: uint64(i + 1), KernelID: uint64(i + 1)}
			s.decoded(&r)
		}
		if s.earlyUsed != settlementEvidenceEarly || e.gap&settlementGapCapacity == 0 {
			t.Fatal("early overflow was not retained")
		}
	})
	t.Run("early-cgroup-association", func(t *testing.T) {
		s, b, g := newFixture()
		b.effects.enrollmentPending = map[uint64]int{g.id: 4}
		r := guardRecord{Sequence: 12, Kind: 5, RootCookie: 20, CgroupID: 21}
		guardPut64(r.Detail[:], 0, 50)
		s.decoded(&r)
		if s.earlyUsed != 1 {
			t.Fatal("early cgroup event was lost")
		}
		s.enroll(g, &liveCgroup{GrantID: g.id, RootCookie: 20, KernfsID: 21, Enrollment: 22})
		e := s.grantFor(g)
		if s.earlyUsed != 0 || e.facts[2].firstNative != 50 || e.facts[1].value0 != 22 {
			t.Fatal("early event was not bound to its enrollment")
		}
	})
	t.Run("loss-and-partial-pipeline", func(t *testing.T) {
		s, b, g := newFixture()
		e := s.grantFor(g)
		o := &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 7, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE}
		b.effects.objects.byCookie[o.cookie] = o
		s.publish(g, o, guardRecord{Sequence: 7, Kind: 1, ObjectCookie: 11, KernelID: 12})
		r := guardRecord{Sequence: 8, Kind: 2, ObjectCookie: 11, KernelID: 12}
		guardPut64(r.Detail[:], 0, evidenceNow())
		s.decoded(&r)
		s.status(1, guardStatus{Sequence: 8, Lost: 1, ProbeMissed: 1, Sticky: 1, AuthorityPhase: 2}, nil, evidenceNow(), evidenceNow())
		s.terminal(e, 5, evidenceNow(), 0)
		final := &e.snapshots[1]
		if read(final, 256+24*80+48) != 1 || read(final, 256+25*80+48) != 0 {
			t.Fatal("decoded record was confused with transcript delivery")
		}
		if binary.LittleEndian.Uint32(final[12:])&settlementGapLoss == 0 {
			t.Fatal("loss did not disqualify coverage")
		}
	})
	t.Run("partial-batch-decode", func(t *testing.T) {
		s, b, g := newFixture()
		o := &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 7, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE}
		b.effects.objects.byCookie[o.cookie] = o
		s.publish(g, o, guardRecord{Sequence: 7, Kind: 1, ObjectCookie: 11, KernelID: 12})
		wire := guardMessage(guardEventsSize)
		guardPut32(wire, 8, 2)
		guardPut64(wire, 16, 7)
		guardPut64(wire, 24, 7)
		guardPut64(wire, 32, 8)
		guardPut64(wire, 40, 9)
		guardPut32(wire, 48, 2)
		guardPut64(wire, 56, 8)
		guardPut32(wire, 64, 2)
		guardPut64(wire, 72, 11)
		guardPut64(wire, 80, 12)
		guardPut64(wire, 144, 123)
		guardPut64(wire, 56+guardRecordSize, 9)
		guardPut32(wire, 64+guardRecordSize, 2)
		device := guardDevice{fd: -1, diagnostic: s}
		if _, err := device.decodeEvents(wire, 2, 7, 7); err == nil {
			t.Fatal("accepted malformed second record")
		}
		if s.decodedThrough != 8 || s.batchError != 2 || s.ringThrough != 0 || s.transcriptThrough != 0 || s.trackerThrough != 0 || s.lifecycleThrough != 0 {
			t.Fatal("partial batch advanced a downstream watermark")
		}
		f := s.objectFor(o).facts[24]
		if f.count != 1 || f.firstSeq != 8 || f.firstNative != 123 {
			t.Fatal("successfully decoded prefix was lost")
		}
	})
	t.Run("tracker-lifecycle-and-cleanup", func(t *testing.T) {
		s, b, g := newFixture()
		b.transcript = newAuthorityTranscript(b.bootID, b.epoch)
		b.resolver = &brokerAuditResolver{}
		b.checkpoint = &checkpointController{lifecycle: &checkpointLifecycle{objects: map[uint64]*deferredObject{11: {kernelID: 12, class: "program", removalAllowed: true}}, diagnostic: s}}
		b.effects.objects.transcript = b.transcript
		b.effects.grantCleanup = map[uint64]*grantObjectCleanup{11: {grantID: g.id, creatorID: 13, cookie: 11}}
		o := &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 7, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE, removalAuthorized: true, state: "live"}
		b.effects.objects.byCookie[11] = o
		s.publish(g, o, guardRecord{Sequence: 7, Kind: 1, ObjectCookie: 11, KernelID: 12})
		r := guardRecord{Sequence: 8, Kind: 2, ObjectCookie: 11, KernelID: 12}
		guardPut64(r.Detail[:], 0, evidenceNow())
		s.decoded(&r)
		s.pipeline(&r, 25)
		if err := b.observeRuntimeGuard(r); err != nil {
			t.Fatalf("actual removal rejected: %v", err)
		}
		d := s.objectFor(o)
		for _, slot := range []int{24, 25, 26, 27, 28, 29} {
			if d.facts[slot].count != 1 {
				t.Fatalf("fact %d missing", slot)
			}
		}
		if d.facts[26].value0 != 1 || d.facts[27].value0 != 1 || d.facts[28].value0 != 2 || d.facts[29].value0 != 8 {
			t.Fatal("inconsistent accepted removal")
		}
	})
	t.Run("cached-prefix-with-uncertified-tail", func(t *testing.T) {
		s, b, g := newFixture()
		e := s.grantFor(g)
		o := &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 7, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE}
		b.effects.objects.byCookie[o.cookie] = o
		s.publish(g, o, guardRecord{Sequence: 7, Kind: 1, ObjectCookie: 11, KernelID: 12})
		now := evidenceNow()
		s.status(1, guardStatus{Sequence: 7, AckCursor: 7, ObserverAckCursor: 7, AuthorityPhase: 1}, nil, now, now)
		s.ringThrough, s.transcriptThrough, s.trackerThrough, s.lifecycleThrough = 7, 7, 7, 7
		s.terminal(e, 2, evidenceNow(), 1)
		final := &e.snapshots[1]
		if read(final, 3328+21*8) != 15 {
			t.Fatal("complete sampled prefix was not retained")
		}
		if binary.LittleEndian.Uint32(final[12:])&settlementGapTail == 0 {
			t.Fatal("tail was incorrectly certified")
		}
	})
	t.Run("count-saturation", func(t *testing.T) {
		f := settlementEvidenceFact{count: ^uint64(0) - 1, state: 1}
		if gap := f.add(2, 0, 1, 0, 0); gap != settlementGapSaturated || f.count != ^uint64(0) || f.state != 4 || f.flags&4 == 0 {
			t.Fatal("saturation was not reported at the boundary")
		}
		f.add(3, 0, 2, 0, 0)
		if f.count != ^uint64(0) {
			t.Fatal("saturated count wrapped")
		}
	})
	t.Run("duplicate-native-time-conflict", func(t *testing.T) {
		var f settlementEvidenceFact
		if f.add(8, 30, 40, 8, 0) != 0 {
			t.Fatal("first observation was rejected")
		}
		if gap := f.add(8, 20, 41, 8, 0); gap != settlementGapContradiction || f.count != 2 || f.state != 4 || f.flags&3 != 3 || f.firstNative != 30 || f.lastNative != 0 {
			t.Fatal("contradictory timestamp was not retained as a gap")
		}
	})
	t.Run("tracker-rejections", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			reason uint64
			change func(*objectIncarnation, *guardRecord)
		}{
			{"unauthorized", 2, func(o *objectIncarnation, _ *guardRecord) { o.removalAuthorized = false }},
			{"already-removed", 8, func(o *objectIncarnation, _ *guardRecord) { o.removalSeq = 8 }},
			{"wrong-id", 4, func(o *objectIncarnation, _ *guardRecord) { o.id = 13 }},
			{"sequence", 6, func(_ *objectIncarnation, r *guardRecord) { r.Sequence = 7 }},
			{"class", 5, func(o *objectIncarnation, _ *guardRecord) { o.class = "map" }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s, b, g := newFixture()
				b.resolver = &brokerAuditResolver{}
				b.checkpoint = &checkpointController{lifecycle: &checkpointLifecycle{objects: map[uint64]*deferredObject{11: {kernelID: 12, class: "program", removalAllowed: true}}}}
				b.effects.objects.transcript = newAuthorityTranscript(b.bootID, b.epoch)
				o := &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 7, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE, removalAuthorized: true}
				r := guardRecord{Sequence: 8, Kind: 2, ObjectCookie: 11, KernelID: 12}
				guardPut64(r.Detail[:], 0, 1)
				tc.change(o, &r)
				b.effects.objects.byCookie[11] = o
				s.publish(g, o, guardRecord{Sequence: 7, Kind: 1, ObjectCookie: 11, KernelID: o.id})
				if err := b.observeRuntimeGuard(r); err == nil {
					t.Fatal("tracker accepted invalid removal")
				}
				fact := s.objectFor(o).facts[26]
				if fact.value0 != 2 || fact.value1 != tc.reason {
					t.Fatalf("decision=%d reason=%d", fact.value0, fact.value1)
				}
			})
		}
	})
	t.Run("tracker-other-error", func(t *testing.T) {
		s, b, g := newFixture()
		b.resolver = &brokerAuditResolver{}
		b.checkpoint = &checkpointController{lifecycle: &checkpointLifecycle{objects: map[uint64]*deferredObject{11: {kernelID: 12, class: "program", removalAllowed: true}}}}
		o := &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 7, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE, removalAuthorized: true}
		b.effects.objects.byCookie[11] = o
		s.publish(g, o, guardRecord{Sequence: 7, Kind: 1, ObjectCookie: 11, KernelID: 12})
		r := guardRecord{Sequence: 8, Kind: 2, ObjectCookie: 11, KernelID: 12}
		guardPut64(r.Detail[:], 0, 1)
		if err := b.observeRuntimeGuard(r); err == nil {
			t.Fatal("tracker accepted missing transcript")
		}
		f := s.objectFor(o).facts[26]
		if f.value0 != 2 || f.value1 != 9 {
			t.Fatal("opaque tracker error was misclassified")
		}
	})
	t.Run("unpublished-and-cookie-mismatch", func(t *testing.T) {
		s, b, g := newFixture()
		b.resolver = &brokerAuditResolver{}
		b.checkpoint = &checkpointController{lifecycle: &checkpointLifecycle{objects: make(map[uint64]*deferredObject), diagnostic: s}}
		o := &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 7, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE}
		b.effects.objects.byCookie[11] = o
		s.publish(g, o, guardRecord{Sequence: 7, Kind: 1, ObjectCookie: 11, KernelID: 12})
		for _, cookie := range []uint64{77, 99} {
			if cookie == 99 {
				b.effects.objects.byCookie[cookie] = o
			}
			r := guardRecord{Sequence: 8, Kind: 2, ObjectCookie: cookie, KernelID: 12}
			guardPut64(r.Detail[:], 0, 1)
			s.decoded(&r)
			if err := b.observeRuntimeGuard(r); err == nil {
				t.Fatal("invalid identity was accepted")
			}
		}
		d := s.objectFor(o)
		if d.facts[24].count != 0 || d.facts[26].count != 0 || d.facts[27].count != 0 || d.facts[30].count != 0 {
			t.Fatal("untrusted cookie was attributed to a grant")
		}
		if s.grantFor(g).gap&settlementGapContradiction == 0 {
			t.Fatal("contradictory tracker route was not retained")
		}
	})
	t.Run("pre-tracker-not-evaluated", func(t *testing.T) {
		s, b, g := newFixture()
		b.resolver = &brokerAuditResolver{}
		b.checkpoint = &checkpointController{lifecycle: &checkpointLifecycle{objects: map[uint64]*deferredObject{11: {kernelID: 12, class: "program"}}, diagnostic: s}}
		o := &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 7, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE}
		b.effects.objects.byCookie[11] = o
		s.publish(g, o, guardRecord{Sequence: 7, Kind: 1, ObjectCookie: 11, KernelID: 12})
		r := guardRecord{Sequence: 8, Kind: 2, ObjectCookie: 11, KernelID: 12}
		guardPut64(r.Detail[:], 0, 1)
		s.decoded(&r)
		if err := b.observeRuntimeGuard(r); err == nil {
			t.Fatal("unauthorized removal was accepted")
		}
		d := s.objectFor(o)
		if d.facts[24].count != 1 || d.facts[26].count != 0 || d.facts[27].count != 0 || d.facts[30].value0 != 2 {
			t.Fatal("precheck was misreported as a tracker decision")
		}
	})
	t.Run("lifecycle-decisions", func(t *testing.T) {
		for _, tc := range []struct {
			name             string
			item             *deferredObject
			decision, reason uint64
		}{
			{"accepted", &deferredObject{kernelID: 12, class: "program", removalAllowed: true}, 1, 0},
			{"authority", nil, 2, 7},
			{"unauthorized", &deferredObject{kernelID: 12, class: "program"}, 2, 2},
			{"already-removed", &deferredObject{kernelID: 12, class: "program", removalAllowed: true, removed: true}, 2, 8},
			{"wrong-id", &deferredObject{kernelID: 13, class: "program", removalAllowed: true}, 2, 4},
			{"wrong-class", &deferredObject{kernelID: 12, class: "map", removalAllowed: true}, 2, 5},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s, b, g := newFixture()
				o := &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 7, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE}
				b.effects.objects.byCookie[11] = o
				s.publish(g, o, guardRecord{Sequence: 7, Kind: 1, ObjectCookie: 11, KernelID: 12})
				c := &checkpointLifecycle{paused: true, objects: map[uint64]*deferredObject{11: tc.item}, diagnostic: s}
				r := guardRecord{Sequence: 8, Kind: 2, ObjectCookie: 11, KernelID: 12}
				guardPut64(r.Detail[:], 0, 1)
				err := c.observe(r)
				if (err == nil) != (tc.decision == 1) {
					t.Fatalf("unexpected decision: %v", err)
				}
				f := s.objectFor(o).facts[27]
				if f.count != 1 || f.value0 != tc.decision || f.value1 != tc.reason {
					t.Fatalf("decision=%d reason=%d", f.value0, f.value1)
				}
			})
		}
	})
	t.Run("actual-wait-mask", func(t *testing.T) {
		s, _, g := newFixture()
		q := &pendingBrokerSettlement{grant: g, evidence: s.grantFor(g), evidenceState: s}
		q.evaluated(1, false)
		q.evaluated(2, true)
		if q.evidence.known != 3 || q.evidence.pending != 2 {
			t.Fatalf("mask known=%x pending=%x", q.evidence.known, q.evidence.pending)
		}
	})
	t.Run("ack-commit-and-send", func(t *testing.T) {
		b := controlDiagnosticFixture()
		b.bootOwner = 1
		g := &brokerGrant{id: 2, owner: 1, generation: 1, purpose: "postgres-uid", containerID: strings.Repeat("a", 64), phase: "retired", outcome: "completed", settlementRequests: 1}
		p := &controlPrincipal{ID: 1, Role: "BOOT_OWNER", NextRequest: 1}
		b.grants = map[uint64]*brokerGrant{g.id: g}
		s := &ownerProbeEvidenceState{broker: b}
		b.diagnostic = s
		s.issue(b, g)
		q := &pendingBrokerSettlement{principal: p, grant: g, requestID: 1, raw: []byte("request"), evidence: s.grantFor(g), evidenceState: s}
		b.pendingSettlement = q
		if _, _, done, err := b.finishBrokerSettlement(q, "retired"); err != nil || !done {
			t.Fatalf("commit failed: %v", err)
		}
		if q.evidence.facts[7].count != 1 || q.evidence.facts[8].count != 0 || q.evidence.final {
			t.Fatal("ACK commit was confused with send")
		}
		s.terminal(q.evidence, 2, evidenceNow(), q.requestID)
		if !q.evidence.final || q.evidence.facts[8].count != 1 || q.evidence.facts[8].value0 != 1 {
			t.Fatal("ACK send was not captured")
		}
	})
	t.Run("credentialled-hello-and-bind", func(t *testing.T) {
		rootTestPinProc(t)
		pid := uint32(os.Getpid())
		pidfd, err := unix.PidfdOpen(int(pid), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pidfd)
		process, err := observeProcess(pid, pidfd)
		if err != nil {
			t.Fatal(err)
		}
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pair[1])
		if err = unix.SetsockoptInt(pair[0], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
			t.Fatal(err)
		}
		p := &controlPrincipal{ID: 7, Role: "BOOT_OWNER", PID: pid, UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), PIDFD: pidfd, StartNS: process.StartTicks * 10000000}
		b := &authorityBroker{bootOwner: 7, principals: map[uint64]*controlPrincipal{7: p}}
		s := &ownerProbeEvidenceState{broker: b}
		tr := &settlementEvidenceTransport{listener: -1, parent: -1, dir: -1, socket: -1}
		tr.connections[0].fd, tr.connections[1].fd = pair[0], -1
		defer func() {
			if tr.connections[0].fd >= 0 {
				_ = unix.Close(tr.connections[0].fd)
			}
		}()
		nonce := strings.Repeat("1", 32)
		hello, err := canonicalControl(map[string]any{"version": json.Number("1"), "purpose": "postgres-uid", "nonce": nonce, "principal_id": json.Number("7")})
		if err != nil {
			t.Fatal(err)
		}
		wrongPeers := []*unix.Ucred{{Pid: int32(pid) + 1, Uid: p.UID, Gid: p.GID}, {Pid: int32(pid), Uid: p.UID + 1, Gid: p.GID}, {Pid: int32(pid), Uid: p.UID, Gid: p.GID + 1}}
		for _, wrong := range wrongPeers {
			if tr.input(b, s, &tr.connections[0], hello, wrong) || tr.connections[0].hello {
				t.Fatal("accepted another peer")
			}
		}
		if sendControl(pair[1], hello, nil) != nil {
			t.Fatal("sending HELLO")
		}
		tr.service(b, s)
		var response [128]byte
		n, _, err := unix.Recvfrom(pair[1], response[:], 0)
		if err != nil || n == 0 || !tr.connections[0].hello {
			t.Fatalf("HELLO was not armed: %v", err)
		}
		bind, err := canonicalControl(map[string]any{"version": json.Number("1"), "purpose": "postgres-uid", "nonce": nonce, "principal_id": json.Number("7"), "container_id": strings.Repeat("a", 64), "operation_start_ns": json.Number("1"), "operation_deadline_ns": json.Number("2"), "authority_deadline_ns": json.Number("3")})
		badBind, badErr := canonicalControl(map[string]any{"version": json.Number("1"), "purpose": "postgres-uid", "nonce": strings.Repeat("2", 32), "principal_id": json.Number("7"), "container_id": strings.Repeat("a", 64), "operation_start_ns": json.Number("1"), "operation_deadline_ns": json.Number("2"), "authority_deadline_ns": json.Number("3")})
		if badErr != nil || tr.input(b, s, &tr.connections[0], badBind, &unix.Ucred{Pid: int32(pid), Uid: p.UID, Gid: p.GID}) {
			t.Fatal("accepted a mismatched BIND nonce")
		}
		if err != nil || sendControl(pair[1], bind, nil) != nil {
			t.Fatal("sending BIND")
		}
		tr.service(b, s)
		if !tr.connections[0].bound {
			t.Fatal("BIND not accepted")
		}
		if sendControl(pair[1], bind, nil) != nil {
			t.Fatal("sending duplicate")
		}
		tr.service(b, s)
		if tr.connections[0].fd != -1 {
			t.Fatal("accepted third input")
		}
	})
	t.Run("rights-and-truncation", func(t *testing.T) {
		for _, rightsCount := range []int{0, 1, 64} {
			pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err = unix.SetsockoptInt(pair[0], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
				t.Fatal(err)
			}
			var data, control []byte
			if rightsCount != 0 {
				fd, openErr := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
				if openErr != nil {
					t.Fatal(openErr)
				}
				rights := make([]int, rightsCount)
				for i := range rights {
					rights[i] = fd
				}
				control = unix.UnixRights(rights...)
				data = []byte("{}")
				if _, err = unix.SendmsgN(pair[1], data, control, nil, 0); err != nil {
					t.Fatal(err)
				}
				_ = unix.Close(fd)
			} else {
				data = make([]byte, 1024)
				if _, err = unix.SendmsgN(pair[1], data, nil, nil, 0); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err = receiveSettlementEvidenceFrame(pair[0]); err == nil {
				t.Fatal("accepted forbidden frame")
			}
			_ = unix.Close(pair[0])
			_ = unix.Close(pair[1])
		}
	})
}

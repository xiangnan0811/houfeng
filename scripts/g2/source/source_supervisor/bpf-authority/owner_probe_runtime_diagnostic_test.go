//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOwnerProbeSettlementDiagnosticPolicy(t *testing.T) {
	pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(pidfd) })
	newFixture := func() (*authorityBroker, *controlPrincipal, *brokerGrant) {
		b := controlDiagnosticFixture()
		b.bootOwner = 1
		b.effects = &runtimeEffects{}
		p := &controlPrincipal{ID: 1, Role: "BOOT_OWNER", PIDFD: pidfd, NextRequest: 1}
		g := &brokerGrant{id: 2, owner: 1, generation: 1, purpose: "postgres-uid", containerID: strings.Repeat("a", 64), phase: "active", createSettled: true, actualQueryConfirmed: true, startObserved: true}
		b.grants = map[uint64]*brokerGrant{g.id: g}
		return b, p, g
	}
	t.Run("published-and-committed", func(t *testing.T) {
		b, p, g := newFixture()
		request := map[string]any{"request_id": json.Number("1"), "body": map[string]any{"grant_id": json.Number("2"), "outcome": "completed"}}
		raw := []byte(`{"request_id":1}`)
		if err := b.startBrokerSettlement(p, request, raw); err != nil {
			t.Fatal(err)
		}
		q := b.pendingSettlement
		if q == nil || q.principal != p || q.grant != g || q.requestID != 1 || !reflect.DeepEqual(q.raw, raw) || g.phase != "settling" || g.settlementRequests != 1 || !q.diagnostic.valid || q.diagnostic.purpose != 1 || q.diagnosticEvents != 1<<1 {
			t.Fatalf("unexpected published state: %+v grant=%+v", q, g)
		}
		g.phase = "retired"
		principal, ack, done, err := b.finishBrokerSettlement(q, "retired")
		if err != nil || !done || principal != p || len(ack) == 0 || b.pendingSettlement != nil || p.NextRequest != 2 || !reflect.DeepEqual(p.Replay[1].Ack, ack) || q.diagnosticEvents != (1<<1|1<<3) {
			t.Fatalf("commit: done=%v err=%v q=%+v", done, err, q)
		}
	})
	t.Run("settling-and-failed-commit", func(t *testing.T) {
		b, p, g := newFixture()
		g.phase, g.outcome, g.settlementRequests = "settling", "completed", 1
		q := &pendingBrokerSettlement{principal: p, grant: g, requestID: 1, raw: []byte("request"), firstRecovery: true, diagnostic: ownerProbeSettlementReceipt{cid: g.containerID, purpose: 1, valid: true}}
		b.pendingSettlement = q
		if _, _, done, err := b.finishBrokerSettlement(q, "settling"); err != nil || !done || q.diagnosticEvents != 0 {
			t.Fatalf("recovery finish: done=%v err=%v", done, err)
		}
		b, p, g = newFixture()
		g.phase, g.outcome, g.settlementRequests = "retired", "completed", 1
		q = &pendingBrokerSettlement{principal: p, grant: g, requestID: 2, raw: []byte("request"), diagnostic: ownerProbeSettlementReceipt{cid: g.containerID, purpose: 1, valid: true}}
		b.pendingSettlement = q
		if _, _, done, err := b.finishBrokerSettlement(q, "retired"); err == nil || done || q.diagnosticEvents != 0 || b.pendingSettlement != q {
			t.Fatalf("failed commit: done=%v err=%v", done, err)
		}
	})
	t.Run("followup-after-commit-only", func(t *testing.T) {
		b, p, g := newFixture()
		g.phase, g.outcome, g.settlementRequests = "retired", "completed", 1
		q := &pendingBrokerSettlement{principal: p, grant: g, requestID: 1, raw: []byte("request"), diagnostic: ownerProbeSettlementReceipt{cid: g.containerID, purpose: 1, valid: true}, followupGrant: g, followupObject: &objectIncarnation{cookie: 13}, lastWait: 10}
		b.pendingSettlement = q
		if _, _, done, err := b.finishBrokerSettlement(q, "retired"); err != nil || !done || b.pendingSettlement != nil || q.diagnosticEvents != 1<<3|1<<5 {
			t.Fatalf("committed followup done=%v err=%v events=%d", done, err, q.diagnosticEvents)
		}
		b, p, g = newFixture()
		g.phase, g.outcome, g.settlementRequests = "retired", "completed", 1
		q = &pendingBrokerSettlement{principal: p, grant: g, requestID: 2, raw: []byte("request"), diagnostic: ownerProbeSettlementReceipt{cid: g.containerID, purpose: 1, valid: true}, followupGrant: g, followupObject: &objectIncarnation{cookie: 13}}
		b.pendingSettlement = q
		if _, _, done, err := b.finishBrokerSettlement(q, "retired"); err == nil || done || q.diagnosticEvents&(1<<5) != 0 || b.pendingSettlement != q {
			t.Fatalf("failed commit reported followup: %v", err)
		}
	})
	for _, tc := range []struct {
		name   string
		change func(*authorityBroker, *pendingBrokerSettlement)
	}{
		{"owner", func(b *authorityBroker, _ *pendingBrokerSettlement) { b.bootOwner = 3 }},
		{"role", func(_ *authorityBroker, q *pendingBrokerSettlement) { q.principal.Role = "REALM_ROOT" }},
		{"grant-owner", func(_ *authorityBroker, q *pendingBrokerSettlement) { q.grant.owner = 3 }},
		{"grant-alias", func(b *authorityBroker, q *pendingBrokerSettlement) { clone := *q.grant; b.grants[q.grant.id] = &clone }},
		{"purpose", func(_ *authorityBroker, q *pendingBrokerSettlement) { q.grant.purpose = "postgres-init-x" }},
		{"outcome", func(_ *authorityBroker, q *pendingBrokerSettlement) { q.grant.outcome = "failed" }},
		{"repeat", func(_ *authorityBroker, q *pendingBrokerSettlement) { q.grant.settlementRequests = 2 }},
		{"recovery", func(_ *authorityBroker, q *pendingBrokerSettlement) { q.firstRecovery = true }},
		{"cid", func(_ *authorityBroker, q *pendingBrokerSettlement) { q.grant.containerID = "bad" }},
		{"nil-principal", func(_ *authorityBroker, q *pendingBrokerSettlement) { q.principal = nil }},
		{"nil-grant", func(_ *authorityBroker, q *pendingBrokerSettlement) { q.grant = nil }},
	} {
		t.Run("eligibility-"+tc.name, func(t *testing.T) {
			b, p, g := newFixture()
			g.outcome, g.settlementRequests = "completed", 1
			q := &pendingBrokerSettlement{principal: p, grant: g}
			if !b.settlementDiagnosticEligible(q) {
				t.Fatal("fixture not eligible")
			}
			tc.change(b, q)
			if b.settlementDiagnosticEligible(q) {
				t.Fatal("ineligible scope accepted")
			}
		})
	}
	for point := byte(1); point <= 13; point++ {
		t.Run(fmt.Sprintf("sample-%d", point), func(t *testing.T) {
			q := &pendingBrokerSettlement{diagnostic: ownerProbeSettlementReceipt{cid: strings.Repeat("a", 64), purpose: 1, valid: true}}
			q.sample(0, point)
			q.sample(999_999_999, point)
			if !q.sampleArmed || q.sampleBaseNS != 0 || q.diagnosticEvents != 0 {
				t.Fatal("early sample")
			}
			q.sample(1_000_000_000, point)
			q.sample(2_000_000_000, point)
			if q.diagnosticEvents != 1<<2 {
				t.Fatal("sample not reserved exactly once")
			}
		})
	}
	t.Run("rollback-and-invalid", func(t *testing.T) {
		q := &pendingBrokerSettlement{diagnostic: ownerProbeSettlementReceipt{cid: strings.Repeat("a", 64), purpose: 1, valid: true}}
		q.sample(10, 0)
		q.observe(1, 1, ownerProbeRemovalDiagnostic{})
		if q.sampleArmed || q.diagnosticEvents != 0 {
			t.Fatal("invalid observation recorded")
		}
		q.sample(10, 1)
		q.sample(9, 1)
		if q.diagnosticEvents != 0 || q.sampleBaseNS != 10 {
			t.Fatal("clock rollback changed sample")
		}
	})
	t.Run("control-socket-outcomes", func(t *testing.T) {
		pair, e := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
		if e != nil {
			t.Fatal(e)
		}
		defer unix.Close(pair[0])
		defer func() {
			if pair[1] >= 0 {
				_ = unix.Close(pair[1])
			}
		}()
		if e = sendControl(pair[0], []byte("ack"), nil); e != nil {
			t.Fatal(e)
		}
		var payload [16]byte
		n, e := unix.Read(pair[1], payload[:])
		if e != nil || string(payload[:n]) != "ack" {
			t.Fatalf("ack read: %v", e)
		}
		if e = unix.SetsockoptInt(pair[0], unix.SOL_SOCKET, unix.SO_SNDBUF, 4096); e != nil {
			t.Fatal(e)
		}
		filled := false
		for range 4096 {
			if e = sendControl(pair[0], []byte("x"), nil); errors.Is(e, unix.EAGAIN) {
				filled = true
				break
			} else if e != nil {
				t.Fatal(e)
			}
		}
		if !filled {
			t.Fatal("socket did not reach EAGAIN")
		}
		if e = unix.Close(pair[1]); e != nil {
			t.Fatal(e)
		}
		pair[1] = -1
		if e = sendControl(pair[0], []byte("ack"), nil); !errors.Is(e, unix.EPIPE) && !errors.Is(e, unix.ECONNRESET) {
			t.Fatalf("closed peer: %v", e)
		}
	})
}

func TestOwnerProbeRemovalDiagnostic(t *testing.T) {
	file, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	fd := int(file.Fd())
	var beforeStat unix.Stat_t
	if err := unix.Fstat(fd, &beforeStat); err != nil {
		t.Fatal(err)
	}
	beforeFD, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	beforeFL, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		b       *authorityBroker
		g       *brokerGrant
		o       *objectIncarnation
		tx      *runtimeEffectTransaction
		cleanup *grantObjectCleanup
		item    *deferredObject
	}
	newFixture := func() fixture {
		g := &brokerGrant{id: 9}
		o := &objectIncarnation{cookie: 13, id: 17, ownerTransaction: 5, class: "map"}
		tx := &runtimeEffectTransaction{id: 5, grantID: 9, state: "retired", publication: &guardRecord{ObjectCookie: 13}}
		cleanup := &grantObjectCleanup{cookie: 13, grantID: 9, creatorID: 5}
		item := &deferredObject{kernelID: 17, class: "map"}
		b := &authorityBroker{grants: map[uint64]*brokerGrant{9: g}, checkpoint: &checkpointController{lifecycle: &checkpointLifecycle{objects: map[uint64]*deferredObject{13: item}}}, effects: &runtimeEffects{
			objects: &objectTracker{byCookie: map[uint64]*objectIncarnation{13: o}}, objectGrant: map[uint64]uint64{13: 9},
			objectFD: map[uint64]int{13: fd}, grantCleanup: map[uint64]*grantObjectCleanup{13: cleanup}, transactions: map[uint64]*runtimeEffectTransaction{5: tx},
		}}
		return fixture{b, g, o, tx, cleanup, item}
	}
	base := ownerProbeRemovalDiagnostic{2, 2, 2, 8, 2, 4, 2}
	cases := []struct {
		name   string
		change func(*fixture)
		want   ownerProbeRemovalDiagnostic
	}{
		{"base", func(*fixture) {}, base},
		{"missing-object", func(f *fixture) { f.o = nil; delete(f.b.effects.objects.byCookie, 13) }, ownerProbeRemovalDiagnostic{1, 2, 2, 1, 1, 2, 1}},
		{"object-identity", func(f *fixture) { f.o.cookie = 14 }, ownerProbeRemovalDiagnostic{5, 2, 2, 1, 1, 2, 1}},
		{"program", func(f *fixture) { f.o.class = "program"; f.item.class = "program" }, ownerProbeRemovalDiagnostic{3, 2, 2, 8, 2, 4, 2}},
		{"other-class", func(f *fixture) { f.o.class = "other"; f.item.class = "other" }, ownerProbeRemovalDiagnostic{4, 2, 2, 8, 2, 4, 2}},
		{"fd-absent", func(f *fixture) { delete(f.b.effects.objectFD, 13) }, ownerProbeRemovalDiagnostic{2, 1, 2, 8, 2, 4, 2}},
		{"fd-invalid", func(f *fixture) { f.b.effects.objectFD[13] = -1 }, ownerProbeRemovalDiagnostic{2, 3, 2, 8, 2, 4, 2}},
		{"cleanup-absent", func(f *fixture) { delete(f.b.effects.grantCleanup, 13) }, ownerProbeRemovalDiagnostic{2, 2, 1, 8, 2, 4, 2}},
		{"cleanup-zero-removed", func(f *fixture) { f.cleanup.removalSequence = 1 }, ownerProbeRemovalDiagnostic{2, 2, 3, 8, 2, 4, 2}},
		{"cleanup-negative", func(f *fixture) { f.cleanup.closeResult = -9 }, ownerProbeRemovalDiagnostic{2, 2, 4, 8, 2, 4, 2}},
		{"cleanup-negative-removed", func(f *fixture) { f.cleanup.closeResult = -9; f.cleanup.removalSequence = 1 }, ownerProbeRemovalDiagnostic{2, 2, 5, 8, 2, 4, 2}},
		{"cleanup-positive", func(f *fixture) { f.cleanup.closeResult = 1 }, ownerProbeRemovalDiagnostic{2, 2, 6, 8, 2, 4, 2}},
		{"cleanup-cookie", func(f *fixture) { f.cleanup.cookie = 14 }, ownerProbeRemovalDiagnostic{2, 2, 7, 8, 2, 4, 2}},
		{"cleanup-grant", func(f *fixture) { f.cleanup.grantID = 10 }, ownerProbeRemovalDiagnostic{2, 2, 7, 8, 2, 4, 2}},
		{"cleanup-creator", func(f *fixture) { f.cleanup.creatorID = 6 }, ownerProbeRemovalDiagnostic{2, 2, 7, 8, 2, 4, 2}},
		{"creator-unavailable", func(f *fixture) { f.o.ownerTransaction = 0; f.cleanup.creatorID = 0 }, ownerProbeRemovalDiagnostic{2, 2, 2, 1, 1, 4, 2}},
		{"creator-missing", func(f *fixture) { delete(f.b.effects.transactions, 5) }, ownerProbeRemovalDiagnostic{2, 2, 2, 2, 1, 4, 2}},
		{"creator-id", func(f *fixture) { f.tx.id = 6 }, ownerProbeRemovalDiagnostic{2, 2, 2, 3, 1, 4, 2}},
		{"creator-grant", func(f *fixture) { f.tx.grantID = 10 }, ownerProbeRemovalDiagnostic{2, 2, 2, 3, 1, 4, 2}},
		{"creator-publication", func(f *fixture) { f.tx.publication.ObjectCookie = 14 }, ownerProbeRemovalDiagnostic{2, 2, 2, 3, 1, 4, 2}},
		{"creator-no-publication", func(f *fixture) { f.tx.publication = nil }, ownerProbeRemovalDiagnostic{2, 2, 2, 3, 1, 4, 2}},
		{"prepared", func(f *fixture) { f.tx.state = "prepared" }, ownerProbeRemovalDiagnostic{2, 2, 2, 4, 2, 4, 2}},
		{"committed", func(f *fixture) { f.tx.state = "committed" }, ownerProbeRemovalDiagnostic{2, 2, 2, 5, 2, 4, 2}},
		{"effect-observed", func(f *fixture) { f.tx.state = "effect_observed" }, ownerProbeRemovalDiagnostic{2, 2, 2, 6, 2, 4, 2}},
		{"response-settled", func(f *fixture) { f.tx.state = "response_settled" }, ownerProbeRemovalDiagnostic{2, 2, 2, 7, 2, 4, 2}},
		{"other-state", func(f *fixture) { f.tx.state = "other" }, ownerProbeRemovalDiagnostic{2, 2, 2, 9, 2, 4, 2}},
		{"creation-cleanup", func(f *fixture) { f.tx.cleanupActive = true }, ownerProbeRemovalDiagnostic{2, 2, 2, 8, 3, 4, 2}},
		{"lifecycle-unavailable", func(f *fixture) { f.b.checkpoint = nil }, ownerProbeRemovalDiagnostic{2, 2, 2, 8, 2, 2, 2}},
		{"lifecycle-missing", func(f *fixture) { delete(f.b.checkpoint.lifecycle.objects, 13) }, ownerProbeRemovalDiagnostic{2, 2, 2, 8, 2, 1, 2}},
		{"lifecycle-id", func(f *fixture) { f.item.kernelID = 18 }, ownerProbeRemovalDiagnostic{2, 2, 2, 8, 2, 3, 2}},
		{"lifecycle-class", func(f *fixture) { f.item.class = "program" }, ownerProbeRemovalDiagnostic{2, 2, 2, 8, 2, 3, 2}},
		{"lifecycle-authorized", func(f *fixture) { f.item.removalAllowed = true }, ownerProbeRemovalDiagnostic{2, 2, 2, 8, 2, 5, 2}},
		{"lifecycle-removed", func(f *fixture) { f.item.removed = true }, ownerProbeRemovalDiagnostic{2, 2, 2, 8, 2, 6, 2}},
		{"lifecycle-both", func(f *fixture) { f.item.removed = true; f.item.removalAllowed = true }, ownerProbeRemovalDiagnostic{2, 2, 2, 8, 2, 7, 2}},
		{"tracker-authorized", func(f *fixture) { f.o.removalAuthorized = true }, ownerProbeRemovalDiagnostic{2, 2, 2, 8, 2, 4, 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			tc.change(&f)
			var objectCopy objectIncarnation
			if f.o != nil {
				objectCopy = *f.o
			}
			cleanupCopy, txCopy, itemCopy := *f.cleanup, *f.tx, *f.item
			objectMap := maps.Clone(f.b.effects.objects.byCookie)
			grantMap := maps.Clone(f.b.effects.objectGrant)
			fdMap := maps.Clone(f.b.effects.objectFD)
			cleanupMap := maps.Clone(f.b.effects.grantCleanup)
			txMap := maps.Clone(f.b.effects.transactions)
			var lifecycleMap map[uint64]*deferredObject
			if f.b.checkpoint != nil {
				lifecycleMap = maps.Clone(f.b.checkpoint.lifecycle.objects)
			}
			got := f.b.ownerProbeRemovalSnapshot(f.g, 13, f.o)
			if got != tc.want || !ownerProbeRemovalDiagnosticValid(got) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if f.o != nil && !reflect.DeepEqual(*f.o, objectCopy) || !reflect.DeepEqual(*f.cleanup, cleanupCopy) || !reflect.DeepEqual(*f.tx, txCopy) || !reflect.DeepEqual(*f.item, itemCopy) {
				t.Fatal("snapshot changed source objects")
			}
			if !maps.Equal(objectMap, f.b.effects.objects.byCookie) || !maps.Equal(grantMap, f.b.effects.objectGrant) ||
				!maps.Equal(fdMap, f.b.effects.objectFD) || !maps.Equal(cleanupMap, f.b.effects.grantCleanup) ||
				!maps.Equal(txMap, f.b.effects.transactions) || f.b.checkpoint != nil && !maps.Equal(lifecycleMap, f.b.checkpoint.lifecycle.objects) {
				t.Fatal("snapshot changed source maps")
			}
		})
	}
	t.Run("scope-and-cookie", func(t *testing.T) {
		f := newFixture()
		o2 := &objectIncarnation{cookie: 14, id: 18, class: "program"}
		f.b.effects.objects.byCookie[14] = o2
		f.b.effects.objectGrant[14] = f.g.id
		if got := f.b.ownerProbeRemovalSnapshot(f.g, 14, o2); got != (ownerProbeRemovalDiagnostic{3, 1, 1, 1, 1, 1, 2}) {
			t.Fatalf("second cookie: %v", got)
		}
		if got := f.b.ownerProbeRemovalSnapshot(f.g, 13, o2); got != (ownerProbeRemovalDiagnostic{}) {
			t.Fatal("mismatched pointer", got)
		}
		if got := f.b.ownerProbeRemovalSnapshot(f.g, 0, nil); got != (ownerProbeRemovalDiagnostic{}) {
			t.Fatal("zero cookie", got)
		}
		f.b.grants[9] = &brokerGrant{id: 9}
		if got := f.b.ownerProbeRemovalSnapshot(f.g, 13, f.o); got != (ownerProbeRemovalDiagnostic{}) {
			t.Fatal("grant alias", got)
		}
	})
	t.Run("unavailable-scope", func(t *testing.T) {
		f := newFixture()
		var missing *authorityBroker
		if got := missing.ownerProbeRemovalSnapshot(f.g, 13, f.o); got != (ownerProbeRemovalDiagnostic{}) {
			t.Fatal("nil broker", got)
		}
		if got := f.b.ownerProbeRemovalSnapshot(nil, 13, f.o); got != (ownerProbeRemovalDiagnostic{}) {
			t.Fatal("nil grant", got)
		}
		delete(f.b.effects.objectGrant, 13)
		if got := f.b.ownerProbeRemovalSnapshot(f.g, 13, f.o); got != (ownerProbeRemovalDiagnostic{}) {
			t.Fatal("missing association", got)
		}
		f.b.effects.objectGrant[13] = 10
		if got := f.b.ownerProbeRemovalSnapshot(f.g, 13, f.o); got != (ownerProbeRemovalDiagnostic{}) {
			t.Fatal("different owner", got)
		}
		f.b.effects.objects = nil
		if got := f.b.ownerProbeRemovalSnapshot(f.g, 13, f.o); got != (ownerProbeRemovalDiagnostic{}) {
			t.Fatal("missing tracker", got)
		}
	})
	t.Run("packet-shape", func(t *testing.T) {
		packet := [16]byte{7, 1, 1, 2, 10, 2, 2, 2, 8, 2, 4, 2}
		if !ownerProbeSettlementPacketValid(packet[:]) {
			t.Fatal("valid detail rejected")
		}
		for _, mutate := range []func(*[16]byte){
			func(p *[16]byte) { p[0] = 5 }, func(p *[16]byte) { p[0] = 6 }, func(p *[16]byte) { p[3] = 1; p[4] = 0 },
			func(p *[16]byte) { p[4] = 9 }, func(p *[16]byte) { p[5] = 0 },
			func(p *[16]byte) { p[6] = 4 }, func(p *[16]byte) { p[12] = 1 },
			func(p *[16]byte) { p[5] = 1 }, func(p *[16]byte) { p[8] = 2 },
		} {
			bad := packet
			mutate(&bad)
			if ownerProbeSettlementPacketValid(bad[:]) {
				t.Fatalf("accepted invalid packet %v", bad)
			}
		}
		for i, maximum := range [...]byte{5, 3, 7, 9, 3, 7, 3} {
			for _, value := range []byte{0, maximum + 1} {
				bad := packet
				bad[5+i] = value
				if ownerProbeSettlementPacketValid(bad[:]) {
					t.Fatalf("accepted field %d value %d", i, value)
				}
			}
		}
		zero := [16]byte{7, 1, 1, 2, 10}
		if !ownerProbeSettlementPacketValid(zero[:]) {
			t.Fatal("zero detail fallback rejected")
		}
		if ownerProbeSettlementPacketValid(packet[:8]) {
			t.Fatal("short packet accepted")
		}
	})
	var afterStat unix.Stat_t
	afterFD, e1 := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	afterFL, e2 := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err := unix.Fstat(fd, &afterStat); err != nil || e1 != nil || e2 != nil || beforeStat.Dev != afterStat.Dev || beforeStat.Ino != afterStat.Ino || beforeStat.Mode != afterStat.Mode || beforeFD != afterFD || beforeFL != afterFL {
		t.Fatal("snapshot changed borrowed FD")
	}
}

func TestOwnerProbeRuntimeDiagnosticSend(t *testing.T) {
	ownerRootPrerequisites(t)
	cid := strings.Repeat("a", 64)
	if _, err := os.Lstat(ownerProbeDiagnosticDir); !os.IsNotExist(err) {
		t.Fatalf("diagnostic directory must be absent: %v", err)
	}
	if err := os.Mkdir(ownerProbeDiagnosticDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(ownerProbeDiagnosticDir, 0700)
		_ = os.Remove(ownerProbeDiagnosticDir + "/" + cid)
		_ = os.Remove(ownerProbeDiagnosticDir)
	})
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		t.Fatal(err)
	}
	path := ownerProbeDiagnosticDir + "/" + cid
	if err = unix.Bind(fd, &unix.SockaddrUnix{Name: path}); err != nil {
		t.Fatal(err)
	}
	args := []string{"--root", runtimeShimRoot, "--log", "/run/g2/log.json", "--log-format", "json", "create", "--bundle", "/run/g2/bundle", "--pid-file", "/run/g2/init.pid", cid}
	reportOwnerProbeWrapperFailure(args, ownerProbeStageWrapperArgs, unix.EACCES)
	var data [8]byte
	oob := make([]byte, unix.CmsgSpace(unix.SizeofUcred))
	n, on, flags, _, err := unix.Recvmsg(fd, data[:], oob, unix.MSG_DONTWAIT)
	if err != nil || n != 8 || flags != 0 || data != [8]byte{2, byte(ownerProbeStageWrapperArgs), byte(unix.EACCES), 0, 0, 0, 0, 0} {
		t.Fatalf("packet %v n=%d flags=%d err=%v", data, n, flags, err)
	}
	messages, err := unix.ParseSocketControlMessage(oob[:on])
	if err != nil || len(messages) != 1 {
		t.Fatalf("credentials messages: %v", err)
	}
	cred, err := unix.ParseUnixCredentials(&messages[0])
	if err != nil || cred.Pid != int32(os.Getpid()) || cred.Uid != 0 || cred.Gid != 0 {
		t.Fatalf("credentials: %v %v", cred, err)
	}
	assertEmpty := func() {
		t.Helper()
		_, _, _, _, e := unix.Recvmsg(fd, data[:], oob, unix.MSG_DONTWAIT)
		if !errors.Is(e, unix.EAGAIN) {
			t.Fatalf("unexpected packet: %v", e)
		}
	}
	assertPacket := func(stage ownerProbeRuntimeStage, code byte) {
		t.Helper()
		var packet [8]byte
		control := make([]byte, unix.CmsgSpace(unix.SizeofUcred))
		n, on, flags, _, err := unix.Recvmsg(fd, packet[:], control, unix.MSG_DONTWAIT)
		if err != nil || n != 8 || flags != 0 || packet != [8]byte{2, byte(stage), code, 0, 0, 0, 0, 0} {
			t.Fatalf("packet for stage %d: %v n=%d flags=%d err=%v", stage, packet, n, flags, err)
		}
		messages, err := unix.ParseSocketControlMessage(control[:on])
		if err != nil || len(messages) != 1 {
			t.Fatalf("stage %d credentials messages: %v", stage, err)
		}
		cred, err := unix.ParseUnixCredentials(&messages[0])
		if err != nil || cred.Pid != int32(os.Getpid()) || cred.Uid != 0 || cred.Gid != 0 {
			t.Fatalf("stage %d credentials: %v %v", stage, cred, err)
		}
	}
	t.Run("retained-create-history", func(t *testing.T) {
		b, grant, inv := createHistoryFixture(t, cid)
		b.effects.transactions[9] = &runtimeEffectTransaction{id: 9, grantID: grant.id, invocationID: inv.id, requesterCookie: 4, notificationID: 5, command: "PROG_QUERY", state: "retired", result: json.Number("-28")}
		attr := make([]byte, 24)
		guardPut32(attr, 0, unix.BPF_PROG_TYPE_SOCKET_FILTER)
		b.effects.transactions[10] = &runtimeEffectTransaction{id: 10, grantID: grant.id, invocationID: inv.id, command: "PROG_LOAD", state: "prepared", attributes: attr, license: "MIT"}
		body := map[string]any{"transaction_id": jsonU64(9), "invocation_id": jsonU64(inv.id), "requester_cookie": jsonU64(4), "notification_id": jsonU64(5), "operation": "SEND", "response": map[string]any{"value": jsonI64(0), "error": jsonI64(-28), "flags": jsonU64(0)}, "ioctl_errno": jsonU64(0), "returned_fd": nil, "guard_before": jsonU64(1), "guard_after": jsonU64(2)}
		if e := appendRuntimeRecord(b, "notification_response", body); e != nil {
			t.Fatal(e)
		}
		b.reportOwnerProbeCreateHistory(grant, nil)
		assertEmpty()
		grant.purpose = "other"
		b.reportOwnerProbeCreateHistory(grant, errGuardProtocol)
		assertEmpty()
		grant.purpose = "postgres-uid"
		b.reportOwnerProbeCreateHistory(grant, errGuardProtocol)
		var packet [64]byte
		control := make([]byte, unix.CmsgSpace(unix.SizeofUcred))
		n, on, flags, _, e := unix.Recvmsg(fd, packet[:], control, unix.MSG_DONTWAIT)
		var expected [64]byte
		expected[0], expected[1], expected[2], expected[3] = 4, 1, 1, 1
		expected[6], expected[16], expected[18], expected[19], expected[23] = 2, 1, 1, 1, 1
		expected[40], expected[42], expected[43], expected[44], expected[47] = 1, 5, 2, 28, 2
		if e != nil || n != 64 || flags != 0 || packet != expected {
			t.Fatalf("history packet %v n=%d flags=%d err=%v", packet, n, flags, e)
		}
		messages, e := unix.ParseSocketControlMessage(control[:on])
		if e != nil || len(messages) != 1 {
			t.Fatal("history credentials", e)
		}
		cred, e := unix.ParseUnixCredentials(&messages[0])
		if e != nil || cred.Pid != int32(os.Getpid()) || cred.Uid != 0 || cred.Gid != 0 {
			t.Fatal("history credentials", cred, e)
		}
		packet[0] = 3
		sendOwnerProbeRuntimePacket(cid, packet[:])
		assertEmpty()
		b.runtimeInvocations[-2] = inv
		b.reportOwnerProbeCreateHistory(grant, errGuardProtocol)
		assertEmpty()
	})
	t.Run("settlement-packets", func(t *testing.T) {
		dir := ownerProbeSettlementDiagnosticDir
		if _, e := os.Lstat(dir); !os.IsNotExist(e) {
			t.Fatalf("settlement directory must be absent: %v", e)
		}
		if e := os.Mkdir(dir, 0700); e != nil {
			t.Fatal(e)
		}
		name := dir + "/" + cid
		t.Cleanup(func() {
			_ = os.Chmod(dir, 0700)
			_ = os.Remove(name)
			_ = os.Remove(dir)
		})
		socket, e := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
		if e != nil {
			t.Fatal(e)
		}
		defer unix.Close(socket)
		if e = unix.SetsockoptInt(socket, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); e != nil {
			t.Fatal(e)
		}
		if e = unix.Bind(socket, &unix.SockaddrUnix{Name: name}); e != nil {
			t.Fatal(e)
		}
		for purpose := byte(1); purpose <= 2; purpose++ {
			for event := byte(1); event <= 4; event++ {
				point := byte(0)
				if event == 2 {
					point = 13
				}
				want := [16]byte{7, 1, purpose, event, point}
				sendOwnerProbeRuntimePacket(cid, want[:])
				var got [16]byte
				control := make([]byte, unix.CmsgSpace(unix.SizeofUcred))
				n, cn, flags, _, e := unix.Recvmsg(socket, got[:], control, unix.MSG_DONTWAIT)
				if e != nil || n != len(got) || flags != 0 || got != want {
					t.Fatalf("packet %v n=%d flags=%d error=%v", got, n, flags, e)
				}
				messages, e := unix.ParseSocketControlMessage(control[:cn])
				if e != nil || len(messages) != 1 {
					t.Fatal("credentials", e)
				}
				cred, e := unix.ParseUnixCredentials(&messages[0])
				if e != nil || cred.Pid != int32(os.Getpid()) || cred.Uid != 0 || cred.Gid != 0 {
					t.Fatal("credentials", cred, e)
				}
			}
		}
		q := &pendingBrokerSettlement{diagnostic: ownerProbeSettlementReceipt{cid: cid, purpose: 1, valid: true}}
		q.observe(1, 0, ownerProbeRemovalDiagnostic{})
		q.observe(1, 0, ownerProbeRemovalDiagnostic{})
		q.sample(0, 2)
		q.sample(1_000_000_000, 7)
		q.sample(2_000_000_000, 8)
		q.observe(3, 0, ownerProbeRemovalDiagnostic{})
		receipt := q.diagnostic
		receipt.send(4, 0, ownerProbeRemovalDiagnostic{})
		receipt.send(4, 0, ownerProbeRemovalDiagnostic{})
		for _, want := range [][16]byte{{7, 1, 1, 1}, {7, 1, 1, 2, 7}, {7, 1, 1, 3}, {7, 1, 1, 4}} {
			var got [16]byte
			control := make([]byte, unix.CmsgSpace(unix.SizeofUcred))
			n, _, flags, _, e := unix.Recvmsg(socket, got[:], control, unix.MSG_DONTWAIT)
			if e != nil || n != len(got) || flags != 0 || got != want {
				t.Fatalf("observation %v wanted %v: %v", got, want, e)
			}
		}
		grant := &brokerGrant{id: 9}
		object := &objectIncarnation{cookie: 13, id: 17, class: "map", state: "live"}
		broker := &authorityBroker{grants: map[uint64]*brokerGrant{9: grant}, effects: &runtimeEffects{objects: &objectTracker{byCookie: map[uint64]*objectIncarnation{13: object}}, objectGrant: map[uint64]uint64{13: 9}}}
		sampled := &pendingBrokerSettlement{grant: grant, diagnostic: ownerProbeSettlementReceipt{cid: cid, purpose: 1, valid: true}}
		sampled.sampleRemoval(0, broker, 13, object)
		sampled.sampleRemoval(999_999_999, broker, 13, object)
		sampled.sampleRemoval(0, broker, 13, object)
		var sampledPacket [16]byte
		if _, _, _, _, e = unix.Recvmsg(socket, sampledPacket[:], nil, unix.MSG_DONTWAIT); !errors.Is(e, unix.EAGAIN) {
			t.Fatalf("early or rollback sample: %v", e)
		}
		sampled.sampleRemoval(1_000_000_000, broker, 13, object)
		control := make([]byte, unix.CmsgSpace(unix.SizeofUcred))
		n, cn, flags, _, e := unix.Recvmsg(socket, sampledPacket[:], control, unix.MSG_DONTWAIT)
		if want := [16]byte{7, 1, 1, 2, 10, 2, 1, 1, 1, 1, 2, 2}; e != nil || n != len(want) || flags != 0 || sampledPacket != want {
			t.Fatalf("sample %v n=%d flags=%d err=%v", sampledPacket, n, flags, e)
		}
		messages, e := unix.ParseSocketControlMessage(control[:cn])
		if e != nil || len(messages) != 1 {
			t.Fatalf("sample credentials: %v", e)
		}
		credentials, e := unix.ParseUnixCredentials(&messages[0])
		if e != nil || credentials.Pid != int32(os.Getpid()) || credentials.Uid != 0 || credentials.Gid != 0 {
			t.Fatal("sample credentials", credentials, e)
		}
		sampled.followup(broker, 1)
		n, cn, flags, _, e = unix.Recvmsg(socket, sampledPacket[:], control, unix.MSG_DONTWAIT)
		if want := [16]byte{7, 1, 1, 5, 1, 10, 1, 8, 6, 1, 2, 7, 1, 1, 1, 8}; e != nil || n != len(want) || flags != 0 || sampledPacket != want {
			t.Fatalf("followup %v n=%d flags=%d err=%v", sampledPacket, n, flags, e)
		}
		messages, e = unix.ParseSocketControlMessage(control[:cn])
		if e != nil || len(messages) != 1 {
			t.Fatalf("followup credentials: %v", e)
		}
		credentials, e = unix.ParseUnixCredentials(&messages[0])
		if e != nil || credentials.Pid != int32(os.Getpid()) || credentials.Uid != 0 || credentials.Gid != 0 {
			t.Fatal("followup credentials", credentials, e)
		}
		sampled.followup(broker, 2)
		if _, _, _, _, e = unix.Recvmsg(socket, sampledPacket[:], nil, unix.MSG_DONTWAIT); !errors.Is(e, unix.EAGAIN) {
			t.Fatalf("duplicate followup: %v", e)
		}
		for _, boundary := range []byte{2, 3, 4} {
			next := *sampled
			next.diagnosticEvents = 0
			next.followup(broker, boundary)
			n, cn, flags, _, e = unix.Recvmsg(socket, sampledPacket[:], control, unix.MSG_DONTWAIT)
			wait := byte(10)
			if boundary == 2 {
				wait = 0
			}
			want := [16]byte{7, 1, 1, 5, boundary, wait, 1, 8, 6, 1, 2, 7, 1, 1, 1, 8}
			if e != nil || n != len(want) || flags != 0 || sampledPacket != want {
				t.Fatalf("boundary %d: %v n=%d flags=%d err=%v", boundary, sampledPacket, n, flags, e)
			}
			messages, e = unix.ParseSocketControlMessage(control[:cn])
			if e != nil || len(messages) != 1 {
				t.Fatalf("boundary credentials: %v", e)
			}
			credentials, e = unix.ParseUnixCredentials(&messages[0])
			if e != nil || credentials.Pid != int32(os.Getpid()) || credentials.Uid != 0 || credentials.Gid != 0 {
				t.Fatal("boundary credentials", credentials, e)
			}
		}
		sampled.sampleRemoval(2_000_000_000, broker, 13, object)
		if _, _, _, _, e = unix.Recvmsg(socket, sampledPacket[:], nil, unix.MSG_DONTWAIT); !errors.Is(e, unix.EAGAIN) {
			t.Fatalf("duplicate sample: %v", e)
		}
		fallback := &pendingBrokerSettlement{grant: grant, diagnostic: ownerProbeSettlementReceipt{cid: cid, purpose: 1, valid: true}}
		fallback.sampleRemoval(0, nil, 13, object)
		fallback.sampleRemoval(1_000_000_000, nil, 13, object)
		n, _, flags, _, e = unix.Recvmsg(socket, sampledPacket[:], control, unix.MSG_DONTWAIT)
		if want := [16]byte{7, 1, 1, 2, 10}; e != nil || n != len(want) || flags != 0 || sampledPacket != want {
			t.Fatalf("fallback %v n=%d flags=%d err=%v", sampledPacket, n, flags, e)
		}
		invalid := [][16]byte{{5, 1, 1, 2, 0}, {6, 1, 1, 2, 10}, {7, 1, 1, 1, 1}, {7, 1, 1, 2, 14}, {7, 1, 3, 1}, {7, 1, 1, 4, 0, 1}, {7, 1, 1, 2, 10, 2}, {7, 1, 1, 2, 10, 2, 2, 2, 8, 2, 4, 2, 1}}
		for _, packet := range invalid {
			sendOwnerProbeRuntimePacket(cid, packet[:])
		}
		oldPacket := [8]byte{5, 1, 1, 2, 10}
		sendOwnerProbeRuntimePacket(cid, oldPacket[:])
		var got [16]byte
		if _, _, _, _, e = unix.Recvmsg(socket, got[:], nil, unix.MSG_DONTWAIT); !errors.Is(e, unix.EAGAIN) {
			t.Fatalf("unexpected packet: %v", e)
		}
		if e = os.Chmod(dir, 0750); e != nil {
			t.Fatal(e)
		}
		packet := [16]byte{7, 1, 1, 1}
		sendOwnerProbeRuntimePacket(cid, packet[:])
		if _, _, _, _, e = unix.Recvmsg(socket, got[:], nil, unix.MSG_DONTWAIT); !errors.Is(e, unix.EAGAIN) {
			t.Fatalf("unprotected directory accepted: %v", e)
		}
		if e = os.Chmod(dir, 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.Remove(name); e != nil {
			t.Fatal(e)
		}
		sendOwnerProbeRuntimePacket(cid, packet[:])
		if _, _, _, _, e = unix.Recvmsg(socket, got[:], nil, unix.MSG_DONTWAIT); !errors.Is(e, unix.EAGAIN) {
			t.Fatalf("missing socket accepted: %v", e)
		}
		if e = os.Symlink("/dev/null", name); e != nil {
			t.Fatal(e)
		}
		sendOwnerProbeRuntimePacket(cid, packet[:])
		if _, _, _, _, e = unix.Recvmsg(socket, got[:], nil, unix.MSG_DONTWAIT); !errors.Is(e, unix.EAGAIN) {
			t.Fatalf("symlink accepted: %v", e)
		}
		if st, e := os.Lstat(name); e != nil || st.Mode()&os.ModeSymlink == 0 {
			t.Fatal("symlink changed")
		}
	})
	for _, stage := range []ownerProbeRuntimeStage{1, 24} {
		reportOwnerProbeWrapperFailure(args, stage, unix.EACCES)
		assertPacket(stage, byte(unix.EACCES))
	}
	for stage := ownerProbeRuntimeStage(80); stage <= 150; stage++ {
		observed := error(errGuardProtocol)
		code := byte(0)
		if stage == 87 {
			observed, code = unix.EACCES, byte(unix.EACCES)
		} else if stage == 150 {
			observed, code = fmt.Errorf("wrapped: %w", unix.EACCES), byte(unix.EACCES)
		}
		reportOwnerProbeWrapperFailure(args, stage, observed)
		assertPacket(stage, code)
	}
	for _, stage := range []ownerProbeRuntimeStage{32, 44} {
		reportOwnerProbeWrapperFailure(args, stage, unix.EACCES)
		assertEmpty()
		sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: stage}, unix.EACCES)
		assertPacket(stage, byte(unix.EACCES))
	}
	for stage := ownerProbeRuntimeStage(45); stage <= 51; stage++ {
		reportOwnerProbeWrapperFailure(args, stage, unix.EACCES)
		assertEmpty()
		for _, observed := range []error{unix.EACCES, fmt.Errorf("wrapped: %w", unix.EACCES), errGuardProtocol} {
			sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: stage}, observed)
			code := byte(0)
			if errors.Is(observed, unix.EACCES) {
				code = byte(unix.EACCES)
			}
			assertPacket(stage, code)
		}
		sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: stage}, nil)
		assertEmpty()
		sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: stage, mountOptions: ownerProbeMountOptionsDiagnostic{1, 1, 1, 1}}, unix.EACCES)
		assertEmpty()
	}
	newForwarding := func(t *testing.T) (*authorityBroker, *runtimeInvocation, seccompNotification) {
		t.Helper()
		b, inv := handlerDiagnosticFixture(t, cid)
		var original unix.Stat_t
		if e := unix.Fstat(inv.listener, &original); e != nil {
			t.Fatal(e)
		}
		descriptorFlags, e := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFD, 0)
		if e != nil {
			t.Fatal(e)
		}
		statusFlags, e := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFL, 0)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			var after unix.Stat_t
			if e := unix.Fstat(inv.listener, &after); e != nil || after.Dev != original.Dev || after.Ino != original.Ino || after.Mode != original.Mode {
				t.Error("forwarding listener changed")
			}
			if f, e := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFD, 0); e != nil || f != descriptorFlags {
				t.Error("forwarding descriptor flags changed")
			}
			if f, e := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFL, 0); e != nil || f != statusFlags {
				t.Error("forwarding status flags changed")
			}
		})
		b.checkpoint.lifecycle = &checkpointLifecycle{}
		b.checkpoint.guard = guardDevice{fd: -1}
		b.join = &auditJoin{}
		b.audit = &auditMulticast{}
		n := seccompNotification{ID: 1, TID: uint32(os.Getpid()), Syscall: unix.SYS_BPF, Arch: auditArchX8664}
		n.Args[0] = unix.BPF_PROG_QUERY
		return b, inv, n
	}
	b, inv, notification := newForwarding(t)
	if e := b.denyRuntimeNotification(inv, notification); !errors.Is(e, unix.ENOTTY) {
		t.Fatalf("original denial did not reach notification ioctl: %v", e)
	}
	b, inv, notification = newForwarding(t)
	if e := b.denyRuntimeHandlerNotification(inv, notification, unix.BPF_PROG_QUERY, fmt.Errorf("handler: %w", unix.EACCES)); !errors.Is(e, unix.ENOTTY) {
		t.Fatalf("handler denial returned %v", e)
	}
	assertPacket(49, byte(unix.EACCES))
	if e := b.denyRuntimeHandlerNotification(inv, notification, unix.BPF_PROG_QUERY, unix.EIO); !errors.Is(e, unix.ENOTTY) {
		t.Fatalf("repeated denial returned %v", e)
	}
	assertEmpty()
	for _, tc := range []struct {
		name     string
		change   func(*authorityBroker, *runtimeInvocation)
		command  uint32
		observed error
	}{
		{"selftest", func(b *authorityBroker, _ *runtimeInvocation) { b.selftestFD = &selftestFDArm{} }, unix.BPF_PROG_QUERY, unix.EACCES},
		{"ineligible", func(_ *authorityBroker, i *runtimeInvocation) { i.runtimeExecRecorded = false }, unix.BPF_PROG_QUERY, unix.EACCES},
		{"unsupported", func(_ *authorityBroker, _ *runtimeInvocation) {}, ^uint32(0), unix.EACCES},
		{"nil-error", func(_ *authorityBroker, _ *runtimeInvocation) {}, unix.BPF_PROG_QUERY, nil},
	} {
		t.Run("forward-"+tc.name, func(t *testing.T) {
			b, inv, n := newForwarding(t)
			tc.change(b, inv)
			if e := b.denyRuntimeHandlerNotification(inv, n, tc.command, tc.observed); !errors.Is(e, unix.ENOTTY) || inv.ownerProbeHandlerDiagnosticAttempted {
				t.Fatalf("denial=%v reserved=%v", e, inv.ownerProbeHandlerDiagnosticAttempted)
			}
			assertEmpty()
		})
	}
	for stage := ownerProbeRuntimeStage(151); stage <= 241; stage++ {
		reportOwnerProbeWrapperFailure(args, stage, unix.EACCES)
		assertEmpty()
		observed := error(errGuardProtocol)
		code := byte(0)
		if stage == 151 {
			observed, code = unix.EACCES, byte(unix.EACCES)
		} else if stage == 241 {
			observed, code = fmt.Errorf("wrapped: %w", unix.EACCES), byte(unix.EACCES)
		}
		sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: stage}, observed)
		assertPacket(stage, code)
	}
	witness := ownerProbeMountOptionsDiagnostic{11, 12, 13, 6}
	sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: 188, mountOptions: witness}, errGuardProtocol)
	n, on, flags, _, err = unix.Recvmsg(fd, data[:], oob, unix.MSG_DONTWAIT)
	if err != nil || n != 8 || flags != 0 || data != [8]byte{2, 188, 0, 0, 11, 12, 13, 6} {
		t.Fatalf("witness packet %v: %v", data, err)
	}
	messages, err = unix.ParseSocketControlMessage(oob[:on])
	if err != nil || len(messages) != 1 {
		t.Fatalf("witness credentials: %v", err)
	}
	cred, err = unix.ParseUnixCredentials(&messages[0])
	if err != nil || cred.Pid != int32(os.Getpid()) || cred.Uid != 0 || cred.Gid != 0 {
		t.Fatalf("witness credentials: %v", err)
	}
	for _, bad := range []ownerProbeRuntimeDiagnostic{
		{stage: 188, mountOptions: ownerProbeMountOptionsDiagnostic{11, 12, 13, 0}},
		{stage: 188, mountOptions: ownerProbeMountOptionsDiagnostic{13, 12, 13, 6}},
		{stage: 188, mountOptions: ownerProbeMountOptionsDiagnostic{11, 18, 13, 6}},
		{stage: 188, mountOptions: ownerProbeMountOptionsDiagnostic{11, 12, 18, 6}},
		{stage: 188, mountOptions: ownerProbeMountOptionsDiagnostic{11, 12, 13, 10}},
		{stage: 151, mountOptions: witness},
	} {
		sendOwnerProbeRuntimeDiagnostic(cid, bad, errGuardProtocol)
		assertEmpty()
	}
	sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: 188, mountOptions: witness}, unix.EACCES)
	assertEmpty()
	sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: 188}, unix.EACCES)
	assertEmpty()
	reportOwnerProbeWrapperFailure(args, 64, unix.EACCES)
	assertEmpty()
	sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: 64}, nil)
	assertPacket(64, 0)
	for _, stage := range []ownerProbeRuntimeStage{25, 31, 52, 63, 65, 79, 242, 255} {
		reportOwnerProbeWrapperFailure(args, stage, unix.EACCES)
		assertEmpty()
		sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: stage}, unix.EACCES)
		assertEmpty()
	}
	reportOwnerProbeWrapperFailure(args, 80, nil)
	assertEmpty()
	sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: 150}, nil)
	assertEmpty()
	sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: 151}, nil)
	assertEmpty()
	for _, tc := range []struct {
		name  string
		args  []string
		stage ownerProbeRuntimeStage
		err   error
	}{
		{"other-command", []string{"start", cid}, ownerProbeStageWrapperArgs, unix.EACCES},
		{"bad-id", []string{"create", "bad"}, ownerProbeStageWrapperArgs, unix.EACCES},
		{"duplicate-create", []string{"create", "create", cid}, ownerProbeStageWrapperArgs, unix.EACCES},
		{"mixed-command", []string{"create", "kill", cid}, ownerProbeStageWrapperArgs, unix.EACCES},
		{"invalid-stage", args, ownerProbeRuntimeStage(25), unix.EACCES},
		{"nil-error", args, ownerProbeStageWrapperArgs, nil},
		{"exec-proof-is-not-wrapper", args, ownerProbeStageRuncExecObserved, unix.EACCES},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reportOwnerProbeWrapperFailure(tc.args, tc.stage, tc.err)
			assertEmpty()
		})
	}
	sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageRuncExecObserved}, unix.EACCES)
	sendOwnerProbeRuntimeDiagnostic(cid, ownerProbeRuntimeDiagnostic{stage: ownerProbeStageBrokerBundleInput}, nil)
	assertEmpty()
	if err = os.Chmod(ownerProbeDiagnosticDir, 0750); err != nil {
		t.Fatal(err)
	}
	reportOwnerProbeWrapperFailure(args, ownerProbeStageWrapperArgs, unix.EACCES)
	assertEmpty()
	if err = os.Chmod(ownerProbeDiagnosticDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	reportOwnerProbeWrapperFailure(args, ownerProbeStageWrapperArgs, unix.EACCES)
	assertEmpty()
	if err = os.Symlink("/dev/null", path); err != nil {
		t.Fatal(err)
	}
	reportOwnerProbeWrapperFailure(args, ownerProbeStageWrapperArgs, unix.EACCES)
	assertEmpty()
	if st, e := os.Lstat(path); e != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink changed")
	}
}

func handlerDiagnosticFixture(t *testing.T, cid string) (*authorityBroker, *runtimeInvocation) {
	t.Helper()
	f, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	b := controlDiagnosticFixture()
	b.bootOwner = 1
	grant := &brokerGrant{id: 1, generation: 2, owner: 1, containerID: cid, purpose: "postgres-uid", phase: "admitted"}
	inv := &runtimeInvocation{id: 1, listener: int(f.Fd()), grantID: 1, generation: 2, state: "running", runtimeExecRecorded: true, command: "create", containerID: cid, grant: grant}
	b.runtimeInvocations = map[int]*runtimeInvocation{inv.listener: inv}
	return b, inv
}

func TestOwnerProbeHandlerDiagnosticPolicy(t *testing.T) {
	cid := strings.Repeat("a", 64)
	commands := []struct {
		command uint32
		stage   ownerProbeRuntimeStage
	}{
		{unix.BPF_MAP_CREATE, 45}, {unix.BPF_PROG_LOAD, 46}, {unix.BPF_OBJ_GET_INFO_BY_FD, 47},
		{unix.BPF_PROG_GET_FD_BY_ID, 48}, {unix.BPF_PROG_QUERY, 49}, {unix.BPF_PROG_ATTACH, 50}, {unix.BPF_PROG_DETACH, 51},
	}
	for _, tc := range commands {
		t.Run(fmt.Sprintf("command-%d", tc.stage), func(t *testing.T) {
			for _, observed := range []error{errors.New("sentinel"), unix.EACCES} {
				b, inv := handlerDiagnosticFixture(t, cid)
				before, grant := *inv, *inv.grant
				var statBefore, statAfter unix.Stat_t
				if err := unix.Fstat(inv.listener, &statBefore); err != nil {
					t.Fatal(err)
				}
				flags, err := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFD, 0)
				if err != nil {
					t.Fatal(err)
				}
				status, err := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFL, 0)
				if err != nil {
					t.Fatal(err)
				}
				if got := b.reserveOwnerProbeHandlerDiagnostic(inv, tc.command, observed); got != tc.stage {
					t.Fatalf("stage=%d", got)
				}
				if got := b.reserveOwnerProbeHandlerDiagnostic(inv, unix.BPF_PROG_QUERY, observed); got != 0 {
					t.Fatalf("second stage=%d", got)
				}
				before.ownerProbeHandlerDiagnosticAttempted = true
				if !reflect.DeepEqual(*inv, before) || !reflect.DeepEqual(*inv.grant, grant) || b.runtimeInvocations[inv.listener] != inv {
					t.Fatal("reservation changed invocation or grant")
				}
				if err := unix.Fstat(inv.listener, &statAfter); err != nil || statBefore.Dev != statAfter.Dev || statBefore.Ino != statAfter.Ino || statBefore.Mode != statAfter.Mode {
					t.Fatal("listener changed")
				}
				if f, e := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFD, 0); e != nil || f != flags {
					t.Fatal("descriptor flags changed")
				}
				if f, e := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFL, 0); e != nil || f != status {
					t.Fatal("status flags changed")
				}
			}
		})
	}
	for _, purpose := range []string{"postgres-uid", "postgres-gid"} {
		for _, phase := range []string{"admitted", "active", "settling"} {
			t.Run(purpose+"-"+phase, func(t *testing.T) {
				b, inv := handlerDiagnosticFixture(t, cid)
				inv.grant.purpose, inv.grant.phase = purpose, phase
				if got := b.reserveOwnerProbeHandlerDiagnostic(inv, unix.BPF_PROG_QUERY, unix.EACCES); got != 49 {
					t.Fatalf("allowed stage=%d", got)
				}
			})
		}
	}

	gates := []struct {
		name   string
		change func(*authorityBroker, *runtimeInvocation)
	}{
		{"selftest", func(b *authorityBroker, _ *runtimeInvocation) { b.selftestFD = &selftestFDArm{} }},
		{"id", func(_ *authorityBroker, i *runtimeInvocation) { i.id = 0 }},
		{"listener", func(_ *authorityBroker, i *runtimeInvocation) { i.listener = -1 }},
		{"map", func(b *authorityBroker, i *runtimeInvocation) {
			b.runtimeInvocations[i.listener] = &runtimeInvocation{}
		}},
		{"grant", func(_ *authorityBroker, i *runtimeInvocation) { i.grant = nil }},
		{"grant-id", func(_ *authorityBroker, i *runtimeInvocation) { i.grantID++ }},
		{"generation", func(_ *authorityBroker, i *runtimeInvocation) { i.generation++ }},
		{"state", func(_ *authorityBroker, i *runtimeInvocation) { i.state = "bound" }},
		{"exec", func(_ *authorityBroker, i *runtimeInvocation) { i.runtimeExecRecorded = false }},
		{"command", func(_ *authorityBroker, i *runtimeInvocation) { i.command = "start" }},
		{"cid", func(_ *authorityBroker, i *runtimeInvocation) { i.containerID = strings.Repeat("b", 64) }},
		{"zero-grant-id", func(_ *authorityBroker, i *runtimeInvocation) { i.grant.id = 0; i.grantID = 0 }},
		{"zero-generation", func(_ *authorityBroker, i *runtimeInvocation) { i.grant.generation = 0; i.generation = 0 }},
		{"boot-owner", func(b *authorityBroker, _ *runtimeInvocation) { b.bootOwner = 0 }},
		{"owner", func(_ *authorityBroker, i *runtimeInvocation) { i.grant.owner = 2 }},
		{"purpose", func(_ *authorityBroker, i *runtimeInvocation) { i.grant.purpose = "other" }},
		{"phase", func(_ *authorityBroker, i *runtimeInvocation) { i.grant.phase = "retired" }},
	}
	for _, tc := range gates {
		t.Run(tc.name, func(t *testing.T) {
			b, inv := handlerDiagnosticFixture(t, cid)
			originalInv, originalGrant, originalMap, originalBoot, originalSelftest := *inv, *inv.grant, b.runtimeInvocations[inv.listener], b.bootOwner, b.selftestFD
			tc.change(b, inv)
			if got := b.reserveOwnerProbeHandlerDiagnostic(inv, unix.BPF_PROG_QUERY, unix.EACCES); got != 0 || inv.ownerProbeHandlerDiagnosticAttempted {
				t.Fatal("ineligible invocation reserved")
			}
			inv.ownerProbeHandlerDiagnosticAttempted = true
			if got := b.reserveOwnerProbeHandlerDiagnostic(inv, unix.BPF_PROG_QUERY, unix.EACCES); got != 0 || !inv.ownerProbeHandlerDiagnosticAttempted {
				t.Fatal("gate changed reservation")
			}
			*originalInv.grant = originalGrant
			*inv = originalInv
			b.runtimeInvocations[inv.listener], b.bootOwner, b.selftestFD = originalMap, originalBoot, originalSelftest
			if got := b.reserveOwnerProbeHandlerDiagnostic(inv, unix.BPF_PROG_QUERY, unix.EACCES); got != 49 {
				t.Fatalf("restored gate=%d", got)
			}
		})
	}
	b, inv := handlerDiagnosticFixture(t, cid)
	if b.reserveOwnerProbeHandlerDiagnostic(inv, ^uint32(0), unix.EACCES) != 0 || b.reserveOwnerProbeHandlerDiagnostic(inv, unix.BPF_PROG_QUERY, nil) != 0 || inv.ownerProbeHandlerDiagnosticAttempted {
		t.Fatal("invalid request reserved")
	}
	if (*authorityBroker)(nil).reserveOwnerProbeHandlerDiagnostic(inv, unix.BPF_PROG_QUERY, unix.EACCES) != 0 || b.reserveOwnerProbeHandlerDiagnostic(nil, unix.BPF_PROG_QUERY, unix.EACCES) != 0 {
		t.Fatal("nil receiver or invocation reserved")
	}
}

func createHistoryFixture(t *testing.T, cid string) (*authorityBroker, *brokerGrant, *runtimeInvocation) {
	t.Helper()
	b, inv := handlerDiagnosticFixture(t, cid)
	inv.grant.createBound = true
	inv.configSHA = "fixed-config"
	inv.grant.configSHA = inv.configSHA
	b.effects = &runtimeEffects{transactions: make(map[uint64]*runtimeEffectTransaction), cgroups: make(map[uint64]*liveCgroup)}
	return b, inv.grant, inv
}

func TestOwnerProbeCreateHistoryPolicy(t *testing.T) {
	// These fixtures exercise user-space snapshots, not authenticated native activity.
	cid := strings.Repeat("a", 64)
	b, grant, inv := createHistoryFixture(t, cid)
	var originalFD unix.Stat_t
	if err := unix.Fstat(inv.listener, &originalFD); err != nil {
		t.Fatal(err)
	}
	fdFlags, err := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	statusFlags, err := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	originalGrant, originalInvocation := *grant, *inv
	commands := []string{"MAP_CREATE", "PROG_LOAD", "OBJ_GET_INFO_BY_FD", "PROG_GET_FD_BY_ID", "PROG_QUERY", "PROG_ATTACH", "PROG_DETACH"}
	for i, command := range commands {
		id := uint64(i + 1)
		b.effects.transactions[id] = &runtimeEffectTransaction{id: id, grantID: grant.id, invocationID: inv.id, command: command, state: "effect_observed", result: json.Number("0")}
	}
	b.effects.transactions[2].attributes = make([]byte, 24)
	guardPut32(b.effects.transactions[2].attributes, 0, unix.BPF_PROG_TYPE_SOCKET_FILTER)
	b.effects.transactions[2].license = "MIT"
	b.effects.transactions[5].result = json.Number("-9")
	b.effects.transactions[20] = &runtimeEffectTransaction{id: 20, grantID: grant.id, invocationID: inv.id, command: "PROG_QUERY", state: "effect_observed", result: json.Number("-28"), policyErr: unix.EACCES}
	b.effects.transactions[30] = &runtimeEffectTransaction{id: 30, grantID: grant.id, invocationID: inv.id, command: "PROG_QUERY", state: "retired", result: json.Number("-9"), internalDetach: true}
	b.effects.transactions[33] = &runtimeEffectTransaction{id: 33, grantID: grant.id, invocationID: inv.id, command: "PROG_QUERY", internalSelftest: true}
	b.effects.transactions[34] = &runtimeEffectTransaction{id: 34, grantID: grant.id, invocationID: inv.id, command: "PROG_QUERY", cleanupParent: b.effects.transactions[5]}
	b.effects.transactions[31] = &runtimeEffectTransaction{id: 31, grantID: grant.id + 1, invocationID: inv.id, command: "UNKNOWN"}
	b.effects.transactions[32] = &runtimeEffectTransaction{id: 32, grantID: grant.id, invocationID: inv.id + 1, command: "UNKNOWN"}
	packet, ok := b.ownerProbeCreateHistory(grant)
	if !ok || packet[0] != 4 || packet[1] != 1 || packet[2] != 1 || packet[3] != 1 || packet[4] != 0 || packet[5] != 0 || packet[6] != 2 {
		t.Fatalf("header: %v %v", packet, ok)
	}
	var afterFD unix.Stat_t
	if err := unix.Fstat(inv.listener, &afterFD); err != nil || originalFD.Dev != afterFD.Dev || originalFD.Ino != afterFD.Ino || originalFD.Mode != afterFD.Mode {
		t.Fatal("snapshot changed borrowed FD", err)
	}
	if f, err := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFD, 0); err != nil || f != fdFlags {
		t.Fatal("snapshot changed FD flags", err)
	}
	if f, err := unix.FcntlInt(uintptr(inv.listener), unix.F_GETFL, 0); err != nil || f != statusFlags {
		t.Fatal("snapshot changed file flags", err)
	}
	if !reflect.DeepEqual(*grant, originalGrant) || !reflect.DeepEqual(*inv, originalInvocation) {
		t.Fatal("snapshot changed grant or invocation")
	}
	for slot := range commands {
		i := 8 + 8*slot
		if slot == 4 {
			if got := packet[i : i+8]; !reflect.DeepEqual(got, []byte{2, 0, 3, 2, 28, 0, 1, 1}) {
				t.Fatalf("query: %v", got)
			}
		} else if got := packet[i : i+8]; !reflect.DeepEqual(got, []byte{1, 0, 3, 3, 0, 0, 0, 1}) {
			t.Fatalf("slot %d: %v", slot, got)
		}
	}
	delete(b.runtimeInvocations, inv.listener)
	b.runtimeInvocations[-int(inv.id)-1] = inv
	retired, ok := b.ownerProbeCreateHistory(grant)
	if !ok || packet != retired {
		t.Fatal("retired create differs")
	}
	b.effects.transactions[20].result = json.Number("-9")
	if packet[8+4*8+4] != 28 {
		t.Fatal("returned packet retained input")
	}
	t.Run("group-and-target", func(t *testing.T) {
		b, g, _ := createHistoryFixture(t, cid)
		group := &liveCgroup{GrantID: g.id, Enrollment: 4, RootCookie: 2, KernfsID: 3, HeldFD: 10}
		b.effects.cgroups[g.id] = group
		p, ok := b.ownerProbeCreateHistory(g)
		if !ok || p[4] != 1 || p[5] != 0 {
			t.Fatal("nil registry", p, ok)
		}
		b.effects.registry = &effectRegistry{targets: make(map[cgroupKey]*effectRegistryTarget)}
		p, ok = b.ownerProbeCreateHistory(g)
		if !ok || p[4] != 1 || p[5] != 1 {
			t.Fatal("absent target", p, ok)
		}
		target := &effectRegistryTarget{group: group, grant: g}
		b.effects.registry.targets[cgroupKey{root: 2, id: 3}] = target
		for phase := uint32(0); phase <= 5; phase++ {
			target.phase = phase
			p, ok = b.ownerProbeCreateHistory(g)
			want := byte(phase + 2)
			if phase == 5 {
				want = 7
			}
			if !ok || p[5] != want {
				t.Fatal("phase", phase, p, ok)
			}
		}
		other := *g
		target.grant = &other
		p, ok = b.ownerProbeCreateHistory(g)
		if !ok || p[5] != 8 {
			t.Fatal("target identity", p, ok)
		}
		group.Retired = true
		group.HeldFD = -1
		p, ok = b.ownerProbeCreateHistory(g)
		if !ok || p[4] != 2 {
			t.Fatal("retired group", p, ok)
		}
		group.Retired = false
		p, ok = b.ownerProbeCreateHistory(g)
		if !ok || p[4] != 3 || p[5] != 0 {
			t.Fatal("invalid group", p, ok)
		}
	})
	for _, tc := range []struct {
		state  string
		result any
		want   byte
	}{{"prepared", nil, 1}, {"committed", nil, 1}, {"retired", nil, 1}, {"effect_observed", json.Number("1"), 4}} {
		t.Run(tc.state, func(t *testing.T) {
			b, g, i := createHistoryFixture(t, cid)
			b.effects.transactions[1] = &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: i.id, command: "MAP_CREATE", state: tc.state, result: tc.result}
			p, ok := b.ownerProbeCreateHistory(g)
			if !ok || p[11] != tc.want {
				t.Fatalf("result: %v %v", p, ok)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*authorityBroker, *brokerGrant, *runtimeInvocation)
	}{
		{"no-create", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) {
			delete(b.runtimeInvocations, i.listener)
		}},
		{"duplicate-create", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { b.runtimeInvocations[-2] = i }},
		{"nil-invocation", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { b.runtimeInvocations[-2] = nil }},
		{"wrong-pointer", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { other := *g; i.grant = &other }},
		{"wrong-generation", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { i.generation++ }},
		{"wrong-cid", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { i.containerID = "other" }},
		{"wrong-config", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { i.configSHA = "other" }},
		{"unbound", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { g.createBound = false }},
		{"purpose", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { g.purpose = "other" }},
		{"owner", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { g.owner++ }},
		{"selftest", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { b.selftestFD = &selftestFDArm{} }},
		{"zero-invocation", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { i.id = 0 }},
		{"zero-tx", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) {
			b.effects.transactions[1] = &runtimeEffectTransaction{grantID: g.id, invocationID: i.id}
		}},
		{"mismatched-tx", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) {
			b.effects.transactions[1] = &runtimeEffectTransaction{id: 2, grantID: g.id, invocationID: i.id}
		}},
		{"prepared-result", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) {
			b.effects.transactions[1] = &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: i.id, command: "MAP_CREATE", state: "prepared", result: json.Number("0")}
		}},
		{"observed-without-result", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) {
			b.effects.transactions[1] = &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: i.id, command: "MAP_CREATE", state: "effect_observed"}
		}},
		{"result-overflow", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) {
			b.effects.transactions[1] = &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: i.id, command: "MAP_CREATE", state: "retired", result: json.Number("9223372036854775808")}
		}},
		{"nil-tx", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) { b.effects.transactions[1] = nil }},
		{"unknown-command", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) {
			b.effects.transactions[1] = &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: i.id, command: "UNKNOWN", state: "prepared"}
		}},
		{"unknown-state", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) {
			b.effects.transactions[1] = &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: i.id, command: "MAP_CREATE", state: "bad"}
		}},
		{"bad-result", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) {
			b.effects.transactions[1] = &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: i.id, command: "MAP_CREATE", state: "retired", result: json.Number("-4096")}
		}},
		{"result-type", func(b *authorityBroker, g *brokerGrant, i *runtimeInvocation) {
			b.effects.transactions[1] = &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: i.id, command: "MAP_CREATE", state: "retired", result: int64(1)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, g, i := createHistoryFixture(t, cid)
			tc.mutate(b, g, i)
			p, ok := b.ownerProbeCreateHistory(g)
			if ok || p != ([64]byte{}) {
				t.Fatalf("malformed graph yielded %v %v", p, ok)
			}
		})
	}
	if p, ok := (*authorityBroker)(nil).ownerProbeCreateHistory(nil); ok || p != ([64]byte{}) {
		t.Fatal("nil broker")
	}
	t.Run("count-limit", func(t *testing.T) {
		b, g, inv := createHistoryFixture(t, cid)
		for id := uint64(1); id <= 4096; id++ {
			b.effects.transactions[id] = &runtimeEffectTransaction{id: id, grantID: g.id, invocationID: inv.id, command: "PROG_ATTACH", state: "prepared"}
		}
		p, ok := b.ownerProbeCreateHistory(g)
		if !ok || p[48] != 0 || p[49] != 16 {
			t.Fatal("count 4096", ok, p[48:56])
		}
		b.effects.transactions[4097] = &runtimeEffectTransaction{id: 4097}
		if p, ok = b.ownerProbeCreateHistory(g); ok || p != ([64]byte{}) {
			t.Fatal("over limit")
		}
	})
	for _, tc := range []struct {
		name    string
		typ     uint32
		license string
		want    byte
	}{
		{"device", unix.BPF_PROG_TYPE_CGROUP_DEVICE, "Apache", 1},
		{"extended", unix.BPF_PROG_TYPE_SOCKET_FILTER, "MIT", 2},
		{"replace", unix.BPF_PROG_TYPE_CGROUP_DEVICE, "MIT", 3},
		{"invalid", unix.BPF_PROG_TYPE_SOCKET_FILTER, "Apache", 0},
	} {
		t.Run("program-"+tc.name, func(t *testing.T) {
			b, g, inv := createHistoryFixture(t, cid)
			attr := make([]byte, 24)
			guardPut32(attr, 0, tc.typ)
			b.effects.transactions[1] = &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: inv.id, command: "PROG_LOAD", state: "prepared", attributes: attr, license: tc.license}
			p, ok := b.ownerProbeCreateHistory(g)
			if ok != (tc.want != 0) || p[6] != tc.want {
				t.Fatalf("program: %v %v", p, ok)
			}
		})
	}
	for _, tc := range []struct {
		name, command, state, result, operation string
		ioctl                                   uint64
		returned                                any
		response                                any
		want                                    byte
	}{
		{"send-ok-error", "PROG_QUERY", "effect_observed", "-9", "SEND", 0, nil, map[string]any{"value": jsonI64(0), "error": jsonI64(-9), "flags": jsonU64(0)}, 2},
		{"send-enoent", "PROG_QUERY", "retired", "0", "SEND", 2, nil, map[string]any{"value": jsonI64(0), "error": jsonI64(0), "flags": jsonU64(0)}, 3},
		{"addfd-ok", "MAP_CREATE", "response_settled", "3", "ADDFD_SEND", 0, jsonU64(7), nil, 4},
		{"addfd-enoent", "MAP_CREATE", "retired", "3", "ADDFD_SEND", 2, nil, nil, 5},
		{"fd-send-denied", "MAP_CREATE", "effect_observed", "0", "SEND", 0, nil, map[string]any{"value": jsonI64(0), "error": jsonI64(-1), "flags": jsonU64(0)}, 2},
		{"fd-send-invalid", "MAP_CREATE", "effect_observed", "0", "SEND", 0, nil, map[string]any{"value": jsonI64(0), "error": jsonI64(0), "flags": jsonU64(0)}, 0},
		{"nonfd-addfd", "PROG_QUERY", "effect_observed", "0", "ADDFD_SEND", 0, jsonU64(7), nil, 0},
	} {
		t.Run("response-"+tc.name, func(t *testing.T) {
			b, g, inv := createHistoryFixture(t, cid)
			tx := &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: inv.id, requesterCookie: 4, notificationID: 5, command: tc.command, state: tc.state, result: json.Number(tc.result), returnedFD: tc.returned}
			b.effects.transactions[1] = tx
			body := map[string]any{"transaction_id": jsonU64(1), "invocation_id": jsonU64(inv.id), "requester_cookie": jsonU64(4), "notification_id": jsonU64(5), "operation": tc.operation, "response": tc.response, "ioctl_errno": jsonU64(tc.ioctl), "returned_fd": tc.returned, "guard_before": jsonU64(1), "guard_after": jsonU64(2)}
			if err := appendRuntimeRecord(b, "notification_response", body); err != nil {
				t.Fatal(err)
			}
			p, ok := b.ownerProbeCreateHistory(g)
			slot := 0
			if tc.command == "PROG_QUERY" {
				slot = 4
			}
			if ok != (tc.want != 0) || ok && p[15+8*slot] != tc.want {
				t.Fatalf("response: %v %v", p, ok)
			}
			if tc.want != 0 {
				if err := appendRuntimeRecord(b, "notification_response", body); err != nil {
					t.Fatal(err)
				}
				if p, ok := b.ownerProbeCreateHistory(g); ok || p != ([64]byte{}) {
					t.Fatal("duplicate accepted")
				}
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any, *runtimeEffectTransaction, *authorityBroker)
	}{
		{"missing-id", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) { delete(m, "transaction_id") }},
		{"bad-id", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) { m["transaction_id"] = "1" }},
		{"zero-id", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["transaction_id"] = jsonU64(0)
		}},
		{"overflow-id", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["transaction_id"] = json.Number("18446744073709551616")
		}},
		{"invocation", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["invocation_id"] = jsonU64(99)
		}},
		{"requester", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["requester_cookie"] = jsonU64(0)
		}},
		{"notification", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) { delete(m, "notification_id") }},
		{"extra", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) { m["extra"] = true }},
		{"guard-order", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) { m["guard_after"] = jsonU64(0) }},
		{"guard-missing", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) { delete(m, "guard_before") }},
		{"guard-type", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) { m["guard_before"] = true }},
		{"ioctl", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["ioctl_errno"] = jsonU64(13)
		}},
		{"operation", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) { m["operation"] = "OTHER" }},
		{"response-shape", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["response"] = map[string]any{}
		}},
		{"response-flags", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["response"].(map[string]any)["flags"] = jsonU64(1)
		}},
		{"response-error", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["response"].(map[string]any)["error"] = jsonI64(-4096)
		}},
		{"response-overflow", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["response"].(map[string]any)["value"] = json.Number("9223372036854775808")
		}},
		{"response-extra", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["response"].(map[string]any)["extra"] = true
		}},
		{"response-type", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["response"].(map[string]any)["error"] = false
		}},
		{"response-value", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["response"].(map[string]any)["value"] = jsonI64(1)
			m["response"].(map[string]any)["error"] = jsonI64(-1)
		}},
		{"send-returned", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) { m["returned_fd"] = jsonU64(7) }},
		{"addfd-nonfd", func(m map[string]any, _ *runtimeEffectTransaction, _ *authorityBroker) {
			m["operation"] = "ADDFD_SEND"
			m["response"] = nil
			m["returned_fd"] = jsonU64(7)
		}},
		{"addfd-negative-native", func(m map[string]any, tx *runtimeEffectTransaction, _ *authorityBroker) {
			tx.command = "MAP_CREATE"
			tx.result = json.Number("-9")
			m["operation"] = "ADDFD_SEND"
			m["response"] = nil
			m["returned_fd"] = jsonU64(7)
		}},
		{"addfd-response", func(m map[string]any, tx *runtimeEffectTransaction, _ *authorityBroker) {
			tx.command = "MAP_CREATE"
			m["operation"] = "ADDFD_SEND"
			m["returned_fd"] = jsonU64(7)
		}},
		{"addfd-oversize", func(m map[string]any, tx *runtimeEffectTransaction, _ *authorityBroker) {
			tx.command = "MAP_CREATE"
			m["operation"] = "ADDFD_SEND"
			m["response"] = nil
			m["returned_fd"] = jsonU64(1 << 31)
		}},
		{"addfd-missing", func(m map[string]any, tx *runtimeEffectTransaction, _ *authorityBroker) {
			tx.command = "MAP_CREATE"
			m["operation"] = "ADDFD_SEND"
			m["response"] = nil
		}},
		{"addfd-enoent-returned", func(m map[string]any, tx *runtimeEffectTransaction, _ *authorityBroker) {
			tx.command = "MAP_CREATE"
			m["operation"] = "ADDFD_SEND"
			m["response"] = nil
			m["ioctl_errno"] = jsonU64(2)
			m["returned_fd"] = jsonU64(7)
		}},
		{"addfd-mismatch", func(m map[string]any, tx *runtimeEffectTransaction, _ *authorityBroker) {
			tx.command = "MAP_CREATE"
			tx.returnedFD = jsonU64(8)
			m["operation"] = "ADDFD_SEND"
			m["response"] = nil
			m["returned_fd"] = jsonU64(7)
		}},
		{"returned", func(_ map[string]any, tx *runtimeEffectTransaction, _ *authorityBroker) { tx.returnedFD = jsonU64(7) }},
		{"prepared", func(_ map[string]any, tx *runtimeEffectTransaction, _ *authorityBroker) {
			tx.state = "prepared"
			tx.result = nil
		}},
		{"committed", func(_ map[string]any, tx *runtimeEffectTransaction, _ *authorityBroker) {
			tx.state = "committed"
			tx.result = nil
		}},
		{"retired-cancel", func(_ map[string]any, tx *runtimeEffectTransaction, _ *authorityBroker) {
			tx.state = "retired"
			tx.result = nil
		}},
	} {
		t.Run("invalid-response-"+tc.name, func(t *testing.T) {
			b, g, inv := createHistoryFixture(t, cid)
			tx := &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: inv.id, requesterCookie: 4, notificationID: 5, command: "PROG_QUERY", state: "retired", result: json.Number("0")}
			b.effects.transactions[1] = tx
			body := map[string]any{"transaction_id": jsonU64(1), "invocation_id": jsonU64(inv.id), "requester_cookie": jsonU64(4), "notification_id": jsonU64(5), "operation": "SEND", "response": map[string]any{"value": jsonI64(0), "error": jsonI64(0), "flags": jsonU64(0)}, "ioctl_errno": jsonU64(0), "returned_fd": nil, "guard_before": jsonU64(1), "guard_after": jsonU64(2)}
			if err := appendRuntimeRecord(b, "notification_response", body); err != nil {
				t.Fatal(err)
			}
			// Corrupt the retained canonical map only after exercising its real append path.
			frozen := b.transcript.entries[0].record["body"].(map[string]any)
			tc.change(frozen, tx, b)
			if p, ok := b.ownerProbeCreateHistory(g); ok || p != ([64]byte{}) {
				t.Fatalf("invalid response accepted: %v", p)
			}
		})
	}
	t.Run("response-missing-and-budget", func(t *testing.T) {
		b, g, inv := createHistoryFixture(t, cid)
		tx := &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: inv.id, command: "PROG_QUERY", state: "effect_observed", result: json.Number("0")}
		b.effects.transactions[1] = tx
		if p, ok := b.ownerProbeCreateHistory(g); !ok || p[47] != 1 {
			t.Fatal("observed without response", p, ok)
		}
		tx.state = "response_settled"
		if _, ok := b.ownerProbeCreateHistory(g); ok {
			t.Fatal("settled without response")
		}
		tx.state, tx.result = "retired", nil
		if p, ok := b.ownerProbeCreateHistory(g); !ok || p[47] != 1 {
			t.Fatal("cancelled without response", p, ok)
		}
		// These are sanity-bound inputs, not evidence of a physically full transcript.
		b.transcript.total = maxAuthorityTranscriptBytes
		if _, ok := b.ownerProbeCreateHistory(g); !ok {
			t.Fatal("maximum total")
		}
		b.transcript.total = maxAuthorityTranscriptBytes + 1
		if _, ok := b.ownerProbeCreateHistory(g); ok {
			t.Fatal("oversize total")
		}
		b.transcript.total = -1
		if _, ok := b.ownerProbeCreateHistory(g); ok {
			t.Fatal("negative total")
		}
		b.transcript.total = 0
		b.transcript.entries = []transcriptEntry{{}}
		if _, ok := b.ownerProbeCreateHistory(g); ok {
			t.Fatal("entry exceeds total")
		}
		b.transcript.total = 1
		if _, ok := b.ownerProbeCreateHistory(g); ok {
			t.Fatal("unknown entry")
		}
		b.transcript.entries[0].record = map[string]any{"kind": "notification_response", "body": nil}
		if _, ok := b.ownerProbeCreateHistory(g); ok {
			t.Fatal("unreadable response body")
		}
		b.transcript = nil
		if _, ok := b.ownerProbeCreateHistory(g); ok {
			t.Fatal("nil transcript")
		}
	})
	t.Run("latest-load-only", func(t *testing.T) {
		b, g, inv := createHistoryFixture(t, cid)
		old := &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: inv.id, command: "PROG_LOAD", state: "prepared", attributes: []byte{1}, license: 42}
		attr := make([]byte, 24)
		guardPut32(attr, 0, unix.BPF_PROG_TYPE_CGROUP_DEVICE)
		newest := &runtimeEffectTransaction{id: 2, grantID: g.id, invocationID: inv.id, command: "PROG_LOAD", state: "retired", attributes: attr, license: "MIT"}
		b.effects.transactions[2], b.effects.transactions[1] = newest, old
		p, ok := b.ownerProbeCreateHistory(g)
		if !ok || p[6] != 3 || p[16] != 2 || p[23] != 1 {
			t.Fatalf("latest load %v %v", p, ok)
		}
		old.state = "bad"
		if _, ok := b.ownerProbeCreateHistory(g); ok {
			t.Fatal("bad earlier state")
		}
		old.state = "prepared"
		old.result = json.Number("1")
		if _, ok := b.ownerProbeCreateHistory(g); ok {
			t.Fatal("bad earlier result")
		}
		old.result = nil
		for _, attributes := range [][]byte{nil, make([]byte, 23), make([]byte, 4097)} {
			newest.attributes = attributes
			if _, ok := b.ownerProbeCreateHistory(g); ok {
				t.Fatal("bad latest length")
			}
		}
		newest.attributes, newest.license = attr, 42
		if _, ok := b.ownerProbeCreateHistory(g); ok {
			t.Fatal("bad latest license")
		}
	})
	t.Run("whole-transcript-and-identity", func(t *testing.T) {
		b, g, inv := createHistoryFixture(t, cid)
		tx := &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: inv.id, requesterCookie: 4, notificationID: 5, command: "PROG_QUERY", state: "effect_observed", result: json.Number("0")}
		b.effects.transactions[1] = tx
		body := map[string]any{"transaction_id": jsonU64(1), "invocation_id": jsonU64(inv.id), "requester_cookie": jsonU64(4), "notification_id": jsonU64(5), "operation": "SEND", "response": map[string]any{"value": jsonI64(0), "error": jsonI64(0), "flags": jsonU64(0)}, "ioctl_errno": jsonU64(0), "returned_fd": nil, "guard_before": jsonU64(1), "guard_after": jsonU64(2)}
		unrelated := map[string]any{"transaction_id": jsonU64(2), "notification_id": jsonU64(5)}
		if err := appendRuntimeRecord(b, "notification_response", unrelated); err != nil {
			t.Fatal(err)
		}
		if err := appendRuntimeRecord(b, "notification_response", body); err != nil {
			t.Fatal(err)
		}
		originalTx := *tx
		originalRaw := append([]byte(nil), b.transcript.entries[1].raw...)
		originalTotal, originalTail := b.transcript.total, b.transcript.tail
		p, ok := b.ownerProbeCreateHistory(g)
		if !ok || p[47] != 2 {
			t.Fatal("response identity", p, ok)
		}
		if !reflect.DeepEqual(*tx, originalTx) || b.effects.transactions[1] != tx || len(b.transcript.entries) != 2 || b.transcript.total != originalTotal || b.transcript.tail != originalTail || !reflect.DeepEqual(b.transcript.entries[1].raw, originalRaw) || !reflect.DeepEqual(b.transcript.entries[1].record["body"], body) {
			t.Fatal("history changed retained graph")
		}
		for range 4097 {
			if _, _, err := b.transcript.append("audit", map[string]any{}); err != nil {
				t.Fatal(err)
			}
		}
		if _, ok := b.ownerProbeCreateHistory(g); !ok {
			t.Fatal("entry count is not transaction count")
		}
		if err := appendRuntimeRecord(b, "notification_response", body); err != nil {
			t.Fatal(err)
		}
		if _, ok := b.ownerProbeCreateHistory(g); ok {
			t.Fatal("tail duplicate")
		}
	})
	t.Run("actual-transcript-byte-limit", func(t *testing.T) {
		b, g, _ := createHistoryFixture(t, cid)
		chunk := strings.Repeat("x", 8192)
		for maxAuthorityTranscriptBytes-b.transcript.total > 16384 {
			if _, _, err := b.transcript.append("audit", map[string]any{"padding": chunk}); err != nil {
				t.Fatal(err)
			}
		}
		body := map[string]any{"padding": ""}
		record := map[string]any{"version": json.Number("1"), "boot_id": b.transcript.bootID, "epoch": b.transcript.epoch, "sequence": jsonU64(uint64(len(b.transcript.entries)) + 1), "previous": b.transcript.tail, "kind": "audit", "body": body}
		raw, err := canonicalControl(record)
		if err != nil {
			t.Fatal(err)
		}
		remaining := maxAuthorityTranscriptBytes - b.transcript.total - len(raw) - 1
		if remaining < 0 || remaining > 8192*2 {
			t.Fatal("invalid fixture remainder", remaining)
		}
		body["padding"] = strings.Repeat("x", remaining)
		if _, _, err := b.transcript.append("audit", body); err != nil {
			t.Fatal(err)
		}
		if b.transcript.total != maxAuthorityTranscriptBytes {
			t.Fatal("transcript not exactly full", b.transcript.total)
		}
		if _, ok := b.ownerProbeCreateHistory(g); !ok {
			t.Fatal("full retained transcript rejected")
		}
		if _, _, err := b.transcript.append("audit", map[string]any{}); err == nil {
			t.Fatal("transcript overflow accepted")
		}
	})
	for _, tc := range []struct {
		command string
		slot    int
	}{{"MAP_CREATE", 0}, {"PROG_LOAD", 1}, {"PROG_GET_FD_BY_ID", 3}} {
		t.Run("fd-command-"+tc.command, func(t *testing.T) {
			for _, ioctlErr := range []uint64{0, 2} {
				b, g, inv := createHistoryFixture(t, cid)
				tx := &runtimeEffectTransaction{id: 1, grantID: g.id, invocationID: inv.id, requesterCookie: 4, notificationID: 5, command: tc.command, state: "effect_observed", result: json.Number("7")}
				if tc.command == "PROG_LOAD" {
					tx.attributes = make([]byte, 24)
					guardPut32(tx.attributes, 0, unix.BPF_PROG_TYPE_CGROUP_DEVICE)
					tx.license = "Apache"
				}
				b.effects.transactions[1] = tx
				var returned any
				if ioctlErr == 0 {
					returned = jsonU64(9)
				}
				body := map[string]any{"transaction_id": jsonU64(1), "invocation_id": jsonU64(inv.id), "requester_cookie": jsonU64(4), "notification_id": jsonU64(5), "operation": "ADDFD_SEND", "response": nil, "ioctl_errno": jsonU64(ioctlErr), "returned_fd": returned, "guard_before": jsonU64(1), "guard_after": jsonU64(2)}
				if err := appendRuntimeRecord(b, "notification_response", body); err != nil {
					t.Fatal(err)
				}
				p, ok := b.ownerProbeCreateHistory(g)
				want := byte(4)
				if ioctlErr != 0 {
					want = 5
				}
				if !ok || p[15+8*tc.slot] != want {
					t.Fatal("observed before returnedFD transition", p, ok)
				}
				tx.state, tx.returnedFD = "response_settled", returned
				if _, ok := b.ownerProbeCreateHistory(g); !ok {
					t.Fatal("settled returnedFD")
				}
				tx.state = "retired"
				if _, ok := b.ownerProbeCreateHistory(g); !ok {
					t.Fatal("retired returnedFD")
				}
				b2, g2, inv2 := createHistoryFixture(t, cid)
				tx2 := &runtimeEffectTransaction{id: 1, grantID: g2.id, invocationID: inv2.id, requesterCookie: 4, notificationID: 5, command: tc.command, state: "effect_observed", result: json.Number("7")}
				if tc.command == "PROG_LOAD" {
					tx2.attributes = make([]byte, 24)
					guardPut32(tx2.attributes, 0, unix.BPF_PROG_TYPE_CGROUP_DEVICE)
					tx2.license = "Apache"
				}
				b2.effects.transactions[1] = tx2
				send := map[string]any{"transaction_id": jsonU64(1), "invocation_id": jsonU64(inv2.id), "requester_cookie": jsonU64(4), "notification_id": jsonU64(5), "operation": "SEND", "response": map[string]any{"value": jsonI64(0), "error": jsonI64(-1), "flags": jsonU64(0)}, "ioctl_errno": jsonU64(ioctlErr), "returned_fd": nil, "guard_before": jsonU64(1), "guard_after": jsonU64(2)}
				if err := appendRuntimeRecord(b2, "notification_response", send); err != nil {
					t.Fatal(err)
				}
				p2, ok := b2.ownerProbeCreateHistory(g2)
				wantSend := byte(2)
				if ioctlErr != 0 {
					wantSend = 3
				}
				if !ok || p2[15+8*tc.slot] != wantSend {
					t.Fatal("FD SEND fallback", p2, ok)
				}
			}
		})
	}
}

func TestOwnerProbeRemovalFollowup(t *testing.T) {
	fixture := func() (*authorityBroker, *pendingBrokerSettlement, *objectIncarnation) {
		g := &brokerGrant{id: 9}
		o := &objectIncarnation{cookie: 13, id: 17, ownerTransaction: 5, publicationSeq: 3, class: "program", objectType: unix.BPF_PROG_TYPE_CGROUP_DEVICE, state: "live", removalAuthorized: true}
		b := controlDiagnosticFixture()
		b.grants = map[uint64]*brokerGrant{9: g}
		b.effects = &runtimeEffects{objects: &objectTracker{byCookie: map[uint64]*objectIncarnation{13: o}}, objectGrant: map[uint64]uint64{13: 9}, transactions: make(map[uint64]*runtimeEffectTransaction), grantCleanup: make(map[uint64]*grantObjectCleanup), attachments: make(map[uint64]*runtimeAttachment)}
		b.checkpoint.lifecycle = &checkpointLifecycle{objects: map[uint64]*deferredObject{13: {kernelID: 17, class: "program", removalAllowed: true}}}
		b.transcript = newAuthorityTranscript("boot", "epoch")
		q := &pendingBrokerSettlement{grant: g, followupGrant: g, followupObject: o, followupCookie: 13, followupID: 17, followupCreator: 5, followupClass: "program", lastWait: 10, diagnostic: ownerProbeSettlementReceipt{cid: strings.Repeat("a", 64), purpose: 1, valid: true}}
		return b, q, o
	}
	t.Run("identity-and-once", func(t *testing.T) {
		b, q, o := fixture()
		want := ownerProbeRemovalFollowup{1, 10, 1, 1, 1, 1, 5, 1, 1, 1, 3, 8}
		if got := b.ownerProbeRemovalFollowup(q, 1); got != want {
			t.Fatalf("got %v want %v", got, want)
		}
		q.followup(b, 1)
		q.followup(b, 2)
		if q.diagnosticEvents != 1<<5 {
			t.Fatal("followup must reserve once")
		}
		clone := *o
		b.effects.objects.byCookie[13] = &clone
		if got := b.ownerProbeRemovalFollowup(q, 1); got[2] != 3 {
			t.Fatalf("replacement accepted %v", got)
		}
		delete(b.effects.objects.byCookie, 13)
		if got := b.ownerProbeRemovalFollowup(q, 1); got[2] != 2 {
			t.Fatalf("missing object %v", got)
		}
	})
	for _, tc := range []struct {
		name   string
		change func(*authorityBroker, *pendingBrokerSettlement, *objectIncarnation)
	}{
		{"grant-pointer", func(b *authorityBroker, q *pendingBrokerSettlement, _ *objectIncarnation) {
			clone := *q.grant
			b.grants[9] = &clone
		}},
		{"grant-owner", func(b *authorityBroker, _ *pendingBrokerSettlement, _ *objectIncarnation) {
			b.effects.objectGrant[13] = 10
		}},
		{"kernel-id", func(_ *authorityBroker, _ *pendingBrokerSettlement, o *objectIncarnation) { o.id++ }},
		{"class", func(_ *authorityBroker, _ *pendingBrokerSettlement, o *objectIncarnation) { o.class = "map" }},
		{"creator", func(_ *authorityBroker, _ *pendingBrokerSettlement, o *objectIncarnation) { o.ownerTransaction++ }},
	} {
		t.Run("identity-"+tc.name, func(t *testing.T) {
			b, q, o := fixture()
			tc.change(b, q, o)
			got := b.ownerProbeRemovalFollowup(q, 1)
			if got[2] != 3 {
				t.Fatalf("identity mismatch %v", got)
			}
			for _, v := range got[3:] {
				if v != 0 {
					t.Fatalf("identity-dependent field %v", got)
				}
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*authorityBroker, *pendingBrokerSettlement, *objectIncarnation)
		index  int
		want   byte
	}{
		{"lifecycle-missing", func(b *authorityBroker, _ *pendingBrokerSettlement, _ *objectIncarnation) {
			delete(b.checkpoint.lifecycle.objects, 13)
		}, 6, 1},
		{"lifecycle-unavailable", func(b *authorityBroker, _ *pendingBrokerSettlement, _ *objectIncarnation) {
			b.checkpoint.lifecycle = nil
		}, 6, 2},
		{"lifecycle-mismatch", func(b *authorityBroker, _ *pendingBrokerSettlement, _ *objectIncarnation) {
			b.checkpoint.lifecycle.objects[13].kernelID++
		}, 6, 3},
		{"lifecycle-pending-unauthorized", func(b *authorityBroker, _ *pendingBrokerSettlement, _ *objectIncarnation) {
			b.checkpoint.lifecycle.objects[13].removalAllowed = false
		}, 6, 4},
		{"lifecycle-removed-unauthorized", func(b *authorityBroker, _ *pendingBrokerSettlement, _ *objectIncarnation) {
			b.checkpoint.lifecycle.objects[13].removalAllowed = false
			b.checkpoint.lifecycle.objects[13].removed = true
		}, 6, 6},
		{"tracker-inconsistent", func(_ *authorityBroker, _ *pendingBrokerSettlement, o *objectIncarnation) { o.removalSeq = 11 }, 5, 3},
		{"lsm", func(_ *authorityBroker, _ *pendingBrokerSettlement, o *objectIncarnation) {
			o.objectType = unix.BPF_PROG_TYPE_LSM
		}, 10, 2},
		{"other-program", func(_ *authorityBroker, _ *pendingBrokerSettlement, o *objectIncarnation) {
			o.objectType = unix.BPF_PROG_TYPE_SOCKET_FILTER
		}, 10, 4},
		{"map", func(_ *authorityBroker, q *pendingBrokerSettlement, o *objectIncarnation) {
			o.class, q.followupClass = "map", "map"
		}, 10, 1},
		{"unknown-class", func(_ *authorityBroker, q *pendingBrokerSettlement, o *objectIncarnation) {
			o.class, q.followupClass = "other", "other"
		}, 10, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, q, o := fixture()
			tc.change(b, q, o)
			got := b.ownerProbeRemovalFollowup(q, 1)
			if got[tc.index] != tc.want {
				t.Fatalf("projection %v", got)
			}
		})
	}
	t.Run("accepted-and-retained-records", func(t *testing.T) {
		b, q, o := fixture()
		o.state, o.removalSeq = "id_removed", 11
		b.checkpoint.lifecycle.objects[13].removed = true
		cleanup := &grantObjectCleanup{grantID: 9, creatorID: 5, cookie: 13, removalSequence: 11}
		b.effects.grantCleanup[13] = cleanup
		b.effects.transactions[5] = &runtimeEffectTransaction{id: 5, grantID: 9, invocationID: 6, requesterCookie: 7, notificationID: 8, command: "PROG_LOAD", state: "retired", result: jsonI64(12), returnedFD: jsonU64(12), publication: &guardRecord{Kind: 1, ObjectCookie: 13, KernelID: 17, Sequence: 3}}
		for _, record := range []struct {
			kind string
			body map[string]any
		}{
			{"grant_object_cleanup", grantCleanupBody(cleanup, 1, "close_ref", 0, nil)},
			{"guard", map[string]any{"record": map[string]any{"kind": jsonU64(2), "object_cookie": jsonU64(13), "kernel_id": jsonU64(17), "sequence": jsonU64(11), "result": jsonI64(0)}}},
			{"grant_object_cleanup", grantCleanupBody(cleanup, 2, "await_id_removed", 0, jsonU64(11))},
			{"notification_response", map[string]any{"transaction_id": jsonU64(5), "invocation_id": jsonU64(6), "requester_cookie": jsonU64(7), "notification_id": jsonU64(8), "operation": "ADDFD_SEND", "response": nil, "ioctl_errno": jsonU64(0), "returned_fd": jsonU64(12), "guard_before": jsonU64(1), "guard_after": jsonU64(2)}},
		} {
			if _, _, err := b.transcript.append(record.kind, record.body); err != nil {
				t.Fatal(err)
			}
		}
		got := b.ownerProbeRemovalFollowup(q, 2)
		if got[1] != 0 || got[3] != 2 || got[4] != 2 || got[5] != 2 || got[6] != 7 || got[7] != 3 || got[8] != 2 {
			t.Fatalf("accepted record %v", got)
		}
		if got[11] != 2 {
			t.Fatalf("creator response %v", got)
		}
		b.effects.attachments = make(map[uint64]*runtimeAttachment, 4097)
		for i := range 4097 {
			id := uint64(i + 1)
			b.effects.attachments[id] = &runtimeAttachment{id: id, grantID: 99}
		}
		if limited := b.ownerProbeRemovalFollowup(q, 1); limited[9] != 7 || limited[8] == 2 {
			t.Fatalf("truncated attachments accepted %v", limited)
		}
		b.effects.attachments = nil
		delete(b.effects.transactions, 5)
		if missing := b.ownerProbeRemovalFollowup(q, 1); missing[11] != 5 || missing[8] == 2 {
			t.Fatalf("unattributed response accepted %v", missing)
		}
		// A copied record published twice is ambiguous, never accepted.
		if _, _, err := b.transcript.append("grant_object_cleanup", grantCleanupBody(cleanup, 1, "close_ref", 0, nil)); err != nil {
			t.Fatal(err)
		}
		got = b.ownerProbeRemovalFollowup(q, 1)
		if got[3] != 6 || got[7] != 5 || got[8] == 2 {
			t.Fatalf("duplicate %v", got)
		}
	})
	t.Run("partial-guard-tracker-lifecycle", func(t *testing.T) {
		b, q, o := fixture()
		guard := map[string]any{"record": map[string]any{"kind": jsonU64(2), "object_cookie": jsonU64(13), "kernel_id": jsonU64(17), "sequence": jsonU64(11), "result": jsonI64(0)}}
		if _, _, err := b.transcript.append("guard", guard); err != nil {
			t.Fatal(err)
		}
		if got := b.ownerProbeRemovalFollowup(q, 1); got[4] != 2 || got[5] != 1 || got[6] != 5 || got[8] == 2 {
			t.Fatalf("guard only %v", got)
		}
		o.state, o.removalSeq = "id_removed", 11
		if got := b.ownerProbeRemovalFollowup(q, 1); got[4] != 2 || got[5] != 2 || got[6] != 5 || got[8] == 2 {
			t.Fatalf("tracker before lifecycle %v", got)
		}
		cleanup := &grantObjectCleanup{grantID: 9, creatorID: 5, cookie: 13, removalSequence: 11}
		b.effects.grantCleanup[13] = cleanup
		if _, _, err := b.transcript.append("grant_object_cleanup", grantCleanupBody(cleanup, 1, "close_ref", 0, nil)); err != nil {
			t.Fatal(err)
		}
		if got := b.ownerProbeRemovalFollowup(q, 1); got[3] != 2 || got[7] != 2 || got[8] == 2 {
			t.Fatalf("close without step two %v", got)
		}
		b.checkpoint.lifecycle.objects[13].removed = true
		if got := b.ownerProbeRemovalFollowup(q, 1); got[6] != 7 || got[8] == 2 {
			t.Fatalf("missing cleanup record accepted %v", got)
		}
	})
	t.Run("creator-response-identity", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			errno    uint64
			returned any
			recorded any
			want     byte
		}{
			{"addfd-ok", 0, jsonU64(12), jsonU64(12), 2},
			{"addfd-enoent", 2, nil, nil, 3},
			{"return-mismatch", 0, jsonU64(12), jsonU64(13), 5},
		} {
			t.Run(tc.name, func(t *testing.T) {
				b, q, _ := fixture()
				b.effects.transactions[5] = &runtimeEffectTransaction{id: 5, grantID: 9, invocationID: 6, requesterCookie: 7, notificationID: 8, command: "PROG_LOAD", state: "retired", result: jsonI64(12), returnedFD: tc.recorded, publication: &guardRecord{Kind: 1, ObjectCookie: 13, KernelID: 17, Sequence: 3}}
				body := map[string]any{"transaction_id": jsonU64(5), "invocation_id": jsonU64(6), "requester_cookie": jsonU64(7), "notification_id": jsonU64(8), "operation": "ADDFD_SEND", "response": nil, "ioctl_errno": jsonU64(tc.errno), "returned_fd": tc.returned, "guard_before": jsonU64(1), "guard_after": jsonU64(2)}
				if _, _, err := b.transcript.append("notification_response", body); err != nil {
					t.Fatal(err)
				}
				if got := b.ownerProbeRemovalFollowup(q, 1); got[11] != tc.want {
					t.Fatalf("response %v", got)
				}
				body["requester_cookie"] = jsonU64(99)
				if got := b.ownerProbeRemovalFollowup(q, 1); got[11] != 5 {
					t.Fatalf("conflicting identity %v", got)
				}
			})
		}
	})
	t.Run("pin-and-last-wait", func(t *testing.T) {
		b, q, o := fixture()
		q.followupObject, q.followupGrant = nil, nil
		q.sampleRemoval(0, b, 13, o)
		if q.followupObject != nil || q.lastWait != 10 {
			t.Fatal("premature pin")
		}
		q.sampleRemoval(1_000_000_000, b, 13, o)
		if q.followupObject != o || q.followupGrant != q.grant {
			t.Fatal("sample did not pin")
		}
		q.sample(2_000_000_000, 11)
		if q.lastWait != 11 {
			t.Fatal("consumed sample left stale wait")
		}
		if d := b.ownerProbeRemovalFollowup(q, 2); d[1] != 0 {
			t.Fatal("committed boundary leaked wait", d)
		}
	})
	t.Run("borrowed-state-unchanged", func(t *testing.T) {
		b, q, o := fixture()
		file, err := os.Open("/dev/null")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		fd := int(file.Fd())
		b.effects.objectFD = map[uint64]int{13: fd}
		before := *o
		beforeItem := *b.checkpoint.lifecycle.objects[13]
		beforeFD, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			t.Fatal(err)
		}
		beforeFL, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil {
			t.Fatal(err)
		}
		var beforeStat, afterStat unix.Stat_t
		if err := unix.Fstat(fd, &beforeStat); err != nil {
			t.Fatal(err)
		}
		_ = b.ownerProbeRemovalFollowup(q, 1)
		afterFD, e1 := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		afterFL, e2 := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err := unix.Fstat(fd, &afterStat); err != nil || e1 != nil || e2 != nil || beforeFD != afterFD || beforeFL != afterFL || beforeStat.Dev != afterStat.Dev || beforeStat.Ino != afterStat.Ino || beforeStat.Mode != afterStat.Mode {
			t.Fatal("borrowed FD changed")
		}
		if !reflect.DeepEqual(before, *o) || !reflect.DeepEqual(beforeItem, *b.checkpoint.lifecycle.objects[13]) || b.effects.objectFD[13] != fd || b.effects.objects.byCookie[13] != o {
			t.Fatal("followup changed retained state")
		}
	})
	t.Run("attachments-and-bounds", func(t *testing.T) {
		b, q, _ := fixture()
		b.effects.attachments[1] = &runtimeAttachment{id: 1, grantID: 9, programCookie: 13, state: "attached"}
		b.effects.attachments[2] = &runtimeAttachment{id: 2, grantID: 9, programCookie: 13, state: "released", offlineSeq: 4, releaseSeq: 5}
		if got := b.ownerProbeRemovalFollowup(q, 1); got[9] != 5 {
			t.Fatalf("mixed attachments %v", got)
		}
		for i := uint64(3); i <= 4097; i++ {
			b.effects.attachments[i] = &runtimeAttachment{id: i, grantID: 99}
		}
		if got := b.ownerProbeRemovalFollowup(q, 1); got[9] != 7 {
			t.Fatalf("attachment cap %v", got)
		}
		b, q, _ = fixture()
		for i := range 4096 {
			if _, _, err := b.transcript.append("principal", map[string]any{"n": jsonU64(uint64(i))}); err != nil {
				t.Fatal(err)
			}
		}
		if got := b.ownerProbeRemovalFollowup(q, 1); got[3] != 1 || got[4] != 1 || got[7] != 1 {
			t.Fatalf("exact entry limit %v", got)
		}
		if _, _, err := b.transcript.append("principal", map[string]any{"n": jsonU64(4096)}); err != nil {
			t.Fatal(err)
		}
		if got := b.ownerProbeRemovalFollowup(q, 1); got[3] != 7 || got[4] != 5 || got[7] != 6 || got[11] != 8 || got[8] == 2 {
			t.Fatalf("transcript cap %v", got)
		}
		b, q, _ = fixture()
		b.transcript.entries = []transcriptEntry{{record: map[string]any{"kind": "principal", "body": map[string]any{}}, raw: make([]byte, maxAuthorityTranscriptBytes-1)}}
		b.transcript.total = maxAuthorityTranscriptBytes
		if got := b.ownerProbeRemovalFollowup(q, 1); got[3] != 1 || got[4] != 1 {
			t.Fatalf("exact byte limit %v", got)
		}
		b.transcript.total++
		if got := b.ownerProbeRemovalFollowup(q, 1); got[3] != 5 || got[4] != 3 || got[8] != 3 {
			t.Fatalf("invalid byte total %v", got)
		}
	})
	t.Run("packet-closed-schema", func(t *testing.T) {
		valid := [16]byte{7, 1, 1, 5, 1, 10, 1, 1, 1, 1, 5, 1, 1, 1, 3, 1}
		if !ownerProbeSettlementPacketValid(valid[:]) {
			t.Fatal("valid followup rejected")
		}
		for _, change := range []func(*[16]byte){func(p *[16]byte) { p[0] = 6 }, func(p *[16]byte) { p[4] = 0 }, func(p *[16]byte) { p[6] = 2 }, func(p *[16]byte) { p[7] = 9 }, func(p *[16]byte) { p[12] = 2 }} {
			bad := valid
			change(&bad)
			if ownerProbeSettlementPacketValid(bad[:]) {
				t.Fatalf("invalid packet accepted %v", bad)
			}
		}
		accepted := [16]byte{7, 1, 1, 5, 1, 10, 1, 2, 2, 2, 7, 3, 2, 1, 3, 2}
		if !ownerProbeSettlementPacketValid(accepted[:]) {
			t.Fatal("complete acceptance rejected")
		}
		for _, tc := range []struct {
			offset int
			value  byte
		}{{13, 6}, {13, 7}, {13, 8}, {14, 5}, {14, 6}, {15, 1}, {15, 5}, {15, 6}, {15, 7}, {15, 8}} {
			bad := accepted
			bad[tc.offset] = tc.value
			if ownerProbeSettlementPacketValid(bad[:]) {
				t.Fatalf("accepted incomplete evidence %v", bad)
			}
		}
	})
}

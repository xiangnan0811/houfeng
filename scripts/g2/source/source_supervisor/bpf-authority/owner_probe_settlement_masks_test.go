//go:build linux && amd64 && !cgo

package main

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOwnerProbeSettlementActualWaitMasks(t *testing.T) {
	pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	type fixture struct {
		b *authorityBroker
		g *brokerGrant
		q *pendingBrokerSettlement
	}
	newFixture := func() fixture {
		b := controlDiagnosticFixture()
		b.bootOwner = 1
		g := &brokerGrant{id: 2, owner: 1, generation: 1, purpose: "postgres-uid", containerID: strings.Repeat("a", 64), phase: "settling", outcome: "completed", startObserved: true}
		p := &controlPrincipal{ID: 1, Role: "BOOT_OWNER", PIDFD: pidfd}
		b.grants = map[uint64]*brokerGrant{g.id: g}
		b.effects = &runtimeEffects{objects: &objectTracker{byCookie: make(map[uint64]*objectIncarnation)}, registry: &effectRegistry{targets: make(map[cgroupKey]*effectRegistryTarget)}}
		s := &ownerProbeEvidenceState{broker: b}
		b.diagnostic = s
		s.issue(b, g)
		q := &pendingBrokerSettlement{grant: g, principal: p, evidence: s.grantFor(g), evidenceState: s}
		b.pendingSettlement = q
		return fixture{b, g, q}
	}
	for _, tc := range []struct {
		name           string
		point          byte
		known, pending uint64
		setup          func(fixture)
	}{
		{"effect", 1, 1, 1, func(f fixture) { f.b.effects.pending = &runtimeEffectTransaction{} }},
		{"alias", 2, 3, 2, func(f fixture) { f.b.effects.aliases = map[uint64]*brokerBorrowedAlias{1: {grantID: f.g.id}} }},
		{"cgroup-unobserved", 4, 47, 8, func(f fixture) {
			f.b.effects.cgroups = map[uint64]*liveCgroup{f.g.id: {GrantID: f.g.id, RootCookie: 20, KernfsID: 21, Enrollment: 22}}
		}},
		{"retire-eagain", 5, 63, 16, func(f fixture) {
			f.b.effects.cgroups = map[uint64]*liveCgroup{f.g.id: {GrantID: f.g.id, RootCookie: 20, KernfsID: 21, Enrollment: 22, OfflineSeq: 1, ReleaseSeq: 2}}
		}},
		{"cgroup-release", 6, 47, 32, func(f fixture) {
			f.b.effects.cgroups = map[uint64]*liveCgroup{f.g.id: {GrantID: f.g.id, RootCookie: 20, KernfsID: 21, Enrollment: 22}}
			f.b.effects.registry.targets[cgroupKey{20, 21}] = &effectRegistryTarget{phase: 1}
		}},
		{"invocation", 7, 111, 64, func(f fixture) {
			f.b.runtimeInvocations = map[int]*runtimeInvocation{3: {grantID: f.g.id, state: "running", pidfd: -1}}
		}},
		{"exec", 8, 239, 128, func(f fixture) { f.g.exec = &brokerExecAdmission{} }},
		{"transaction", 9, 495, 256, func(f fixture) {
			f.b.effects.transactions = map[uint64]*runtimeEffectTransaction{1: {grantID: f.g.id, state: "committed"}}
		}},
		{"removal", 10, 1007, 512, func(f fixture) {
			f.b.effects.objectGrant = map[uint64]uint64{11: f.g.id}
			f.b.effects.objects.byCookie[11] = &objectIncarnation{cookie: 11, id: 12}
		}},
		{"attachment", 11, 2031, 1024, func(f fixture) {
			f.b.effects.attachments = map[uint64]*runtimeAttachment{1: {grantID: f.g.id, state: "attached"}}
		}},
		{"probe-result", 12, 4079, 2048, func(f fixture) {
			f.g.probe = &ownerProbeObservation{}
			f.g.probeResult = make(chan ownerProbeResult, 1)
		}},
		{"audit", 13, 8175, 4096, func(f fixture) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			tc.setup(f)
			calls := 0
			clock := func() (uint64, error) { return 1, nil }
			retire := func(enrollment uint64) (guardRetirement, error) {
				calls++
				if enrollment != 22 {
					t.Fatal("wrong enrollment")
				}
				return guardRetirement{}, unix.EAGAIN
			}
			if _, _, done, err := f.b.advanceBrokerSettlementUsing(clock, retire, nil); err != nil || done {
				t.Fatalf("advance: done=%v err=%v", done, err)
			}
			if f.q.lastWait != tc.point || f.q.knownPredicates != tc.known || f.q.pendingPredicates != tc.pending {
				t.Fatalf("point=%d known=%x pending=%x", f.q.lastWait, f.q.knownPredicates, f.q.pendingPredicates)
			}
			if f.q.evidence.known != tc.known || f.q.evidence.pending != tc.pending {
				t.Fatal("snapshot projection diverged from actual evaluation")
			}
			if (tc.point == 5) != (calls == 1) {
				t.Fatalf("unexpected retire calls: %d", calls)
			}
		})
	}
	t.Run("retire-error-is-not-eagain", func(t *testing.T) {
		f := newFixture()
		f.b.effects.cgroups = map[uint64]*liveCgroup{f.g.id: {GrantID: f.g.id, RootCookie: 20, KernfsID: 21, Enrollment: 22, OfflineSeq: 1, ReleaseSeq: 2}}
		_, _, done, err := f.b.advanceBrokerSettlementUsing(func() (uint64, error) { return 1, nil }, func(uint64) (guardRetirement, error) { return guardRetirement{}, unix.EINVAL }, nil)
		if err == nil || done || f.q.knownPredicates != 63 || f.q.pendingPredicates != 0 || f.q.lastWait != 0 {
			t.Fatal("non-EAGAIN retirement was reported as a wait")
		}
	})
}

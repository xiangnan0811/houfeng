//go:build linux && amd64 && !cgo

package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// These tests substitute only the guard read and BPF-info boundaries. The
// isolated tracker, admission checks, transcript, and pipe FD are real; the
// fixtures are not evidence of kernel BPF authority or native guard events.
func TestOwnerProbeSettlementCleanupBoundary(t *testing.T) {
	type fixture struct {
		b             *authorityBroker
		g             *brokerGrant
		o             *objectIncarnation
		item          *deferredObject
		fd, writeFD   int
		drains, infos int
	}
	newFixture := func(t *testing.T) *fixture {
		t.Helper()
		var pipe [2]int
		if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
			t.Fatal(err)
		}
		f := &fixture{fd: pipe[0], writeFD: pipe[1]}
		t.Cleanup(func() {
			if f.fd >= 0 {
				_ = unix.Close(f.fd)
			}
			_ = unix.Close(f.writeFD)
		})
		f.b = controlDiagnosticFixture()
		f.b.bootOwner = 1
		f.g = &brokerGrant{id: 2, owner: 1, generation: 1, purpose: "postgres-uid", containerID: strings.Repeat("a", 64), phase: "settling", outcome: "completed"}
		f.b.grants = map[uint64]*brokerGrant{f.g.id: f.g}
		f.o = &objectIncarnation{cookie: 11, id: 12, ownerTransaction: 13, publicationSeq: 7, class: "map", objectType: unix.BPF_MAP_TYPE_ARRAY, attributes: map[string]any{"name": "fixture"}, state: "live"}
		f.item = &deferredObject{kernelID: 12, class: "map"}
		f.b.checkpoint.lifecycle = &checkpointLifecycle{objects: map[uint64]*deferredObject{11: f.item}}
		f.b.resolver, f.b.join = &brokerAuditResolver{}, &auditJoin{}
		f.b.effects = &runtimeEffects{
			objects:     &objectTracker{byCookie: map[uint64]*objectIncarnation{11: f.o}},
			objectGrant: map[uint64]uint64{11: f.g.id}, objectFD: map[uint64]int{11: f.fd},
			transactions: map[uint64]*runtimeEffectTransaction{13: {id: 13, grantID: f.g.id, state: "retired", effectExecuted: true, effectFD: f.fd, returnedFD: jsonU64(8)}},
		}
		return f
	}
	callbacks := func(f *fixture) (func() error, func(int, uint64, string, uint32) (uint32, map[string]any, error)) {
		return func() error { f.drains++; return nil }, func(fd int, cookie uint64, class string, typ uint32) (uint32, map[string]any, error) {
			f.infos++
			if fd != f.fd || cookie != 11 || class != "map" || typ != unix.BPF_MAP_TYPE_ARRAY {
				return 0, nil, errGuardProtocol
			}
			return 12, f.o.attributes, nil
		}
	}
	t.Run("real-helper-success", func(t *testing.T) {
		f := newFixture(t)
		drain, info := callbacks(f)
		if err := f.b.beginGrantObjectCleanupUsing(f.g, 11, drain, info); err != nil {
			t.Fatal(err)
		}
		if _, err := unix.FcntlInt(uintptr(f.fd), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
			t.Fatalf("FD was not closed: %v", err)
		}
		f.fd = -1
		if f.drains != 1 || f.infos != 1 || !f.o.removalAuthorized || !f.item.removalAllowed || f.b.effects.grantCleanup[11] == nil || len(f.b.transcript.entries) != 1 {
			t.Fatal("cleanup facts were not committed")
		}
		if _, present := f.b.effects.objectFD[11]; present {
			t.Fatal("closed FD remained registered")
		}
	})
	for _, tc := range []struct {
		name   string
		change func(*fixture) *brokerGrant
	}{
		{"wrong-grant-source", func(f *fixture) *brokerGrant { clone := *f.g; return &clone }},
		{"invalid-fd", func(f *fixture) *brokerGrant { f.b.effects.objectFD[11] = -1; return f.g }},
		{"invalid-object-state", func(f *fixture) *brokerGrant { f.o.state = "id_removed"; return f.g }},
		{"invalid-creator-state", func(f *fixture) *brokerGrant { f.b.effects.transactions[13].state = "committed"; return f.g }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			grant := tc.change(f)
			drain, info := callbacks(f)
			if err := f.b.beginGrantObjectCleanupUsing(grant, 11, drain, info); err == nil {
				t.Fatal("invalid cleanup was accepted")
			}
			if f.drains != 0 || f.infos != 0 || f.o.removalAuthorized || f.item.removalAllowed || f.b.effects.grantCleanup[11] != nil || len(f.b.transcript.entries) != 0 {
				t.Fatal("rejected cleanup changed authority state")
			}
			if _, err := unix.FcntlInt(uintptr(f.fd), unix.F_GETFD, 0); err != nil {
				t.Fatal("rejected cleanup closed the FD")
			}
		})
	}
	t.Run("advance-point-three", func(t *testing.T) {
		f := newFixture(t)
		pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pidfd)
		p := &controlPrincipal{ID: 1, Role: "BOOT_OWNER", PIDFD: pidfd}
		s := &ownerProbeEvidenceState{broker: f.b}
		f.b.diagnostic = s
		s.issue(f.b, f.g)
		s.publish(f.g, f.o, guardRecord{Sequence: 7, Kind: 3, ObjectCookie: 11, KernelID: 12})
		q := &pendingBrokerSettlement{principal: p, grant: f.g, evidence: s.grantFor(f.g), evidenceState: s}
		f.b.pendingSettlement = q
		drain, info := callbacks(f)
		cleanup := func(g *brokerGrant, cookie uint64) error {
			return f.b.beginGrantObjectCleanupUsing(g, cookie, drain, info)
		}
		_, _, done, err := f.b.advanceBrokerSettlementUsing(func() (uint64, error) { return 1, nil }, nil, cleanup)
		if err != nil || done || q.lastWait != 3 || q.knownPredicates != 7 || q.pendingPredicates != 4 {
			t.Fatalf("point 3: done=%v err=%v known=%x pending=%x", done, err, q.knownPredicates, q.pendingPredicates)
		}
		if f.drains != 1 || f.infos != 1 || !f.o.removalAuthorized || !f.item.removalAllowed {
			t.Fatal("real cleanup helper was not used")
		}
		d := s.objectFor(f.o)
		for _, slot := range []int{19, 20, 21, 22, 23} {
			if d.facts[slot].count != 1 {
				t.Fatalf("cleanup fact %d missing", slot)
			}
		}
		if _, err = unix.FcntlInt(uintptr(f.fd), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
			t.Fatal("point-three cleanup did not close its FD")
		}
		f.fd = -1
	})
}

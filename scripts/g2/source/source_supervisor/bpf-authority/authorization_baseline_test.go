//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestAuthorizationBaselineProofs(t *testing.T) {
	t.Run("baseline-operation-role", func(t *testing.T) {
		for _, tc := range []struct {
			role string
			want bool
		}{
			{"REALM_ROOT", true},
			{"BOOT_OWNER", false},
			{"RECOVERY_OBSERVER", false},
			{"RUNTIME", false},
		} {
			if got := allowedOperation(tc.role, "SELFTEST_BASELINE_STEP"); got != tc.want {
				t.Errorf("role %s: got %t, want %t", tc.role, got, tc.want)
			}
		}
	})
	t.Run("tid-reuse-authenticates-registered-incarnation", func(t *testing.T) {
		fd, err := unix.PidfdOpen(os.Getpid(), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fd)
		parent := &selftestProbeActor{name: "tid-reuse", probeCookie: 1, taskCookie: 10, tid: 100, tgid: 100, startNS: 1000, listenerID: 5, pidfd: fd}
		old := &selftestProbeActor{name: "tid-reuse-child", probeCookie: 2, taskCookie: 11, tid: 101, tgid: 101, startNS: 1100, parentCookie: 10, listenerID: 5, pidfd: fd}
		newActor := &selftestProbeActor{name: "tid-reuse-child", probeCookie: 3, taskCookie: 12, tid: 101, tgid: 101, startNS: 1200, parentCookie: 10, listenerID: 5, pidfd: fd}
		b := &authorityBroker{probeActors: map[uint64]*selftestProbeActor{2: old}}
		task := guardTask{Cookie: 12, TID: 101, TGID: 101, StartBoottimeNS: 1200}
		if got := b.probeNotificationActor(parent, task); got != nil {
			t.Fatal("unregistered reused TID authenticated as old actor")
		}
		b.probeActors[3] = newActor
		if got := b.probeNotificationActor(parent, task); got != newActor {
			t.Fatal("new task was not authenticated by its own registration")
		}
		newActor.parentCookie = 99
		if got := b.probeNotificationActor(parent, task); got != nil {
			t.Fatal("accepted child of another listener owner")
		}
		newActor.parentCookie = 10
		newActor.listenerID = 6
		if got := b.probeNotificationActor(parent, task); got != nil {
			t.Fatal("accepted child bound to another listener")
		}
		newActor.listenerID = 5
		duplicate := *newActor
		duplicate.probeCookie = 4
		b.probeActors[4] = &duplicate
		if got := b.probeNotificationActor(parent, task); got != nil {
			t.Fatal("accepted ambiguous registrations")
		}
	})
	t.Run("sealed-status-readback", func(t *testing.T) {
		for _, name := range []string{"seal-unload-denied", "seal-detach-denied"} {
			t.Run(name, func(t *testing.T) {
				step := &selftestBaselineStep{name: name, nonce: strings.Repeat("b", 32)}
				status := guardStatus{ModuleInstance: 8, AuthorityPhase: 1, LinkIDs: [3]uint32{11, 12, 13}, LinkAttached: [3]uint32{1, 1, 1}}
				for _, phase := range []string{"before", "after"} {
					body := selftestSealStatusRecord(step, phase, status)
					projection, ok := body["status"].(map[string]any)
					if !exactKeys(body, "name", "nonce", "phase", "status") || body["name"] != step.name || body["nonce"] != step.nonce || body["phase"] != phase || !ok || projection["module_instance"] != jsonU64(8) || projection["authority_phase"] != jsonU64(1) {
						t.Fatalf("incorrect status identity: %#v", body)
					}
					ids, valid := projection["link_ids"].([]any)
					attached, attachedOK := projection["link_attached"].([]any)
					if !valid || !attachedOK || len(ids) != 3 || len(attached) != 3 || ids[0] != jsonU64(11) || ids[1] != jsonU64(12) || ids[2] != jsonU64(13) || attached[0] != true || attached[1] != true || attached[2] != true {
						t.Fatalf("incorrect link readback: %#v", body)
					}
				}
			})
		}
	})
	t.Run("descriptor-fault-witness", func(t *testing.T) {
		for _, tc := range []struct {
			variant, detector string
			count, rejected   uint64
		}{
			{"fd-fill-failure", "fd_fill_failure", 200, 256},
			{"fd-restore-failure", "fd_restore_failure", 256, 255},
		} {
			t.Run(tc.variant, func(t *testing.T) {
				frame := map[string]any{"variant": tc.variant, "source": "broker_fd_stage", "detector": tc.detector, "request_id": nil, "nonce": nil, "input_sha256": nil, "input_bytes": jsonU64(1024), "original_sha256": nil, "expected_actor": nil, "observed_actor": nil, "deadline_ns": nil, "observed_ns": jsonU64(1), "capacity": jsonU64(256), "count_before": jsonU64(tc.count), "rejected_wire": nil, "guard_sequence": nil, "receive_observation": nil}
				o := &terminalObserver{armedVariant: tc.variant, brokerObservation: frame, brokerWitnessSequences: []any{jsonU64(1)}}
				evidence, err := o.sourceFaultEvidence()
				if err != nil || evidence["count_before"] != jsonU64(tc.count) {
					t.Fatalf("valid witness rejected: %v", err)
				}
				frame["count_before"] = jsonU64(tc.rejected)
				if _, err = o.sourceFaultEvidence(); err == nil {
					t.Fatal("accepted incorrect descriptor boundary")
				}
			})
		}
	})
	t.Run("feature-map-waits-for-native-removal", func(t *testing.T) {
		boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
		transcript := newAuthorityTranscript(boot, epoch)
		if _, _, err := transcript.append("invocation", map[string]any{"id": jsonU64(3), "grant_id": jsonU64(9), "command": "create", "state": "bound"}); err != nil {
			t.Fatal(err)
		}
		attr := make([]byte, 44)
		guardPut32(attr, 0, unix.BPF_MAP_TYPE_ARRAY)
		guardPut32(attr, 4, 4)
		guardPut32(attr, 8, 4)
		guardPut32(attr, 12, 1)
		copy(attr[28:], "feature_test")
		object := &objectIncarnation{cookie: 13, ownerTransaction: 5, publicationSeq: 20, class: "map", state: "live"}
		tx := &runtimeEffectTransaction{id: 5, grantID: 9, invocationID: 3, requesterCookie: 7, command: "MAP_CREATE", attributes: attr, effectExecuted: true, effectFD: 10, publication: &guardRecord{ObjectCookie: 13, Sequence: 20}, state: "retired", result: jsonI64(10)}
		step := &selftestBaselineStep{name: "feature-map", nonce: strings.Repeat("b", 32), grantID: 9}
		b := &authorityBroker{bootID: boot, epoch: epoch, transcript: transcript, effects: &runtimeEffects{transactions: map[uint64]*runtimeEffectTransaction{5: tx}, objects: &objectTracker{byCookie: map[uint64]*objectIncarnation{13: object}}}, selftestBaseline: step}
		if err := b.advanceSelftestBaseline(); err != nil || b.selftestBaseline == nil || !step.observed {
			t.Fatalf("map did not remain observed: %v", err)
		}
		object.removalSeq, object.state = 21, "id_removed"
		if err := b.advanceSelftestBaseline(); err != nil || b.selftestBaseline != nil {
			t.Fatalf("map did not settle after removal: %v", err)
		}
		body := transcript.entries[len(transcript.entries)-1].record["body"].(map[string]any)
		if body["phase"] != "settled" || body["actor_cookie"] != jsonU64(7) || body["invocation_id"] != jsonU64(3) {
			t.Fatalf("incorrect settled identity: %#v", body)
		}
	})
	t.Run("compat-probes-require-retired-ebadf", func(t *testing.T) {
		for _, name := range []string{"extended-info-ebadf", "replace-probe-ebadf"} {
			t.Run(name, func(t *testing.T) {
				boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
				transcript := newAuthorityTranscript(boot, epoch)
				if _, _, err := transcript.append("invocation", map[string]any{"id": jsonU64(3), "grant_id": jsonU64(9), "command": "create", "state": "bound"}); err != nil {
					t.Fatal(err)
				}
				tx := &runtimeEffectTransaction{id: 5, grantID: 9, invocationID: 3, requesterCookie: 7, effectFD: -1, state: "response_settled", result: jsonI64(-int64(unix.EBADF))}
				objects := &objectTracker{byCookie: map[uint64]*objectIncarnation{}}
				if name == "extended-info-ebadf" {
					tx.command, tx.attributes, tx.effectExecuted, tx.effectErrno = "PROG_LOAD", make([]byte, 4), true, unix.EBADF
					guardPut32(tx.attributes, 0, unix.BPF_PROG_TYPE_SOCKET_FILTER)
				} else {
					probe, err := probeProgram()
					if err != nil {
						t.Fatal(err)
					}
					tx.command = "PROG_ATTACH"
					tx.inputs = []any{map[string]any{"field": "target_fd", "kind": "dev_null"}, map[string]any{"field": "attach_bpf_fd", "kind": "program", "cookie": jsonU64(13)}}
					objects.byCookie[13] = &objectIncarnation{cookie: 13, publicationSeq: 20, programBytes: hex.EncodeToString(probe)}
				}
				step := &selftestBaselineStep{name: name, nonce: strings.Repeat("b", 32), grantID: 9}
				b := &authorityBroker{bootID: boot, epoch: epoch, transcript: transcript, effects: &runtimeEffects{transactions: map[uint64]*runtimeEffectTransaction{5: tx}, objects: objects}, selftestBaseline: step}
				if err := b.advanceSelftestBaseline(); err != nil || b.selftestBaseline == nil {
					t.Fatal("settled before transaction retirement")
				}
				tx.state = "retired"
				if err := b.advanceSelftestBaseline(); err != nil || b.selftestBaseline != nil {
					t.Fatalf("retired EBADF did not settle: %v", err)
				}
			})
		}
	})
	t.Run("device-cases-wait-for-cgroup-and-program-retirement", func(t *testing.T) {
		for _, name := range []string{"allowed-device-start", "cgroup-auto-retire"} {
			t.Run(name, func(t *testing.T) {
				boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
				tr := newAuthorityTranscript(boot, epoch)
				if _, _, err := tr.append("invocation", map[string]any{"id": jsonU64(3), "grant_id": jsonU64(9), "command": "create", "state": "bound"}); err != nil {
					t.Fatal(err)
				}
				device, err := deviceProgram()
				if err != nil {
					t.Fatal(err)
				}
				obj := &objectIncarnation{cookie: 13, id: 14, ownerTransaction: 5, publicationSeq: 20, class: "program", state: "live", programBytes: hex.EncodeToString(device)}
				attach := &runtimeAttachment{id: 6, grantID: 9, enrollment: 12, programCookie: 13, transactionID: 6, flags: unix.BPF_F_ALLOW_MULTI, state: "attached"}
				load := &runtimeEffectTransaction{id: 5, grantID: 9, invocationID: 3, requesterCookie: 7, command: "PROG_LOAD", state: "retired", effectExecuted: true, effectFD: 10}
				atx := &runtimeEffectTransaction{id: 6, grantID: 9, invocationID: 3, command: "PROG_ATTACH", state: "retired", result: jsonI64(0)}
				grant := &brokerGrant{id: 9, owner: 2, phase: "active", startObserved: true, actualQueryConfirmed: true}
				step := &selftestBaselineStep{name: name, nonce: strings.Repeat("b", 32), owner: 2, grantID: 9}
				resolver, err := newBrokerAuditResolver(400, 99)
				if err != nil {
					t.Fatal(err)
				}
				b := &authorityBroker{bootID: boot, epoch: epoch, transcript: tr, resolver: resolver, grants: map[uint64]*brokerGrant{9: grant}, effects: &runtimeEffects{transactions: map[uint64]*runtimeEffectTransaction{5: load, 6: atx}, objects: &objectTracker{byCookie: map[uint64]*objectIncarnation{13: obj}}, attachments: map[uint64]*runtimeAttachment{6: attach}}, selftestBaseline: step}
				if err := b.emitRuntimeAttachment(attach); err != nil {
					t.Fatal(err)
				}
				attr := make([]byte, 40)
				guardPut32(attr, 4, unix.BPF_CGROUP_DEVICE)
				guardPut32(attr, 24, 64)
				wrong := map[string]any{"attach_flags": jsonU64(unix.BPF_F_ALLOW_MULTI), "program_ids": []any{jsonU64(14)}, "program_attach_flags": []any{jsonU64(0)}}
				if _, _, err = tr.append("transaction", attachedProgramQueryBody(8, 12, 11, 99, attr, "retired", jsonI64(0), wrong)); err != nil {
					t.Fatal(err)
				}
				if err = b.advanceSelftestBaseline(); err != nil || step.observed {
					t.Fatal("accepted query without the real per-program flags")
				}
				output := map[string]any{"attach_flags": jsonU64(unix.BPF_F_ALLOW_MULTI), "program_ids": []any{jsonU64(14)}, "program_attach_flags": []any{jsonU64(unix.BPF_F_ALLOW_MULTI)}}
				for _, state := range []string{"prepared", "committed", "effect_observed", "response_settled", "retired"} {
					var result, resultOutput any
					if state != "prepared" && state != "committed" {
						result, resultOutput = jsonI64(0), output
					}
					if _, _, err = tr.append("transaction", attachedProgramQueryBody(10, 12, 11, 99, attr, state, result, resultOutput)); err != nil {
						t.Fatal(err)
					}
				}
				if err := b.advanceSelftestBaseline(); err != nil || !step.observed || b.selftestBaseline == nil {
					t.Fatalf("did not wait after running: %v", err)
				}
				if !sameJSON(step.transactionIDs, []any{jsonU64(5), jsonU64(6), jsonU64(10)}) {
					t.Fatalf("case did not index the real internal query: %#v", step.transactionIDs)
				}
				attach.offlineSeq, attach.releaseSeq, attach.state = 21, 22, "released"
				obj.removalSeq, obj.state, grant.phase = 23, "id_removed", "retired"
				if err := b.advanceSelftestBaseline(); err != nil || b.selftestBaseline != nil {
					t.Fatalf("retired lifecycle did not settle: %v", err)
				}
			})
		}
	})
	t.Run("mutations-require-real-denial-without-transaction", func(t *testing.T) {
		for _, name := range []string{"wrong-program-byte", "wrong-flags", "wrong-target"} {
			t.Run(name, func(t *testing.T) {
				boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
				tr := newAuthorityTranscript(boot, epoch)
				step := &selftestBaselineStep{name: name, nonce: strings.Repeat("b", 32), invocationID: 3, notificationID: 4, actorCookie: 7}
				b := &authorityBroker{bootID: boot, epoch: epoch, transcript: tr, effects: &runtimeEffects{transactions: map[uint64]*runtimeEffectTransaction{}}, selftestBaseline: step}
				if err := b.advanceSelftestBaseline(); err != nil || b.selftestBaseline == nil {
					t.Fatal("settled before denial")
				}
				body := map[string]any{"invocation_id": jsonU64(3), "notification_id": jsonU64(4), "requester_cookie": jsonU64(7), "result": jsonI64(-int64(unix.EPERM)), "guard_before": jsonU64(10), "guard_after": jsonU64(10)}
				if _, _, err := tr.append("notification_denial", body); err != nil {
					t.Fatal(err)
				}
				b.effects.transactions[5] = &runtimeEffectTransaction{invocationID: 3, notificationID: 4}
				if err := b.advanceSelftestBaseline(); err == nil {
					t.Fatal("accepted a transaction for the denied notification")
				}
				delete(b.effects.transactions, 5)
				if err := b.advanceSelftestBaseline(); err != nil || b.selftestBaseline != nil {
					t.Fatalf("actual denial did not settle: %v", err)
				}
			})
		}
	})
	t.Run("failure-cleanup-waits-for-removal", func(t *testing.T) {
		boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
		tr := newAuthorityTranscript(boot, epoch)
		object := &objectIncarnation{cookie: 13, ownerTransaction: 5, class: "program", state: "live"}
		tx := &runtimeEffectTransaction{id: 5, grantID: 9, invocationID: 3, requesterCookie: 7, command: "PROG_LOAD", effectExecuted: true, effectFD: 10, publication: &guardRecord{ObjectCookie: 13, Sequence: 20}, cleanupActive: true, state: "response_settled", cleanup: map[string]any{"state": "pending"}}
		step := &selftestBaselineStep{name: "failure-cleanup", nonce: strings.Repeat("b", 32), grantID: 9, invocationID: 3, transactionID: 5}
		b := &authorityBroker{bootID: boot, epoch: epoch, transcript: tr, effects: &runtimeEffects{transactions: map[uint64]*runtimeEffectTransaction{5: tx}, objects: &objectTracker{byCookie: map[uint64]*objectIncarnation{13: object}}}, selftestBaseline: step}
		if err := b.advanceSelftestBaseline(); err != nil || !step.observed || b.selftestBaseline == nil {
			t.Fatalf("settled without removal: %v", err)
		}
		object.removalSeq, object.state = 21, "id_removed"
		tx.state, tx.cleanup["state"] = "retired", "settled"
		if err := b.advanceSelftestBaseline(); err != nil || b.selftestBaseline != nil {
			t.Fatalf("removal did not settle: %v", err)
		}
	})
	t.Run("attachment-replace", func(t *testing.T) {
		boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
		tr := newAuthorityTranscript(boot, epoch)
		old := &objectIncarnation{cookie: 13, state: "live", publicationSeq: 20}
		replacement := &objectIncarnation{cookie: 14, state: "id_removed", publicationSeq: 25, removalSeq: 28}
		step := &selftestBaselineStep{name: "attachment-replace", nonce: strings.Repeat("b", 32), owner: 3, grantID: 9, actorCookie: 7, invocationID: 4, referenceCookie: 13, extraAttachmentID: 10, secondTransactionID: 6, replacementAttachmentID: 11, replacementTransactionID: 8, detachTransactionID: 12}
		effects := &runtimeEffects{objects: &objectTracker{byCookie: map[uint64]*objectIncarnation{13: old, 14: replacement}}, transactions: map[uint64]*runtimeEffectTransaction{6: {id: 6, publication: &guardRecord{ObjectCookie: 14}}, 8: {id: 8, state: "retired"}, 12: {id: 12, state: "retired"}}, attachments: map[uint64]*runtimeAttachment{10: {id: 10, state: "released"}, 11: {id: 11, state: "released", replaces: 10, programCookie: 14}}, grantCleanup: map[uint64]*grantObjectCleanup{13: {cookie: 13}}}
		broker := &authorityBroker{bootID: boot, epoch: epoch, transcript: tr, join: newAuditJoin(tr, nil), resolver: &brokerAuditResolver{}, checkpoint: &checkpointController{lifecycle: &checkpointLifecycle{}}, grants: map[uint64]*brokerGrant{9: {id: 9, owner: 3, phase: "retired"}}, runtimeInvocations: map[int]*runtimeInvocation{-5: {id: 4, state: "settled", pidfd: -1}}, effects: effects, selftestBaseline: step}
		if err := broker.advanceSelftestBaseline(); err != nil || !step.observed || broker.selftestBaseline == nil {
			t.Fatalf("settled before both removals: %v", err)
		}
		old.state, old.removalSeq = "id_removed", 29
		if err := broker.advanceSelftestBaseline(); err != nil || broker.selftestBaseline != nil {
			t.Fatalf("did not settle after both removals: %v", err)
		}
	})
	t.Run("extra-fd-delays-removal", func(t *testing.T) {
		boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
		tr := newAuthorityTranscript(boot, epoch)
		object := &objectIncarnation{cookie: 13, id: 17, publicationSeq: 20, class: "program", state: "live"}
		item := &deferredObject{kernelID: 17, class: "program", deferredRemovalOnNextRelease: true, deferredRemovalOwner: 3}
		step := &selftestBaselineStep{name: "extra-fd-delays-removal", nonce: strings.Repeat("b", 32), owner: 3, actorCookie: 7, referenceCookie: 13, secondTransactionID: 6}
		broker := &authorityBroker{bootID: boot, epoch: epoch, transcript: tr, checkpoint: &checkpointController{lifecycle: &checkpointLifecycle{objects: map[uint64]*deferredObject{13: item}}}, effects: &runtimeEffects{transactions: map[uint64]*runtimeEffectTransaction{6: {id: 6, state: "retired"}}, objects: &objectTracker{byCookie: map[uint64]*objectIncarnation{13: object}}}, probeActors: map[uint64]*selftestProbeActor{1: {taskCookie: 7, pidfd: -1}}, selftestBaseline: step}
		if err := broker.advanceSelftestBaseline(); err != nil || !step.observed || broker.selftestBaseline == nil {
			t.Fatalf("did not retain live object: %v", err)
		}
		object.state, object.removalSeq = "id_removed", 21
		if err := broker.advanceSelftestBaseline(); err == nil {
			t.Fatal("accepted removal before checkpoint release")
		}
		item.removalAllowed, item.deferredRemovalOnNextRelease = true, false
		if err := broker.advanceSelftestBaseline(); err != nil || broker.selftestBaseline != nil {
			t.Fatalf("did not settle after permitted removal: %v", err)
		}
	})
	t.Run("deferred-checkpoint-authenticates-witness", func(t *testing.T) {
		rootTestPinProc(t)
		pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pidfd)
		boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
		tr := newAuthorityTranscript(boot, epoch)
		p := &controlPrincipal{ID: 3, Role: "REALM_ROOT", TaskCookie: 4, PIDFD: pidfd}
		grant := &brokerGrant{id: 9, owner: 3, generation: 2, phase: "active", startObserved: true, createSettled: true, actualQueryConfirmed: true}
		step := &selftestBaselineStep{name: "extra-fd-delays-removal", nonce: strings.Repeat("b", 32), owner: 3, grantID: 9, actorCookie: 7, invocationID: 4, transactionID: 5, secondTransactionID: 6, extraAttachmentID: 10, referenceCookie: 13, publicationSeq: 20, notificationID: 11, observed: true, result: jsonI64(0), transactionIDs: []any{jsonU64(8), jsonU64(6)}}
		item := &deferredObject{kernelID: 17, class: "program", deferredRemovalOnNextRelease: true, deferredRemovalOwner: 3}
		object := &objectIncarnation{cookie: 13, id: 17, ownerTransaction: 5, publicationSeq: 20, class: "program", state: "live", removalAuthorized: true}
		cleanup := &grantObjectCleanup{grantID: 9, creatorID: 5, cookie: 13}
		actor := &selftestProbeActor{name: step.name, taskCookie: 7, parentCookie: 4, tgid: uint32(os.Getpid()), tid: uint32(os.Getpid()), startNS: 1, pidfd: pidfd, listenerFD: 99, listenerID: 12}
		b := &authorityBroker{bootID: boot, epoch: epoch, transcript: tr, caseFacts: map[string]any{"case": "realm-negative", "scenario": "bpf-authorization", "run_token": authorityRunToken}, checkpoint: &checkpointController{lifecycle: &checkpointLifecycle{objects: map[uint64]*deferredObject{13: item}}}, grants: map[uint64]*brokerGrant{9: grant}, probeActors: map[uint64]*selftestProbeActor{1: actor}, probeListeners: map[int]*selftestProbeActor{99: actor}, runtimeInvocations: map[int]*runtimeInvocation{-1: {id: 4, grantID: 9, generation: 2, grant: grant, command: "create"}}, selftestBaseline: step, effects: &runtimeEffects{objects: &objectTracker{byCookie: map[uint64]*objectIncarnation{13: object}}, objectGrant: map[uint64]uint64{13: 9}, attachments: map[uint64]*runtimeAttachment{10: {id: 10, grantID: 9, programCookie: 13, transactionID: 8, state: "released"}}, grantCleanup: map[uint64]*grantObjectCleanup{13: cleanup}, transactions: map[uint64]*runtimeEffectTransaction{5: {id: 5, grantID: 9, invocationID: 4, command: "PROG_LOAD", state: "retired"}, 6: {id: 6, grantID: 9, command: "PROG_DETACH", state: "retired", internalSelftest: true, result: jsonI64(0)}, 8: {id: 8, grantID: 9, invocationID: 4, command: "PROG_ATTACH", state: "retired"}}}}
		b.effects.transactions[6].output = map[string]any{"attachment_id": jsonU64(10)}
		stage := &selftestRuntimeArm{nonce: step.nonce, grantID: 9, invocationID: 4, mode: step.name, phase: "injected", notifyingCookie: 7, notificationID: 11, mutation: map[string]any{"program_cookie": jsonU64(13), "listener_id": jsonU64(12), "installed_fd": jsonU64(10)}}
		if err = appendRuntimeRecord(b, "selftest_runtime_stage", stage.body()); err != nil {
			t.Fatal(err)
		}
		if err = appendRuntimeRecord(b, "attachment", map[string]any{"id": jsonU64(10), "state": "detached", "grant_id": jsonU64(9), "program_cookie": jsonU64(13), "transaction_id": jsonU64(8)}); err != nil {
			t.Fatal(err)
		}
		if err = appendRuntimeRecord(b, "grant_object_cleanup", grantCleanupBody(cleanup, 1, "close_ref", 0, nil)); err != nil {
			t.Fatal(err)
		}
		if err = appendRuntimeRecord(b, "transaction", map[string]any{"id": jsonU64(6), "state": "retired", "grant_id": jsonU64(9), "command": "PROG_DETACH", "result": jsonI64(0), "output": b.effects.transactions[6].output}); err != nil {
			t.Fatal(err)
		}
		if err = appendRuntimeRecord(b, "selftest_case", step.record("observed", jsonU64(7), nil, nil)); err != nil {
			t.Fatal(err)
		}
		if err = b.deferredCheckpointReady(p); err != nil {
			t.Fatalf("rejected authenticated witness: %v", err)
		}
		p.ID = 4
		if b.deferredCheckpointReady(p) == nil {
			t.Fatal("accepted another owner")
		}
		p.ID = 3
		object.removalSeq = 21
		if b.deferredCheckpointReady(p) == nil {
			t.Fatal("accepted premature removal")
		}
		object.removalSeq = 0
		b.resolver = &brokerAuditResolver{}
		if b.observeRuntimeGuard(guardRecord{Kind: 2, Sequence: 21, ObjectCookie: 13, KernelID: 17}) == nil {
			t.Fatal("accepted premature kernel removal")
		}
		if cleanup.removalSequence != 0 || object.removalSeq != 0 {
			t.Fatal("recorded cleanup for unauthorized removal")
		}
		grant.phase = "retired"
		if b.deferredCheckpointReady(p) == nil {
			t.Fatal("accepted early grant retirement")
		}
		grant.phase = "active"
		b.effects.transactions[8].state = "committed"
		if b.deferredCheckpointReady(p) == nil {
			t.Fatal("accepted active transaction")
		}
		b.effects.transactions[8].state = "retired"
		b.effects.objects.transcript = tr
		item.removalAllowed, item.deferredRemovalOnNextRelease = true, false
		if err = b.observeRuntimeGuard(guardRecord{Kind: 2, Sequence: 21, ObjectCookie: 13, KernelID: 17}); err != nil {
			t.Fatalf("failed to finalize permitted removal: %v", err)
		}
		if cleanup.removalSequence != 21 || object.removalSeq != 21 || !item.removed || grant.phase != "active" {
			t.Fatal("removal did not finalize before grant settlement")
		}
		last := tr.entries[len(tr.entries)-1].record["body"].(map[string]any)
		if !sameJSON(last, grantCleanupBody(cleanup, 2, "await_id_removed", 0, jsonU64(21))) {
			t.Fatal("missing matching removal cleanup record")
		}
	})
	t.Run("audit-interleaved", func(t *testing.T) {
		guardA, _, ae := rawJoinFixture(t)
		a, err := parseAuditFact(auditSyscallRecord, []byte("audit(42.123:17): arch=c000003e syscall=321 success=no exit=-1 a0=0 pid=123 ppid=12"))
		if err != nil {
			t.Fatal(err)
		}
		b, err := parseAuditFact(auditSyscallRecord, []byte("audit(42.123:18): arch=c000003e syscall=321 success=no exit=-1 a0=0 pid=124 ppid=12"))
		if err != nil {
			t.Fatal(err)
		}
		be, err := parseAuditFact(auditEOERecord, []byte("audit(42.123:18):"))
		if err != nil {
			t.Fatal(err)
		}
		guardA.Operation = unix.BPF_MAP_CREATE
		guardB := guardA
		guardB.Sequence, guardB.TaskCookie, guardB.AuditSerial = 2, 102, 18
		guardPut32(guardB.Detail[:], 0, 124)
		j := rawJoinBroker(t)
		nonce := strings.Repeat("b", 32)
		j.reorder = &auditReorder{actors: [2]uint64{a.PID, b.PID}, nonce: nonce}
		original := []auditFact{b, be, a, ae}
		for i, fact := range original {
			if err := j.observeFact(fact); err != nil {
				t.Fatal(err)
			}
			if i < 3 && len(j.transcript.entries) != 0 {
				t.Fatal("selected facts reached the join before both pairs arrived")
			}
		}
		if !j.reorder.complete || len(j.transcript.entries) != 5 {
			t.Fatal("did not emit witness and four reordered wires")
		}
		witness := j.transcript.entries[0].record
		body := witness["body"].(map[string]any)
		if witness["kind"] != "selftest_audit_reorder" || body["name"] != "audit-interleaved" || body["nonce"] != nonce || !sameJSON(body["delivery_order"], []any{jsonU64(3), jsonU64(1), jsonU64(4), jsonU64(2)}) {
			t.Fatalf("incorrect reorder witness: %#v", witness)
		}
		received := body["received"].([]any)
		for i, fact := range original {
			want := map[string]any{"stamp": fact.Key.stamp(), "serial": jsonU64(fact.Serial), "record_type": jsonU64(uint64(fact.Kind)), "payload_sha256": fact.PayloadSHA256, "occurrence": jsonU64(1)}
			if !sameJSON(received[i], want) {
				t.Fatalf("lost original receipt %d: %#v", i, received[i])
			}
		}
		for i, index := range []int{2, 0, 3, 1} {
			entry := j.transcript.entries[i+1].record
			if entry["kind"] != "audit_wire" || !sameJSON(entry["body"], received[index]) {
				t.Fatalf("delivery does not map to receipt: %#v", entry)
			}
		}
		for _, guard := range []guardRecord{guardA, guardB} {
			if err := j.observeGuard(guard); err != nil {
				t.Fatal(err)
			}
		}
		if err := j.drain(); err != nil || !j.settled() {
			t.Fatalf("ordinary raw joins did not settle: %v", err)
		}
		t.Run("map-and-program-siblings", func(t *testing.T) {
			parse := func(kind uint16, text string) auditFact {
				t.Helper()
				fact, err := parseAuditFact(kind, []byte(text))
				if err != nil {
					t.Fatal(err)
				}
				return fact
			}
			ap := parse(auditSyscallRecord, "audit(42.123:19): arch=c000003e syscall=321 success=no exit=-1 a0=5 pid=123 ppid=12")
			ape := parse(auditEOERecord, "audit(42.123:19):")
			bp := parse(auditSyscallRecord, "audit(42.123:20): arch=c000003e syscall=321 success=no exit=-1 a0=5 pid=124 ppid=12")
			bpe := parse(auditEOERecord, "audit(42.123:20):")
			programA, programB := guardA, guardB
			programA.Sequence, programA.AuditSerial, programA.Operation = 3, 19, unix.BPF_PROG_LOAD
			programB.Sequence, programB.AuditSerial, programB.Operation = 4, 20, unix.BPF_PROG_LOAD
			join := rawJoinBroker(t)
			join.reorder = &auditReorder{actors: [2]uint64{a.PID, b.PID}, nonce: nonce}
			for _, fact := range []auditFact{a, ae, ap, ape, b, be, bp, bpe} {
				if err := join.observeFact(fact); err != nil {
					t.Fatalf("observing serial %d kind %d: %v", fact.Serial, fact.Kind, err)
				}
			}
			if !join.reorder.complete || len(join.transcript.entries) != 9 {
				t.Fatal("sibling facts did not pass through while selected pairs were buffered")
			}
			for i, fact := range []auditFact{ap, ape} {
				entry := join.transcript.entries[i].record
				body := entry["body"].(map[string]any)
				if entry["kind"] != "audit_wire" || body["payload_sha256"] != fact.PayloadSHA256 {
					t.Fatalf("program sibling did not take ordinary delivery: %#v", entry)
				}
			}
			witness := join.transcript.entries[2].record
			body := witness["body"].(map[string]any)
			if witness["kind"] != "selftest_audit_reorder" || !sameJSON(body["delivery_order"], []any{jsonU64(1), jsonU64(3), jsonU64(2), jsonU64(4)}) {
				t.Fatalf("incorrect sibling reorder witness: %#v", witness)
			}
			received := body["received"].([]any)
			for i, fact := range []auditFact{a, ae, b, be} {
				want := map[string]any{"stamp": fact.Key.stamp(), "serial": jsonU64(fact.Serial), "record_type": jsonU64(uint64(fact.Kind)), "payload_sha256": fact.PayloadSHA256, "occurrence": jsonU64(1)}
				if !sameJSON(received[i], want) {
					t.Fatalf("lost selected receipt %d: %#v", i, received[i])
				}
			}
			for i, index := range []int{0, 2, 1, 3} {
				entry := join.transcript.entries[i+3].record
				if entry["kind"] != "audit_wire" || !sameJSON(entry["body"], received[index]) {
					t.Fatalf("selected delivery differs from receipt: %#v", entry)
				}
			}
			for _, guard := range []guardRecord{guardA, programA, guardB, programB} {
				if err := join.observeGuard(guard); err != nil {
					t.Fatal(err)
				}
			}
			if err := join.drain(); err != nil || !join.settled() {
				t.Fatalf("map and program raw joins did not settle: %v", err)
			}
			wantCommands := map[json.Number]string{"17": "MAP_CREATE", "18": "MAP_CREATE", "19": "PROG_LOAD", "20": "PROG_LOAD"}
			for _, entry := range join.transcript.entries {
				if entry.record["kind"] != "audit" {
					continue
				}
				body := entry.record["body"].(map[string]any)
				serial, ok := body["serial"].(json.Number)
				if !ok || wantCommands[serial] == "" || body["command"] != wantCommands[serial] || body["raw"] == nil {
					t.Fatalf("wrong native-bound audit: %#v", body)
				}
				delete(wantCommands, serial)
			}
			if len(wantCommands) != 0 {
				t.Fatalf("missing native-bound audits: %#v", wantCommands)
			}
		})
		t.Run("duplicate-selected-map", func(t *testing.T) {
			join := rawJoinBroker(t)
			join.reorder = &auditReorder{actors: [2]uint64{a.PID, b.PID}, nonce: nonce}
			if err := join.observeFact(a); err != nil {
				t.Fatal(err)
			}
			if err := join.observeFact(a); err == nil {
				t.Fatal("accepted a duplicate selected MAP_CREATE syscall")
			}
		})
	})
	t.Run("id-reuse", func(t *testing.T) {
		boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
		tr := newAuthorityTranscript(boot, epoch)
		old := &objectIncarnation{cookie: 13, id: 17, publicationSeq: 20, removalSeq: 21, state: "id_removed"}
		newObject := &objectIncarnation{cookie: 14, id: 17, publicationSeq: 25, state: "live"}
		first := &runtimeEffectTransaction{id: 5, state: "retired", publication: &guardRecord{ObjectCookie: 13}}
		second := &runtimeEffectTransaction{id: 6, state: "response_settled", publication: &guardRecord{ObjectCookie: 14}}
		step := &selftestBaselineStep{name: "id-reuse", nonce: strings.Repeat("b", 32), actorCookie: 7, transactionID: 5, secondTransactionID: 6, transactionIDs: []any{jsonU64(6)}}
		broker := &authorityBroker{bootID: boot, epoch: epoch, transcript: tr, resolver: &brokerAuditResolver{}, effects: &runtimeEffects{transactions: map[uint64]*runtimeEffectTransaction{5: first, 6: second}, objects: &objectTracker{byCookie: map[uint64]*objectIncarnation{13: old, 14: newObject}}}, selftestBaseline: step}
		if err := broker.advanceSelftestBaseline(); err != nil || !step.observed || broker.selftestBaseline == nil {
			t.Fatalf("settled before second removal: %v", err)
		}
		newObject.state, newObject.removalSeq, second.state = "id_removed", 26, "retired"
		if err := broker.advanceSelftestBaseline(); err != nil || broker.selftestBaseline != nil {
			t.Fatalf("failed to settle after removal: %v", err)
		}
	})
	t.Run("container-bpf", func(t *testing.T) {
		boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
		tr := newAuthorityTranscript(boot, epoch)
		if _, _, err := tr.append("invocation", map[string]any{"id": jsonU64(3), "grant_id": jsonU64(9), "command": "create", "state": "bound"}); err != nil {
			t.Fatal(err)
		}
		step := &selftestBaselineStep{name: "container-bpf", nonce: strings.Repeat("b", 32), grantID: 9, actorCookie: 7}
		broker := &authorityBroker{bootID: boot, epoch: epoch, transcript: tr, selftestBaseline: step}
		for i, operation := range []uint64{unix.BPF_MAP_CREATE, unix.BPF_PROG_LOAD} {
			record := map[string]any{"task_cookie": jsonU64(7), "kind": jsonU64(13), "audit_serial": jsonU64(uint64(i + 10)), "operation": jsonU64(operation), "sequence": jsonU64(uint64(i + 20)), "result": jsonI64(-int64(unix.EPERM))}
			if _, _, err := tr.append("guard", map[string]any{"record": record}); err != nil {
				t.Fatal(err)
			}
		}
		if err := broker.advanceSelftestBaseline(); err != nil || broker.selftestBaseline == nil {
			t.Fatalf("settled without audit joins: %v", err)
		}
		for _, serial := range []uint64{10, 11} {
			body := map[string]any{"serial": jsonU64(serial), "task_cookie": jsonU64(7), "result": jsonI64(-int64(unix.EPERM)), "raw": map[string]any{"witness": true}, "transaction_id": nil}
			if _, _, err := tr.append("audit", body); err != nil {
				t.Fatal(err)
			}
		}
		if err := broker.advanceSelftestBaseline(); err != nil || broker.selftestBaseline != nil {
			t.Fatalf("did not settle with both joins: %v", err)
		}
	})
	t.Run("transferred-owner-can-only-record-fixed-denial", func(t *testing.T) {
		fd, err := unix.PidfdOpen(os.Getpid(), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fd)
		p := &controlPrincipal{ID: 7, Role: "BOOT_OWNER", Transferred: true, TransferChildPIDFD: fd, NextRequest: 3}
		request := map[string]any{"principal_id": jsonU64(7), "request_id": jsonU64(3), "operation": "SELFTEST_FD_ARM", "body": map[string]any{"nonce": strings.Repeat("c", 32), "grant_id": jsonU64(9), "class": "map"}}
		raw, err := canonicalControl(request)
		if err != nil {
			t.Fatal(err)
		}
		if err = p.commitTransferredFDArmDenial(3, raw, []byte("denied")); err != nil {
			t.Fatal(err)
		}
		if !p.Transferred || p.NextRequest != 4 || len(p.Replay) != 1 {
			t.Fatal("denial reopened transferred authority")
		}
		request["request_id"], request["operation"] = jsonU64(4), "ADMIT"
		raw, err = canonicalControl(request)
		if err != nil {
			t.Fatal(err)
		}
		if err = p.commitTransferredFDArmDenial(4, raw, []byte("denied")); err == nil {
			t.Fatal("accepted another operation")
		}
	})
	t.Run("step-validation", func(t *testing.T) {
		fd, err := unix.PidfdOpen(os.Getpid(), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fd)
		boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
		transcript := newAuthorityTranscript(boot, epoch)
		b := &authorityBroker{bootID: boot, epoch: epoch, transcript: transcript, checkpoint: &checkpointController{}, reads: &brokerReadState{bootID: boot, epoch: epoch, runToken: authorityRunToken}, caseFacts: map[string]any{"case": "realm-negative", "scenario": "bpf-authorization", "run_token": authorityRunToken, "bpf_variant": "baseline"}}
		p := &controlPrincipal{ID: 7, Role: "REALM_ROOT", PIDFD: fd, NextRequest: 1}
		body := map[string]any{"name": "wrong-generation", "nonce": strings.Repeat("b", 32), "grant_id": nil}
		req := map[string]any{"request_id": jsonU64(1), "body": body}
		raw, err := canonicalControl(req)
		if err != nil {
			t.Fatal(err)
		}
		p.Role = "BOOT_OWNER"
		if _, err = b.startSelftestBaseline(p, req, raw); err == nil {
			t.Fatal("accepted wrong role")
		}
		p.Role = "REALM_ROOT"
		b.caseFacts["scenario"] = "bpf-broker-kill"
		if _, err = b.startSelftestBaseline(p, req, raw); err == nil {
			t.Fatal("accepted wrong scenario")
		}
		b.caseFacts["scenario"] = "bpf-authorization"
		body["extra"] = true
		if _, err = b.startSelftestBaseline(p, req, raw); err == nil {
			t.Fatal("accepted extra body key")
		}
		delete(body, "extra")
		b.selftestFD = &selftestFDArm{}
		if _, err = b.startSelftestBaseline(p, req, raw); err == nil {
			t.Fatal("accepted an active FD arm")
		}
		b.selftestFD, b.selftestRuntime = nil, &selftestRuntimeArm{}
		if _, err = b.startSelftestBaseline(p, req, raw); err == nil {
			t.Fatal("accepted an active runtime arm")
		}
		b.selftestRuntime = nil
		ack, err := b.startSelftestBaseline(p, req, raw)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parseCanonicalControl(ack)
		if err != nil {
			t.Fatal(err)
		}
		ackBody := parsed["body"].(map[string]any)
		if !exactKeys(ackBody, "started") || ackBody["started"] != true {
			t.Fatalf("unexpected ACK: %#v", ackBody)
		}
		event := transcript.entries[0].record["body"].(map[string]any)
		if !exactKeys(event, "name", "nonce", "phase", "actor_cookie", "invocation_id", "transaction_ids", "guard_sequences", "reference_cookies", "request_sha256", "result", "error_code") || event["phase"] != "started" {
			t.Fatalf("unexpected case record: %#v", event)
		}
		b.selftestBaseline = nil
		req["request_id"] = jsonU64(2)
		if _, err = b.startSelftestBaseline(p, req, raw); err == nil {
			t.Fatal("accepted case replay with a fresh request")
		}
	})
	t.Run("rejection-witness-required", func(t *testing.T) {
		boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
		transcript := newAuthorityTranscript(boot, epoch)
		b := &authorityBroker{bootID: boot, epoch: epoch, transcript: transcript, selftestBaseline: &selftestBaselineStep{name: "wrong-generation", nonce: strings.Repeat("b", 32), ackDelivered: true}}
		if err := b.advanceSelftestBaseline(); err == nil {
			t.Fatal("settled without an authenticated witness")
		}
		if len(transcript.entries) != 0 {
			t.Fatal("missing witness produced transcript evidence")
		}
		b.selftestBaseline.witnessed = true
		b.selftestBaseline.actorCookie = 8
		b.selftestBaseline.requestHash = "invalid"
		if err := b.advanceSelftestBaseline(); err == nil {
			t.Fatal("accepted an invalid request hash")
		}
		if len(transcript.entries) != 0 {
			t.Fatal("invalid witness produced transcript evidence")
		}
	})
	t.Run("control-case-does-not-borrow-prior-evidence", func(t *testing.T) {
		boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
		transcript := newAuthorityTranscript(boot, epoch)
		p := &controlPrincipal{ID: 7, Role: "REALM_ROOT", TaskCookie: 19}
		b := &authorityBroker{bootID: boot, epoch: epoch, transcript: transcript, principals: map[uint64]*controlPrincipal{7: p}, grants: map[uint64]*brokerGrant{9: {id: 9, owner: 8, phase: "admitted"}}}
		request := map[string]any{"body": map[string]any{"nonce": strings.Repeat("c", 32), "grant_id": jsonU64(9), "class": "map"}}
		raw, err := canonicalControl(request)
		if err != nil {
			t.Fatal(err)
		}
		if err = b.appendBaselineControlRejection(p, 1, "SELFTEST_FD_ARM", request, raw, 10, 10); err != nil {
			t.Fatal(err)
		}
		b.selftestBaseline = &selftestBaselineStep{name: "fd-arm-wrong-grant", nonce: strings.Repeat("b", 32), owner: 7, begin: len(transcript.entries)}
		if err = b.advanceSelftestBaseline(); err != nil || b.selftestBaseline == nil {
			t.Fatal("used a denial from before this case")
		}
		if err = b.appendBaselineControlRejection(p, 2, "SELFTEST_FD_ARM", request, raw, 11, 11); err != nil {
			t.Fatal(err)
		}
		if err = b.advanceSelftestBaseline(); err != nil || b.selftestBaseline != nil {
			t.Fatal("did not settle on this case's denial")
		}
		last := transcript.entries[len(transcript.entries)-1].record["body"].(map[string]any)
		if last["phase"] != "settled" || last["actor_cookie"] != jsonU64(19) || last["request_sha256"] != hashBytes(raw) || last["error_code"] != "BPF_AUTH_DENIED" {
			t.Fatalf("wrong settled evidence: %#v", last)
		}
	})
	t.Run("wrong-grant-eligible-budget", func(t *testing.T) {
		rootTestPinProc(t)
		boot, err := fixedBootID()
		if err != nil {
			t.Fatal(err)
		}
		pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(pidfd)
		p := &controlPrincipal{ID: 7, Role: "REALM_ROOT", TaskCookie: 19, PIDFD: pidfd, NextRequest: 1}
		config, err := postgresTemplate("app", true, 10001, 10001)
		if err != nil {
			t.Fatal(err)
		}
		grant := &brokerGrant{id: 9, owner: 7, phase: "retired", purpose: "postgres-init-app", config: config}
		if eligibleSelftestFDGrant(grant, p.ID) || eligibleSelftestFDGrant(&brokerGrant{owner: 8, phase: "admitted"}, p.ID) || !eligibleSelftestFDGrant(&brokerGrant{owner: 7, phase: "admitted"}, p.ID) {
			t.Fatal("grant guard did not distinguish retired, foreign and eligible grants")
		}
		tr := newAuthorityTranscript(boot, strings.Repeat("a", 32))
		step := &selftestBaselineStep{name: "fd-arm-wrong-grant", nonce: strings.Repeat("b", 32), owner: 7}
		b := &authorityBroker{bootID: boot, epoch: strings.Repeat("a", 32), principals: map[uint64]*controlPrincipal{7: p}, grants: map[uint64]*brokerGrant{9: grant}, checkpoint: &checkpointController{}, effects: &runtimeEffects{}, postgresUID: 10001, postgresGID: 10001, caseFacts: map[string]any{"case": "realm-negative", "scenario": "bpf-authorization", "run_token": authorityRunToken}, selftestBaseline: step}
		request := map[string]any{"request_id": jsonU64(1), "body": map[string]any{"nonce": strings.Repeat("c", 32), "grant_id": jsonU64(9), "class": "map"}}
		raw, err := canonicalControl(request)
		if err != nil {
			t.Fatal(err)
		}
		// With every pre-grant prerequisite eligible, an admitted grant reaches
		// the deliberately absent transcript (or the real deadline) instead.
		grant.phase = "admitted"
		if _, err = b.armSelftestFD(p, request, raw); err != errGuardProtocol {
			t.Fatalf("eligible control did not reach the later guard: %v", err)
		}
		grant.phase = "retired"
		if _, err = b.armSelftestFD(p, request, raw); err != errControlFrame || b.selftestFD != nil || b.selftestMapArmed || b.selftestProgramArmed || len(tr.entries) != 0 {
			t.Fatal("retired grant armed or bypassed the grant guard")
		}
		b.transcript = tr
		if err = b.appendBaselineControlRejection(p, 1, "SELFTEST_FD_ARM", request, raw, 10, 10); err != nil {
			t.Fatal(err)
		}
		b.selftestMapArmed = true
		if err = b.advanceSelftestBaseline(); err != nil || b.selftestBaseline == nil {
			t.Fatal("accepted a denial with consumed map budget")
		}
		b.selftestMapArmed = false
		if err = b.advanceSelftestBaseline(); err != nil || b.selftestBaseline != nil {
			t.Fatal("eligible-budget denial did not settle")
		}
	})
	t.Run("control-case-state-transitions", func(t *testing.T) {
		for _, tc := range []struct {
			name, role        string
			owner, grantOwner uint64
			consumed          bool
		}{
			{"fd-arm-wrong-role", "BOOT_OWNER", 8, 7, false},
			{"fd-arm-wrong-grant", "REALM_ROOT", 7, 8, false},
			{"fd-arm-second", "REALM_ROOT", 7, 7, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
				transcript := newAuthorityTranscript(boot, epoch)
				p := &controlPrincipal{ID: tc.owner, Role: tc.role, TaskCookie: 19}
				b := &authorityBroker{bootID: boot, epoch: epoch, transcript: transcript, principals: map[uint64]*controlPrincipal{p.ID: p}, grants: map[uint64]*brokerGrant{9: {id: 9, owner: tc.grantOwner, phase: "admitted"}}, selftestMapArmed: tc.consumed, selftestBaseline: &selftestBaselineStep{name: tc.name, nonce: strings.Repeat("b", 32), owner: 7}}
				request := map[string]any{"body": map[string]any{"nonce": strings.Repeat("c", 32), "grant_id": jsonU64(9), "class": "map"}}
				raw, err := canonicalControl(request)
				if err != nil {
					t.Fatal(err)
				}
				if err = b.appendBaselineControlRejection(p, 1, "SELFTEST_FD_ARM", request, raw, 10, 10); err != nil {
					t.Fatal(err)
				}
				if err = b.advanceSelftestBaseline(); err != nil || b.selftestBaseline != nil {
					t.Fatalf("case did not settle: %v", err)
				}
			})
		}
	})
	t.Run("exact-replay-before-mutated-replay", func(t *testing.T) {
		boot, epoch := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32)
		transcript := newAuthorityTranscript(boot, epoch)
		p := &controlPrincipal{ID: 7, Role: "REALM_ROOT", TaskCookie: 19}
		b := &authorityBroker{bootID: boot, epoch: epoch, transcript: transcript, principals: map[uint64]*controlPrincipal{7: p}, selftestBaseline: &selftestBaselineStep{name: "fd-arm-replay", nonce: strings.Repeat("b", 32), owner: 7, grantID: 9}, selftestFD: &selftestFDArm{nonce: strings.Repeat("c", 32), class: "map", owner: 7, grantID: 9, phase: "armed", replayOnly: true}}
		request := map[string]any{"body": map[string]any{"nonce": strings.Repeat("c", 32), "grant_id": jsonU64(9), "class": "map"}}
		raw, err := canonicalControl(request)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err = b.appendBaselineControlSuccess(p, 1, request, raw, 10, 10); err != nil {
				t.Fatal(err)
			}
			if err = b.advanceSelftestBaseline(); err != nil || b.selftestBaseline == nil {
				t.Fatal("settled before the changed replay")
			}
		}
		changed := map[string]any{"body": map[string]any{"nonce": strings.Repeat("d", 32), "grant_id": jsonU64(9), "class": "map"}}
		changedRaw, err := canonicalControl(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err = b.appendBaselineControlRejection(p, 1, "SELFTEST_FD_ARM", changed, changedRaw, 10, 10); err != nil {
			t.Fatal(err)
		}
		if err = b.advanceSelftestBaseline(); err != nil || b.selftestBaseline != nil {
			t.Fatalf("changed replay did not settle: %v", err)
		}
		if b.selftestFD != nil || b.selftestMapArmed || b.selftestProgramArmed {
			t.Fatal("replay retained the arm or consumed an FD class")
		}
	})
	t.Run("deferred-removal-release", func(t *testing.T) {
		boot, epoch, nonce := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32), strings.Repeat("b", 32)
		transcript := newAuthorityTranscript(boot, epoch)
		item := &deferredObject{kernelID: 17, class: "program", deferredRemovalOnNextRelease: true, deferredRemovalOwner: 3}
		c := &checkpointController{transcript: transcript, join: newAuditJoin(transcript, nil), lifecycle: &checkpointLifecycle{paused: true, objects: map[uint64]*deferredObject{9: item}}, cursor: 8, active: &activeCheckpoint{principalID: 3, nonce: nonce, watermark: 1, guardWatermark: 8, tailBefore: transcript.tail, depth: 2}}
		status := guardStatus{Sequence: 8, AckCursor: 8}
		if _, _, err := c.release(4, nonce, 1, status); err == nil {
			t.Fatal("accepted another checkpoint owner")
		}
		if item.removalAllowed || !item.deferredRemovalOnNextRelease || c.active.depth != 2 {
			t.Fatal("wrong owner changed the checkpoint")
		}
		if _, _, err := c.release(3, nonce, 1, status); err != nil {
			t.Fatal(err)
		}
		if item.removalAllowed || !item.deferredRemovalOnNextRelease {
			t.Fatal("nested release authorized removal")
		}
		if _, _, err := c.release(3, nonce, 1, status); err != nil {
			t.Fatal(err)
		}
		if !item.removalAllowed || item.deferredRemovalOnNextRelease || c.active != nil {
			t.Fatal("outer release did not authorize removal")
		}
	})
	t.Run("deferred-removal-rejects-premature-exit", func(t *testing.T) {
		boot, epoch, nonce := "00000000-0000-0000-0000-000000000000", strings.Repeat("a", 32), strings.Repeat("b", 32)
		transcript := newAuthorityTranscript(boot, epoch)
		item := &deferredObject{kernelID: 17, class: "program", removed: true, deferredRemovalOnNextRelease: true, deferredRemovalOwner: 3}
		c := &checkpointController{transcript: transcript, join: newAuditJoin(transcript, nil), lifecycle: &checkpointLifecycle{paused: true, objects: map[uint64]*deferredObject{9: item}}, cursor: 8, active: &activeCheckpoint{principalID: 3, nonce: nonce, watermark: 1, guardWatermark: 8, tailBefore: transcript.tail, depth: 1}}
		if _, _, err := c.release(3, nonce, 1, guardStatus{Sequence: 8, AckCursor: 8}); err == nil {
			t.Fatal("released after premature program removal")
		}
		if !c.invalid || item.removalAllowed || len(transcript.entries) != 0 {
			t.Fatal("premature removal did not fail closed")
		}
	})
	t.Run("control-rejection-record", func(t *testing.T) {
		boot := "00000000-0000-0000-0000-000000000000"
		epoch := strings.Repeat("a", 32)
		b := &authorityBroker{bootID: boot, epoch: epoch, transcript: newAuthorityTranscript(boot, epoch)}
		body := map[string]any{"nonce": strings.Repeat("b", 32), "grant_id": jsonU64(9), "class": "map"}
		request := map[string]any{"version": json.Number("1"), "boot_id": boot, "epoch": epoch, "run_token": authorityRunToken, "principal_id": jsonU64(7), "request_id": jsonU64(3), "operation": "SELFTEST_FD_ARM", "body": body}
		raw, err := canonicalControl(request)
		if err != nil {
			t.Fatal(err)
		}
		if err = b.appendBaselineControlRejection(&controlPrincipal{ID: 7}, 3, "SELFTEST_FD_ARM", request, raw, 41, 41); err != nil {
			t.Fatal(err)
		}
		entry := b.transcript.entries[0].record
		record, ok := entry["body"].(map[string]any)
		if !ok || entry["kind"] != "control_rejection" || !exactKeys(record, "principal_id", "request_id", "operation", "nonce", "body", "request_sha256", "status", "code", "guard_before", "guard_after") {
			t.Fatalf("unexpected rejection record: %#v", entry)
		}
		if record["principal_id"] != jsonU64(7) || record["request_id"] != jsonU64(3) || record["nonce"] != body["nonce"] || record["request_sha256"] != hashBytes(raw) || record["guard_before"] != jsonU64(41) || record["guard_after"] != jsonU64(41) || record["status"] != "BPF_AUTH_DENIED" || record["code"] != "BPF_AUTH_DENIED" {
			t.Fatalf("rejection does not bind the actual request: %#v", record)
		}
		body["grant_id"] = jsonU64(10)
		if recordBody := record["body"].(map[string]any); recordBody["grant_id"] != jsonU64(9) {
			t.Fatal("transcript retained mutable request body")
		}
	})
	t.Run("preprincipal-rejection-record", func(t *testing.T) {
		boot := "00000000-0000-0000-0000-000000000000"
		epoch := strings.Repeat("a", 32)
		b := &authorityBroker{bootID: boot, epoch: epoch, transcript: newAuthorityTranscript(boot, epoch)}
		request := map[string]any{"version": json.Number("1"), "boot_id": boot, "request_id": jsonU64(1), "operation": "BIND_RUNTIME", "body": map[string]any{"container_id": strings.Repeat("c", 64), "command": "start", "bundle_sha256": nil, "process_sha256": nil}}
		raw, err := canonicalControl(request)
		if err != nil {
			t.Fatal(err)
		}
		if err = b.appendBaselineControlRejection(nil, 1, "BIND_RUNTIME", request, raw, 73, 73); err != nil {
			t.Fatal(err)
		}
		body := b.transcript.entries[0].record["body"].(map[string]any)
		if body["principal_id"] != nil || body["nonce"] != nil || body["request_sha256"] != hashBytes(raw) || body["guard_before"] != jsonU64(73) || body["guard_after"] != jsonU64(73) {
			t.Fatalf("preprincipal rejection binding differs: %#v", body)
		}
		if err = b.appendBaselineControlRejection(nil, 1, "BIND_RUNTIME", request, raw, 74, 73); err == nil {
			t.Fatal("accepted reversed guard bounds")
		}
		if len(b.transcript.entries) != 1 {
			t.Fatal("invalid rejection changed transcript")
		}
	})
}

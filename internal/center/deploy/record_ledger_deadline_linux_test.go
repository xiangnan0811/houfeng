//go:build linux

package deploy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecordLedgerMaintenanceDeadlineLifecycle(t *testing.T) {
	work, cancel, err := ledgerMaintenanceWorkContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	limit, ok := work.Value(ledgerMaintenanceDeadlineKey{}).(ledgerMaintenanceDeadline)
	if !ok || !limit.work.Equal(limit.total.Add(-ledgerMaintenanceCleanupReserve)) {
		t.Fatal("maintenance did not reserve cleanup inside its original deadline")
	}
	if deadline, ok := work.Deadline(); !ok || !deadline.Equal(limit.work) {
		t.Fatal("work did not stop at the reserved cleanup boundary")
	}

	f := &RecordLedgerFence{ctx: context.Background()}
	f.self = f
	bound, cancelBound, err := f.bindMaintenanceWork(work)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelBound()
	reentered, cancelReentry, err := ledgerMaintenanceWorkContext(bound)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelReentry()
	if got, _ := reentered.Deadline(); !got.Equal(limit.work) {
		t.Fatal("reentry extended the work deadline")
	}

	// A later public call in the same outer maintenance scope cannot renew it.
	later := ledgerMaintenanceDeadline{work: limit.work.Add(time.Minute), total: limit.total.Add(time.Minute)}
	laterContext := context.WithValue(context.Background(), ledgerMaintenanceDeadlineKey{}, later)
	second, cancelSecond, err := f.bindMaintenanceWork(laterContext)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSecond()
	if got, _ := second.Deadline(); !got.Equal(limit.work) {
		t.Fatal("a second callback renewed the fence lifetime")
	}

	scopeDeadline := time.Now().Add(30 * time.Second)
	scope, cancelScope := context.WithDeadline(context.Background(), scopeDeadline)
	defer cancelScope()
	scopedFence := &RecordLedgerFence{ctx: scope}
	scopedFence.self = scopedFence
	scopedWork, cancelScopedWork, err := scopedFence.bindMaintenanceWork(work)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelScopedWork()
	if got, _ := scopedWork.Deadline(); !got.Equal(scopeDeadline.Add(-ledgerMaintenanceCleanupReserve)) {
		t.Fatal("callback ignored the enclosing maintenance deadline")
	}
	scopedCleanup, cancelScopedCleanup, err := ledgerFenceCleanupContext(scope, scopedFence)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelScopedCleanup()
	if got, _ := scopedCleanup.Deadline(); !got.Equal(scopeDeadline) {
		t.Fatal("outer cleanup ignored the enclosing maintenance deadline")
	}
	cancelScope()
	select {
	case <-scopedWork.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("callback did not stop when its enclosing scope was canceled")
	}
	if scopedCleanup.Err() != nil {
		t.Fatal("scope cancellation canceled the reserved cleanup window")
	}

	interrupted, interrupt := context.WithCancel(reentered)
	interrupt()
	cleanup, cancelCleanup, err := ledgerMaintenanceCallbackCleanupContext(interrupted)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelCleanup()
	if got, _ := cleanup.Deadline(); !got.Equal(limit.total) || cleanup.Err() != nil {
		t.Fatal("cleanup did not survive cancellation within the original lifetime")
	}
	outerCleanup, cancelOuter, err := ledgerFenceCleanupContext(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelOuter()
	if got, _ := outerCleanup.Deadline(); !got.Equal(limit.total) {
		t.Fatal("outer administrator cleanup renewed the callback lifetime")
	}

	// The work deadline may expire while the absolute cleanup deadline is
	// still live; use real expired and live context deadlines without sleeps.
	workDeadline := time.Now().Add(-time.Second)
	elapsedLimit := ledgerMaintenanceDeadline{work: workDeadline, total: workDeadline.Add(ledgerMaintenanceCleanupReserve)}
	elapsedParent := context.WithValue(context.Background(), ledgerMaintenanceDeadlineKey{}, elapsedLimit)
	elapsedWork, cancelElapsed := context.WithDeadline(elapsedParent, workDeadline)
	defer cancelElapsed()
	if !errors.Is(elapsedWork.Err(), context.DeadlineExceeded) {
		t.Fatal("fixture did not reach the work deadline")
	}
	elapsedCleanup, cancelElapsedCleanup, err := ledgerMaintenanceCallbackCleanupContext(elapsedWork)
	if err != nil {
		t.Fatal("elapsed work window consumed its cleanup reserve")
	}
	defer cancelElapsedCleanup()
	if got, _ := elapsedCleanup.Deadline(); !got.Equal(elapsedLimit.total) || elapsedCleanup.Err() != nil {
		t.Fatal("elapsed work did not retain the original cleanup window")
	}

	shorter, cancelShorter := context.WithDeadline(work, limit.work.Add(-time.Second))
	defer cancelShorter()
	shortCleanup, cancelShortCleanup, err := ledgerMaintenanceCleanupContext(shorter)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelShortCleanup()
	if got, _ := shortCleanup.Deadline(); !got.Equal(limit.work.Add(-time.Second)) {
		t.Fatal("cleanup extended an independently shorter deadline")
	}

	expired, cancelExpired := context.WithDeadline(work, time.Now().Add(-time.Second))
	defer cancelExpired()
	if _, _, err := ledgerMaintenanceCleanupContext(expired); !errors.Is(err, ErrRecordLedgerFence) {
		t.Fatal("expired bounded cleanup received a new lifetime")
	}
	past := time.Now().Add(-time.Second)
	expiredTotal := ledgerMaintenanceDeadline{work: past.Add(-ledgerMaintenanceCleanupReserve), total: past}
	if _, _, err := ledgerMaintenanceCleanupContext(context.WithValue(context.Background(), ledgerMaintenanceDeadlineKey{}, expiredTotal)); !errors.Is(err, ErrRecordLedgerFence) {
		t.Fatal("expired original total deadline received a new lifetime")
	}
	expiredFence := &RecordLedgerFence{callbackDeadline: expiredTotal}
	if _, _, err := ledgerFenceCleanupContext(context.Background(), expiredFence); !errors.Is(err, ErrRecordLedgerFence) {
		t.Fatal("outer cleanup renewed an expired callback lifetime")
	}

	ordinary, cancelOrdinary := context.WithCancel(context.Background())
	cancelOrdinary()
	before := time.Now()
	ordinaryCleanup, cancelOrdinaryCleanup, err := ledgerMaintenanceCleanupContext(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelOrdinaryCleanup()
	if got, ok := ordinaryCleanup.Deadline(); !ok || got.Before(before) || got.After(before.Add(ledgerOrdinaryCleanupLimit+time.Second)) || ordinaryCleanup.Err() != nil {
		t.Fatal("ordinary cancellation did not retain bounded cleanup")
	}
	ordinaryExpired, cancelOrdinaryExpired := context.WithDeadline(context.Background(), past)
	defer cancelOrdinaryExpired()
	if _, _, err := ledgerMaintenanceCleanupContext(ordinaryExpired); !errors.Is(err, ErrRecordLedgerFence) {
		t.Fatal("ordinary cleanup extended an expired caller deadline")
	}
}

func TestRecordLedgerExpiredPredecessorCleanupPreservesIntent(t *testing.T) {
	dir, _ := ledgerTestControl(t)
	request := RecordLedgerMaintenanceRequest{
		Compose:     ComposeOfflineFenceRequest{RecoveryID: "rid_deadline"},
		AppIdentity: RecordLedgerApplicationIdentity{ID: "app", SystemIdentifier: "101", DatabaseName: "houfeng", DatabaseOID: 123, Epoch: 1},
	}
	f := &RecordLedgerFence{control: dir, request: request, uses: 1, predecessorPending: true}
	if err := f.loadMaintenancePhase(); err != nil {
		t.Fatal(err)
	}
	if err := f.transitionMaintenancePhase(ledgerPhasePredecessor); err != nil {
		t.Fatal(err)
	}
	intent := ledgerPredecessorIntent{Version: 1, PhaseGeneration: f.phase.Generation, RecoveryID: request.Compose.RecoveryID, Application: request.AppIdentity}
	name := ledgerPredecessorName("callback-intent", intent.PhaseGeneration)
	if _, err := ledgerPublishExact(dir, name, intent); err != nil {
		t.Fatal(err)
	}
	call := &ledgerPredecessorCall{fence: f, intent: intent}
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := call.refence(expired); !errors.Is(err, ErrRecordLedgerFence) {
		t.Fatal("expired refencing did not fail closed")
	}
	phases, err := ledgerReadMaintenancePhases(dir)
	if err != nil || len(phases) != 2 || phases[1].Phase != ledgerPhasePredecessor || f.phase != phases[1] {
		t.Fatal("expired refencing advanced or lost its durable phase")
	}
	if !f.predecessorPending || ledgerReadExact(dir, name, intent) != nil {
		t.Fatal("expired refencing discarded its unresolved callback intent")
	}
}

func TestRecordLedgerFenceContextTracksRootAndCaller(t *testing.T) {
	rootDeadline := time.Now().Add(time.Minute)
	root, stopRoot := context.WithDeadline(context.Background(), rootDeadline)
	defer stopRoot()
	callerDeadline := rootDeadline.Add(time.Minute)
	caller, stopCaller := context.WithDeadline(context.Background(), callerDeadline)
	defer stopCaller()
	f := &RecordLedgerFence{ctx: root}
	bound, cancel := f.bindContext(caller)
	defer cancel()
	if deadline, ok := bound.Deadline(); !ok || !deadline.Equal(rootDeadline) {
		t.Fatal("operation did not inherit the earlier root deadline")
	}
	earlierDeadline := rootDeadline.Add(-time.Second)
	earlier, stopEarlier := context.WithDeadline(context.Background(), earlierDeadline)
	defer stopEarlier()
	short, cancelShort := f.bindContext(earlier)
	defer cancelShort()
	if deadline, ok := short.Deadline(); !ok || !deadline.Equal(earlierDeadline) {
		t.Fatal("operation did not preserve the earlier caller deadline")
	}
	stopRoot()
	select {
	case <-bound.Done():
	case <-time.After(time.Second):
		t.Fatal("root cancellation did not cancel the operation")
	}

	root, stopRoot = context.WithCancel(context.Background())
	defer stopRoot()
	caller, stopCaller = context.WithCancel(context.Background())
	defer stopCaller()
	f.ctx = root
	bound, cancel = f.bindContext(caller)
	defer cancel()
	stopCaller()
	select {
	case <-bound.Done():
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not cancel the operation")
	}
}

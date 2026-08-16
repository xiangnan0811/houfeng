package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEvidenceMaintenanceWorkerRunOnceIsBoundedAndPublishesAggregateMetrics(t *testing.T) {
	now := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	repository := &maintenanceRepositoryStub{
		deletedIntentIDs: []string{"evi_000000000000000000000001", "evi_000000000000000000000002"},
		collectedPayloads: []PayloadGCReceipt{
			maintenancePayloadReceipt(40, 20),
			maintenancePayloadReceipt(60, 25),
		},
		backlog: EvidenceLifecycleBacklog{ExpiredIntentCount: 30, EligibleOrphanPayloadCount: 4, MoreExpiredIntents: true},
		capacity: EvidenceCapacityAggregate{
			ProjectCount: 2, LogicalSnapshotBytes: 1_350, HighestProjectLogicalBytes: 850,
			PhysicalCanonicalBytes: 1_000, PhysicalCompressedBytes: 700,
			OrphanPayloadCount: 5, OrphanCanonicalBytes: 300, OrphanCompressedBytes: 180,
		},
	}
	observer := NewMaintenanceObserver()
	worker := NewMaintenanceWorker(repository, observer, MaintenanceWorkerOptions{
		Interval: time.Hour, IntentBatchLimit: 10, PayloadBatchLimit: 20, BacklogProbeLimit: 30,
		CapacityPolicy: CapacityPolicy{ProjectLimitBytes: 1_000, WarningPercent: 80},
		Clock:          func() time.Time { return now }, Logger: discardEvidenceLogger(),
	})

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if got, want := repository.calls, []string{"delete:10", "collect:20", "backlog:30", "capacity"}; !equalStrings(got, want) {
		t.Fatalf("repository calls = %#v, want %#v", got, want)
	}
	metrics := observer.Snapshot()
	if metrics.PassAttempts != 1 || metrics.PassSuccesses != 1 || metrics.PassFailures != 0 ||
		metrics.DeletedIntentCount != 2 || metrics.ReclaimedPayloadCount != 2 ||
		metrics.ReclaimedCanonicalBytes != 100 || metrics.ReclaimedCompressedBytes != 45 ||
		metrics.CapacityStatus != QuotaWarning || metrics.CapacityLogicalBytes != 850 ||
		metrics.CapacityLimitBytes != 1_000 || metrics.CapacityWarningThresholdBytes != 800 ||
		metrics.TotalLogicalSnapshotBytes != 1_350 || metrics.LastAttemptedAt != now ||
		metrics.LastSucceededAt != now || metrics.Backlog != repository.backlog {
		t.Fatalf("observer metrics = %#v", metrics)
	}
	if got, want := metrics.Alerts, []MaintenanceAlert{MaintenanceAlertCapacityWarning}; !equalAlerts(got, want) {
		t.Fatalf("alerts = %#v, want %#v", got, want)
	}
}

func TestEvidenceMaintenanceWorkerStopsAfterStageFailure(t *testing.T) {
	repository := &maintenanceRepositoryStub{deleteErr: errors.New("database unavailable intent=evi_secret digest=deadbeef")}
	var logs bytes.Buffer
	observer := NewMaintenanceObserver()
	worker := NewMaintenanceWorker(repository, observer, MaintenanceWorkerOptions{
		Interval: time.Hour, IntentBatchLimit: 1, PayloadBatchLimit: 1, BacklogProbeLimit: 1,
		CapacityPolicy: DefaultCapacityPolicy(), Clock: func() time.Time { return time.Unix(1, 0).UTC() },
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})

	for pass := 0; pass < 2; pass++ {
		if err := worker.RunOnce(context.Background()); err == nil {
			t.Fatal("RunOnce() error = nil, want stage failure")
		}
	}
	if got, want := repository.calls, []string{"delete:1", "delete:1"}; !equalStrings(got, want) {
		t.Fatalf("repository calls = %#v, want first-stage-only %#v", got, want)
	}
	metrics := observer.Snapshot()
	if metrics.PassAttempts != 2 || metrics.PassFailures != 2 || metrics.PassSuccesses != 0 ||
		metrics.AlertTransitionCount != 1 {
		t.Fatalf("metrics = %#v", metrics)
	}
	if got, want := metrics.Alerts, []MaintenanceAlert{MaintenanceAlertJanitorUnavailable}; !equalAlerts(got, want) {
		t.Fatalf("alerts = %#v, want %#v", got, want)
	}
	if strings.Contains(logs.String(), "evi_secret") || strings.Contains(logs.String(), "deadbeef") {
		t.Fatalf("worker log leaked dependency material: %q", logs.String())
	}
}

func TestEvidenceMaintenanceWorkerClearsAlertOnRecovery(t *testing.T) {
	repository := &maintenanceRepositoryStub{deleteErr: errors.New("unavailable")}
	observer := NewMaintenanceObserver()
	worker := NewMaintenanceWorker(repository, observer, MaintenanceWorkerOptions{
		Interval: time.Hour, IntentBatchLimit: 1, PayloadBatchLimit: 1, BacklogProbeLimit: 1,
		CapacityPolicy: DefaultCapacityPolicy(), Clock: func() time.Time { return time.Unix(1, 0).UTC() },
		Logger: discardEvidenceLogger(),
	})
	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("first RunOnce() error = nil")
	}
	repository.deleteErr = nil
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("recovery RunOnce() error = %v", err)
	}
	metrics := observer.Snapshot()
	if len(metrics.Alerts) != 0 || metrics.AlertTransitionCount != 2 || metrics.PassSuccesses != 1 {
		t.Fatalf("recovered metrics = %#v", metrics)
	}
}

func TestEvidenceMaintenanceWorkerRecordsCompletedCleanupWhenCapacityStageFails(t *testing.T) {
	now := time.Unix(2, 0).UTC()
	repository := &maintenanceRepositoryStub{
		deletedIntentIDs:  []string{"evi_000000000000000000000001"},
		collectedPayloads: []PayloadGCReceipt{maintenancePayloadReceipt(40, 20)},
		capacityErr:       errors.New("capacity unavailable digest=secret"),
	}
	observer := NewMaintenanceObserver()
	worker := NewMaintenanceWorker(repository, observer, MaintenanceWorkerOptions{
		Interval: time.Hour, IntentBatchLimit: 1, PayloadBatchLimit: 1, BacklogProbeLimit: 1,
		CapacityPolicy: DefaultCapacityPolicy(), Clock: func() time.Time { return now }, Logger: discardEvidenceLogger(),
	})

	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil, want capacity-stage failure")
	}
	metrics := observer.Snapshot()
	if metrics.PassFailures != 1 || metrics.PassSuccesses != 0 ||
		metrics.DeletedIntentCount != 1 || metrics.ReclaimedPayloadCount != 1 ||
		metrics.ReclaimedCanonicalBytes != 40 || metrics.ReclaimedCompressedBytes != 20 ||
		metrics.CapacityStatus != QuotaUnavailable {
		t.Fatalf("failed-pass metrics = %#v", metrics)
	}
	if got, want := metrics.Alerts, []MaintenanceAlert{MaintenanceAlertCapacityUnavailable}; !equalAlerts(got, want) {
		t.Fatalf("alerts = %#v, want %#v", got, want)
	}
}

func TestEvidenceMaintenanceWorkerRejectsUnboundedAndInconsistentRepositoryResults(t *testing.T) {
	tests := []struct {
		name       string
		repository *maintenanceRepositoryStub
		want       error
	}{
		{
			name: "too many deleted intents",
			repository: &maintenanceRepositoryStub{deletedIntentIDs: []string{
				"evi_000000000000000000000001", "evi_000000000000000000000002",
			}},
		},
		{
			name: "too many payload receipts",
			repository: &maintenanceRepositoryStub{collectedPayloads: []PayloadGCReceipt{
				maintenancePayloadReceipt(1, 1), maintenancePayloadReceipt(1, 1),
			}},
		},
		{
			name: "noncanonical payload receipt",
			repository: &maintenanceRepositoryStub{collectedPayloads: []PayloadGCReceipt{{
				CanonicalSizeBytes: 1, CompressedSizeBytes: 1,
			}}},
		},
		{
			name: "inconsistent backlog",
			repository: &maintenanceRepositoryStub{backlog: EvidenceLifecycleBacklog{
				MoreExpiredIntents: true,
			}},
		},
		{
			name: "inconsistent physical aggregate",
			repository: &maintenanceRepositoryStub{capacity: EvidenceCapacityAggregate{
				ProjectCount: 1, PhysicalCanonicalBytes: 1,
			}},
			want: ErrCapacityUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			worker := NewMaintenanceWorker(test.repository, NewMaintenanceObserver(), MaintenanceWorkerOptions{
				Interval: time.Hour, IntentBatchLimit: 1, PayloadBatchLimit: 1, BacklogProbeLimit: 1,
				CapacityPolicy: DefaultCapacityPolicy(), Clock: func() time.Time { return time.Unix(5, 0).UTC() },
				Logger: discardEvidenceLogger(),
			})
			err := worker.RunOnce(context.Background())
			if err == nil || (test.want != nil && !errors.Is(err, test.want)) {
				t.Fatalf("RunOnce() error = %v, want fail-closed %v", err, test.want)
			}
			metrics := worker.observer.Snapshot()
			if metrics.PassFailures != 1 || metrics.PassSuccesses != 0 {
				t.Fatalf("failed result metrics = %#v", metrics)
			}
		})
	}
}

func TestEvidenceMaintenanceWorkerStopsCleanlyOnMidPassCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repository := &maintenanceRepositoryStub{
		deletedIntentIDs: []string{"evi_000000000000000000000001"},
		afterDelete:      cancel,
	}
	observer := NewMaintenanceObserver()
	worker := NewMaintenanceWorker(repository, observer, MaintenanceWorkerOptions{
		Interval: time.Hour, IntentBatchLimit: 1, PayloadBatchLimit: 1, BacklogProbeLimit: 1,
		CapacityPolicy: DefaultCapacityPolicy(), Clock: func() time.Time { return time.Unix(6, 0).UTC() },
		Logger: discardEvidenceLogger(),
	})
	if err := worker.RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce() error = %v, want context.Canceled", err)
	}
	if got, want := repository.calls, []string{"delete:1"}; !equalStrings(got, want) {
		t.Fatalf("repository calls = %#v, want %#v", got, want)
	}
	metrics := observer.Snapshot()
	if metrics.PassAttempts != 1 || metrics.PassFailures != 0 || metrics.PassSuccesses != 0 ||
		metrics.DeletedIntentCount != 1 || len(metrics.Alerts) != 0 {
		t.Fatalf("cancelled pass metrics = %#v", metrics)
	}
}

func TestEvidenceMaintenanceWorkerPreservesCapacityAlertDuringJanitorFailure(t *testing.T) {
	repository := &maintenanceRepositoryStub{capacity: EvidenceCapacityAggregate{
		ProjectCount: 1, LogicalSnapshotBytes: 850, HighestProjectLogicalBytes: 850,
		PhysicalCanonicalBytes: 850, PhysicalCompressedBytes: 400,
	}}
	observer := NewMaintenanceObserver()
	worker := NewMaintenanceWorker(repository, observer, MaintenanceWorkerOptions{
		Interval: time.Hour, IntentBatchLimit: 1, PayloadBatchLimit: 1, BacklogProbeLimit: 1,
		CapacityPolicy: CapacityPolicy{ProjectLimitBytes: 1_000, WarningPercent: 80},
		Clock:          func() time.Time { return time.Unix(3, 0).UTC() }, Logger: discardEvidenceLogger(),
	})
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("warning RunOnce() error = %v", err)
	}
	repository.deleteErr = errors.New("janitor unavailable")
	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("failing RunOnce() error = nil")
	}
	metrics := observer.Snapshot()
	if got, want := metrics.Alerts, []MaintenanceAlert{
		MaintenanceAlertCapacityWarning,
		MaintenanceAlertJanitorUnavailable,
	}; !equalAlerts(got, want) {
		t.Fatalf("alerts = %#v, want %#v", got, want)
	}
	if metrics.CapacityStatus != QuotaWarning {
		t.Fatalf("capacity status = %q, want retained warning", metrics.CapacityStatus)
	}
}

func TestEvidenceMaintenanceWorkerRunLogsSanitizedStructuredFailure(t *testing.T) {
	var logs bytes.Buffer
	called := make(chan struct{})
	repository := &maintenanceRepositoryStub{
		deleteErr:    errors.New("database unavailable intent=evi_secret digest=deadbeef"),
		deleteCalled: called,
	}
	worker := NewMaintenanceWorker(repository, NewMaintenanceObserver(), MaintenanceWorkerOptions{
		Interval: time.Hour, IntentBatchLimit: 1, PayloadBatchLimit: 1, BacklogProbeLimit: 1,
		CapacityPolicy: DefaultCapacityPolicy(), Clock: func() time.Time { return time.Unix(4, 0).UTC() },
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	<-called
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	output := logs.String()
	if !strings.Contains(output, "error=janitor_unavailable") {
		t.Fatalf("worker log missing sanitized structured error: %q", output)
	}
	if strings.Contains(output, "evi_secret") || strings.Contains(output, "deadbeef") {
		t.Fatalf("worker log leaked dependency material: %q", output)
	}
}

func TestEvidenceMaintenanceWorkerRejectsNilTypedNilAndPreCancelledContext(t *testing.T) {
	var typedNil *maintenanceRepositoryStub
	observer := NewMaintenanceObserver()
	options := MaintenanceWorkerOptions{
		Interval: time.Hour, IntentBatchLimit: 1, PayloadBatchLimit: 1, BacklogProbeLimit: 1,
		CapacityPolicy: DefaultCapacityPolicy(), Logger: discardEvidenceLogger(),
	}
	for _, test := range []struct {
		name   string
		worker *MaintenanceWorker
		ctx    context.Context
	}{
		{name: "nil worker", worker: nil, ctx: context.Background()},
		{name: "nil repository", worker: NewMaintenanceWorker(nil, observer, options), ctx: context.Background()},
		{name: "typed nil repository", worker: NewMaintenanceWorker(typedNil, observer, options), ctx: context.Background()},
		{name: "nil context", worker: NewMaintenanceWorker(&maintenanceRepositoryStub{}, observer, options), ctx: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.worker.RunOnce(test.ctx); !errors.Is(err, ErrInvalidMaintenanceWorker) {
				t.Fatalf("RunOnce() error = %v, want ErrInvalidMaintenanceWorker", err)
			}
		})
	}

	repository := &maintenanceRepositoryStub{}
	worker := NewMaintenanceWorker(repository, observer, options)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce(cancelled) error = %v, want context.Canceled", err)
	}
	if len(repository.calls) != 0 {
		t.Fatalf("pre-cancelled RunOnce() calls = %#v", repository.calls)
	}
}

func TestEvidenceMaintenanceObserverReturnsDefensiveConcurrentSnapshots(t *testing.T) {
	observer := NewMaintenanceObserver()
	observer.recordFailure(maintenancePassResult{at: time.Unix(1, 0).UTC()}, MaintenanceAlertJanitorUnavailable)
	first := observer.Snapshot()
	first.Alerts[0] = MaintenanceAlertCapacityExceeded
	if got := observer.Snapshot().Alerts; len(got) != 1 || got[0] != MaintenanceAlertJanitorUnavailable {
		t.Fatalf("observer alerts mutated through snapshot = %#v", got)
	}

	var wait sync.WaitGroup
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for pass := 0; pass < 100; pass++ {
				_ = observer.Snapshot()
			}
		}()
	}
	wait.Wait()
}

type maintenanceRepositoryStub struct {
	mu                sync.Mutex
	calls             []string
	deletedIntentIDs  []string
	collectedPayloads []PayloadGCReceipt
	backlog           EvidenceLifecycleBacklog
	capacity          EvidenceCapacityAggregate
	deleteErr         error
	collectErr        error
	backlogErr        error
	capacityErr       error
	deleteCalled      chan struct{}
	deleteSignalOnce  sync.Once
	afterDelete       func()
}

func (repository *maintenanceRepositoryStub) DeleteExpiredCaptureIntents(_ context.Context, limit uint64) ([]string, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.calls = append(repository.calls, "delete:"+uintString(limit))
	if repository.deleteCalled != nil {
		repository.deleteSignalOnce.Do(func() { close(repository.deleteCalled) })
	}
	if repository.afterDelete != nil {
		repository.afterDelete()
	}
	return append([]string(nil), repository.deletedIntentIDs...), repository.deleteErr
}

func (repository *maintenanceRepositoryStub) CollectUnreferencedPayloads(_ context.Context, limit uint64) ([]PayloadGCReceipt, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.calls = append(repository.calls, "collect:"+uintString(limit))
	return append([]PayloadGCReceipt(nil), repository.collectedPayloads...), repository.collectErr
}

func (repository *maintenanceRepositoryStub) ReadEvidenceLifecycleBacklog(_ context.Context, limit uint64) (EvidenceLifecycleBacklog, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.calls = append(repository.calls, "backlog:"+uintString(limit))
	return repository.backlog, repository.backlogErr
}

func (repository *maintenanceRepositoryStub) ReadEvidenceCapacityAggregate(context.Context) (EvidenceCapacityAggregate, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.calls = append(repository.calls, "capacity")
	return repository.capacity, repository.capacityErr
}

func discardEvidenceLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func maintenancePayloadReceipt(canonicalBytes, compressedBytes uint64) PayloadGCReceipt {
	digestInput := []byte(uintString(canonicalBytes) + ":" + uintString(compressedBytes))
	return PayloadGCReceipt{
		PayloadVersionDigest: sha256.Sum256(append([]byte("payload version:"), digestInput...)),
		ReceiptDigest:        sha256.Sum256(append([]byte("receipt:"), digestInput...)),
		DeletedAt:            time.Unix(1, 0).UTC(),
		CanonicalSizeBytes:   canonicalBytes,
		CompressedSizeBytes:  compressedBytes,
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalAlerts(left, right []MaintenanceAlert) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func uintString(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

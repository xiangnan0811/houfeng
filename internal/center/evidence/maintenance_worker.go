package evidence

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"sort"
	"sync"
	"time"
)

const (
	DefaultMaintenanceInterval = time.Hour
	MaxMaintenanceBatchSize    = uint64(100)
)

var (
	ErrInvalidMaintenanceWorker = errors.New("invalid evidence maintenance worker")
	ErrInvalidMaintenanceResult = errors.New("invalid evidence maintenance result")
)

type PayloadGCReceipt struct {
	PayloadVersionDigest [sha256.Size]byte
	ReceiptDigest        [sha256.Size]byte
	DeletedAt            time.Time
	CanonicalSizeBytes   uint64
	CompressedSizeBytes  uint64
}

func (receipt PayloadGCReceipt) Validate() error {
	if receipt.PayloadVersionDigest == [sha256.Size]byte{} || receipt.ReceiptDigest == [sha256.Size]byte{} ||
		receipt.DeletedAt.IsZero() || receipt.DeletedAt.Location() != time.UTC ||
		receipt.DeletedAt != receipt.DeletedAt.Round(0) || receipt.DeletedAt.Nanosecond()%int(time.Microsecond) != 0 ||
		receipt.CanonicalSizeBytes == 0 || receipt.CanonicalSizeBytes > MaxCanonicalPayloadBytes ||
		receipt.CompressedSizeBytes == 0 || receipt.CompressedSizeBytes > math.MaxInt64 {
		return ErrInvalidMaintenanceResult
	}
	return nil
}

type EvidenceLifecycleBacklog struct {
	ExpiredIntentCount         uint64
	EligibleOrphanPayloadCount uint64
	MoreExpiredIntents         bool
	MoreEligibleOrphanPayloads bool
}

func (backlog EvidenceLifecycleBacklog) Validate(limit uint64) error {
	if limit == 0 || limit > MaxMaintenanceBatchSize || backlog.ExpiredIntentCount > limit ||
		backlog.EligibleOrphanPayloadCount > limit ||
		(backlog.MoreExpiredIntents && backlog.ExpiredIntentCount != limit) ||
		(backlog.MoreEligibleOrphanPayloads && backlog.EligibleOrphanPayloadCount != limit) {
		return ErrInvalidMaintenanceResult
	}
	return nil
}

type EvidenceCapacityAggregate struct {
	ProjectCount               uint64
	LogicalSnapshotBytes       uint64
	HighestProjectLogicalBytes uint64
	PhysicalCanonicalBytes     uint64
	PhysicalCompressedBytes    uint64
	OrphanPayloadCount         uint64
	OrphanCanonicalBytes       uint64
	OrphanCompressedBytes      uint64
}

func (aggregate EvidenceCapacityAggregate) Validate() error {
	if aggregate.HighestProjectLogicalBytes > aggregate.LogicalSnapshotBytes ||
		(aggregate.LogicalSnapshotBytes == 0) != (aggregate.HighestProjectLogicalBytes == 0) ||
		(aggregate.ProjectCount == 0 && aggregate.LogicalSnapshotBytes != 0) ||
		(aggregate.LogicalSnapshotBytes == 0) != (aggregate.PhysicalCanonicalBytes == 0) ||
		(aggregate.PhysicalCanonicalBytes == 0) != (aggregate.PhysicalCompressedBytes == 0) ||
		aggregate.PhysicalCanonicalBytes > aggregate.LogicalSnapshotBytes ||
		(aggregate.OrphanPayloadCount == 0) != (aggregate.OrphanCanonicalBytes == 0) ||
		(aggregate.OrphanPayloadCount == 0) != (aggregate.OrphanCompressedBytes == 0) ||
		aggregate.LogicalSnapshotBytes > math.MaxInt64 || aggregate.HighestProjectLogicalBytes > math.MaxInt64 ||
		aggregate.PhysicalCanonicalBytes > math.MaxInt64 || aggregate.PhysicalCompressedBytes > math.MaxInt64 ||
		aggregate.OrphanCanonicalBytes > math.MaxInt64 || aggregate.OrphanCompressedBytes > math.MaxInt64 {
		return ErrCapacityUnavailable
	}
	return nil
}

type MaintenanceRepository interface {
	DeleteExpiredCaptureIntents(context.Context, uint64) ([]string, error)
	CollectUnreferencedPayloads(context.Context, uint64) ([]PayloadGCReceipt, error)
	ReadEvidenceLifecycleBacklog(context.Context, uint64) (EvidenceLifecycleBacklog, error)
	ReadEvidenceCapacityAggregate(context.Context) (EvidenceCapacityAggregate, error)
}

type MaintenanceAlert string

const (
	MaintenanceAlertJanitorUnavailable  MaintenanceAlert = "janitor_unavailable"
	MaintenanceAlertCapacityUnavailable MaintenanceAlert = "capacity_unavailable"
	MaintenanceAlertCapacityWarning     MaintenanceAlert = "capacity_warning"
	MaintenanceAlertCapacityExceeded    MaintenanceAlert = "capacity_exceeded"
)

type MaintenanceMetrics struct {
	PassAttempts                  uint64
	PassSuccesses                 uint64
	PassFailures                  uint64
	DeletedIntentCount            uint64
	ReclaimedPayloadCount         uint64
	ReclaimedCanonicalBytes       uint64
	ReclaimedCompressedBytes      uint64
	CapacityStatus                QuotaStatus
	CapacityLogicalBytes          uint64
	CapacityLimitBytes            uint64
	CapacityWarningThresholdBytes uint64
	TotalLogicalSnapshotBytes     uint64
	PhysicalCanonicalBytes        uint64
	PhysicalCompressedBytes       uint64
	OrphanPayloadCount            uint64
	OrphanCanonicalBytes          uint64
	OrphanCompressedBytes         uint64
	LastAttemptedAt               time.Time
	LastSucceededAt               time.Time
	Backlog                       EvidenceLifecycleBacklog
	Alerts                        []MaintenanceAlert
	AlertTransitionCount          uint64
}

type MaintenanceObserver struct {
	mu      sync.RWMutex
	metrics MaintenanceMetrics
}

func NewMaintenanceObserver() *MaintenanceObserver {
	return &MaintenanceObserver{metrics: MaintenanceMetrics{Alerts: []MaintenanceAlert{}}}
}

func (observer *MaintenanceObserver) Snapshot() MaintenanceMetrics {
	if observer == nil {
		return MaintenanceMetrics{Alerts: []MaintenanceAlert{}}
	}
	observer.mu.RLock()
	defer observer.mu.RUnlock()
	metrics := observer.metrics
	metrics.Alerts = append([]MaintenanceAlert{}, observer.metrics.Alerts...)
	return metrics
}

func (observer *MaintenanceObserver) recordAttempt(at time.Time) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.metrics.PassAttempts++
	observer.metrics.LastAttemptedAt = at
}

func (observer *MaintenanceObserver) recordFailure(result maintenancePassResult, alert MaintenanceAlert) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.metrics.PassFailures++
	observer.metrics.LastAttemptedAt = result.at
	observer.recordCleanupLocked(result)
	if result.backlogRead {
		observer.metrics.Backlog = result.backlog
	}
	if alert == MaintenanceAlertCapacityUnavailable {
		observer.metrics.CapacityStatus = QuotaUnavailable
		observer.metrics.CapacityLimitBytes = result.capacity.LimitBytes
		observer.metrics.CapacityWarningThresholdBytes = result.capacity.WarningThresholdBytes
	}
	alerts := []MaintenanceAlert{alert}
	if alert == MaintenanceAlertJanitorUnavailable {
		switch observer.metrics.CapacityStatus {
		case QuotaWarning:
			alerts = append(alerts, MaintenanceAlertCapacityWarning)
		case QuotaExceeded:
			alerts = append(alerts, MaintenanceAlertCapacityExceeded)
		case QuotaUnavailable:
			alerts = append(alerts, MaintenanceAlertCapacityUnavailable)
		}
	}
	observer.setAlertsLocked(alerts)
}

func (observer *MaintenanceObserver) recordSuccess(result maintenancePassResult) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.metrics.PassSuccesses++
	observer.recordCleanupLocked(result)
	observer.metrics.CapacityStatus = result.capacity.Outcome.Status
	observer.metrics.CapacityLogicalBytes = result.capacity.UsedBytes
	observer.metrics.CapacityLimitBytes = result.capacity.LimitBytes
	observer.metrics.CapacityWarningThresholdBytes = result.capacity.WarningThresholdBytes
	observer.metrics.TotalLogicalSnapshotBytes = result.aggregate.LogicalSnapshotBytes
	observer.metrics.PhysicalCanonicalBytes = result.aggregate.PhysicalCanonicalBytes
	observer.metrics.PhysicalCompressedBytes = result.aggregate.PhysicalCompressedBytes
	observer.metrics.OrphanPayloadCount = result.aggregate.OrphanPayloadCount
	observer.metrics.OrphanCanonicalBytes = result.aggregate.OrphanCanonicalBytes
	observer.metrics.OrphanCompressedBytes = result.aggregate.OrphanCompressedBytes
	observer.metrics.LastSucceededAt = result.at
	observer.metrics.Backlog = result.backlog
	alerts := make([]MaintenanceAlert, 0, 1)
	switch result.capacity.Outcome.Status {
	case QuotaWarning:
		alerts = append(alerts, MaintenanceAlertCapacityWarning)
	case QuotaExceeded:
		alerts = append(alerts, MaintenanceAlertCapacityExceeded)
	}
	observer.setAlertsLocked(alerts)
}

func (observer *MaintenanceObserver) recordCanceled(result maintenancePassResult) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.recordCleanupLocked(result)
	if result.backlogRead {
		observer.metrics.Backlog = result.backlog
	}
}

func (observer *MaintenanceObserver) recordCleanupLocked(result maintenancePassResult) {
	observer.metrics.DeletedIntentCount += result.deletedIntentCount
	observer.metrics.ReclaimedPayloadCount += result.reclaimedPayloadCount
	observer.metrics.ReclaimedCanonicalBytes += result.reclaimedCanonicalBytes
	observer.metrics.ReclaimedCompressedBytes += result.reclaimedCompressedBytes
}

func (observer *MaintenanceObserver) setAlertsLocked(alerts []MaintenanceAlert) {
	sort.Slice(alerts, func(left, right int) bool { return alerts[left] < alerts[right] })
	if reflect.DeepEqual(observer.metrics.Alerts, alerts) {
		return
	}
	observer.metrics.Alerts = append([]MaintenanceAlert{}, alerts...)
	observer.metrics.AlertTransitionCount++
}

type MaintenanceWorkerOptions struct {
	Interval          time.Duration
	IntentBatchLimit  uint64
	PayloadBatchLimit uint64
	BacklogProbeLimit uint64
	CapacityPolicy    CapacityPolicy
	Clock             func() time.Time
	Logger            *slog.Logger
}

type MaintenanceWorker struct {
	repository MaintenanceRepository
	observer   *MaintenanceObserver
	options    MaintenanceWorkerOptions
}

func NewMaintenanceWorker(repository MaintenanceRepository, observer *MaintenanceObserver, options MaintenanceWorkerOptions) *MaintenanceWorker {
	if options.Interval == 0 {
		options.Interval = DefaultMaintenanceInterval
	}
	if options.Clock == nil {
		options.Clock = func() time.Time { return time.Now().UTC() }
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &MaintenanceWorker{repository: repository, observer: observer, options: options}
}

func (worker *MaintenanceWorker) Run(ctx context.Context) error {
	if ctx == nil || worker == nil || worker.validate() != nil {
		return ErrInvalidMaintenanceWorker
	}
	if ctx.Err() != nil {
		return nil
	}
	ticker := time.NewTicker(worker.options.Interval)
	defer ticker.Stop()
	for {
		if err := worker.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			worker.options.Logger.Error("evidence maintenance pass failed", "error", maintenanceLogError(err))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func maintenanceLogError(err error) MaintenanceAlert {
	if errors.Is(err, ErrCapacityUnavailable) {
		return MaintenanceAlertCapacityUnavailable
	}
	return MaintenanceAlertJanitorUnavailable
}

func (worker *MaintenanceWorker) RunOnce(ctx context.Context) error {
	if ctx == nil || worker == nil || worker.validate() != nil {
		return ErrInvalidMaintenanceWorker
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	at := worker.options.Clock().Round(0).UTC()
	if at.IsZero() {
		return ErrInvalidMaintenanceWorker
	}
	warningThresholdBytes, err := worker.options.CapacityPolicy.WarningThresholdBytes()
	if err != nil {
		return ErrInvalidMaintenanceWorker
	}
	worker.observer.recordAttempt(at)
	result := maintenancePassResult{
		at: at,
		capacity: CapacityEvaluation{
			LimitBytes:            worker.options.CapacityPolicy.ProjectLimitBytes,
			WarningThresholdBytes: warningThresholdBytes,
		},
	}
	deleted, err := worker.repository.DeleteExpiredCaptureIntents(ctx, worker.options.IntentBatchLimit)
	if err != nil {
		if cancellation := maintenanceCancellation(ctx, err); cancellation != nil {
			worker.observer.recordCanceled(result)
			return cancellation
		}
		worker.observer.recordFailure(result, MaintenanceAlertJanitorUnavailable)
		return fmt.Errorf("delete expired evidence intents: %w", err)
	}
	if uint64(len(deleted)) > worker.options.IntentBatchLimit {
		worker.observer.recordFailure(result, MaintenanceAlertJanitorUnavailable)
		return ErrInvalidMaintenanceResult
	}
	result.deletedIntentCount = uint64(len(deleted))
	if cancellation := maintenanceCancellation(ctx, nil); cancellation != nil {
		worker.observer.recordCanceled(result)
		return cancellation
	}
	collected, err := worker.repository.CollectUnreferencedPayloads(ctx, worker.options.PayloadBatchLimit)
	if err != nil {
		if cancellation := maintenanceCancellation(ctx, err); cancellation != nil {
			worker.observer.recordCanceled(result)
			return cancellation
		}
		worker.observer.recordFailure(result, MaintenanceAlertJanitorUnavailable)
		return fmt.Errorf("collect unreferenced evidence payloads: %w", err)
	}
	if uint64(len(collected)) > worker.options.PayloadBatchLimit {
		worker.observer.recordFailure(result, MaintenanceAlertJanitorUnavailable)
		return ErrInvalidMaintenanceResult
	}
	result.reclaimedPayloadCount = uint64(len(collected))
	seenVersions := make(map[[sha256.Size]byte]struct{}, len(collected))
	seenReceipts := make(map[[sha256.Size]byte]struct{}, len(collected))
	for _, receipt := range collected {
		_, duplicateVersion := seenVersions[receipt.PayloadVersionDigest]
		_, duplicateReceipt := seenReceipts[receipt.ReceiptDigest]
		if receipt.Validate() != nil ||
			duplicateVersion || duplicateReceipt ||
			receipt.CanonicalSizeBytes > math.MaxUint64-result.reclaimedCanonicalBytes ||
			receipt.CompressedSizeBytes > math.MaxUint64-result.reclaimedCompressedBytes {
			worker.observer.recordFailure(result, MaintenanceAlertJanitorUnavailable)
			return ErrInvalidMaintenanceResult
		}
		seenVersions[receipt.PayloadVersionDigest] = struct{}{}
		seenReceipts[receipt.ReceiptDigest] = struct{}{}
		result.reclaimedCanonicalBytes += receipt.CanonicalSizeBytes
		result.reclaimedCompressedBytes += receipt.CompressedSizeBytes
	}
	if cancellation := maintenanceCancellation(ctx, nil); cancellation != nil {
		worker.observer.recordCanceled(result)
		return cancellation
	}
	backlog, err := worker.repository.ReadEvidenceLifecycleBacklog(ctx, worker.options.BacklogProbeLimit)
	if err != nil {
		if cancellation := maintenanceCancellation(ctx, err); cancellation != nil {
			worker.observer.recordCanceled(result)
			return cancellation
		}
		worker.observer.recordFailure(result, MaintenanceAlertJanitorUnavailable)
		return fmt.Errorf("read evidence lifecycle backlog: %w", err)
	}
	if backlog.Validate(worker.options.BacklogProbeLimit) != nil {
		worker.observer.recordFailure(result, MaintenanceAlertJanitorUnavailable)
		return ErrInvalidMaintenanceResult
	}
	result.backlog = backlog
	result.backlogRead = true
	if cancellation := maintenanceCancellation(ctx, nil); cancellation != nil {
		worker.observer.recordCanceled(result)
		return cancellation
	}
	aggregate, err := worker.repository.ReadEvidenceCapacityAggregate(ctx)
	if err != nil || aggregate.Validate() != nil {
		if cancellation := maintenanceCancellation(ctx, err); cancellation != nil {
			worker.observer.recordCanceled(result)
			return cancellation
		}
		worker.observer.recordFailure(result, MaintenanceAlertCapacityUnavailable)
		return fmt.Errorf("read evidence capacity aggregate: %w", errors.Join(ErrCapacityUnavailable, err))
	}
	result.aggregate = aggregate
	if cancellation := maintenanceCancellation(ctx, nil); cancellation != nil {
		worker.observer.recordCanceled(result)
		return cancellation
	}
	capacity, err := worker.options.CapacityPolicy.Evaluate(aggregate.HighestProjectLogicalBytes, 0)
	if err != nil {
		worker.observer.recordFailure(result, MaintenanceAlertCapacityUnavailable)
		return fmt.Errorf("evaluate evidence capacity aggregate: %w", err)
	}
	result.capacity = capacity
	worker.observer.recordSuccess(result)
	return nil
}

func maintenanceCancellation(ctx context.Context, dependencyErr error) error {
	if errors.Is(dependencyErr, context.Canceled) || errors.Is(dependencyErr, context.DeadlineExceeded) {
		return dependencyErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (worker *MaintenanceWorker) validate() error {
	if worker == nil || nilMaintenanceDependency(worker.repository) || worker.observer == nil ||
		worker.options.Interval <= 0 || worker.options.IntentBatchLimit == 0 ||
		worker.options.IntentBatchLimit > MaxMaintenanceBatchSize || worker.options.PayloadBatchLimit == 0 ||
		worker.options.PayloadBatchLimit > MaxMaintenanceBatchSize || worker.options.BacklogProbeLimit == 0 ||
		worker.options.BacklogProbeLimit > MaxMaintenanceBatchSize || worker.options.CapacityPolicy.Validate() != nil ||
		worker.options.Clock == nil || worker.options.Logger == nil {
		return ErrInvalidMaintenanceWorker
	}
	return nil
}

func nilMaintenanceDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return value.IsNil()
	default:
		return false
	}
}

type maintenancePassResult struct {
	at                       time.Time
	deletedIntentCount       uint64
	reclaimedPayloadCount    uint64
	reclaimedCanonicalBytes  uint64
	reclaimedCompressedBytes uint64
	backlog                  EvidenceLifecycleBacklog
	backlogRead              bool
	aggregate                EvidenceCapacityAggregate
	capacity                 CapacityEvaluation
}

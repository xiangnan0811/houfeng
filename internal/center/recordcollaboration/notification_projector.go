package recordcollaboration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"houfeng/internal/center/recordplatform"
)

var (
	ErrInvalidNotificationProjector = errors.New("invalid record notification projector")
	ErrNotificationSourceMissing    = errors.New("record notification source missing")
	ErrNotificationSourceStale      = errors.New("record notification source stale")
)

type NotificationProjectionResult struct {
	NotificationID string
	RecipientCount int
}

func (result NotificationProjectionResult) Validate() error {
	if result.RecipientCount < 0 || (result.RecipientCount == 0) != (result.NotificationID == "") {
		return ErrInvalidNotificationProjector
	}
	if result.NotificationID != "" && !validNotificationID(result.NotificationID) {
		return ErrInvalidNotificationProjector
	}
	return nil
}

type NotificationProjectionStore interface {
	ProjectNotification(context.Context, recordplatform.ClaimedOutboxEventV1) (NotificationProjectionResult, error)
}

type NotificationOutboxQueue interface {
	ClaimOutbox(context.Context, recordplatform.OutboxClaimInputV1) (*recordplatform.ClaimedOutboxEventV1, error)
	CancelOutbox(context.Context, recordplatform.ClaimedOutboxEventV1) error
	RetryOutbox(context.Context, recordplatform.ClaimedOutboxEventV1, time.Duration) error
	MarkOutboxSent(context.Context, recordplatform.ClaimedOutboxEventV1) error
}

type NotificationProjector struct {
	queue      NotificationOutboxQueue
	projection NotificationProjectionStore
	retryAfter time.Duration
}

type NotificationProjectionWorkerOptions struct {
	OwnerID            string
	OwnerLeaseDuration time.Duration
	PollInterval       time.Duration
	Logger             *slog.Logger
}

type NotificationProjectionWorker struct {
	projector *NotificationProjector
	options   NotificationProjectionWorkerOptions
}

func NewNotificationProjectionWorker(projector *NotificationProjector, options NotificationProjectionWorkerOptions) (*NotificationProjectionWorker, error) {
	claim := recordplatform.OutboxClaimInputV1{OwnerID: options.OwnerID, OwnerLeaseDuration: options.OwnerLeaseDuration}
	if projector == nil || claim.Validate() != nil || options.PollInterval <= 0 {
		return nil, ErrInvalidNotificationProjector
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &NotificationProjectionWorker{projector: projector, options: options}, nil
}

func (worker *NotificationProjectionWorker) RunOnce(ctx context.Context) (bool, error) {
	if ctx == nil || worker == nil || worker.projector == nil {
		return false, ErrInvalidNotificationProjector
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return worker.projector.ProjectNext(ctx, recordplatform.OutboxClaimInputV1{
		OwnerID: worker.options.OwnerID, OwnerLeaseDuration: worker.options.OwnerLeaseDuration,
	})
}

func (worker *NotificationProjectionWorker) Run(ctx context.Context) error {
	if ctx == nil || worker == nil || worker.projector == nil || worker.options.Logger == nil || worker.options.PollInterval <= 0 {
		return ErrInvalidNotificationProjector
	}
	if ctx.Err() != nil {
		return nil
	}
	ticker := time.NewTicker(worker.options.PollInterval)
	defer ticker.Stop()
	for {
		if _, err := worker.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			worker.options.Logger.Error("record notification projection pass failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func NewNotificationProjector(queue NotificationOutboxQueue, projection NotificationProjectionStore, retryAfter time.Duration) (*NotificationProjector, error) {
	if nilNotificationDependency(queue) || nilNotificationDependency(projection) || retryAfter.Microseconds() <= 0 {
		return nil, ErrInvalidNotificationProjector
	}
	return &NotificationProjector{queue: queue, projection: projection, retryAfter: retryAfter}, nil
}

func (projector *NotificationProjector) ProjectNext(ctx context.Context, input recordplatform.OutboxClaimInputV1) (bool, error) {
	if ctx == nil || projector == nil || nilNotificationDependency(projector.queue) || nilNotificationDependency(projector.projection) || input.Validate() != nil {
		return false, ErrInvalidNotificationProjector
	}
	claim, err := projector.queue.ClaimOutbox(ctx, input)
	if err != nil {
		return false, err
	}
	if claim == nil {
		return false, nil
	}
	if claim.Validate() != nil {
		return true, ErrInvalidNotificationProjector
	}
	if _, ok := NotificationEventKindFromOutbox(claim.Event.EventKind); !ok {
		return true, projector.queue.CancelOutbox(ctx, *claim)
	}
	result, err := projector.projection.ProjectNotification(ctx, *claim)
	if errors.Is(err, ErrNotificationSourceMissing) || errors.Is(err, ErrNotificationSourceStale) || errors.Is(err, ErrInvalidNotificationFacts) {
		return true, projector.queue.CancelOutbox(ctx, *claim)
	}
	if err != nil {
		if retryErr := projector.queue.RetryOutbox(ctx, *claim, projector.retryAfter); retryErr != nil {
			return true, errors.Join(err, fmt.Errorf("retry record notification outbox: %w", retryErr))
		}
		return true, err
	}
	if result.Validate() != nil {
		if retryErr := projector.queue.RetryOutbox(ctx, *claim, projector.retryAfter); retryErr != nil {
			return true, errors.Join(ErrInvalidNotificationProjector, retryErr)
		}
		return true, ErrInvalidNotificationProjector
	}
	return true, projector.queue.MarkOutboxSent(ctx, *claim)
}

func NotificationEventKindFromOutbox(kind string) (NotificationEventKind, bool) {
	switch kind {
	case recordplatform.OutboxEventKindRecordOwnerChanged:
		return NotificationEventRecordOwnerChanged, true
	case recordplatform.OutboxEventKindRecordParticipantChanged:
		return NotificationEventRecordParticipantChanged, true
	case recordplatform.OutboxEventKindRecordActionAssigned:
		return NotificationEventActionAssigned, true
	case recordplatform.OutboxEventKindRecordActionCompleted:
		return NotificationEventActionCompleted, true
	case recordplatform.OutboxEventKindRecordActionCancelled:
		return NotificationEventActionCancelled, true
	case recordplatform.OutboxEventKindRecordCommentReplied:
		return NotificationEventCommentReplied, true
	case recordplatform.OutboxEventKindRecordCommentMentioned:
		return NotificationEventCommentMentioned, true
	default:
		return "", false
	}
}

func nilNotificationDependency(dependency any) bool {
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

func validNotificationID(value string) bool {
	return ValidateNotificationID(value) == nil
}

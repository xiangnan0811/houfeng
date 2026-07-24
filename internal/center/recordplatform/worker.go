package recordplatform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"
)

var (
	ErrInvalidOutboxWorker  = errors.New("invalid outbox worker")
	ErrOutboxHandlerMissing = errors.New("outbox handler missing")
)

// OutboxRepository is the durable boundary for claiming and finalizing an
// identity-only event. Every state transition must use the included owner
// generation fence and PostgreSQL transaction time.
type OutboxRepository interface {
	ClaimOutbox(context.Context, OutboxClaimInputV1) (*ClaimedOutboxEventV1, error)
	CancelOutbox(context.Context, ClaimedOutboxEventV1) error
	RetryOutbox(context.Context, ClaimedOutboxEventV1, time.Duration) error
	MarkOutboxSent(context.Context, ClaimedOutboxEventV1) error
}

// RenderedDelivery is transient delivery material returned by fresh
// authorization. It is intentionally opaque and cannot cross persistence APIs.
type RenderedDelivery any

// FreshAuthDecision is re-derived on every delivery attempt. CurrentEpoch must
// exactly equal the event's captured authorization epoch before sending.
type FreshAuthDecision struct {
	Allowed      bool
	CurrentEpoch ContentEpoch
}

// FreshOutboxAuthorizer re-evaluates current visibility and renders transient
// delivery material after the claim transaction commits.
type FreshOutboxAuthorizer interface {
	AuthorizeAndRender(context.Context, OutboxEvent) (RenderedDelivery, FreshAuthDecision, error)
}

// OutboxSender transmits already-rendered delivery material outside database
// transactions.
type OutboxSender interface {
	SendOutbox(context.Context, RenderedDelivery) error
}

// OutboxWorkerOptions fixes an immutable worker identity and lease/retry
// bounds. Database time, not this process's clock, decides durable liveness.
type OutboxWorkerOptions struct {
	OwnerID            string
	OwnerLeaseDuration time.Duration
	RetryDelay         time.Duration
	PollInterval       time.Duration
	Logger             *slog.Logger
}

// OutboxWorker claims one committed event at a time, then performs all
// authorization, rendering, and network work outside the database transaction.
type OutboxWorker struct {
	repository OutboxRepository
	authorizer FreshOutboxAuthorizer
	sender     OutboxSender
	options    OutboxWorkerOptions
}

// NewOutboxWorker constructs a delivery worker. Configuration is validated by
// RunOnce/Run so callers can assemble dependencies without an error branch.
func NewOutboxWorker(repository OutboxRepository, authorizer FreshOutboxAuthorizer, sender OutboxSender, options OutboxWorkerOptions) *OutboxWorker {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.PollInterval == 0 {
		options.PollInterval = time.Second
	}
	return &OutboxWorker{repository: repository, authorizer: authorizer, sender: sender, options: options}
}

// Run repeatedly attempts one queued event and keeps transient repository or
// delivery failures local to the worker rather than ending the center process.
func (worker *OutboxWorker) Run(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidOutboxWorker
	}
	if err := worker.validate(); err != nil {
		return err
	}
	ticker := time.NewTicker(worker.options.PollInterval)
	defer ticker.Stop()
	for {
		if err := worker.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			worker.options.Logger.Error("record outbox pass failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// RunOnce performs exactly one durable claim. ClaimOutbox returns only after
// its transaction commits, so no authorization, render, or send can occur
// inside the claim transaction.
func (worker *OutboxWorker) RunOnce(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidOutboxWorker
	}
	if err := worker.validate(); err != nil {
		return err
	}
	claim, err := worker.repository.ClaimOutbox(ctx, OutboxClaimInputV1{
		OwnerID:            worker.options.OwnerID,
		OwnerLeaseDuration: worker.options.OwnerLeaseDuration,
	})
	if err != nil {
		return fmt.Errorf("claim outbox event: %w", err)
	}
	if claim == nil {
		return nil
	}
	if err := claim.Validate(); err != nil {
		return fmt.Errorf("validate claimed outbox event: %w", err)
	}

	delivery, decision, err := worker.authorizer.AuthorizeAndRender(ctx, claim.Event)
	if err != nil {
		if errors.Is(err, ErrOutboxHandlerMissing) {
			return worker.cancel(ctx, *claim, "cancel unhandled outbox event")
		}
		return worker.retry(ctx, *claim)
	}
	if !decision.Allowed || decision.CurrentEpoch != ContentEpoch(claim.Event.AuthorizationEpoch) {
		return worker.cancel(ctx, *claim, "cancel unauthorized outbox event")
	}
	if err := worker.sender.SendOutbox(ctx, delivery); err != nil {
		return worker.retry(ctx, *claim)
	}
	if err := worker.repository.MarkOutboxSent(ctx, *claim); err != nil {
		return fmt.Errorf("mark outbox event sent: %w", err)
	}
	return nil
}

func (worker *OutboxWorker) retry(ctx context.Context, claim ClaimedOutboxEventV1) error {
	if err := worker.repository.RetryOutbox(ctx, claim, worker.options.RetryDelay); err != nil {
		return fmt.Errorf("schedule outbox retry: %w", err)
	}
	return nil
}

func (worker *OutboxWorker) cancel(ctx context.Context, claim ClaimedOutboxEventV1, operation string) error {
	if err := worker.repository.CancelOutbox(ctx, claim); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func (worker *OutboxWorker) validate() error {
	if worker == nil || isNilOutboxWorkerDependency(worker.repository) || isNilOutboxWorkerDependency(worker.authorizer) || isNilOutboxWorkerDependency(worker.sender) {
		return ErrInvalidOutboxWorker
	}
	if err := (OutboxClaimInputV1{OwnerID: worker.options.OwnerID, OwnerLeaseDuration: worker.options.OwnerLeaseDuration}).Validate(); err != nil {
		return fmt.Errorf("%w: claim: %w", ErrInvalidOutboxWorker, err)
	}
	if worker.options.RetryDelay.Microseconds() <= 0 || worker.options.PollInterval <= 0 {
		return fmt.Errorf("%w: retry or poll interval", ErrInvalidOutboxWorker)
	}
	return nil
}

func isNilOutboxWorkerDependency(dependency any) bool {
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

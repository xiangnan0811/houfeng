package recordcollaboration

import (
	"context"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

type ActionApplicationOptions struct {
	IdempotencyOwnerID string
	OwnerLeaseDuration time.Duration
	IdempotencyTTL     time.Duration
	OutboxTTL          time.Duration
}

func (options ActionApplicationOptions) validate() error {
	owner := recordplatform.OwnerLease{OwnerID: options.IdempotencyOwnerID, Generation: 1, ExpiresAt: time.Unix(1, 0).UTC()}
	if owner.Validate() != nil || options.OwnerLeaseDuration.Microseconds() <= 0 ||
		options.IdempotencyTTL.Microseconds() <= options.OwnerLeaseDuration.Microseconds() || options.OutboxTTL.Microseconds() <= 0 {
		return ErrInvalidActionRequest
	}
	return nil
}

type ActionCreateApplicationRequest struct {
	Actor          recordauth.ActorScope
	RecordID       string
	Fields         ActionFieldValues
	IdempotencyKey string
}

type ActionUpdateApplicationRequest struct {
	Actor           recordauth.ActorScope
	RecordID        string
	ActionID        string
	ExpectedVersion uint64
	Fields          ActionFieldValues
	IdempotencyKey  string
}

type ActionTransitionApplicationRequest struct {
	Actor           recordauth.ActorScope
	RecordID        string
	ActionID        string
	ExpectedVersion uint64
	IdempotencyKey  string
}

type ActionApplication struct {
	service *ActionService
	options ActionApplicationOptions
}

func NewActionApplication(service *ActionService, options ActionApplicationOptions) (*ActionApplication, error) {
	if service == nil || options.validate() != nil {
		return nil, ErrInvalidActionRequest
	}
	return &ActionApplication{service: service, options: options}, nil
}

func (application *ActionApplication) CreateAction(ctx context.Context, request ActionCreateApplicationRequest) (ActionMutationResult, error) {
	if ctx == nil || application == nil || application.service == nil || application.options.validate() != nil {
		return ActionMutationResult{}, ErrInvalidActionRequest
	}
	return application.service.CreateAction(ctx, ActionCreateRequest{
		Actor: request.Actor, RecordID: request.RecordID, Fields: request.Fields, IdempotencyKey: request.IdempotencyKey,
		IdempotencyOwnerID: application.options.IdempotencyOwnerID, OwnerLeaseDuration: application.options.OwnerLeaseDuration,
		IdempotencyTTL: application.options.IdempotencyTTL, OutboxTTL: application.options.OutboxTTL,
	})
}

func (application *ActionApplication) UpdateAction(ctx context.Context, request ActionUpdateApplicationRequest) (ActionMutationResult, error) {
	if ctx == nil || application == nil || application.service == nil || application.options.validate() != nil {
		return ActionMutationResult{}, ErrInvalidActionRequest
	}
	return application.service.UpdateAction(ctx, ActionUpdateRequest{
		ActionCommandRequest: application.commandRequest(request.Actor, request.RecordID, request.ActionID, request.ExpectedVersion, request.IdempotencyKey),
		Fields:               request.Fields,
	})
}

func (application *ActionApplication) CompleteAction(ctx context.Context, request ActionTransitionApplicationRequest) (ActionMutationResult, error) {
	return application.transition(ctx, request, (*ActionService).CompleteAction)
}

func (application *ActionApplication) CancelAction(ctx context.Context, request ActionTransitionApplicationRequest) (ActionMutationResult, error) {
	return application.transition(ctx, request, (*ActionService).CancelAction)
}

func (application *ActionApplication) ReopenAction(ctx context.Context, request ActionTransitionApplicationRequest) (ActionMutationResult, error) {
	return application.transition(ctx, request, (*ActionService).ReopenAction)
}

func (application *ActionApplication) transition(ctx context.Context, request ActionTransitionApplicationRequest, operation func(*ActionService, context.Context, ActionCommandRequest) (ActionMutationResult, error)) (ActionMutationResult, error) {
	if ctx == nil || application == nil || application.service == nil || application.options.validate() != nil || operation == nil {
		return ActionMutationResult{}, ErrInvalidActionRequest
	}
	return operation(application.service, ctx, application.commandRequest(request.Actor, request.RecordID, request.ActionID, request.ExpectedVersion, request.IdempotencyKey))
}

func (application *ActionApplication) commandRequest(actor recordauth.ActorScope, recordID, actionID string, expectedVersion uint64, key string) ActionCommandRequest {
	return ActionCommandRequest{
		Actor: actor, RecordID: recordID, ActionID: actionID, ExpectedVersion: expectedVersion,
		IdempotencyKey: key, IdempotencyOwnerID: application.options.IdempotencyOwnerID,
		OwnerLeaseDuration: application.options.OwnerLeaseDuration, IdempotencyTTL: application.options.IdempotencyTTL,
		OutboxTTL: application.options.OutboxTTL,
	}
}

package recordcollaboration

import (
	"context"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

type WatchApplicationOptions struct {
	IdempotencyOwnerID string
	OwnerLeaseDuration time.Duration
	IdempotencyTTL     time.Duration
}

func (options WatchApplicationOptions) validate() error {
	owner := recordplatform.OwnerLease{OwnerID: options.IdempotencyOwnerID, Generation: 1, ExpiresAt: time.Unix(1, 0).UTC()}
	if owner.Validate() != nil || options.OwnerLeaseDuration.Microseconds() <= 0 || options.IdempotencyTTL.Microseconds() <= options.OwnerLeaseDuration.Microseconds() {
		return ErrInvalidWatchRequest
	}
	return nil
}

type WatchSetApplicationRequest struct {
	Actor           recordauth.ActorScope
	RecordID        string
	ExpectedVersion uint64
	Preference      FollowerPreference
	IdempotencyKey  string
}

type WatchReadApplicationRequest struct {
	Actor    recordauth.ActorScope
	RecordID string
}

type WatchApplication struct {
	service *WatchService
	options WatchApplicationOptions
}

func NewWatchApplication(service *WatchService, options WatchApplicationOptions) (*WatchApplication, error) {
	if service == nil || options.validate() != nil {
		return nil, ErrInvalidWatchRequest
	}
	return &WatchApplication{service: service, options: options}, nil
}

func (application *WatchApplication) SetWatch(ctx context.Context, request WatchSetApplicationRequest) (WatchStatus, error) {
	if !application.ready(ctx) {
		return WatchStatus{}, ErrInvalidWatchRequest
	}
	return application.service.SetWatch(ctx, WatchSetRequest{
		Actor: request.Actor, RecordID: request.RecordID, ExpectedVersion: request.ExpectedVersion,
		Preference: request.Preference, IdempotencyKey: request.IdempotencyKey,
		IdempotencyOwnerID: application.options.IdempotencyOwnerID,
		OwnerLeaseDuration: application.options.OwnerLeaseDuration, IdempotencyTTL: application.options.IdempotencyTTL,
	})
}

func (application *WatchApplication) GetWatch(ctx context.Context, request WatchReadApplicationRequest) (WatchStatus, error) {
	if !application.ready(ctx) {
		return WatchStatus{}, ErrInvalidWatchRequest
	}
	return application.service.GetWatch(ctx, WatchReadRequest{Actor: request.Actor, RecordID: request.RecordID})
}

func (application *WatchApplication) ready(ctx context.Context) bool {
	return ctx != nil && application != nil && application.service != nil && application.options.validate() == nil
}

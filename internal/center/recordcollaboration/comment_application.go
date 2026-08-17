package recordcollaboration

import (
	"context"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

type CommentApplicationOptions struct {
	IdempotencyOwnerID string
	OwnerLeaseDuration time.Duration
	IdempotencyTTL     time.Duration
	OutboxTTL          time.Duration
}

func (options CommentApplicationOptions) validate() error {
	owner := recordplatform.OwnerLease{OwnerID: options.IdempotencyOwnerID, Generation: 1, ExpiresAt: time.Unix(1, 0).UTC()}
	if owner.Validate() != nil || options.OwnerLeaseDuration.Microseconds() <= 0 ||
		options.IdempotencyTTL.Microseconds() <= options.OwnerLeaseDuration.Microseconds() || options.OutboxTTL.Microseconds() <= 0 {
		return ErrInvalidCommentRequest
	}
	return nil
}

type CommentCreateApplicationRequest struct {
	Actor            recordauth.ActorScope
	RecordID         string
	BodyMarkdown     string
	ReplyToCommentID string
	MentionUserIDs   []string
	IdempotencyKey   string
}

type CommentEditApplicationRequest struct {
	Actor           recordauth.ActorScope
	RecordID        string
	CommentID       string
	ExpectedVersion uint64
	BodyMarkdown    string
	MentionUserIDs  []string
	IdempotencyKey  string
}

type CommentRedactApplicationRequest struct {
	Actor           recordauth.ActorScope
	RecordID        string
	CommentID       string
	ExpectedVersion uint64
	IdempotencyKey  string
}

type CommentListApplicationRequest struct {
	Actor    recordauth.ActorScope
	RecordID string
	Limit    uint64
}

type CommentApplication struct {
	service *CommentService
	options CommentApplicationOptions
}

func NewCommentApplication(service *CommentService, options CommentApplicationOptions) (*CommentApplication, error) {
	if service == nil || options.validate() != nil {
		return nil, ErrInvalidCommentRequest
	}
	return &CommentApplication{service: service, options: options}, nil
}

func (application *CommentApplication) CreateComment(ctx context.Context, request CommentCreateApplicationRequest) (CommentMutationResult, error) {
	if !application.ready(ctx) {
		return CommentMutationResult{}, ErrInvalidCommentRequest
	}
	return application.service.CreateComment(ctx, CommentCreateRequest{
		Actor: request.Actor, RecordID: request.RecordID, BodyMarkdown: request.BodyMarkdown,
		ReplyToCommentID: request.ReplyToCommentID, MentionUserIDs: append([]string(nil), request.MentionUserIDs...),
		IdempotencyKey: request.IdempotencyKey, IdempotencyOwnerID: application.options.IdempotencyOwnerID,
		OwnerLeaseDuration: application.options.OwnerLeaseDuration, IdempotencyTTL: application.options.IdempotencyTTL,
		OutboxTTL: application.options.OutboxTTL,
	})
}

func (application *CommentApplication) EditComment(ctx context.Context, request CommentEditApplicationRequest) (CommentMutationResult, error) {
	if !application.ready(ctx) {
		return CommentMutationResult{}, ErrInvalidCommentRequest
	}
	return application.service.EditComment(ctx, CommentEditRequest{
		CommentCommandRequest: application.commandRequest(request.Actor, request.RecordID, request.CommentID, request.ExpectedVersion, request.IdempotencyKey),
		BodyMarkdown:          request.BodyMarkdown, MentionUserIDs: append([]string(nil), request.MentionUserIDs...),
	})
}

func (application *CommentApplication) RedactComment(ctx context.Context, request CommentRedactApplicationRequest) (CommentMutationResult, error) {
	if !application.ready(ctx) {
		return CommentMutationResult{}, ErrInvalidCommentRequest
	}
	return application.service.RedactComment(ctx, application.commandRequest(
		request.Actor, request.RecordID, request.CommentID, request.ExpectedVersion, request.IdempotencyKey,
	))
}

func (application *CommentApplication) ListComments(ctx context.Context, request CommentListApplicationRequest) ([]CommentRecord, error) {
	if !application.ready(ctx) {
		return nil, ErrInvalidCommentRequest
	}
	return application.service.ListComments(ctx, CommentListRequest{
		Actor: request.Actor, RecordID: request.RecordID, Limit: request.Limit,
	})
}

func (application *CommentApplication) commandRequest(actor recordauth.ActorScope, recordID, commentID string, expectedVersion uint64, key string) CommentCommandRequest {
	return CommentCommandRequest{
		Actor: actor, RecordID: recordID, CommentID: commentID, ExpectedVersion: expectedVersion,
		IdempotencyKey: key, IdempotencyOwnerID: application.options.IdempotencyOwnerID,
		OwnerLeaseDuration: application.options.OwnerLeaseDuration, IdempotencyTTL: application.options.IdempotencyTTL,
		OutboxTTL: application.options.OutboxTTL,
	}
}

func (application *CommentApplication) ready(ctx context.Context) bool {
	return ctx != nil && application != nil && application.service != nil && application.options.validate() == nil
}

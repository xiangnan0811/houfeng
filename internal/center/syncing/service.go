package syncing

import (
	"context"
	"errors"
	"time"

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/observations"
)

var (
	ErrBindingNotAccepted = enrollment.ErrBindingNotAccepted
	ErrInvalidSyncToken   = enrollment.ErrInvalidSyncToken
	ErrHeartbeatRequired  = errors.New("heartbeat carrier required for sync batch")
)

type HeartbeatPayload = enrollment.HeartbeatPayload

// CommandResult carries the output of an executed pending action back from the agent.
type CommandResult struct {
	ActionID  string
	CommandID string
	Stdout    string
	Stderr    string
	ExitCode  int
}

type Batch struct {
	NodeID         string
	SyncToken      string
	Heartbeats     []HeartbeatPayload
	Observations   observations.BatchWrite
	CommandResults []CommandResult
}

type Result struct {
	AcceptedAt time.Time
	Plan       agentplan.SyncPlan
}

type PostSyncProcessor interface {
	AfterSuccessfulSync(context.Context, Batch, Result) error
}

type Repository interface {
	ApplyBatch(context.Context, Batch) (Result, error)
}

type Service struct {
	repo     Repository
	postSync PostSyncProcessor
}

func NewService(repo Repository, postSync ...PostSyncProcessor) *Service {
	service := &Service{repo: repo}
	if len(postSync) > 0 {
		service.postSync = postSync[0]
	}
	return service
}

func (s *Service) SyncBatch(ctx context.Context, batch Batch) (Result, error) {
	result, err := s.repo.ApplyBatch(ctx, batch)
	if err != nil {
		return Result{}, err
	}
	if s.postSync != nil {
		if err := s.postSync.AfterSuccessfulSync(ctx, batch, result); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

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

type Batch struct {
	NodeID       string
	SyncToken    string
	Heartbeats   []HeartbeatPayload
	Observations observations.BatchWrite
}

type Result struct {
	AcceptedAt time.Time
	Plan       agentplan.SyncPlan
}

type Repository interface {
	ApplyBatch(context.Context, Batch) (Result, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) SyncBatch(ctx context.Context, batch Batch) (Result, error) {
	return s.repo.ApplyBatch(ctx, batch)
}

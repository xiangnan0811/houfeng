package syncing

import (
	"context"
	"errors"

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

type Repository interface {
	ApplyBatch(context.Context, Batch) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) SyncBatch(ctx context.Context, batch Batch) error {
	return s.repo.ApplyBatch(ctx, batch)
}

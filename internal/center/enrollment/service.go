package enrollment

import (
	"context"
	"errors"
	"time"

	"houfeng/internal/center/nodes"
)

var ErrBindingNotAccepted = errors.New("binding not accepted")

type Repository interface {
	IssueEnrollmentToken(context.Context, string) (string, error)
	FindNodeByEnrollmentToken(context.Context, string) (nodes.Record, error)
	UpdateBindingState(context.Context, BindingUpdate) (nodes.Record, error)
	RecordHeartbeat(context.Context, HeartbeatWrite) error
	TouchHeartbeatState(context.Context, HeartbeatWrite) (nodes.Record, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) IssueNodeEnrollmentToken(ctx context.Context, nodeID string) (string, error) {
	return s.repo.IssueEnrollmentToken(ctx, nodeID)
}

func (s *Service) EnrollNode(ctx context.Context, input EnrollInput) (EnrollResult, error) {
	record, err := s.repo.FindNodeByEnrollmentToken(ctx, input.Token)
	if err != nil {
		return EnrollResult{}, err
	}

	update := BindingUpdate{
		NodeID:             record.NodeID,
		BindingStatus:      record.BindingStatus,
		BindingFingerprint: record.BindingFingerprint,
	}

	switch record.BindingStatus {
	case nodes.BindingUnbound:
		update.BindingStatus = nodes.BindingBound
		update.BindingFingerprint = input.Fingerprint
	case nodes.BindingBound:
		if record.BindingFingerprint == input.Fingerprint {
			update.BindingStatus = nodes.BindingBound
			update.BindingFingerprint = input.Fingerprint
			break
		}

		update.BindingStatus = nodes.BindingPendingConfirmation
		update.BindingFingerprint = record.BindingFingerprint
	}

	record, err = s.repo.UpdateBindingState(ctx, update)
	if err != nil {
		return EnrollResult{}, err
	}

	return EnrollResult{
		NodeID:        record.NodeID,
		BindingStatus: record.BindingStatus,
		Status:        "ok",
	}, nil
}

func (s *Service) RecordHeartbeatSync(ctx context.Context, input SyncInput) error {
	for _, heartbeat := range input.Heartbeats {
		write := HeartbeatWrite{
			NodeID:       input.NodeID,
			ObservedAt:   heartbeat.ObservedAt,
			ReceivedAt:   time.Now().UTC(),
			AgentVersion: heartbeat.AgentVersion,
			Fingerprint:  heartbeat.Fingerprint,
			SyncBatchID:  heartbeat.SyncBatchID,
		}

		record, err := s.repo.TouchHeartbeatState(ctx, write)
		if err != nil {
			return err
		}
		if record.BindingStatus != nodes.BindingBound {
			return ErrBindingNotAccepted
		}
		if err := s.repo.RecordHeartbeat(ctx, write); err != nil {
			return err
		}
	}

	return nil
}

package enrollment

import (
	"context"
	"errors"
	"time"

	"houfeng/internal/center/nodes"
)

var (
	ErrBindingNotAccepted     = errors.New("binding not accepted")
	ErrInvalidEnrollmentToken = errors.New("invalid enrollment token")
)

type Repository interface {
	IssueEnrollmentToken(context.Context, string) (string, error)
	FindNodeByEnrollmentToken(context.Context, string) (nodes.Record, error)
	GetNode(context.Context, string) (nodes.Record, error)
	UpdateBindingState(context.Context, BindingUpdate) (nodes.Record, error)
	RecordAcceptedHeartbeats(context.Context, []HeartbeatWrite) error
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
		if errors.Is(err, nodes.ErrNodeNotFound) {
			return EnrollResult{}, ErrInvalidEnrollmentToken
		}
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
	}, nil
}

func (s *Service) RecordHeartbeatSync(ctx context.Context, input SyncInput) error {
	record, err := s.repo.GetNode(ctx, input.NodeID)
	if err != nil {
		return err
	}
	if record.BindingStatus != nodes.BindingBound {
		return ErrBindingNotAccepted
	}

	receivedAt := time.Now().UTC()
	writes := make([]HeartbeatWrite, 0, len(input.Heartbeats))
	for _, heartbeat := range input.Heartbeats {
		writes = append(writes, HeartbeatWrite{
			NodeID:       input.NodeID,
			ObservedAt:   heartbeat.ObservedAt,
			ReceivedAt:   receivedAt,
			AgentVersion: heartbeat.AgentVersion,
			Fingerprint:  heartbeat.Fingerprint,
			SyncBatchID:  heartbeat.SyncBatchID,
		})
	}

	return s.repo.RecordAcceptedHeartbeats(ctx, writes)
}

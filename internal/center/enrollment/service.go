package enrollment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"houfeng/internal/center/nodes"
)

var (
	ErrBindingNotAccepted     = errors.New("binding not accepted")
	ErrInvalidEnrollmentToken = errors.New("invalid enrollment token")
	ErrInvalidSyncToken       = errors.New("invalid sync token")
)

type Repository interface {
	IssueEnrollmentToken(context.Context, string) (string, error)
	IssueSyncToken(context.Context, string) (string, error)
	ApplyEnrollment(context.Context, EnrollInput) (nodes.Record, error)
	GetNode(context.Context, string) (nodes.Record, error)
	RecordAcceptedHeartbeats(context.Context, string, []HeartbeatWrite) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func hashSyncToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) IssueNodeEnrollmentToken(ctx context.Context, nodeID string) (string, error) {
	return s.repo.IssueEnrollmentToken(ctx, nodeID)
}

func (s *Service) EnrollNode(ctx context.Context, input EnrollInput) (EnrollResult, error) {
	record, err := s.repo.ApplyEnrollment(ctx, input)
	if err != nil {
		if errors.Is(err, nodes.ErrNodeNotFound) {
			return EnrollResult{}, ErrInvalidEnrollmentToken
		}
		return EnrollResult{}, err
	}

	result := EnrollResult{
		NodeID:        record.NodeID,
		BindingStatus: record.BindingStatus,
	}
	if record.BindingStatus != nodes.BindingBound {
		return result, nil
	}

	syncToken, err := s.repo.IssueSyncToken(ctx, record.NodeID)
	if err != nil {
		return EnrollResult{}, err
	}
	result.SyncToken = syncToken
	return result, nil
}

func (s *Service) RecordHeartbeatSync(ctx context.Context, input SyncInput) error {
	if input.SyncToken == "" {
		return ErrInvalidSyncToken
	}

	record, err := s.repo.GetNode(ctx, input.NodeID)
	if err != nil {
		return err
	}
	if record.BindingStatus != nodes.BindingBound {
		return ErrBindingNotAccepted
	}
	if record.SyncTokenHash == "" || record.SyncTokenHash != hashSyncToken(input.SyncToken) {
		return ErrInvalidSyncToken
	}

	receivedAt := time.Now().UTC()
	writes := make([]HeartbeatWrite, 0, len(input.Heartbeats))
	for _, heartbeat := range input.Heartbeats {
		if heartbeat.Fingerprint != record.BindingFingerprint {
			return ErrBindingNotAccepted
		}
		writes = append(writes, HeartbeatWrite{
			NodeID:       input.NodeID,
			ObservedAt:   heartbeat.ObservedAt,
			ReceivedAt:   receivedAt,
			AgentVersion: heartbeat.AgentVersion,
			Fingerprint:  heartbeat.Fingerprint,
			SyncBatchID:  heartbeat.SyncBatchID,
		})
	}

	return s.repo.RecordAcceptedHeartbeats(ctx, input.SyncToken, writes)
}

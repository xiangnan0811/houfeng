package enrollment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"houfeng/internal/center/monitoringinstances"
)

var (
	ErrBindingNotAccepted     = errors.New("binding not accepted")
	ErrInvalidEnrollmentToken = errors.New("invalid enrollment token")
	ErrInvalidSyncToken       = errors.New("invalid sync token")
)

type Repository interface {
	IssueEnrollmentToken(context.Context, string) (string, error)
	ApplyEnrollment(context.Context, EnrollInput) (monitoringinstances.Record, string, error)
	GetMonitoringInstance(context.Context, string) (monitoringinstances.Record, error)
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

func (s *Service) IssueMonitoringInstanceEnrollmentToken(ctx context.Context, monitoringInstanceID string) (string, error) {
	return s.repo.IssueEnrollmentToken(ctx, monitoringInstanceID)
}

func (s *Service) EnrollMonitoringInstance(ctx context.Context, input EnrollInput) (EnrollResult, error) {
	record, syncToken, err := s.repo.ApplyEnrollment(ctx, input)
	if err != nil {
		if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			return EnrollResult{}, ErrInvalidEnrollmentToken
		}
		return EnrollResult{}, err
	}

	return EnrollResult{
		MonitoringInstanceID: record.MonitoringInstanceID,
		BindingStatus:        record.BindingStatus,
		SyncToken:            syncToken,
	}, nil
}

func (s *Service) RecordHeartbeatSync(ctx context.Context, input SyncInput) error {
	if input.SyncToken == "" {
		return ErrInvalidSyncToken
	}

	record, err := s.repo.GetMonitoringInstance(ctx, input.MonitoringInstanceID)
	if err != nil {
		return err
	}
	if record.BindingStatus != monitoringinstances.BindingBound {
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
			MonitoringInstanceID: input.MonitoringInstanceID,
			ObservedAt:           heartbeat.ObservedAt,
			ReceivedAt:           receivedAt,
			AgentVersion:         heartbeat.AgentVersion,
			Fingerprint:          heartbeat.Fingerprint,
			SyncBatchID:          heartbeat.SyncBatchID,
			IsBackfilled:         heartbeat.IsBackfilled,
		})
	}

	return s.repo.RecordAcceptedHeartbeats(ctx, input.SyncToken, writes)
}

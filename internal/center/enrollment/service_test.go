package enrollment

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/monitoringinstances"
)

func TestIssueEnrollmentTokenReturnsToken(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		issuedToken: "enroll_token",
	}
	service := NewService(repo)

	token, err := service.IssueMonitoringInstanceEnrollmentToken(context.Background(), "mi_123")
	if err != nil {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken() error = %v", err)
	}

	if token != "enroll_token" {
		t.Fatalf("token = %q, want %q", token, "enroll_token")
	}
	if repo.issuedMonitoringInstanceID != "mi_123" {
		t.Fatalf("issuedMonitoringInstanceID = %q, want %q", repo.issuedMonitoringInstanceID, "mi_123")
	}
}

func TestEnrollMonitoringInstanceBindsUnboundMonitoringInstanceAndIssuesSyncToken(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		enrollmentResultByToken: map[string]monitoringinstances.Record{
			"plain-token": {
				MonitoringInstanceID: "mi_123",
				BindingStatus:        monitoringinstances.BindingBound,
				BindingFingerprint:   "fp-new",
			},
		},
		atomicSyncTokenByToken: map[string]string{"plain-token": "sync-token-001"},
	}
	service := NewService(repo)

	result, err := service.EnrollMonitoringInstance(context.Background(), EnrollInput{
		Token:       "plain-token",
		Fingerprint: "fp-new",
	})
	if err != nil {
		t.Fatalf("EnrollMonitoringInstance() error = %v", err)
	}

	if len(repo.enrollmentCalls) != 1 {
		t.Fatalf("enrollmentCalls = %d, want 1", len(repo.enrollmentCalls))
	}
	if repo.enrollmentCalls[0] != (EnrollInput{Token: "plain-token", Fingerprint: "fp-new"}) {
		t.Fatalf("enrollmentCalls[0] = %#v", repo.enrollmentCalls[0])
	}
	if len(repo.issuedSyncMonitoringInstanceIDs) != 0 {
		t.Fatalf("issuedSyncMonitoringInstanceIDs len = %d, want 0 because sync token is issued atomically by ApplyEnrollment", len(repo.issuedSyncMonitoringInstanceIDs))
	}

	if result != (EnrollResult{
		MonitoringInstanceID: "mi_123",
		BindingStatus:        monitoringinstances.BindingBound,
		SyncToken:            "sync-token-001",
	}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestEnrollMonitoringInstanceMarksConflictForNewFingerprintWithoutIssuingSyncToken(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		enrollmentResultByToken: map[string]monitoringinstances.Record{
			"plain-token": {
				MonitoringInstanceID: "mi_456",
				BindingStatus:        monitoringinstances.BindingPendingConfirmation,
				BindingFingerprint:   "fp-old",
			},
		},
	}
	service := NewService(repo)

	result, err := service.EnrollMonitoringInstance(context.Background(), EnrollInput{
		Token:       "plain-token",
		Fingerprint: "fp-new",
	})
	if err != nil {
		t.Fatalf("EnrollMonitoringInstance() error = %v", err)
	}

	if len(repo.enrollmentCalls) != 1 {
		t.Fatalf("enrollmentCalls = %d, want 1", len(repo.enrollmentCalls))
	}
	if repo.enrollmentCalls[0] != (EnrollInput{Token: "plain-token", Fingerprint: "fp-new"}) {
		t.Fatalf("enrollmentCalls[0] = %#v", repo.enrollmentCalls[0])
	}
	if len(repo.issuedSyncMonitoringInstanceIDs) != 0 {
		t.Fatalf("issuedSyncMonitoringInstanceIDs len = %d, want 0", len(repo.issuedSyncMonitoringInstanceIDs))
	}

	if result != (EnrollResult{
		MonitoringInstanceID: "mi_456",
		BindingStatus:        monitoringinstances.BindingPendingConfirmation,
	}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestEnrollMonitoringInstanceReturnsInvalidEnrollmentToken(t *testing.T) {
	t.Parallel()

	service := NewService(&fakeRepository{enrollmentResultByToken: map[string]monitoringinstances.Record{}})

	_, err := service.EnrollMonitoringInstance(context.Background(), EnrollInput{
		Token:       "missing-token",
		Fingerprint: "fp-new",
	})
	if !errors.Is(err, ErrInvalidEnrollmentToken) {
		t.Fatalf("EnrollMonitoringInstance() error = %v, want ErrInvalidEnrollmentToken", err)
	}
}

func TestEnrollMonitoringInstanceUsesSingleAtomicRepositoryCall(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		enrollmentResultByToken: map[string]monitoringinstances.Record{
			"token-1": {
				MonitoringInstanceID: "mi_atomic",
				BindingStatus:        monitoringinstances.BindingBound,
			},
		},
		atomicSyncTokenByToken: map[string]string{"token-1": "sync-token-atomic"},
	}
	service := NewService(repo)

	_, err := service.EnrollMonitoringInstance(context.Background(), EnrollInput{Token: "token-1", Fingerprint: "fp-atomic"})
	if err != nil {
		t.Fatalf("EnrollMonitoringInstance() error = %v", err)
	}

	if len(repo.enrollmentCalls) != 1 {
		t.Fatalf("enrollmentCalls = %d, want 1", len(repo.enrollmentCalls))
	}
}

func TestRecordHeartbeatRejectsPendingBinding(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 4, 23, 11, 0, 0, 0, time.UTC)
	repo := &fakeRepository{
		heartbeatMonitoringInstance: monitoringinstances.Record{
			MonitoringInstanceID: "mi_789",
			BindingStatus:        monitoringinstances.BindingPendingConfirmation,
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		MonitoringInstanceID: "mi_789",
		SyncToken:            "sync-token-001",
		Heartbeats: []HeartbeatPayload{
			{
				ObservedAt:   observedAt,
				AgentVersion: "v1.0.0",
				Fingerprint:  "fp-1",
				SyncBatchID:  "sync_123",
			},
		},
	})
	if !errors.Is(err, ErrBindingNotAccepted) {
		t.Fatalf("RecordHeartbeatSync() error = %v, want ErrBindingNotAccepted", err)
	}

	if len(repo.heartbeatGetMonitoringInstanceCalls) != 1 {
		t.Fatalf("heartbeatGetMonitoringInstanceCalls = %d, want 1", len(repo.heartbeatGetMonitoringInstanceCalls))
	}
	if len(repo.acceptedHeartbeatBatches) != 0 {
		t.Fatalf("acceptedHeartbeatBatches = %d, want 0", len(repo.acceptedHeartbeatBatches))
	}
}

func TestRecordHeartbeatRejectsUnboundBindingWithoutSideEffects(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		heartbeatMonitoringInstance: monitoringinstances.Record{
			MonitoringInstanceID: "mi_790",
			BindingStatus:        monitoringinstances.BindingUnbound,
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		MonitoringInstanceID: "mi_790",
		SyncToken:            "sync-token-001",
		Heartbeats: []HeartbeatPayload{{
			ObservedAt:   time.Date(2026, 4, 23, 11, 30, 0, 0, time.UTC),
			AgentVersion: "v1.0.0",
			Fingerprint:  "fp-2",
			SyncBatchID:  "sync_456",
		}},
	})
	if !errors.Is(err, ErrBindingNotAccepted) {
		t.Fatalf("RecordHeartbeatSync() error = %v, want ErrBindingNotAccepted", err)
	}

	if len(repo.heartbeatGetMonitoringInstanceCalls) != 1 {
		t.Fatalf("heartbeatGetMonitoringInstanceCalls = %d, want 1", len(repo.heartbeatGetMonitoringInstanceCalls))
	}
	if len(repo.acceptedHeartbeatBatches) != 0 {
		t.Fatalf("acceptedHeartbeatBatches = %d, want 0", len(repo.acceptedHeartbeatBatches))
	}
}

func TestRecordHeartbeatRejectsInvalidSyncToken(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		heartbeatMonitoringInstance: monitoringinstances.Record{
			MonitoringInstanceID: "mi_791",
			BindingStatus:        monitoringinstances.BindingBound,
			BindingFingerprint:   "fp-expected",
			SyncTokenHash:        hashSyncToken("good-token"),
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		MonitoringInstanceID: "mi_791",
		SyncToken:            "bad-token",
		Heartbeats: []HeartbeatPayload{{
			ObservedAt:   time.Date(2026, 4, 23, 11, 45, 0, 0, time.UTC),
			AgentVersion: "v1.0.0",
			Fingerprint:  "fp-expected",
			SyncBatchID:  "sync_457",
		}},
	})
	if !errors.Is(err, ErrInvalidSyncToken) {
		t.Fatalf("RecordHeartbeatSync() error = %v, want ErrInvalidSyncToken", err)
	}
	if len(repo.acceptedHeartbeatBatches) != 0 {
		t.Fatalf("acceptedHeartbeatBatches = %d, want 0", len(repo.acceptedHeartbeatBatches))
	}
}

func TestEnrollmentServiceSourceUsesConstantTimeSyncTokenHashCompare(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("ReadFile(service.go) error = %v", err)
	}

	recordHeartbeatPath := sourceBetween(t, string(source), "func (s *Service) RecordHeartbeatSync", "")
	if !strings.Contains(recordHeartbeatPath, "syncTokenHashesEqual(record.SyncTokenHash, hashSyncToken(input.SyncToken))") {
		t.Fatal("RecordHeartbeatSync() should compare sync token hashes with the shared constant-time helper")
	}
	if strings.Contains(recordHeartbeatPath, "record.SyncTokenHash != hashSyncToken(input.SyncToken)") {
		t.Fatal("RecordHeartbeatSync() should not use plain string inequality for sync token hashes")
	}
}

func TestRecordHeartbeatRejectsMismatchedFingerprint(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		heartbeatMonitoringInstance: monitoringinstances.Record{
			MonitoringInstanceID: "mi_792",
			BindingStatus:        monitoringinstances.BindingBound,
			BindingFingerprint:   "fp-expected",
			SyncTokenHash:        hashSyncToken("good-token"),
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		MonitoringInstanceID: "mi_792",
		SyncToken:            "good-token",
		Heartbeats: []HeartbeatPayload{{
			ObservedAt:   time.Date(2026, 4, 23, 11, 45, 0, 0, time.UTC),
			AgentVersion: "v1.0.0",
			Fingerprint:  "fp-actual",
			SyncBatchID:  "sync_457",
		}},
	})
	if !errors.Is(err, ErrBindingNotAccepted) {
		t.Fatalf("RecordHeartbeatSync() error = %v, want ErrBindingNotAccepted", err)
	}
}

func TestRecordHeartbeatRejectsMismatchedFingerprintWithoutSideEffects(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		heartbeatMonitoringInstance: monitoringinstances.Record{
			MonitoringInstanceID: "mi_793",
			BindingStatus:        monitoringinstances.BindingBound,
			BindingFingerprint:   "fp-expected",
			SyncTokenHash:        hashSyncToken("good-token"),
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		MonitoringInstanceID: "mi_793",
		SyncToken:            "good-token",
		Heartbeats: []HeartbeatPayload{
			{
				ObservedAt:   time.Date(2026, 4, 23, 11, 46, 0, 0, time.UTC),
				AgentVersion: "v1.0.0",
				Fingerprint:  "fp-expected",
				SyncBatchID:  "sync_458",
			},
			{
				ObservedAt:   time.Date(2026, 4, 23, 11, 47, 0, 0, time.UTC),
				AgentVersion: "v1.0.0",
				Fingerprint:  "fp-actual",
				SyncBatchID:  "sync_458",
			},
		},
	})
	if !errors.Is(err, ErrBindingNotAccepted) {
		t.Fatalf("RecordHeartbeatSync() error = %v, want ErrBindingNotAccepted", err)
	}

	if len(repo.heartbeatGetMonitoringInstanceCalls) != 1 {
		t.Fatalf("heartbeatGetMonitoringInstanceCalls = %d, want 1", len(repo.heartbeatGetMonitoringInstanceCalls))
	}
	if len(repo.acceptedHeartbeatBatches) != 0 {
		t.Fatalf("acceptedHeartbeatBatches = %d, want 0", len(repo.acceptedHeartbeatBatches))
	}
}

func TestRecordHeartbeatUsesAtomicAcceptedWritePath(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	secondObservedAt := observedAt.Add(2 * time.Minute)
	repo := &fakeRepository{
		heartbeatMonitoringInstance: monitoringinstances.Record{
			MonitoringInstanceID: "mi_794",
			BindingStatus:        monitoringinstances.BindingBound,
			BindingFingerprint:   "fp-3",
			SyncTokenHash:        hashSyncToken("good-token"),
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		MonitoringInstanceID: "mi_794",
		SyncToken:            "good-token",
		Heartbeats: []HeartbeatPayload{
			{
				ObservedAt:   observedAt,
				AgentVersion: "v1.0.1",
				Fingerprint:  "fp-3",
				SyncBatchID:  "sync_789",
				IsBackfilled: true,
			},
			{
				ObservedAt:   secondObservedAt,
				AgentVersion: "v1.0.2",
				Fingerprint:  "fp-3",
				SyncBatchID:  "sync_789",
			},
		},
	})
	if err != nil {
		t.Fatalf("RecordHeartbeatSync() error = %v", err)
	}

	if len(repo.heartbeatGetMonitoringInstanceCalls) != 1 {
		t.Fatalf("heartbeatGetMonitoringInstanceCalls = %d, want 1", len(repo.heartbeatGetMonitoringInstanceCalls))
	}
	if len(repo.recordAcceptedSyncTokens) != 1 {
		t.Fatalf("recordAcceptedSyncTokens = %d, want 1", len(repo.recordAcceptedSyncTokens))
	}
	if repo.recordAcceptedSyncTokens[0] != "good-token" {
		t.Fatalf("recordAcceptedSyncTokens[0] = %q, want %q", repo.recordAcceptedSyncTokens[0], "good-token")
	}
	if len(repo.acceptedHeartbeatBatches) != 1 {
		t.Fatalf("acceptedHeartbeatBatches = %d, want 1", len(repo.acceptedHeartbeatBatches))
	}
	if len(repo.acceptedHeartbeatBatches[0]) != 2 {
		t.Fatalf("acceptedHeartbeatBatches[0] len = %d, want 2", len(repo.acceptedHeartbeatBatches[0]))
	}
	if repo.acceptedHeartbeatBatches[0][0].SyncBatchID != "sync_789" {
		t.Fatalf("acceptedHeartbeatBatches[0][0].SyncBatchID = %q, want %q", repo.acceptedHeartbeatBatches[0][0].SyncBatchID, "sync_789")
	}
	if !repo.acceptedHeartbeatBatches[0][0].IsBackfilled {
		t.Fatal("acceptedHeartbeatBatches[0][0].IsBackfilled = false, want true")
	}
	if repo.acceptedHeartbeatBatches[0][1].ObservedAt != secondObservedAt {
		t.Fatalf("acceptedHeartbeatBatches[0][1].ObservedAt = %s, want %s", repo.acceptedHeartbeatBatches[0][1].ObservedAt.Format(time.RFC3339), secondObservedAt.Format(time.RFC3339))
	}
}

type fakeRepository struct {
	issuedToken                string
	issuedMonitoringInstanceID string
	issueErr                   error

	issuedSyncToken                 string
	issuedSyncMonitoringInstanceIDs []string
	issueSyncErr                    error
	atomicSyncTokenByToken          map[string]string

	enrollmentResultByToken map[string]monitoringinstances.Record
	enrollmentCalls         []EnrollInput
	enrollErr               error

	heartbeatGetMonitoringInstanceCalls []string
	heartbeatMonitoringInstance         monitoringinstances.Record
	heartbeatMonitoringInstanceErr      error

	recordAcceptedSyncTokens []string
	acceptedHeartbeatBatches [][]HeartbeatWrite
	recordAcceptedErr        error
}

func (f *fakeRepository) IssueEnrollmentToken(_ context.Context, monitoringInstanceID string) (string, error) {
	f.issuedMonitoringInstanceID = monitoringInstanceID
	if f.issueErr != nil {
		return "", f.issueErr
	}
	return f.issuedToken, nil
}

func (f *fakeRepository) IssueSyncToken(_ context.Context, monitoringInstanceID string) (string, error) {
	f.issuedSyncMonitoringInstanceIDs = append(f.issuedSyncMonitoringInstanceIDs, monitoringInstanceID)
	if f.issueSyncErr != nil {
		return "", f.issueSyncErr
	}
	return f.issuedSyncToken, nil
}

func (f *fakeRepository) ApplyEnrollment(_ context.Context, input EnrollInput) (monitoringinstances.Record, string, error) {
	f.enrollmentCalls = append(f.enrollmentCalls, input)
	if f.enrollErr != nil {
		return monitoringinstances.Record{}, "", f.enrollErr
	}
	record, ok := f.enrollmentResultByToken[input.Token]
	if !ok {
		return monitoringinstances.Record{}, "", monitoringinstances.ErrMonitoringInstanceNotFound
	}
	return record, f.atomicSyncTokenByToken[input.Token], nil
}

func (f *fakeRepository) GetMonitoringInstance(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	f.heartbeatGetMonitoringInstanceCalls = append(f.heartbeatGetMonitoringInstanceCalls, monitoringInstanceID)
	if f.heartbeatMonitoringInstanceErr != nil {
		return monitoringinstances.Record{}, f.heartbeatMonitoringInstanceErr
	}
	return f.heartbeatMonitoringInstance, nil
}

func (f *fakeRepository) RecordAcceptedHeartbeats(_ context.Context, syncToken string, writes []HeartbeatWrite) error {
	f.recordAcceptedSyncTokens = append(f.recordAcceptedSyncTokens, syncToken)
	batch := append([]HeartbeatWrite(nil), writes...)
	f.acceptedHeartbeatBatches = append(f.acceptedHeartbeatBatches, batch)
	return f.recordAcceptedErr
}

func sourceBetween(t *testing.T, source, start, end string) string {
	t.Helper()

	startIndex := strings.Index(source, start)
	if startIndex == -1 {
		t.Fatalf("source missing start marker %q", start)
	}
	section := source[startIndex:]
	if end == "" {
		return section
	}
	endIndex := strings.Index(section, end)
	if endIndex == -1 {
		t.Fatalf("source missing end marker %q", end)
	}
	return section[:endIndex]
}

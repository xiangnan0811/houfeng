package enrollment

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/nodes"
)

func TestIssueEnrollmentTokenReturnsToken(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		issuedToken: "enroll_token",
	}
	service := NewService(repo)

	token, err := service.IssueNodeEnrollmentToken(context.Background(), "nd_123")
	if err != nil {
		t.Fatalf("IssueNodeEnrollmentToken() error = %v", err)
	}

	if token != "enroll_token" {
		t.Fatalf("token = %q, want %q", token, "enroll_token")
	}
	if repo.issuedNodeID != "nd_123" {
		t.Fatalf("issuedNodeID = %q, want %q", repo.issuedNodeID, "nd_123")
	}
}

func TestEnrollNodeBindsUnboundNodeAndIssuesSyncToken(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		enrollmentResultByToken: map[string]nodes.Record{
			"plain-token": {
				NodeID:             "nd_123",
				BindingStatus:      nodes.BindingBound,
				BindingFingerprint: "fp-new",
			},
		},
		issuedSyncToken: "sync-token-001",
	}
	service := NewService(repo)

	result, err := service.EnrollNode(context.Background(), EnrollInput{
		Token:       "plain-token",
		Fingerprint: "fp-new",
	})
	if err != nil {
		t.Fatalf("EnrollNode() error = %v", err)
	}

	if len(repo.enrollmentCalls) != 1 {
		t.Fatalf("enrollmentCalls = %d, want 1", len(repo.enrollmentCalls))
	}
	if repo.enrollmentCalls[0] != (EnrollInput{Token: "plain-token", Fingerprint: "fp-new"}) {
		t.Fatalf("enrollmentCalls[0] = %#v", repo.enrollmentCalls[0])
	}
	if repo.issuedSyncNodeIDs != nil && len(repo.issuedSyncNodeIDs) != 1 {
		t.Fatalf("issuedSyncNodeIDs len = %d, want 1", len(repo.issuedSyncNodeIDs))
	}
	if repo.issuedSyncNodeIDs[0] != "nd_123" {
		t.Fatalf("issuedSyncNodeIDs[0] = %q, want %q", repo.issuedSyncNodeIDs[0], "nd_123")
	}

	if result != (EnrollResult{
		NodeID:        "nd_123",
		BindingStatus: nodes.BindingBound,
		SyncToken:     "sync-token-001",
	}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestEnrollNodeMarksConflictForNewFingerprintWithoutIssuingSyncToken(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		enrollmentResultByToken: map[string]nodes.Record{
			"plain-token": {
				NodeID:             "nd_456",
				BindingStatus:      nodes.BindingPendingConfirmation,
				BindingFingerprint: "fp-old",
			},
		},
	}
	service := NewService(repo)

	result, err := service.EnrollNode(context.Background(), EnrollInput{
		Token:       "plain-token",
		Fingerprint: "fp-new",
	})
	if err != nil {
		t.Fatalf("EnrollNode() error = %v", err)
	}

	if len(repo.enrollmentCalls) != 1 {
		t.Fatalf("enrollmentCalls = %d, want 1", len(repo.enrollmentCalls))
	}
	if repo.enrollmentCalls[0] != (EnrollInput{Token: "plain-token", Fingerprint: "fp-new"}) {
		t.Fatalf("enrollmentCalls[0] = %#v", repo.enrollmentCalls[0])
	}
	if len(repo.issuedSyncNodeIDs) != 0 {
		t.Fatalf("issuedSyncNodeIDs len = %d, want 0", len(repo.issuedSyncNodeIDs))
	}

	if result != (EnrollResult{
		NodeID:        "nd_456",
		BindingStatus: nodes.BindingPendingConfirmation,
	}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestEnrollNodeReturnsInvalidEnrollmentToken(t *testing.T) {
	t.Parallel()

	service := NewService(&fakeRepository{enrollmentResultByToken: map[string]nodes.Record{}})

	_, err := service.EnrollNode(context.Background(), EnrollInput{
		Token:       "missing-token",
		Fingerprint: "fp-new",
	})
	if !errors.Is(err, ErrInvalidEnrollmentToken) {
		t.Fatalf("EnrollNode() error = %v, want ErrInvalidEnrollmentToken", err)
	}
}

func TestEnrollNodeUsesSingleAtomicRepositoryCall(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		enrollmentResultByToken: map[string]nodes.Record{
			"token-1": {
				NodeID:        "nd_atomic",
				BindingStatus: nodes.BindingBound,
			},
		},
		issuedSyncToken: "sync-token-atomic",
	}
	service := NewService(repo)

	_, err := service.EnrollNode(context.Background(), EnrollInput{Token: "token-1", Fingerprint: "fp-atomic"})
	if err != nil {
		t.Fatalf("EnrollNode() error = %v", err)
	}

	if len(repo.enrollmentCalls) != 1 {
		t.Fatalf("enrollmentCalls = %d, want 1", len(repo.enrollmentCalls))
	}
}

func TestRecordHeartbeatRejectsPendingBinding(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 4, 23, 11, 0, 0, 0, time.UTC)
	repo := &fakeRepository{
		heartbeatNode: nodes.Record{
			NodeID:        "nd_789",
			BindingStatus: nodes.BindingPendingConfirmation,
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		NodeID:    "nd_789",
		SyncToken: "sync-token-001",
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

	if len(repo.heartbeatGetNodeCalls) != 1 {
		t.Fatalf("heartbeatGetNodeCalls = %d, want 1", len(repo.heartbeatGetNodeCalls))
	}
	if len(repo.acceptedHeartbeatBatches) != 0 {
		t.Fatalf("acceptedHeartbeatBatches = %d, want 0", len(repo.acceptedHeartbeatBatches))
	}
}

func TestRecordHeartbeatRejectsUnboundBindingWithoutSideEffects(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		heartbeatNode: nodes.Record{
			NodeID:        "nd_790",
			BindingStatus: nodes.BindingUnbound,
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		NodeID:    "nd_790",
		SyncToken: "sync-token-001",
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

	if len(repo.heartbeatGetNodeCalls) != 1 {
		t.Fatalf("heartbeatGetNodeCalls = %d, want 1", len(repo.heartbeatGetNodeCalls))
	}
	if len(repo.acceptedHeartbeatBatches) != 0 {
		t.Fatalf("acceptedHeartbeatBatches = %d, want 0", len(repo.acceptedHeartbeatBatches))
	}
}

func TestRecordHeartbeatRejectsInvalidSyncToken(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		heartbeatNode: nodes.Record{
			NodeID:             "nd_791",
			BindingStatus:      nodes.BindingBound,
			BindingFingerprint: "fp-expected",
			SyncTokenHash:      hashSyncToken("good-token"),
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		NodeID:    "nd_791",
		SyncToken: "bad-token",
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

func TestRecordHeartbeatRejectsMismatchedFingerprint(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		heartbeatNode: nodes.Record{
			NodeID:             "nd_792",
			BindingStatus:      nodes.BindingBound,
			BindingFingerprint: "fp-expected",
			SyncTokenHash:      hashSyncToken("good-token"),
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		NodeID:    "nd_792",
		SyncToken: "good-token",
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
		heartbeatNode: nodes.Record{
			NodeID:             "nd_793",
			BindingStatus:      nodes.BindingBound,
			BindingFingerprint: "fp-expected",
			SyncTokenHash:      hashSyncToken("good-token"),
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		NodeID:    "nd_793",
		SyncToken: "good-token",
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

	if len(repo.heartbeatGetNodeCalls) != 1 {
		t.Fatalf("heartbeatGetNodeCalls = %d, want 1", len(repo.heartbeatGetNodeCalls))
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
		heartbeatNode: nodes.Record{
			NodeID:             "nd_794",
			BindingStatus:      nodes.BindingBound,
			BindingFingerprint: "fp-3",
			SyncTokenHash:      hashSyncToken("good-token"),
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		NodeID:    "nd_794",
		SyncToken: "good-token",
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

	if len(repo.heartbeatGetNodeCalls) != 1 {
		t.Fatalf("heartbeatGetNodeCalls = %d, want 1", len(repo.heartbeatGetNodeCalls))
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
	issuedToken  string
	issuedNodeID string
	issueErr     error

	issuedSyncToken   string
	issuedSyncNodeIDs []string
	issueSyncErr      error

	enrollmentResultByToken map[string]nodes.Record
	enrollmentCalls         []EnrollInput
	enrollErr               error

	heartbeatGetNodeCalls []string
	heartbeatNode         nodes.Record
	heartbeatNodeErr      error

	recordAcceptedSyncTokens []string
	acceptedHeartbeatBatches [][]HeartbeatWrite
	recordAcceptedErr        error
}

func (f *fakeRepository) IssueEnrollmentToken(_ context.Context, nodeID string) (string, error) {
	f.issuedNodeID = nodeID
	if f.issueErr != nil {
		return "", f.issueErr
	}
	return f.issuedToken, nil
}

func (f *fakeRepository) IssueSyncToken(_ context.Context, nodeID string) (string, error) {
	f.issuedSyncNodeIDs = append(f.issuedSyncNodeIDs, nodeID)
	if f.issueSyncErr != nil {
		return "", f.issueSyncErr
	}
	return f.issuedSyncToken, nil
}

func (f *fakeRepository) ApplyEnrollment(_ context.Context, input EnrollInput) (nodes.Record, error) {
	f.enrollmentCalls = append(f.enrollmentCalls, input)
	if f.enrollErr != nil {
		return nodes.Record{}, f.enrollErr
	}
	record, ok := f.enrollmentResultByToken[input.Token]
	if !ok {
		return nodes.Record{}, nodes.ErrNodeNotFound
	}
	return record, nil
}

func (f *fakeRepository) GetNode(_ context.Context, nodeID string) (nodes.Record, error) {
	f.heartbeatGetNodeCalls = append(f.heartbeatGetNodeCalls, nodeID)
	if f.heartbeatNodeErr != nil {
		return nodes.Record{}, f.heartbeatNodeErr
	}
	return f.heartbeatNode, nil
}

func (f *fakeRepository) RecordAcceptedHeartbeats(_ context.Context, syncToken string, writes []HeartbeatWrite) error {
	f.recordAcceptedSyncTokens = append(f.recordAcceptedSyncTokens, syncToken)
	batch := append([]HeartbeatWrite(nil), writes...)
	f.acceptedHeartbeatBatches = append(f.acceptedHeartbeatBatches, batch)
	return f.recordAcceptedErr
}

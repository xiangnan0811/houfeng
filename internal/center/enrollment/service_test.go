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

func TestEnrollNodeBindsUnboundNode(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		nodeByToken: map[string]nodes.Record{
			"plain-token": {
				NodeID:        "nd_123",
				BindingStatus: nodes.BindingUnbound,
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

	if len(repo.bindingUpdates) != 1 {
		t.Fatalf("bindingUpdates = %d, want 1", len(repo.bindingUpdates))
	}

	update := repo.bindingUpdates[0]
	if update.NodeID != "nd_123" {
		t.Fatalf("update.NodeID = %q, want %q", update.NodeID, "nd_123")
	}
	if update.BindingStatus != nodes.BindingBound {
		t.Fatalf("update.BindingStatus = %q, want %q", update.BindingStatus, nodes.BindingBound)
	}
	if update.BindingFingerprint != "fp-new" {
		t.Fatalf("update.BindingFingerprint = %q, want %q", update.BindingFingerprint, "fp-new")
	}

	if result != (EnrollResult{
		NodeID:        "nd_123",
		BindingStatus: nodes.BindingBound,
	}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestEnrollNodeMarksConflictForNewFingerprint(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		nodeByToken: map[string]nodes.Record{
			"plain-token": {
				NodeID:             "nd_456",
				BindingStatus:      nodes.BindingBound,
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

	if len(repo.bindingUpdates) != 1 {
		t.Fatalf("bindingUpdates = %d, want 1", len(repo.bindingUpdates))
	}

	update := repo.bindingUpdates[0]
	if update.BindingStatus != nodes.BindingPendingConfirmation {
		t.Fatalf("update.BindingStatus = %q, want %q", update.BindingStatus, nodes.BindingPendingConfirmation)
	}
	if update.BindingFingerprint != "fp-old" {
		t.Fatalf("update.BindingFingerprint = %q, want %q", update.BindingFingerprint, "fp-old")
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

	service := NewService(&fakeRepository{nodeByToken: map[string]nodes.Record{}})

	_, err := service.EnrollNode(context.Background(), EnrollInput{
		Token:       "missing-token",
		Fingerprint: "fp-new",
	})
	if !errors.Is(err, ErrInvalidEnrollmentToken) {
		t.Fatalf("EnrollNode() error = %v, want ErrInvalidEnrollmentToken", err)
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
		NodeID: "nd_789",
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
		NodeID: "nd_790",
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

func TestRecordHeartbeatRejectsMismatchedFingerprint(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		heartbeatNode: nodes.Record{
			NodeID:             "nd_792",
			BindingStatus:      nodes.BindingBound,
			BindingFingerprint: "fp-expected",
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		NodeID: "nd_792",
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
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		NodeID: "nd_793",
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
			NodeID:             "nd_791",
			BindingStatus:      nodes.BindingBound,
			BindingFingerprint: "fp-3",
		},
	}
	service := NewService(repo)

	err := service.RecordHeartbeatSync(context.Background(), SyncInput{
		NodeID: "nd_791",
		Heartbeats: []HeartbeatPayload{
			{
				ObservedAt:   observedAt,
				AgentVersion: "v1.0.1",
				Fingerprint:  "fp-3",
				SyncBatchID:  "sync_789",
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
	if len(repo.acceptedHeartbeatBatches) != 1 {
		t.Fatalf("acceptedHeartbeatBatches = %d, want 1", len(repo.acceptedHeartbeatBatches))
	}
	if len(repo.acceptedHeartbeatBatches[0]) != 2 {
		t.Fatalf("acceptedHeartbeatBatches[0] len = %d, want 2", len(repo.acceptedHeartbeatBatches[0]))
	}
	if repo.acceptedHeartbeatBatches[0][0].SyncBatchID != "sync_789" {
		t.Fatalf("acceptedHeartbeatBatches[0][0].SyncBatchID = %q, want %q", repo.acceptedHeartbeatBatches[0][0].SyncBatchID, "sync_789")
	}
	if repo.acceptedHeartbeatBatches[0][1].ObservedAt != secondObservedAt {
		t.Fatalf("acceptedHeartbeatBatches[0][1].ObservedAt = %s, want %s", repo.acceptedHeartbeatBatches[0][1].ObservedAt.Format(time.RFC3339), secondObservedAt.Format(time.RFC3339))
	}
}

type fakeRepository struct {
	issuedToken  string
	issuedNodeID string
	issueErr     error

	nodeByToken map[string]nodes.Record
	findErr     error

	bindingUpdates []BindingUpdate
	updateResult   nodes.Record
	updateErr      error

	heartbeatGetNodeCalls []string
	heartbeatNode         nodes.Record
	heartbeatNodeErr      error

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

func (f *fakeRepository) FindNodeByEnrollmentToken(_ context.Context, token string) (nodes.Record, error) {
	if f.findErr != nil {
		return nodes.Record{}, f.findErr
	}
	record, ok := f.nodeByToken[token]
	if !ok {
		return nodes.Record{}, nodes.ErrNodeNotFound
	}
	return record, nil
}

func (f *fakeRepository) UpdateBindingState(_ context.Context, update BindingUpdate) (nodes.Record, error) {
	f.bindingUpdates = append(f.bindingUpdates, update)
	if f.updateErr != nil {
		return nodes.Record{}, f.updateErr
	}
	if f.updateResult.NodeID != "" {
		return f.updateResult, nil
	}

	record := nodes.Record{
		NodeID:             update.NodeID,
		BindingStatus:      update.BindingStatus,
		BindingFingerprint: update.BindingFingerprint,
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

func (f *fakeRepository) RecordAcceptedHeartbeats(_ context.Context, writes []HeartbeatWrite) error {
	batch := append([]HeartbeatWrite(nil), writes...)
	f.acceptedHeartbeatBatches = append(f.acceptedHeartbeatBatches, batch)
	return f.recordAcceptedErr
}

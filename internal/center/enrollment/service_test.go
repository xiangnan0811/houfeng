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
		Token:        "plain-token",
		Fingerprint:  "fp-new",
		AgentVersion: "v1.0.0",
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
		Status:        "ok",
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
		Token:        "plain-token",
		Fingerprint:  "fp-new",
		AgentVersion: "v1.0.0",
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
		Status:        "ok",
	}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestRecordHeartbeatRejectsPendingBinding(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 4, 23, 11, 0, 0, 0, time.UTC)
	repo := &fakeRepository{
		touchedNode: nodes.Record{
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

	if len(repo.heartbeatTouches) != 1 {
		t.Fatalf("heartbeatTouches = %d, want 1", len(repo.heartbeatTouches))
	}
	if repo.heartbeatTouches[0].SyncBatchID != "sync_123" {
		t.Fatalf("heartbeatTouches[0].SyncBatchID = %q, want %q", repo.heartbeatTouches[0].SyncBatchID, "sync_123")
	}
	if len(repo.heartbeatWrites) != 0 {
		t.Fatalf("heartbeatWrites = %d, want 0", len(repo.heartbeatWrites))
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

	heartbeatWrites []HeartbeatWrite
	recordErr       error

	heartbeatTouches []HeartbeatWrite
	touchedNode      nodes.Record
	touchErr         error
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

func (f *fakeRepository) RecordHeartbeat(_ context.Context, write HeartbeatWrite) error {
	f.heartbeatWrites = append(f.heartbeatWrites, write)
	return f.recordErr
}

func (f *fakeRepository) TouchHeartbeatState(_ context.Context, write HeartbeatWrite) (nodes.Record, error) {
	f.heartbeatTouches = append(f.heartbeatTouches, write)
	if f.touchErr != nil {
		return nodes.Record{}, f.touchErr
	}
	return f.touchedNode, nil
}

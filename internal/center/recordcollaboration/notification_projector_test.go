package recordcollaboration

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordplatform"
)

func TestNotificationProjectionWorkerRunsBoundedClaimWithStableOwner(t *testing.T) {
	queue := &notificationQueueStub{}
	projector, err := NewNotificationProjector(queue, &notificationProjectionStub{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewNotificationProjectionWorker(projector, NotificationProjectionWorkerOptions{
		OwnerID: "record_notifications_projector", OwnerLeaseDuration: time.Minute,
		PollInterval: time.Second, Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewNotificationProjectionWorker() error = %v", err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || processed {
		t.Fatalf("RunOnce() = (%v, %v), want false, nil", processed, err)
	}
	if queue.claimInput.OwnerID != "record_notifications_projector" || queue.claimInput.OwnerLeaseDuration != time.Minute {
		t.Fatalf("claim input = %#v", queue.claimInput)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(cancelled); err != nil {
		t.Fatalf("Run(cancelled) error = %v", err)
	}
}

func TestNotificationProjectionWorkerRejectsClosedConfiguration(t *testing.T) {
	projector, _ := NewNotificationProjector(&notificationQueueStub{}, &notificationProjectionStub{}, time.Second)
	for _, options := range []NotificationProjectionWorkerOptions{
		{OwnerID: "record_notifications_projector", OwnerLeaseDuration: time.Minute},
		{OwnerID: "", OwnerLeaseDuration: time.Minute, PollInterval: time.Second},
		{OwnerID: "record_notifications_projector", OwnerLeaseDuration: time.Nanosecond, PollInterval: time.Second},
	} {
		if worker, err := NewNotificationProjectionWorker(projector, options); worker != nil || !errors.Is(err, ErrInvalidNotificationProjector) {
			t.Fatalf("NewNotificationProjectionWorker(%#v) = (%#v, %v)", options, worker, err)
		}
	}
	if worker, err := NewNotificationProjectionWorker(nil, NotificationProjectionWorkerOptions{
		OwnerID: "record_notifications_projector", OwnerLeaseDuration: time.Minute, PollInterval: time.Second,
	}); worker != nil || !errors.Is(err, ErrInvalidNotificationProjector) {
		t.Fatalf("NewNotificationProjectionWorker(nil) = (%#v, %v)", worker, err)
	}
}

func TestNotificationProjectorReusesClaimProjectionAndOwnerFencedFinalize(t *testing.T) {
	claim := testNotificationClaim(recordplatform.OutboxEventKindRecordActionAssigned, 7)
	queue := &notificationQueueStub{claim: &claim}
	projection := &notificationProjectionStub{result: NotificationProjectionResult{NotificationID: "rnt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RecipientCount: 2}}
	projector, err := NewNotificationProjector(queue, projection, time.Second)
	if err != nil {
		t.Fatalf("NewNotificationProjector() error = %v", err)
	}

	processed, err := projector.ProjectNext(context.Background(), recordplatform.OutboxClaimInputV1{OwnerID: "notification_worker", OwnerLeaseDuration: time.Minute})
	if err != nil || !processed {
		t.Fatalf("ProjectNext() = (%v, %v)", processed, err)
	}
	if !reflect.DeepEqual(queue.steps, []string{"claim", "sent"}) || projection.calls != 1 || projection.event != claim.Event {
		t.Fatalf("steps=%#v projection=%#v", queue.steps, projection)
	}
}

func TestNotificationProjectorCancelsUnsupportedAndMissingExactSources(t *testing.T) {
	tests := []struct {
		name       string
		claim      recordplatform.ClaimedOutboxEventV1
		projectErr error
		wantCalls  int
	}{
		{name: "unsupported generic event", claim: testNotificationClaim(recordplatform.OutboxEventKindRecordActionUpdated, 0)},
		{name: "missing exact source", claim: testNotificationClaim(recordplatform.OutboxEventKindRecordCommentMentioned, 4), projectErr: ErrNotificationSourceMissing, wantCalls: 1},
		{name: "stale authorization or fence source", claim: testNotificationClaim(recordplatform.OutboxEventKindRecordActionAssigned, 4), projectErr: ErrNotificationSourceStale, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue := &notificationQueueStub{claim: &tt.claim}
			projection := &notificationProjectionStub{err: tt.projectErr}
			projector, err := NewNotificationProjector(queue, projection, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			processed, err := projector.ProjectNext(context.Background(), recordplatform.OutboxClaimInputV1{OwnerID: "notification_worker", OwnerLeaseDuration: time.Minute})
			if err != nil || !processed || !reflect.DeepEqual(queue.steps, []string{"claim", "cancel"}) || projection.calls != tt.wantCalls {
				t.Fatalf("ProjectNext()=(%v,%v) steps=%#v calls=%d", processed, err, queue.steps, projection.calls)
			}
		})
	}
}

func TestNotificationProjectorRetriesTransactionalFailureAndLeavesFinalizeCrashForTakeover(t *testing.T) {
	claim := testNotificationClaim(recordplatform.OutboxEventKindRecordOwnerChanged, 3)
	t.Run("projection failure", func(t *testing.T) {
		dependencyErr := errors.New("authorization resolver unavailable")
		queue := &notificationQueueStub{claim: &claim}
		projector, _ := NewNotificationProjector(queue, &notificationProjectionStub{err: dependencyErr}, time.Second)
		processed, err := projector.ProjectNext(context.Background(), recordplatform.OutboxClaimInputV1{OwnerID: "notification_worker", OwnerLeaseDuration: time.Minute})
		if !processed || !errors.Is(err, dependencyErr) || !reflect.DeepEqual(queue.steps, []string{"claim", "retry"}) {
			t.Fatalf("ProjectNext()=(%v,%v) steps=%#v", processed, err, queue.steps)
		}
	})
	t.Run("finalize crash", func(t *testing.T) {
		crashErr := errors.New("sent commit unavailable")
		queue := &notificationQueueStub{claim: &claim, sentErr: crashErr}
		projector, _ := NewNotificationProjector(queue, &notificationProjectionStub{result: NotificationProjectionResult{}}, time.Second)
		processed, err := projector.ProjectNext(context.Background(), recordplatform.OutboxClaimInputV1{OwnerID: "notification_worker", OwnerLeaseDuration: time.Minute})
		if !processed || !errors.Is(err, crashErr) || !reflect.DeepEqual(queue.steps, []string{"claim", "sent"}) {
			t.Fatalf("ProjectNext()=(%v,%v) steps=%#v", processed, err, queue.steps)
		}
	})
}

type notificationQueueStub struct {
	claim      *recordplatform.ClaimedOutboxEventV1
	claimInput recordplatform.OutboxClaimInputV1
	steps      []string
	sentErr    error
}

func (queue *notificationQueueStub) ClaimOutbox(_ context.Context, input recordplatform.OutboxClaimInputV1) (*recordplatform.ClaimedOutboxEventV1, error) {
	queue.claimInput = input
	queue.steps = append(queue.steps, "claim")
	return queue.claim, nil
}
func (queue *notificationQueueStub) CancelOutbox(context.Context, recordplatform.ClaimedOutboxEventV1) error {
	queue.steps = append(queue.steps, "cancel")
	return nil
}
func (queue *notificationQueueStub) RetryOutbox(context.Context, recordplatform.ClaimedOutboxEventV1, time.Duration) error {
	queue.steps = append(queue.steps, "retry")
	return nil
}
func (queue *notificationQueueStub) MarkOutboxSent(context.Context, recordplatform.ClaimedOutboxEventV1) error {
	queue.steps = append(queue.steps, "sent")
	return queue.sentErr
}

type notificationProjectionStub struct {
	event  recordplatform.OutboxEvent
	result NotificationProjectionResult
	err    error
	calls  int
}

func (projection *notificationProjectionStub) ProjectNotification(_ context.Context, claim recordplatform.ClaimedOutboxEventV1) (NotificationProjectionResult, error) {
	projection.calls++
	projection.event = claim.Event
	return projection.result, projection.err
}

func testNotificationClaim(kind string, sourceVersion uint64) recordplatform.ClaimedOutboxEventV1 {
	subjectKind := recordplatform.OutboxSubjectKindRecord
	subjectID := "rec_projector"
	switch kind {
	case recordplatform.OutboxEventKindRecordActionAssigned, recordplatform.OutboxEventKindRecordActionUpdated:
		subjectKind, subjectID = recordplatform.OutboxSubjectKindAction, "ract_projector"
	case recordplatform.OutboxEventKindRecordCommentMentioned:
		subjectKind, subjectID = recordplatform.OutboxSubjectKindComment, "rcm_projector"
	}
	ownerExpiry := time.Date(2026, 8, 17, 11, 0, 0, 0, time.UTC)
	return recordplatform.ClaimedOutboxEventV1{
		Event:     recordplatform.OutboxEvent{RowID: 41, ProjectID: "default", EventKind: kind, SubjectKind: subjectKind, SubjectID: subjectID, SourceVersion: sourceVersion, AuthorizationEpoch: 3},
		Owner:     recordplatform.OwnerLease{OwnerID: "notification_worker", Generation: 1, ExpiresAt: ownerExpiry},
		ExpiresAt: ownerExpiry.Add(time.Hour),
	}
}

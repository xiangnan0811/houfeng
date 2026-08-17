package recordcollaboration

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestCommentApplicationOwnsPlatformTimingAndForwardsClosedRequests(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	current.RecordID = "rec_commentparent1"
	store := &commentCommandStoreStub{result: CommentMutationResult{
		CommentID: "rcm_result1", RecordID: current.RecordID, Version: 1,
		State: CommentStateActive, EventKind: CommentMutationCreate, ChangedAt: time.Now().UTC(),
	}}
	service, err := NewCommentService(&commentCurrentSourceStub{result: current}, store)
	if err != nil {
		t.Fatal(err)
	}
	application, err := NewCommentApplication(service, CommentApplicationOptions{
		IdempotencyOwnerID: "record_comments_api", OwnerLeaseDuration: time.Minute,
		IdempotencyTTL: 24 * time.Hour, OutboxTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewCommentApplication() error = %v", err)
	}
	if _, err := application.CreateComment(context.Background(), CommentCreateApplicationRequest{
		Actor: actor, RecordID: current.RecordID, BodyMarkdown: "Safe comment.",
		MentionUserIDs: []string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb"}, IdempotencyKey: "comment-app-create",
	}); err != nil {
		t.Fatalf("CreateComment() error = %v", err)
	}
	if store.command.Idempotency.OwnerID != "record_comments_api" ||
		store.command.Idempotency.OwnerLeaseDuration != time.Minute || store.command.Idempotency.RecordTTL != 24*time.Hour ||
		store.command.OutboxTTL != 24*time.Hour || store.command.Kind != CommentMutationCreate {
		t.Fatalf("application command = %#v", store.command)
	}
}

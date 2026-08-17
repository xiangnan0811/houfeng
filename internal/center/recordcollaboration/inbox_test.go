package recordcollaboration

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestInboxItemIsClosedIdentityOnlyAndEnforcesReadDismissState(t *testing.T) {
	created := time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC)
	read := created.Add(time.Minute)
	dismissed := read.Add(time.Minute)
	valid := InboxItem{
		NotificationID: "rnt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RecordID:       "rec_inbox", EventKind: NotificationEventActionAssigned,
		SubjectKind: NotificationSubjectAction, SubjectID: "ract_inbox", SourceVersion: 3,
		Reason: NotificationReasonAssignee, Mandatory: true, EventAt: created,
		ReadAt: &read, DismissedAt: &dismissed,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("InboxItem.Validate() error = %v", err)
	}
	for _, field := range []string{"Body", "Markdown", "HTML", "RenderModel", "Evidence", "Outbox", "Payload", "Title"} {
		if _, ok := reflect.TypeFor[InboxItem]().FieldByName(field); ok {
			t.Fatalf("InboxItem exposes forbidden field %q", field)
		}
	}
	invalid := valid
	invalid.ReadAt = nil
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidInboxRequest) {
		t.Fatalf("dismissed without read error = %v, want ErrInvalidInboxRequest", err)
	}
}

func TestInboxTransitionKindsAreClosedAndDeepLinkContainsOnlyStableIdentity(t *testing.T) {
	for _, kind := range []InboxTransitionKind{InboxTransitionUnread, InboxTransitionRead, InboxTransitionDismiss} {
		if err := ValidateInboxTransitionKind(kind); err != nil {
			t.Errorf("ValidateInboxTransitionKind(%q) error = %v", kind, err)
		}
	}
	if err := ValidateInboxTransitionKind("archive"); !errors.Is(err, ErrInvalidInboxRequest) {
		t.Fatalf("unknown transition error = %v", err)
	}
	target := InboxDeepLinkTarget{RecordID: "rec_inbox", SubjectKind: NotificationSubjectComment, SubjectID: "rcm_inbox"}
	if err := target.Validate(); err != nil {
		t.Fatalf("InboxDeepLinkTarget.Validate() error = %v", err)
	}
	for _, field := range []string{"URL", "Label", "Title", "Body", "Content"} {
		if _, ok := reflect.TypeFor[InboxDeepLinkTarget]().FieldByName(field); ok {
			t.Fatalf("InboxDeepLinkTarget exposes presentation field %q", field)
		}
	}
}

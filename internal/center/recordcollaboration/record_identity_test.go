package recordcollaboration

import (
	"errors"
	"testing"

	"houfeng/internal/center/recordplatform"
)

func TestCollaborationRecordIdentityDomains(t *testing.T) {
	for _, recordID := range []string{
		"rec_legacy",
		"rec2_70000000000000000000000000000000",
		"rec2_80000000000000000000000000000000",
	} {
		t.Run(recordID, func(t *testing.T) {
			binding, err := NewRecordFenceBinding(recordplatform.ProjectIDDefault, recordID, 3)
			if err != nil || binding.RecordID() != recordID {
				t.Fatalf("NewRecordFenceBinding() = %#v, %v", binding, err)
			}
			target := InboxDeepLinkTarget{RecordID: recordID, SubjectKind: NotificationSubjectRecord, SubjectID: recordID}
			if err := target.Validate(); err != nil {
				t.Fatalf("InboxDeepLinkTarget.Validate() error = %v", err)
			}
			if err := ValidateActionID(recordID); !errors.Is(err, ErrInvalidActionID) {
				t.Fatalf("record as action error = %v", err)
			}
		})
	}
	invalid := InboxDeepLinkTarget{RecordID: "rec2_7000000000000000000000000000000g", SubjectKind: NotificationSubjectRecord, SubjectID: "rec_legacy"}
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidInboxRequest) {
		t.Fatalf("invalid record target error = %v", err)
	}
}

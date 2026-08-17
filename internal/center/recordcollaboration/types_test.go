package recordcollaboration

import (
	"errors"
	"math"
	"strings"
	"testing"

	"houfeng/internal/center/recordplatform"
)

func TestRecordCollaborationIdentityValidators(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    string
		validate func(string) error
		wantErr  error
	}{
		{name: "action", value: "ract_0123456789abcdef", validate: ValidateActionID},
		{name: "action event", value: "raev_0123456789abcdef", validate: ValidateActionEventID},
		{name: "comment", value: "rcm_0123456789abcdef", validate: ValidateCommentID},
		{name: "comment revision", value: "rcr_0123456789abcdef", validate: ValidateCommentRevisionID},
		{name: "comment tombstone", value: "rct_0123456789abcdef", validate: ValidateCommentTombstoneID},
		{name: "notification", value: "rnt_" + strings.Repeat("a", 64), validate: ValidateNotificationID},
		{name: "notification delivery", value: "rnd_0123456789abcdef", validate: ValidateNotificationDeliveryID},
		{name: "delivery attempt", value: "rna_0123456789abcdef", validate: ValidateNotificationDeliveryAttemptID},
		{name: "wrong action prefix", value: "rcm_abc", validate: ValidateActionID, wantErr: ErrInvalidActionID},
		{name: "uppercase comment", value: "rcm_ABC", validate: ValidateCommentID, wantErr: ErrInvalidCommentID},
		{name: "empty notification", validate: ValidateNotificationID, wantErr: ErrInvalidNotificationID},
		{name: "short notification", value: "rnt_0123456789abcdef", validate: ValidateNotificationID, wantErr: ErrInvalidNotificationID},
		{name: "non hex notification", value: "rnt_" + strings.Repeat("g", 64), validate: ValidateNotificationID, wantErr: ErrInvalidNotificationID},
		{name: "long delivery", value: "rnd_" + strings.Repeat("a", 65), validate: ValidateNotificationDeliveryID, wantErr: ErrInvalidNotificationDeliveryID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.validate(tt.value)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("validate(%q) error = %v", tt.value, err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("validate(%q) error = %v, want %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestRecordFenceBindingIsValidatedAndImmutable(t *testing.T) {
	t.Parallel()
	binding, err := NewRecordFenceBinding(recordplatform.ProjectIDDefault, "rec_0123456789abcdef", 0)
	if err != nil {
		t.Fatalf("NewRecordFenceBinding() error = %v", err)
	}
	if binding.ProjectID() != recordplatform.ProjectIDDefault ||
		binding.RecordID() != "rec_0123456789abcdef" || binding.Epoch() != 0 {
		t.Fatalf("record fence binding = %#v", binding)
	}
	if err := binding.Validate(); err != nil {
		t.Fatalf("binding.Validate() error = %v", err)
	}
	if err := (RecordFenceBinding{}).Validate(); !errors.Is(err, ErrInvalidRecordFenceBinding) {
		t.Fatalf("zero RecordFenceBinding.Validate() error = %v, want ErrInvalidRecordFenceBinding", err)
	}
	if _, err := NewRecordFenceBinding(recordplatform.ProjectID("other"), "rec_0123456789abcdef", 1); !errors.Is(err, ErrInvalidRecordFenceBinding) {
		t.Fatalf("cross-project binding error = %v, want ErrInvalidRecordFenceBinding", err)
	}
	if _, err := NewRecordFenceBinding(recordplatform.ProjectIDDefault, "bad", 1); !errors.Is(err, ErrInvalidRecordFenceBinding) {
		t.Fatalf("malformed record binding error = %v, want ErrInvalidRecordFenceBinding", err)
	}
}

func TestRecordFenceBindingEpochFitsPostgresBigint(t *testing.T) {
	t.Parallel()
	if _, err := NewRecordFenceBinding(
		recordplatform.ProjectIDDefault,
		"rec_0123456789abcdef",
		recordplatform.ContentEpoch(math.MaxInt64),
	); err != nil {
		t.Fatalf("NewRecordFenceBinding(MaxInt64) error = %v", err)
	}
	if _, err := NewRecordFenceBinding(
		recordplatform.ProjectIDDefault,
		"rec_0123456789abcdef",
		recordplatform.ContentEpoch(uint64(math.MaxInt64)+1),
	); !errors.Is(err, ErrInvalidRecordFenceBinding) {
		t.Fatalf("NewRecordFenceBinding(MaxInt64+1) error = %v, want ErrInvalidRecordFenceBinding", err)
	}
}

func TestActionStateMachine(t *testing.T) {
	t.Parallel()
	valid := [][2]ActionStatus{
		{ActionStatusOpen, ActionStatusCompleted},
		{ActionStatusOpen, ActionStatusCancelled},
		{ActionStatusCompleted, ActionStatusOpen},
		{ActionStatusCancelled, ActionStatusOpen},
	}
	for _, transition := range valid {
		if err := ValidateActionStatusTransition(transition[0], transition[1]); err != nil {
			t.Errorf("ValidateActionStatusTransition(%q, %q) error = %v", transition[0], transition[1], err)
		}
	}
	invalid := [][2]ActionStatus{
		{ActionStatusOpen, ActionStatusOpen},
		{ActionStatusCompleted, ActionStatusCancelled},
		{ActionStatusCancelled, ActionStatusCompleted},
		{ActionStatus("unknown"), ActionStatusOpen},
	}
	for _, transition := range invalid {
		if err := ValidateActionStatusTransition(transition[0], transition[1]); !errors.Is(err, ErrInvalidActionStateTransition) {
			t.Errorf("ValidateActionStatusTransition(%q, %q) error = %v, want ErrInvalidActionStateTransition", transition[0], transition[1], err)
		}
	}
}

func TestCommentStateMachineIsOneWay(t *testing.T) {
	t.Parallel()
	if err := ValidateCommentStateTransition(CommentStateActive, CommentStateRedacted); err != nil {
		t.Fatalf("active to redacted error = %v", err)
	}
	for _, transition := range [][2]CommentState{
		{CommentStateRedacted, CommentStateActive},
		{CommentStateActive, CommentStateActive},
		{CommentStateRedacted, CommentStateRedacted},
		{CommentState("unknown"), CommentStateRedacted},
	} {
		if err := ValidateCommentStateTransition(transition[0], transition[1]); !errors.Is(err, ErrInvalidCommentStateTransition) {
			t.Errorf("ValidateCommentStateTransition(%q, %q) error = %v, want ErrInvalidCommentStateTransition", transition[0], transition[1], err)
		}
	}
}

func TestNotificationDeliveryStateMachine(t *testing.T) {
	t.Parallel()
	valid := [][2]NotificationDeliveryState{
		{NotificationDeliveryPending, NotificationDeliveryProcessing},
		{NotificationDeliveryPending, NotificationDeliveryCancelled},
		{NotificationDeliveryProcessing, NotificationDeliverySent},
		{NotificationDeliveryProcessing, NotificationDeliveryRetryWait},
		{NotificationDeliveryProcessing, NotificationDeliveryPermanentFailure},
		{NotificationDeliveryProcessing, NotificationDeliveryUnknownOutcome},
		{NotificationDeliveryProcessing, NotificationDeliveryCancelled},
		{NotificationDeliveryRetryWait, NotificationDeliveryProcessing},
		{NotificationDeliveryRetryWait, NotificationDeliveryPermanentFailure},
		{NotificationDeliveryRetryWait, NotificationDeliveryCancelled},
	}
	for _, transition := range valid {
		if err := ValidateNotificationDeliveryStateTransition(transition[0], transition[1]); err != nil {
			t.Errorf("ValidateNotificationDeliveryStateTransition(%q, %q) error = %v", transition[0], transition[1], err)
		}
	}
	invalid := [][2]NotificationDeliveryState{
		{NotificationDeliveryPending, NotificationDeliverySent},
		{NotificationDeliverySent, NotificationDeliveryRetryWait},
		{NotificationDeliveryCancelled, NotificationDeliveryProcessing},
		{NotificationDeliveryPermanentFailure, NotificationDeliveryProcessing},
		{NotificationDeliveryUnknownOutcome, NotificationDeliveryProcessing},
		{NotificationDeliveryState("unknown"), NotificationDeliveryPending},
	}
	for _, transition := range invalid {
		if err := ValidateNotificationDeliveryStateTransition(transition[0], transition[1]); !errors.Is(err, ErrInvalidNotificationDeliveryStateTransition) {
			t.Errorf("ValidateNotificationDeliveryStateTransition(%q, %q) error = %v, want ErrInvalidNotificationDeliveryStateTransition", transition[0], transition[1], err)
		}
	}
}

func TestNotificationValuesAreClosedAndRetryBounded(t *testing.T) {
	t.Parallel()
	if CommentRenderContractVersionV1 != "comment_markdown/v1" {
		t.Fatalf("CommentRenderContractVersionV1 = %q", CommentRenderContractVersionV1)
	}
	for _, preference := range []FollowerPreference{FollowerPreferenceDefault, FollowerPreferenceWatching, FollowerPreferenceMuted} {
		if err := ValidateFollowerPreference(preference); err != nil {
			t.Errorf("ValidateFollowerPreference(%q) error = %v", preference, err)
		}
	}
	if err := ValidateFollowerPreference("unknown"); !errors.Is(err, ErrInvalidFollowerPreference) {
		t.Fatalf("unknown follower preference error = %v", err)
	}
	for _, reason := range []NotificationReason{
		NotificationReasonOwner,
		NotificationReasonParticipant,
		NotificationReasonAssignee,
		NotificationReasonMention,
		NotificationReasonReply,
		NotificationReasonFollower,
		NotificationReasonSecurity,
	} {
		if err := ValidateNotificationReason(reason); err != nil {
			t.Errorf("ValidateNotificationReason(%q) error = %v", reason, err)
		}
		wantMandatory := reason == NotificationReasonAssignee || reason == NotificationReasonMention || reason == NotificationReasonSecurity
		if got := reason.Mandatory(); got != wantMandatory {
			t.Errorf("NotificationReason(%q).Mandatory() = %t, want %t", reason, got, wantMandatory)
		}
	}
	for _, channel := range []NotificationDeliveryChannel{NotificationDeliveryTelegram, NotificationDeliveryFeishu} {
		if err := ValidateNotificationDeliveryChannel(channel); err != nil {
			t.Errorf("ValidateNotificationDeliveryChannel(%q) error = %v", channel, err)
		}
	}
	if MaxNotificationDeliveryAttempts != 8 {
		t.Fatalf("MaxNotificationDeliveryAttempts = %d, want 8", MaxNotificationDeliveryAttempts)
	}
	for attempt := uint8(1); attempt <= MaxNotificationDeliveryAttempts; attempt++ {
		if err := ValidateNotificationDeliveryAttempt(attempt); err != nil {
			t.Errorf("ValidateNotificationDeliveryAttempt(%d) error = %v", attempt, err)
		}
	}
	for _, attempt := range []uint8{0, MaxNotificationDeliveryAttempts + 1} {
		if err := ValidateNotificationDeliveryAttempt(attempt); !errors.Is(err, ErrInvalidNotificationDeliveryAttempt) {
			t.Errorf("ValidateNotificationDeliveryAttempt(%d) error = %v, want ErrInvalidNotificationDeliveryAttempt", attempt, err)
		}
	}
}

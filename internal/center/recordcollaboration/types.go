// Package recordcollaboration owns transport-neutral collaboration identities,
// immutable fence bindings, and closed state registries.
package recordcollaboration

import (
	"errors"
	"fmt"
	"math"

	"houfeng/internal/center/recordplatform"
)

var (
	ErrInvalidActionID                            = errors.New("invalid record action id")
	ErrInvalidActionEventID                       = errors.New("invalid record action event id")
	ErrInvalidCommentID                           = errors.New("invalid record comment id")
	ErrInvalidCommentRevisionID                   = errors.New("invalid record comment revision id")
	ErrInvalidCommentTombstoneID                  = errors.New("invalid record comment tombstone id")
	ErrInvalidNotificationID                      = errors.New("invalid record notification id")
	ErrInvalidNotificationDeliveryID              = errors.New("invalid record notification delivery id")
	ErrInvalidNotificationDeliveryAttemptID       = errors.New("invalid record notification delivery attempt id")
	ErrInvalidRecordFenceBinding                  = errors.New("invalid record collaboration fence binding")
	ErrInvalidActionStateTransition               = errors.New("invalid record action state transition")
	ErrInvalidCommentStateTransition              = errors.New("invalid record comment state transition")
	ErrInvalidNotificationDeliveryStateTransition = errors.New("invalid record notification delivery state transition")
	ErrInvalidFollowerPreference                  = errors.New("invalid record follower preference")
	ErrInvalidNotificationReason                  = errors.New("invalid record notification reason")
	ErrInvalidNotificationDeliveryChannel         = errors.New("invalid record notification delivery channel")
	ErrInvalidNotificationDeliveryAttempt         = errors.New("invalid record notification delivery attempt")
)

const CommentRenderContractVersionV1 = "comment_markdown/v1"

const MaxNotificationDeliveryAttempts uint8 = 8

func ValidateActionID(value string) error {
	return validateCollaborationID(value, "ract_", ErrInvalidActionID)
}

func ValidateActionEventID(value string) error {
	return validateCollaborationID(value, "raev_", ErrInvalidActionEventID)
}

func ValidateCommentID(value string) error {
	return validateCollaborationID(value, "rcm_", ErrInvalidCommentID)
}

func ValidateCommentRevisionID(value string) error {
	return validateCollaborationID(value, "rcr_", ErrInvalidCommentRevisionID)
}

func ValidateCommentTombstoneID(value string) error {
	return validateCollaborationID(value, "rct_", ErrInvalidCommentTombstoneID)
}

func ValidateNotificationID(value string) error {
	const prefix = "rnt_"
	if len(value) != len(prefix)+64 || value[:len(prefix)] != prefix {
		return ErrInvalidNotificationID
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'f') && (character < '0' || character > '9') {
			return ErrInvalidNotificationID
		}
	}
	return nil
}

func ValidateNotificationDeliveryID(value string) error {
	return validateCollaborationID(value, "rnd_", ErrInvalidNotificationDeliveryID)
}

func ValidateNotificationDeliveryAttemptID(value string) error {
	return validateCollaborationID(value, "rna_", ErrInvalidNotificationDeliveryAttemptID)
}

func validateCollaborationID(value, prefix string, sentinel error) error {
	if len(value) <= len(prefix) || len(value) > len(prefix)+64 || value[:len(prefix)] != prefix {
		return sentinel
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return sentinel
		}
	}
	return nil
}

type RecordFenceBinding struct {
	projectID recordplatform.ProjectID
	recordID  string
	epoch     recordplatform.ContentEpoch
	sealed    bool
}

func NewRecordFenceBinding(
	projectID recordplatform.ProjectID,
	recordID string,
	epoch recordplatform.ContentEpoch,
) (RecordFenceBinding, error) {
	binding := RecordFenceBinding{
		projectID: projectID,
		recordID:  recordID,
		epoch:     epoch,
		sealed:    true,
	}
	if err := binding.Validate(); err != nil {
		return RecordFenceBinding{}, err
	}
	return binding, nil
}

func (binding RecordFenceBinding) ProjectID() recordplatform.ProjectID {
	return binding.projectID
}

func (binding RecordFenceBinding) RecordID() string {
	return binding.recordID
}

func (binding RecordFenceBinding) Epoch() recordplatform.ContentEpoch {
	return binding.epoch
}

func (binding RecordFenceBinding) Validate() error {
	if !binding.sealed || recordplatform.ValidateProjectID(binding.projectID) != nil ||
		!validRecordID(binding.recordID) ||
		binding.epoch > recordplatform.ContentEpoch(math.MaxInt64) {
		return ErrInvalidRecordFenceBinding
	}
	return nil
}

func validRecordID(value string) bool {
	if len(value) <= len("rec_") || len(value) > len("rec_")+64 || value[:len("rec_")] != "rec_" {
		return false
	}
	for _, character := range value[len("rec_"):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

type ActionStatus string

const (
	ActionStatusOpen      ActionStatus = "open"
	ActionStatusCompleted ActionStatus = "completed"
	ActionStatusCancelled ActionStatus = "cancelled"
)

func ValidateActionStatusTransition(from, to ActionStatus) error {
	valid := false
	switch from {
	case ActionStatusOpen:
		valid = to == ActionStatusCompleted || to == ActionStatusCancelled
	case ActionStatusCompleted, ActionStatusCancelled:
		valid = to == ActionStatusOpen
	}
	if !valid {
		return fmt.Errorf("%w: %q to %q", ErrInvalidActionStateTransition, from, to)
	}
	return nil
}

type CommentState string

const (
	CommentStateActive   CommentState = "active"
	CommentStateRedacted CommentState = "redacted"
)

func ValidateCommentStateTransition(from, to CommentState) error {
	if from != CommentStateActive || to != CommentStateRedacted {
		return fmt.Errorf("%w: %q to %q", ErrInvalidCommentStateTransition, from, to)
	}
	return nil
}

type NotificationDeliveryState string

const (
	NotificationDeliveryPending          NotificationDeliveryState = "pending"
	NotificationDeliveryProcessing       NotificationDeliveryState = "processing"
	NotificationDeliveryRetryWait        NotificationDeliveryState = "retry_wait"
	NotificationDeliverySent             NotificationDeliveryState = "sent"
	NotificationDeliveryCancelled        NotificationDeliveryState = "cancelled"
	NotificationDeliveryPermanentFailure NotificationDeliveryState = "permanent_failure"
	NotificationDeliveryUnknownOutcome   NotificationDeliveryState = "unknown_outcome"
)

func ValidateNotificationDeliveryStateTransition(from, to NotificationDeliveryState) error {
	valid := false
	switch from {
	case NotificationDeliveryPending:
		valid = to == NotificationDeliveryProcessing || to == NotificationDeliveryCancelled
	case NotificationDeliveryProcessing:
		valid = to == NotificationDeliverySent || to == NotificationDeliveryRetryWait ||
			to == NotificationDeliveryPermanentFailure || to == NotificationDeliveryCancelled ||
			to == NotificationDeliveryUnknownOutcome
	case NotificationDeliveryRetryWait:
		valid = to == NotificationDeliveryProcessing || to == NotificationDeliveryPermanentFailure ||
			to == NotificationDeliveryCancelled
	}
	if !valid {
		return fmt.Errorf("%w: %q to %q", ErrInvalidNotificationDeliveryStateTransition, from, to)
	}
	return nil
}

type FollowerPreference string

const (
	FollowerPreferenceDefault  FollowerPreference = "default"
	FollowerPreferenceWatching FollowerPreference = "watching"
	FollowerPreferenceMuted    FollowerPreference = "muted"
)

func ValidateFollowerPreference(preference FollowerPreference) error {
	switch preference {
	case FollowerPreferenceDefault, FollowerPreferenceWatching, FollowerPreferenceMuted:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidFollowerPreference, preference)
	}
}

type NotificationReason string

const (
	NotificationReasonOwner       NotificationReason = "owner"
	NotificationReasonParticipant NotificationReason = "participant"
	NotificationReasonAssignee    NotificationReason = "assignee"
	NotificationReasonMention     NotificationReason = "mention"
	NotificationReasonReply       NotificationReason = "reply"
	NotificationReasonFollower    NotificationReason = "follower"
	NotificationReasonSecurity    NotificationReason = "security"
)

func ValidateNotificationReason(reason NotificationReason) error {
	switch reason {
	case NotificationReasonOwner, NotificationReasonParticipant,
		NotificationReasonAssignee, NotificationReasonMention,
		NotificationReasonReply, NotificationReasonFollower,
		NotificationReasonSecurity:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidNotificationReason, reason)
	}
}

func (reason NotificationReason) Mandatory() bool {
	return reason == NotificationReasonAssignee || reason == NotificationReasonMention ||
		reason == NotificationReasonSecurity
}

type NotificationDeliveryChannel string

const (
	NotificationDeliveryTelegram NotificationDeliveryChannel = "telegram"
	NotificationDeliveryFeishu   NotificationDeliveryChannel = "feishu"
)

func ValidateNotificationDeliveryChannel(channel NotificationDeliveryChannel) error {
	switch channel {
	case NotificationDeliveryTelegram, NotificationDeliveryFeishu:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidNotificationDeliveryChannel, channel)
	}
}

func ValidateNotificationDeliveryAttempt(attempt uint8) error {
	if attempt == 0 || attempt > MaxNotificationDeliveryAttempts {
		return fmt.Errorf("%w: %d", ErrInvalidNotificationDeliveryAttempt, attempt)
	}
	return nil
}

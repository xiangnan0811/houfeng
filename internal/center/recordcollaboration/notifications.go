package recordcollaboration

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"houfeng/internal/center/recordauth"
)

const notificationIdentityDomainV1 = "houfeng.record-collaboration.notification.v1"

var ErrInvalidNotificationFacts = errors.New("invalid record notification facts")

type NotificationEventKind string

const (
	NotificationEventRecordOwnerChanged       NotificationEventKind = "record_owner_changed"
	NotificationEventRecordParticipantChanged NotificationEventKind = "record_participant_changed"
	NotificationEventRecordFollowUpDue        NotificationEventKind = "record_follow_up_due"
	NotificationEventActionAssigned           NotificationEventKind = "action_assigned"
	NotificationEventActionCompleted          NotificationEventKind = "action_completed"
	NotificationEventActionCancelled          NotificationEventKind = "action_cancelled"
	NotificationEventCommentReplied           NotificationEventKind = "comment_replied"
	NotificationEventCommentMentioned         NotificationEventKind = "comment_mentioned"
	NotificationEventSecurityAccessRevoked    NotificationEventKind = "security_access_revoked"
)

type NotificationSubjectKind string

const (
	NotificationSubjectRecord  NotificationSubjectKind = "record"
	NotificationSubjectAction  NotificationSubjectKind = "action"
	NotificationSubjectComment NotificationSubjectKind = "comment"
)

type FollowerSources struct {
	Author      bool
	Owner       bool
	Participant bool
	Comment     bool
	Mention     bool
	Action      bool
}

func (sources FollowerSources) Any() bool {
	return sources.Author || sources.Owner || sources.Participant || sources.Comment || sources.Mention || sources.Action
}

func (sources FollowerSources) merged(other FollowerSources) FollowerSources {
	return FollowerSources{
		Author: sources.Author || other.Author, Owner: sources.Owner || other.Owner,
		Participant: sources.Participant || other.Participant, Comment: sources.Comment || other.Comment,
		Mention: sources.Mention || other.Mention, Action: sources.Action || other.Action,
	}
}

type FollowerFacts struct {
	UserID     string
	Version    uint64
	Preference FollowerPreference
	Sources    FollowerSources
}

func (facts FollowerFacts) Validate() error {
	if recordauth.ValidateActorUserID(facts.UserID) != nil || ValidateFollowerPreference(facts.Preference) != nil || facts.Version > math.MaxInt64 {
		return ErrInvalidNotificationFacts
	}
	if facts.Version == 0 && (facts.Preference != FollowerPreferenceDefault || facts.Sources.Any()) {
		return ErrInvalidNotificationFacts
	}
	return nil
}

type NotificationEventFacts struct {
	Kind               NotificationEventKind
	RecordID           string
	SubjectKind        NotificationSubjectKind
	SubjectID          string
	SourceVersion      uint64
	ActorID            string
	AuthorizationEpoch uint64
	RecordFenceEpoch   uint64
	OccurredAt         time.Time
}

func (facts NotificationEventFacts) Validate() error {
	expected, ok := notificationSubjectForEvent(facts.Kind)
	if !ok || facts.SubjectKind != expected || !validRecordID(facts.RecordID) ||
		facts.SourceVersion == 0 || facts.SourceVersion > math.MaxInt64 ||
		facts.AuthorizationEpoch == 0 || facts.AuthorizationEpoch > math.MaxInt64 ||
		facts.RecordFenceEpoch > math.MaxInt64 || recordauth.ValidateActorUserID(facts.ActorID) != nil || facts.OccurredAt.IsZero() {
		return ErrInvalidNotificationFacts
	}
	switch facts.SubjectKind {
	case NotificationSubjectRecord:
		if facts.SubjectID != facts.RecordID {
			return ErrInvalidNotificationFacts
		}
	case NotificationSubjectAction:
		if ValidateActionID(facts.SubjectID) != nil {
			return ErrInvalidNotificationFacts
		}
	case NotificationSubjectComment:
		if ValidateCommentID(facts.SubjectID) != nil {
			return ErrInvalidNotificationFacts
		}
	default:
		return ErrInvalidNotificationFacts
	}
	return nil
}

func (facts NotificationEventFacts) NotificationID() string {
	if facts.Validate() != nil {
		return ""
	}
	encoder := actionCanonicalEncoder{}
	encoder.string(notificationIdentityDomainV1)
	encoder.string(string(facts.Kind))
	encoder.string(facts.RecordID)
	encoder.string(string(facts.SubjectKind))
	encoder.string(facts.SubjectID)
	encoder.uint64(facts.SourceVersion)
	digest := sha256.Sum256(encoder.bytes)
	return "rnt_" + hex.EncodeToString(digest[:])
}

type NotificationRecipientCandidate struct {
	UserID string
	Reason NotificationReason
}

type NotificationRecipientFacts struct {
	UserID    string
	Reason    NotificationReason
	Mandatory bool
}

func NormalizeNotificationRecipients(
	event NotificationEventFacts,
	followers []FollowerFacts,
	direct []NotificationRecipientCandidate,
) ([]NotificationRecipientFacts, error) {
	if event.Validate() != nil {
		return nil, ErrInvalidNotificationFacts
	}
	followerByUser := make(map[string]FollowerFacts, len(followers))
	for _, follower := range followers {
		if follower.Validate() != nil {
			return nil, ErrInvalidNotificationFacts
		}
		if prior, ok := followerByUser[follower.UserID]; ok {
			if prior.Preference != follower.Preference || (prior.Version != 0 && follower.Version != 0 && prior.Version != follower.Version) {
				return nil, ErrInvalidNotificationFacts
			}
			prior.Sources = prior.Sources.merged(follower.Sources)
			followerByUser[follower.UserID] = prior
			continue
		}
		followerByUser[follower.UserID] = follower
	}

	selected := make(map[string]NotificationReason)
	for userID, follower := range followerByUser {
		if userID == event.ActorID || follower.Preference == FollowerPreferenceMuted ||
			(follower.Preference != FollowerPreferenceWatching && !follower.Sources.Any()) {
			continue
		}
		selected[userID] = NotificationReasonFollower
	}
	for _, candidate := range direct {
		if recordauth.ValidateActorUserID(candidate.UserID) != nil || ValidateNotificationReason(candidate.Reason) != nil {
			return nil, ErrInvalidNotificationFacts
		}
		if candidate.UserID == event.ActorID {
			continue
		}
		mandatory := candidate.Reason.Mandatory()
		if !mandatory {
			if follower, ok := followerByUser[candidate.UserID]; ok && follower.Preference == FollowerPreferenceMuted {
				continue
			}
		}
		if prior, ok := selected[candidate.UserID]; !ok || notificationReasonPriority(candidate.Reason) > notificationReasonPriority(prior) {
			selected[candidate.UserID] = candidate.Reason
		}
	}

	userIDs := make([]string, 0, len(selected))
	for userID := range selected {
		userIDs = append(userIDs, userID)
	}
	sort.Strings(userIDs)
	result := make([]NotificationRecipientFacts, 0, len(userIDs))
	for _, userID := range userIDs {
		reason := selected[userID]
		result = append(result, NotificationRecipientFacts{UserID: userID, Reason: reason, Mandatory: reason.Mandatory()})
	}
	return result, nil
}

func notificationSubjectForEvent(kind NotificationEventKind) (NotificationSubjectKind, bool) {
	switch kind {
	case NotificationEventRecordOwnerChanged, NotificationEventRecordParticipantChanged,
		NotificationEventRecordFollowUpDue, NotificationEventSecurityAccessRevoked:
		return NotificationSubjectRecord, true
	case NotificationEventActionAssigned, NotificationEventActionCompleted, NotificationEventActionCancelled:
		return NotificationSubjectAction, true
	case NotificationEventCommentReplied, NotificationEventCommentMentioned:
		return NotificationSubjectComment, true
	default:
		return "", false
	}
}

func notificationReasonPriority(reason NotificationReason) int {
	return slices.Index([]NotificationReason{
		NotificationReasonFollower, NotificationReasonParticipant, NotificationReasonOwner,
		NotificationReasonReply, NotificationReasonAssignee, NotificationReasonMention,
		NotificationReasonSecurity,
	}, reason)
}

func (facts NotificationRecipientFacts) Validate() error {
	if recordauth.ValidateActorUserID(facts.UserID) != nil || ValidateNotificationReason(facts.Reason) != nil || facts.Mandatory != facts.Reason.Mandatory() {
		return fmt.Errorf("%w: recipient", ErrInvalidNotificationFacts)
	}
	return nil
}

func validNotificationSubjectIdentity(kind NotificationSubjectKind, subjectID string) bool {
	switch kind {
	case NotificationSubjectRecord:
		return validRecordID(subjectID)
	case NotificationSubjectAction:
		return ValidateActionID(subjectID) == nil
	case NotificationSubjectComment:
		return ValidateCommentID(subjectID) == nil
	default:
		return false
	}
}

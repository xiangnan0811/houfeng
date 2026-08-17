package recordcollaboration

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestNormalizeNotificationRecipientsAppliesMandatoryOptionalAndSelfNoisePolicy(t *testing.T) {
	event := NotificationEventFacts{
		Kind: NotificationEventActionCompleted, RecordID: "rec_policy1",
		SubjectKind: NotificationSubjectAction, SubjectID: "ract_policy1", SourceVersion: 3,
		ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", AuthorizationEpoch: 7, RecordFenceEpoch: 4,
		OccurredAt: time.Date(2026, 8, 17, 2, 3, 4, 0, time.UTC),
	}
	followers := []FollowerFacts{
		{UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Version: 1, Preference: FollowerPreferenceWatching},
		{UserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", Version: 1, Preference: FollowerPreferenceMuted, Sources: FollowerSources{Action: true}},
		{UserID: "usr_cccccccccccccccccccccccc", Version: 1, Preference: FollowerPreferenceDefault, Sources: FollowerSources{Participant: true}},
		{UserID: "usr_dddddddddddddddddddddddd", Version: 1, Preference: FollowerPreferenceMuted, Sources: FollowerSources{Owner: true}},
		{UserID: "usr_eeeeeeeeeeeeeeeeeeeeeeee", Version: 1, Preference: FollowerPreferenceDefault},
		{UserID: "usr_ffffffffffffffffffffffff", Version: 1, Preference: FollowerPreferenceWatching},
	}
	direct := []NotificationRecipientCandidate{
		{UserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", Reason: NotificationReasonFollower},
		{UserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", Reason: NotificationReasonAssignee},
		{UserID: "usr_cccccccccccccccccccccccc", Reason: NotificationReasonParticipant},
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("event.Validate() error = %v", err)
	}
	for _, follower := range followers {
		if err := follower.Validate(); err != nil {
			t.Fatalf("follower %#v Validate() error = %v", follower, err)
		}
	}

	recipients, err := NormalizeNotificationRecipients(event, followers, direct)
	if err != nil {
		t.Fatalf("NormalizeNotificationRecipients() error = %v", err)
	}
	want := []NotificationRecipientFacts{
		{UserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", Reason: NotificationReasonAssignee, Mandatory: true},
		{UserID: "usr_cccccccccccccccccccccccc", Reason: NotificationReasonParticipant},
		{UserID: "usr_ffffffffffffffffffffffff", Reason: NotificationReasonFollower},
	}
	if !slices.Equal(recipients, want) {
		t.Fatalf("recipients = %#v, want %#v", recipients, want)
	}
	if got := event.NotificationID(); got != "rnt_6337a588d8c27f5f41d1d821457a0d55eda8c94e7ccebc1b6363542b0ffbbab3" {
		t.Fatalf("NotificationID() = %q", got)
	}
}

func TestNormalizeNotificationRecipientsUsesClosedReasonPrecedenceAndStableOrdering(t *testing.T) {
	event := NotificationEventFacts{
		Kind: NotificationEventCommentMentioned, RecordID: "rec_policy2",
		SubjectKind: NotificationSubjectComment, SubjectID: "rcm_policy2", SourceVersion: 2,
		ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", AuthorizationEpoch: 9, RecordFenceEpoch: 5,
		OccurredAt: time.Date(2026, 8, 17, 2, 3, 4, 0, time.UTC),
	}
	followers := []FollowerFacts{
		{UserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", Version: 1, Preference: FollowerPreferenceWatching},
		{UserID: "usr_cccccccccccccccccccccccc", Version: 1, Preference: FollowerPreferenceMuted, Sources: FollowerSources{Mention: true}},
	}
	direct := []NotificationRecipientCandidate{
		{UserID: "usr_cccccccccccccccccccccccc", Reason: NotificationReasonOwner},
		{UserID: "usr_cccccccccccccccccccccccc", Reason: NotificationReasonAssignee},
		{UserID: "usr_cccccccccccccccccccccccc", Reason: NotificationReasonMention},
		{UserID: "usr_cccccccccccccccccccccccc", Reason: NotificationReasonSecurity},
		{UserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", Reason: NotificationReasonReply},
	}

	recipients, err := NormalizeNotificationRecipients(event, followers, direct)
	if err != nil {
		t.Fatalf("NormalizeNotificationRecipients() error = %v", err)
	}
	want := []NotificationRecipientFacts{
		{UserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", Reason: NotificationReasonReply},
		{UserID: "usr_cccccccccccccccccccccccc", Reason: NotificationReasonSecurity, Mandatory: true},
	}
	if !slices.Equal(recipients, want) {
		t.Fatalf("recipients = %#v, want %#v", recipients, want)
	}
}

func TestNotificationFactsRejectUnknownAndMalformedValues(t *testing.T) {
	valid := NotificationEventFacts{
		Kind: NotificationEventRecordOwnerChanged, RecordID: "rec_policy3",
		SubjectKind: NotificationSubjectRecord, SubjectID: "rec_policy3", SourceVersion: 1,
		ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", AuthorizationEpoch: 1, RecordFenceEpoch: 0,
		OccurredAt: time.Date(2026, 8, 17, 2, 3, 4, 0, time.UTC),
	}
	tests := []struct {
		name   string
		mutate func(*NotificationEventFacts)
	}{
		{name: "unknown event", mutate: func(facts *NotificationEventFacts) { facts.Kind = "unknown" }},
		{name: "subject mismatch", mutate: func(facts *NotificationEventFacts) { facts.SubjectKind = NotificationSubjectAction }},
		{name: "zero version", mutate: func(facts *NotificationEventFacts) { facts.SourceVersion = 0 }},
		{name: "invalid actor", mutate: func(facts *NotificationEventFacts) { facts.ActorID = "actor" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts := valid
			tt.mutate(&facts)
			if err := facts.Validate(); !errors.Is(err, ErrInvalidNotificationFacts) {
				t.Fatalf("Validate() error = %v, want ErrInvalidNotificationFacts", err)
			}
		})
	}
}

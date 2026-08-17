package recordplatform

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestOutboxEventV1AcceptsOnlyClosedIdentityFields(t *testing.T) {
	event := OutboxEvent{
		ProjectID:          string(ProjectIDDefault),
		EventKind:          OutboxEventKindRecordCreated,
		SubjectKind:        OutboxSubjectKindRecord,
		SubjectID:          "rec_01",
		AuthorizationEpoch: 0,
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("OutboxEvent.Validate() error = %v", err)
	}
	for _, fieldName := range []string{"ProjectID", "EventKind", "SubjectKind", "SubjectID"} {
		field, exists := reflect.TypeFor[OutboxEvent]().FieldByName(fieldName)
		if !exists || field.Type.Kind() != reflect.String {
			t.Fatalf("OutboxEvent.%s must be a string identity field", fieldName)
		}
	}

	for _, fieldName := range []string{"Body", "Payload", "Recipient", "Renderer", "Rendered", "Template"} {
		if _, exists := reflect.TypeFor[OutboxEvent]().FieldByName(fieldName); exists {
			t.Fatalf("OutboxEvent must not retain delivery field %q", fieldName)
		}
	}

	for _, invalid := range []OutboxEvent{
		{RowID: -1, ProjectID: string(ProjectIDDefault), EventKind: OutboxEventKindRecordCreated, SubjectKind: OutboxSubjectKindRecord, SubjectID: "rec_01"},
		{ProjectID: "other", EventKind: OutboxEventKindRecordCreated, SubjectKind: OutboxSubjectKindRecord, SubjectID: "rec_01"},
		{ProjectID: string(ProjectIDDefault), EventKind: "record_rendered", SubjectKind: OutboxSubjectKindRecord, SubjectID: "rec_01"},
		{ProjectID: string(ProjectIDDefault), EventKind: OutboxEventKindRecordCreated, SubjectKind: "record_body", SubjectID: "rec_01"},
		{ProjectID: string(ProjectIDDefault), EventKind: OutboxEventKindRecordCreated, SubjectKind: OutboxSubjectKindRecord, SubjectID: "REC_01"},
		{ProjectID: string(ProjectIDDefault), EventKind: OutboxEventKindRecordCreated, SubjectKind: OutboxSubjectKindRecord, SubjectID: ""},
	} {
		if err := invalid.Validate(); !errors.Is(err, ErrInvalidOutboxEvent) {
			t.Fatalf("OutboxEvent.Validate(%#v) error = %v, want ErrInvalidOutboxEvent", invalid, err)
		}
	}
}

func TestOutboxEventV1AcceptsClosedCollaborationRevisionKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{
		OutboxEventKindRecordOwnerChanged,
		OutboxEventKindRecordParticipantChanged,
	} {
		event := OutboxEvent{
			ProjectID: string(ProjectIDDefault), EventKind: kind,
			SubjectKind: OutboxSubjectKindRecord, SubjectID: "rec_collaboration",
			AuthorizationEpoch: 7,
		}
		if err := event.Validate(); err != nil {
			t.Fatalf("OutboxEvent.Validate(%q) error = %v", kind, err)
		}
	}
}

func TestOutboxEnqueueInputV1RequiresPositiveMicrosecondExpiry(t *testing.T) {
	event := OutboxEvent{
		ProjectID:          string(ProjectIDDefault),
		EventKind:          OutboxEventKindRecordCreated,
		SubjectKind:        OutboxSubjectKindRecord,
		SubjectID:          "rec_01",
		AuthorizationEpoch: 3,
	}
	input := OutboxEnqueueInputV1{Event: event, ExpiresAfter: time.Hour}
	if err := input.Validate(); err != nil {
		t.Fatalf("OutboxEnqueueInputV1.Validate() error = %v", err)
	}

	for _, expiry := range []time.Duration{0, -time.Second, time.Nanosecond} {
		if err := (OutboxEnqueueInputV1{Event: event, ExpiresAfter: expiry}).Validate(); !errors.Is(err, ErrInvalidOutboxEnqueue) {
			t.Fatalf("OutboxEnqueueInputV1{ExpiresAfter: %s}.Validate() error = %v, want ErrInvalidOutboxEnqueue", expiry, err)
		}
	}
	if err := (OutboxEnqueueInputV1{Event: OutboxEvent{RowID: 42, ProjectID: event.ProjectID, EventKind: event.EventKind, SubjectKind: event.SubjectKind, SubjectID: event.SubjectID}, ExpiresAfter: time.Hour}).Validate(); !errors.Is(err, ErrInvalidOutboxEnqueue) {
		t.Fatalf("OutboxEnqueueInputV1 with row identity error = %v, want ErrInvalidOutboxEnqueue", err)
	}
}

func TestOutboxClaimV1RequiresLiveFencedOwnerAndExpiryAfterLease(t *testing.T) {
	claim := ClaimedOutboxEventV1{
		Event: OutboxEvent{
			RowID:              42,
			ProjectID:          string(ProjectIDDefault),
			EventKind:          OutboxEventKindRecordCreated,
			SubjectKind:        OutboxSubjectKindRecord,
			SubjectID:          "rec_01",
			AuthorizationEpoch: 3,
		},
		Owner: OwnerLease{
			OwnerID:    "worker_01",
			Generation: 2,
			ExpiresAt:  time.Date(2026, time.July, 24, 13, 0, 0, 0, time.UTC),
		},
		ExpiresAt: time.Date(2026, time.July, 24, 14, 0, 0, 0, time.UTC),
	}
	if err := claim.Validate(); err != nil {
		t.Fatalf("ClaimedOutboxEventV1.Validate() error = %v", err)
	}

	eventWithoutRowID := claim.Event
	eventWithoutRowID.RowID = 0
	for _, invalid := range []ClaimedOutboxEventV1{
		{Event: eventWithoutRowID, Owner: claim.Owner, ExpiresAt: claim.ExpiresAt},
		{Event: claim.Event, ExpiresAt: claim.ExpiresAt},
		{Event: claim.Event, Owner: claim.Owner, ExpiresAt: claim.Owner.ExpiresAt},
	} {
		if err := invalid.Validate(); !errors.Is(err, ErrInvalidOutboxClaim) {
			t.Fatalf("ClaimedOutboxEventV1.Validate(%#v) error = %v, want ErrInvalidOutboxClaim", invalid, err)
		}
	}

	input := OutboxClaimInputV1{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute}
	if err := input.Validate(); err != nil {
		t.Fatalf("OutboxClaimInputV1.Validate() error = %v", err)
	}
	if err := (OutboxClaimInputV1{OwnerID: "worker_01", OwnerLeaseDuration: time.Nanosecond}).Validate(); !errors.Is(err, ErrInvalidOutboxClaim) {
		t.Fatalf("OutboxClaimInputV1 short lease error = %v, want ErrInvalidOutboxClaim", err)
	}
}

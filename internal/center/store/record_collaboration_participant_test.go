package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestCollaborationRevisionParticipantAppliesMembershipFenceFollowersActivitiesAndOutboxInOrder(t *testing.T) {
	t.Parallel()

	input := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		participantIDs: []string{
			"usr_dddddddddddddddddddddddd",
			"usr_cccccccccccccccccccccccc",
		},
		followUpAt: collaborationTimePointer(time.Date(2026, time.August, 19, 8, 0, 0, 0, time.UTC)),
	})
	steps := make([]string, 0, 24)
	reader := &collaborationMembershipReaderStub{read: func(_ context.Context, tx pgx.Tx, projectID recordauth.ProjectID, userID string) (recordauth.ActorScope, error) {
		steps = append(steps, "membership:"+userID)
		if tx == nil || projectID != recordauth.ProjectIDDefault {
			t.Fatalf("membership transaction/project = (%#v, %q)", tx, projectID)
		}
		return collaborationActor(t, userID, nil), nil
	}}
	tx := &fakeCollaborationParticipantTx{steps: &steps, fenceEpoch: 7}
	outbox := &collaborationRevisionOutboxStub{steps: &steps}
	participant := NewCollaborationRevisionParticipant(reader)

	if participant.Name() != "collaboration" {
		t.Fatalf("Name() = %q, want collaboration", participant.Name())
	}
	err := participant.ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		Result: records.RevisionCommitResult{
			RecordID: "rec_collaboration", RevisionID: "rrv_collaboration", RevisionNo: 1,
			LockVersion: 1, AuthorizationEpoch: 1, Created: true,
			CommittedAt: time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC),
		},
		Input:        input,
		ActivityKind: records.DomainActivityRecordCreated,
		OutboxTTL:    time.Hour,
		Outbox:       outbox,
	})
	if err != nil {
		t.Fatalf("ApplyRevision() error = %v", err)
	}

	wantSteps := []string{
		"membership:usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		"membership:usr_cccccccccccccccccccccccc",
		"membership:usr_dddddddddddddddddddddddd",
		"deletion_reservation",
		"fence_initialize",
		"fence",
		"deletion_fence_lease",
		"deletion_reservation_recheck",
		"fence",
		"follower:usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		"follower:usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		"follower:usr_cccccccccccccccccccccccc",
		"follower:usr_dddddddddddddddddddddddd",
		"follower_delete_stale",
		"follower_update_stale",
		"activity:record_owner_changed",
		"activity:record_participant_changed",
		"activity:record_follow_up_changed",
		"outbox:record_owner_changed",
		"outbox:record_participant_changed",
	}
	if !reflect.DeepEqual(steps, wantSteps) {
		t.Fatalf("participant steps =\n%#v\nwant\n%#v", steps, wantSteps)
	}
	if got, want := tx.followerFlags, map[string][3]bool{
		"usr_aaaaaaaaaaaaaaaaaaaaaaaa": {true, false, false},
		"usr_bbbbbbbbbbbbbbbbbbbbbbbb": {false, true, false},
		"usr_cccccccccccccccccccccccc": {false, false, true},
		"usr_dddddddddddddddddddddddd": {false, false, true},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("follower flags = %#v, want %#v", got, want)
	}
	for _, epoch := range tx.followerEpochs {
		if epoch != 7 {
			t.Fatalf("follower epoch = %d, want 7", epoch)
		}
	}
	if len(outbox.inputs) != 2 || outbox.inputs[0].Event.AuthorizationEpoch != 1 ||
		outbox.inputs[0].Event.SubjectID != "rec_collaboration" || outbox.inputs[0].ExpiresAfter != time.Hour {
		t.Fatalf("outbox inputs = %#v", outbox.inputs)
	}
}

func TestCollaborationRevisionParticipantRestoreComparesAgainstCurrentBaseRevision(t *testing.T) {
	t.Parallel()

	previousFollowUp := time.Date(2026, time.August, 18, 8, 0, 0, 0, time.UTC)
	restoredFollowUp := previousFollowUp.Add(24 * time.Hour)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID:        "usr_dddddddddddddddddddddddd",
		participantIDs: []string{"usr_cccccccccccccccccccccccc"},
		followUpAt:     &restoredFollowUp,
	})
	steps := make([]string, 0, 24)
	tx := &fakeCollaborationParticipantTx{
		steps:                  &steps,
		fenceEpoch:             9,
		previousOwnerID:        "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		previousParticipantIDs: []string{"usr_cccccccccccccccccccccccc"},
		previousFollowUpAt:     &previousFollowUp,
	}
	reader := &collaborationMembershipReaderStub{read: func(_ context.Context, _ pgx.Tx, _ recordauth.ProjectID, userID string) (recordauth.ActorScope, error) {
		return collaborationActor(t, userID, nil), nil
	}}
	outbox := &collaborationRevisionOutboxStub{steps: &steps}
	participant := NewCollaborationRevisionParticipant(reader)
	err := participant.ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		BaseRevisionID: "rrv_currentbase",
		Result: records.RevisionCommitResult{
			RecordID: "rec_restorecollaboration", RevisionID: "rrv_restoredcollaboration", RevisionNo: 4,
			LockVersion: 8, AuthorizationEpoch: 6, Created: true,
			CommittedAt: time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC),
		},
		Input: input, ActivityKind: records.DomainActivityRecordRestored,
		OutboxTTL: time.Hour, Outbox: outbox,
	})
	if err != nil {
		t.Fatalf("ApplyRevision(restore) error = %v", err)
	}
	if countCollaborationStep(steps, "previous_revision") != 1 || countCollaborationStep(steps, "previous_participants") != 1 {
		t.Fatalf("restore steps = %#v, want current-base reads", steps)
	}
	if countCollaborationStep(steps, "activity:record_owner_changed") != 1 ||
		countCollaborationStep(steps, "activity:record_follow_up_changed") != 1 ||
		countCollaborationStep(steps, "activity:record_participant_changed") != 0 {
		t.Fatalf("restore activity delta steps = %#v", steps)
	}
}

func TestCollaborationRevisionParticipantFailsClosedOnVisibilityAndSourceFloor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input records.CompleteRevisionInput
	}{
		{
			name:  "visibility",
			input: collaborationRestrictedRevisionInput(t, recordauth.SourceStateLive),
		},
		{
			name:  "tombstoned source floor",
			input: collaborationRestrictedRevisionInput(t, recordauth.SourceStateTombstoned),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := make([]string, 0, 4)
			reader := &collaborationMembershipReaderStub{read: func(_ context.Context, _ pgx.Tx, _ recordauth.ProjectID, userID string) (recordauth.ActorScope, error) {
				return collaborationActor(t, userID, nil), nil
			}}
			participant := NewCollaborationRevisionParticipant(reader)
			err := participant.ApplyRevision(context.Background(), &fakeCollaborationParticipantTx{steps: &steps}, records.RevisionCommitted{
				Result: records.RevisionCommitResult{
					RecordID: "rec_authcollaboration", RevisionID: "rrv_authcollaboration", RevisionNo: 1,
					LockVersion: 1, AuthorizationEpoch: 1, Created: true, CommittedAt: time.Now().UTC(),
				},
				Input: tt.input, ActivityKind: records.DomainActivityRecordCreated,
				OutboxTTL: time.Hour, Outbox: &collaborationRevisionOutboxStub{},
			})
			if !errors.Is(err, recordauth.ErrDenied) {
				t.Fatalf("ApplyRevision() error = %v, want recordauth.ErrDenied", err)
			}
			for _, step := range steps {
				if step == "fence" || strings.HasPrefix(step, "follower:") || strings.HasPrefix(step, "activity:") {
					t.Fatalf("authorization failure reached write/fence step %q: %#v", step, steps)
				}
			}
		})
	}
}

func TestCollaborationRevisionParticipantRechecksCurrentDeletionReservationBeforeFollowerWrites(t *testing.T) {
	t.Parallel()

	steps := make([]string, 0, 8)
	reader := &collaborationMembershipReaderStub{read: func(_ context.Context, _ pgx.Tx, _ recordauth.ProjectID, userID string) (recordauth.ActorScope, error) {
		return collaborationActor(t, userID, nil), nil
	}}
	tx := &fakeCollaborationParticipantTx{steps: &steps, reservationState: "fenced"}
	err := NewCollaborationRevisionParticipant(reader).ApplyRevision(context.Background(), tx, records.RevisionCommitted{
		Result: records.RevisionCommitResult{
			RecordID: "rec_reservedcollaboration", RevisionID: "rrv_reservedcollaboration", RevisionNo: 1,
			LockVersion: 1, AuthorizationEpoch: 1, Created: true, CommittedAt: time.Now().UTC(),
		},
		Input: collaborationRevisionInput(t, collaborationRevisionInputValues{
			ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		}),
		ActivityKind: records.DomainActivityRecordCreated,
		OutboxTTL:    time.Hour,
		Outbox:       &collaborationRevisionOutboxStub{},
	})
	if !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("ApplyRevision() error = %v, want ErrRecordDeletionReserved", err)
	}
	for _, step := range steps {
		if strings.HasPrefix(step, "follower:") || strings.HasPrefix(step, "activity:") || strings.HasPrefix(step, "outbox:") {
			t.Fatalf("reserved revision reached projection step %q: %#v", step, steps)
		}
	}
}

func TestCollaborationRevisionParticipantRejectsNilAndTypedNilDependencies(t *testing.T) {
	t.Parallel()

	var typedNilReader *collaborationMembershipReaderStub
	var typedNilOutbox *collaborationRevisionOutboxStub
	tests := []struct {
		name        string
		reader      CollaborationMembershipReader
		transaction pgx.Tx
		outbox      records.RevisionOutbox
	}{
		{name: "nil reader", transaction: &fakeCollaborationParticipantTx{}, outbox: &collaborationRevisionOutboxStub{}},
		{name: "typed nil reader", reader: typedNilReader, transaction: &fakeCollaborationParticipantTx{}, outbox: &collaborationRevisionOutboxStub{}},
		{name: "nil transaction", reader: &collaborationMembershipReaderStub{}, outbox: &collaborationRevisionOutboxStub{}},
		{name: "nil outbox", reader: &collaborationMembershipReaderStub{}, transaction: &fakeCollaborationParticipantTx{}},
		{name: "typed nil outbox", reader: &collaborationMembershipReaderStub{}, transaction: &fakeCollaborationParticipantTx{}, outbox: typedNilOutbox},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			participant := NewCollaborationRevisionParticipant(tt.reader)
			err := participant.ApplyRevision(context.Background(), tt.transaction, records.RevisionCommitted{Outbox: tt.outbox})
			if !errors.Is(err, recordcollaboration.ErrRevisionParticipationUnavailable) {
				t.Fatalf("ApplyRevision() error = %v, want ErrRevisionParticipationUnavailable", err)
			}
		})
	}
}

type collaborationMembershipReaderStub struct {
	read func(context.Context, pgx.Tx, recordauth.ProjectID, string) (recordauth.ActorScope, error)
}

func (reader *collaborationMembershipReaderStub) ReadMemberActor(ctx context.Context, tx pgx.Tx, projectID recordauth.ProjectID, userID string) (recordauth.ActorScope, error) {
	if reader.read == nil {
		return recordauth.ActorScope{}, recordcollaboration.ErrMembershipUnavailable
	}
	return reader.read(ctx, tx, projectID, userID)
}

type collaborationRevisionOutboxStub struct {
	steps  *[]string
	inputs []recordplatform.OutboxEnqueueInputV1
	err    error
}

func (outbox *collaborationRevisionOutboxStub) EnqueueOutbox(_ context.Context, input recordplatform.OutboxEnqueueInputV1) (recordplatform.OutboxEventRecordV1, error) {
	if outbox.steps != nil {
		*outbox.steps = append(*outbox.steps, "outbox:"+input.Event.EventKind)
	}
	outbox.inputs = append(outbox.inputs, input)
	if outbox.err != nil {
		return recordplatform.OutboxEventRecordV1{}, outbox.err
	}
	return recordplatform.OutboxEventRecordV1{Event: input.Event}, nil
}

type fakeCollaborationParticipantTx struct {
	pgx.Tx
	steps                  *[]string
	fenceEpoch             int64
	previousOwnerID        string
	previousParticipantIDs []string
	previousFollowUpAt     *time.Time
	reservationState       string
	followerFlags          map[string][3]bool
	followerEpochs         []int64
	failStep               string
	failErr                error
}

func (tx *fakeCollaborationParticipantTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch {
	case strings.Contains(compact, "from public.deletion_reservations") &&
		strings.Contains(compact, "state in ('previewed', 'fenced', 'committed')"):
		tx.appendStep("deletion_reservation")
		if tx.reservationState == "" {
			return collaborationParticipantRow{err: pgx.ErrNoRows}
		}
		return collaborationParticipantRow{values: []any{tx.reservationState}}
	case strings.Contains(compact, "from public.deletion_fence_leases"):
		tx.appendStep("deletion_fence_lease")
		return collaborationParticipantRow{err: pgx.ErrNoRows}
	case strings.Contains(compact, "from public.deletion_reservations"):
		tx.appendStep("deletion_reservation_recheck")
		return collaborationParticipantRow{err: pgx.ErrNoRows}
	case strings.Contains(compact, "from public.record_revisions"):
		tx.appendStep("previous_revision")
		if tx.shouldFail("previous_revision") {
			return collaborationParticipantRow{err: tx.failErr}
		}
		return collaborationParticipantRow{values: []any{tx.previousOwnerID, tx.previousFollowUpAt}}
	case strings.Contains(compact, "from public.content_delivery_epochs"):
		tx.appendStep("fence")
		if tx.shouldFail("fence") {
			return collaborationParticipantRow{err: tx.failErr}
		}
		return collaborationParticipantRow{values: []any{tx.fenceEpoch}}
	case strings.Contains(compact, "insert into public.record_domain_activities"):
		kind := args[4].(string)
		tx.appendStep("activity:" + kind)
		if tx.shouldFail("activity:" + kind) {
			return collaborationParticipantRow{err: tx.failErr}
		}
		return collaborationParticipantRow{values: []any{time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC)}}
	default:
		return collaborationParticipantRow{err: errors.New("unexpected collaboration QueryRow SQL")}
	}
}

func (tx *fakeCollaborationParticipantTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	if !strings.Contains(compact, "from public.record_revision_participants") {
		return nil, errors.New("unexpected collaboration Query SQL")
	}
	tx.appendStep("previous_participants")
	if tx.shouldFail("previous_participants") {
		return nil, tx.failErr
	}
	return &fakeCollaborationParticipantRows{participantIDs: tx.previousParticipantIDs}, nil
}

func (tx *fakeCollaborationParticipantTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	step := ""
	switch {
	case strings.Contains(compact, "insert into public.content_delivery_epochs"):
		step = "fence_initialize"
	case strings.Contains(compact, "insert into public.record_followers"):
		userID := args[2].(string)
		step = "follower:" + userID
		if tx.followerFlags == nil {
			tx.followerFlags = make(map[string][3]bool)
		}
		tx.followerFlags[userID] = [3]bool{args[3].(bool), args[4].(bool), args[5].(bool)}
		tx.followerEpochs = append(tx.followerEpochs, args[6].(int64))
	case strings.Contains(compact, "delete from public.record_followers"):
		step = "follower_delete_stale"
	case strings.Contains(compact, "update public.record_followers"):
		step = "follower_update_stale"
	default:
		return pgconn.CommandTag{}, errors.New("unexpected collaboration Exec SQL")
	}
	tx.appendStep(step)
	if tx.shouldFail(step) {
		return pgconn.CommandTag{}, tx.failErr
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *fakeCollaborationParticipantTx) appendStep(step string) {
	if tx.steps != nil {
		*tx.steps = append(*tx.steps, step)
	}
}

func (tx *fakeCollaborationParticipantTx) shouldFail(step string) bool {
	return tx.failStep == step
}

type collaborationParticipantRow struct {
	values []any
	err    error
}

func (row collaborationParticipantRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != len(row.values) {
		return errors.New("unexpected collaboration scan destination count")
	}
	for index, value := range row.values {
		if value == nil {
			continue
		}
		target := reflect.ValueOf(dest[index])
		observed := reflect.ValueOf(value)
		if !observed.Type().AssignableTo(target.Elem().Type()) {
			return errors.New("unexpected collaboration scan destination type")
		}
		target.Elem().Set(observed)
	}
	return nil
}

type fakeCollaborationParticipantRows struct {
	pgx.Rows
	participantIDs []string
	index          int
}

func (rows *fakeCollaborationParticipantRows) Close()     {}
func (rows *fakeCollaborationParticipantRows) Err() error { return nil }
func (rows *fakeCollaborationParticipantRows) Next() bool {
	return rows.index < len(rows.participantIDs)
}
func (rows *fakeCollaborationParticipantRows) Scan(dest ...any) error {
	*(dest[0].(*string)) = rows.participantIDs[rows.index]
	rows.index++
	return nil
}

type collaborationRevisionInputValues struct {
	title          string
	ownerID        string
	participantIDs []string
	followUpAt     *time.Time
}

func collaborationRevisionInput(t *testing.T, values collaborationRevisionInputValues) records.CompleteRevisionInput {
	t.Helper()
	visibility := collaborationVisibility(t, recordauth.VisibilityKindProject, nil)
	return collaborationRevisionInputWithAuthorization(t, values, visibility, collaborationSourceAuthorization(t, visibility, visibility, recordauth.SourceStateLive))
}

func collaborationRestrictedRevisionInput(t *testing.T, sourceState recordauth.SourceState) records.CompleteRevisionInput {
	t.Helper()
	project := collaborationVisibility(t, recordauth.VisibilityKindProject, nil)
	restricted := collaborationVisibility(t, recordauth.VisibilityKindRestricted, []string{"rag_collaboration"})
	visibility := restricted
	sourceScope := project
	if sourceState == recordauth.SourceStateTombstoned {
		visibility = project
		sourceScope = restricted
	}
	return collaborationRevisionInputWithAuthorization(t, collaborationRevisionInputValues{
		ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
	}, visibility, collaborationSourceAuthorization(t, project, sourceScope, sourceState))
}

func collaborationRevisionInputWithAuthorization(
	t *testing.T,
	values collaborationRevisionInputValues,
	visibility recordauth.VisibilityScope,
	authorization recordauth.SourceAuthorization,
) records.CompleteRevisionInput {
	t.Helper()
	title := values.title
	if title == "" {
		title = "Collaboration revision"
	}
	participants := make([]records.RevisionParticipantSnapshot, 0, len(values.participantIDs))
	for _, participantID := range values.participantIDs {
		participants = append(participants, records.RevisionParticipantSnapshot{ParticipantID: participantID, IdentitySnapshot: map[string]string{"display_name": "Operator"}})
	}
	input, err := records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues{
		Title: title, BodyMarkdown: "Collaboration revision body",
		MarkdownDialectVersion: records.MarkdownDialectVersionV1,
		RecordType:             records.RecordTypeNote, ImpactLevel: "informational",
		VisibilityScope: visibility,
		Subjects: []records.RevisionSubject{{
			RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
			Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true,
			IdentitySnapshot: map[string]string{"display_name": "VPS"}, CaptureAuthorization: authorization,
		}},
		OwnerID: values.ownerID, Participants: participants, FollowUpAt: values.followUpAt,
		AuthorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("NormalizeCompleteRevisionInput() error = %v", err)
	}
	return input
}

func collaborationVisibility(t *testing.T, kind recordauth.VisibilityKind, groups []string) recordauth.VisibilityScope {
	t.Helper()
	scope, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: kind, ProjectID: recordauth.ProjectIDDefault,
		AllowedGroupIDs: groups, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	return scope
}

func collaborationSourceAuthorization(t *testing.T, capture, current recordauth.VisibilityScope, state recordauth.SourceState) recordauth.SourceAuthorization {
	t.Helper()
	values := recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKindVPS,
		SourceID: testStoreRecordVPSID, State: state, CaptureScope: capture,
	}
	if state == recordauth.SourceStateLive {
		values.CurrentScope = &current
	} else {
		values.FinalFloor = &current
		values.LastLiveScope = &current
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(values)
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return authorization
}

func collaborationActor(t *testing.T, userID string, groups []string) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: userID, Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault, GroupIDs: groups,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func collaborationTimePointer(value time.Time) *time.Time { return &value }

func countCollaborationStep(steps []string, want string) int {
	count := 0
	for _, step := range steps {
		if step == want {
			count++
		}
	}
	return count
}

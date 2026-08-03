package records

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestRecordReadServiceAuthorizesCurrentSourcesBeforeLoadingContent(t *testing.T) {
	actor := mustAuthorizationActor(t, recordauth.RoleProjectAdmin, testRecordGroupID)
	input := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	steps := make([]string, 0, 2)
	current := &currentRecordAuthorizationSourceStub{
		steps: &steps,
		current: CurrentRecordAuthorization{
			RecordID:           "rec_read1",
			CurrentRevisionID:  "rrv_current1",
			LockVersion:        7,
			AuthorizationEpoch: 5,
			Lifecycle:          LifecycleActive,
			Evidence: RecordAuthorizationEvidence{
				ProjectID:  recordauth.ProjectIDDefault,
				Visibility: input.VisibilityScope(),
				Sources:    []recordauth.SourceAuthorization{input.Subjects()[0].CaptureAuthorization},
			},
		},
	}
	store := &recordReadStoreStub{
		steps: &steps,
		revision: StoredRecordRevision{
			RecordID:           "rec_read1",
			RevisionID:         "rrv_current1",
			RevisionNo:         4,
			LockVersion:        7,
			AuthorizationEpoch: 5,
			Lifecycle:          LifecycleActive,
			Input:              input,
			CreatedAt:          time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC),
			RecordCreatedAt:    time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC),
			RecordUpdatedAt:    time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC),
		},
	}
	service, err := NewRecordReadService(current, &recordRevisionAuthorizationSourceStub{}, store)
	if err != nil {
		t.Fatalf("NewRecordReadService() error = %v", err)
	}

	got, err := service.GetRecord(context.Background(), RecordGetRequest{Actor: actor, RecordID: "rec_read1"})
	if err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	if got.RecordID != "rec_read1" || got.Current.RevisionID != "rrv_current1" || !got.Capabilities.Update {
		t.Fatalf("GetRecord() = %#v", got)
	}
	if want := []string{"current", "read:rrv_current1"}; !equalRecordReadSteps(steps, want) {
		t.Fatalf("steps = %#v, want %#v", steps, want)
	}
	if store.readRequest.LockVersion != 7 || store.readRequest.AuthorizationEpoch != 5 {
		t.Fatalf("read request = %#v", store.readRequest)
	}

	denied := actor
	denied.Role = recordauth.RoleViewer
	visibility := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{"rag_secret"})
	current.current.Evidence.Visibility = visibility
	current.current.Evidence.Sources = []recordauth.SourceAuthorization{mustLiveAuthorization(
		t,
		recordauth.SourceKindVPS,
		testRecordVPSID,
		visibility,
		visibility,
	)}
	store.calls = 0
	if _, err := service.GetRecord(context.Background(), RecordGetRequest{Actor: denied, RecordID: "rec_read1"}); !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("GetRecord(denied) error = %v, want ErrDenied", err)
	}
	if store.calls != 0 {
		t.Fatalf("GetRecord(denied) content reads = %d, want 0", store.calls)
	}
}

func TestRecordReadServiceReauthorizesHistoricalRevisionBeforeLoadingIt(t *testing.T) {
	actor := mustAuthorizationActor(t, recordauth.RoleProjectAdmin, testRecordGroupID)
	input := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	steps := make([]string, 0, 2)
	authorizations := &recordRevisionAuthorizationSourceStub{
		steps: &steps,
		result: RecordRevisionAuthorization{
			RecordID:           "rec_history1",
			RevisionID:         "rrv_history1",
			CurrentRevisionID:  "rrv_current1",
			LockVersion:        9,
			AuthorizationEpoch: 6,
			Lifecycle:          LifecycleActive,
			Evidence: RecordAuthorizationEvidence{
				ProjectID:  recordauth.ProjectIDDefault,
				Visibility: input.VisibilityScope(),
				Sources:    []recordauth.SourceAuthorization{input.Subjects()[0].CaptureAuthorization},
			},
		},
	}
	store := &recordReadStoreStub{
		steps: &steps,
		revision: StoredRecordRevision{
			RecordID:           "rec_history1",
			RevisionID:         "rrv_history1",
			RevisionNo:         2,
			LockVersion:        9,
			AuthorizationEpoch: 6,
			Lifecycle:          LifecycleActive,
			Input:              input,
			CreatedAt:          time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC),
			RecordCreatedAt:    time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC),
			RecordUpdatedAt:    time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC),
		},
	}
	current := &currentRecordAuthorizationSourceStub{
		steps: &steps,
		current: CurrentRecordAuthorization{
			RecordID:           "rec_history1",
			CurrentRevisionID:  "rrv_current1",
			LockVersion:        9,
			AuthorizationEpoch: 6,
			Lifecycle:          LifecycleActive,
			Evidence: RecordAuthorizationEvidence{
				ProjectID:  recordauth.ProjectIDDefault,
				Visibility: input.VisibilityScope(),
				Sources:    []recordauth.SourceAuthorization{input.Subjects()[0].CaptureAuthorization},
			},
		},
	}
	service, err := NewRecordReadService(current, authorizations, store)
	if err != nil {
		t.Fatalf("NewRecordReadService() error = %v", err)
	}

	got, err := service.GetRevision(context.Background(), RecordRevisionGetRequest{
		Actor:      actor,
		RecordID:   "rec_history1",
		RevisionID: "rrv_history1",
	})
	if err != nil {
		t.Fatalf("GetRevision() error = %v", err)
	}
	if got.RevisionID != "rrv_history1" || got.RevisionNo != 2 {
		t.Fatalf("GetRevision() = %#v", got)
	}
	if want := []string{"current", "authorize:rrv_history1", "read:rrv_history1"}; !equalRecordReadSteps(steps, want) {
		t.Fatalf("steps = %#v, want %#v", steps, want)
	}
	if store.readRequest.CurrentRevisionID != "rrv_current1" || store.readRequest.LockVersion != 9 || store.readRequest.AuthorizationEpoch != 6 {
		t.Fatalf("historical read CAS = %#v", store.readRequest)
	}
}

func TestRecordReadServiceRejectsHistoricalRevisionOutsideCurrentScopeBeforeLoadingIt(t *testing.T) {
	actor := mustAuthorizationActor(t, recordauth.RoleViewer, testRecordGroupID)
	input := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	deniedVisibility := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{"rag_other"})
	steps := make([]string, 0, 3)
	current := &currentRecordAuthorizationSourceStub{
		steps: &steps,
		current: CurrentRecordAuthorization{
			RecordID:           "rec_history1",
			CurrentRevisionID:  "rrv_current1",
			LockVersion:        9,
			AuthorizationEpoch: 6,
			Lifecycle:          LifecycleActive,
			Evidence: RecordAuthorizationEvidence{
				ProjectID:  recordauth.ProjectIDDefault,
				Visibility: deniedVisibility,
				Sources: []recordauth.SourceAuthorization{mustLiveAuthorization(
					t,
					recordauth.SourceKindVPS,
					testRecordVPSID,
					deniedVisibility,
					deniedVisibility,
				)},
			},
		},
	}
	historical := &recordRevisionAuthorizationSourceStub{
		steps: &steps,
		result: RecordRevisionAuthorization{
			RecordID:           "rec_history1",
			RevisionID:         "rrv_history1",
			CurrentRevisionID:  "rrv_current1",
			LockVersion:        9,
			AuthorizationEpoch: 6,
			Lifecycle:          LifecycleActive,
			Evidence: RecordAuthorizationEvidence{
				ProjectID:  recordauth.ProjectIDDefault,
				Visibility: input.VisibilityScope(),
				Sources:    []recordauth.SourceAuthorization{input.Subjects()[0].CaptureAuthorization},
			},
		},
	}
	store := &recordReadStoreStub{}
	service, err := NewRecordReadService(current, historical, store)
	if err != nil {
		t.Fatalf("NewRecordReadService() error = %v", err)
	}

	_, err = service.GetRevision(context.Background(), RecordRevisionGetRequest{
		Actor:      actor,
		RecordID:   "rec_history1",
		RevisionID: "rrv_history1",
	})
	if !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("GetRevision() error = %v, want ErrDenied", err)
	}
	if want := []string{"current"}; !equalRecordReadSteps(steps, want) {
		t.Fatalf("steps = %#v, want %#v", steps, want)
	}
	if historical.calls != 0 || store.calls != 0 {
		t.Fatalf("historical authorization/content calls = %d/%d, want 0/0", historical.calls, store.calls)
	}
}

func TestRecordReadServiceRejectsHistoricalAuthorizationTupleMismatchBeforeLoadingContent(t *testing.T) {
	actor := mustAuthorizationActor(t, recordauth.RoleProjectAdmin, testRecordGroupID)
	input := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	evidence := RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: input.VisibilityScope(),
		Sources:    []recordauth.SourceAuthorization{input.Subjects()[0].CaptureAuthorization},
	}
	current := CurrentRecordAuthorization{
		RecordID:           "rec_history1",
		CurrentRevisionID:  "rrv_current1",
		LockVersion:        9,
		AuthorizationEpoch: 6,
		Lifecycle:          LifecycleActive,
		Evidence:           evidence,
	}
	baseHistorical := RecordRevisionAuthorization{
		RecordID:           "rec_history1",
		RevisionID:         "rrv_history1",
		CurrentRevisionID:  "rrv_current1",
		LockVersion:        9,
		AuthorizationEpoch: 6,
		Lifecycle:          LifecycleActive,
		Evidence:           evidence,
	}

	tests := map[string]func(*RecordRevisionAuthorization){
		"current revision": func(value *RecordRevisionAuthorization) { value.CurrentRevisionID = "rrv_current2" },
		"lock version":     func(value *RecordRevisionAuthorization) { value.LockVersion++ },
		"authorization epoch": func(value *RecordRevisionAuthorization) {
			value.AuthorizationEpoch++
		},
		"lifecycle": func(value *RecordRevisionAuthorization) { value.Lifecycle = LifecycleArchived },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			historical := baseHistorical
			mutate(&historical)
			authorizations := &recordRevisionAuthorizationSourceStub{result: historical}
			store := &recordReadStoreStub{}
			service, err := NewRecordReadService(
				&currentRecordAuthorizationSourceStub{current: current},
				authorizations,
				store,
			)
			if err != nil {
				t.Fatalf("NewRecordReadService() error = %v", err)
			}

			_, err = service.GetRevision(context.Background(), RecordRevisionGetRequest{
				Actor: actor, RecordID: "rec_history1", RevisionID: "rrv_history1",
			})
			if !errors.Is(err, ErrRecordRevisionConflict) {
				t.Fatalf("GetRevision() error = %v, want ErrRecordRevisionConflict", err)
			}
			if authorizations.calls != 1 || store.calls != 0 {
				t.Fatalf("historical authorization/content calls = %d/%d, want 1/0", authorizations.calls, store.calls)
			}
		})
	}
}

func TestRecordLifecycleServiceUsesCurrentAuthorizationAndCAS(t *testing.T) {
	actor := mustAuthorizationActor(t, recordauth.RoleProjectAdmin, testRecordGroupID)
	input := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	current := &currentRecordAuthorizationSourceStub{current: CurrentRecordAuthorization{
		RecordID:           "rec_lifecycle1",
		CurrentRevisionID:  "rrv_current1",
		LockVersion:        7,
		AuthorizationEpoch: 5,
		Lifecycle:          LifecycleActive,
		Evidence: RecordAuthorizationEvidence{
			ProjectID:  recordauth.ProjectIDDefault,
			Visibility: input.VisibilityScope(),
			Sources:    []recordauth.SourceAuthorization{input.Subjects()[0].CaptureAuthorization},
		},
	}}
	store := &recordLifecycleStoreStub{result: RecordLifecycleResult{
		RecordID:           "rec_lifecycle1",
		CurrentRevisionID:  "rrv_current1",
		LockVersion:        8,
		AuthorizationEpoch: 6,
		Lifecycle:          LifecycleArchived,
		ChangedAt:          time.Date(2026, time.August, 3, 11, 0, 0, 0, time.UTC),
	}}
	service, err := NewRecordLifecycleService(current, store)
	if err != nil {
		t.Fatalf("NewRecordLifecycleService() error = %v", err)
	}

	got, err := service.ChangeLifecycle(context.Background(), RecordLifecycleRequest{
		Actor:              actor,
		RecordID:           "rec_lifecycle1",
		TargetLifecycle:    LifecycleArchived,
		IdempotencyKey:     "archive-1",
		IdempotencyOwnerID: "records_api_01",
		OwnerLeaseDuration: time.Minute,
		IdempotencyTTL:     24 * time.Hour,
		OutboxTTL:          24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ChangeLifecycle() error = %v", err)
	}
	if got.Lifecycle != LifecycleArchived || store.calls != 1 {
		t.Fatalf("ChangeLifecycle() = %#v calls=%d", got, store.calls)
	}
	if store.command.CurrentRevisionID != "rrv_current1" || store.command.LockVersion != 7 ||
		store.command.AuthorizationEpoch != 5 || store.command.ActorID != actor.UserID ||
		store.command.Idempotency.Key.Key != "archive-1" || store.command.Idempotency.RequestFingerprint.Validate() != nil {
		t.Fatalf("lifecycle command = %#v", store.command)
	}

	current.current.Lifecycle = LifecycleArchived
	if _, err := service.ChangeLifecycle(context.Background(), RecordLifecycleRequest{
		Actor:              actor,
		RecordID:           "rec_lifecycle1",
		TargetLifecycle:    LifecycleArchived,
		IdempotencyKey:     "archive-2",
		IdempotencyOwnerID: "records_api_01",
		OwnerLeaseDuration: time.Minute,
		IdempotencyTTL:     24 * time.Hour,
		OutboxTTL:          24 * time.Hour,
	}); !errors.Is(err, ErrRecordRevisionConflict) {
		t.Fatalf("ChangeLifecycle(same state) error = %v, want ErrRecordRevisionConflict", err)
	}
}

func TestRecordReadServiceListSkipsDeniedCandidatesBeforeContentRead(t *testing.T) {
	actor := mustAuthorizationActor(t, recordauth.RoleViewer, testRecordGroupID)
	allowedInput := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	deniedVisibility := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{"rag_other"})
	deniedEvidence := RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: deniedVisibility,
		Sources: []recordauth.SourceAuthorization{mustLiveAuthorization(
			t,
			recordauth.SourceKindVPS,
			testRecordVPSID,
			deniedVisibility,
			deniedVisibility,
		)},
	}
	allowedEvidence := RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: allowedInput.VisibilityScope(),
		Sources:    []recordauth.SourceAuthorization{allowedInput.Subjects()[0].CaptureAuthorization},
	}
	current := &recordListCurrentSourceStub{values: map[string]CurrentRecordAuthorization{
		"rec_denied1": {
			RecordID:           "rec_denied1",
			CurrentRevisionID:  "rrv_denied1",
			LockVersion:        2,
			AuthorizationEpoch: 2,
			Lifecycle:          LifecycleActive,
			Evidence:           deniedEvidence,
		},
		"rec_allowed1": {
			RecordID:           "rec_allowed1",
			CurrentRevisionID:  "rrv_allowed1",
			LockVersion:        3,
			AuthorizationEpoch: 3,
			Lifecycle:          LifecycleActive,
			Evidence:           allowedEvidence,
		},
	}}
	updatedAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	store := &recordReadStoreStub{
		candidates: []RecordCandidate{
			{RecordID: "rec_denied1", UpdatedAt: updatedAt},
			{RecordID: "rec_allowed1", UpdatedAt: updatedAt.Add(-time.Minute)},
		},
		revisions: map[string]StoredRecordRevision{
			"rrv_allowed1": {
				RecordID:           "rec_allowed1",
				RevisionID:         "rrv_allowed1",
				RevisionNo:         1,
				LockVersion:        3,
				AuthorizationEpoch: 3,
				Lifecycle:          LifecycleActive,
				Input:              allowedInput,
				CreatedAt:          updatedAt.Add(-time.Hour),
				RecordCreatedAt:    updatedAt.Add(-time.Hour),
				RecordUpdatedAt:    updatedAt.Add(-time.Minute),
			},
		},
	}
	service, err := NewRecordReadService(current, &recordRevisionAuthorizationSourceStub{}, store)
	if err != nil {
		t.Fatalf("NewRecordReadService() error = %v", err)
	}

	got, err := service.ListRecords(context.Background(), RecordListRequest{
		Actor: actor,
		Sort:  RecordSortUpdatedDesc,
		Limit: 20,
		Query: "packet loss",
	})
	if err != nil {
		t.Fatalf("ListRecords() error = %v", err)
	}
	if len(got.Records) != 1 || got.Records[0].RecordID != "rec_allowed1" {
		t.Fatalf("ListRecords() = %#v", got)
	}
	if store.readIDs["rrv_denied1"] != 0 || store.readIDs["rrv_allowed1"] != 1 {
		t.Fatalf("content reads = %#v", store.readIDs)
	}
}

func TestRecordReadServiceListContinuesPastDeniedCandidateBatches(t *testing.T) {
	actor := mustAuthorizationActor(t, recordauth.RoleViewer, testRecordGroupID)
	allowedInput := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	deniedVisibility := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{"rag_other"})
	deniedEvidence := RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: deniedVisibility,
		Sources: []recordauth.SourceAuthorization{mustLiveAuthorization(
			t,
			recordauth.SourceKindVPS,
			testRecordVPSID,
			deniedVisibility,
			deniedVisibility,
		)},
	}
	allowedEvidence := RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: allowedInput.VisibilityScope(),
		Sources:    []recordauth.SourceAuthorization{allowedInput.Subjects()[0].CaptureAuthorization},
	}

	const deniedCount = 2000
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	candidates := make([]RecordCandidate, 0, deniedCount+1)
	currentValues := make(map[string]CurrentRecordAuthorization, deniedCount+1)
	for index := 0; index < deniedCount; index++ {
		recordID := fmt.Sprintf("rec_denied%04d", index)
		revisionID := fmt.Sprintf("rrv_denied%04d", index)
		candidates = append(candidates, RecordCandidate{
			RecordID: recordID, UpdatedAt: now.Add(-time.Duration(index) * time.Second),
		})
		currentValues[recordID] = CurrentRecordAuthorization{
			RecordID: recordID, CurrentRevisionID: revisionID,
			LockVersion: 2, AuthorizationEpoch: 2, Lifecycle: LifecycleActive,
			Evidence: deniedEvidence,
		}
	}
	const allowedRecordID = "rec_allowedafterlimit"
	const allowedRevisionID = "rrv_allowedafterlimit"
	allowedUpdatedAt := now.Add(-deniedCount * time.Second)
	candidates = append(candidates, RecordCandidate{RecordID: allowedRecordID, UpdatedAt: allowedUpdatedAt})
	currentValues[allowedRecordID] = CurrentRecordAuthorization{
		RecordID: allowedRecordID, CurrentRevisionID: allowedRevisionID,
		LockVersion: 3, AuthorizationEpoch: 3, Lifecycle: LifecycleActive,
		Evidence: allowedEvidence,
	}
	store := &recordReadStoreStub{
		candidates: candidates,
		revisions: map[string]StoredRecordRevision{
			allowedRevisionID: {
				RecordID: allowedRecordID, RevisionID: allowedRevisionID, RevisionNo: 1,
				LockVersion: 3, AuthorizationEpoch: 3, Lifecycle: LifecycleActive,
				Input: allowedInput, CreatedAt: allowedUpdatedAt,
				RecordCreatedAt: allowedUpdatedAt, RecordUpdatedAt: allowedUpdatedAt,
			},
		},
	}
	service, err := NewRecordReadService(
		&recordListCurrentSourceStub{values: currentValues},
		&recordRevisionAuthorizationSourceStub{},
		store,
	)
	if err != nil {
		t.Fatalf("NewRecordReadService() error = %v", err)
	}

	got, err := service.ListRecords(context.Background(), RecordListRequest{
		Actor: actor, Sort: RecordSortUpdatedDesc, Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListRecords() error = %v", err)
	}
	if len(got.Records) != 1 || got.Records[0].RecordID != allowedRecordID || got.NextCursor != nil {
		t.Fatalf("ListRecords() = %#v", got)
	}
	if store.readIDs[allowedRevisionID] != 1 {
		t.Fatalf("allowed content reads = %d, want 1", store.readIDs[allowedRevisionID])
	}
}

func TestRecordReadServiceListRevisionsReauthorizesEachHistoricalRevision(t *testing.T) {
	actor := mustAuthorizationActor(t, recordauth.RoleViewer, testRecordGroupID)
	input := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	allowedEvidence := RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: input.VisibilityScope(),
		Sources:    []recordauth.SourceAuthorization{input.Subjects()[0].CaptureAuthorization},
	}
	deniedVisibility := mustAuthorizationVisibility(t, recordauth.VisibilityKindRestricted, []string{"rag_other"})
	deniedEvidence := RecordAuthorizationEvidence{
		ProjectID:  recordauth.ProjectIDDefault,
		Visibility: deniedVisibility,
		Sources: []recordauth.SourceAuthorization{mustLiveAuthorization(
			t,
			recordauth.SourceKindVPS,
			testRecordVPSID,
			deniedVisibility,
			deniedVisibility,
		)},
	}
	current := &currentRecordAuthorizationSourceStub{current: CurrentRecordAuthorization{
		RecordID:           "rec_history1",
		CurrentRevisionID:  "rrv_current1",
		LockVersion:        9,
		AuthorizationEpoch: 6,
		Lifecycle:          LifecycleActive,
		Evidence:           allowedEvidence,
	}}
	authorizations := &recordRevisionAuthorizationSourceStub{values: map[string]RecordRevisionAuthorization{
		"rrv_current1": {
			RecordID:           "rec_history1",
			RevisionID:         "rrv_current1",
			CurrentRevisionID:  "rrv_current1",
			LockVersion:        9,
			AuthorizationEpoch: 6,
			Lifecycle:          LifecycleActive,
			Evidence:           allowedEvidence,
		},
		"rrv_private1": {
			RecordID:           "rec_history1",
			RevisionID:         "rrv_private1",
			CurrentRevisionID:  "rrv_current1",
			LockVersion:        9,
			AuthorizationEpoch: 6,
			Lifecycle:          LifecycleActive,
			Evidence:           deniedEvidence,
		},
	}}
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	store := &recordReadStoreStub{
		revisionCandidates: []RecordRevisionCandidate{
			{RevisionID: "rrv_current1", RevisionNo: 2},
			{RevisionID: "rrv_private1", RevisionNo: 1},
		},
		revisions: map[string]StoredRecordRevision{
			"rrv_current1": {
				RecordID:           "rec_history1",
				RevisionID:         "rrv_current1",
				RevisionNo:         2,
				LockVersion:        9,
				AuthorizationEpoch: 6,
				Lifecycle:          LifecycleActive,
				Input:              input,
				CreatedAt:          now,
				RecordCreatedAt:    now.Add(-time.Hour),
				RecordUpdatedAt:    now,
			},
		},
	}
	service, err := NewRecordReadService(current, authorizations, store)
	if err != nil {
		t.Fatalf("NewRecordReadService() error = %v", err)
	}

	got, err := service.ListRevisions(context.Background(), RecordRevisionListRequest{
		Actor:    actor,
		RecordID: "rec_history1",
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("ListRevisions() error = %v", err)
	}
	if len(got) != 1 || got[0].RevisionID != "rrv_current1" || store.readIDs["rrv_private1"] != 0 {
		t.Fatalf("ListRevisions() = %#v reads=%#v", got, store.readIDs)
	}
}

type recordReadStoreStub struct {
	steps              *[]string
	revision           StoredRecordRevision
	revisions          map[string]StoredRecordRevision
	candidates         []RecordCandidate
	revisionCandidates []RecordRevisionCandidate
	readRequest        StoredRecordRevisionRequest
	readIDs            map[string]int
	calls              int
}

func (store *recordReadStoreStub) ListRecordCandidates(_ context.Context, page RecordCandidatePage) ([]RecordCandidate, error) {
	start := 0
	if page.After != nil {
		start = len(store.candidates)
		for index, candidate := range store.candidates {
			if candidate.RecordID == page.After.RecordID && candidate.UpdatedAt.Equal(page.After.UpdatedAt) {
				start = index + 1
				break
			}
		}
	}
	end := start + int(page.Limit)
	if end > len(store.candidates) {
		end = len(store.candidates)
	}
	return append([]RecordCandidate(nil), store.candidates[start:end]...), nil
}

func (store *recordReadStoreStub) ListRevisionCandidates(context.Context, RecordRevisionCandidatePage) ([]RecordRevisionCandidate, error) {
	return append([]RecordRevisionCandidate(nil), store.revisionCandidates...), nil
}

func (store *recordReadStoreStub) ReadRecordRevision(_ context.Context, request StoredRecordRevisionRequest) (StoredRecordRevision, error) {
	store.calls++
	store.readRequest = request
	if store.readIDs == nil {
		store.readIDs = make(map[string]int)
	}
	store.readIDs[request.RevisionID]++
	if store.steps != nil {
		*store.steps = append(*store.steps, "read:"+request.RevisionID)
	}
	if revision, ok := store.revisions[request.RevisionID]; ok {
		return revision, nil
	}
	return store.revision, nil
}

type recordListCurrentSourceStub struct {
	values map[string]CurrentRecordAuthorization
}

func (source *recordListCurrentSourceStub) ResolveCurrentRecordAuthorization(
	_ context.Context,
	_ recordauth.ActorScope,
	recordID string,
) (CurrentRecordAuthorization, error) {
	value, ok := source.values[recordID]
	if !ok {
		return CurrentRecordAuthorization{}, ErrRecordNotFound
	}
	return value, nil
}

type recordRevisionAuthorizationSourceStub struct {
	steps  *[]string
	result RecordRevisionAuthorization
	values map[string]RecordRevisionAuthorization
	err    error
	calls  int
}

func (source *recordRevisionAuthorizationSourceStub) ResolveRecordRevisionAuthorization(
	_ context.Context,
	_ recordauth.ActorScope,
	_ string,
	revisionID string,
) (RecordRevisionAuthorization, error) {
	source.calls++
	if source.steps != nil {
		*source.steps = append(*source.steps, "authorize:"+revisionID)
	}
	if value, ok := source.values[revisionID]; ok {
		return value, source.err
	}
	return source.result, source.err
}

type recordLifecycleStoreStub struct {
	command RecordLifecycleCommand
	result  RecordLifecycleResult
	err     error
	calls   int
}

func (store *recordLifecycleStoreStub) CommitRecordLifecycle(_ context.Context, command RecordLifecycleCommand) (RecordLifecycleResult, error) {
	store.calls++
	store.command = command
	return store.result, store.err
}

func equalRecordReadSteps(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

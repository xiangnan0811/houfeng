package records

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

func TestRevisionServiceResolvesServerSubjectsAuthorizesAndCommitsCreate(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	request, resolved := testRevisionServiceRequest(t, actor, DomainActivityRecordCreated)
	steps := make([]string, 0, 4)
	adapter := &revisionServiceSubjectAdapter{kind: SubjectKindVPS, resolved: resolved, steps: &steps}
	registry, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{adapter})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	current := &currentRecordAuthorizationSourceStub{steps: &steps}
	store := &revisionCommitStoreStub{
		steps:  &steps,
		result: RevisionCommitResult{RecordID: request.RecordID, RevisionID: "rrv_created00000001", RevisionNo: 1, Created: true},
	}
	service, err := NewRevisionService(registry, current, store)
	if err != nil {
		t.Fatalf("NewRevisionService() error = %v", err)
	}

	result, err := service.SaveRevision(context.Background(), request)
	if err != nil {
		t.Fatalf("SaveRevision() error = %v", err)
	}
	if result != store.result {
		t.Fatalf("SaveRevision() = %#v, want store result %#v", result, store.result)
	}
	if !reflect.DeepEqual(steps, []string{"subject:vps", "commit"}) {
		t.Fatalf("service steps = %#v, want source resolution before transaction commit", steps)
	}
	if current.calls != 0 {
		t.Fatalf("current authorization calls = %d, want zero for create", current.calls)
	}
	if store.calls != 1 || store.command.Validate() != nil {
		t.Fatalf("committed command = %#v, want one valid command", store.command)
	}
	if store.command.Idempotency.Key.OperationKind != recordplatform.OperationKindRecordCreate ||
		store.command.ActivityKind != DomainActivityRecordCreated || store.command.Input.AuthorID() != actor.UserID {
		t.Fatalf("committed command identity = %#v, want trusted create actor", store.command)
	}
	subjects := store.command.Input.Subjects()
	if len(subjects) != 1 || subjects[0].SourceID != request.SubjectReferences[0].SourceID ||
		subjects[0].IdentitySnapshot["display_name"] != "Server VPS" ||
		subjects[0].CaptureAuthorization.Digest != resolved.CaptureAuthorization.Digest {
		t.Fatalf("committed subjects = %#v, want server-resolved immutable evidence", subjects)
	}
	if adapter.actor.CanonicalHash() != actor.CanonicalHash() || adapter.reference != request.SubjectReferences[0] {
		t.Fatalf("adapter input = (%#v, %#v), want trusted actor and normalized reference", adapter.actor, adapter.reference)
	}
}

func TestRevisionServiceBindsPublishedDraftToCommandAndRequestFingerprint(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	request, resolved := testRevisionServiceRequest(t, actor, DomainActivityRecordCreated)
	payload, err := NewDraftPayload([]byte(`{"title":"Publish"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	firstETag, err := NewDraftETag("rdf_0123456789abcdef", actor.UserID, 4, payload)
	if err != nil {
		t.Fatalf("NewDraftETag(first) error = %v", err)
	}
	secondETag, err := NewDraftETag("rdf_0123456789abcdef", actor.UserID, 5, payload)
	if err != nil {
		t.Fatalf("NewDraftETag(second) error = %v", err)
	}
	request.DraftID = "rdf_0123456789abcdef"
	request.DraftETag = firstETag

	adapter := &revisionServiceSubjectAdapter{kind: SubjectKindVPS, resolved: resolved}
	registry, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{adapter})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	store := &revisionCommitStoreStub{result: RevisionCommitResult{RecordID: request.RecordID, RevisionNo: 1, Created: true}}
	service, err := NewRevisionService(registry, &currentRecordAuthorizationSourceStub{}, store)
	if err != nil {
		t.Fatalf("NewRevisionService() error = %v", err)
	}

	if _, err := service.SaveRevision(context.Background(), request); err != nil {
		t.Fatalf("SaveRevision(first) error = %v", err)
	}
	firstCommand := store.command
	if firstCommand.DraftID != request.DraftID || firstCommand.DraftETag != firstETag {
		t.Fatalf("SaveRevision(first) draft binding = (%q, %q)", firstCommand.DraftID, firstCommand.DraftETag.String())
	}
	request.DraftETag = secondETag
	if _, err := service.SaveRevision(context.Background(), request); err != nil {
		t.Fatalf("SaveRevision(second) error = %v", err)
	}
	firstFingerprint, err := firstCommand.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes(first) error = %v", err)
	}
	secondFingerprint, err := store.command.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes(second) error = %v", err)
	}
	if firstFingerprint == secondFingerprint {
		t.Fatal("published draft ETag did not change request fingerprint")
	}

	invalid := []RevisionSaveRequest{request, request}
	invalid[0].DraftETag = DraftETag{}
	invalid[1].DraftID = ""
	for _, candidate := range invalid {
		callsBefore := adapter.calls
		storeCallsBefore := store.calls
		if _, err := service.SaveRevision(context.Background(), candidate); !errors.Is(err, ErrInvalidRevisionServiceRequest) {
			t.Fatalf("SaveRevision(invalid draft pair) error = %v, want ErrInvalidRevisionServiceRequest", err)
		}
		if adapter.calls != callsBefore || store.calls != storeCallsBefore {
			t.Fatalf("invalid draft pair reached adapter/store: adapter=%d store=%d", adapter.calls-callsBefore, store.calls-storeCallsBefore)
		}
	}
}

func TestRevisionServiceAuthorizesCurrentBeforeResolvingProposedUpdate(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	request, resolved := testRevisionServiceRequest(t, actor, DomainActivityRecordRevised)
	steps := make([]string, 0, 4)
	adapter := &revisionServiceSubjectAdapter{kind: SubjectKindVPS, resolved: resolved, steps: &steps}
	registry, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{adapter})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	current := &currentRecordAuthorizationSourceStub{
		steps: &steps,
		current: CurrentRecordAuthorization{
			RecordID:           request.RecordID,
			CurrentRevisionID:  request.BaseRevisionID,
			LockVersion:        request.LockVersion,
			AuthorizationEpoch: request.AuthorizationEpoch,
			Lifecycle:          LifecycleActive,
			Evidence: RecordAuthorizationEvidence{
				ProjectID:  recordauth.ProjectIDDefault,
				Visibility: request.Values.VisibilityScope,
				Sources:    []recordauth.SourceAuthorization{resolved.CaptureAuthorization},
			},
		},
	}
	store := &revisionCommitStoreStub{steps: &steps, result: RevisionCommitResult{RecordID: request.RecordID, RevisionNo: 5, Created: true}}
	service, err := NewRevisionService(registry, current, store)
	if err != nil {
		t.Fatalf("NewRevisionService() error = %v", err)
	}

	if _, err := service.SaveRevision(context.Background(), request); err != nil {
		t.Fatalf("SaveRevision() error = %v", err)
	}
	if !reflect.DeepEqual(steps, []string{"current", "subject:vps", "commit"}) {
		t.Fatalf("service steps = %#v, want current authorization before proposed resolution and commit", steps)
	}
	if store.command.Idempotency.Key.OperationKind != recordplatform.OperationKindRecordUpdate ||
		store.command.BaseRevisionID != request.BaseRevisionID || store.command.LockVersion != request.LockVersion ||
		store.command.AuthorizationEpoch != request.AuthorizationEpoch {
		t.Fatalf("committed update CAS = %#v, want request base and versions", store.command)
	}
}

func TestRevisionServiceRejectsCurrentDenialOrConflictBeforeProposedResolution(t *testing.T) {
	t.Parallel()

	admin := mustRecordActor(t)
	viewer := mustAuthorizationActor(t, recordauth.RoleViewer)
	tests := []struct {
		name    string
		actor   recordauth.ActorScope
		mutate  func(*CurrentRecordAuthorization)
		wantErr error
	}{
		{name: "viewer cannot update", actor: viewer, wantErr: recordauth.ErrDenied},
		{name: "base revision advanced", actor: admin, mutate: func(current *CurrentRecordAuthorization) { current.CurrentRevisionID = "rrv_advanced0000001" }, wantErr: ErrRecordRevisionConflict},
		{name: "lock advanced", actor: admin, mutate: func(current *CurrentRecordAuthorization) { current.LockVersion++ }, wantErr: ErrRecordRevisionConflict},
		{name: "authorization advanced", actor: admin, mutate: func(current *CurrentRecordAuthorization) { current.AuthorizationEpoch++ }, wantErr: ErrRecordRevisionConflict},
		{name: "record archived", actor: admin, mutate: func(current *CurrentRecordAuthorization) { current.Lifecycle = LifecycleArchived }, wantErr: ErrRecordRevisionConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request, resolved := testRevisionServiceRequest(t, test.actor, DomainActivityRecordRevised)
			steps := make([]string, 0, 2)
			adapter := &revisionServiceSubjectAdapter{kind: SubjectKindVPS, resolved: resolved, steps: &steps}
			registry, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{adapter})
			if err != nil {
				t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
			}
			state := CurrentRecordAuthorization{
				RecordID:           request.RecordID,
				CurrentRevisionID:  request.BaseRevisionID,
				LockVersion:        request.LockVersion,
				AuthorizationEpoch: request.AuthorizationEpoch,
				Lifecycle:          LifecycleActive,
				Evidence: RecordAuthorizationEvidence{
					ProjectID:  recordauth.ProjectIDDefault,
					Visibility: request.Values.VisibilityScope,
					Sources:    []recordauth.SourceAuthorization{resolved.CaptureAuthorization},
				},
			}
			if test.mutate != nil {
				test.mutate(&state)
			}
			current := &currentRecordAuthorizationSourceStub{steps: &steps, current: state}
			store := &revisionCommitStoreStub{steps: &steps}
			service, err := NewRevisionService(registry, current, store)
			if err != nil {
				t.Fatalf("NewRevisionService() error = %v", err)
			}

			result, err := service.SaveRevision(context.Background(), request)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("SaveRevision() error = %v, want %v", err, test.wantErr)
			}
			if result != (RevisionCommitResult{}) || adapter.calls != 0 || store.calls != 0 {
				t.Fatalf("denied/conflicting update leaked into proposed resolution or commit: result=%#v adapter=%d store=%d", result, adapter.calls, store.calls)
			}
			if !reflect.DeepEqual(steps, []string{"current"}) {
				t.Fatalf("service steps = %#v, want current-only fail closed", steps)
			}
		})
	}
}

func TestRevisionServiceRejectsClientOwnedSubjectEvidence(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	request, resolved := testRevisionServiceRequest(t, actor, DomainActivityRecordCreated)
	request.Values.Subjects = []RevisionSubject{{
		RegistryVersion:  SubjectRegistryVersionV1,
		Kind:             SubjectKindVPS,
		Role:             RelationRoleAffected,
		SourceID:         testRecordVPSID,
		Primary:          true,
		IdentitySnapshot: map[string]string{"display_name": "Client spoof"},
	}}
	adapter := &revisionServiceSubjectAdapter{kind: SubjectKindVPS, resolved: resolved}
	registry, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{adapter})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	store := &revisionCommitStoreStub{}
	service, err := NewRevisionService(registry, &currentRecordAuthorizationSourceStub{}, store)
	if err != nil {
		t.Fatalf("NewRevisionService() error = %v", err)
	}

	if _, err := service.SaveRevision(context.Background(), request); !errors.Is(err, ErrInvalidRevisionServiceRequest) {
		t.Fatalf("SaveRevision() error = %v, want ErrInvalidRevisionServiceRequest", err)
	}
	if adapter.calls != 0 || store.calls != 0 {
		t.Fatalf("client-owned evidence reached adapter/store: adapter=%d store=%d", adapter.calls, store.calls)
	}
}

func testRevisionServiceRequest(
	t *testing.T,
	actor recordauth.ActorScope,
	activityKind DomainActivityKind,
) (RevisionSaveRequest, ResolvedSubject) {
	t.Helper()
	values := validCompleteRevisionValues(t)
	persistedSubject := values.Subjects[0]
	values.Subjects = nil
	values.AuthorID = "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
	snapshot := mustSubjectSnapshot(t, SubjectKindVPS, map[string]string{
		"display_name": "Server VPS",
		"provider":     "Server Cloud",
	})
	resolved := ResolvedSubject{
		ProjectID:            recordauth.ProjectIDDefault,
		StableID:             testRecordVPSID,
		IdentitySnapshot:     snapshot,
		LiveRoute:            "/vps/" + testRecordVPSID,
		CaptureAuthorization: persistedSubject.CaptureAuthorization,
	}
	request := RevisionSaveRequest{
		Actor:    actor,
		RecordID: "rec_service1",
		Values:   values,
		SubjectReferences: []SubjectReference{{
			RegistryVersion: SubjectRegistryVersionV1,
			Kind:            SubjectKindVPS,
			Role:            RelationRoleAffected,
			SourceID:        testRecordVPSID,
			Primary:         true,
		}},
		ActivityKind:       activityKind,
		IdempotencyKey:     "service-save-1",
		IdempotencyOwnerID: "records_api_01",
		OwnerLeaseDuration: time.Minute,
		IdempotencyTTL:     24 * time.Hour,
		OutboxTTL:          24 * time.Hour,
	}
	if activityKind != DomainActivityRecordCreated {
		request.BaseRevisionID = "rrv_current00000004"
		request.LockVersion = 7
		request.AuthorizationEpoch = 5
	}
	return request, resolved
}

type revisionServiceSubjectAdapter struct {
	kind      SubjectKind
	resolved  ResolvedSubject
	err       error
	steps     *[]string
	calls     int
	actor     recordauth.ActorScope
	reference SubjectReference
}

func (adapter *revisionServiceSubjectAdapter) Kind() SubjectKind {
	return adapter.kind
}

func (adapter *revisionServiceSubjectAdapter) Resolve(_ context.Context, actor recordauth.ActorScope, reference SubjectReference) (ResolvedSubject, error) {
	adapter.calls++
	adapter.actor = actor.Clone()
	adapter.reference = reference
	if adapter.steps != nil {
		*adapter.steps = append(*adapter.steps, "subject:"+string(reference.Kind))
	}
	return adapter.resolved, adapter.err
}

type currentRecordAuthorizationSourceStub struct {
	current CurrentRecordAuthorization
	err     error
	steps   *[]string
	calls   int
}

func (source *currentRecordAuthorizationSourceStub) ResolveCurrentRecordAuthorization(
	_ context.Context,
	_ recordauth.ActorScope,
	_ string,
) (CurrentRecordAuthorization, error) {
	source.calls++
	if source.steps != nil {
		*source.steps = append(*source.steps, "current")
	}
	return source.current, source.err
}

type revisionCommitStoreStub struct {
	command RevisionCommitCommand
	result  RevisionCommitResult
	err     error
	steps   *[]string
	calls   int
}

func (store *revisionCommitStoreStub) CommitRevision(_ context.Context, command RevisionCommitCommand) (RevisionCommitResult, error) {
	store.calls++
	store.command = command
	if store.steps != nil {
		*store.steps = append(*store.steps, "commit")
	}
	return store.result, store.err
}

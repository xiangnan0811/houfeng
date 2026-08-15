package records

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestApplicationRestoreRevisionCopiesHistoricalContentThroughFreshSubjectReferences(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	historicalValues := validCompleteRevisionValues(t)
	historicalValues.EvidenceSnapshotIDs = []string{testRecordEvidenceID1, testRecordEvidenceID2}
	historicalInput := mustCompleteRevisionInput(t, historicalValues)
	evidencePreparation := mustRevisionEvidencePreparation(t, actor, "rec_restoreapp", historicalInput.EvidenceSnapshotIDs())
	steps := make([]string, 0, 3)
	read := &recordApplicationReadStub{
		getRevision: func(_ context.Context, request RecordRevisionGetRequest) (RecordRevision, error) {
			steps = append(steps, "historical_read")
			if request.RecordID != "rec_restoreapp" || request.RevisionID != "rrv_historicalapp" ||
				!reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("GetRevision() request = %#v", request)
			}
			return RecordRevision{
				RecordID:   request.RecordID,
				RevisionID: request.RevisionID,
				RevisionNo: 2,
				Input:      historicalInput,
				CreatedAt:  time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC),
			}, nil
		},
		getRecord: func(_ context.Context, request RecordGetRequest) (Record, error) {
			steps = append(steps, "current_read")
			if request.RecordID != "rec_restoreapp" || !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("GetRecord() request = %#v", request)
			}
			return Record{
				RecordID:           request.RecordID,
				ProjectID:          recordauth.ProjectIDDefault,
				Lifecycle:          LifecycleActive,
				CurrentRevisionID:  "rrv_currentapp",
				LockVersion:        7,
				AuthorizationEpoch: 5,
			}, nil
		},
	}
	revisions := &recordApplicationRevisionStub{
		save: func(_ context.Context, request RevisionSaveRequest) (RevisionCommitResult, error) {
			steps = append(steps, "revision_save")
			if request.ActivityKind != DomainActivityRecordRestored || request.RecordID != "rec_restoreapp" ||
				request.BaseRevisionID != "rrv_currentapp" || request.LockVersion != 7 ||
				request.AuthorizationEpoch != 5 || request.IdempotencyKey != "restore-app-key" ||
				request.IdempotencyOwnerID != "records_api" || request.OwnerLeaseDuration != time.Minute ||
				request.IdempotencyTTL != 24*time.Hour || request.OutboxTTL != 24*time.Hour {
				t.Fatalf("SaveRevision() request = %#v", request)
			}
			if len(request.Values.Subjects) != 0 || request.Values.AuthorID != "" ||
				request.Values.SaveReason != "restore historical revision" {
				t.Fatalf("SaveRevision() transport-owned values = %#v", request.Values)
			}
			if !reflect.DeepEqual(request.Values.AttachmentIDs, historicalInput.AttachmentIDs()) {
				t.Fatalf("SaveRevision() attachment IDs = %#v, want %#v", request.Values.AttachmentIDs, historicalInput.AttachmentIDs())
			}
			if len(request.Values.EvidenceSnapshotIDs) != 0 {
				t.Fatalf("SaveRevision() accepted client-owned evidence snapshot IDs = %#v", request.Values.EvidenceSnapshotIDs)
			}
			if !slices.Equal(request.EvidencePreparation.SnapshotIDs(), historicalInput.EvidenceSnapshotIDs()) {
				t.Fatalf("SaveRevision() evidence preparation IDs = %#v, want %#v", request.EvidencePreparation.SnapshotIDs(), historicalInput.EvidenceSnapshotIDs())
			}
			wantReferences := []SubjectReference{{
				RegistryVersion: historicalInput.Subjects()[0].RegistryVersion,
				Kind:            historicalInput.Subjects()[0].Kind,
				Role:            historicalInput.Subjects()[0].Role,
				SourceID:        historicalInput.Subjects()[0].SourceID,
				Primary:         historicalInput.Subjects()[0].Primary,
			}}
			if !reflect.DeepEqual(request.SubjectReferences, wantReferences) {
				t.Fatalf("SaveRevision() subject references = %#v, want %#v", request.SubjectReferences, wantReferences)
			}

			copied := request.Values
			copied.Subjects = historicalInput.Subjects()
			copied.EvidenceSnapshotIDs = request.EvidencePreparation.SnapshotIDs()
			copied.AuthorID = actor.UserID
			input, err := NormalizeCompleteRevisionInput(copied)
			if err != nil {
				t.Fatalf("NormalizeCompleteRevisionInput(copied) error = %v", err)
			}
			if input.CanonicalHash() != historicalInput.CanonicalHash() {
				t.Fatalf("restored canonical hash = %x, want %x", input.CanonicalHash(), historicalInput.CanonicalHash())
			}
			return RevisionCommitResult{
				RecordID:           request.RecordID,
				RevisionID:         "rrv_restoredapp",
				RevisionNo:         4,
				LockVersion:        8,
				AuthorizationEpoch: 6,
				Lifecycle:          LifecycleActive,
				Created:            true,
			}, nil
		},
	}
	application, err := NewApplication(
		read,
		revisions,
		&recordApplicationLifecycleStub{},
		&recordApplicationDraftStub{},
		ApplicationOptions{
			IdempotencyOwnerID: "records_api",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     24 * time.Hour,
			OutboxTTL:          24 * time.Hour,
		},
	)
	if err != nil {
		t.Fatalf("NewApplication() error = %v", err)
	}

	result, err := application.RestoreRevision(context.Background(), RecordRestoreRequest{
		Actor:               actor,
		RecordID:            "rec_restoreapp",
		RevisionID:          "rrv_historicalapp",
		EvidencePreparation: evidencePreparation,
		SaveReason:          "restore historical revision",
		IdempotencyKey:      "restore-app-key",
	})
	if err != nil {
		t.Fatalf("RestoreRevision() error = %v", err)
	}
	if result.RevisionID != "rrv_restoredapp" || !result.Created {
		t.Fatalf("RestoreRevision() = %#v", result)
	}
	if want := []string{"historical_read", "current_read", "revision_save"}; !reflect.DeepEqual(steps, want) {
		t.Fatalf("RestoreRevision() steps = %#v, want %#v", steps, want)
	}

	steps = steps[:0]
	_, err = application.RestoreRevision(context.Background(), RecordRestoreRequest{
		Actor:          actor,
		RecordID:       "rec_restoreapp",
		RevisionID:     "rrv_historicalapp",
		SaveReason:     "restore without prepared evidence",
		IdempotencyKey: "restore-app-missing-evidence",
	})
	if !errors.Is(err, ErrInvalidApplicationRequest) {
		t.Fatalf("RestoreRevision(missing evidence preparation) error = %v, want ErrInvalidApplicationRequest", err)
	}
	if want := []string{"historical_read"}; !reflect.DeepEqual(steps, want) {
		t.Fatalf("RestoreRevision(missing evidence preparation) steps = %#v, want %#v", steps, want)
	}
}

func TestApplicationMutationEntryPointsSetKindsAndInternalOptions(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	values := validCompleteRevisionValues(t)
	references := subjectReferencesForRestore(values.Subjects)
	evidencePreparation := mustRevisionEvidencePreparation(t, actor, "rec_applicationcreate", []string{testRecordEvidenceID1})
	values.Subjects = nil
	values.AuthorID = ""
	payload, err := NewDraftPayload([]byte(`{"title":"application draft"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	draftETag, err := NewDraftETag("rdf_application", actor.UserID, 1, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}

	var revisionRequests []RevisionSaveRequest
	revisions := &recordApplicationRevisionStub{save: func(_ context.Context, request RevisionSaveRequest) (RevisionCommitResult, error) {
		revisionRequests = append(revisionRequests, request)
		return RevisionCommitResult{RecordID: request.RecordID, RevisionID: "rrv_application", Created: true}, nil
	}}
	var lifecycleRequest RecordLifecycleRequest
	lifecycle := &recordApplicationLifecycleStub{change: func(_ context.Context, request RecordLifecycleRequest) (RecordLifecycleResult, error) {
		lifecycleRequest = request
		return RecordLifecycleResult{RecordID: request.RecordID, Lifecycle: request.TargetLifecycle}, nil
	}}
	application, err := NewApplication(
		&recordApplicationReadStub{},
		revisions,
		lifecycle,
		&recordApplicationDraftStub{},
		ApplicationOptions{
			IdempotencyOwnerID: "records_api",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     24 * time.Hour,
			OutboxTTL:          24 * time.Hour,
		},
	)
	if err != nil {
		t.Fatalf("NewApplication() error = %v", err)
	}

	_, err = application.CreateRecord(context.Background(), RecordCreateRequest{
		Actor:               actor,
		RecordID:            "rec_applicationcreate",
		DraftID:             "rdf_application",
		DraftETag:           draftETag,
		Values:              values,
		SubjectReferences:   references,
		EvidencePreparation: evidencePreparation,
		IdempotencyKey:      "application-create",
	})
	if err != nil {
		t.Fatalf("CreateRecord() error = %v", err)
	}
	_, err = application.CreateRevision(context.Background(), RecordRevisionCreateRequest{
		Actor:               actor,
		RecordID:            "rec_applicationcreate",
		BaseRevisionID:      "rrv_applicationbase",
		LockVersion:         7,
		AuthorizationEpoch:  5,
		DraftID:             "rdf_application",
		DraftETag:           draftETag,
		Values:              values,
		SubjectReferences:   references,
		EvidencePreparation: evidencePreparation,
		IdempotencyKey:      "application-revise",
	})
	if err != nil {
		t.Fatalf("CreateRevision() error = %v", err)
	}
	_, err = application.ChangeLifecycle(context.Background(), RecordLifecycleChangeRequest{
		Actor:           actor,
		RecordID:        "rec_applicationcreate",
		TargetLifecycle: LifecycleArchived,
		IdempotencyKey:  "application-archive",
	})
	if err != nil {
		t.Fatalf("ChangeLifecycle() error = %v", err)
	}

	if len(revisionRequests) != 2 {
		t.Fatalf("SaveRevision() request count = %d, want 2", len(revisionRequests))
	}
	create := revisionRequests[0]
	if create.ActivityKind != DomainActivityRecordCreated || create.BaseRevisionID != "" ||
		create.LockVersion != 0 || create.AuthorizationEpoch != 0 {
		t.Fatalf("CreateRecord() SaveRevision request = %#v", create)
	}
	revise := revisionRequests[1]
	if revise.ActivityKind != DomainActivityRecordRevised || revise.BaseRevisionID != "rrv_applicationbase" ||
		revise.LockVersion != 7 || revise.AuthorizationEpoch != 5 {
		t.Fatalf("CreateRevision() SaveRevision request = %#v", revise)
	}
	for _, request := range revisionRequests {
		if request.IdempotencyOwnerID != "records_api" || request.OwnerLeaseDuration != time.Minute ||
			request.IdempotencyTTL != 24*time.Hour || request.OutboxTTL != 24*time.Hour ||
			request.DraftID != "rdf_application" || request.DraftETag != draftETag ||
			!reflect.DeepEqual(request.Actor, actor) || !reflect.DeepEqual(request.Values, values) ||
			!reflect.DeepEqual(request.SubjectReferences, references) ||
			!slices.Equal(request.EvidencePreparation.SnapshotIDs(), evidencePreparation.SnapshotIDs()) {
			t.Fatalf("SaveRevision() request = %#v", request)
		}
	}
	if lifecycleRequest.TargetLifecycle != LifecycleArchived || lifecycleRequest.RecordID != "rec_applicationcreate" ||
		lifecycleRequest.IdempotencyKey != "application-archive" || lifecycleRequest.IdempotencyOwnerID != "records_api" ||
		lifecycleRequest.OwnerLeaseDuration != time.Minute || lifecycleRequest.IdempotencyTTL != 24*time.Hour ||
		lifecycleRequest.OutboxTTL != 24*time.Hour || !reflect.DeepEqual(lifecycleRequest.Actor, actor) {
		t.Fatalf("ChangeLifecycle() request = %#v", lifecycleRequest)
	}
}

func TestApplicationReadEntryPointsForwardExactRequests(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	cursor := &RecordCursor{
		UpdatedAt: time.Date(2026, time.August, 3, 14, 0, 0, 0, time.UTC),
		RecordID:  "rec_applicationread",
	}
	getRequest := RecordGetRequest{Actor: actor, RecordID: "rec_applicationread"}
	listRequest := RecordListRequest{
		Actor:      actor,
		Query:      "database",
		Lifecycle:  LifecycleActive,
		RecordType: RecordTypeTroubleshooting,
		Sort:       RecordSortUpdatedAsc,
		After:      cursor,
		Limit:      25,
	}
	getRevisionRequest := RecordRevisionGetRequest{
		Actor:      actor,
		RecordID:   "rec_applicationread",
		RevisionID: "rrv_applicationread",
	}
	listRevisionsRequest := RecordRevisionListRequest{
		Actor:    actor,
		RecordID: "rec_applicationread",
		Limit:    50,
	}

	read := &recordApplicationReadStub{
		getRecord: func(_ context.Context, request RecordGetRequest) (Record, error) {
			if !reflect.DeepEqual(request, getRequest) {
				t.Fatalf("GetRecord() request = %#v, want %#v", request, getRequest)
			}
			return Record{RecordID: request.RecordID}, nil
		},
		listRecords: func(_ context.Context, request RecordListRequest) (RecordListResult, error) {
			if !reflect.DeepEqual(request, listRequest) {
				t.Fatalf("ListRecords() request = %#v, want %#v", request, listRequest)
			}
			return RecordListResult{Records: []Record{{RecordID: "rec_applicationlisted"}}}, nil
		},
		getRevision: func(_ context.Context, request RecordRevisionGetRequest) (RecordRevision, error) {
			if !reflect.DeepEqual(request, getRevisionRequest) {
				t.Fatalf("GetRevision() request = %#v, want %#v", request, getRevisionRequest)
			}
			return RecordRevision{RecordID: request.RecordID, RevisionID: request.RevisionID}, nil
		},
		listRevisions: func(_ context.Context, request RecordRevisionListRequest) ([]RecordRevision, error) {
			if !reflect.DeepEqual(request, listRevisionsRequest) {
				t.Fatalf("ListRevisions() request = %#v, want %#v", request, listRevisionsRequest)
			}
			return []RecordRevision{{RecordID: request.RecordID, RevisionID: "rrv_applicationlisted"}}, nil
		},
	}
	application := mustRecordApplication(t, read, &recordApplicationDraftStub{})

	record, err := application.GetRecord(context.Background(), getRequest)
	if err != nil || record.RecordID != getRequest.RecordID {
		t.Fatalf("GetRecord() = %#v, %v", record, err)
	}
	listed, err := application.ListRecords(context.Background(), listRequest)
	if err != nil || len(listed.Records) != 1 || listed.Records[0].RecordID != "rec_applicationlisted" {
		t.Fatalf("ListRecords() = %#v, %v", listed, err)
	}
	revision, err := application.GetRevision(context.Background(), getRevisionRequest)
	if err != nil || revision.RevisionID != getRevisionRequest.RevisionID {
		t.Fatalf("GetRevision() = %#v, %v", revision, err)
	}
	revisions, err := application.ListRevisions(context.Background(), listRevisionsRequest)
	if err != nil || len(revisions) != 1 || revisions[0].RevisionID != "rrv_applicationlisted" {
		t.Fatalf("ListRevisions() = %#v, %v", revisions, err)
	}
}

func TestApplicationDraftEntryPointsForwardExactRequests(t *testing.T) {
	t.Parallel()

	actor := mustRecordActor(t)
	payload, err := NewDraftPayload([]byte(`{"title":"application forwarding"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := NewDraftETag("rdf_applicationforward", actor.UserID, 1, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	readRequest := DraftReadRequest{Actor: actor, DraftID: "rdf_applicationforward"}
	listRequest := DraftListRequest{Actor: actor, Limit: 25}
	createRequest := DraftCreateRequest{
		Actor:   actor,
		DraftID: "rdf_applicationforward",
		Payload: payload,
	}
	patchRequest := DraftPatchRequest{
		Actor:   actor,
		DraftID: "rdf_applicationforward",
		IfMatch: etag,
		Payload: payload,
	}
	discardRequest := DraftDiscardRequest{Actor: actor, DraftID: "rdf_applicationforward"}
	publishRequest := DraftPublishRequest{Actor: actor, DraftID: "rdf_applicationforward"}

	wantDraft := Draft{DraftID: "rdf_applicationforward"}
	drafts := &recordApplicationDraftStub{
		read: func(_ context.Context, request DraftReadRequest) (Draft, error) {
			if !reflect.DeepEqual(request, readRequest) {
				t.Fatalf("ReadDraft() request = %#v, want %#v", request, readRequest)
			}
			return wantDraft, nil
		},
		list: func(_ context.Context, request DraftListRequest) ([]Draft, error) {
			if !reflect.DeepEqual(request, listRequest) {
				t.Fatalf("ListDrafts() request = %#v, want %#v", request, listRequest)
			}
			return []Draft{wantDraft}, nil
		},
		create: func(_ context.Context, request DraftCreateRequest) (Draft, error) {
			if !reflect.DeepEqual(request, createRequest) {
				t.Fatalf("CreateDraft() request = %#v, want %#v", request, createRequest)
			}
			return wantDraft, nil
		},
		patch: func(_ context.Context, request DraftPatchRequest) (Draft, error) {
			if !reflect.DeepEqual(request, patchRequest) {
				t.Fatalf("PatchDraft() request = %#v, want %#v", request, patchRequest)
			}
			return wantDraft, nil
		},
		discard: func(_ context.Context, request DraftDiscardRequest) error {
			if !reflect.DeepEqual(request, discardRequest) {
				t.Fatalf("DiscardDraft() request = %#v, want %#v", request, discardRequest)
			}
			return nil
		},
		preparePublish: func(_ context.Context, request DraftPublishRequest) (Draft, error) {
			if !reflect.DeepEqual(request, publishRequest) {
				t.Fatalf("PreparePublish() request = %#v, want %#v", request, publishRequest)
			}
			return wantDraft, nil
		},
	}
	application := mustRecordApplication(t, &recordApplicationReadStub{}, drafts)

	for name, call := range map[string]func() (Draft, error){
		"read":    func() (Draft, error) { return application.ReadDraft(context.Background(), readRequest) },
		"create":  func() (Draft, error) { return application.CreateDraft(context.Background(), createRequest) },
		"patch":   func() (Draft, error) { return application.PatchDraft(context.Background(), patchRequest) },
		"publish": func() (Draft, error) { return application.PreparePublish(context.Background(), publishRequest) },
	} {
		got, callErr := call()
		if callErr != nil || got.DraftID != wantDraft.DraftID {
			t.Fatalf("%s draft entry point = %#v, %v", name, got, callErr)
		}
	}
	listed, err := application.ListDrafts(context.Background(), listRequest)
	if err != nil || len(listed) != 1 || listed[0].DraftID != wantDraft.DraftID {
		t.Fatalf("ListDrafts() = %#v, %v", listed, err)
	}
	if err := application.DiscardDraft(context.Background(), discardRequest); err != nil {
		t.Fatalf("DiscardDraft() error = %v", err)
	}
}

func mustRecordApplication(
	t *testing.T,
	read applicationReadService,
	drafts applicationDraftService,
) *Application {
	t.Helper()
	application, err := NewApplication(
		read,
		&recordApplicationRevisionStub{},
		&recordApplicationLifecycleStub{},
		drafts,
		ApplicationOptions{
			IdempotencyOwnerID: "records_api",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     24 * time.Hour,
			OutboxTTL:          24 * time.Hour,
		},
	)
	if err != nil {
		t.Fatalf("NewApplication() error = %v", err)
	}
	return application
}

type recordApplicationReadStub struct {
	getRecord     func(context.Context, RecordGetRequest) (Record, error)
	listRecords   func(context.Context, RecordListRequest) (RecordListResult, error)
	getRevision   func(context.Context, RecordRevisionGetRequest) (RecordRevision, error)
	listRevisions func(context.Context, RecordRevisionListRequest) ([]RecordRevision, error)
}

func (stub *recordApplicationReadStub) GetRecord(ctx context.Context, request RecordGetRequest) (Record, error) {
	if stub.getRecord == nil {
		return Record{}, errors.New("unexpected GetRecord call")
	}
	return stub.getRecord(ctx, request)
}

func (stub *recordApplicationReadStub) ListRecords(ctx context.Context, request RecordListRequest) (RecordListResult, error) {
	if stub.listRecords == nil {
		return RecordListResult{}, errors.New("unexpected ListRecords call")
	}
	return stub.listRecords(ctx, request)
}

func (stub *recordApplicationReadStub) GetRevision(ctx context.Context, request RecordRevisionGetRequest) (RecordRevision, error) {
	if stub.getRevision == nil {
		return RecordRevision{}, errors.New("unexpected GetRevision call")
	}
	return stub.getRevision(ctx, request)
}

func (stub *recordApplicationReadStub) ListRevisions(ctx context.Context, request RecordRevisionListRequest) ([]RecordRevision, error) {
	if stub.listRevisions == nil {
		return nil, errors.New("unexpected ListRevisions call")
	}
	return stub.listRevisions(ctx, request)
}

type recordApplicationRevisionStub struct {
	save func(context.Context, RevisionSaveRequest) (RevisionCommitResult, error)
}

func (stub *recordApplicationRevisionStub) SaveRevision(ctx context.Context, request RevisionSaveRequest) (RevisionCommitResult, error) {
	if stub.save == nil {
		return RevisionCommitResult{}, errors.New("unexpected SaveRevision call")
	}
	return stub.save(ctx, request)
}

type recordApplicationLifecycleStub struct {
	change func(context.Context, RecordLifecycleRequest) (RecordLifecycleResult, error)
}

func (stub *recordApplicationLifecycleStub) ChangeLifecycle(ctx context.Context, request RecordLifecycleRequest) (RecordLifecycleResult, error) {
	if stub.change == nil {
		return RecordLifecycleResult{}, errors.New("unexpected ChangeLifecycle call")
	}
	return stub.change(ctx, request)
}

type recordApplicationDraftStub struct {
	read           func(context.Context, DraftReadRequest) (Draft, error)
	list           func(context.Context, DraftListRequest) ([]Draft, error)
	create         func(context.Context, DraftCreateRequest) (Draft, error)
	patch          func(context.Context, DraftPatchRequest) (Draft, error)
	discard        func(context.Context, DraftDiscardRequest) error
	preparePublish func(context.Context, DraftPublishRequest) (Draft, error)
}

func (stub *recordApplicationDraftStub) ReadDraft(ctx context.Context, request DraftReadRequest) (Draft, error) {
	if stub.read == nil {
		return Draft{}, errors.New("unexpected ReadDraft call")
	}
	return stub.read(ctx, request)
}

func (stub *recordApplicationDraftStub) ListDrafts(ctx context.Context, request DraftListRequest) ([]Draft, error) {
	if stub.list == nil {
		return nil, errors.New("unexpected ListDrafts call")
	}
	return stub.list(ctx, request)
}

func (stub *recordApplicationDraftStub) CreateDraft(ctx context.Context, request DraftCreateRequest) (Draft, error) {
	if stub.create == nil {
		return Draft{}, errors.New("unexpected CreateDraft call")
	}
	return stub.create(ctx, request)
}

func (stub *recordApplicationDraftStub) PatchDraft(ctx context.Context, request DraftPatchRequest) (Draft, error) {
	if stub.patch == nil {
		return Draft{}, errors.New("unexpected PatchDraft call")
	}
	return stub.patch(ctx, request)
}

func (stub *recordApplicationDraftStub) DiscardDraft(ctx context.Context, request DraftDiscardRequest) error {
	if stub.discard == nil {
		return errors.New("unexpected DiscardDraft call")
	}
	return stub.discard(ctx, request)
}

func (stub *recordApplicationDraftStub) PreparePublish(ctx context.Context, request DraftPublishRequest) (Draft, error) {
	if stub.preparePublish == nil {
		return Draft{}, errors.New("unexpected PreparePublish call")
	}
	return stub.preparePublish(ctx, request)
}

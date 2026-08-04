package records

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestDraftPayloadCanonicalizesJSONObjectAndOwnsItsBytes(t *testing.T) {
	input := []byte(` { "title": "Draft", "tags": ["ops"], "body_markdown": "# Notes\n" } `)
	payload, err := NewDraftPayload(input)
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}

	wantJSON := []byte(`{"body_markdown":"# Notes\n","tags":["ops"],"title":"Draft"}`)
	if got := payload.JSON(); !bytes.Equal(got, wantJSON) {
		t.Fatalf("DraftPayload.JSON() = %s, want %s", got, wantJSON)
	}

	input[0] = 'x'
	returned := payload.JSON()
	returned[0] = 'x'
	if got := payload.JSON(); !bytes.Equal(got, wantJSON) {
		t.Fatalf("DraftPayload JSON changed through caller mutation: %s", got)
	}

	equivalent, err := NewDraftPayload([]byte(`{"tags":["ops"],"body_markdown":"# Notes\n","title":"Draft"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload(equivalent) error = %v", err)
	}
	if payload.Hash() != equivalent.Hash() {
		t.Fatalf("equivalent draft payload hashes differ: %x != %x", payload.Hash(), equivalent.Hash())
	}
}

func TestDraftServicePreservesDraftWhenBaseRevisionHasAdvanced(t *testing.T) {
	actor := mustAuthorizationActor(t, recordauth.RoleProjectAdmin)
	payload, err := NewDraftPayload([]byte(`{"title":"Local draft","body_markdown":"local"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := NewDraftETag("rdf_0123456789abcdef", actor.UserID, 4, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	draft := Draft{
		DraftID:        "rdf_0123456789abcdef",
		ProjectID:      recordauth.ProjectIDDefault,
		RecordID:       "rec_0123456789abcdef",
		BaseRevisionID: "rrv_1111111111111111",
		AuthorID:       actor.UserID,
		Payload:        payload,
		Version:        4,
		ETag:           etag,
		WarningAt:      now.Add(83 * 24 * time.Hour),
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now,
		ExpiresAt:      now.Add(90 * 24 * time.Hour),
	}
	visibility := mustAuthorizationVisibility(t, recordauth.VisibilityKindProject, nil)
	current := &currentRecordAuthorizationSourceStub{current: CurrentRecordAuthorization{
		RecordID:           draft.RecordID,
		CurrentRevisionID:  "rrv_2222222222222222",
		LockVersion:        7,
		AuthorizationEpoch: 9,
		Lifecycle:          LifecycleActive,
		Evidence: RecordAuthorizationEvidence{
			ProjectID:  recordauth.ProjectIDDefault,
			Visibility: visibility,
			Sources: []recordauth.SourceAuthorization{
				mustLiveAuthorization(t, recordauth.SourceKindVPS, testRecordVPSID, visibility, visibility),
			},
		},
	}}
	store := &draftServiceStoreStub{draft: draft}
	service, err := NewDraftService(store, current)
	if err != nil {
		t.Fatalf("NewDraftService() error = %v", err)
	}

	_, err = service.PreparePublish(context.Background(), DraftPublishRequest{
		Actor:   actor,
		DraftID: draft.DraftID,
	})
	if !errors.Is(err, ErrDraftRevisionConflict) {
		t.Fatalf("PreparePublish() error = %v, want ErrDraftRevisionConflict", err)
	}
	var conflict *DraftRevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("PreparePublish() error type = %T, want *DraftRevisionConflictError", err)
	}
	if conflict.ServerRevisionID != current.current.CurrentRevisionID ||
		conflict.ServerLockVersion != current.current.LockVersion ||
		conflict.ServerAuthorizationEpoch != current.current.AuthorizationEpoch ||
		conflict.Draft.DraftID != draft.DraftID || conflict.Draft.Payload.Hash() != payload.Hash() {
		t.Fatalf("PreparePublish() conflict = %#v", conflict)
	}
	if store.deleteCalls != 0 {
		t.Fatalf("PreparePublish() deleted draft on conflict: %d calls", store.deleteCalls)
	}
	if strings.Contains(err.Error(), draft.RecordID) || strings.Contains(err.Error(), draft.DraftID) {
		t.Fatalf("PreparePublish() error leaks resource identity: %q", err)
	}
}

func TestDraftServicePatchUsesTrustedAuthorAndKeepsAdvancedBase(t *testing.T) {
	actor := mustAuthorizationActor(t, recordauth.RoleProjectAdmin)
	originalPayload, err := NewDraftPayload([]byte(`{"title":"Original"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload(original) error = %v", err)
	}
	updatedPayload, err := NewDraftPayload([]byte(`{"title":"Updated"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload(updated) error = %v", err)
	}
	originalETag, err := NewDraftETag("rdf_0123456789abcdef", actor.UserID, 1, originalPayload)
	if err != nil {
		t.Fatalf("NewDraftETag(original) error = %v", err)
	}
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	draft := Draft{
		DraftID:        "rdf_0123456789abcdef",
		ProjectID:      recordauth.ProjectIDDefault,
		RecordID:       "rec_0123456789abcdef",
		BaseRevisionID: "rrv_1111111111111111",
		AuthorID:       actor.UserID,
		Payload:        originalPayload,
		Version:        1,
		ETag:           originalETag,
		WarningAt:      now.Add(83 * 24 * time.Hour),
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(90 * 24 * time.Hour),
	}
	visibility := mustAuthorizationVisibility(t, recordauth.VisibilityKindProject, nil)
	current := &currentRecordAuthorizationSourceStub{current: CurrentRecordAuthorization{
		RecordID:           draft.RecordID,
		CurrentRevisionID:  "rrv_2222222222222222",
		LockVersion:        2,
		AuthorizationEpoch: 2,
		Lifecycle:          LifecycleActive,
		Evidence: RecordAuthorizationEvidence{
			ProjectID:  recordauth.ProjectIDDefault,
			Visibility: visibility,
			Sources: []recordauth.SourceAuthorization{
				mustLiveAuthorization(t, recordauth.SourceKindVPS, testRecordVPSID, visibility, visibility),
			},
		},
	}}
	store := &draftServiceStoreStub{draft: draft}
	service, err := NewDraftService(store, current)
	if err != nil {
		t.Fatalf("NewDraftService() error = %v", err)
	}

	updated, err := service.PatchDraft(context.Background(), DraftPatchRequest{
		Actor:   actor,
		DraftID: draft.DraftID,
		IfMatch: originalETag,
		Payload: updatedPayload,
	})
	if err != nil {
		t.Fatalf("PatchDraft() error = %v", err)
	}
	if updated.BaseRevisionID != draft.BaseRevisionID || updated.Payload.Hash() != updatedPayload.Hash() || updated.Version != 2 {
		t.Fatalf("PatchDraft() = %#v", updated)
	}
	if store.patchAuthorID != actor.UserID {
		t.Fatalf("PatchDraft() author = %q, want trusted actor %q", store.patchAuthorID, actor.UserID)
	}
}

func TestDraftServiceCreatesNewAndExistingDraftsFromTrustedActor(t *testing.T) {
	actor := mustAuthorizationActor(t, recordauth.RoleProjectAdmin)
	payload, err := NewDraftPayload([]byte(`{"title":"Draft"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	visibility := mustAuthorizationVisibility(t, recordauth.VisibilityKindProject, nil)
	current := &currentRecordAuthorizationSourceStub{current: CurrentRecordAuthorization{
		RecordID:           "rec_0123456789abcdef",
		CurrentRevisionID:  "rrv_0123456789abcdef",
		LockVersion:        3,
		AuthorizationEpoch: 4,
		Lifecycle:          LifecycleActive,
		Evidence: RecordAuthorizationEvidence{
			ProjectID:  recordauth.ProjectIDDefault,
			Visibility: visibility,
			Sources: []recordauth.SourceAuthorization{
				mustLiveAuthorization(t, recordauth.SourceKindVPS, testRecordVPSID, visibility, visibility),
			},
		},
	}}

	tests := []struct {
		name           string
		request        DraftCreateRequest
		wantCurrentUse bool
	}{
		{name: "new record", request: DraftCreateRequest{
			Actor:   actor,
			DraftID: "rdf_1111111111111111",
			Payload: payload,
		}},
		{name: "existing record", request: DraftCreateRequest{
			Actor:          actor,
			DraftID:        "rdf_2222222222222222",
			RecordID:       current.current.RecordID,
			BaseRevisionID: current.current.CurrentRevisionID,
			Payload:        payload,
		}, wantCurrentUse: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current.calls = 0
			store := &draftServiceStoreStub{}
			service, err := NewDraftService(store, current)
			if err != nil {
				t.Fatalf("NewDraftService() error = %v", err)
			}
			draft, err := service.CreateDraft(context.Background(), tt.request)
			if err != nil {
				t.Fatalf("CreateDraft() error = %v", err)
			}
			if draft.AuthorID != actor.UserID || draft.ProjectID != actor.ProjectID ||
				draft.RecordID != tt.request.RecordID || draft.BaseRevisionID != tt.request.BaseRevisionID ||
				draft.Payload.Hash() != payload.Hash() {
				t.Fatalf("CreateDraft() = %#v", draft)
			}
			if (current.calls > 0) != tt.wantCurrentUse {
				t.Fatalf("current authorization calls = %d, want use %v", current.calls, tt.wantCurrentUse)
			}
		})
	}
}

func TestDraftServiceDiscardIsAuthorPrivate(t *testing.T) {
	owner := mustAuthorizationActor(t, recordauth.RoleProjectAdmin)
	other := mustAuthorizationActor(t, recordauth.RoleProjectAdmin)
	other.UserID = "usr_89abcdef0123456701234567"
	other, err := recordauth.NormalizeActorScope(other)
	if err != nil {
		t.Fatalf("NormalizeActorScope(other) error = %v", err)
	}
	payload, err := NewDraftPayload([]byte(`{"title":"Private"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := NewDraftETag("rdf_0123456789abcdef", owner.UserID, 1, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	store := &draftServiceStoreStub{draft: Draft{
		DraftID:   "rdf_0123456789abcdef",
		ProjectID: recordauth.ProjectIDDefault,
		AuthorID:  owner.UserID,
		Payload:   payload,
		Version:   1,
		ETag:      etag,
		WarningAt: now.Add(83 * 24 * time.Hour),
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(90 * 24 * time.Hour),
	}}
	service, err := NewDraftService(store, &currentRecordAuthorizationSourceStub{})
	if err != nil {
		t.Fatalf("NewDraftService() error = %v", err)
	}

	if err := service.DiscardDraft(context.Background(), DraftDiscardRequest{Actor: other, DraftID: store.draft.DraftID}); !errors.Is(err, ErrDraftNotFound) {
		t.Fatalf("DiscardDraft(other) error = %v, want ErrDraftNotFound", err)
	}
	if store.deleteCalls != 0 {
		t.Fatalf("DiscardDraft(other) delete calls = %d, want 0", store.deleteCalls)
	}
	if err := service.DiscardDraft(context.Background(), DraftDiscardRequest{Actor: owner, DraftID: store.draft.DraftID}); err != nil {
		t.Fatalf("DiscardDraft(owner) error = %v", err)
	}
	if store.deleteCalls != 1 || store.deleteReason != DraftDeleteDiscarded {
		t.Fatalf("DiscardDraft(owner) cleanup = (%d, %q)", store.deleteCalls, store.deleteReason)
	}
}

func TestDraftServiceReadsRoutingAuthorizationBeforePayload(t *testing.T) {
	owner := mustAuthorizationActor(t, recordauth.RoleProjectAdmin, testRecordGroupID)
	visibility := mustRecordVisibility(t)
	payload, err := NewDraftPayload([]byte(`{"title":"Private draft"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	etag, err := NewDraftETag("rdf_0123456789abcdef", owner.UserID, 1, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	steps := make([]string, 0, 3)
	store := &draftServiceStoreStub{
		steps: &steps,
		draft: Draft{
			DraftID:        "rdf_0123456789abcdef",
			ProjectID:      recordauth.ProjectIDDefault,
			RecordID:       "rec_service1",
			BaseRevisionID: "rrv_current00000004",
			AuthorID:       owner.UserID,
			Payload:        payload,
			Version:        1,
			ETag:           etag,
			CreatedAt:      now,
			UpdatedAt:      now,
			WarningAt:      now.Add(83 * 24 * time.Hour),
			ExpiresAt:      now.Add(90 * 24 * time.Hour),
		},
	}
	current := &currentRecordAuthorizationSourceStub{
		steps: &steps,
		current: CurrentRecordAuthorization{
			RecordID:           "rec_service1",
			CurrentRevisionID:  "rrv_current00000004",
			LockVersion:        7,
			AuthorizationEpoch: 5,
			Lifecycle:          LifecycleActive,
			Evidence: RecordAuthorizationEvidence{
				ProjectID:  recordauth.ProjectIDDefault,
				Visibility: visibility,
				Sources:    []recordauth.SourceAuthorization{mustRecordSourceAuthorization(t, visibility)},
			},
		},
	}
	service, err := NewDraftService(store, current)
	if err != nil {
		t.Fatalf("NewDraftService() error = %v", err)
	}

	got, err := service.ReadDraft(context.Background(), DraftReadRequest{Actor: owner, DraftID: store.draft.DraftID})
	if err != nil {
		t.Fatalf("ReadDraft() error = %v", err)
	}
	if got.DraftID != store.draft.DraftID || !reflect.DeepEqual(steps, []string{"routing", "current", "payload"}) {
		t.Fatalf("ReadDraft() = %#v steps=%#v", got, steps)
	}

	denied := mustAuthorizationActor(t, recordauth.RoleViewer)
	store.getCalls = 0
	if _, err := service.ReadDraft(context.Background(), DraftReadRequest{Actor: denied, DraftID: store.draft.DraftID}); !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("ReadDraft(denied) error = %v, want ErrDenied", err)
	}
	if store.getCalls != 0 {
		t.Fatalf("ReadDraft(denied) payload reads = %d, want 0", store.getCalls)
	}
}

type draftServiceStoreStub struct {
	draft         Draft
	deleteCalls   int
	deleteReason  DraftDeleteReason
	patchAuthorID string
	steps         *[]string
	getCalls      int
}

func (store *draftServiceStoreStub) CreateDraft(_ context.Context, command DraftCreateCommand) (Draft, error) {
	if err := command.Validate(); err != nil {
		return Draft{}, err
	}
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	etag, err := NewDraftETag(command.DraftID, command.AuthorID, 1, command.Payload)
	if err != nil {
		return Draft{}, err
	}
	store.draft = Draft{
		DraftID:        command.DraftID,
		ProjectID:      command.ProjectID,
		RecordID:       command.RecordID,
		BaseRevisionID: command.BaseRevisionID,
		AuthorID:       command.AuthorID,
		Payload:        command.Payload,
		Version:        1,
		ETag:           etag,
		WarningAt:      now.Add(command.Policy.DraftTTL - command.Policy.WarningLead),
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(command.Policy.DraftTTL),
	}
	return store.draft, nil
}

func (store *draftServiceStoreStub) GetDraft(_ context.Context, draftID, authorID string) (Draft, error) {
	store.getCalls++
	if store.steps != nil {
		*store.steps = append(*store.steps, "payload")
	}
	if draftID != store.draft.DraftID || authorID != store.draft.AuthorID {
		return Draft{}, ErrDraftNotFound
	}
	return store.draft, nil
}

func (store *draftServiceStoreStub) GetDraftRouting(_ context.Context, draftID, authorID string) (DraftRouting, error) {
	if store.steps != nil {
		*store.steps = append(*store.steps, "routing")
	}
	if draftID != store.draft.DraftID || authorID != store.draft.AuthorID {
		return DraftRouting{}, ErrDraftNotFound
	}
	return DraftRoutingFromDraft(store.draft), nil
}

func (store *draftServiceStoreStub) ListDraftRoutings(_ context.Context, authorID string, _ uint64) ([]DraftRouting, error) {
	if authorID != store.draft.AuthorID {
		return nil, nil
	}
	return []DraftRouting{DraftRoutingFromDraft(store.draft)}, nil
}

func (store *draftServiceStoreStub) PatchDraft(_ context.Context, command DraftPatchCommand) (Draft, error) {
	store.patchAuthorID = command.AuthorID
	if command.DraftID != store.draft.DraftID || command.AuthorID != store.draft.AuthorID || command.IfMatch != store.draft.ETag {
		return Draft{}, ErrDraftNotFound
	}
	etag, err := NewDraftETag(store.draft.DraftID, store.draft.AuthorID, store.draft.Version+1, command.Payload)
	if err != nil {
		return Draft{}, err
	}
	store.draft.Payload = command.Payload
	store.draft.Version++
	store.draft.ETag = etag
	store.draft.UpdatedAt = store.draft.UpdatedAt.Add(time.Minute)
	store.draft.WarningAt = store.draft.UpdatedAt.Add(83 * 24 * time.Hour)
	store.draft.ExpiresAt = store.draft.UpdatedAt.Add(90 * 24 * time.Hour)
	return store.draft, nil
}

func (store *draftServiceStoreStub) DeleteDraft(_ context.Context, command DraftDeleteCommand) error {
	if command.DraftID != store.draft.DraftID || command.AuthorID != store.draft.AuthorID {
		return ErrDraftNotFound
	}
	store.deleteCalls++
	store.deleteReason = command.Reason
	return nil
}

func TestDraftCreateCommandSeparatesNewAndExistingDraftsWithBoundedRetention(t *testing.T) {
	payload, err := NewDraftPayload([]byte(`{"title":"Draft"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	policy := DefaultDraftRetentionPolicy()
	if policy.DraftTTL != 90*24*time.Hour || policy.WarningLead != 7*24*time.Hour ||
		policy.CheckpointBucket != 5*time.Minute || policy.CheckpointTTL != 7*24*time.Hour ||
		policy.CheckpointLimit != 20 {
		t.Fatalf("DefaultDraftRetentionPolicy() = %#v", policy)
	}

	base := DraftCreateCommand{
		DraftID:   "rdf_0123456789abcdef",
		ProjectID: recordauth.ProjectIDDefault,
		AuthorID:  "usr_0123456789abcdef01234567",
		Payload:   payload,
		Policy:    policy,
	}
	tests := []struct {
		name    string
		command DraftCreateCommand
		wantErr bool
	}{
		{name: "new record draft", command: base},
		{name: "existing record draft", command: func() DraftCreateCommand {
			command := base
			command.RecordID = "rec_0123456789abcdef"
			command.BaseRevisionID = "rrv_0123456789abcdef"
			return command
		}()},
		{name: "record without base", command: func() DraftCreateCommand {
			command := base
			command.RecordID = "rec_0123456789abcdef"
			return command
		}(), wantErr: true},
		{name: "base without record", command: func() DraftCreateCommand {
			command := base
			command.BaseRevisionID = "rrv_0123456789abcdef"
			return command
		}(), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.command.Validate()
			if tt.wantErr && !errors.Is(err, ErrInvalidDraftCommand) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDraftCommand", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestDraftDeleteCommandClosesPublishDiscardAndRevokeShapes(t *testing.T) {
	payload, err := NewDraftPayload([]byte(`{"title":"Draft"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := NewDraftETag("rdf_0123456789abcdef", "usr_0123456789abcdef01234567", 1, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	base := DraftDeleteCommand{
		DraftID:  "rdf_0123456789abcdef",
		AuthorID: "usr_0123456789abcdef01234567",
	}
	tests := []struct {
		name    string
		command DraftDeleteCommand
		wantErr bool
	}{
		{name: "published with exact etag", command: func() DraftDeleteCommand {
			command := base
			command.Reason = DraftDeletePublished
			command.IfMatch = etag
			return command
		}()},
		{name: "published without etag", command: func() DraftDeleteCommand {
			command := base
			command.Reason = DraftDeletePublished
			return command
		}(), wantErr: true},
		{name: "discarded", command: func() DraftDeleteCommand {
			command := base
			command.Reason = DraftDeleteDiscarded
			return command
		}()},
		{name: "revoked", command: func() DraftDeleteCommand {
			command := base
			command.Reason = DraftDeleteRevoked
			return command
		}()},
		{name: "unknown", command: func() DraftDeleteCommand {
			command := base
			command.Reason = "expired"
			return command
		}(), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.command.Validate()
			if tt.wantErr && !errors.Is(err, ErrInvalidDraftCommand) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDraftCommand", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestDraftETagHasOneExactStrongSpellingAndBindsDraftVersion(t *testing.T) {
	payload, err := NewDraftPayload([]byte(`{"title":"Draft"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}

	etag, err := NewDraftETag(
		"rdf_0123456789abcdef",
		"usr_0123456789abcdef01234567",
		1,
		payload,
	)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	if got := etag.String(); !strings.HasPrefix(got, `"draft-v1-`) || !strings.HasSuffix(got, `"`) || len(got) != len(`"draft-v1-`)+64+1 {
		t.Fatalf("DraftETag.String() = %q, want one quoted draft-v1 digest", got)
	}
	parsed, err := ParseDraftETag(etag.String())
	if err != nil {
		t.Fatalf("ParseDraftETag() error = %v", err)
	}
	if parsed != etag {
		t.Fatalf("ParseDraftETag() = %#v, want %#v", parsed, etag)
	}

	for _, invalid := range []string{
		strings.Trim(etag.String(), `"`),
		"W/" + etag.String(),
		strings.ToUpper(etag.String()),
		`*`,
	} {
		if _, err := ParseDraftETag(invalid); !errors.Is(err, ErrInvalidDraftETag) {
			t.Fatalf("ParseDraftETag(%q) error = %v, want ErrInvalidDraftETag", invalid, err)
		}
	}

	for _, changed := range []struct {
		name     string
		draftID  string
		authorID string
		version  uint64
		payload  DraftPayload
	}{
		{name: "draft", draftID: "rdf_fedcba9876543210", authorID: "usr_0123456789abcdef01234567", version: 1, payload: payload},
		{name: "author", draftID: "rdf_0123456789abcdef", authorID: "usr_89abcdef0123456701234567", version: 1, payload: payload},
		{name: "version", draftID: "rdf_0123456789abcdef", authorID: "usr_0123456789abcdef01234567", version: 2, payload: payload},
	} {
		t.Run(changed.name, func(t *testing.T) {
			other, err := NewDraftETag(changed.draftID, changed.authorID, changed.version, changed.payload)
			if err != nil {
				t.Fatalf("NewDraftETag() error = %v", err)
			}
			if other == etag {
				t.Fatal("changed draft authority produced the same ETag")
			}
		})
	}
}

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/store"
)

func TestRecordDeletionsHandlerPreviewsWithTrustedActorAndAllowlistedToken(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	token := mustRecordDeletionHandlerToken(t)
	expiresAt := time.Date(2026, time.August, 3, 23, 10, 0, 0, time.UTC)
	backupExpiresAt := time.Date(2026, time.September, 2, 23, 10, 0, 0, time.UTC)
	application := &recordDeletionHandlerApplicationStub{
		preview: func(_ context.Context, request recorddeletion.PreviewRequest) (recorddeletion.PreviewResult, error) {
			if !reflect.DeepEqual(request.Actor, actor) || request.RecordID != "rec_httpcontract" {
				t.Fatalf("Preview() request = %#v", request)
			}
			return recorddeletion.PreviewResult{
				ReservationID: "drs_httpcontract",
				Token:         token,
				ExpiresAt:     expiresAt,
				Summary: recorddeletion.PreviewSummary{
					OnlinePurgeScopes: recorddeletion.RequiredAdapterNames(),
					SurvivingCopies: []recorddeletion.SurvivingCopySummary{
						{
							Scope:     recorddeletion.AdapterNameRecordAttachments,
							Kind:      recorddeletion.SurvivingCopyKindOtherRecord,
							CopyCount: 2,
						},
					},
					ManagedBackup: recorddeletion.ManagedBackupSummary{
						RetainedCopyCount:    1,
						MaximumRetentionDays: 30,
						LatestExpiresAt:      backupExpiresAt,
					},
					LedgerHealth: recorddeletion.LedgerHealthHealthy,
				},
			}, nil
		},
	}
	handler := RecordDeletions(application)
	request := recordDeletionHandlerRequest(
		t,
		actor,
		http.MethodPost,
		"/api/records/rec_httpcontract/permanent-delete-preview",
		"",
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	assertRecordDeletionNoStore(t, recorder)
	assertRecordDeletionJSONKeys(
		t,
		recorder.Body.Bytes(),
		"deletion_request_token",
		"expires_at",
		"ledger_health",
		"managed_backup",
		"online_purge_scopes",
		"reservation_id",
		"surviving_copies",
	)
	var response recordDeletionPreviewResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ReservationID != "drs_httpcontract" || response.DeletionRequestToken != token.Transport() ||
		!response.ExpiresAt.Equal(expiresAt) || response.LedgerHealth != "healthy" ||
		!reflect.DeepEqual(response.OnlinePurgeScopes, []string{
			"record_core",
			"record_attachments",
			"record_evidence",
			"record_markdown_client",
			"record_search",
			"record_activity_projection",
			"record_comparison",
			"record_collaboration",
			"record_portability",
		}) || !reflect.DeepEqual(response.SurvivingCopies, []recordDeletionSurvivingCopyResponse{{
		Scope: "record_attachments", Kind: "other_record", CopyCount: 2,
	}}) || response.ManagedBackup.RetainedCopyCount != 1 ||
		response.ManagedBackup.MaximumRetentionDays != 30 || response.ManagedBackup.LatestExpiresAt == nil ||
		!response.ManagedBackup.LatestExpiresAt.Equal(backupExpiresAt) {
		t.Fatalf("preview response = %#v", response)
	}
	assertRecordDeletionJSONKeys(t, mustRecordDeletionJSONField(t, recorder.Body.Bytes(), "managed_backup"), "latest_expires_at", "maximum_retention_days", "retained_copy_count")
	for _, forbidden := range []string{
		"binding_digest",
		"dependency_digest",
		"impact_digest",
		"ledger_entry_hash",
		"project_id",
		"record_id",
		"witness_sequence",
	} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("preview response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
	if reflect.TypeOf(response).PkgPath() != reflect.TypeOf(recordResponse{}).PkgPath() {
		t.Fatal("deletion preview response is not handler-owned")
	}
}

func TestRecordDeletionsHandlerPreviewSerializesEmptySurvivorsAndNoBackup(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	token := mustRecordDeletionHandlerToken(t)
	application := &recordDeletionHandlerApplicationStub{
		preview: func(context.Context, recorddeletion.PreviewRequest) (recorddeletion.PreviewResult, error) {
			return recorddeletion.PreviewResult{
				ReservationID: "drs_httpcontract",
				Token:         token,
				ExpiresAt:     time.Date(2026, time.August, 3, 23, 10, 0, 0, time.UTC),
				Summary: recorddeletion.PreviewSummary{
					OnlinePurgeScopes: recorddeletion.RequiredAdapterNames(),
					SurvivingCopies:   []recorddeletion.SurvivingCopySummary{},
					LedgerHealth:      recorddeletion.LedgerHealthHealthy,
				},
			}, nil
		},
	}
	handler := RecordDeletions(application)
	request := recordDeletionHandlerRequest(
		t,
		actor,
		http.MethodPost,
		"/api/records/rec_httpcontract/permanent-delete-preview",
		"",
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response recordDeletionPreviewResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.SurvivingCopies == nil || len(response.SurvivingCopies) != 0 {
		t.Fatalf("surviving_copies = %#v, want []", response.SurvivingCopies)
	}
	if response.ManagedBackup != (recordDeletionManagedBackupResponse{}) {
		t.Fatalf("managed_backup = %#v, want zero counts and null expiry", response.ManagedBackup)
	}
	if raw := mustRecordDeletionJSONField(t, recorder.Body.Bytes(), "surviving_copies"); string(raw) != "[]" {
		t.Fatalf("surviving_copies JSON = %s, want []", raw)
	}
	managedBackup := mustRecordDeletionJSONField(t, recorder.Body.Bytes(), "managed_backup")
	if raw := mustRecordDeletionJSONField(t, managedBackup, "latest_expires_at"); string(raw) != "null" {
		t.Fatalf("latest_expires_at JSON = %s, want null", raw)
	}
}

func TestRecordDeletionsHandlerRejectsInvalidPreviewSafetySummary(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	token := mustRecordDeletionHandlerToken(t)
	application := &recordDeletionHandlerApplicationStub{
		preview: func(context.Context, recorddeletion.PreviewRequest) (recorddeletion.PreviewResult, error) {
			return recorddeletion.PreviewResult{
				ReservationID: "drs_httpcontract",
				Token:         token,
				ExpiresAt:     time.Date(2026, time.August, 3, 23, 10, 0, 0, time.UTC),
				Summary: recorddeletion.PreviewSummary{
					OnlinePurgeScopes: recorddeletion.RequiredAdapterNames(),
					SurvivingCopies: []recorddeletion.SurvivingCopySummary{
						{Scope: recorddeletion.AdapterNameRecordAttachments, Kind: recorddeletion.SurvivingCopyKindOtherRecord, CopyCount: 1},
						{Scope: recorddeletion.AdapterNameRecordCore, Kind: recorddeletion.SurvivingCopyKindOtherRecord, CopyCount: 1},
					},
					LedgerHealth: recorddeletion.LedgerHealthHealthy,
				},
			}, nil
		},
	}
	handler := RecordDeletions(application)
	request := recordDeletionHandlerRequest(
		t,
		actor,
		http.MethodPost,
		"/api/records/rec_httpcontract/permanent-delete-preview",
		"",
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	assertRecordDeletionNoStore(t, recorder)
	assertRecordDeletionError(t, recorder, "internal_error")
	if strings.Contains(recorder.Body.String(), token.Transport()) {
		t.Fatalf("invalid preview response leaked token: %s", recorder.Body.String())
	}
}

func TestRecordDeletionsHandlerExecutesCanonicalTokenAndReplaysSameOperation(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	token := mustRecordDeletionHandlerToken(t)
	operation := recordDeletionHandlerOperation(recorddeletion.DeletionStateWitnessPending)
	calls := 0
	application := &recordDeletionHandlerApplicationStub{
		execute: func(_ context.Context, request recorddeletion.ExecuteRequest) (recorddeletion.DeletionOperation, error) {
			calls++
			if !reflect.DeepEqual(request.Actor, actor) || request.RecordID != operation.Object.ObjectID ||
				request.ReservationID != operation.ReservationID || request.Token.Transport() != token.Transport() ||
				request.ReasonCode != recorddeletion.DeletionReasonUserConfirmed {
				t.Fatalf("Execute() request = %#v", request)
			}
			return operation, nil
		},
	}
	handler := RecordDeletions(application)
	body := validRecordDeletionExecuteBody()
	var firstBody string
	for attempt := 0; attempt < 2; attempt++ {
		request := recordDeletionHandlerRequest(
			t,
			actor,
			http.MethodPost,
			"/api/records/rec_httpcontract/permanent-delete",
			body,
		)
		request.Header.Set("Idempotency-Key", token.Transport())
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusAccepted || recorder.Header().Get("Retry-After") != "1" {
			t.Fatalf("attempt %d status=%d retry-after=%q body=%s", attempt, recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
		}
		assertRecordDeletionNoStore(t, recorder)
		assertRecordDeletionJSONKeys(t, recorder.Body.Bytes(), "operation_id", "state")
		if attempt == 0 {
			firstBody = recorder.Body.String()
		} else if recorder.Body.String() != firstBody {
			t.Fatalf("replay body = %q, want %q", recorder.Body.String(), firstBody)
		}
	}
	if calls != 2 {
		t.Fatalf("Execute() calls = %d, want 2", calls)
	}
}

func TestRecordDeletionsHandlerExecuteReturnsNotCommittedAsTerminal200(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	operation := recordDeletionHandlerOperation(recorddeletion.DeletionStateNotCommitted)
	application := &recordDeletionHandlerApplicationStub{
		execute: func(context.Context, recorddeletion.ExecuteRequest) (recorddeletion.DeletionOperation, error) {
			return operation, nil
		},
	}
	handler := RecordDeletions(application)
	token := mustRecordDeletionHandlerToken(t)
	request := recordDeletionHandlerRequest(
		t,
		actor,
		http.MethodPost,
		"/api/records/rec_httpcontract/permanent-delete",
		validRecordDeletionExecuteBody(),
	)
	request.Header.Set("Idempotency-Key", token.Transport())
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Header().Get("Retry-After") != "" {
		t.Fatalf("status=%d retry-after=%q body=%s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
	}
	assertRecordDeletionNoStore(t, recorder)
	assertRecordDeletionJSONKeys(t, recorder.Body.Bytes(), "operation_id", "state")
}

func TestRecordDeletionsHandlerMapsOperationStatesToPendingAndTerminalHTTP(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	tests := []struct {
		name       string
		state      recorddeletion.DeletionState
		wantStatus int
		wantRetry  string
	}{
		{name: "provisional fenced", state: recorddeletion.DeletionStateProvisionalFenced, wantStatus: http.StatusAccepted, wantRetry: "1"},
		{name: "ledger unknown", state: recorddeletion.DeletionStateLedgerCommitUnknown, wantStatus: http.StatusAccepted, wantRetry: "1"},
		{name: "release pending", state: recorddeletion.DeletionStateReleasePending, wantStatus: http.StatusAccepted, wantRetry: "1"},
		{name: "retry required", state: recorddeletion.DeletionStateRetryRequired, wantStatus: http.StatusAccepted, wantRetry: "1"},
		{name: "not committed", state: recorddeletion.DeletionStateNotCommitted, wantStatus: http.StatusOK},
		{name: "online purged", state: recorddeletion.DeletionStateOnlinePurged, wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation := recordDeletionHandlerOperation(tt.state)
			application := &recordDeletionHandlerApplicationStub{
				status: func(_ context.Context, request recorddeletion.StatusRequest) (recorddeletion.DeletionOperation, error) {
					if !reflect.DeepEqual(request.Actor, actor) || request.OperationID != operation.OperationID {
						t.Fatalf("Status() request = %#v", request)
					}
					return operation, nil
				},
			}
			handler := RecordDeletions(application)
			request := recordDeletionHandlerRequest(t, actor, http.MethodGet, "/api/record-deletions/"+operation.OperationID, "")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus || recorder.Header().Get("Retry-After") != tt.wantRetry {
				t.Fatalf("status=%d retry-after=%q body=%s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
			}
			assertRecordDeletionNoStore(t, recorder)
			assertRecordDeletionJSONKeys(t, recorder.Body.Bytes(), "operation_id", "state")
			body := recorder.Body.String()
			for _, forbidden := range []string{
				operation.ReservationID,
				"ledger_sequence",
				"ledger_entry_hash",
				"receipt_digest",
				"fence_epoch",
				"release_epoch",
				"reason_code",
				"project_id",
				"record_id",
			} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("response leaked %q: %s", forbidden, body)
				}
			}
		})
	}
}

func TestRecordDeletionsHandlerMapsOpaqueConflictAndUnavailableErrors(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	tests := []struct {
		name       string
		path       string
		method     string
		body       string
		previewErr error
		executeErr error
		statusErr  error
		wantStatus int
		wantCode   string
	}{
		{name: "preview denied", path: "/api/records/rec_httpcontract/permanent-delete-preview", method: http.MethodPost, previewErr: recordauth.ErrDenied, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "preview missing", path: "/api/records/rec_httpcontract/permanent-delete-preview", method: http.MethodPost, previewErr: recorddeletion.ErrDeletionPreviewNotFound, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "preview unavailable", path: "/api/records/rec_httpcontract/permanent-delete-preview", method: http.MethodPost, previewErr: recorddeletion.ErrDeletionSafetyUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "deletion_safety_unavailable"},
		{name: "preview authorization source unavailable", path: "/api/records/rec_httpcontract/permanent-delete-preview", method: http.MethodPost, previewErr: store.ErrRecordSubjectUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "deletion_safety_unavailable"},
		{name: "execute stale", path: "/api/records/rec_httpcontract/permanent-delete", method: http.MethodPost, body: validRecordDeletionExecuteBody(), executeErr: recorddeletion.ErrDeletionPreviewStale, wantStatus: http.StatusConflict, wantCode: "deletion_preview_stale"},
		{name: "execute token reused", path: "/api/records/rec_httpcontract/permanent-delete", method: http.MethodPost, body: validRecordDeletionExecuteBody(), executeErr: recorddeletion.ErrDeletionRequestTokenReused, wantStatus: http.StatusConflict, wantCode: "deletion_request_token_reused"},
		{name: "execute unavailable", path: "/api/records/rec_httpcontract/permanent-delete", method: http.MethodPost, body: validRecordDeletionExecuteBody(), executeErr: recorddeletion.ErrDeletionSafetyUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "deletion_safety_unavailable"},
		{name: "execute admission unavailable", path: "/api/records/rec_httpcontract/permanent-delete", method: http.MethodPost, body: validRecordDeletionExecuteBody(), executeErr: store.ErrRecordPlatformAdmissionUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "deletion_safety_unavailable"},
		{name: "execute store unavailable", path: "/api/records/rec_httpcontract/permanent-delete", method: http.MethodPost, body: validRecordDeletionExecuteBody(), executeErr: store.ErrRecordDeletionStoreUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "deletion_safety_unavailable"},
		{name: "status denied", path: "/api/record-deletions/rpo_httpcontract", method: http.MethodGet, statusErr: recorddeletion.ErrDeletionOperationNotFound, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "status unavailable", path: "/api/record-deletions/rpo_httpcontract", method: http.MethodGet, statusErr: recorddeletion.ErrDeletionStatusUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "deletion_status_unavailable"},
		{name: "unknown", path: "/api/record-deletions/rpo_httpcontract", method: http.MethodGet, statusErr: errors.New("sql secret detail"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			application := &recordDeletionHandlerApplicationStub{
				preview: func(context.Context, recorddeletion.PreviewRequest) (recorddeletion.PreviewResult, error) {
					return recorddeletion.PreviewResult{}, tt.previewErr
				},
				execute: func(context.Context, recorddeletion.ExecuteRequest) (recorddeletion.DeletionOperation, error) {
					return recorddeletion.DeletionOperation{}, tt.executeErr
				},
				status: func(context.Context, recorddeletion.StatusRequest) (recorddeletion.DeletionOperation, error) {
					return recorddeletion.DeletionOperation{}, tt.statusErr
				},
			}
			handler := RecordDeletions(application)
			request := recordDeletionHandlerRequest(t, actor, tt.method, tt.path, tt.body)
			if tt.method == http.MethodPost && tt.path == "/api/records/rec_httpcontract/permanent-delete" {
				request.Header.Set("Idempotency-Key", mustRecordDeletionHandlerToken(t).Transport())
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			assertRecordDeletionNoStore(t, recorder)
			assertRecordDeletionError(t, recorder, tt.wantCode)
			for _, leaked := range []string{"sql secret detail", "record deletion preview", "record deletion operation"} {
				if strings.Contains(recorder.Body.String(), leaked) {
					t.Fatalf("error leaked %q: %s", leaked, recorder.Body.String())
				}
			}
		})
	}
}

func TestRecordDeletionsHandlerRejectsInvalidTransportBeforeApplication(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	calls := 0
	application := &recordDeletionHandlerApplicationStub{
		preview: func(context.Context, recorddeletion.PreviewRequest) (recorddeletion.PreviewResult, error) {
			calls++
			return recorddeletion.PreviewResult{}, nil
		},
		execute: func(context.Context, recorddeletion.ExecuteRequest) (recorddeletion.DeletionOperation, error) {
			calls++
			return recorddeletion.DeletionOperation{}, nil
		},
		status: func(context.Context, recorddeletion.StatusRequest) (recorddeletion.DeletionOperation, error) {
			calls++
			return recorddeletion.DeletionOperation{}, nil
		},
	}
	tests := []struct {
		name            string
		method          string
		path            string
		body            string
		idempotencyKeys []string
		wantStatus      int
		wantCode        string
	}{
		{name: "invalid record path", method: http.MethodPost, path: "/api/records/not-a-record/permanent-delete-preview", wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "invalid operation path", method: http.MethodGet, path: "/api/record-deletions/not-an-operation", wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "unknown subtree", method: http.MethodGet, path: "/api/record-deletions/rpo_httpcontract/secret", wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "wrong method", method: http.MethodGet, path: "/api/records/rec_httpcontract/permanent-delete-preview", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{name: "malformed JSON", method: http.MethodPost, path: "/api/records/rec_httpcontract/permanent-delete", body: `{`, idempotencyKeys: []string{mustRecordDeletionHandlerToken(t).Transport()}, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "token is header only", method: http.MethodPost, path: "/api/records/rec_httpcontract/permanent-delete", body: `{"reservation_id":"drs_httpcontract","deletion_request_token":"` + mustRecordDeletionHandlerToken(t).Transport() + `"}`, idempotencyKeys: []string{mustRecordDeletionHandlerToken(t).Transport()}, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "missing idempotency header", method: http.MethodPost, path: "/api/records/rec_httpcontract/permanent-delete", body: validRecordDeletionExecuteBody(), wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "invalid idempotency header", method: http.MethodPost, path: "/api/records/rec_httpcontract/permanent-delete", body: validRecordDeletionExecuteBody(), idempotencyKeys: []string{"drt1_invalid"}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "multiple idempotency headers", method: http.MethodPost, path: "/api/records/rec_httpcontract/permanent-delete", body: validRecordDeletionExecuteBody(), idempotencyKeys: []string{mustRecordDeletionHandlerToken(t).Transport(), mustRecordDeletionHandlerToken(t).Transport()}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "invalid reservation", method: http.MethodPost, path: "/api/records/rec_httpcontract/permanent-delete", body: `{"reservation_id":"bad"}`, idempotencyKeys: []string{mustRecordDeletionHandlerToken(t).Transport()}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
	}
	handler := RecordDeletions(application)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := recordDeletionHandlerRequest(t, actor, tt.method, tt.path, tt.body)
			for _, key := range tt.idempotencyKeys {
				request.Header.Add("Idempotency-Key", key)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			assertRecordDeletionNoStore(t, recorder)
			assertRecordDeletionError(t, recorder, tt.wantCode)
		})
	}
	if calls != 0 {
		t.Fatalf("application calls = %d, want zero", calls)
	}
}

func TestRecordDeletionsHandlerFailsClosedWithoutActorOrApplication(t *testing.T) {
	t.Parallel()

	t.Run("missing actor", func(t *testing.T) {
		handler := RecordDeletions(&recordDeletionHandlerApplicationStub{})
		request := httptest.NewRequest(http.MethodPost, "/api/records/rec_httpcontract/permanent-delete-preview", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		assertRecordDeletionError(t, recorder, "authorization_unavailable")
	})

	t.Run("missing preview application", func(t *testing.T) {
		handler := RecordDeletions(nil)
		request := recordDeletionHandlerRequest(t, mustRecordsHandlerActor(t), http.MethodPost, "/api/records/rec_httpcontract/permanent-delete-preview", "")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		assertRecordDeletionError(t, recorder, "deletion_safety_unavailable")
		if strings.Contains(recorder.Body.String(), "drt1_") {
			t.Fatalf("fail-closed preview returned a token: %s", recorder.Body.String())
		}
	})

	t.Run("missing status application", func(t *testing.T) {
		handler := RecordDeletions(nil)
		request := recordDeletionHandlerRequest(t, mustRecordsHandlerActor(t), http.MethodGet, "/api/record-deletions/rpo_httpcontract", "")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		assertRecordDeletionError(t, recorder, "deletion_status_unavailable")
	})
}

type recordDeletionHandlerApplicationStub struct {
	preview func(context.Context, recorddeletion.PreviewRequest) (recorddeletion.PreviewResult, error)
	execute func(context.Context, recorddeletion.ExecuteRequest) (recorddeletion.DeletionOperation, error)
	status  func(context.Context, recorddeletion.StatusRequest) (recorddeletion.DeletionOperation, error)
}

func (application *recordDeletionHandlerApplicationStub) Preview(
	ctx context.Context,
	request recorddeletion.PreviewRequest,
) (recorddeletion.PreviewResult, error) {
	if application.preview == nil {
		return recorddeletion.PreviewResult{}, errors.New("unexpected preview call")
	}
	return application.preview(ctx, request)
}

func (application *recordDeletionHandlerApplicationStub) Execute(
	ctx context.Context,
	request recorddeletion.ExecuteRequest,
) (recorddeletion.DeletionOperation, error) {
	if application.execute == nil {
		return recorddeletion.DeletionOperation{}, errors.New("unexpected execute call")
	}
	return application.execute(ctx, request)
}

func (application *recordDeletionHandlerApplicationStub) Status(
	ctx context.Context,
	request recorddeletion.StatusRequest,
) (recorddeletion.DeletionOperation, error) {
	if application.status == nil {
		return recorddeletion.DeletionOperation{}, errors.New("unexpected status call")
	}
	return application.status(ctx, request)
}

func recordDeletionHandlerRequest(
	t *testing.T,
	actor recordauth.ActorScope,
	method string,
	path string,
	body string,
) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	return request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
}

func mustRecordDeletionHandlerToken(t *testing.T) recordplatform.IssuedDeletionRequestTokenV1 {
	t.Helper()
	token, err := recordplatform.NewIssuedDeletionRequestTokenV1()
	if err != nil {
		t.Fatalf("NewIssuedDeletionRequestTokenV1() error = %v", err)
	}
	return token
}

func validRecordDeletionExecuteBody() string {
	return `{"reservation_id":"drs_httpcontract"}`
}

func recordDeletionHandlerOperation(state recorddeletion.DeletionState) recorddeletion.DeletionOperation {
	operation := recorddeletion.DeletionOperation{
		OperationID:   "rpo_httpcontract",
		ReservationID: "drs_httpcontract",
		Object: recordplatform.ObjectRef{
			ProjectID:  string(recordplatform.ProjectIDDefault),
			ObjectKind: "record",
			ObjectID:   "rec_httpcontract",
		},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      state,
		FenceEpoch: 7,
	}
	switch state {
	case recorddeletion.DeletionStateWitnessPending,
		recorddeletion.DeletionStateDeleteRequested,
		recorddeletion.DeletionStateFencePropagating,
		recorddeletion.DeletionStateReadFenced,
		recorddeletion.DeletionStateOnlinePurging,
		recorddeletion.DeletionStateRetryRequired:
		operation.LedgerSequence = 11
		operation.LedgerEntryHash = recordDeletionHandlerDigest(0x21)
	case recorddeletion.DeletionStateOnlinePurged:
		operation.LedgerSequence = 11
		operation.LedgerEntryHash = recordDeletionHandlerDigest(0x21)
		operation.ReceiptDigest = recordDeletionHandlerDigest(0x22)
	case recorddeletion.DeletionStateReleasePending, recorddeletion.DeletionStateNotCommitted:
		operation.LedgerSequence = 12
		operation.LedgerEntryHash = recordDeletionHandlerDigest(0x23)
		operation.ReleaseEpoch = 3
	}
	return operation
}

func recordDeletionHandlerDigest(seed byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = seed + byte(index)
	}
	return digest
}

func assertRecordDeletionNoStore(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", got)
	}
}

func assertRecordDeletionJSONKeys(t *testing.T, body []byte, keys ...string) {
	t.Helper()
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode JSON object: %v; body=%s", err, body)
	}
	if len(decoded) != len(keys) {
		t.Fatalf("JSON keys = %#v, want %#v", reflect.ValueOf(decoded).MapKeys(), keys)
	}
	for _, key := range keys {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("JSON missing key %q: %s", key, body)
		}
	}
}

func mustRecordDeletionJSONField(t *testing.T, body []byte, field string) []byte {
	t.Helper()
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode JSON object: %v; body=%s", err, body)
	}
	value, ok := decoded[field]
	if !ok {
		t.Fatalf("JSON missing key %q: %s", field, body)
	}
	return value
}

func assertRecordDeletionError(t *testing.T, recorder *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var response recordErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, recorder.Body.String())
	}
	if response.Code != wantCode || response.Message == "" || response.FieldErrors == nil || len(response.FieldErrors) != 0 || response.Recovery != nil {
		t.Fatalf("error response = %#v, want code %q with empty allowlists", response, wantCode)
	}
}

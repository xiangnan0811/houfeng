package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestSaveComparisonRecordHTTPRejectsForgedExpiredStaleAndClientItems(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	tests := []struct {
		name     string
		save     recordComparisonSaveService
		body     string
		want     int
		wantCode string
	}{
		{
			name:     "capability off",
			save:     nil,
			body:     comparisonSaveBody(draft, "cmp1.off.payload.mac"),
			want:     http.StatusNotFound,
			wantCode: "resource_not_found",
		},
		{
			name:     "forged",
			save:     recordComparisonSaveStub{err: evidence.ErrComparisonIntentInvalid},
			body:     comparisonSaveBody(draft, "cmp1.forged.payload.mac"),
			want:     http.StatusUnprocessableEntity,
			wantCode: "comparison_intent_invalid",
		},
		{
			name:     "expired",
			save:     recordComparisonSaveStub{err: evidence.ErrComparisonIntentExpired},
			body:     comparisonSaveBody(draft, "cmp1.expired.payload.mac"),
			want:     http.StatusUnprocessableEntity,
			wantCode: "comparison_intent_expired",
		},
		{
			name:     "stale",
			save:     recordComparisonSaveStub{err: evidence.ErrComparisonIntentStale},
			body:     comparisonSaveBody(draft, "cmp1.stale.payload.mac"),
			want:     http.StatusUnprocessableEntity,
			wantCode: "comparison_intent_stale",
		},
		{
			name:     "client evidence items",
			save:     recordComparisonSaveStub{},
			body:     `{"record_id":"rec_comparisonsave","draft_id":"` + draft.DraftID + `","draft_etag":` + strconvQuote(draft.ETag.String()) + `,"comparison_intent":"cmp1.x.y.z","evidence_items":[{"existing_snapshot_id":"evs_httpcontract"}]}`,
			want:     http.StatusBadRequest,
			wantCode: "invalid_request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := RecordsWithOptions(&recordsHandlerApplicationStub{
				preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) {
					return draft, nil
				},
				createRecord: func(context.Context, records.RecordCreateRequest) (records.RevisionCommitResult, error) {
					t.Fatal("CreateRecord must not run on rejected comparison save")
					return records.RevisionCommitResult{}, nil
				},
			}, RecordHandlerOptions{ComparisonSave: tt.save})
			request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", tt.body)
			request.Header.Set("Idempotency-Key", "comparison-save-"+tt.name)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.want {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tt.want)
			}
			var payload recordErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || payload.Code != tt.wantCode {
				t.Fatalf("error payload = %#v (%v), want %q", payload, err, tt.wantCode)
			}
		})
	}
}

func TestSaveComparisonRecordHTTPReplaysExpiredIntentOnlyWhenKeyCompleted(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	plan := mustHandlerComparisonSavePlan(t, "rec_comparisonsave")
	tests := []struct {
		name      string
		completed bool
		want      int
		wantCode  string
		create    bool
	}{
		{name: "new key", completed: false, want: http.StatusUnprocessableEntity, wantCode: "comparison_intent_expired"},
		{name: "same key replay", completed: true, want: http.StatusCreated, create: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created := false
			handler := RecordsWithOptions(&recordsHandlerApplicationStub{
				preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) {
					return draft, nil
				},
				createRecord: func(context.Context, records.RecordCreateRequest) (records.RevisionCommitResult, error) {
					created = true
					if !tt.create {
						t.Fatal("CreateRecord must not run for a new expired comparison save")
					}
					return records.RevisionCommitResult{
						RecordID: "rec_comparisonsave", RevisionID: "rrv_comparisonsave", RevisionNo: 1,
						LockVersion: 1, AuthorizationEpoch: 1, Lifecycle: records.LifecycleActive,
						Created: true, CommittedAt: time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC),
					}, nil
				},
			}, RecordHandlerOptions{
				ComparisonSave: recordComparisonSaveStub{plan: plan, err: evidence.ErrComparisonIntentExpired},
				CompletedIdempotency: func(context.Context, recordauth.ActorScope, recordplatform.OperationKind, string) (bool, error) {
					return tt.completed, nil
				},
			})
			request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", comparisonSaveBody(draft, "cmp1.expired.payload.mac"))
			request.Header.Set("Idempotency-Key", "comparison-save-"+tt.name)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.want {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tt.want)
			}
			if tt.wantCode != "" {
				var payload recordErrorResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || payload.Code != tt.wantCode {
					t.Fatalf("error payload = %#v (%v), want %q", payload, err, tt.wantCode)
				}
			}
			if created != tt.create {
				t.Fatalf("CreateRecord called = %v, want %v", created, tt.create)
			}
		})
	}
}

func TestSaveComparisonRecordHTTPPreparesCopiedEvidenceFromIntent(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	plan := mustHandlerComparisonSavePlan(t, "rec_comparisonsave")
	var got records.RecordCreateRequest
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{
		preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) {
			return draft, nil
		},
		createRecord: func(_ context.Context, request records.RecordCreateRequest) (records.RevisionCommitResult, error) {
			got = request
			return records.RevisionCommitResult{
				RecordID: request.RecordID, RevisionID: "rrv_comparisonsave", RevisionNo: 1,
				LockVersion: 1, AuthorizationEpoch: 1, Lifecycle: records.LifecycleActive,
				Created: true, CommittedAt: time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC),
			}, nil
		},
	}, RecordHandlerOptions{ComparisonSave: recordComparisonSaveStub{plan: plan}})
	request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", comparisonSaveBody(draft, "cmp1.valid.payload.mac"))
	request.Header.Set("Idempotency-Key", "comparison-save-valid")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	if got.RecordID != "rec_comparisonsave" || !reflectDeepEqual(got.EvidencePreparation.SnapshotIDs(), planSnapshotIDs(plan)) {
		t.Fatalf("CreateRecord preparation = %#v", got.EvidencePreparation.SnapshotIDs())
	}
	if len(got.EvidencePreparation.ComparisonSave().Copies) != 1 ||
		got.EvidencePreparation.ComparisonSave().Result.Snapshot().Envelope().Key != evidence.ComparisonResultV1Key() {
		t.Fatalf("CreateRecord comparison save = %#v", got.EvidencePreparation.ComparisonSave())
	}
}

func comparisonSaveBody(draft records.Draft, token string) string {
	return `{"record_id":"rec_comparisonsave","draft_id":"` + draft.DraftID + `","draft_etag":` + strconvQuote(draft.ETag.String()) + `,"comparison_intent":"` + token + `"}`
}

type recordComparisonSaveStub struct {
	plan evidence.ComparisonSavePreparation
	err  error
}

func (stub recordComparisonSaveStub) PrepareComparisonSave(
	_ context.Context,
	request evidence.ComparisonSaveRequest,
) (evidence.ComparisonSavePreparation, error) {
	if stub.err != nil && !(errors.Is(stub.err, evidence.ErrComparisonIntentExpired) && request.AllowExpiredReplay) {
		return evidence.ComparisonSavePreparation{}, stub.err
	}
	return stub.plan, nil
}

func mustHandlerComparisonSavePlan(t *testing.T, recordID string) evidence.ComparisonSavePreparation {
	t.Helper()
	source := storeHandlerComparisonSourceSnapshot(t)
	copy, err := evidence.NewPreparedComparisonCopy(recordID, "evs_comparisoncopy", "evs_comparisonsource", source)
	if err != nil {
		t.Fatalf("NewPreparedComparisonCopy() error = %v", err)
	}
	result, err := evidence.NewPreparedComparisonResult(recordID, "evs_comparisonresult", storeHandlerComparisonResultSnapshot(t))
	if err != nil {
		t.Fatalf("NewPreparedComparisonResult() error = %v", err)
	}
	return evidence.ComparisonSavePreparation{Token: "cmp1.valid.payload.mac", Copies: []evidence.PreparedComparisonCopy{copy}, Result: result}
}

func storeHandlerComparisonSourceSnapshot(t *testing.T) evidence.CanonicalSnapshot {
	t.Helper()
	return storeHandlerComparisonSnapshot(t, evidence.MonitoringProbeV2Key(), map[string]any{"metric_name": "latency_ms", "metric_value": "source"})
}

func storeHandlerComparisonResultSnapshot(t *testing.T) evidence.CanonicalSnapshot {
	t.Helper()
	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	source := storeHandlerComparisonSnapshot(t, evidence.MonitoringProbeV2Key(), map[string]any{"metric_name": "latency_ms", "metric_value": "env"})
	envelope := source.Envelope()
	envelope.Key = evidence.ComparisonResultV1Key()
	envelope.ProducerVersion = "comparison-result/v1"
	envelope.CalculationVersion = evidence.ComparisonCalculationVersion
	envelope.Units = evidence.UnitsSemantics{Status: evidence.UnitsNotApplicable, Reason: "comparison result metadata"}
	envelope.ActualPrecision = evidence.DurationSemantics{Applicable: false, Reason: "comparison result metadata"}
	envelope.BucketWidth = evidence.DurationSemantics{Applicable: false, Reason: "comparison result metadata"}
	envelope.Quality = evidence.Quality{Status: evidence.QualityComplete, SampleCount: 2}
	envelope.Redaction = nil
	envelope.CanonicalHash = [32]byte{}
	envelope.CanonicalSize = 0
	payload := map[string]any{
		"version": "comparison_result/v1", "baseline_index": 0, "alignment": string(evidence.CoverageActual),
		"requested_from": "2026-08-10T11:00:00Z", "requested_to": "2026-08-10T12:00:00Z",
		"tolerance_seconds": int64(60), "digest": "abababababababababababababababababababababababababababababababab",
		"registry_version": "evidence-kinds/v1", "calculation_version": evidence.ComparisonCalculationVersion,
		"items": []any{map[string]any{
			"original_snapshot_id": "evs_comparisonsource", "copied_snapshot_id": "evs_comparisoncopy",
			"hash": "1111111111111111111111111111111111111111111111111111111111111111",
			"kind": evidence.MonitoringProbeV2Key().String(), "revision_context": string(evidence.RevisionContextNotApplicable),
		}},
		"warnings": []any{}, "system_differences": []any{},
		"available_kinds": []any{evidence.MonitoringProbeV2Key().String()},
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(kind.Descriptor(), envelope, payload, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	return snapshot
}

func storeHandlerComparisonSnapshot(t *testing.T, key evidence.KindKey, payload map[string]any) evidence.CanonicalSnapshot {
	t.Helper()
	actor := mustRecordsHandlerActor(t)
	authorization := mustRecordsHandlerRecord(t, actor).Current.Input.Subjects()[0].CaptureAuthorization
	previewedAt := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: previewedAt.Add(-time.Hour), End: previewedAt}
	descriptor := evidence.Descriptor{
		Key: key,
		Fields: []evidence.FieldDefinition{
			{Path: "metric_name", Sensitivity: evidence.SensitivityNormal},
			{Path: "metric_value", Sensitivity: evidence.SensitivityNormal},
		},
		Conformance: evidence.ConformanceMetadata{
			CanonicalizationVersion: evidence.CanonicalizationVersionV1,
			ForbiddenCorpusVersion:  evidence.ForbiddenCorpusVersionV1,
			RendererVersion:         "renderer.v1",
			MaxCanonicalBytes:       evidence.MaxCanonicalPayloadBytes,
		},
	}
	envelope := evidence.SnapshotEnvelope{
		Key:           key,
		Subject:       evidence.IdentitySnapshot{Type: "target", ID: authorization.SourceID, Fields: map[string]string{"display_name": "Evidence target"}},
		Source:        evidence.IdentitySnapshot{Type: string(authorization.Kind), ID: authorization.SourceID, Fields: map[string]string{"display_name": "Evidence target"}},
		Authorization: authorization, RequestedWindow: window, ActualWindow: window,
		ObservedAt: previewedAt, CapturedAt: previewedAt.Add(time.Minute), ReferencedAt: previewedAt.Add(2 * time.Minute),
		SourceRevision: "revision-1", SourceWatermark: "watermark-1", SourceDigest: [32]byte{1},
		ProducerVersion: "producer-1", CalculationVersion: "calculation-1",
		Units:           evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: map[string]string{"latency_ms": "ms"}},
		Quality:         evidence.Quality{Status: evidence.QualityComplete, SampleCount: 60, BucketCount: 60, DataPointCount: 60},
		Sensitivity:     evidence.SensitivityNormal,
		ActualPrecision: evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		BucketWidth:     evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		QuotaOutcome:    evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention:       evidence.RetentionSemantics{Immutable: true, Scope: evidence.RetentionScopeRecordRevision, SourceDeletion: evidence.SourceDeletionSnapshotRetained},
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(descriptor, envelope, payload, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	return snapshot
}

func planSnapshotIDs(plan evidence.ComparisonSavePreparation) []string {
	ids := make([]string, 0, len(plan.Copies)+1)
	for _, copy := range plan.Copies {
		ids = append(ids, copy.SnapshotID())
	}
	if !plan.Result.Empty() {
		ids = append(ids, plan.Result.SnapshotID())
	}
	return ids
}

func reflectDeepEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

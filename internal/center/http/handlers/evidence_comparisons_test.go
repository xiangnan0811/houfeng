package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

func TestEvidenceHandlerComparisonCandidatesMatrix(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	start := time.Date(2026, time.August, 10, 11, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	hash := sha256.Sum256([]byte("evs_candidate"))
	application := &evidenceHandlerApplicationStub{
		candidates: func(_ context.Context, request evidence.ComparisonCandidateRequest) (evidence.ComparisonCandidateResult, error) {
			if request.Actor.CanonicalHash() != actor.CanonicalHash() ||
				len(request.Subjects) != 2 || request.Subjects[0].ID != "vps_0123456789abcdef" ||
				request.RequestedWindow != (evidence.TimeWindow{Start: start, End: end}) ||
				len(request.Kinds) != 1 || request.Kinds[0] != evidence.MonitoringHostV1Key() {
				t.Fatalf("ResolveComparisonCandidates request = %#v", request)
			}
			return evidence.ComparisonCandidateResult{
				Subjects: request.Subjects,
				Candidates: []evidence.ComparisonCandidate{{
					Subject: request.Subjects[0], SnapshotID: "evs_candidate", RecordID: "rec_candidate",
					RevisionIDs: []string{"rrv_candidate"}, Kind: evidence.MonitoringHostV1Key(), CanonicalHash: hash,
					RequestedWindow: request.RequestedWindow, ActualWindow: request.RequestedWindow,
					Quality: evidence.Quality{Status: evidence.QualityComplete}, CapturedAt: end,
					Recommendation: evidence.RecommendationNearestWindow,
				}},
			}, nil
		},
	}
	handler := EvidenceWithOptions(application, EvidenceHandlerOptions{ComparisonEnabled: true})
	request := evidenceHandlerRequest(t, actor, http.MethodPost, "/api/evidence/comparison-candidates", strings.NewReader(
		`{"subjects":[{"kind":"vps","id":"vps_0123456789abcdef"},{"kind":"vps","id":"vps_0123456789abcde0"}],"requested_window":{"start":"2026-08-10T11:00:00Z","end":"2026-08-10T12:00:00Z"},"kinds":[{"kind":"monitoring.host","schema_version":1}]}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, required := range []string{
		`"snapshot_id":"evs_candidate"`, `"record_id":"rec_candidate"`, `"revision_ids":["rrv_candidate"]`,
		`"kind":"monitoring.host"`, `"schema_version":1`, `"canonical_hash":"` + hex.EncodeToString(hash[:]) + `"`,
		`"quality_status":"complete"`, `"recommendation":"nearest_window"`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("candidates response missing %s: %s", required, body)
		}
	}
	for _, forbidden := range []string{`"canonical_payload":`, `"capture_authorization":`, `"authorization_digest":`, `"payload":`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("candidates response leaked %q: %s", forbidden, body)
		}
	}
}

func TestEvidenceHandlerComparisonCandidatesStayAbsentWhenCapabilityOff(t *testing.T) {
	t.Parallel()

	calls := 0
	handler := EvidenceWithOptions(&evidenceHandlerApplicationStub{
		candidates: func(context.Context, evidence.ComparisonCandidateRequest) (evidence.ComparisonCandidateResult, error) {
			calls++
			return evidence.ComparisonCandidateResult{}, nil
		},
	}, EvidenceHandlerOptions{})
	request := evidenceHandlerRequest(t, mustRecordsHandlerActor(t), http.MethodPost, "/api/evidence/comparison-candidates", strings.NewReader(
		`{"subjects":[{"kind":"vps","id":"vps_0123456789abcdef"},{"kind":"vps","id":"vps_0123456789abcde0"}],"requested_window":{"start":"2026-08-10T11:00:00Z","end":"2026-08-10T12:00:00Z"}}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"resource_not_found"`) {
		t.Fatalf("status/body = %d %s, want opaque 404", recorder.Code, recorder.Body.String())
	}
	if calls != 0 {
		t.Fatalf("comparison application calls = %d, want 0", calls)
	}
}

func TestEvidenceHandlerComparisonCandidatesFailClosed(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	validBody := `{"subjects":[{"kind":"vps","id":"vps_0123456789abcdef"},{"kind":"vps","id":"vps_0123456789abcde0"}],"requested_window":{"start":"2026-08-10T11:00:00Z","end":"2026-08-10T12:00:00Z"}}`
	tests := []struct {
		name       string
		method     string
		body       string
		resolveErr error
		wantStatus int
		wantCode   string
	}{
		{name: "missing subject", method: http.MethodPost, body: validBody, resolveErr: evidence.ErrComparisonSubjectNotFound, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "invalid selection", method: http.MethodPost, body: validBody, resolveErr: evidence.ErrInvalidComparisonSelection, wantStatus: http.StatusUnprocessableEntity, wantCode: "comparison_selection_invalid"},
		{name: "denied", method: http.MethodPost, body: validBody, resolveErr: recordauth.ErrDenied, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "method", method: http.MethodGet, body: "", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{name: "oversized", method: http.MethodPost, body: `{"subjects":[],"padding":"` + strings.Repeat("x", DefaultJSONBodyLimit) + `"}`, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "request_too_large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			application := &evidenceHandlerApplicationStub{
				candidates: func(context.Context, evidence.ComparisonCandidateRequest) (evidence.ComparisonCandidateResult, error) {
					calls++
					return evidence.ComparisonCandidateResult{}, test.resolveErr
				},
			}
			handler := EvidenceWithOptions(application, EvidenceHandlerOptions{ComparisonEnabled: true})
			var body *strings.Reader
			if test.body != "" {
				body = strings.NewReader(test.body)
			}
			request := evidenceHandlerRequest(t, actor, test.method, "/api/evidence/comparison-candidates", body)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("status/body = %d %s, want %d/%s", recorder.Code, recorder.Body.String(), test.wantStatus, test.wantCode)
			}
			var payload map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if _, ok := payload["candidates"]; ok {
				t.Fatalf("error body leaked candidates: %s", recorder.Body.String())
			}
			if test.wantStatus == http.StatusRequestEntityTooLarge && calls != 0 {
				t.Fatalf("oversized body still reached application (%d calls)", calls)
			}
		})
	}
}

func TestEvidenceHandlerFixedComparisonSummaryAndDetail(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	digest := sha256.Sum256([]byte("fixed-compare"))
	now := time.Date(2026, time.August, 20, 8, 0, 0, 0, time.UTC)
	application := &evidenceHandlerApplicationStub{
		compare: func(_ context.Context, request evidence.ComparisonEvaluateRequest) (evidence.ComparisonEvaluateOutput, error) {
			if request.Actor.CanonicalHash() != actor.CanonicalHash() ||
				len(request.Items) != 2 || request.Items[0].SnapshotID == nil ||
				*request.Items[0].SnapshotID != "evs_fixeda" ||
				request.Items[1].Revision == nil ||
				request.Items[1].Revision.RecordID != "rec_fixedb" ||
				request.Alignment != evidence.CoverageActual {
				t.Fatalf("EvaluateFixedComparison request = %#v", request)
			}
			output := evidence.ComparisonEvaluateOutput{
				Digest: digest,
				Items: []evidence.ResolvedComparisonItem{
					{SnapshotID: "evs_fixeda", Hash: digest, Kind: evidence.CommandAuditV1Key(), RevisionContext: evidence.RevisionContextNotApplicable},
					{
						SnapshotID: "evs_fixedb", Hash: digest, Kind: evidence.CommandAuditV1Key(),
						RevisionContext: evidence.RevisionContextBound,
						Revision:        &evidence.RevisionMetadataSnapshot{RecordType: "incident", ImpactLevel: "high"},
					},
				},
				AvailableKinds:  []evidence.KindKey{evidence.CommandAuditV1Key()},
				SaveEligibility: evidence.ComparisonSaveEligibility{Eligible: true, Blockers: []evidence.ComparisonReason{}},
				Intent:          &evidence.ComparisonIntent{Token: "cmp-token", KeyID: "cmp_test", IssuedAt: now, ExpiresAt: now.Add(evidence.ComparisonIntentTTL)},
			}
			if request.Detail != nil {
				output.Pairwise = []evidence.Comparison{{
					Key: request.Detail.Kind, Compatible: true, Reason: "compatible_test", Values: map[string]any{"equal": true},
				}}
			}
			return output, nil
		},
	}
	handler := EvidenceWithOptions(application, EvidenceHandlerOptions{ComparisonEnabled: true})

	summary := evidenceHandlerRequest(t, actor, http.MethodPost, "/api/evidence/comparisons", strings.NewReader(
		`{"items":[{"snapshot_id":"evs_fixeda"},{"record_id":"rec_fixedb","revision_id":"rrv_fixedb"}],"baseline_index":0,"alignment":"actual_coverage","requested_window":{"start":"2026-08-10T11:00:00Z","end":"2026-08-10T12:00:00Z"},"tolerance_seconds":60}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, summary)
	if recorder.Code != http.StatusOK {
		t.Fatalf("summary status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, required := range []string{
		`"revision_context":"not_applicable"`, `"impact_level":"high"`, `"save_eligibility"`,
		`"comparison_intent"`, `"key_id":"cmp_test"`, `"kind":"command.audit"`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("summary response missing %s: %s", required, body)
		}
	}
	for _, forbidden := range []string{`"payload":`, `"canonical_payload":`, `"markdown":`, `"capture_authorization":`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("summary leaked %s: %s", forbidden, body)
		}
	}

	detail := evidenceHandlerRequest(t, actor, http.MethodPost, "/api/evidence/comparisons", strings.NewReader(
		`{"items":[{"snapshot_id":"evs_fixeda"},{"record_id":"rec_fixedb","revision_id":"rrv_fixedb"}],"baseline_index":0,"alignment":"actual_coverage","requested_window":{"start":"2026-08-10T11:00:00Z","end":"2026-08-10T12:00:00Z"},"detail":{"kind":"command.audit","schema_version":1}}`,
	))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, detail)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"pairwise"`) {
		t.Fatalf("detail status/body = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestEvidenceHandlerFixedComparisonStayAbsentWhenCapabilityOff(t *testing.T) {
	t.Parallel()

	calls := 0
	handler := EvidenceWithOptions(&evidenceHandlerApplicationStub{
		compare: func(context.Context, evidence.ComparisonEvaluateRequest) (evidence.ComparisonEvaluateOutput, error) {
			calls++
			return evidence.ComparisonEvaluateOutput{}, nil
		},
	}, EvidenceHandlerOptions{})
	request := evidenceHandlerRequest(t, mustRecordsHandlerActor(t), http.MethodPost, "/api/evidence/comparisons", strings.NewReader(
		`{"items":[{"snapshot_id":"evs_fixeda"},{"snapshot_id":"evs_fixedb"}],"baseline_index":0,"alignment":"actual_coverage","requested_window":{"start":"2026-08-10T11:00:00Z","end":"2026-08-10T12:00:00Z"}}`,
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"resource_not_found"`) {
		t.Fatalf("status/body = %d %s, want opaque 404", recorder.Code, recorder.Body.String())
	}
	if calls != 0 {
		t.Fatalf("comparison application calls = %d, want 0", calls)
	}
}

func TestEvidenceHandlerFixedComparisonFailClosed(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	validBody := `{"items":[{"snapshot_id":"evs_fixeda"},{"snapshot_id":"evs_fixedb"}],"baseline_index":0,"alignment":"actual_coverage","requested_window":{"start":"2026-08-10T11:00:00Z","end":"2026-08-10T12:00:00Z"}}`
	tests := []struct {
		name       string
		method     string
		body       string
		resolveErr error
		wantStatus int
		wantCode   string
	}{
		{name: "missing selection", method: http.MethodPost, body: validBody, resolveErr: evidence.ErrComparisonSelectionNotFound, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "invalid selection", method: http.MethodPost, body: validBody, resolveErr: evidence.ErrInvalidComparisonSelection, wantStatus: http.StatusUnprocessableEntity, wantCode: "comparison_selection_invalid"},
		{name: "incomplete", method: http.MethodPost, body: validBody, resolveErr: evidence.ErrComparisonSelectionIncomplete, wantStatus: http.StatusUnprocessableEntity, wantCode: "comparison_selection_incomplete"},
		{name: "admission saturated", method: http.MethodPost, body: validBody, resolveErr: evidence.ErrComparisonCapacityExhausted, wantStatus: http.StatusTooManyRequests, wantCode: "comparison_capacity_exhausted"},
		{name: "admission over budget", method: http.MethodPost, body: validBody, resolveErr: evidence.ErrComparisonRequestMemoryLimit, wantStatus: http.StatusUnprocessableEntity, wantCode: "comparison_request_memory_limit"},
		{name: "denied", method: http.MethodPost, body: validBody, resolveErr: recordauth.ErrDenied, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "method", method: http.MethodGet, body: "", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{name: "oversized", method: http.MethodPost, body: `{"items":[],"padding":"` + strings.Repeat("x", DefaultJSONBodyLimit) + `"}`, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "request_too_large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			application := &evidenceHandlerApplicationStub{
				compare: func(context.Context, evidence.ComparisonEvaluateRequest) (evidence.ComparisonEvaluateOutput, error) {
					calls++
					return evidence.ComparisonEvaluateOutput{}, test.resolveErr
				},
			}
			handler := EvidenceWithOptions(application, EvidenceHandlerOptions{ComparisonEnabled: true})
			var body *strings.Reader
			if test.body != "" {
				body = strings.NewReader(test.body)
			}
			request := evidenceHandlerRequest(t, actor, test.method, "/api/evidence/comparisons", body)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("status/body = %d %s, want %d/%s", recorder.Code, recorder.Body.String(), test.wantStatus, test.wantCode)
			}
			if test.wantStatus == http.StatusRequestEntityTooLarge && calls != 0 {
				t.Fatalf("oversized body still reached application (%d calls)", calls)
			}
		})
	}
}

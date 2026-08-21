package handlers

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
)

func TestEvidenceHandlerCapturePreviewAndReadMatrix(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	start := time.Date(2026, time.August, 16, 1, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	preview := evidence.Preview{
		IntentID: "evi_0123456789abcdef01234567",
		Key:      evidence.MonitoringHostV1Key(),
		Selection: evidence.Selection{
			Key: evidence.MonitoringHostV1Key(), SourceType: "monitoring_instance",
			SourceID: "mi_0123456789abcdef", RequestedWindow: evidence.TimeWindow{Start: start, End: end},
			Metrics: []string{"cpu_usage_pct"}, Precision: time.Minute, SensitiveTopologyFields: []string{},
		},
		Subject:                 evidence.IdentitySnapshot{Type: "vps", ID: "vps_0123456789abcdef", Fields: map[string]string{"display_name": "node-a"}},
		Source:                  evidence.IdentitySnapshot{Type: "monitoring_instance", ID: "mi_0123456789abcdef", Fields: map[string]string{"display_name": "monitor-a"}},
		RequestedWindow:         evidence.TimeWindow{Start: start, End: end},
		ActualWindow:            evidence.TimeWindow{Start: start, End: end},
		ObservedAt:              end,
		SourceRevision:          "revision-7",
		SourceWatermark:         "2026-08-16T02:00:00Z",
		ProducerVersion:         "collector/v1",
		CalculationVersion:      "monitoring-evidence/v1",
		Units:                   evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: map[string]string{"cpu_usage_pct": "percent"}},
		Quality:                 evidence.Quality{Status: evidence.QualityComplete, SampleCount: 60, BucketCount: 60, DataPointCount: 60},
		Sensitivity:             evidence.SensitivityNormal,
		ActualPrecision:         evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		BucketWidth:             evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		QuotaOutcome:            evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention:               evidence.RetentionSemantics{Immutable: true, Scope: evidence.RetentionScopeRecordRevision, SourceDeletion: evidence.SourceDeletionSnapshotRetained},
		Redaction:               []evidence.FieldDecision{{Path: "payload.buckets", Sensitivity: evidence.SensitivityNormal, Action: evidence.RedactionActionIncluded}},
		EstimatedCanonicalBytes: 1024,
		SourceDigest:            sha256.Sum256([]byte("source")),
		RendererVersion:         "monitoring_host/v1",
		PreviewedAt:             end.Add(time.Minute),
		ValidUntil:              end.Add(time.Minute).Add(evidence.CaptureIntentTTL),
	}
	application := &evidenceHandlerApplicationStub{
		capture: func(_ context.Context, request evidence.CapturePreviewRequest) (evidence.CapturePreviewResult, error) {
			if request.Actor.CanonicalHash() != actor.CanonicalHash() || request.RecordID != "rec_httpcontract" ||
				request.SnapshotID != "evs_httpcontract" || request.Selection.Key != evidence.MonitoringHostV1Key() {
				t.Fatalf("CapturePreview request = %#v", request)
			}
			return evidence.CapturePreviewResult{RecordID: request.RecordID, SnapshotID: request.SnapshotID, Preview: preview}, nil
		},
		read: func(_ context.Context, request evidence.ReadSnapshotRequest) (evidence.ReadSnapshotResult, error) {
			if request.Actor.CanonicalHash() != actor.CanonicalHash() || request.SnapshotID != "evs_httpcontract" {
				t.Fatalf("ReadSnapshot request = %#v", request)
			}
			return evidence.ReadSnapshotResult{
				RecordID: "rec_httpcontract", SnapshotID: request.SnapshotID,
				Envelope: evidence.SnapshotEnvelope{
					Key: evidence.MonitoringHostV1Key(), Subject: preview.Subject, Source: preview.Source,
					RequestedWindow: preview.RequestedWindow, ActualWindow: preview.ActualWindow,
					ObservedAt: preview.ObservedAt, CapturedAt: end.Add(2 * time.Minute), ReferencedAt: end.Add(3 * time.Minute),
					SourceRevision: preview.SourceRevision, SourceWatermark: preview.SourceWatermark,
					ProducerVersion: preview.ProducerVersion, CalculationVersion: preview.CalculationVersion,
					Units: preview.Units, Quality: preview.Quality, Sensitivity: preview.Sensitivity,
					ActualPrecision: preview.ActualPrecision, BucketWidth: preview.BucketWidth,
					QuotaOutcome: preview.QuotaOutcome, Retention: preview.Retention, Redaction: preview.Redaction,
				},
				Summary: evidence.Summary{
					Key: evidence.MonitoringHostV1Key(), RendererVersion: "monitoring_host/v1", Title: "Host evidence",
					ReadModel: map[string]any{"version": "monitoring_host_read_model/v1", "buckets": []any{}},
				},
				SourceAvailable: true,
			}, nil
		},
	}
	handler := EvidenceWithOptions(application, EvidenceHandlerOptions{
		NewRecordID:   func() (string, error) { return "rec_generated", nil },
		NewSnapshotID: func() (string, error) { return "evs_httpcontract", nil },
	})

	t.Run("preview", func(t *testing.T) {
		request := evidenceHandlerRequest(t, actor, http.MethodPost, "/api/evidence/capture-previews", strings.NewReader(
			`{"record_id":"rec_httpcontract","kind":"monitoring.host","schema_version":1,"source_type":"monitoring_instance","source_id":"mi_0123456789abcdef","requested_window":{"start":"2026-08-16T01:00:00Z","end":"2026-08-16T02:00:00Z"},"metrics":["cpu_usage_pct"],"precision_seconds":60,"sensitive_topology_fields":[]}`,
		))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
		}
		body := recorder.Body.String()
		for _, required := range []string{`"record_id":"rec_httpcontract"`, `"snapshot_id":"evs_httpcontract"`, `"capture_intent_id":"evi_0123456789abcdef01234567"`, `"renderer_version":"monitoring_host/v1"`, `"sample_count":60`, `"values":{"cpu_usage_pct":"percent"}`} {
			if !strings.Contains(body, required) {
				t.Fatalf("preview response missing %s: %s", required, body)
			}
		}
		assertEvidenceResponseAllowlist(t, body)
	})

	t.Run("read", func(t *testing.T) {
		request := evidenceHandlerRequest(t, actor, http.MethodGet, "/api/evidence/evs_httpcontract", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
		}
		body := recorder.Body.String()
		for _, required := range []string{
			`"snapshot_id":"evs_httpcontract"`, `"source_available":true`,
			`"version":"monitoring_host_read_model/v1"`, `"actual_precision_seconds":60`,
			`"bucket_width_seconds":60`, `"quota":{"status":"allowed"}`,
			`"retention":{"immutable":true,"scope":"record_revision"`,
			`"redaction":[{"path":"payload.buckets","sensitivity":"normal","action":"included"}]`,
		} {
			if !strings.Contains(body, required) {
				t.Fatalf("read response missing %s: %s", required, body)
			}
		}
		assertEvidenceResponseAllowlist(t, body)
	})
}

func TestEvidenceHandlerFailClosedMatrix(t *testing.T) {
	t.Parallel()
	actor := mustRecordsHandlerActor(t)
	tests := []struct {
		name       string
		path       string
		method     string
		body       string
		captureErr error
		readErr    error
		wantStatus int
		wantCode   string
	}{
		{name: "unknown kind", path: "/api/evidence/capture-previews", method: http.MethodPost, body: `{"record_id":"rec_httpcontract","kind":"unknown.kind","schema_version":1,"source_type":"monitoring_instance","source_id":"mi_0123456789abcdef","requested_window":{"start":"2026-08-16T01:00:00Z","end":"2026-08-16T02:00:00Z"},"metrics":[],"precision_seconds":0,"sensitive_topology_fields":[]}`, captureErr: evidence.ErrKindNotRegistered, wantStatus: http.StatusServiceUnavailable, wantCode: "evidence_kind_unavailable"},
		{name: "unstable source", path: "/api/evidence/capture-previews", method: http.MethodPost, body: `{"record_id":"rec_httpcontract","kind":"monitoring.host","schema_version":1,"source_type":"monitoring_instance","source_id":"mi_0123456789abcdef","requested_window":{"start":"2026-08-16T01:00:00Z","end":"2026-08-16T02:00:00Z"},"metrics":["cpu_usage_pct"],"precision_seconds":60,"sensitive_topology_fields":[]}`, captureErr: evidence.ErrSourceUnstable, wantStatus: http.StatusConflict, wantCode: "evidence_source_unstable"},
		{name: "preview stale", path: "/api/evidence/capture-previews", method: http.MethodPost, body: `{"record_id":"rec_httpcontract","kind":"monitoring.host","schema_version":1,"source_type":"monitoring_instance","source_id":"mi_0123456789abcdef","requested_window":{"start":"2026-08-16T01:00:00Z","end":"2026-08-16T02:00:00Z"},"metrics":["cpu_usage_pct"],"precision_seconds":60,"sensitive_topology_fields":[]}`, captureErr: evidence.ErrPreviewStale, wantStatus: http.StatusConflict, wantCode: "evidence_preview_stale"},
		{name: "record source permission intersection", path: "/api/evidence/evs_httpcontract", method: http.MethodGet, readErr: recordauth.ErrDenied, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := &evidenceHandlerApplicationStub{
				capture: func(context.Context, evidence.CapturePreviewRequest) (evidence.CapturePreviewResult, error) {
					return evidence.CapturePreviewResult{}, test.captureErr
				},
				read: func(context.Context, evidence.ReadSnapshotRequest) (evidence.ReadSnapshotResult, error) {
					return evidence.ReadSnapshotResult{}, test.readErr
				},
			}
			handler := EvidenceWithOptions(application, EvidenceHandlerOptions{NewSnapshotID: func() (string, error) { return "evs_httpcontract", nil }})
			request := evidenceHandlerRequest(t, actor, test.method, test.path, strings.NewReader(test.body))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("status/body = %d %s, want %d/%s", recorder.Code, recorder.Body.String(), test.wantStatus, test.wantCode)
			}
		})
	}
}

type evidenceHandlerApplicationStub struct {
	capture    func(context.Context, evidence.CapturePreviewRequest) (evidence.CapturePreviewResult, error)
	read       func(context.Context, evidence.ReadSnapshotRequest) (evidence.ReadSnapshotResult, error)
	candidates func(context.Context, evidence.ComparisonCandidateRequest) (evidence.ComparisonCandidateResult, error)
	compare    func(context.Context, evidence.ComparisonEvaluateRequest) (evidence.ComparisonEvaluateOutput, error)
}

func (stub *evidenceHandlerApplicationStub) CapturePreview(ctx context.Context, request evidence.CapturePreviewRequest) (evidence.CapturePreviewResult, error) {
	return stub.capture(ctx, request)
}

func (stub *evidenceHandlerApplicationStub) ReadSnapshot(ctx context.Context, request evidence.ReadSnapshotRequest) (evidence.ReadSnapshotResult, error) {
	return stub.read(ctx, request)
}

func (stub *evidenceHandlerApplicationStub) ResolveComparisonCandidates(ctx context.Context, request evidence.ComparisonCandidateRequest) (evidence.ComparisonCandidateResult, error) {
	if stub.candidates == nil {
		return evidence.ComparisonCandidateResult{}, evidence.ErrEvidenceServiceUnavailable
	}
	return stub.candidates(ctx, request)
}

func (stub *evidenceHandlerApplicationStub) EvaluateFixedComparison(ctx context.Context, request evidence.ComparisonEvaluateRequest) (evidence.ComparisonEvaluateOutput, error) {
	if stub.compare == nil {
		return evidence.ComparisonEvaluateOutput{}, evidence.ErrEvidenceServiceUnavailable
	}
	return stub.compare(ctx, request)
}

func evidenceHandlerRequest(t *testing.T, actor recordauth.ActorScope, method, path string, body *strings.Reader) *http.Request {
	t.Helper()
	var request *http.Request
	if body == nil {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, body)
	}
	return request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
}

func assertEvidenceResponseAllowlist(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{`"canonical_payload":`, `"canonical_bytes":`, `"capture_authorization":`, `"authorization_digest":`, `"source_digest":`, `"preview_digest":`, `"search_text":`, `"Status":`, `"Values":`, `"SampleCount":`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("evidence response leaked %q: %s", forbidden, body)
		}
	}
}

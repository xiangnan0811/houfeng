package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestRegistryConformanceExercisesPreviewCaptureAndAuthorize(t *testing.T) {
	stub, fixture := testConformingKind(t)
	if err := VerifyKindConformance(context.Background(), stub, fixture); err != nil {
		t.Fatalf("VerifyKindConformance() error = %v", err)
	}
	if !stub.selectionChecked || !stub.previewCalled || !stub.authorizeCalled {
		t.Fatalf("conformance calls: validate=%t preview=%t authorize=%t", stub.selectionChecked, stub.previewCalled, stub.authorizeCalled)
	}
}

func TestRegistryConformanceRejectsPreviewCaptureDrift(t *testing.T) {
	stub, fixture := testConformingKind(t)
	envelope := testEnvelope(t, stub.descriptor.Key)
	envelope.ActualWindow.Start = envelope.ActualWindow.Start.Add(time.Minute)
	snapshot, _, err := NewCanonicalSnapshot(stub.descriptor, envelope, map[string]any{"metric_name": "latency_ms"}, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot(drift) error = %v", err)
	}
	stub.snapshot = snapshot
	if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrKindConformance) {
		t.Fatalf("VerifyKindConformance(drift) error = %v, want ErrKindConformance", err)
	}
}

func TestRegistryConformanceRejectsTombstonedAuthorizationForNewCapture(t *testing.T) {
	stub, fixture := testConformingKind(t)
	lastLiveScope := *stub.authorization.CurrentScope
	tombstoned, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:       stub.authorization.Version,
		Kind:          stub.authorization.Kind,
		SourceID:      stub.authorization.SourceID,
		State:         recordauth.SourceStateTombstoned,
		CaptureScope:  stub.authorization.CaptureScope,
		FinalFloor:    &lastLiveScope,
		LastLiveScope: &lastLiveScope,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	envelope := stub.snapshot.Envelope()
	envelope.Authorization = tombstoned
	snapshot, _, err := NewCanonicalSnapshot(
		stub.descriptor,
		envelope,
		map[string]any{"metric_name": "latency_ms"},
		RedactionNormalOnly,
	)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	stub.authorization = tombstoned
	stub.snapshot = snapshot

	if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrKindConformance) {
		t.Fatalf("VerifyKindConformance() error = %v, want ErrKindConformance", err)
	}
}

func TestRegistryConformanceRejectsMislabeledSensitivePreviewBeforeCapture(t *testing.T) {
	stub, fixture := testConformingKind(t)
	stub.preview.Redaction = []FieldDecision{
		{Path: "endpoint", Sensitivity: SensitivitySensitiveTopology, Action: RedactionActionIncluded},
		{Path: "metric_name", Sensitivity: SensitivityNormal, Action: RedactionActionIncluded},
	}
	stub.preview.Sensitivity = SensitivityNormal
	if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrKindConformance) {
		t.Fatalf("VerifyKindConformance(mislabeled preview) error = %v, want ErrKindConformance", err)
	}
	if stub.captureCalled {
		t.Fatal("VerifyKindConformance called Capture for a mislabeled sensitive preview")
	}
}

func TestRegistryConformanceNormalizesMaskedPreviewForCapture(t *testing.T) {
	stub, fixture := testConformingKind(t)
	envelope := testEnvelope(t, stub.descriptor.Key)
	snapshot, _, err := NewCanonicalSnapshot(stub.descriptor, envelope, map[string]any{
		"endpoint":    "https://example.com/health",
		"metric_name": "latency_ms",
	}, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot(masked preview capture) error = %v", err)
	}
	stub.preview.Redaction = []FieldDecision{
		{Path: "endpoint", Sensitivity: SensitivitySensitiveTopology, Action: RedactionActionMasked},
		{Path: "metric_name", Sensitivity: SensitivityNormal, Action: RedactionActionIncluded},
	}
	stub.preview.EstimatedCanonicalBytes = snapshot.Size()
	stub.snapshot = snapshot

	if err := VerifyKindConformance(context.Background(), stub, fixture); err != nil {
		t.Fatalf("VerifyKindConformance(masked preview) error = %v", err)
	}
	if got := decisionAction(t, RedactionReport{Decisions: stub.preview.Redaction}, "endpoint"); got != RedactionActionMasked {
		t.Fatalf("preview endpoint action = %q, want %q", got, RedactionActionMasked)
	}
	if got := decisionAction(t, RedactionReport{Decisions: snapshot.Envelope().Redaction}, "endpoint"); got != RedactionActionStripped {
		t.Fatalf("capture endpoint action = %q, want %q", got, RedactionActionStripped)
	}
	if bytes := string(snapshot.Bytes()); strings.Contains(bytes, "endpoint") || strings.Contains(bytes, "[redacted]") {
		t.Fatalf("capture payload retained masked field: %s", bytes)
	}
}

func TestRegistryConformanceNormalizesForbiddenPreviewForCapture(t *testing.T) {
	stub, fixture := testConformingKind(t)
	envelope := testEnvelope(t, stub.descriptor.Key)
	envelope.Redaction = []FieldDecision{
		{Path: "metric_name", Sensitivity: SensitivityNormal, Action: RedactionActionIncluded},
		{Path: "stdout", Sensitivity: SensitivityForbidden, Action: RedactionActionStripped},
	}
	snapshot, captureReport, err := NewCanonicalSnapshot(stub.descriptor, envelope, map[string]any{"metric_name": "latency_ms"}, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot(forbidden preview capture) error = %v", err)
	}
	stub.preview.Redaction = []FieldDecision{
		{Path: "metric_name", Sensitivity: SensitivityNormal, Action: RedactionActionIncluded},
		{Path: "stdout", Sensitivity: SensitivityForbidden, Action: RedactionActionForbidden},
	}
	stub.snapshot = snapshot

	if err := VerifyKindConformance(context.Background(), stub, fixture); err != nil {
		t.Fatalf("VerifyKindConformance(forbidden preview) error = %v", err)
	}
	if got := decisionAction(t, RedactionReport{Decisions: stub.preview.Redaction}, "stdout"); got != RedactionActionForbidden {
		t.Fatalf("preview stdout action = %q, want %q", got, RedactionActionForbidden)
	}
	if got := decisionAction(t, RedactionReport{Decisions: snapshot.Envelope().Redaction}, "stdout"); got != RedactionActionStripped {
		t.Fatalf("capture stdout action = %q, want %q", got, RedactionActionStripped)
	}
	if got := decisionAction(t, captureReport, "stdout"); got != RedactionActionStripped {
		t.Fatalf("capture report stdout action = %q, want %q", got, RedactionActionStripped)
	}
	if strings.Contains(string(snapshot.Bytes()), "stdout") {
		t.Fatalf("capture payload retained forbidden field: %s", snapshot.Bytes())
	}
}

func TestRegistryConformanceRejectsCaptureDispositionMismatch(t *testing.T) {
	t.Run("selected masked field missing from capture metadata", func(t *testing.T) {
		stub, fixture := testConformingKind(t)
		stub.preview.Redaction = []FieldDecision{
			{Path: "endpoint", Sensitivity: SensitivitySensitiveTopology, Action: RedactionActionMasked},
			{Path: "metric_name", Sensitivity: SensitivityNormal, Action: RedactionActionIncluded},
		}
		if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrKindConformance) {
			t.Fatalf("VerifyKindConformance(missing masked disposition) error = %v, want ErrKindConformance", err)
		}
	})

	t.Run("included topology captured as stripped", func(t *testing.T) {
		stub, fixture := testConformingKind(t)
		envelope := testEnvelope(t, stub.descriptor.Key)
		snapshot, _, err := NewCanonicalSnapshot(stub.descriptor, envelope, map[string]any{
			"endpoint":    "https://example.com/health",
			"metric_name": "latency_ms",
		}, RedactionNormalOnly)
		if err != nil {
			t.Fatalf("NewCanonicalSnapshot(stripped topology) error = %v", err)
		}
		stub.preview.Redaction = []FieldDecision{
			{Path: "endpoint", Sensitivity: SensitivitySensitiveTopology, Action: RedactionActionIncluded},
			{Path: "metric_name", Sensitivity: SensitivityNormal, Action: RedactionActionIncluded},
		}
		stub.preview.Sensitivity = SensitivitySensitiveTopology
		stub.preview.EstimatedCanonicalBytes = snapshot.Size()
		stub.snapshot = snapshot
		if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrKindConformance) {
			t.Fatalf("VerifyKindConformance(classification drift) error = %v, want ErrKindConformance", err)
		}
	})
}

func TestRegistryConformanceRejectsPrecisionUnitsAndRetentionDrift(t *testing.T) {
	t.Run("actual precision", func(t *testing.T) {
		stub, fixture := testConformingKind(t)
		stub.preview.ActualPrecision.Value = 5 * time.Minute
		if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrKindConformance) {
			t.Fatalf("VerifyKindConformance(precision drift) error = %v, want ErrKindConformance", err)
		}
	})
	t.Run("metric units", func(t *testing.T) {
		stub, fixture := testConformingKind(t)
		stub.preview.Units.Values["latency_ms"] = "seconds"
		if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrKindConformance) {
			t.Fatalf("VerifyKindConformance(metric units drift) error = %v, want ErrKindConformance", err)
		}
	})
	t.Run("non-metric units", func(t *testing.T) {
		stub, fixture := testConformingNonMetricKind(t)
		stub.preview.Units.Reason = "point-in-time command audit"
		if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrKindConformance) {
			t.Fatalf("VerifyKindConformance(non-metric units drift) error = %v, want ErrKindConformance", err)
		}
	})
	t.Run("retention required", func(t *testing.T) {
		stub, fixture := testConformingKind(t)
		stub.preview.Retention = RetentionSemantics{}
		if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrKindConformance) {
			t.Fatalf("VerifyKindConformance(missing retention) error = %v, want ErrKindConformance", err)
		}
	})
}

func TestRegistryConformanceBlocksCaptureForBlockingQuotaPreview(t *testing.T) {
	for _, outcome := range []QuotaOutcome{
		{Status: QuotaExceeded, Reason: "project evidence quota exceeded"},
		{Status: QuotaUnavailable, Reason: "quota service unavailable"},
	} {
		t.Run(string(outcome.Status), func(t *testing.T) {
			stub, fixture := testConformingKind(t)
			stub.preview.QuotaOutcome = outcome
			if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrKindConformance) {
				t.Fatalf("VerifyKindConformance(%q quota) error = %v, want ErrKindConformance", outcome.Status, err)
			}
			if stub.captureCalled {
				t.Fatalf("VerifyKindConformance called Capture for %q quota preview", outcome.Status)
			}
		})
	}
}

func TestRegistryConformancePreviewRepresentsQuotaStates(t *testing.T) {
	for _, outcome := range []QuotaOutcome{
		{Status: QuotaAllowed},
		{Status: QuotaExceeded, Reason: "project evidence quota exceeded"},
		{Status: QuotaUnavailable, Reason: "quota service unavailable"},
	} {
		t.Run(string(outcome.Status), func(t *testing.T) {
			stub, fixture := testConformingKind(t)
			stub.preview.QuotaOutcome = outcome
			if err := validateConformancePreview(stub.descriptor, fixture.Selection, stub.preview); err != nil {
				t.Fatalf("validateConformancePreview(%q quota) error = %v", outcome.Status, err)
			}
		})
	}
}

func TestRegistryConformanceAcceptsNonMetricUnits(t *testing.T) {
	stub, fixture := testConformingNonMetricKind(t)
	if err := VerifyKindConformance(context.Background(), stub, fixture); err != nil {
		t.Fatalf("VerifyKindConformance(non-metric units) error = %v", err)
	}
}

func TestRegistryConformanceRejectsForbiddenStructuredExportField(t *testing.T) {
	stub, fixture := testConformingKind(t)
	stub.exportMaterial = &ExportMaterial{
		Key:       stub.descriptor.Key,
		MediaType: "application/json; charset=utf-8",
		Filename:  "evidence.json",
		Bytes:     []byte(`{"stdout":"benign-looking"}`),
	}
	if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrForbiddenField) {
		t.Fatalf("VerifyKindConformance(hostile export) error = %v, want ErrForbiddenField", err)
	}
}

func TestRegistryConformanceRejectsCompoundForbiddenStructuredExportFields(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		body      string
	}{
		{name: "command output", mediaType: "application/json", body: `{"command_output":"benign-looking"}`},
		{name: "output preview", mediaType: "application/vnd.houfeng.evidence+json", body: `{"nested":{"outputPreview":"benign-looking"}}`},
		{name: "command details", mediaType: "application/json", body: `{"commandDetails":"benign-looking"}`},
		{name: "URL query", mediaType: "application/vnd.houfeng.evidence+json", body: `{"url_query":"page=1"}`},
		{name: "URL fragment", mediaType: "application/json", body: `{"nested":{"urlFragment":"section"}}`},
		{name: "middle output", mediaType: "application/json", body: `{"command_output_preview":"benign-looking"}`},
		{name: "middle stdout", mediaType: "application/vnd.houfeng.evidence+json", body: `{"nested":{"commandStdoutPreview":"benign-looking"}}`},
		{name: "middle query", mediaType: "application/json", body: `{"archived_url_query_value":"page=1"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub, fixture := testConformingKind(t)
			stub.exportMaterial = &ExportMaterial{
				Key:       stub.descriptor.Key,
				MediaType: tt.mediaType,
				Filename:  "evidence.json",
				Bytes:     []byte(tt.body),
			}
			if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrForbiddenField) {
				t.Fatalf("VerifyKindConformance(%s) error = %v, want ErrForbiddenField", tt.name, err)
			}
		})
	}
}

func TestRegistryConformanceScansEveryOutboundSurface(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*kindStub)
	}{
		{name: "summary title", mutate: func(stub *kindStub) {
			stub.summary = &Summary{Key: stub.descriptor.Key, RendererVersion: stub.descriptor.Conformance.RendererVersion, Title: "secret=abc", SearchText: "safe", ReadModel: map[string]any{"status": "ok"}}
		}},
		{name: "summary search text", mutate: func(stub *kindStub) {
			stub.summary = &Summary{Key: stub.descriptor.Key, RendererVersion: stub.descriptor.Conformance.RendererVersion, Title: "safe", SearchText: "command stdout: abc", ReadModel: map[string]any{"status": "ok"}}
		}},
		{name: "summary DTO value", mutate: func(stub *kindStub) {
			stub.summary = &Summary{Key: stub.descriptor.Key, RendererVersion: stub.descriptor.Conformance.RendererVersion, Title: "safe", SearchText: "safe", ReadModel: map[string]any{"nested": []string{"token=abc"}}}
		}},
		{name: "comparison reason", mutate: func(stub *kindStub) {
			stub.comparison = &Comparison{Key: stub.descriptor.Key, Compatible: true, Reason: "password=abc", Values: map[string]any{"equal": true}}
		}},
		{name: "comparison DTO field", mutate: func(stub *kindStub) {
			stub.comparison = &Comparison{Key: stub.descriptor.Key, Compatible: true, Values: map[string]any{"commandOutput": "first line"}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub, fixture := testConformingKind(t)
			tt.mutate(stub)
			if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrForbiddenField) {
				t.Fatalf("VerifyKindConformance(%s) error = %v, want ErrForbiddenField", tt.name, err)
			}
		})
	}
}

func TestRegistryConformanceRejectsPGPPrivateKeyBlockAcrossOutboundSurfaces(t *testing.T) {
	hostile := []string{
		"-----BEGIN PGP PRIVATE KEY BLOCK-----",
		"\uff0d\uff0d\uff0d\uff0d\uff0d\uff22\uff25\uff27\uff29\uff2e \uff30\uff27\uff30 \uff30\uff32\uff29\uff36\uff21\uff34\uff25 \uff2b\uff25\uff39 \uff22\uff2c\uff2f\uff23\uff2b\uff0d\uff0d\uff0d\uff0d\uff0d",
	}
	surfaces := []struct {
		name   string
		mutate func(*testing.T, *kindStub, string)
	}{
		{name: "summary title", mutate: func(_ *testing.T, stub *kindStub, value string) {
			stub.summary = &Summary{Key: stub.descriptor.Key, RendererVersion: stub.descriptor.Conformance.RendererVersion, Title: value, SearchText: "safe", ReadModel: map[string]any{"status": "ok"}}
		}},
		{name: "summary read model", mutate: func(_ *testing.T, stub *kindStub, value string) {
			stub.summary = &Summary{Key: stub.descriptor.Key, RendererVersion: stub.descriptor.Conformance.RendererVersion, Title: "safe", SearchText: "safe", ReadModel: map[string]any{"status": value}}
		}},
		{name: "comparison reason", mutate: func(_ *testing.T, stub *kindStub, value string) {
			stub.comparison = &Comparison{Key: stub.descriptor.Key, Compatible: true, Reason: value, Values: map[string]any{"equal": true}}
		}},
		{name: "comparison values", mutate: func(_ *testing.T, stub *kindStub, value string) {
			stub.comparison = &Comparison{Key: stub.descriptor.Key, Compatible: true, Values: map[string]any{"status": value}}
		}},
		{name: "JSON export", mutate: func(t *testing.T, stub *kindStub, value string) {
			encoded, err := json.Marshal(map[string]string{"status": value})
			if err != nil {
				t.Fatalf("json.Marshal(export fixture) error = %v", err)
			}
			stub.exportMaterial = &ExportMaterial{Key: stub.descriptor.Key, MediaType: "application/json", Filename: "evidence.json", Bytes: encoded}
		}},
	}
	for hostileIndex, value := range hostile {
		for _, surface := range surfaces {
			t.Run(fmt.Sprintf("form_%d_%s", hostileIndex, surface.name), func(t *testing.T) {
				stub, fixture := testConformingKind(t)
				surface.mutate(t, stub, value)
				if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrForbiddenField) {
					t.Fatalf("VerifyKindConformance(%s) error = %v, want ErrForbiddenField", surface.name, err)
				}
			})
		}
	}
}

func TestRegistryConformanceRejectsOpaqueExportMedia(t *testing.T) {
	for _, mediaType := range []string{"application/octet-stream", "text/html", "image/svg+xml"} {
		t.Run(mediaType, func(t *testing.T) {
			stub, fixture := testConformingKind(t)
			stub.exportMaterial = &ExportMaterial{
				Key:       stub.descriptor.Key,
				MediaType: mediaType,
				Filename:  "evidence.bin",
				Bytes:     []byte("benign but uninspectable"),
			}
			if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrInvalidCanonicalPayload) {
				t.Fatalf("VerifyKindConformance(%q) error = %v, want ErrInvalidCanonicalPayload", mediaType, err)
			}
		})
	}
}

func TestRegistryConformanceRejectsLeakHiddenByDuplicateExportKey(t *testing.T) {
	stub, fixture := testConformingKind(t)
	stub.exportMaterial = &ExportMaterial{
		Key:       stub.descriptor.Key,
		MediaType: "application/json",
		Filename:  "evidence.json",
		Bytes:     []byte(`{"status":"secret=abc","status":"ok"}`),
	}
	if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrForbiddenField) {
		t.Fatalf("VerifyKindConformance(duplicate-key leak) error = %v, want ErrForbiddenField", err)
	}
}

func testConformingKind(t *testing.T) (*kindStub, ConformanceFixture) {
	t.Helper()
	return testConformingKindWithContract(
		t,
		MonitoringProbeV2Key(),
		map[string]any{"metric_name": "latency_ms"},
		[]string{"latency_ms"},
		time.Minute,
		UnitsSemantics{Status: UnitsApplicable, Values: map[string]string{"latency_ms": "ms"}},
		testDurationSemantics(time.Minute),
		testDurationSemantics(time.Minute),
	)
}

func testConformingNonMetricKind(t *testing.T) (*kindStub, ConformanceFixture) {
	t.Helper()
	notApplicableDuration := DurationSemantics{Applicable: false, Reason: "command audit is point-in-time"}
	return testConformingKindWithContract(
		t,
		CommandAuditV1Key(),
		map[string]any{"command_id": "cmd_1"},
		nil,
		0,
		UnitsSemantics{Status: UnitsNotApplicable, Reason: "command audit is non-metric"},
		notApplicableDuration,
		notApplicableDuration,
	)
}

func testConformingKindWithContract(
	t *testing.T,
	key KindKey,
	payload map[string]any,
	metrics []string,
	precision time.Duration,
	units UnitsSemantics,
	actualPrecision DurationSemantics,
	bucketWidth DurationSemantics,
) (*kindStub, ConformanceFixture) {
	t.Helper()
	descriptor := testDescriptor(t, key)
	actor := testActor(t)
	selection := Selection{
		Key:             descriptor.Key,
		SourceType:      string(recordauth.SourceKindTarget),
		SourceID:        "tg_0123456789abcdef",
		RequestedWindow: testWindow(),
		Metrics:         metrics,
		Precision:       precision,
	}
	authorization := testAuthorization(t)
	envelope := testEnvelope(t, descriptor.Key)
	envelope.Units = units
	envelope.ActualPrecision = actualPrecision
	envelope.BucketWidth = bucketWidth
	snapshot, redaction, err := NewCanonicalSnapshot(descriptor, envelope, payload, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	preview := Preview{
		IntentID:                "evi_0123456789abcdef01234567",
		Key:                     descriptor.Key,
		Selection:               selection,
		Subject:                 envelope.Subject,
		Source:                  envelope.Source,
		RequestedWindow:         selection.RequestedWindow,
		ActualWindow:            selection.RequestedWindow,
		ObservedAt:              selection.RequestedWindow.End,
		SourceRevision:          "revision-1",
		SourceWatermark:         "watermark-1",
		ProducerVersion:         "producer-1",
		CalculationVersion:      "calculation-1",
		Quality:                 testQuality(),
		Sensitivity:             SensitivityNormal,
		ActualPrecision:         actualPrecision,
		BucketWidth:             bucketWidth,
		QuotaOutcome:            QuotaOutcome{Status: QuotaAllowed},
		Retention:               testRetentionSemantics(),
		Units:                   envelope.Units,
		Redaction:               redaction.Decisions,
		EstimatedCanonicalBytes: snapshot.Size(),
		SourceDigest:            sha256.Sum256([]byte("source")),
		RendererVersion:         descriptor.Conformance.RendererVersion,
		PreviewedAt:             time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		ValidUntil:              time.Date(2026, 8, 10, 12, 15, 0, 0, time.UTC),
	}
	stub := &kindStub{
		descriptor:    descriptor,
		preview:       preview,
		authorization: authorization,
		snapshot:      snapshot,
	}
	fixture := ConformanceFixture{
		Actor:      actor,
		Selection:  selection,
		Intent:     Intent{ID: preview.IntentID, Key: descriptor.Key, Selection: selection, PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil},
		Alignment:  Alignment{Mode: AlignmentExact},
		ExportMode: ExportModeSafe,
	}
	return stub, fixture
}

func TestRegistryConformanceRejectsPreviewKindDrift(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	stub := &kindStub{
		descriptor: descriptor,
		preview: Preview{
			Key: MonitoringHostV1Key(),
		},
	}
	fixture := ConformanceFixture{Actor: testActor(t), Selection: Selection{Key: descriptor.Key}}
	if err := VerifyKindConformance(context.Background(), stub, fixture); !errors.Is(err, ErrKindConformance) {
		t.Fatalf("VerifyKindConformance() error = %v, want ErrKindConformance", err)
	}
}

func testDescriptor(t *testing.T, key KindKey) Descriptor {
	t.Helper()
	descriptor := Descriptor{
		Key: key,
		Fields: []FieldDefinition{
			{Path: "command_id", Sensitivity: SensitivityNormal},
			{Path: "diagnostic_summary", Sensitivity: SensitivityNormal},
			{Path: "endpoint", Sensitivity: SensitivitySensitiveTopology, Format: FieldFormatURL},
			{Path: "metric_name", Sensitivity: SensitivityNormal},
			{Path: "metric_value", Sensitivity: SensitivityNormal},
			{Path: "status", Sensitivity: SensitivityNormal},
			{Path: "tags.a", Sensitivity: SensitivityNormal},
			{Path: "tags.z", Sensitivity: SensitivityNormal},
			{Path: "stdout", Sensitivity: SensitivityForbidden},
			{Path: "stderr", Sensitivity: SensitivityForbidden},
		},
		Conformance: ConformanceMetadata{
			CanonicalizationVersion: CanonicalizationVersionV1,
			ForbiddenCorpusVersion:  ForbiddenCorpusVersionV1,
			RendererVersion:         "renderer.v1",
			MaxCanonicalBytes:       MaxCanonicalPayloadBytes,
		},
	}
	if err := descriptor.Validate(); err != nil {
		t.Fatalf("descriptor.Validate() error = %v", err)
	}
	return descriptor
}

func testActor(t *testing.T) ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_0123456789abcdef01234567",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func testAuthorization(t *testing.T) AuthorizationScope {
	t.Helper()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      recordauth.ProjectIDDefault,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindTarget,
		SourceID:     "tg_0123456789abcdef",
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return authorization
}

func testWindow() TimeWindow {
	return TimeWindow{
		Start: time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
	}
}

func testQuality() Quality {
	return Quality{Status: QualityComplete, SampleCount: 60}
}

func testDurationSemantics(value time.Duration) DurationSemantics {
	return DurationSemantics{Applicable: true, Value: value}
}

func testRetentionSemantics() RetentionSemantics {
	return RetentionSemantics{
		Immutable:      true,
		Scope:          RetentionScopeRecordRevision,
		SourceDeletion: SourceDeletionSnapshotRetained,
	}
}

func testEnvelope(t *testing.T, key KindKey) SnapshotEnvelope {
	t.Helper()
	window := testWindow()
	return SnapshotEnvelope{
		Key: key,
		Subject: IdentitySnapshot{
			Type:   "target",
			ID:     "tg_0123456789abcdef",
			Fields: map[string]string{"display_name": "edge probe"},
		},
		Source: IdentitySnapshot{
			Type:   string(recordauth.SourceKindTarget),
			ID:     "tg_0123456789abcdef",
			Fields: map[string]string{"display_name": "edge probe"},
		},
		Authorization:      testAuthorization(t),
		SourceDigest:       sha256.Sum256([]byte("source")),
		RequestedWindow:    window,
		ActualWindow:       window,
		ObservedAt:         window.End,
		CapturedAt:         window.End.Add(time.Minute),
		ReferencedAt:       window.End.Add(2 * time.Minute),
		SourceRevision:     "revision-1",
		SourceWatermark:    "watermark-1",
		ProducerVersion:    "producer-1",
		CalculationVersion: "calculation-1",
		Units:              UnitsSemantics{Status: UnitsApplicable, Values: map[string]string{"latency_ms": "ms"}},
		Quality:            testQuality(),
		Sensitivity:        SensitivityNormal,
		ActualPrecision:    testDurationSemantics(time.Minute),
		BucketWidth:        testDurationSemantics(time.Minute),
		QuotaOutcome:       QuotaOutcome{Status: QuotaAllowed},
		Retention:          testRetentionSemantics(),
	}
}

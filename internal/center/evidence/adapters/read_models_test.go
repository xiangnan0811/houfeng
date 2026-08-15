package adapters

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

func TestIPQualitySummaryAndComparisonExposeVersionedFacts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 3, 12, 0, 0, 0, time.UTC)
	leftReport := validIPQualityTestReport(now.Add(-2 * time.Hour))
	leftReport.ReportID = "ipq_left"
	leftReport.RiskLevel = "low"
	leftReport.Coverage = IPQualityCoverage{ExpectedProviderCount: 1, SuccessfulProviderCount: 1}
	leftReport.Providers = []IPQualityProviderEvidence{{Provider: "provider-a", Status: "success", SourceType: "default"}}
	rightReport := validIPQualityTestReport(now.Add(-time.Hour))
	rightReport.ReportID = "ipq_right"
	rightReport.RiskLevel = "high"
	rightReport.StaleAfter = 14 * 24 * time.Hour
	rightReport.Coverage = IPQualityCoverage{ExpectedProviderCount: 2, SuccessfulProviderCount: 2}
	rightReport.Providers = []IPQualityProviderEvidence{
		{Provider: "provider-a", Status: "success", SourceType: "default"},
		{Provider: "provider-b", Status: "success", SourceType: "optional"},
	}

	leftAdapter, left := captureIPQualityReadModelFixture(t, now, leftReport)
	_, right := captureIPQualityReadModelFixture(t, now, rightReport)

	summary := leftAdapter.Summarize(left)
	if summary.RendererVersion != "ip_quality_report_v1" || summary.ReadModel["version"] != "ip_quality_report_read_model/v1" {
		t.Fatalf("summary renderer/version = %q/%#v", summary.RendererVersion, summary.ReadModel["version"])
	}
	if summary.ReadModel["report_id"] != "ipq_left" || summary.ReadModel["risk_level"] != "low" {
		t.Fatalf("summary read model = %#v, want allowlisted IP quality facts", summary.ReadModel)
	}
	if _, exposed := summary.ReadModel["ip_address"]; exposed {
		t.Fatalf("summary read model exposes sensitive topology: %#v", summary.ReadModel)
	}

	comparison := leftAdapter.Compare(left, right, evidence.Alignment{Mode: evidence.AlignmentExact})
	if !comparison.Compatible || comparison.Reason != "compatible_ip_quality_report_v1" {
		t.Fatalf("comparison = %#v, want compatible IP quality reports", comparison)
	}
	if comparison.Values["version"] != "ip_quality_report_comparison/v1" {
		t.Fatalf("comparison version = %#v", comparison.Values["version"])
	}
	if comparison.Values["window_duration_seconds"] != int64(3*60*60) || comparison.Values["units_status"] != string(evidence.UnitsNotApplicable) {
		t.Fatalf("comparison window/units = %#v/%#v", comparison.Values["window_duration_seconds"], comparison.Values["units_status"])
	}
	changes := comparison.Values["changes"].(map[string]any)
	if changes["risk_level_changed"] != true {
		t.Fatalf("comparison changes = %#v, want risk level change", changes)
	}
	if changes["stale_policy_changed"] != true || changes["stale_after_seconds_delta"] != int64(7*24*60*60) {
		t.Fatalf("comparison stale policy changes = %#v, want explicit policy delta", changes)
	}
	deltas := comparison.Values["coverage_deltas"].(map[string]any)
	if deltas["successful_provider_count"] != int64(1) {
		t.Fatalf("coverage deltas = %#v, want successful provider delta 1", deltas)
	}
}

func TestMonitoringSummaryAndComparisonExposeVersionedSeriesDeltas(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	leftAdapter, left := captureMonitoringReadModelFixture(t, start, time.Hour, 10)
	_, right := captureMonitoringReadModelFixture(t, start.Add(24*time.Hour), time.Hour, 25)

	summary := leftAdapter.Summarize(left)
	if summary.RendererVersion != "monitoring_host_v1" || summary.ReadModel["version"] != "monitoring_host_read_model/v1" {
		t.Fatalf("summary renderer/version = %q/%#v", summary.RendererVersion, summary.ReadModel["version"])
	}
	if summary.ReadModel["actual_precision_seconds"] != int64(3600) {
		t.Fatalf("summary read model = %#v, want 1h precision", summary.ReadModel)
	}
	if _, ok := summary.ReadModel["buckets"].([]any); !ok {
		t.Fatalf("summary buckets = %#v, want allowlisted series", summary.ReadModel["buckets"])
	}

	comparison := leftAdapter.Compare(left, right, evidence.Alignment{Mode: evidence.AlignmentExact})
	if !comparison.Compatible || comparison.Reason != "compatible_monitoring_host_v1" {
		t.Fatalf("comparison = %#v, want compatible monitoring windows", comparison)
	}
	if comparison.Values["version"] != "monitoring_host_comparison/v1" {
		t.Fatalf("comparison version = %#v", comparison.Values["version"])
	}
	if comparison.Values["window_duration_seconds"] != int64(60*60) || comparison.Values["actual_precision_seconds"] != int64(60*60) {
		t.Fatalf("comparison window/precision = %#v/%#v", comparison.Values["window_duration_seconds"], comparison.Values["actual_precision_seconds"])
	}
	units := comparison.Values["units"].(map[string]string)
	if units["cpu_usage_pct"] != "percent" {
		t.Fatalf("comparison units = %#v", units)
	}
	metrics := comparison.Values["metric_deltas"].([]any)
	if len(metrics) != 1 {
		t.Fatalf("metric deltas = %#v, want one metric", metrics)
	}
	metric := metrics[0].(map[string]any)
	if metric["metric"] != "cpu_usage_pct" || metric["average_delta"] != float64(15) {
		t.Fatalf("metric delta = %#v, want cpu average delta 15", metric)
	}
	qualityDeltas := comparison.Values["quality_deltas"].(map[string]any)
	if qualityDeltas["peak_count"] != int64(0) {
		t.Fatalf("quality deltas = %#v, want explicit peak count delta", qualityDeltas)
	}

	_, incompatible := captureMonitoringReadModelFixture(t, start.Add(48*time.Hour), 5*time.Minute, 30)
	comparison = leftAdapter.Compare(left, incompatible, evidence.Alignment{Mode: evidence.AlignmentExact})
	if comparison.Compatible || comparison.Reason != "precision_incompatible" {
		t.Fatalf("precision comparison = %#v, want precision_incompatible", comparison)
	}
}

func TestReadModelComparisonsRejectUnitsReasonDrift(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 3, 12, 0, 0, 0, time.UTC)
	ipAdapter, ipSnapshot := captureIPQualityReadModelFixture(t, now, validIPQualityTestReport(now.Add(-time.Hour)))
	ipDrift := rebuildReadModelSnapshot(t, ipAdapter.Descriptor(), ipSnapshot, func(envelope *evidence.SnapshotEnvelope) {
		envelope.Units.Reason = "different point semantics"
	})
	comparison := ipAdapter.Compare(ipSnapshot, ipDrift, evidence.Alignment{Mode: evidence.AlignmentExact})
	if comparison.Compatible || comparison.Reason != "units_or_precision_incompatible" {
		t.Fatalf("IP units reason comparison = %#v, want fail-closed incompatibility", comparison)
	}

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	monitoringAdapter, monitoringSnapshot := captureMonitoringReadModelFixture(t, start, time.Hour, 10)
	monitoringDrift := rebuildReadModelSnapshot(t, monitoringAdapter.Descriptor(), monitoringSnapshot, func(envelope *evidence.SnapshotEnvelope) {
		envelope.Units.Values["cpu_usage_pct"] = "ratio"
	})
	comparison = monitoringAdapter.Compare(monitoringSnapshot, monitoringDrift, evidence.Alignment{Mode: evidence.AlignmentExact})
	if comparison.Compatible || comparison.Reason != "units_incompatible" {
		t.Fatalf("monitoring units reason comparison = %#v, want fail-closed incompatibility", comparison)
	}
}

func TestMonitoringMetricDeltasPreserveExtremaWithoutAverage(t *testing.T) {
	t.Parallel()

	left := map[string]any{"buckets": []any{map[string]any{"metrics": []any{map[string]any{
		"name": "http_status", "min": float64(200), "max": float64(204),
	}}}}}
	right := map[string]any{"buckets": []any{map[string]any{"metrics": []any{map[string]any{
		"name": "http_status", "min": float64(500), "max": float64(503),
	}}}}}
	deltas := monitoringMetricDeltas(left, right)
	if len(deltas) != 1 {
		t.Fatalf("metric deltas = %#v, want one HTTP status metric", deltas)
	}
	metric := deltas[0].(map[string]any)
	if metric["min_delta"] != float64(300) || metric["max_delta"] != float64(299) {
		t.Fatalf("HTTP status deltas = %#v, want min/max without synthetic average", metric)
	}
	if _, present := metric["average_delta"]; present {
		t.Fatalf("HTTP status deltas include synthetic average: %#v", metric)
	}
}

func TestMonitoringMetricDeltasKeepSeriesSeparateAndWeightBucketAverages(t *testing.T) {
	t.Parallel()

	left := map[string]any{"buckets": []any{
		map[string]any{"series_id": "probe-a", "sample_count": float64(1), "metrics": []any{map[string]any{"name": "latency_ms", "average": float64(10)}}},
		map[string]any{"series_id": "probe-a", "sample_count": float64(3), "metrics": []any{map[string]any{"name": "latency_ms", "average": float64(20)}}},
		map[string]any{"series_id": "probe-b", "sample_count": float64(1), "metrics": []any{map[string]any{"name": "latency_ms", "average": float64(100)}}},
	}}
	right := map[string]any{"buckets": []any{
		map[string]any{"series_id": "probe-a", "sample_count": float64(1), "metrics": []any{map[string]any{"name": "latency_ms", "average": float64(20)}}},
		map[string]any{"series_id": "probe-a", "sample_count": float64(3), "metrics": []any{map[string]any{"name": "latency_ms", "average": float64(30)}}},
		map[string]any{"series_id": "probe-b", "sample_count": float64(1), "metrics": []any{map[string]any{"name": "latency_ms", "average": float64(110)}}},
	}}
	deltas := monitoringMetricDeltas(left, right)
	if len(deltas) != 2 {
		t.Fatalf("metric deltas = %#v, want one row per probe series", deltas)
	}
	first := deltas[0].(map[string]any)
	if first["series_id"] != "probe-a" || first["left_average"] != float64(17.5) || first["average_delta"] != float64(10) {
		t.Fatalf("probe-a delta = %#v, want sample-weighted average delta", first)
	}
	second := deltas[1].(map[string]any)
	if second["series_id"] != "probe-b" || second["average_delta"] != float64(10) {
		t.Fatalf("probe-b delta = %#v, want independent series delta", second)
	}
}

func captureIPQualityReadModelFixture(
	t *testing.T,
	now time.Time,
	report IPQualityEvidenceReport,
) (*IPQualityAdapter, evidence.CanonicalSnapshot) {
	t.Helper()
	adapter, err := NewIPQualityAdapter(
		staticIPQualitySource{report: report},
		monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"),
		AdapterOptions{Clock: func() time.Time { return now }, NewIntentID: func() (string, error) { return "evi_89abcdef0123456701234567", nil }},
	)
	if err != nil {
		t.Fatalf("NewIPQualityAdapter() error = %v", err)
	}
	selection := evidence.Selection{
		Key: evidence.IPQualityReportV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: now.Add(-3 * time.Hour), End: now},
	}
	preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), selection)
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v", err)
	}
	snapshot, err := adapter.Capture(context.Background(), monitoringTestActor(t), evidence.Intent{
		ID: preview.IntentID, Key: selection.Key, Selection: selection,
		PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil,
	})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	return adapter, snapshot
}

func captureMonitoringReadModelFixture(
	t *testing.T,
	start time.Time,
	precision time.Duration,
	average float64,
) (*MonitoringAdapter, evidence.CanonicalSnapshot) {
	t.Helper()
	window := evidence.TimeWindow{Start: start, End: start.Add(time.Hour)}
	capture := validMonitoringTestCapture(window, precision, "", "host", "cpu_usage_pct", "percent")
	capture.Buckets[0].Metrics[0].Average = &average
	capture.Buckets[0].Metrics[0].Max = &average
	adapter, err := NewMonitoringHostAdapter(
		staticMonitoringSource{host: capture},
		monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"),
		AdapterOptions{Clock: func() time.Time { return window.End.Add(time.Hour) }, NewIntentID: func() (string, error) { return "evi_0123456789abcdef01234567", nil }},
	)
	if err != nil {
		t.Fatalf("NewMonitoringHostAdapter() error = %v", err)
	}
	selection := evidence.Selection{
		Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef",
		RequestedWindow: window, Metrics: []string{"cpu_usage_pct"}, Precision: precision,
	}
	preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), selection)
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v", err)
	}
	snapshot, err := adapter.Capture(context.Background(), monitoringTestActor(t), evidence.Intent{
		ID: preview.IntentID, Key: selection.Key, Selection: selection,
		PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil,
	})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	return adapter, snapshot
}

func rebuildReadModelSnapshot(
	t *testing.T,
	descriptor evidence.Descriptor,
	snapshot evidence.CanonicalSnapshot,
	mutate func(*evidence.SnapshotEnvelope),
) evidence.CanonicalSnapshot {
	t.Helper()
	envelope := snapshot.Envelope()
	mutate(&envelope)
	envelope.Redaction = nil
	payload := decodeAdapterCanonicalPayload(t, snapshot.Bytes())
	rebuilt, _, err := evidence.NewCanonicalSnapshot(descriptor, envelope, payload, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	return rebuilt
}

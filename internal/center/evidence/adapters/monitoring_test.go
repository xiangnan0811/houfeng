package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

var _ evidence.Kind = (*MonitoringAdapter)(nil)

func TestMonitoringAdaptersRejectLegacySparklineEvidenceShapes(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)
	resolver := monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef")

	tests := []struct {
		name   string
		result MonitoringSeriesCapture
	}{
		{
			name: "zero-filled buckets erase gaps",
			result: MonitoringSeriesCapture{
				RequestedWindow: evidence.TimeWindow{Start: start, End: end},
				ActualPrecision: time.Hour,
				ZeroFilled:      true,
				Buckets: []MonitoringBucket{
					{Start: start, End: start.Add(time.Hour), SampleCount: 1},
					{Start: start.Add(time.Hour), End: start.Add(2 * time.Hour), SampleCount: 0},
				},
			},
		},
		{
			name: "row-count truncation hides retention coverage",
			result: MonitoringSeriesCapture{
				RequestedWindow: evidence.TimeWindow{Start: start, End: end},
				ActualPrecision: time.Hour,
				Truncated:       true,
				Buckets: []MonitoringBucket{
					{Start: start.Add(3 * time.Hour), End: end, SampleCount: 1},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := NewMonitoringHostAdapter(
				staticMonitoringSource{host: tt.result},
				resolver,
				AdapterOptions{Clock: func() time.Time { return end.Add(time.Hour) }},
			)
			if err != nil {
				t.Fatalf("NewMonitoringHostAdapter() error = %v", err)
			}

			_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
				Key:             evidence.MonitoringHostV1Key(),
				SourceType:      string(recordauth.SourceKindMonitoringInstance),
				SourceID:        "mi_0123456789abcdef",
				RequestedWindow: evidence.TimeWindow{Start: start, End: end},
				Metrics:         []string{"cpu_usage_pct"},
			})
			if !errors.Is(err, ErrUnacceptableMonitoringEvidenceSource) {
				t.Fatalf("PreviewCapture() error = %v, want ErrUnacceptableMonitoringEvidenceSource", err)
			}
		})
	}
}

func TestMonitoringAdapterRejectsSubMicrosecondCustomSourceTimes(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: start, End: start.Add(time.Hour)}
	tests := []struct {
		name   string
		mutate func(*MonitoringSeriesCapture)
	}{
		{name: "coverage start", mutate: func(capture *MonitoringSeriesCapture) {
			capture.CoverageStart = capture.CoverageStart.Add(time.Nanosecond)
		}},
		{name: "coverage end", mutate: func(capture *MonitoringSeriesCapture) { capture.CoverageEnd = capture.CoverageEnd.Add(time.Nanosecond) }},
		{name: "observed at", mutate: func(capture *MonitoringSeriesCapture) { capture.ObservedAt = capture.ObservedAt.Add(time.Nanosecond) }},
		{name: "bucket start", mutate: func(capture *MonitoringSeriesCapture) {
			capture.Buckets[0].Start = capture.Buckets[0].Start.Add(time.Nanosecond)
		}},
		{name: "bucket end", mutate: func(capture *MonitoringSeriesCapture) {
			capture.Buckets[0].End = capture.Buckets[0].End.Add(time.Nanosecond)
		}},
		{name: "observation outside coverage", mutate: func(capture *MonitoringSeriesCapture) {
			capture.ObservedAt = capture.CoverageEnd
		}},
		{name: "malformed source watermark", mutate: func(capture *MonitoringSeriesCapture) {
			capture.SourceWatermark = "not-a-timestamp"
		}},
		{name: "sub-microsecond source watermark", mutate: func(capture *MonitoringSeriesCapture) {
			capture.SourceWatermark = capture.ObservedAt.Add(time.Nanosecond).Format(time.RFC3339Nano)
		}},
		{name: "source watermark before observation", mutate: func(capture *MonitoringSeriesCapture) {
			capture.SourceWatermark = capture.ObservedAt.Add(-time.Microsecond).Format(time.RFC3339Nano)
		}},
		{name: "future source watermark", mutate: func(capture *MonitoringSeriesCapture) {
			capture.SourceWatermark = window.End.Add(time.Hour + time.Microsecond).Format(time.RFC3339Nano)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := validMonitoringTestCapture(window, time.Minute, "", "host", "cpu_usage_pct", "percent")
			tt.mutate(&capture)
			adapter, err := NewMonitoringHostAdapter(
				staticMonitoringSource{host: capture},
				monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"),
				AdapterOptions{Clock: func() time.Time { return window.End.Add(time.Hour) }},
			)
			if err != nil {
				t.Fatalf("NewMonitoringHostAdapter() error = %v", err)
			}
			_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
				Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef",
				RequestedWindow: window, Metrics: []string{"cpu_usage_pct"},
			})
			if !errors.Is(err, ErrUnacceptableMonitoringEvidenceSource) {
				t.Fatalf("PreviewCapture() error = %v, want ErrUnacceptableMonitoringEvidenceSource", err)
			}
		})
	}
}

func TestMonitoringAdapterCanonicalizesCustomSourceOrder(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: start, End: start.Add(time.Hour)}
	latency, success := 25.0, 1.0
	firstCapture := MonitoringSeriesCapture{
		RequestedWindow: window, ActualPrecision: time.Hour,
		CoverageStart: start, CoverageEnd: window.End, ObservedAt: window.End.Add(-time.Microsecond),
		SourceWatermark: window.End.Format(time.RFC3339Nano), ProducerVersion: "agent/v1",
		Buckets: []MonitoringBucket{
			{SeriesID: "probe-b", SeriesKind: "http", Start: start, End: window.End, SourceLayer: MonitoringSourceRaw, SourceGranularity: time.Minute, SampleCount: 1, Metrics: []MonitoringMetric{
				{Name: "success_ratio", Unit: "ratio", Average: &success},
				{Name: "latency_ms", Unit: "ms", Average: &latency},
			}},
			{SeriesID: "probe-a", SeriesKind: "http", Start: start, End: window.End, SourceLayer: MonitoringSourceRaw, SourceGranularity: time.Minute, SampleCount: 1, Metrics: []MonitoringMetric{
				{Name: "success_ratio", Unit: "ratio", Average: &success},
				{Name: "latency_ms", Unit: "ms", Average: &latency},
			}},
		},
	}
	secondCapture := firstCapture
	secondCapture.Buckets = append([]MonitoringBucket(nil), firstCapture.Buckets...)
	secondCapture.Buckets[0], secondCapture.Buckets[1] = secondCapture.Buckets[1], secondCapture.Buckets[0]
	for index := range secondCapture.Buckets {
		secondCapture.Buckets[index].Metrics = append([]MonitoringMetric(nil), secondCapture.Buckets[index].Metrics...)
		secondCapture.Buckets[index].Metrics[0], secondCapture.Buckets[index].Metrics[1] = secondCapture.Buckets[index].Metrics[1], secondCapture.Buckets[index].Metrics[0]
	}

	first := captureMonitoringCustomSourceFixture(t, window, firstCapture)
	second := captureMonitoringCustomSourceFixture(t, window, secondCapture)
	if first.Hash() != second.Hash() {
		t.Fatalf("canonical hashes differ for equivalent source order: %x != %x", first.Hash(), second.Hash())
	}
}

func TestMonitoringAdapterBoundsExpandedGapRows(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: start, End: start.Add(time.Duration(evidence.MaxMetricBucketCount) * 24 * time.Hour)}
	value := 1.0
	buckets := make([]MonitoringBucket, 26)
	for index := range buckets {
		buckets[index] = MonitoringBucket{
			SeriesID: fmt.Sprintf("probe-%02d", index), SeriesKind: "http",
			Start: start, End: start.Add(24 * time.Hour), SourceLayer: MonitoringSourceDailyAggregate, SourceGranularity: 24 * time.Hour,
			SampleCount: 1, Metrics: []MonitoringMetric{{Name: "success_ratio", Unit: "ratio", Average: &value}},
		}
	}
	capture := MonitoringSeriesCapture{
		RequestedWindow: window, ActualPrecision: 24 * time.Hour,
		CoverageStart: start, CoverageEnd: start.Add(24 * time.Hour), ObservedAt: start.Add(24*time.Hour - time.Microsecond),
		SourceWatermark: start.Add(24 * time.Hour).Format(time.RFC3339Nano), ProducerVersion: "agent/v1", Buckets: buckets,
	}
	adapter, err := NewMonitoringProbeAdapter(
		staticMonitoringSource{probe: capture},
		monitoringTestResolver(t, recordauth.SourceKindTarget, "tg_0123456789abcdef"),
		AdapterOptions{Clock: func() time.Time { return window.End.Add(time.Hour) }},
	)
	if err != nil {
		t.Fatalf("NewMonitoringProbeAdapter() error = %v", err)
	}
	_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
		Key: evidence.MonitoringProbeV2Key(), SourceType: string(recordauth.SourceKindTarget), SourceID: "tg_0123456789abcdef",
		RequestedWindow: window, Metrics: []string{"success_ratio"}, Precision: 24 * time.Hour,
	})
	if !errors.Is(err, ErrMonitoringEvidenceLimitExceeded) {
		t.Fatalf("PreviewCapture(expanded gaps) error = %v, want ErrMonitoringEvidenceLimitExceeded", err)
	}
}

func TestMonitoringHostPreviewPreservesCoverageGapsAndProvenance(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)
	firstValue := 25.0
	secondValue := 75.0
	adapter, err := NewMonitoringHostAdapter(
		staticMonitoringSource{host: MonitoringSeriesCapture{
			RequestedWindow: evidence.TimeWindow{Start: start, End: end},
			ActualPrecision: time.Hour,
			CoverageStart:   start.Add(5 * time.Minute),
			CoverageEnd:     start.Add(3*time.Hour + 55*time.Minute),
			ObservedAt:      start.Add(3*time.Hour + 55*time.Minute - time.Microsecond),
			SourceWatermark: "2026-07-01T03:56:00Z",
			ProducerVersion: "agent/v1",
			Buckets: []MonitoringBucket{
				{
					Start: start, End: start.Add(time.Hour), SourceLayer: MonitoringSourceRaw,
					SourceGranularity: 5 * time.Minute, SampleCount: 2, MaintenanceCount: 1,
					Metrics: []MonitoringMetric{{Name: "cpu_usage_pct", Unit: "percent", Average: &firstValue, Max: &firstValue}},
				},
				{
					Start: start.Add(3 * time.Hour), End: end, SourceLayer: MonitoringSourceRaw,
					SourceGranularity: 5 * time.Minute, SampleCount: 1, BackfilledCount: 1,
					Metrics: []MonitoringMetric{{Name: "cpu_usage_pct", Unit: "percent", Average: &secondValue, Max: &secondValue}},
				},
			},
		}},
		monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"),
		AdapterOptions{
			Clock:       func() time.Time { return end.Add(time.Hour) },
			NewIntentID: func() (string, error) { return "evi_0123456789abcdef01234567", nil },
		},
	)
	if err != nil {
		t.Fatalf("NewMonitoringHostAdapter() error = %v", err)
	}

	preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
		Key:             evidence.MonitoringHostV1Key(),
		SourceType:      string(recordauth.SourceKindMonitoringInstance),
		SourceID:        "mi_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: start, End: end},
		Metrics:         []string{"cpu_usage_pct"},
	})
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v", err)
	}
	if preview.ActualWindow != (evidence.TimeWindow{Start: start.Add(5 * time.Minute), End: start.Add(3*time.Hour + 55*time.Minute)}) {
		t.Fatalf("ActualWindow = %#v, want source coverage", preview.ActualWindow)
	}
	if preview.Quality.SampleCount != 3 || preview.Quality.GapCount != 2 ||
		preview.Quality.MaintenanceCount != 1 || preview.Quality.BackfilledCount != 1 ||
		preview.Quality.BucketCount != 2 || preview.Quality.DataPointCount != 2 ||
		preview.Quality.Status != evidence.QualityPartial || !preview.Quality.Partial || preview.Quality.Truncated {
		t.Fatalf("Quality = %#v, want actual counts and two missing buckets", preview.Quality)
	}
	if !preview.ActualPrecision.Applicable || preview.ActualPrecision.Value != time.Hour ||
		!preview.BucketWidth.Applicable || preview.BucketWidth.Value != time.Hour {
		t.Fatalf("precision/bucket = %#v/%#v, want 1h", preview.ActualPrecision, preview.BucketWidth)
	}
}

func TestMonitoringHostCaptureKeepsPopulatedBucketsAndExplicitGaps(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)
	firstAverage, firstMax := 20.0, 30.0
	lastAverage, lastMax := 70.0, 90.0
	result := MonitoringSeriesCapture{
		RequestedWindow: evidence.TimeWindow{Start: start, End: end},
		ActualPrecision: time.Hour,
		CoverageStart:   start.Add(5 * time.Minute),
		CoverageEnd:     end.Add(-5 * time.Minute),
		ObservedAt:      end.Add(-5*time.Minute - time.Microsecond),
		SourceWatermark: "2026-07-01T03:56:00Z",
		ProducerVersion: "agent/v1",
		Buckets: []MonitoringBucket{
			{
				Start: start, End: start.Add(time.Hour), SourceLayer: MonitoringSourceRaw,
				SourceGranularity: 5 * time.Minute, SampleCount: 12, MaintenanceCount: 2,
				Metrics: []MonitoringMetric{{Name: "cpu_usage_pct", Unit: "percent", Average: &firstAverage, Max: &firstMax}},
			},
			{
				Start: start.Add(3 * time.Hour), End: end, SourceLayer: MonitoringSourceRaw,
				SourceGranularity: 5 * time.Minute, SampleCount: 10, BackfilledCount: 3,
				Metrics: []MonitoringMetric{{Name: "cpu_usage_pct", Unit: "percent", Average: &lastAverage, Max: &lastMax}},
			},
		},
	}
	adapter, err := NewMonitoringHostAdapter(
		staticMonitoringSource{host: result},
		monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"),
		AdapterOptions{
			Clock:       func() time.Time { return end.Add(time.Hour) },
			NewIntentID: func() (string, error) { return "evi_0123456789abcdef01234567", nil },
		},
	)
	if err != nil {
		t.Fatalf("NewMonitoringHostAdapter() error = %v", err)
	}
	selection := evidence.Selection{
		Key:             evidence.MonitoringHostV1Key(),
		SourceType:      string(recordauth.SourceKindMonitoringInstance),
		SourceID:        "mi_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: start, End: end},
		Metrics:         []string{"cpu_usage_pct"},
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
	payload := decodeAdapterCanonicalPayload(t, snapshot.Bytes())
	buckets, ok := payload["buckets"].([]any)
	if !ok || len(buckets) != 2 {
		t.Fatalf("payload buckets = %#v, want two populated buckets only", payload["buckets"])
	}
	for _, bucket := range buckets {
		bucketMap := bucket.(map[string]any)
		if bucketMap["sample_count"].(float64) == 0 {
			t.Fatalf("payload bucket = %#v, zero-filled buckets are forbidden", bucketMap)
		}
	}
	gaps, ok := payload["gaps"].([]any)
	if !ok || len(gaps) != 2 {
		t.Fatalf("payload gaps = %#v, want two explicit missing buckets", payload["gaps"])
	}
	if payload["actual_precision_seconds"] != float64(3600) {
		t.Fatalf("actual_precision_seconds = %#v, want 3600", payload["actual_precision_seconds"])
	}
	if preview.Quality.PeakCount != 2 || snapshot.Envelope().Quality.PeakCount != 2 {
		t.Fatalf("preview/snapshot peak counts = %d/%d, want 2", preview.Quality.PeakCount, snapshot.Envelope().Quality.PeakCount)
	}
}

func TestMonitoringGapsIncludeMissingBoundaryBuckets(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	capture := MonitoringSeriesCapture{
		RequestedWindow: evidence.TimeWindow{Start: start, End: start.Add(4 * time.Hour)},
		ActualPrecision: time.Hour,
		Buckets: []MonitoringBucket{
			{SeriesID: "host", Start: start.Add(time.Hour), End: start.Add(2 * time.Hour)},
			{SeriesID: "host", Start: start.Add(2 * time.Hour), End: start.Add(3 * time.Hour)},
		},
	}
	gaps, err := monitoringGaps(capture)
	if err != nil {
		t.Fatalf("monitoringGaps() error = %v", err)
	}
	if len(gaps) != 2 || gaps[0].Start != start || gaps[0].End != start.Add(time.Hour) ||
		gaps[1].Start != start.Add(3*time.Hour) || gaps[1].End != start.Add(4*time.Hour) {
		t.Fatalf("monitoringGaps() = %#v, want leading and trailing missing buckets", gaps)
	}
}

func TestMonitoringDefaultPrecisionMatrix(t *testing.T) {
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		duration  time.Duration
		precision time.Duration
	}{
		{name: "six hours", duration: 6 * time.Hour, precision: time.Minute},
		{name: "over six hours", duration: 6*time.Hour + time.Microsecond, precision: 5 * time.Minute},
		{name: "forty eight hours", duration: 48 * time.Hour, precision: 5 * time.Minute},
		{name: "over forty eight hours", duration: 48*time.Hour + time.Microsecond, precision: time.Hour},
		{name: "thirty days", duration: 30 * 24 * time.Hour, precision: time.Hour},
		{name: "over thirty days", duration: 30*24*time.Hour + time.Microsecond, precision: 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestedPrecision time.Duration
			source := monitoringSourceFunc{host: func(_ string, window evidence.TimeWindow, precision time.Duration, _ []string) (MonitoringSeriesCapture, error) {
				requestedPrecision = precision
				return validMonitoringTestCapture(window, precision, "", "host", "cpu_usage_pct", "percent"), nil
			}}
			adapter, err := NewMonitoringHostAdapter(
				source,
				monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"),
				AdapterOptions{Clock: func() time.Time { return start.Add(tt.duration + time.Hour) }, NewIntentID: func() (string, error) { return "evi_0123456789abcdef01234567", nil }},
			)
			if err != nil {
				t.Fatalf("NewMonitoringHostAdapter() error = %v", err)
			}
			_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
				Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef",
				RequestedWindow: evidence.TimeWindow{Start: start, End: start.Add(tt.duration)}, Metrics: []string{"cpu_usage_pct"},
			})
			if err != nil {
				t.Fatalf("PreviewCapture() error = %v", err)
			}
			if requestedPrecision != tt.precision {
				t.Fatalf("source precision = %s, want %s", requestedPrecision, tt.precision)
			}
		})
	}
}

func TestMonitoringDefaultPrecisionDoesNotSilentlyCoarsenPastBucketLimit(t *testing.T) {
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	duration := time.Duration(evidence.MaxMetricBucketCount+1) * 24 * time.Hour
	adapter, err := NewMonitoringHostAdapter(
		staticMonitoringSource{},
		monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"),
		AdapterOptions{Clock: func() time.Time { return start.Add(duration + time.Hour) }},
	)
	if err != nil {
		t.Fatalf("NewMonitoringHostAdapter() error = %v", err)
	}
	selection := evidence.Selection{
		Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: start, End: start.Add(duration)}, Metrics: []string{"cpu_usage_pct"},
	}
	if err := adapter.ValidateSelection(context.Background(), monitoringTestActor(t), selection); !errors.Is(err, ErrMonitoringEvidenceLimitExceeded) {
		t.Fatalf("ValidateSelection(default over limit) error = %v, want ErrMonitoringEvidenceLimitExceeded", err)
	}
	selection.Precision = 48 * time.Hour
	if err := adapter.ValidateSelection(context.Background(), monitoringTestActor(t), selection); !errors.Is(err, evidence.ErrInvalidCanonicalPayload) {
		t.Fatalf("ValidateSelection(unsupported multi-day precision) error = %v, want ErrInvalidCanonicalPayload", err)
	}
}

func TestMonitoringMetricRegistriesCoverParentHostAndProbeContracts(t *testing.T) {
	t.Parallel()

	for metric, unit := range map[string]string{
		"disk_used_pct": "percent", "inode_used_pct": "percent",
		"net_in_bytes_per_sec": "bytes_per_second", "disk_read_bytes_per_sec": "bytes_per_second",
	} {
		if got := hostMonitoringMetricUnits[metric]; got != unit {
			t.Fatalf("host metric %q unit = %q, want %q", metric, got, unit)
		}
	}
	for metric, unit := range map[string]string{"http_status": "status_code", "tls_expiry_days": "days"} {
		if got := probeMonitoringMetricUnits[metric]; got != unit {
			t.Fatalf("probe metric %q unit = %q, want %q", metric, got, unit)
		}
	}
}

func TestAdapterGeneratedIntentTimesMatchPostgresPrecision(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	now := end.Add(time.Hour + 987*time.Nanosecond)
	adapter, err := NewMonitoringHostAdapter(
		staticMonitoringSource{host: validMonitoringTestCapture(evidence.TimeWindow{Start: start, End: end}, time.Minute, "", "host", "cpu_usage_pct", "percent")},
		monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"),
		AdapterOptions{Clock: func() time.Time { return now }, NewIntentID: func() (string, error) { return "evi_0123456789abcdef01234567", nil }},
	)
	if err != nil {
		t.Fatalf("NewMonitoringHostAdapter() error = %v", err)
	}
	preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
		Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: start, End: end}, Metrics: []string{"cpu_usage_pct"},
	})
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v", err)
	}
	if preview.PreviewedAt != now.Truncate(time.Microsecond) || preview.ValidUntil != now.Truncate(time.Microsecond).Add(evidence.CaptureIntentTTL) {
		t.Fatalf("preview lifetime = %s/%s, want PostgreSQL-exact microseconds", preview.PreviewedAt, preview.ValidUntil)
	}

	selection := preview.Selection
	selection.RequestedWindow.End = selection.RequestedWindow.End.Add(time.Nanosecond)
	if err := adapter.ValidateSelection(context.Background(), monitoringTestActor(t), selection); !errors.Is(err, evidence.ErrInvalidCanonicalPayload) {
		t.Fatalf("ValidateSelection(sub-microsecond window) error = %v, want ErrInvalidCanonicalPayload", err)
	}
}

func TestMonitoringProbeV2PreservesMultipleSeries(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	latency, success := 25.0, 1.0
	capture := MonitoringSeriesCapture{
		RequestedWindow: evidence.TimeWindow{Start: start, End: end}, ActualPrecision: time.Hour,
		CoverageStart: start, CoverageEnd: end, ObservedAt: end.Add(-time.Microsecond),
		SourceWatermark: "2026-07-01T02:00:00Z", ProducerVersion: "agent/v1",
		Buckets: []MonitoringBucket{
			{SeriesID: "probe-a", SeriesKind: "icmp", Start: start, End: start.Add(time.Hour), SourceLayer: MonitoringSourceRaw, SourceGranularity: time.Minute, SampleCount: 1, Metrics: []MonitoringMetric{{Name: "latency_ms", Unit: "ms", Average: &latency}}},
			{SeriesID: "probe-b", SeriesKind: "tcp", Start: start.Add(time.Hour), End: end, SourceLayer: MonitoringSourceDailyAggregate, SourceGranularity: time.Hour, SampleCount: 1, Metrics: []MonitoringMetric{{Name: "success_ratio", Unit: "ratio", Average: &success}}},
		},
	}
	adapter, err := NewMonitoringProbeAdapter(
		staticMonitoringSource{probe: capture},
		monitoringTestResolver(t, recordauth.SourceKindTarget, "tg_0123456789abcdef"),
		AdapterOptions{Clock: func() time.Time { return end.Add(time.Hour) }, NewIntentID: func() (string, error) { return "evi_0123456789abcdef01234567", nil }},
	)
	if err != nil {
		t.Fatalf("NewMonitoringProbeAdapter() error = %v", err)
	}
	selection := evidence.Selection{
		Key: evidence.MonitoringProbeV2Key(), SourceType: string(recordauth.SourceKindTarget), SourceID: "tg_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: start, End: end}, Metrics: []string{"latency_ms", "success_ratio"}, Precision: time.Hour,
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
	payload := decodeAdapterCanonicalPayload(t, snapshot.Bytes())
	buckets := payload["buckets"].([]any)
	seen := map[string]bool{}
	for _, item := range buckets {
		seen[item.(map[string]any)["series_id"].(string)] = true
	}
	if !seen["probe-a"] || !seen["probe-b"] {
		t.Fatalf("series IDs = %#v, want both probe series", seen)
	}
}

func TestMonitoringLimitsPeakBucketAndDataPointCounts(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	value := 1.0
	peakBuckets := make([]MonitoringBucket, evidence.MaxPeakCount+1)
	for index := range peakBuckets {
		peakValue := value + float64(index)
		peakBuckets[index] = MonitoringBucket{SeriesID: "host", Start: start.Add(time.Duration(index) * time.Hour), Metrics: []MonitoringMetric{{Name: "cpu_usage_pct", Max: &peakValue}}}
	}
	if peaks := monitoringPeaks(peakBuckets); len(peaks) != int(evidence.MaxPeakCount) {
		t.Fatalf("peak count = %d, want %d", len(peaks), evidence.MaxPeakCount)
	}

	buckets := make([]MonitoringBucket, evidence.MaxMetricBucketCount+1)
	for index := range buckets {
		buckets[index] = MonitoringBucket{SeriesID: "host", SampleCount: 1, Metrics: []MonitoringMetric{{Name: "cpu_usage_pct", Average: &value}}}
	}
	_, err := monitoringQuality(evidence.TimeWindow{Start: start, End: start.Add(time.Hour)}, MonitoringSeriesCapture{
		CoverageStart: start, CoverageEnd: start.Add(time.Hour), Buckets: buckets,
	}, nil, nil)
	if !errors.Is(err, ErrMonitoringEvidenceLimitExceeded) {
		t.Fatalf("monitoringQuality(bucket limit) error = %v, want ErrMonitoringEvidenceLimitExceeded", err)
	}

	points := make([]MonitoringMetric, evidence.MaxSnapshotDataPoints+1)
	for index := range points {
		points[index] = MonitoringMetric{Name: "cpu_usage_pct", Average: &value}
	}
	_, err = monitoringQuality(evidence.TimeWindow{Start: start, End: start.Add(time.Hour)}, MonitoringSeriesCapture{
		CoverageStart: start, CoverageEnd: start.Add(time.Hour), Buckets: []MonitoringBucket{{SeriesID: "host", SampleCount: 1, Metrics: points}},
	}, nil, nil)
	if !errors.Is(err, ErrMonitoringEvidenceLimitExceeded) {
		t.Fatalf("monitoringQuality(point limit) error = %v, want ErrMonitoringEvidenceLimitExceeded", err)
	}
}

func TestMonitoringAuthorizationRunsBeforeSourceRead(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	source := &countingMonitoringSource{}
	adapter, err := NewMonitoringHostAdapter(source, staticSourceResolver{err: recordauth.ErrDenied}, AdapterOptions{Clock: func() time.Time { return start.Add(2 * time.Hour) }})
	if err != nil {
		t.Fatalf("NewMonitoringHostAdapter() error = %v", err)
	}
	_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
		Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: start, End: start.Add(time.Hour)}, Metrics: []string{"cpu_usage_pct"},
	})
	if !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("PreviewCapture() error = %v, want ErrDenied", err)
	}
	if source.calls != 0 {
		t.Fatalf("source reads = %d, want zero before authorization", source.calls)
	}
}

func TestMonitoringAdaptersConformance(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	tests := []struct {
		name        string
		kind        evidence.Kind
		selection   evidence.Selection
		constructor func() (evidence.Kind, error)
	}{
		{
			name:      "host",
			selection: evidence.Selection{Key: evidence.MonitoringHostV1Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: evidence.TimeWindow{Start: start, End: end}, Metrics: []string{"cpu_usage_pct"}},
			constructor: func() (evidence.Kind, error) {
				return NewMonitoringHostAdapter(staticMonitoringSource{host: validMonitoringTestCapture(evidence.TimeWindow{Start: start, End: end}, time.Minute, "", "host", "cpu_usage_pct", "percent")}, monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return end.Add(time.Hour) }, NewIntentID: func() (string, error) { return "evi_0123456789abcdef01234567", nil }})
			},
		},
		{
			name:      "probe",
			selection: evidence.Selection{Key: evidence.MonitoringProbeV2Key(), SourceType: string(recordauth.SourceKindTarget), SourceID: "tg_0123456789abcdef", RequestedWindow: evidence.TimeWindow{Start: start, End: end}, Metrics: []string{"latency_ms"}},
			constructor: func() (evidence.Kind, error) {
				return NewMonitoringProbeAdapter(staticMonitoringSource{probe: validMonitoringTestCapture(evidence.TimeWindow{Start: start, End: end}, time.Minute, "probe-a", "icmp", "latency_ms", "ms")}, monitoringTestResolver(t, recordauth.SourceKindTarget, "tg_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return end.Add(time.Hour) }, NewIntentID: func() (string, error) { return "evi_0123456789abcdef01234567", nil }})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, err := tt.constructor()
			if err != nil {
				t.Fatalf("constructor() error = %v", err)
			}
			fixture := evidence.ConformanceFixture{
				Actor: monitoringTestActor(t), Selection: tt.selection,
				Intent:    evidence.Intent{ID: "evi_0123456789abcdef01234567", Key: tt.selection.Key, Selection: tt.selection, PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: end.Add(time.Hour + evidence.CaptureIntentTTL)},
				Alignment: evidence.Alignment{Mode: evidence.AlignmentExact}, ExportMode: evidence.ExportModeSafe,
			}
			if err := evidence.VerifyKindConformance(context.Background(), kind, fixture); err != nil {
				t.Fatalf("VerifyKindConformance() error = %v", err)
			}
		})
	}
}

type staticMonitoringSource struct {
	host  MonitoringSeriesCapture
	probe MonitoringSeriesCapture
	err   error
}

type monitoringSourceFunc struct {
	host  func(string, evidence.TimeWindow, time.Duration, []string) (MonitoringSeriesCapture, error)
	probe func(string, evidence.TimeWindow, time.Duration, []string) (MonitoringSeriesCapture, error)
}

func (source monitoringSourceFunc) LoadMonitoringHostEvidence(_ context.Context, sourceID string, window evidence.TimeWindow, precision time.Duration, metrics []string) (MonitoringSeriesCapture, error) {
	return source.host(sourceID, window, precision, metrics)
}

func (source monitoringSourceFunc) LoadMonitoringProbeEvidence(_ context.Context, sourceID string, window evidence.TimeWindow, precision time.Duration, metrics []string) (MonitoringSeriesCapture, error) {
	return source.probe(sourceID, window, precision, metrics)
}

type countingMonitoringSource struct {
	calls int
}

func (source *countingMonitoringSource) LoadMonitoringHostEvidence(context.Context, string, evidence.TimeWindow, time.Duration, []string) (MonitoringSeriesCapture, error) {
	source.calls++
	return MonitoringSeriesCapture{}, nil
}

func (source *countingMonitoringSource) LoadMonitoringProbeEvidence(context.Context, string, evidence.TimeWindow, time.Duration, []string) (MonitoringSeriesCapture, error) {
	source.calls++
	return MonitoringSeriesCapture{}, nil
}

func validMonitoringTestCapture(window evidence.TimeWindow, precision time.Duration, seriesID, seriesKind, metric, unit string) MonitoringSeriesCapture {
	value := 1.0
	bucketEnd := window.Start.Add(precision)
	if bucketEnd.After(window.End) {
		bucketEnd = window.End
	}
	return MonitoringSeriesCapture{
		RequestedWindow: window, ActualPrecision: precision, CoverageStart: window.Start, CoverageEnd: bucketEnd,
		ObservedAt: bucketEnd.Add(-time.Microsecond), SourceWatermark: bucketEnd.UTC().Format(time.RFC3339Nano), ProducerVersion: "agent/v1",
		Buckets: []MonitoringBucket{{
			SeriesID: seriesID, SeriesKind: seriesKind, Start: window.Start, End: bucketEnd,
			SourceLayer: MonitoringSourceRaw, SourceGranularity: precision, SampleCount: 1,
			Metrics: []MonitoringMetric{{Name: metric, Unit: unit, Average: &value, Max: &value}},
		}},
	}
}

func captureMonitoringCustomSourceFixture(
	t *testing.T,
	window evidence.TimeWindow,
	capture MonitoringSeriesCapture,
) evidence.CanonicalSnapshot {
	t.Helper()
	adapter, err := NewMonitoringProbeAdapter(
		staticMonitoringSource{probe: capture},
		monitoringTestResolver(t, recordauth.SourceKindTarget, "tg_0123456789abcdef"),
		AdapterOptions{
			Clock:       func() time.Time { return window.End.Add(time.Hour) },
			NewIntentID: func() (string, error) { return "evi_0123456789abcdef01234567", nil },
		},
	)
	if err != nil {
		t.Fatalf("NewMonitoringProbeAdapter() error = %v", err)
	}
	selection := evidence.Selection{
		Key: evidence.MonitoringProbeV2Key(), SourceType: string(recordauth.SourceKindTarget), SourceID: "tg_0123456789abcdef",
		RequestedWindow: window, Metrics: []string{"latency_ms", "success_ratio"}, Precision: time.Hour,
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
	return snapshot
}

func (source staticMonitoringSource) LoadMonitoringHostEvidence(
	context.Context,
	string,
	evidence.TimeWindow,
	time.Duration,
	[]string,
) (MonitoringSeriesCapture, error) {
	return source.host, source.err
}

func (source staticMonitoringSource) LoadMonitoringProbeEvidence(
	context.Context,
	string,
	evidence.TimeWindow,
	time.Duration,
	[]string,
) (MonitoringSeriesCapture, error) {
	return source.probe, source.err
}

func monitoringTestActor(t *testing.T) recordauth.ActorScope {
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

func monitoringTestResolver(t *testing.T, kind recordauth.SourceKind, sourceID string) staticSourceResolver {
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
		Kind:         kind,
		SourceID:     sourceID,
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	identity := evidence.IdentitySnapshot{
		Type:   string(kind),
		ID:     sourceID,
		Fields: map[string]string{"display_name": "Evidence source"},
	}
	return staticSourceResolver{resolved: ResolvedEvidenceSource{
		Subject:       identity,
		Source:        identity,
		Authorization: authorization,
	}}
}

type staticSourceResolver struct {
	resolved ResolvedEvidenceSource
	err      error
}

func decodeAdapterCanonicalPayload(t *testing.T, encoded []byte) map[string]any {
	t.Helper()
	var document struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode canonical payload: %v", err)
	}
	return document.Payload
}

func (resolver staticSourceResolver) ResolveEvidenceSource(
	context.Context,
	evidence.ActorScope,
	evidence.Selection,
) (ResolvedEvidenceSource, error) {
	return resolver.resolved, resolver.err
}

package evidence

import (
	"crypto/sha256"
	"reflect"
	"testing"
	"time"
)

func TestAlignActualCoverageMatchesNearestBucketsAndSplitsGaps(t *testing.T) {
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	left := []MonitoringBucketPoint{
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start, End: start.Add(time.Minute), Value: 10, Present: true, Hash: sha256.Sum256([]byte("l1"))},
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start.Add(3 * time.Minute), End: start.Add(4 * time.Minute), Value: 12, Present: true, Hash: sha256.Sum256([]byte("l2"))},
	}
	right := []MonitoringBucketPoint{
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start.Add(2 * time.Second), End: start.Add(time.Minute + 2*time.Second), Value: 20, Present: true, Hash: sha256.Sum256([]byte("r1"))},
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start.Add(3 * time.Minute), End: start.Add(4 * time.Minute), Value: 0, Present: true, Hash: sha256.Sum256([]byte("r2"))},
	}
	series, matches, err := AlignActualCoverage([][]MonitoringBucketPoint{left, right}, 0, 5*time.Second, "latency_ms")
	if err != nil {
		t.Fatalf("AlignActualCoverage() error = %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("series count = %d, want 2", len(series))
	}
	if series[0].ItemIndex != 0 || series[1].ItemIndex != 1 {
		t.Fatalf("item indexes = %d,%d", series[0].ItemIndex, series[1].ItemIndex)
	}
	if len(series[0].Segments) != 2 || len(series[1].Segments) != 2 {
		t.Fatalf("gap split failed: left=%d right=%d", len(series[0].Segments), len(series[1].Segments))
	}
	if series[1].Segments[1][0].Value != 0 {
		t.Fatalf("explicit zero was dropped: %#v", series[1].Segments[1])
	}
	if len(matches) != 1 || matches[0].ItemIndex != 1 || matches[0].Matched != 2 || len(matches[0].Deltas) != 2 {
		t.Fatalf("matches = %#v, want two paired deltas on item 1", matches)
	}
	if matches[0].Deltas[0].Delta != 10 || matches[0].Deltas[1].Delta != -12 {
		t.Fatalf("bucket deltas = %#v, want pairing to change the output", matches[0].Deltas)
	}
}

func TestAlignActualCoverageDoesNotReuseCandidateOrFillMissing(t *testing.T) {
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	left := []MonitoringBucketPoint{
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start, End: start.Add(time.Minute), Value: 1, Present: true, Hash: sha256.Sum256([]byte("a"))},
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start.Add(time.Minute), End: start.Add(2 * time.Minute), Value: 2, Present: true, Hash: sha256.Sum256([]byte("b"))},
	}
	right := []MonitoringBucketPoint{
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start, End: start.Add(time.Minute), Value: 9, Present: true, Hash: sha256.Sum256([]byte("c"))},
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start.Add(10 * time.Minute), End: start.Add(11 * time.Minute), Value: 99, Present: true, Hash: sha256.Sum256([]byte("extra"))},
	}
	series, matches, err := AlignActualCoverage([][]MonitoringBucketPoint{left, right}, 0, time.Minute, "latency_ms")
	if err != nil {
		t.Fatalf("AlignActualCoverage() error = %v", err)
	}
	if pointCount(series[0]) != 2 {
		t.Fatalf("baseline dropped an unmatched bucket: %#v", series[0])
	}
	if pointCount(series[1]) != 1 {
		t.Fatalf("right reused a bucket, filled missing, or kept an unmatched extra: %#v", series[1])
	}
	if series[1].Segments[0][0].Value != 9 {
		t.Fatalf("matched right point = %#v, want value 9", series[1].Segments[0])
	}
	if len(matches) != 1 || matches[0].Matched != 1 || matches[0].UnmatchedBaseline != 1 || matches[0].UnmatchedItem != 1 {
		t.Fatalf("matches = %#v, want one pair plus unmatched baseline and extra item", matches)
	}
}

func TestAlignActualCoverageTieBreakIsIndependentOfInputOrder(t *testing.T) {
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	earlier := MonitoringBucketPoint{
		SeriesID: "probe-a", Metric: "latency_ms", Start: start, End: start.Add(time.Minute),
		Value: 1, Present: true, Hash: sha256.Sum256([]byte("zz")),
	}
	later := MonitoringBucketPoint{
		SeriesID: "probe-a", Metric: "latency_ms", Start: start, End: start.Add(time.Minute),
		Value: 2, Present: true, Hash: sha256.Sum256([]byte("aa")),
	}
	// Same start; earlier UTC end wins? Both same window. Hash tie-break: smaller hash wins after UTC.
	// later has hash("aa") < hash("zz"), same start/end so hash decides if we treat them as same UTC.
	// Design: min abs offset, then earlier UTC, then source ordinal, then smaller hash.
	// Same window → hash decides. Input order must not change winner.
	first, firstMatches, err := AlignActualCoverage([][]MonitoringBucketPoint{{earlier}, {later, earlier}}, 0, 0, "latency_ms")
	if err != nil {
		t.Fatalf("AlignActualCoverage(first) error = %v", err)
	}
	second, secondMatches, err := AlignActualCoverage([][]MonitoringBucketPoint{{earlier}, {earlier, later}}, 0, 0, "latency_ms")
	if err != nil {
		t.Fatalf("AlignActualCoverage(second) error = %v", err)
	}
	if !reflect.DeepEqual(first[1].Segments, second[1].Segments) {
		t.Fatalf("tie-break depended on input order:\n%#v\n%#v", first[1].Segments, second[1].Segments)
	}
	if pointCount(first[1]) != 1 || pointCount(second[1]) != 1 {
		t.Fatalf("tie-break kept both candidates: %#v %#v", first[1], second[1])
	}
	if firstMatches[0].Matched != 1 || secondMatches[0].Matched != 1 {
		t.Fatalf("tie-break deltas drifted: %#v %#v", firstMatches, secondMatches)
	}
}

func TestAlignActualCoverageHonorsBaselineAndPairsEveryNonBaselineItem(t *testing.T) {
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	first := []MonitoringBucketPoint{
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start, End: start.Add(time.Minute), Value: 1, Present: true, Hash: sha256.Sum256([]byte("a"))},
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start.Add(2 * time.Minute), End: start.Add(3 * time.Minute), Value: 3, Present: true, Hash: sha256.Sum256([]byte("extra-a"))},
	}
	second := []MonitoringBucketPoint{
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start.Add(time.Second), End: start.Add(time.Minute + time.Second), Value: 10, Present: true, Hash: sha256.Sum256([]byte("b"))},
	}
	third := []MonitoringBucketPoint{
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start, End: start.Add(time.Minute), Value: 20, Present: true, Hash: sha256.Sum256([]byte("c"))},
		{SeriesID: "probe-a", Metric: "latency_ms", Start: start.Add(4 * time.Minute), End: start.Add(5 * time.Minute), Value: 30, Present: true, Hash: sha256.Sum256([]byte("extra-c"))},
	}
	asBaselineZero, matchesZero, err := AlignActualCoverage([][]MonitoringBucketPoint{first, second, third}, 0, 5*time.Second, "latency_ms")
	if err != nil {
		t.Fatalf("AlignActualCoverage(baseline 0) error = %v", err)
	}
	if pointCount(asBaselineZero[0]) != 2 || pointCount(asBaselineZero[1]) != 1 || pointCount(asBaselineZero[2]) != 1 {
		t.Fatalf("baseline 0 series = %#v", asBaselineZero)
	}
	if asBaselineZero[2].Segments[0][0].Value != 20 {
		t.Fatalf("item 2 kept an unmatched extra or the wrong pair: %#v", asBaselineZero[2])
	}
	if len(matchesZero) != 2 || matchesZero[0].ItemIndex != 1 || matchesZero[1].ItemIndex != 2 {
		t.Fatalf("baseline 0 matches = %#v, want pairings for items 1 and 2", matchesZero)
	}

	asBaselineOne, matchesOne, err := AlignActualCoverage([][]MonitoringBucketPoint{first, second, third}, 1, 5*time.Second, "latency_ms")
	if err != nil {
		t.Fatalf("AlignActualCoverage(baseline 1) error = %v", err)
	}
	if pointCount(asBaselineOne[1]) != 1 || pointCount(asBaselineOne[0]) != 1 || pointCount(asBaselineOne[2]) != 1 {
		t.Fatalf("baseline 1 series = %#v, matching must drop first-item extra", asBaselineOne)
	}
	if asBaselineOne[0].Segments[0][0].Value != 1 {
		t.Fatalf("baseline 1 left unmatched extra on item 0: %#v", asBaselineOne[0])
	}
	if len(matchesOne) != 2 || matchesOne[0].ItemIndex != 0 || matchesOne[1].ItemIndex != 2 {
		t.Fatalf("baseline 1 matches = %#v, want pairings against item 1", matchesOne)
	}
	if reflect.DeepEqual(asBaselineZero[0].Segments, asBaselineOne[0].Segments) {
		t.Fatal("changing BaselineIndex did not change who kept unmatched points")
	}
}

func TestExtractMonitoringSeriesPointsSkipsMissingValues(t *testing.T) {
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	points, err := ExtractMonitoringSeriesPoints(map[string]any{
		"buckets": []any{
			map[string]any{
				"series_id": "probe-a",
				"start":     start.Format(time.RFC3339Nano),
				"end":       start.Add(time.Minute).Format(time.RFC3339Nano),
				"metrics": []any{
					map[string]any{"name": "latency_ms", "average": float64(4)},
					map[string]any{"name": "loss_pct"},
				},
			},
		},
	}, "latency_ms", sha256.Sum256([]byte("snap")))
	if err != nil {
		t.Fatalf("ExtractMonitoringSeriesPoints() error = %v", err)
	}
	if len(points) != 1 || points[0].Value != 4 || !points[0].Present {
		t.Fatalf("points = %#v, want one present latency value", points)
	}
	missing, err := ExtractMonitoringSeriesPoints(map[string]any{
		"buckets": []any{
			map[string]any{
				"series_id": "probe-a",
				"start":     start.Format(time.RFC3339Nano),
				"end":       start.Add(time.Minute).Format(time.RFC3339Nano),
				"metrics":   []any{map[string]any{"name": "latency_ms"}},
			},
		},
	}, "latency_ms", sha256.Sum256([]byte("snap")))
	if err != nil {
		t.Fatalf("ExtractMonitoringSeriesPoints(missing) error = %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing average created a point: %#v", missing)
	}
}

func TestEvaluateComparisonBuildsHostSeriesFromSnapshotBuckets(t *testing.T) {
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	left := comparisonHostItem(t, "evs_host_a", start, 10)
	right := comparisonHostItem(t, "evs_host_b", start.Add(time.Second), 12)
	result, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items:           []ComparisonItemInput{left, right},
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: TimeWindow{Start: start, End: start.Add(10 * time.Minute)},
		Tolerance:       5 * time.Second,
		Detail:          &ComparisonDetail{Kind: MonitoringHostV1Key(), Metric: "cpu_pct"},
	})
	if err != nil {
		t.Fatalf("EvaluateComparison() error = %v", err)
	}
	if len(result.Series) != 2 || pointCount(result.Series[0]) == 0 || pointCount(result.Series[1]) == 0 {
		t.Fatalf("host series missing: %#v", result.Series)
	}
	if len(result.Pairwise) != 1 || result.Pairwise[0].ItemIndex != 1 {
		t.Fatalf("host pairwise = %#v, want one pairing against item 1", result.Pairwise)
	}
	equal, _ := result.Pairwise[0].Values["equal"].(bool)
	if equal || result.Pairwise[0].Values["matched"] != 1 {
		t.Fatalf("host pairwise values = %#v, matching must emit a verifiable delta", result.Pairwise[0].Values)
	}
}

func TestEvaluateComparisonItemUnmatchedEmitsCoveragePartial(t *testing.T) {
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	left := comparisonHostItem(t, "evs_host_a", start, 10)
	right := comparisonHostItemWithBuckets(t, "evs_host_b", []hostBucket{
		{Start: start.Add(time.Second), Value: 12},
		{Start: start.Add(10 * time.Minute), Value: 99},
	})
	result, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items:           []ComparisonItemInput{left, right},
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: TimeWindow{Start: start, End: start.Add(20 * time.Minute)},
		Tolerance:       5 * time.Second,
		Detail:          &ComparisonDetail{Kind: MonitoringHostV1Key(), Metric: "cpu_pct"},
	})
	if err != nil {
		t.Fatalf("EvaluateComparison() error = %v", err)
	}
	if !hasComparisonReason(result.Review, ReasonCoveragePartial) {
		t.Fatalf("Review = %#v, want coverage_partial for unmatched item points", result.Review)
	}
	if len(result.Pairwise) != 1 || result.Pairwise[0].Values["unmatched_item"] != 1 {
		t.Fatalf("pairwise = %#v, want unmatched_item=1", result.Pairwise)
	}
}

func TestEvaluateComparisonHostMetricMissingAddsReview(t *testing.T) {
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	result, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items:           []ComparisonItemInput{comparisonHostItem(t, "evs_host_a", start, 10), comparisonHostItem(t, "evs_host_b", start, 12)},
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: TimeWindow{Start: start, End: start.Add(10 * time.Minute)},
		Detail:          &ComparisonDetail{Kind: MonitoringHostV1Key()},
	})
	if err != nil {
		t.Fatalf("EvaluateComparison() error = %v", err)
	}
	if len(result.Series) != 0 || len(result.Pairwise) != 0 {
		t.Fatalf("empty metric leaked series: %#v %#v", result.Series, result.Pairwise)
	}
	if !hasComparisonReason(result.Review, ReasonMetricMissing) {
		t.Fatalf("Review = %#v, want %q", result.Review, ReasonMetricMissing)
	}
}

func TestEvaluateComparisonMixedMetadataStillAlignsReadableHostItems(t *testing.T) {
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	left := comparisonHostItem(t, "evs_host_a", start, 10)
	right := comparisonHostItem(t, "evs_host_b", start.Add(time.Second), 12)
	meta := ComparisonItemInput{
		Kind:            MonitoringHostV1Key(),
		RevisionContext: RevisionContextBound,
		Revision:        &RevisionMetadataSnapshot{RecordType: "note", ImpactLevel: "low"},
		RecordID:        "rec_meta",
		RevisionID:      "rrv_meta",
		Reasons:         []ComparisonReason{ReasonMetadataOnly},
	}
	result, err := EvaluateComparison(comparisonTestRegistry(t), ComparisonEvaluateInput{
		Items:           []ComparisonItemInput{left, right, meta},
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: TimeWindow{Start: start, End: start.Add(10 * time.Minute)},
		Tolerance:       5 * time.Second,
		Detail:          &ComparisonDetail{Kind: MonitoringHostV1Key(), Metric: "cpu_pct"},
	})
	if err != nil {
		t.Fatalf("EvaluateComparison() error = %v", err)
	}
	if !hasComparisonReason(result.Review, ReasonMetadataOnly) {
		t.Fatalf("Review = %#v, want metadata_only on the revision item", result.Review)
	}
	if len(result.Series) != 3 || pointCount(result.Series[0]) == 0 || pointCount(result.Series[1]) == 0 || pointCount(result.Series[2]) != 0 {
		t.Fatalf("mixed basket series = %#v", result.Series)
	}
	if len(result.Pairwise) != 1 || result.Pairwise[0].ItemIndex != 1 {
		t.Fatalf("mixed basket pairwise = %#v, want only the readable host item", result.Pairwise)
	}
	if result.Pairwise[0].Values["matched"] != 1 {
		t.Fatalf("readable pair was short-circuited: %#v", result.Pairwise[0])
	}
}

type hostBucket struct {
	Start time.Time
	Value float64
}

func comparisonHostItem(t *testing.T, snapshotID string, start time.Time, value float64) ComparisonItemInput {
	t.Helper()
	return comparisonHostItemWithBuckets(t, snapshotID, []hostBucket{{Start: start, Value: value}})
}

func comparisonHostItemWithBuckets(t *testing.T, snapshotID string, buckets []hostBucket) ComparisonItemInput {
	t.Helper()
	descriptor := monitoringSeriesTestDescriptor(t)
	envelope := testEnvelope(t, MonitoringHostV1Key())
	encoded := make([]any, 0, len(buckets))
	for _, bucket := range buckets {
		encoded = append(encoded, map[string]any{
			"series_id": "host-a",
			"start":     bucket.Start.UTC().Format(time.RFC3339Nano),
			"end":       bucket.Start.Add(time.Minute).UTC().Format(time.RFC3339Nano),
			"metrics":   []any{map[string]any{"name": "cpu_pct", "average": bucket.Value}},
		})
	}
	snapshot, _, err := NewCanonicalSnapshot(descriptor, envelope, map[string]any{"buckets": encoded}, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot(host) error = %v", err)
	}
	return ComparisonItemInput{
		SnapshotID:      snapshotID,
		Hash:            snapshot.Hash(),
		Kind:            MonitoringHostV1Key(),
		RevisionContext: RevisionContextNotApplicable,
		Snapshot:        snapshot,
	}
}

func monitoringSeriesTestDescriptor(t *testing.T) Descriptor {
	t.Helper()
	descriptor := Descriptor{
		Key: MonitoringHostV1Key(),
		Fields: []FieldDefinition{
			{Path: "buckets.series_id", Sensitivity: SensitivityNormal},
			{Path: "buckets.start", Sensitivity: SensitivityNormal},
			{Path: "buckets.end", Sensitivity: SensitivityNormal},
			{Path: "buckets.metrics.name", Sensitivity: SensitivityNormal},
			{Path: "buckets.metrics.average", Sensitivity: SensitivityNormal},
		},
		Conformance: ConformanceMetadata{
			CanonicalizationVersion: CanonicalizationVersionV1,
			ForbiddenCorpusVersion:  ForbiddenCorpusVersionV1,
			RendererVersion:         "monitoring_host_v1",
			MaxCanonicalBytes:       MaxCanonicalPayloadBytes,
		},
	}
	if err := descriptor.Validate(); err != nil {
		t.Fatalf("monitoring series descriptor: %v", err)
	}
	return descriptor
}

func pointCount(series Series) int {
	count := 0
	for _, segment := range series.Segments {
		count += len(segment)
	}
	return count
}

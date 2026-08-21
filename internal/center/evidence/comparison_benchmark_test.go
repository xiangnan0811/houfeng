//go:build !race

package evidence

import (
	"encoding/json"
	"runtime"
	"sort"
	"testing"
	"time"
)

func TestComparisonDetailPerformanceRecordsQuantiles(t *testing.T) {
	start := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	items := make([]ComparisonItemInput, 0, 6)
	for index := 0; index < 6; index++ {
		items = append(items, comparisonHostSeriesItem(t, "evs_perf"+string(rune('a'+index)), start, 2000))
	}
	input := ComparisonEvaluateInput{
		Items:           items,
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: TimeWindow{Start: start, End: start.Add(2000 * time.Minute)},
		Tolerance:       time.Minute,
		Detail:          &ComparisonDetail{Kind: MonitoringHostV1Key(), Metric: "cpu_pct"},
	}
	registry := comparisonTestRegistry(t)

	const rounds = 12
	durations := make([]time.Duration, 0, rounds)
	var allocs uint64
	var lastBytes int
	for round := 0; round < rounds; round++ {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		began := time.Now()
		result, err := EvaluateComparison(registry, input)
		elapsed := time.Since(began)
		runtime.ReadMemStats(&after)
		if err != nil {
			t.Fatalf("EvaluateComparison() error = %v", err)
		}
		if len(result.Series) != 6 {
			t.Fatalf("series count = %d, want 6", len(result.Series))
		}
		lastBytes = resultSizeBytes(result)
		durations = append(durations, elapsed)
		if after.TotalAlloc >= before.TotalAlloc {
			allocs += after.TotalAlloc - before.TotalAlloc
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	t.Logf(
		"comparison detail p50=%s p95=%s allocs_avg=%d response_bytes=%d (regression signal only; not a cgroup peak gate)",
		durations[len(durations)*50/100],
		durations[len(durations)*95/100],
		allocs/uint64(rounds),
		lastBytes,
	)
}

func comparisonHostSeriesItem(t *testing.T, snapshotID string, start time.Time, buckets int) ComparisonItemInput {
	t.Helper()
	payloadBuckets := make([]any, 0, buckets)
	for index := 0; index < buckets; index++ {
		bucketStart := start.Add(time.Duration(index) * time.Minute)
		payloadBuckets = append(payloadBuckets, map[string]any{
			"series_id": "host-a",
			"start":     bucketStart.UTC().Format(time.RFC3339Nano),
			"end":       bucketStart.Add(time.Minute).UTC().Format(time.RFC3339Nano),
			"metrics":   []any{map[string]any{"name": "cpu_pct", "average": float64(index % 17)}},
		})
	}
	snapshot, _, err := NewCanonicalSnapshot(monitoringSeriesTestDescriptor(t), testEnvelope(t, MonitoringHostV1Key()), map[string]any{
		"buckets": payloadBuckets,
	}, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot(host series) error = %v", err)
	}
	return ComparisonItemInput{
		SnapshotID: snapshotID, Hash: snapshot.Hash(), Kind: MonitoringHostV1Key(),
		RevisionContext: RevisionContextNotApplicable, Snapshot: snapshot,
	}
}

func resultSizeBytes(result ComparisonEvaluateResult) int {
	encoded, err := json.Marshal(struct {
		Series []Series
		Review []ComparabilityFinding
	}{Series: result.Series, Review: result.Review})
	if err != nil {
		return 0
	}
	return len(encoded)
}

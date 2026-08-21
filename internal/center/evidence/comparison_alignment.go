package evidence

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type MonitoringBucketPoint struct {
	SeriesID string
	Metric   string
	Start    time.Time
	End      time.Time
	Value    float64
	Present  bool
	Hash     [sha256.Size]byte
	ordinal  int
}

func ExtractMonitoringSeriesPoints(payload map[string]any, metric string, hash [sha256.Size]byte) ([]MonitoringBucketPoint, error) {
	if metric == "" {
		return nil, nil
	}
	rawBuckets, _ := payload["buckets"].([]any)
	points := make([]MonitoringBucketPoint, 0, len(rawBuckets))
	for _, raw := range rawBuckets {
		bucket, _ := raw.(map[string]any)
		metrics, _ := bucket["metrics"].([]any)
		var value float64
		found := false
		for _, rawMetric := range metrics {
			item, _ := rawMetric.(map[string]any)
			if stringValue(item["name"]) != metric {
				continue
			}
			parsed, ok := numberValue(item["average"])
			if !ok {
				continue
			}
			value = parsed
			found = true
			break
		}
		if !found {
			continue
		}
		start, startOK := parseUTCTime(bucket["start"])
		end, endOK := parseUTCTime(bucket["end"])
		if !startOK || !endOK || !end.After(start) {
			return nil, fmt.Errorf("%w: bucket window", ErrInvalidCanonicalPayload)
		}
		points = append(points, MonitoringBucketPoint{
			SeriesID: stringValue(bucket["series_id"]),
			Metric:   metric,
			Start:    start,
			End:      end,
			Value:    value,
			Present:  true,
			Hash:     hash,
		})
	}
	return points, nil
}

type CoverageBucketDelta struct {
	BaselineStart time.Time
	BaselineEnd   time.Time
	ItemStart     time.Time
	ItemEnd       time.Time
	BaselineValue float64
	ItemValue     float64
	Delta         float64
}

type CoverageMatch struct {
	ItemIndex         int
	Matched           int
	UnmatchedBaseline int
	UnmatchedItem     int
	Deltas            []CoverageBucketDelta
	Equal             bool
}

func AlignActualCoverage(items [][]MonitoringBucketPoint, baselineIndex int, tolerance time.Duration, metric string) ([]Series, []CoverageMatch, error) {
	if baselineIndex < 0 || baselineIndex >= len(items) {
		return nil, nil, fmt.Errorf("%w: baseline", ErrInvalidComparisonSelection)
	}
	prepared := make([][]MonitoringBucketPoint, len(items))
	for index, item := range items {
		filtered := filterMonitoringBuckets(item, metric)
		sortMonitoringBuckets(filtered)
		if len(filtered) > int(MaxMetricBucketCount) {
			return nil, nil, fmt.Errorf("%w: %d points", ErrComparisonResultTooLarge, len(filtered))
		}
		assignOrdinals(filtered)
		prepared[index] = filtered
	}
	baseline := prepared[baselineIndex]
	series := make([]Series, 0, len(items))
	matches := make([]CoverageMatch, 0, len(items)-1)
	for index, item := range prepared {
		if index == baselineIndex {
			series = append(series, Series{ItemIndex: index, MetricID: metric, Segments: splitGapSegments(item)})
			continue
		}
		pairs := matchActualCoverage(baseline, item, tolerance, metric)
		matched := make([]MonitoringBucketPoint, 0, len(pairs))
		deltas := make([]CoverageBucketDelta, 0, len(pairs))
		usedBaseline := make(map[int]struct{}, len(pairs))
		usedItem := make(map[int]struct{}, len(pairs))
		equal := len(pairs) == len(baseline) && len(pairs) == len(item)
		for _, pair := range pairs {
			left := baseline[pair[0]]
			right := item[pair[1]]
			usedBaseline[pair[0]] = struct{}{}
			usedItem[pair[1]] = struct{}{}
			matched = append(matched, right)
			delta := right.Value - left.Value
			if delta != 0 {
				equal = false
			}
			deltas = append(deltas, CoverageBucketDelta{
				BaselineStart: left.Start, BaselineEnd: left.End, BaselineValue: left.Value,
				ItemStart: right.Start, ItemEnd: right.End, ItemValue: right.Value,
				Delta: delta,
			})
		}
		series = append(series, Series{ItemIndex: index, MetricID: metric, Segments: splitGapSegments(matched)})
		matches = append(matches, CoverageMatch{
			ItemIndex:         index,
			Matched:           len(pairs),
			UnmatchedBaseline: len(baseline) - len(usedBaseline),
			UnmatchedItem:     len(item) - len(usedItem),
			Deltas:            deltas,
			Equal:             equal,
		})
	}
	return series, matches, nil
}

func filterMonitoringBuckets(item []MonitoringBucketPoint, metric string) []MonitoringBucketPoint {
	filtered := make([]MonitoringBucketPoint, 0, len(item))
	for _, point := range item {
		if !point.Present || (metric != "" && point.Metric != metric) {
			continue
		}
		filtered = append(filtered, point)
	}
	return filtered
}

func matchActualCoverage(baseline, candidates []MonitoringBucketPoint, tolerance time.Duration, metric string) [][2]int {
	used := make([]bool, len(candidates))
	pairs := make([][2]int, 0)
	for leftIndex, left := range baseline {
		if metric != "" && left.Metric != metric {
			continue
		}
		best := -1
		for rightIndex, right := range candidates {
			if used[rightIndex] || !right.Present || (metric != "" && right.Metric != metric) {
				continue
			}
			if left.End.Sub(left.Start) != right.End.Sub(right.Start) {
				continue
			}
			startDelta := absDuration(right.Start.Sub(left.Start))
			endDelta := absDuration(right.End.Sub(left.End))
			if startDelta > tolerance || endDelta > tolerance {
				continue
			}
			if best < 0 || compareBucketMatch(candidates[best], right, left, startDelta+endDelta, matchOffset(candidates[best], left)) < 0 {
				best = rightIndex
			}
		}
		if best >= 0 {
			used[best] = true
			pairs = append(pairs, [2]int{leftIndex, best})
		}
	}
	return pairs
}

func coverageMatchComparison(kind KindKey, match CoverageMatch) Comparison {
	reason := "actual_coverage nearest match"
	if match.UnmatchedBaseline > 0 || match.UnmatchedItem > 0 {
		reason = string(ReasonCoveragePartial)
	}
	deltas := make([]any, 0, len(match.Deltas))
	for _, delta := range match.Deltas {
		deltas = append(deltas, map[string]any{
			"baseline_start": delta.BaselineStart.UTC().Format(time.RFC3339Nano),
			"baseline_end":   delta.BaselineEnd.UTC().Format(time.RFC3339Nano),
			"item_start":     delta.ItemStart.UTC().Format(time.RFC3339Nano),
			"item_end":       delta.ItemEnd.UTC().Format(time.RFC3339Nano),
			"baseline_value": delta.BaselineValue,
			"item_value":     delta.ItemValue,
			"delta":          delta.Delta,
		})
	}
	return Comparison{
		Key:        kind,
		ItemIndex:  match.ItemIndex,
		Compatible: true,
		Reason:     reason,
		Values: map[string]any{
			"version":            "monitoring_series_comparison/v1",
			"item_index":         match.ItemIndex,
			"matched":            match.Matched,
			"unmatched_baseline": match.UnmatchedBaseline,
			"unmatched_item":     match.UnmatchedItem,
			"equal":              match.Equal,
			"deltas":             deltas,
		},
	}
}

func matchOffset(candidate, baseline MonitoringBucketPoint) time.Duration {
	return absDuration(candidate.Start.Sub(baseline.Start)) + absDuration(candidate.End.Sub(baseline.End))
}

func compareBucketMatch(current, candidate, baseline MonitoringBucketPoint, candidateOffset, currentOffset time.Duration) int {
	if candidateOffset != currentOffset {
		if candidateOffset < currentOffset {
			return -1
		}
		return 1
	}
	if candidate.Start.Before(current.Start) {
		return -1
	}
	if candidate.Start.After(current.Start) {
		return 1
	}
	if candidate.ordinal != current.ordinal {
		if candidate.ordinal < current.ordinal {
			return -1
		}
		return 1
	}
	return bytesCompareHash(candidate.Hash, current.Hash)
}

func bytesCompareHash(left, right [sha256.Size]byte) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}

func sortMonitoringBuckets(points []MonitoringBucketPoint) {
	sort.SliceStable(points, func(left, right int) bool {
		if !points[left].Start.Equal(points[right].Start) {
			return points[left].Start.Before(points[right].Start)
		}
		if !points[left].End.Equal(points[right].End) {
			return points[left].End.Before(points[right].End)
		}
		if points[left].SeriesID != points[right].SeriesID {
			return points[left].SeriesID < points[right].SeriesID
		}
		return bytesCompareHash(points[left].Hash, points[right].Hash) < 0
	})
}

func assignOrdinals(points []MonitoringBucketPoint) {
	for index := range points {
		points[index].ordinal = index
	}
}

func splitGapSegments(points []MonitoringBucketPoint) [][]SeriesPoint {
	if len(points) == 0 {
		return nil
	}
	segments := make([][]SeriesPoint, 0, 1)
	current := []SeriesPoint{seriesPoint(points[0])}
	for index := 1; index < len(points); index++ {
		if points[index].Start.After(points[index-1].End) {
			segments = append(segments, current)
			current = []SeriesPoint{seriesPoint(points[index])}
			continue
		}
		current = append(current, seriesPoint(points[index]))
	}
	return append(segments, current)
}

func seriesPoint(point MonitoringBucketPoint) SeriesPoint {
	return SeriesPoint{Start: point.Start, End: point.End, Value: point.Value}
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func parseUTCTime(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok || text == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, text)
		if err != nil {
			return time.Time{}, false
		}
	}
	return parsed.UTC(), true
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

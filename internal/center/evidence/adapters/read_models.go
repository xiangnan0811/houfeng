package adapters

import (
	"fmt"
	"sort"
	"strings"

	"houfeng/internal/center/evidence"
)

const (
	ipQualityReadModelVersion        = "ip_quality_report_read_model/v1"
	ipQualityComparisonVersion       = "ip_quality_report_comparison/v1"
	monitoringHostReadModelVersion   = "monitoring_host_read_model/v1"
	monitoringProbeReadModelVersion  = "monitoring_probe_read_model/v1"
	monitoringHostComparisonVersion  = "monitoring_host_comparison/v1"
	monitoringProbeComparisonVersion = "monitoring_probe_comparison/v1"
)

func summarizeIPQualitySnapshot(descriptor evidence.Descriptor, snapshot evidence.CanonicalSnapshot) evidence.Summary {
	payload, ok := safeSnapshotPayload(descriptor, snapshot)
	if !ok {
		return invalidEvidenceSummary(descriptor, "IP quality report")
	}
	readModel := map[string]any{
		"version":             ipQualityReadModelVersion,
		"report_id":           payload["report_id"],
		"observed_at":         payload["observed_at"],
		"received_at":         payload["received_at"],
		"ip_version":          payload["ip_version"],
		"status":              payload["status"],
		"stale":               payload["stale"],
		"stale_after_seconds": payload["stale_after_seconds"],
		"risk_level":          payload["risk_level"],
		"coverage":            cloneJSONValue(payload["coverage"]),
		"providers":           cloneJSONValue(payload["providers"]),
		"services":            cloneJSONValue(payload["services"]),
		"quality":             qualityReadModel(snapshot.Envelope().Quality),
	}
	envelope := snapshot.Envelope()
	return evidence.Summary{
		Key: descriptor.Key, RendererVersion: descriptor.Conformance.RendererVersion,
		Title: "IP quality report", SearchText: strings.TrimSpace("IP quality report " + envelope.Source.ID + " " + stringValue(payload["status"]) + " " + stringValue(payload["risk_level"])),
		ReadModel: readModel,
	}
}

func summarizeMonitoringSnapshot(descriptor evidence.Descriptor, snapshot evidence.CanonicalSnapshot) evidence.Summary {
	payload, ok := safeSnapshotPayload(descriptor, snapshot)
	if !ok {
		return invalidEvidenceSummary(descriptor, "Monitoring evidence")
	}
	version := monitoringHostReadModelVersion
	title := "Monitoring host evidence"
	if descriptor.Key == evidence.MonitoringProbeV2Key() {
		version = monitoringProbeReadModelVersion
		title = "Monitoring probe evidence"
	}
	readModel := map[string]any{
		"version":                  version,
		"requested_start":          payload["requested_start"],
		"requested_end":            payload["requested_end"],
		"coverage_start":           payload["coverage_start"],
		"coverage_end":             payload["coverage_end"],
		"actual_precision_seconds": numberAsInt64(payload["actual_precision_seconds"]),
		"buckets":                  cloneJSONValue(payload["buckets"]),
		"gaps":                     cloneJSONValue(payload["gaps"]),
		"peaks":                    cloneJSONValue(payload["peaks"]),
		"quality":                  qualityReadModel(snapshot.Envelope().Quality),
	}
	envelope := snapshot.Envelope()
	return evidence.Summary{
		Key: descriptor.Key, RendererVersion: descriptor.Conformance.RendererVersion,
		Title: title, SearchText: strings.TrimSpace(title + " " + envelope.Source.ID), ReadModel: readModel,
	}
}

func compareIPQualitySnapshots(descriptor evidence.Descriptor, left, right evidence.CanonicalSnapshot, alignment evidence.Alignment) evidence.Comparison {
	leftEnvelope, rightEnvelope := left.Envelope(), right.Envelope()
	if alignment.Mode != evidence.AlignmentExact {
		return incompatibleComparison(descriptor, "alignment_unsupported")
	}
	if leftEnvelope.Key != descriptor.Key || rightEnvelope.Key != descriptor.Key {
		return incompatibleComparison(descriptor, "kind_or_schema_mismatch")
	}
	if leftEnvelope.CalculationVersion != rightEnvelope.CalculationVersion {
		return incompatibleComparison(descriptor, "calculation_version_incompatible")
	}
	if leftEnvelope.Units.Status != evidence.UnitsNotApplicable || rightEnvelope.Units.Status != evidence.UnitsNotApplicable ||
		!equalUnitsSemantics(leftEnvelope.Units, rightEnvelope.Units) ||
		leftEnvelope.ActualPrecision != rightEnvelope.ActualPrecision || leftEnvelope.BucketWidth != rightEnvelope.BucketWidth {
		return incompatibleComparison(descriptor, "units_or_precision_incompatible")
	}
	if leftEnvelope.RequestedWindow.End.Sub(leftEnvelope.RequestedWindow.Start) != rightEnvelope.RequestedWindow.End.Sub(rightEnvelope.RequestedWindow.Start) {
		return incompatibleComparison(descriptor, "window_incompatible")
	}
	leftPayload, leftOK := safeSnapshotPayload(descriptor, left)
	rightPayload, rightOK := safeSnapshotPayload(descriptor, right)
	if !leftOK || !rightOK {
		return incompatibleComparison(descriptor, "invalid_payload")
	}
	leftCoverage, _ := leftPayload["coverage"].(map[string]any)
	rightCoverage, _ := rightPayload["coverage"].(map[string]any)
	coverageDeltas := map[string]any{}
	for _, field := range []string{
		"expected_provider_count", "successful_provider_count", "failed_provider_count", "skipped_provider_count", "not_configured_provider_count",
		"expected_service_count", "successful_service_count", "failed_service_count", "skipped_service_count", "not_configured_service_count",
	} {
		coverageDeltas[field] = numberAsInt64(rightCoverage[field]) - numberAsInt64(leftCoverage[field])
	}
	values := map[string]any{
		"version":                      ipQualityComparisonVersion,
		"equal":                        left.Hash() == right.Hash(),
		"left_hash":                    fmt.Sprintf("%x", left.Hash()),
		"right_hash":                   fmt.Sprintf("%x", right.Hash()),
		"coverage_deltas":              coverageDeltas,
		"window_duration_seconds":      int64(leftEnvelope.RequestedWindow.End.Sub(leftEnvelope.RequestedWindow.Start).Seconds()),
		"window_duration_microseconds": leftEnvelope.RequestedWindow.End.Sub(leftEnvelope.RequestedWindow.Start).Microseconds(),
		"units_status":                 string(leftEnvelope.Units.Status),
		"left": map[string]any{
			"observed_at": leftPayload["observed_at"], "status": leftPayload["status"],
			"stale": leftPayload["stale"], "stale_after_seconds": numberAsInt64(leftPayload["stale_after_seconds"]),
			"risk_level": leftPayload["risk_level"], "coverage": cloneJSONValue(leftPayload["coverage"]),
		},
		"right": map[string]any{
			"observed_at": rightPayload["observed_at"], "status": rightPayload["status"],
			"stale": rightPayload["stale"], "stale_after_seconds": numberAsInt64(rightPayload["stale_after_seconds"]),
			"risk_level": rightPayload["risk_level"], "coverage": cloneJSONValue(rightPayload["coverage"]),
		},
		"changes": map[string]any{
			"status_changed":            stringValue(leftPayload["status"]) != stringValue(rightPayload["status"]),
			"stale_changed":             boolValue(leftPayload["stale"]) != boolValue(rightPayload["stale"]),
			"stale_policy_changed":      numberAsInt64(leftPayload["stale_after_seconds"]) != numberAsInt64(rightPayload["stale_after_seconds"]),
			"stale_after_seconds_delta": numberAsInt64(rightPayload["stale_after_seconds"]) - numberAsInt64(leftPayload["stale_after_seconds"]),
			"risk_level_changed":        stringValue(leftPayload["risk_level"]) != stringValue(rightPayload["risk_level"]),
		},
	}
	return evidence.Comparison{Key: descriptor.Key, Compatible: true, Reason: "compatible_ip_quality_report_v1", Values: values}
}

func compareMonitoringSnapshots(descriptor evidence.Descriptor, left, right evidence.CanonicalSnapshot, alignment evidence.Alignment) evidence.Comparison {
	leftEnvelope, rightEnvelope := left.Envelope(), right.Envelope()
	if alignment.Mode != evidence.AlignmentExact {
		return incompatibleComparison(descriptor, "alignment_unsupported")
	}
	if leftEnvelope.Key != descriptor.Key || rightEnvelope.Key != descriptor.Key {
		return incompatibleComparison(descriptor, "kind_or_schema_mismatch")
	}
	if leftEnvelope.CalculationVersion != rightEnvelope.CalculationVersion {
		return incompatibleComparison(descriptor, "calculation_version_incompatible")
	}
	if !equalUnitsSemantics(leftEnvelope.Units, rightEnvelope.Units) {
		return incompatibleComparison(descriptor, "units_incompatible")
	}
	if leftEnvelope.RequestedWindow.End.Sub(leftEnvelope.RequestedWindow.Start) != rightEnvelope.RequestedWindow.End.Sub(rightEnvelope.RequestedWindow.Start) {
		return incompatibleComparison(descriptor, "window_incompatible")
	}
	if leftEnvelope.ActualPrecision != rightEnvelope.ActualPrecision || leftEnvelope.BucketWidth != rightEnvelope.BucketWidth {
		return incompatibleComparison(descriptor, "precision_incompatible")
	}
	leftPayload, leftOK := safeSnapshotPayload(descriptor, left)
	rightPayload, rightOK := safeSnapshotPayload(descriptor, right)
	if !leftOK || !rightOK {
		return incompatibleComparison(descriptor, "invalid_payload")
	}
	metricDeltas := monitoringMetricDeltas(leftPayload, rightPayload)
	version := monitoringHostComparisonVersion
	reason := "compatible_monitoring_host_v1"
	if descriptor.Key == evidence.MonitoringProbeV2Key() {
		version = monitoringProbeComparisonVersion
		reason = "compatible_monitoring_probe_v2"
	}
	values := map[string]any{
		"version":                      version,
		"equal":                        left.Hash() == right.Hash(),
		"left_hash":                    fmt.Sprintf("%x", left.Hash()),
		"right_hash":                   fmt.Sprintf("%x", right.Hash()),
		"window_duration_seconds":      int64(leftEnvelope.RequestedWindow.End.Sub(leftEnvelope.RequestedWindow.Start).Seconds()),
		"window_duration_microseconds": leftEnvelope.RequestedWindow.End.Sub(leftEnvelope.RequestedWindow.Start).Microseconds(),
		"actual_precision_seconds":     int64(leftEnvelope.ActualPrecision.Value.Seconds()),
		"units":                        cloneStringMap(leftEnvelope.Units.Values),
		"metric_deltas":                metricDeltas,
		"left": map[string]any{
			"quality":         qualityReadModel(leftEnvelope.Quality),
			"requested_start": leftPayload["requested_start"], "requested_end": leftPayload["requested_end"],
			"coverage_start": leftPayload["coverage_start"], "coverage_end": leftPayload["coverage_end"],
		},
		"right": map[string]any{
			"quality":         qualityReadModel(rightEnvelope.Quality),
			"requested_start": rightPayload["requested_start"], "requested_end": rightPayload["requested_end"],
			"coverage_start": rightPayload["coverage_start"], "coverage_end": rightPayload["coverage_end"],
		},
		"quality_deltas": map[string]any{
			"sample_count":      int64(rightEnvelope.Quality.SampleCount) - int64(leftEnvelope.Quality.SampleCount),
			"maintenance_count": int64(rightEnvelope.Quality.MaintenanceCount) - int64(leftEnvelope.Quality.MaintenanceCount),
			"backfilled_count":  int64(rightEnvelope.Quality.BackfilledCount) - int64(leftEnvelope.Quality.BackfilledCount),
			"bucket_count":      int64(rightEnvelope.Quality.BucketCount) - int64(leftEnvelope.Quality.BucketCount),
			"gap_count":         int64(rightEnvelope.Quality.GapCount) - int64(leftEnvelope.Quality.GapCount),
			"peak_count":        int64(rightEnvelope.Quality.PeakCount) - int64(leftEnvelope.Quality.PeakCount),
			"data_point_count":  int64(rightEnvelope.Quality.DataPointCount) - int64(leftEnvelope.Quality.DataPointCount),
		},
	}
	return evidence.Comparison{Key: descriptor.Key, Compatible: true, Reason: reason, Values: values}
}

type monitoringMetricAggregate struct {
	sampleCount   int64
	averageSum    float64
	averageWeight int64
	min           float64
	minSet        bool
	max           float64
	maxSet        bool
	p95Count      int64
	p95Sum        float64
}

type monitoringMetricKey struct {
	seriesID string
	metric   string
}

func monitoringMetricDeltas(left, right map[string]any) []any {
	leftMetrics := aggregateMonitoringMetrics(left["buckets"])
	rightMetrics := aggregateMonitoringMetrics(right["buckets"])
	keys := make([]monitoringMetricKey, 0, len(leftMetrics)+len(rightMetrics))
	seen := make(map[monitoringMetricKey]struct{}, len(leftMetrics)+len(rightMetrics))
	for key := range leftMetrics {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range rightMetrics {
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].seriesID != keys[right].seriesID {
			return keys[left].seriesID < keys[right].seriesID
		}
		return keys[left].metric < keys[right].metric
	})
	out := make([]any, 0, len(keys))
	for _, key := range keys {
		leftValue, leftOK := leftMetrics[key]
		rightValue, rightOK := rightMetrics[key]
		item := map[string]any{"series_id": key.seriesID, "metric": key.metric, "left_count": int64(0), "right_count": int64(0)}
		if leftOK {
			item["left_count"] = leftValue.sampleCount
		}
		if rightOK {
			item["right_count"] = rightValue.sampleCount
		}
		if leftOK && rightOK && leftValue.averageWeight > 0 && rightValue.averageWeight > 0 {
			leftAverage := leftValue.averageSum / float64(leftValue.averageWeight)
			rightAverage := rightValue.averageSum / float64(rightValue.averageWeight)
			item["left_average"] = leftAverage
			item["right_average"] = rightAverage
			item["average_delta"] = rightAverage - leftAverage
		}
		if leftOK && rightOK && leftValue.minSet && rightValue.minSet {
			item["left_min"] = leftValue.min
			item["right_min"] = rightValue.min
			item["min_delta"] = rightValue.min - leftValue.min
		}
		if leftOK && rightOK && leftValue.maxSet && rightValue.maxSet {
			item["left_max"] = leftValue.max
			item["right_max"] = rightValue.max
			item["max_delta"] = rightValue.max - leftValue.max
		}
		if leftOK && rightOK && leftValue.p95Count > 0 && rightValue.p95Count > 0 {
			leftP95 := leftValue.p95Sum / float64(leftValue.p95Count)
			rightP95 := rightValue.p95Sum / float64(rightValue.p95Count)
			item["left_mean_bucket_p95"] = leftP95
			item["right_mean_bucket_p95"] = rightP95
			item["mean_bucket_p95_delta"] = rightP95 - leftP95
		}
		out = append(out, item)
	}
	return out
}

func aggregateMonitoringMetrics(value any) map[monitoringMetricKey]monitoringMetricAggregate {
	result := make(map[monitoringMetricKey]monitoringMetricAggregate)
	buckets, _ := value.([]any)
	for _, rawBucket := range buckets {
		bucket, _ := rawBucket.(map[string]any)
		seriesID := stringValue(bucket["series_id"])
		sampleCount := numberAsInt64(bucket["sample_count"])
		metrics, _ := bucket["metrics"].([]any)
		for _, rawMetric := range metrics {
			metric, _ := rawMetric.(map[string]any)
			name := stringValue(metric["name"])
			if name == "" {
				continue
			}
			average, averageOK := numberValue(metric["average"])
			minimum, minOK := numberValue(metric["min"])
			maximum, maxOK := numberValue(metric["max"])
			p95, p95OK := numberValue(metric["p95"])
			key := monitoringMetricKey{seriesID: seriesID, metric: name}
			current := result[key]
			current.sampleCount += sampleCount
			if averageOK {
				current.averageSum += average * float64(sampleCount)
				current.averageWeight += sampleCount
			}
			if minOK && (!current.minSet || minimum < current.min) {
				current.min = minimum
				current.minSet = true
			}
			if maxOK && (!current.maxSet || maximum > current.max) {
				current.max = maximum
				current.maxSet = true
			}
			if p95OK {
				current.p95Sum += p95
				current.p95Count++
			}
			result[key] = current
		}
	}
	return result
}

func safeSnapshotPayload(descriptor evidence.Descriptor, snapshot evidence.CanonicalSnapshot) (map[string]any, bool) {
	if err := snapshot.Validate(descriptor); err != nil {
		return nil, false
	}
	payload, err := decodeEvidencePayload(snapshot.Bytes())
	if err != nil {
		return nil, false
	}
	canonical, _, err := evidence.CanonicalizePayload(descriptor, payload, evidence.RedactionNormalOnly)
	if err != nil {
		return nil, false
	}
	payload, err = decodeEvidencePayload(canonical.Bytes())
	return payload, err == nil
}

func invalidEvidenceSummary(descriptor evidence.Descriptor, title string) evidence.Summary {
	return evidence.Summary{Key: descriptor.Key, RendererVersion: descriptor.Conformance.RendererVersion, Title: title, SearchText: title, ReadModel: map[string]any{"version": "invalid", "quality_status": "unavailable"}}
}

func incompatibleComparison(descriptor evidence.Descriptor, reason string) evidence.Comparison {
	return evidence.Comparison{Key: descriptor.Key, Compatible: false, Reason: reason, Values: map[string]any{"version": comparisonVersion(descriptor.Key), "equal": false}}
}

func comparisonVersion(key evidence.KindKey) string {
	switch key {
	case evidence.IPQualityReportV1Key():
		return ipQualityComparisonVersion
	case evidence.MonitoringProbeV2Key():
		return monitoringProbeComparisonVersion
	default:
		return monitoringHostComparisonVersion
	}
}

func qualityReadModel(quality evidence.Quality) map[string]any {
	return map[string]any{
		"status":            string(quality.Status),
		"partial":           quality.Partial,
		"truncated":         quality.Truncated,
		"sample_count":      quality.SampleCount,
		"maintenance_count": quality.MaintenanceCount,
		"backfilled_count":  quality.BackfilledCount,
		"bucket_count":      quality.BucketCount,
		"gap_count":         quality.GapCount,
		"peak_count":        quality.PeakCount,
		"data_point_count":  quality.DataPointCount,
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	clone := make(map[string]string, len(input))
	for key, value := range input {
		clone[key] = value
	}
	return clone
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, value := range typed {
			clone[key] = cloneJSONValue(value)
		}
		return clone
	case []any:
		clone := make([]any, len(typed))
		for index, value := range typed {
			clone[index] = cloneJSONValue(value)
		}
		return clone
	default:
		return value
	}
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func numberAsInt64(value any) int64 {
	number, _ := numberValue(value)
	return int64(number)
}

func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func equalUnitsSemantics(left, right evidence.UnitsSemantics) bool {
	return left.Status == right.Status && left.Reason == right.Reason && equalStringMap(left.Values, right.Values)
}

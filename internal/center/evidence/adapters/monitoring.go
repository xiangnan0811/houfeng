package adapters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

var (
	ErrUnacceptableMonitoringEvidenceSource = errors.New("unacceptable monitoring evidence source")
	ErrMonitoringEvidenceLimitExceeded      = errors.New("monitoring evidence limit exceeded")
)

const monitoringCalculationVersion = "monitoring-evidence/v1"

type AdapterOptions struct {
	Clock       func() time.Time
	NewIntentID func() (string, error)
}

type ResolvedEvidenceSource struct {
	Subject       evidence.IdentitySnapshot
	Source        evidence.IdentitySnapshot
	Authorization evidence.AuthorizationScope
}

type EvidenceSourceResolver interface {
	ResolveEvidenceSource(context.Context, evidence.ActorScope, evidence.Selection) (ResolvedEvidenceSource, error)
}

type MonitoringEvidenceSource interface {
	LoadMonitoringHostEvidence(context.Context, string, evidence.TimeWindow, time.Duration, []string) (MonitoringSeriesCapture, error)
	LoadMonitoringProbeEvidence(context.Context, string, evidence.TimeWindow, time.Duration, []string) (MonitoringSeriesCapture, error)
}

type MonitoringSeriesCapture struct {
	RequestedWindow evidence.TimeWindow
	ActualPrecision time.Duration
	CoverageStart   time.Time
	CoverageEnd     time.Time
	ObservedAt      time.Time
	SourceWatermark string
	ProducerVersion string
	Buckets         []MonitoringBucket
	ZeroFilled      bool
	Truncated       bool
}

type MonitoringSourceLayer string

const (
	MonitoringSourceRaw            MonitoringSourceLayer = "raw"
	MonitoringSourceDailyAggregate MonitoringSourceLayer = "daily_aggregate"
)

type MonitoringBucket struct {
	SeriesID          string
	SeriesKind        string
	Start             time.Time
	End               time.Time
	SourceLayer       MonitoringSourceLayer
	SourceGranularity time.Duration
	SampleCount       uint64
	MaintenanceCount  uint64
	BackfilledCount   uint64
	Metrics           []MonitoringMetric
}

type MonitoringMetric struct {
	Name    string
	Unit    string
	Average *float64
	Min     *float64
	Max     *float64
	P95     *float64
}

func normalizeMonitoringCapture(capture MonitoringSeriesCapture) MonitoringSeriesCapture {
	capture.Buckets = append([]MonitoringBucket(nil), capture.Buckets...)
	for bucketIndex := range capture.Buckets {
		bucket := &capture.Buckets[bucketIndex]
		bucket.Metrics = append([]MonitoringMetric(nil), bucket.Metrics...)
		for metricIndex := range bucket.Metrics {
			metric := &bucket.Metrics[metricIndex]
			metric.Average = cloneFloat64Pointer(metric.Average)
			metric.Min = cloneFloat64Pointer(metric.Min)
			metric.Max = cloneFloat64Pointer(metric.Max)
			metric.P95 = cloneFloat64Pointer(metric.P95)
		}
		sort.Slice(bucket.Metrics, func(left, right int) bool {
			return bucket.Metrics[left].Name < bucket.Metrics[right].Name
		})
	}
	sort.Slice(capture.Buckets, func(left, right int) bool {
		leftBucket, rightBucket := capture.Buckets[left], capture.Buckets[right]
		if leftBucket.SeriesID != rightBucket.SeriesID {
			return leftBucket.SeriesID < rightBucket.SeriesID
		}
		if !leftBucket.Start.Equal(rightBucket.Start) {
			return leftBucket.Start.Before(rightBucket.Start)
		}
		if !leftBucket.End.Equal(rightBucket.End) {
			return leftBucket.End.Before(rightBucket.End)
		}
		if leftBucket.SeriesKind != rightBucket.SeriesKind {
			return leftBucket.SeriesKind < rightBucket.SeriesKind
		}
		return leftBucket.SourceLayer < rightBucket.SourceLayer
	})
	return capture
}

type MonitoringAdapter struct {
	source     MonitoringEvidenceSource
	resolver   EvidenceSourceResolver
	options    AdapterOptions
	key        evidence.KindKey
	sourceType recordauth.SourceKind
	descriptor evidence.Descriptor
}

func NewMonitoringHostAdapter(
	source MonitoringEvidenceSource,
	resolver EvidenceSourceResolver,
	options AdapterOptions,
) (*MonitoringAdapter, error) {
	return newMonitoringAdapter(source, resolver, options, evidence.MonitoringHostV1Key(), recordauth.SourceKindMonitoringInstance)
}

func NewMonitoringProbeAdapter(
	source MonitoringEvidenceSource,
	resolver EvidenceSourceResolver,
	options AdapterOptions,
) (*MonitoringAdapter, error) {
	return newMonitoringAdapter(source, resolver, options, evidence.MonitoringProbeV2Key(), recordauth.SourceKindTarget)
}

func newMonitoringAdapter(
	source MonitoringEvidenceSource,
	resolver EvidenceSourceResolver,
	options AdapterOptions,
	key evidence.KindKey,
	sourceType recordauth.SourceKind,
) (*MonitoringAdapter, error) {
	if source == nil || resolver == nil {
		return nil, fmt.Errorf("%w: nil monitoring adapter dependency", evidence.ErrInvalidKindDescriptor)
	}
	descriptor := monitoringDescriptor(key)
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	return &MonitoringAdapter{
		source: source, resolver: resolver, options: options,
		key: key, sourceType: sourceType, descriptor: descriptor,
	}, nil
}

func (adapter *MonitoringAdapter) Descriptor() evidence.Descriptor {
	return adapter.descriptor
}

func (adapter *MonitoringAdapter) ValidateSelection(
	_ context.Context,
	_ evidence.ActorScope,
	selection evidence.Selection,
) error {
	if adapter == nil || selection.Key != adapter.key || selection.SourceType != string(adapter.sourceType) ||
		selection.SourceID == "" || !validEvidenceWindow(selection.RequestedWindow) ||
		len(selection.Metrics) == 0 || len(selection.SensitiveTopologyFields) != 0 {
		return fmt.Errorf("%w: monitoring selection", evidence.ErrInvalidCanonicalPayload)
	}
	allowed := adapter.metricUnits()
	for index, metric := range selection.Metrics {
		if _, ok := allowed[metric]; !ok || (index > 0 && selection.Metrics[index-1] >= metric) {
			return fmt.Errorf("%w: monitoring metrics", evidence.ErrInvalidCanonicalPayload)
		}
	}
	defaultPrecision := defaultMonitoringPrecision(selection.RequestedWindow.End.Sub(selection.RequestedWindow.Start))
	if selection.Precision != 0 && (selection.Precision < defaultPrecision || selection.Precision > 24*time.Hour || selection.Precision%time.Minute != 0) {
		return fmt.Errorf("%w: monitoring precision", evidence.ErrInvalidCanonicalPayload)
	}
	precision := selection.Precision
	if precision == 0 {
		precision = defaultPrecision
	}
	if monitoringBucketCount(selection.RequestedWindow, precision) > evidence.MaxMetricBucketCount {
		return ErrMonitoringEvidenceLimitExceeded
	}
	return nil
}

func (adapter *MonitoringAdapter) PreviewCapture(
	ctx context.Context,
	actor evidence.ActorScope,
	selection evidence.Selection,
) (evidence.Preview, error) {
	if err := adapter.ValidateSelection(ctx, actor, selection); err != nil {
		return evidence.Preview{}, err
	}
	evaluated, err := adapter.evaluate(ctx, actor, selection)
	if err != nil {
		return evidence.Preview{}, err
	}
	intentID, err := newAdapterIntentID(adapter.options)
	if err != nil {
		return evidence.Preview{}, err
	}
	now := adapterNow(adapter.options)
	return evidence.Preview{
		IntentID:                intentID,
		Key:                     adapter.key,
		Selection:               selection,
		Subject:                 evaluated.resolved.Subject,
		Source:                  evaluated.resolved.Source,
		RequestedWindow:         selection.RequestedWindow,
		ActualWindow:            evidence.TimeWindow{Start: evaluated.capture.CoverageStart, End: evaluated.capture.CoverageEnd},
		ObservedAt:              evaluated.capture.ObservedAt.UTC(),
		SourceWatermark:         evaluated.capture.SourceWatermark,
		ProducerVersion:         evaluated.capture.ProducerVersion,
		CalculationVersion:      monitoringCalculationVersion,
		Units:                   evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: adapter.selectedUnits(selection.Metrics)},
		Quality:                 evaluated.quality,
		Sensitivity:             evidence.SensitivityNormal,
		ActualPrecision:         evidence.DurationSemantics{Applicable: true, Value: evaluated.capture.ActualPrecision},
		BucketWidth:             evidence.DurationSemantics{Applicable: true, Value: evaluated.capture.ActualPrecision},
		QuotaOutcome:            evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention:               immutableEvidenceRetention(),
		Redaction:               evaluated.previewRedaction,
		EstimatedCanonicalBytes: evaluated.canonical.Size(),
		SourceDigest:            evaluated.canonical.Hash(),
		RendererVersion:         adapter.descriptor.Conformance.RendererVersion,
		PreviewedAt:             now,
		ValidUntil:              now.Add(evidence.CaptureIntentTTL),
	}, nil
}

func (adapter *MonitoringAdapter) Capture(
	ctx context.Context,
	actor evidence.ActorScope,
	intent evidence.Intent,
) (evidence.CanonicalSnapshot, error) {
	if adapter == nil || intent.Key != adapter.key || !evidence.ValidCaptureIntentID(intent.ID) ||
		intent.PreviewDigest == [32]byte{} || intent.ValidUntil.IsZero() {
		return evidence.CanonicalSnapshot{}, fmt.Errorf("%w: monitoring intent", evidence.ErrInvalidCanonicalPayload)
	}
	if err := adapter.ValidateSelection(ctx, actor, intent.Selection); err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	evaluated, err := adapter.evaluate(ctx, actor, intent.Selection)
	if err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	captureRedaction, err := evidence.NormalizeCaptureRedaction(adapter.descriptor, evaluated.previewRedaction)
	if err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	now := adapterNow(adapter.options)
	envelope := evidence.SnapshotEnvelope{
		Key:                adapter.key,
		Subject:            evaluated.resolved.Subject,
		Source:             evaluated.resolved.Source,
		Authorization:      evaluated.resolved.Authorization,
		RequestedWindow:    intent.Selection.RequestedWindow,
		ActualWindow:       evidence.TimeWindow{Start: evaluated.capture.CoverageStart, End: evaluated.capture.CoverageEnd},
		ObservedAt:         evaluated.capture.ObservedAt.UTC(),
		CapturedAt:         now,
		ReferencedAt:       now,
		SourceWatermark:    evaluated.capture.SourceWatermark,
		SourceDigest:       evaluated.canonical.Hash(),
		ProducerVersion:    evaluated.capture.ProducerVersion,
		CalculationVersion: monitoringCalculationVersion,
		Units:              evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: adapter.selectedUnits(intent.Selection.Metrics)},
		Quality:            evaluated.quality,
		Sensitivity:        evidence.SensitivityNormal,
		ActualPrecision:    evidence.DurationSemantics{Applicable: true, Value: evaluated.capture.ActualPrecision},
		BucketWidth:        evidence.DurationSemantics{Applicable: true, Value: evaluated.capture.ActualPrecision},
		QuotaOutcome:       evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention:          immutableEvidenceRetention(),
		Redaction:          captureRedaction,
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(
		adapter.descriptor,
		envelope,
		evaluated.payload,
		evidence.RedactionNormalOnly,
	)
	return snapshot, err
}

func (adapter *MonitoringAdapter) Authorize(
	ctx context.Context,
	actor evidence.ActorScope,
	selection evidence.Selection,
) (evidence.AuthorizationScope, error) {
	if err := adapter.ValidateSelection(ctx, actor, selection); err != nil {
		return evidence.AuthorizationScope{}, err
	}
	resolved, err := resolveEvidenceSource(ctx, adapter.resolver, actor, selection)
	if err != nil {
		return evidence.AuthorizationScope{}, err
	}
	return resolved.Authorization, nil
}

func (adapter *MonitoringAdapter) Summarize(snapshot evidence.CanonicalSnapshot) evidence.Summary {
	return summarizeMonitoringSnapshot(adapter.descriptor, snapshot)
}

func (adapter *MonitoringAdapter) Compare(left, right evidence.CanonicalSnapshot, alignment evidence.Alignment) evidence.Comparison {
	return compareMonitoringSnapshots(adapter.descriptor, left, right, alignment)
}

func (adapter *MonitoringAdapter) Export(snapshot evidence.CanonicalSnapshot, mode evidence.ExportMode) evidence.ExportMaterial {
	return exportEvidenceSnapshot(adapter.descriptor, snapshot, mode)
}

type evaluatedMonitoring struct {
	capture          MonitoringSeriesCapture
	resolved         ResolvedEvidenceSource
	payload          map[string]any
	canonical        evidence.CanonicalPayload
	previewRedaction []evidence.FieldDecision
	quality          evidence.Quality
}

func (adapter *MonitoringAdapter) evaluate(
	ctx context.Context,
	actor evidence.ActorScope,
	selection evidence.Selection,
) (evaluatedMonitoring, error) {
	resolved, err := resolveEvidenceSource(ctx, adapter.resolver, actor, selection)
	if err != nil {
		return evaluatedMonitoring{}, err
	}
	precision := selection.Precision
	if precision == 0 {
		precision = defaultMonitoringPrecision(selection.RequestedWindow.End.Sub(selection.RequestedWindow.Start))
	}
	var capture MonitoringSeriesCapture
	if adapter.key == evidence.MonitoringHostV1Key() {
		capture, err = adapter.source.LoadMonitoringHostEvidence(
			ctx, selection.SourceID, selection.RequestedWindow, precision, append([]string(nil), selection.Metrics...),
		)
	} else {
		capture, err = adapter.source.LoadMonitoringProbeEvidence(
			ctx, selection.SourceID, selection.RequestedWindow, precision, append([]string(nil), selection.Metrics...),
		)
	}
	if err != nil {
		return evaluatedMonitoring{}, err
	}
	capture = normalizeMonitoringCapture(capture)
	if err := adapter.validateCapture(selection, precision, capture); err != nil {
		return evaluatedMonitoring{}, err
	}
	gaps, err := monitoringGaps(capture)
	if err != nil {
		return evaluatedMonitoring{}, err
	}
	peaks := monitoringPeaks(capture.Buckets)
	quality, err := monitoringQuality(selection.RequestedWindow, capture, gaps, peaks)
	if err != nil {
		return evaluatedMonitoring{}, err
	}
	payload := monitoringPayload(selection.RequestedWindow, capture, gaps, peaks)
	canonical, redaction, err := evidence.CanonicalizePayload(adapter.descriptor, payload, evidence.RedactionNormalOnly)
	if err != nil {
		return evaluatedMonitoring{}, err
	}
	redaction.Decisions = appendForbiddenPreviewDecisions(adapter.descriptor, redaction.Decisions)
	return evaluatedMonitoring{
		capture: capture, resolved: resolved, payload: payload, canonical: canonical,
		previewRedaction: redaction.Decisions, quality: quality,
	}, nil
}

func (adapter *MonitoringAdapter) validateCapture(
	selection evidence.Selection,
	requestedPrecision time.Duration,
	capture MonitoringSeriesCapture,
) error {
	if capture.ZeroFilled || capture.Truncated {
		return ErrUnacceptableMonitoringEvidenceSource
	}
	now := adapterNow(adapter.options)
	watermark, watermarkErr := time.Parse(time.RFC3339Nano, capture.SourceWatermark)
	if capture.RequestedWindow != selection.RequestedWindow || capture.ActualPrecision < requestedPrecision ||
		capture.ActualPrecision <= 0 || capture.ActualPrecision%time.Minute != 0 ||
		!postgresTimestampRepresentable(capture.CoverageStart) || !postgresTimestampRepresentable(capture.CoverageEnd) ||
		!postgresTimestampRepresentable(capture.ObservedAt) ||
		capture.CoverageStart.Before(selection.RequestedWindow.Start) || capture.CoverageEnd.After(selection.RequestedWindow.End) ||
		!capture.CoverageEnd.After(capture.CoverageStart) || capture.ObservedAt.Before(capture.CoverageStart) ||
		!capture.ObservedAt.Before(capture.CoverageEnd) || now.Before(capture.ObservedAt) ||
		watermarkErr != nil || !postgresTimestampRepresentable(watermark) ||
		watermark.UTC().Format(time.RFC3339Nano) != capture.SourceWatermark || watermark.Before(capture.ObservedAt) || now.Before(watermark) ||
		capture.ProducerVersion == "" || len(capture.Buckets) == 0 || uint64(len(capture.Buckets)) > evidence.MaxSnapshotDataPoints {
		return fmt.Errorf("%w: monitoring coverage", ErrUnacceptableMonitoringEvidenceSource)
	}
	units := adapter.metricUnits()
	seriesEnds := make(map[string]time.Time)
	seriesKinds := make(map[string]string)
	var dataPointCount uint64
	for _, bucket := range capture.Buckets {
		if bucket.SampleCount == 0 || !postgresTimestampRepresentable(bucket.Start) || !postgresTimestampRepresentable(bucket.End) ||
			!bucket.End.After(bucket.Start) ||
			bucket.Start.Before(selection.RequestedWindow.Start) || bucket.End.After(selection.RequestedWindow.End) ||
			bucket.End.Sub(bucket.Start) > capture.ActualPrecision ||
			bucket.SourceGranularity <= 0 || bucket.SourceGranularity > capture.ActualPrecision ||
			bucket.SourceGranularity%time.Second != 0 || bucket.SampleCount > math.MaxInt64 ||
			bucket.MaintenanceCount > bucket.SampleCount || bucket.BackfilledCount > bucket.SampleCount ||
			(bucket.SourceLayer != MonitoringSourceRaw && bucket.SourceLayer != MonitoringSourceDailyAggregate) ||
			len(bucket.Metrics) == 0 {
			return ErrUnacceptableMonitoringEvidenceSource
		}
		if kind, ok := seriesKinds[bucket.SeriesID]; ok && kind != bucket.SeriesKind {
			return ErrUnacceptableMonitoringEvidenceSource
		}
		seriesKinds[bucket.SeriesID] = bucket.SeriesKind
		if previousEnd := seriesEnds[bucket.SeriesID]; !previousEnd.IsZero() && bucket.Start.Before(previousEnd) {
			return ErrUnacceptableMonitoringEvidenceSource
		}
		if bucket.End.After(seriesEnds[bucket.SeriesID]) {
			seriesEnds[bucket.SeriesID] = bucket.End
		}
		for metricIndex, metric := range bucket.Metrics {
			if units[metric.Name] != metric.Unit || !containsString(selection.Metrics, metric.Name) ||
				!validMonitoringMetric(metric) || (metricIndex > 0 && bucket.Metrics[metricIndex-1].Name == metric.Name) {
				return ErrUnacceptableMonitoringEvidenceSource
			}
		}
		if uint64(len(bucket.Metrics)) > evidence.MaxSnapshotDataPoints-dataPointCount {
			return ErrMonitoringEvidenceLimitExceeded
		}
		dataPointCount += uint64(len(bucket.Metrics))
	}
	return nil
}

type monitoringGap struct {
	SeriesID string
	Start    time.Time
	End      time.Time
}

type monitoringPeak struct {
	SeriesID    string
	Metric      string
	At          time.Time
	Value       float64
	SourceLayer MonitoringSourceLayer
}

func monitoringGaps(capture MonitoringSeriesCapture) ([]monitoringGap, error) {
	bySeries := make(map[string][]MonitoringBucket)
	for _, bucket := range capture.Buckets {
		bySeries[bucket.SeriesID] = append(bySeries[bucket.SeriesID], bucket)
	}
	var gaps []monitoringGap
	for seriesID, buckets := range bySeries {
		sort.Slice(buckets, func(left, right int) bool { return buckets[left].Start.Before(buckets[right].Start) })
		cursor := capture.RequestedWindow.Start
		for _, bucket := range buckets {
			var err error
			gaps, err = appendMonitoringGaps(gaps, seriesID, cursor, bucket.Start, capture.ActualPrecision)
			if err != nil {
				return nil, err
			}
			if bucket.End.After(cursor) {
				cursor = bucket.End
			}
		}
		var err error
		gaps, err = appendMonitoringGaps(gaps, seriesID, cursor, capture.RequestedWindow.End, capture.ActualPrecision)
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(gaps, func(left, right int) bool {
		if gaps[left].SeriesID != gaps[right].SeriesID {
			return gaps[left].SeriesID < gaps[right].SeriesID
		}
		return gaps[left].Start.Before(gaps[right].Start)
	})
	return gaps, nil
}

func appendMonitoringGaps(gaps []monitoringGap, seriesID string, start, end time.Time, precision time.Duration) ([]monitoringGap, error) {
	for cursor := start; cursor.Before(end); cursor = cursor.Add(precision) {
		if uint64(len(gaps)) >= evidence.MaxSnapshotDataPoints {
			return nil, ErrMonitoringEvidenceLimitExceeded
		}
		gapEnd := cursor.Add(precision)
		if gapEnd.After(end) {
			gapEnd = end
		}
		gaps = append(gaps, monitoringGap{SeriesID: seriesID, Start: cursor, End: gapEnd})
	}
	return gaps, nil
}

func monitoringPeaks(buckets []MonitoringBucket) []monitoringPeak {
	peaks := make([]monitoringPeak, 0)
	for _, bucket := range buckets {
		for _, metric := range bucket.Metrics {
			value := metric.Average
			if metric.Max != nil {
				value = metric.Max
			}
			if value != nil {
				peaks = append(peaks, monitoringPeak{
					SeriesID: bucket.SeriesID, Metric: metric.Name, At: bucket.Start,
					Value: *value, SourceLayer: bucket.SourceLayer,
				})
			}
		}
	}
	sort.Slice(peaks, func(left, right int) bool {
		if peaks[left].Value != peaks[right].Value {
			return peaks[left].Value > peaks[right].Value
		}
		if peaks[left].SeriesID != peaks[right].SeriesID {
			return peaks[left].SeriesID < peaks[right].SeriesID
		}
		if peaks[left].Metric != peaks[right].Metric {
			return peaks[left].Metric < peaks[right].Metric
		}
		return peaks[left].At.Before(peaks[right].At)
	})
	if len(peaks) > int(evidence.MaxPeakCount) {
		peaks = peaks[:evidence.MaxPeakCount]
	}
	return peaks
}

func monitoringQuality(
	requested evidence.TimeWindow,
	capture MonitoringSeriesCapture,
	gaps []monitoringGap,
	peaks []monitoringPeak,
) (evidence.Quality, error) {
	quality := evidence.Quality{
		Status:    evidence.QualityComplete,
		GapCount:  uint64(len(gaps)),
		PeakCount: uint64(len(peaks)),
	}
	seriesBuckets := make(map[string]uint64)
	for _, bucket := range capture.Buckets {
		if bucket.SampleCount > math.MaxInt64-quality.SampleCount ||
			bucket.MaintenanceCount > math.MaxInt64-quality.MaintenanceCount ||
			bucket.BackfilledCount > math.MaxInt64-quality.BackfilledCount {
			return evidence.Quality{}, ErrMonitoringEvidenceLimitExceeded
		}
		quality.SampleCount += bucket.SampleCount
		quality.MaintenanceCount += bucket.MaintenanceCount
		quality.BackfilledCount += bucket.BackfilledCount
		if uint64(len(bucket.Metrics)) > evidence.MaxSnapshotDataPoints-quality.DataPointCount {
			return evidence.Quality{}, ErrMonitoringEvidenceLimitExceeded
		}
		quality.DataPointCount += uint64(len(bucket.Metrics))
		seriesBuckets[bucket.SeriesID]++
	}
	for _, count := range seriesBuckets {
		if count > quality.BucketCount {
			quality.BucketCount = count
		}
	}
	if quality.BucketCount > evidence.MaxMetricBucketCount || quality.DataPointCount > evidence.MaxSnapshotDataPoints {
		return evidence.Quality{}, ErrMonitoringEvidenceLimitExceeded
	}
	if quality.GapCount > 0 || capture.CoverageStart.After(requested.Start) || capture.CoverageEnd.Before(requested.End) {
		quality.Status = evidence.QualityPartial
		quality.Partial = true
	}
	return quality, nil
}

func monitoringPayload(
	requested evidence.TimeWindow,
	capture MonitoringSeriesCapture,
	gaps []monitoringGap,
	peaks []monitoringPeak,
) map[string]any {
	buckets := make([]any, 0, len(capture.Buckets))
	for _, bucket := range capture.Buckets {
		metrics := make([]any, 0, len(bucket.Metrics))
		for _, metric := range bucket.Metrics {
			item := map[string]any{"name": metric.Name, "unit": metric.Unit}
			if metric.Average != nil {
				item["average"] = *metric.Average
			}
			if metric.Min != nil {
				item["min"] = *metric.Min
			}
			if metric.Max != nil {
				item["max"] = *metric.Max
			}
			if metric.P95 != nil {
				item["p95"] = *metric.P95
			}
			metrics = append(metrics, item)
		}
		buckets = append(buckets, map[string]any{
			"series_id":                  bucket.SeriesID,
			"series_kind":                bucket.SeriesKind,
			"start":                      bucket.Start.UTC().Format(time.RFC3339Nano),
			"end":                        bucket.End.UTC().Format(time.RFC3339Nano),
			"source_layer":               string(bucket.SourceLayer),
			"source_granularity_seconds": int64(bucket.SourceGranularity / time.Second),
			"sample_count":               bucket.SampleCount,
			"maintenance_count":          bucket.MaintenanceCount,
			"backfilled_count":           bucket.BackfilledCount,
			"metrics":                    metrics,
		})
	}
	gapValues := make([]any, 0, len(gaps))
	for _, gap := range gaps {
		gapValues = append(gapValues, map[string]any{
			"series_id": gap.SeriesID,
			"start":     gap.Start.UTC().Format(time.RFC3339Nano),
			"end":       gap.End.UTC().Format(time.RFC3339Nano),
		})
	}
	peakValues := make([]any, 0, len(peaks))
	for _, peak := range peaks {
		peakValues = append(peakValues, map[string]any{
			"series_id":    peak.SeriesID,
			"metric":       peak.Metric,
			"at":           peak.At.UTC().Format(time.RFC3339Nano),
			"value":        peak.Value,
			"source_layer": string(peak.SourceLayer),
		})
	}
	return map[string]any{
		"requested_start":          requested.Start.UTC().Format(time.RFC3339Nano),
		"requested_end":            requested.End.UTC().Format(time.RFC3339Nano),
		"coverage_start":           capture.CoverageStart.UTC().Format(time.RFC3339Nano),
		"coverage_end":             capture.CoverageEnd.UTC().Format(time.RFC3339Nano),
		"actual_precision_seconds": int64(capture.ActualPrecision / time.Second),
		"buckets":                  buckets,
		"gaps":                     gapValues,
		"peaks":                    peakValues,
	}
}

func monitoringDescriptor(key evidence.KindKey) evidence.Descriptor {
	normal := []string{
		"requested_start", "requested_end", "coverage_start", "coverage_end", "actual_precision_seconds",
		"buckets.series_id", "buckets.series_kind", "buckets.start", "buckets.end", "buckets.source_layer",
		"buckets.source_granularity_seconds", "buckets.sample_count", "buckets.maintenance_count", "buckets.backfilled_count",
		"buckets.metrics.name", "buckets.metrics.unit", "buckets.metrics.average", "buckets.metrics.min", "buckets.metrics.max", "buckets.metrics.p95",
		"gaps.series_id", "gaps.start", "gaps.end",
		"peaks.series_id", "peaks.metric", "peaks.at", "peaks.value", "peaks.source_layer",
	}
	forbidden := []string{"buckets.raw_json", "buckets.fingerprint", "raw_payload", "stdout", "stderr"}
	fields := make([]evidence.FieldDefinition, 0, len(normal)+len(forbidden))
	for _, path := range normal {
		fields = append(fields, evidence.FieldDefinition{Path: path, Sensitivity: evidence.SensitivityNormal})
	}
	for _, path := range forbidden {
		fields = append(fields, evidence.FieldDefinition{Path: path, Sensitivity: evidence.SensitivityForbidden})
	}
	renderer := "monitoring_host_v1"
	if key == evidence.MonitoringProbeV2Key() {
		renderer = "monitoring_probe_v2"
	}
	return evidence.Descriptor{
		Key:    key,
		Fields: fields,
		Conformance: evidence.ConformanceMetadata{
			CanonicalizationVersion: evidence.CanonicalizationVersionV1,
			ForbiddenCorpusVersion:  evidence.ForbiddenCorpusVersionV1,
			RendererVersion:         renderer,
			MaxCanonicalBytes:       evidence.MaxCanonicalPayloadBytes,
		},
	}
}

var hostMonitoringMetricUnits = map[string]string{
	"cpu_iowait_pct":           "percent",
	"cpu_steal_pct":            "percent",
	"cpu_usage_pct":            "percent",
	"disk_busy_pct":            "percent",
	"disk_read_bytes_per_sec":  "bytes_per_second",
	"disk_total_bytes":         "bytes",
	"disk_used_pct":            "percent",
	"disk_write_bytes_per_sec": "bytes_per_second",
	"inode_used_pct":           "percent",
	"load_1":                   "load",
	"load_15":                  "load",
	"load_5":                   "load",
	"mem_available_bytes":      "bytes",
	"mem_total_bytes":          "bytes",
	"mem_used_pct":             "percent",
	"net_in_bytes_per_sec":     "bytes_per_second",
	"net_out_bytes_per_sec":    "bytes_per_second",
	"swap_used_pct":            "percent",
	"uptime_seconds":           "seconds",
}

var probeMonitoringMetricUnits = map[string]string{
	"http_status":     "status_code",
	"latency_ms":      "ms",
	"success_ratio":   "ratio",
	"tls_expiry_days": "days",
}

func (adapter *MonitoringAdapter) metricUnits() map[string]string {
	if adapter.key == evidence.MonitoringProbeV2Key() {
		return probeMonitoringMetricUnits
	}
	return hostMonitoringMetricUnits
}

func (adapter *MonitoringAdapter) selectedUnits(metrics []string) map[string]string {
	units := make(map[string]string, len(metrics))
	for _, metric := range metrics {
		units[metric] = adapter.metricUnits()[metric]
	}
	return units
}

func defaultMonitoringPrecision(duration time.Duration) time.Duration {
	switch {
	case duration <= 6*time.Hour:
		return time.Minute
	case duration <= 48*time.Hour:
		return 5 * time.Minute
	case duration <= 30*24*time.Hour:
		return time.Hour
	}
	return 24 * time.Hour
}

func monitoringBucketCount(window evidence.TimeWindow, precision time.Duration) uint64 {
	return uint64(math.Ceil(float64(window.End.Sub(window.Start)) / float64(precision)))
}

func containsString(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func validMonitoringMetric(metric MonitoringMetric) bool {
	values := []*float64{metric.Average, metric.Min, metric.Max, metric.P95}
	seen := false
	for _, value := range values {
		if value == nil {
			continue
		}
		if math.IsNaN(*value) || math.IsInf(*value, 0) {
			return false
		}
		seen = true
	}
	return seen
}

func adapterNow(options AdapterOptions) time.Time {
	if options.Clock != nil {
		return options.Clock().UTC().Round(0).Truncate(time.Microsecond)
	}
	return time.Now().UTC().Round(0).Truncate(time.Microsecond)
}

func newAdapterIntentID(options AdapterOptions) (string, error) {
	if options.NewIntentID != nil {
		intentID, err := options.NewIntentID()
		if err != nil {
			return "", err
		}
		if !evidence.ValidCaptureIntentID(intentID) {
			return "", fmt.Errorf("%w: capture intent ID", evidence.ErrInvalidCanonicalPayload)
		}
		return intentID, nil
	}
	var entropy [12]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate evidence intent ID: %w", err)
	}
	return "evi_" + hex.EncodeToString(entropy[:]), nil
}

func validEvidenceWindow(window evidence.TimeWindow) bool {
	return !window.Start.IsZero() && !window.End.IsZero() && window.End.After(window.Start) &&
		window.Start == window.Start.UTC().Round(0) && window.End == window.End.UTC().Round(0) &&
		window.Start.Nanosecond()%int(time.Microsecond) == 0 && window.End.Nanosecond()%int(time.Microsecond) == 0
}

func postgresTimestampRepresentable(value time.Time) bool {
	return !value.IsZero() && value.Equal(value.Truncate(time.Microsecond))
}

func immutableEvidenceRetention() evidence.RetentionSemantics {
	return evidence.RetentionSemantics{
		Immutable:      true,
		Scope:          evidence.RetentionScopeRecordRevision,
		SourceDeletion: evidence.SourceDeletionSnapshotRetained,
	}
}

func resolveEvidenceSource(
	ctx context.Context,
	resolver EvidenceSourceResolver,
	actor evidence.ActorScope,
	selection evidence.Selection,
) (ResolvedEvidenceSource, error) {
	if ctx == nil || resolver == nil {
		return ResolvedEvidenceSource{}, recordauth.ErrDenied
	}
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return ResolvedEvidenceSource{}, recordauth.ErrDenied
	}
	resolved, err := resolver.ResolveEvidenceSource(ctx, normalizedActor, selection)
	if err != nil {
		return ResolvedEvidenceSource{}, err
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(resolved.Authorization)
	if err != nil || authorization.Digest != resolved.Authorization.Digest ||
		authorization.State != recordauth.SourceStateLive || authorization.CurrentScope == nil ||
		string(authorization.Kind) != selection.SourceType || authorization.SourceID != selection.SourceID ||
		resolved.Source.Type != selection.SourceType || resolved.Source.ID != selection.SourceID {
		return ResolvedEvidenceSource{}, recordauth.ErrDenied
	}
	resource := recordauth.ResourceScope{
		Version:    recordauth.ResourceScopeVersionV1,
		ProjectID:  authorization.CaptureScope.ProjectID,
		Visibility: authorization.CaptureScope,
		Sources:    []recordauth.SourceAuthorization{authorization},
	}
	if err := recordauth.Authorize(normalizedActor, recordauth.CapabilityEvidenceCreate, resource); err != nil {
		return ResolvedEvidenceSource{}, err
	}
	resolved.Authorization = authorization
	return resolved, nil
}

func summarizeEvidenceSnapshot(descriptor evidence.Descriptor, snapshot evidence.CanonicalSnapshot) evidence.Summary {
	envelope := snapshot.Envelope()
	return evidence.Summary{
		Key: descriptor.Key, RendererVersion: descriptor.Conformance.RendererVersion,
		Title:      string(descriptor.Key.Kind),
		SearchText: string(descriptor.Key.Kind) + " " + envelope.Source.ID,
		ReadModel: map[string]any{
			"source_id":        envelope.Source.ID,
			"quality_status":   string(envelope.Quality.Status),
			"sample_count":     envelope.Quality.SampleCount,
			"gap_count":        envelope.Quality.GapCount,
			"bucket_count":     envelope.Quality.BucketCount,
			"data_point_count": envelope.Quality.DataPointCount,
		},
	}
}

func compareEvidenceSnapshots(
	descriptor evidence.Descriptor,
	left, right evidence.CanonicalSnapshot,
	alignment evidence.Alignment,
) evidence.Comparison {
	compatible := alignment.Mode == evidence.AlignmentExact && left.Envelope().Key == descriptor.Key && right.Envelope().Key == descriptor.Key
	return evidence.Comparison{
		Key: descriptor.Key, Compatible: compatible,
		Reason: "exact evidence snapshots",
		Values: map[string]any{
			"left_hash":  fmt.Sprintf("%x", left.Hash()),
			"right_hash": fmt.Sprintf("%x", right.Hash()),
			"equal":      left.Hash() == right.Hash(),
		},
	}
}

func exportEvidenceSnapshot(
	descriptor evidence.Descriptor,
	snapshot evidence.CanonicalSnapshot,
	mode evidence.ExportMode,
) evidence.ExportMaterial {
	if mode != evidence.ExportModeSafe && mode != evidence.ExportModeSensitiveTopology {
		return evidence.ExportMaterial{Key: descriptor.Key}
	}
	payload, err := decodeEvidencePayload(snapshot.Bytes())
	if err != nil {
		return evidence.ExportMaterial{Key: descriptor.Key}
	}
	redactionMode := evidence.RedactionNormalOnly
	if mode == evidence.ExportModeSensitiveTopology {
		redactionMode = evidence.RedactionIncludeSensitiveTopology
	}
	canonical, _, err := evidence.CanonicalizePayload(descriptor, payload, redactionMode)
	if err != nil {
		return evidence.ExportMaterial{Key: descriptor.Key}
	}
	filename := string(descriptor.Key.Kind) + "_v" + fmt.Sprint(descriptor.Key.SchemaVersion) + ".json"
	return evidence.ExportMaterial{Key: descriptor.Key, MediaType: "application/json", Filename: filename, Bytes: canonical.Bytes()}
}

func decodeEvidencePayload(encoded []byte) (map[string]any, error) {
	var document struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil || document.Payload == nil {
		return nil, evidence.ErrInvalidCanonicalPayload
	}
	return document.Payload, nil
}

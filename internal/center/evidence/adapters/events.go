package adapters

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/recordauth"
)

const monitoringEventCalculationVersion = "monitoring-event-evidence/v2"

type MonitoringEventSource interface {
	LoadMonitoringEventEvidence(context.Context, string, string, evidence.TimeWindow) (MonitoringEventCapture, error)
}

type MonitoringEventCapture struct {
	EventCount      uint64
	ProducerVersion string
	SourceWatermark string
	Events          []MonitoringEventFact
}

type MonitoringEventFact struct {
	EventID             string
	ObjectType          string
	ObjectID            string
	EventType           string
	Severity            string
	Summary             string
	EventAt             time.Time
	RecordedAt          time.Time
	Backfilled          bool
	Provenance          string
	ProducerVersion     string
	RuleVersion         string
	PriorState          string
	ResultingState      string
	CorrectionOfEventID string
	Metrics             []MonitoringEventMetric
}

type MonitoringEventMetric struct {
	Metric    string
	Unit      string
	Value     float64
	Threshold float64
}

type MonitoringEventAdapter struct {
	source     MonitoringEventSource
	resolver   EvidenceSourceResolver
	options    AdapterOptions
	descriptor evidence.Descriptor
}

func NewMonitoringEventAdapter(source MonitoringEventSource, resolver EvidenceSourceResolver, options AdapterOptions) (*MonitoringEventAdapter, error) {
	if source == nil || resolver == nil {
		return nil, fmt.Errorf("%w: nil monitoring event adapter dependency", evidence.ErrInvalidKindDescriptor)
	}
	descriptor := monitoringEventDescriptor()
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	return &MonitoringEventAdapter{source: source, resolver: resolver, options: options, descriptor: descriptor}, nil
}

func (adapter *MonitoringEventAdapter) Descriptor() evidence.Descriptor { return adapter.descriptor }

func (adapter *MonitoringEventAdapter) ValidateSelection(_ context.Context, _ evidence.ActorScope, selection evidence.Selection) error {
	if adapter == nil || selection.Key != evidence.MonitoringEventV2Key() ||
		(selection.SourceType != string(recordauth.SourceKindMonitoringInstance) && selection.SourceType != string(recordauth.SourceKindTarget)) ||
		!validSourceIdentifier(selection.SourceID) || !validEvidenceWindow(selection.RequestedWindow) ||
		len(selection.Metrics) != 0 || selection.Precision != 0 || len(selection.SensitiveTopologyFields) != 0 {
		return fmt.Errorf("%w: monitoring event selection", evidence.ErrInvalidCanonicalPayload)
	}
	return nil
}

func (adapter *MonitoringEventAdapter) PreviewCapture(ctx context.Context, actor evidence.ActorScope, selection evidence.Selection) (evidence.Preview, error) {
	if err := adapter.ValidateSelection(ctx, actor, selection); err != nil {
		return evidence.Preview{}, err
	}
	evaluated, err := adapter.evaluate(ctx, actor, selection)
	if err != nil {
		return evidence.Preview{}, err
	}
	return previewDiscreteEvidence(adapter.options, adapter.descriptor, selection, evaluated)
}

func (adapter *MonitoringEventAdapter) Capture(ctx context.Context, actor evidence.ActorScope, intent evidence.Intent) (evidence.CanonicalSnapshot, error) {
	if err := validateDiscreteIntent(adapter.descriptor.Key, intent); err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	if err := adapter.ValidateSelection(ctx, actor, intent.Selection); err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	evaluated, err := adapter.evaluate(ctx, actor, intent.Selection)
	if err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	return captureDiscreteEvidence(adapter.options, adapter.descriptor, intent.Selection, evaluated)
}

func (adapter *MonitoringEventAdapter) Authorize(ctx context.Context, actor evidence.ActorScope, selection evidence.Selection) (evidence.AuthorizationScope, error) {
	if err := adapter.ValidateSelection(ctx, actor, selection); err != nil {
		return evidence.AuthorizationScope{}, err
	}
	resolved, err := resolveEvidenceSource(ctx, adapter.resolver, actor, selection)
	if err != nil {
		return evidence.AuthorizationScope{}, err
	}
	return resolved.Authorization, nil
}

func (adapter *MonitoringEventAdapter) Summarize(snapshot evidence.CanonicalSnapshot) evidence.Summary {
	if err := snapshot.Validate(adapter.descriptor); err != nil {
		return evidence.Summary{Key: adapter.descriptor.Key, RendererVersion: adapter.descriptor.Conformance.RendererVersion}
	}
	payload, err := decodeEvidencePayload(snapshot.Bytes())
	if err != nil {
		return evidence.Summary{Key: adapter.descriptor.Key, RendererVersion: adapter.descriptor.Conformance.RendererVersion}
	}
	events := allowlistedObjects(payload["events"], []string{"event_id", "object_type", "object_id", "event_type", "severity", "summary", "event_at", "recorded_at", "backfilled", "provenance", "producer_version", "rule_version", "prior_state", "resulting_state", "correction_of_event_id", "metrics"})
	envelope := snapshot.Envelope()
	return evidence.Summary{
		Key: adapter.descriptor.Key, RendererVersion: adapter.descriptor.Conformance.RendererVersion,
		Title: "Monitoring events", SearchText: "monitoring events " + envelope.Source.ID,
		ReadModel: map[string]any{"version": "monitoring_event_read_model/v2", "events": events, "quality_status": string(envelope.Quality.Status), "event_count": envelope.Quality.SampleCount, "backfilled_count": envelope.Quality.BackfilledCount},
	}
}

func (adapter *MonitoringEventAdapter) Compare(left, right evidence.CanonicalSnapshot, alignment evidence.Alignment) evidence.Comparison {
	compatible, reason := compatibleDiscreteSnapshots(adapter.descriptor, left, right, alignment)
	values := map[string]any{"version": "monitoring_event_comparison/v2"}
	if compatible {
		leftEnvelope, rightEnvelope := left.Envelope(), right.Envelope()
		values["event_count_left"] = leftEnvelope.Quality.SampleCount
		values["event_count_right"] = rightEnvelope.Quality.SampleCount
		values["event_count_delta"] = int64(rightEnvelope.Quality.SampleCount) - int64(leftEnvelope.Quality.SampleCount)
		values["backfilled_count_left"] = leftEnvelope.Quality.BackfilledCount
		values["backfilled_count_right"] = rightEnvelope.Quality.BackfilledCount
		values["backfilled_count_delta"] = int64(rightEnvelope.Quality.BackfilledCount) - int64(leftEnvelope.Quality.BackfilledCount)
		leftPayload, leftErr := decodeEvidencePayload(left.Bytes())
		rightPayload, rightErr := decodeEvidencePayload(right.Bytes())
		if leftErr != nil || rightErr != nil {
			compatible, reason = false, "invalid monitoring event payload"
		} else {
			leftCorrections := countNonEmptyField(leftPayload["events"], "correction_of_event_id")
			rightCorrections := countNonEmptyField(rightPayload["events"], "correction_of_event_id")
			values["correction_count_left"] = leftCorrections
			values["correction_count_right"] = rightCorrections
			values["correction_count_delta"] = int64(rightCorrections) - int64(leftCorrections)
		}
	}
	return evidence.Comparison{Key: adapter.descriptor.Key, Compatible: compatible, Reason: reason, Values: values}
}

func (adapter *MonitoringEventAdapter) Export(snapshot evidence.CanonicalSnapshot, mode evidence.ExportMode) evidence.ExportMaterial {
	return exportEvidenceSnapshot(adapter.descriptor, snapshot, mode)
}

func (adapter *MonitoringEventAdapter) evaluate(ctx context.Context, actor evidence.ActorScope, selection evidence.Selection) (discreteEvaluation, error) {
	resolved, err := resolveEvidenceSource(ctx, adapter.resolver, actor, selection)
	if err != nil {
		return discreteEvaluation{}, err
	}
	capture, err := adapter.source.LoadMonitoringEventEvidence(ctx, selection.SourceType, selection.SourceID, selection.RequestedWindow)
	if err != nil {
		return discreteEvaluation{}, err
	}
	if capture.EventCount == 0 || capture.EventCount != uint64(len(capture.Events)) || capture.EventCount > evidence.MaxSnapshotDataPoints {
		return discreteEvaluation{}, fmt.Errorf("%w: monitoring event source bound", evidence.ErrInvalidCanonicalPayload)
	}
	capture = cloneAndSortMonitoringEventCapture(capture)
	now := adapterNow(adapter.options)
	if err := validateMonitoringEventCapture(capture, selection, now); err != nil {
		return discreteEvaluation{}, err
	}
	payloadEvents := make([]any, 0, len(capture.Events))
	units := make(map[string]string)
	var backfilled, metricCount uint64
	for _, event := range capture.Events {
		metrics := make([]any, 0, len(event.Metrics))
		for _, metric := range event.Metrics {
			metrics = append(metrics, map[string]any{"metric": metric.Metric, "unit": metric.Unit, "value": metric.Value, "threshold": metric.Threshold})
			units[metric.Metric] = metric.Unit
			metricCount++
		}
		if event.Backfilled {
			backfilled++
		}
		payloadEvents = append(payloadEvents, map[string]any{
			"event_id": event.EventID, "object_type": event.ObjectType, "object_id": event.ObjectID,
			"event_type": event.EventType, "severity": event.Severity, "summary": event.Summary,
			"event_at": event.EventAt.UTC().Format(time.RFC3339Nano), "recorded_at": event.RecordedAt.UTC().Format(time.RFC3339Nano),
			"backfilled": event.Backfilled, "provenance": event.Provenance, "producer_version": event.ProducerVersion,
			"rule_version": event.RuleVersion, "prior_state": event.PriorState, "resulting_state": event.ResultingState,
			"correction_of_event_id": event.CorrectionOfEventID, "metrics": metrics,
		})
	}
	payload := map[string]any{"event_count": capture.EventCount, "events": payloadEvents}
	canonical, redaction, err := evidence.CanonicalizePayload(adapter.descriptor, payload, evidence.RedactionNormalOnly)
	if err != nil {
		return discreteEvaluation{}, err
	}
	redaction.Decisions = appendForbiddenPreviewDecisions(adapter.descriptor, redaction.Decisions)
	first, last := capture.Events[0], capture.Events[len(capture.Events)-1]
	actualEnd := last.EventAt.Add(time.Microsecond)
	if actualEnd.After(selection.RequestedWindow.End) {
		actualEnd = selection.RequestedWindow.End
	}
	unitSemantics := evidence.UnitsSemantics{Status: evidence.UnitsNotApplicable, Reason: "event metadata without metric context"}
	if len(units) > 0 {
		unitSemantics = evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: units}
	}
	return discreteEvaluation{
		resolved: resolved, actualWindow: evidence.TimeWindow{Start: first.EventAt, End: actualEnd}, observedAt: last.EventAt,
		sourceRevision: capture.Events[len(capture.Events)-1].EventID, sourceWatermark: capture.SourceWatermark,
		producerVersion: capture.ProducerVersion, calculationVersion: monitoringEventCalculationVersion,
		units: unitSemantics, quality: evidence.Quality{Status: evidence.QualityComplete, SampleCount: capture.EventCount, BackfilledCount: backfilled, DataPointCount: capture.EventCount + metricCount},
		payload: payload, canonical: canonical, redaction: redaction.Decisions,
	}, nil
}

func cloneAndSortMonitoringEventCapture(capture MonitoringEventCapture) MonitoringEventCapture {
	capture.Events = append([]MonitoringEventFact(nil), capture.Events...)
	for index := range capture.Events {
		capture.Events[index].Metrics = append([]MonitoringEventMetric(nil), capture.Events[index].Metrics...)
		sort.Slice(capture.Events[index].Metrics, func(left, right int) bool {
			return capture.Events[index].Metrics[left].Metric < capture.Events[index].Metrics[right].Metric
		})
	}
	sort.Slice(capture.Events, func(left, right int) bool {
		if !capture.Events[left].EventAt.Equal(capture.Events[right].EventAt) {
			return capture.Events[left].EventAt.Before(capture.Events[right].EventAt)
		}
		return capture.Events[left].EventID < capture.Events[right].EventID
	})
	return capture
}

func validateMonitoringEventCapture(capture MonitoringEventCapture, selection evidence.Selection, now time.Time) error {
	if capture.EventCount == 0 || capture.EventCount != uint64(len(capture.Events)) || capture.EventCount > evidence.MaxSnapshotDataPoints ||
		capture.ProducerVersion != incidents.MonitoringEventEvidenceSourceVersion || len(capture.Events) == 0 {
		return fmt.Errorf("%w: monitoring event source", evidence.ErrInvalidCanonicalPayload)
	}
	watermark, err := parseCanonicalPostgresTimestamp(capture.SourceWatermark)
	if err != nil || watermark.After(now) {
		return fmt.Errorf("%w: monitoring event watermark", evidence.ErrInvalidCanonicalPayload)
	}
	seenEvents := make(map[string]struct{}, len(capture.Events))
	metricUnits := make(map[string]string)
	var latestRecorded time.Time
	var pointCount uint64
	for _, event := range capture.Events {
		if !validSourceIdentifier(event.EventID) || event.ObjectType != selection.SourceType || event.ObjectID != selection.SourceID ||
			!safeActivityText(event.Summary, 2048) || !validEventState(event.PriorState) || !validEventState(event.ResultingState) ||
			!canonicalTask4Timestamp(event.EventAt) || !canonicalTask4Timestamp(event.RecordedAt) ||
			event.RecordedAt.Before(event.EventAt) || event.RecordedAt.After(now) || event.EventAt.Before(selection.RequestedWindow.Start) || !event.EventAt.Before(selection.RequestedWindow.End) ||
			len(event.Metrics) > 20 || (event.CorrectionOfEventID != "" && (!validSourceIdentifier(event.CorrectionOfEventID) || event.CorrectionOfEventID == event.EventID)) {
			return fmt.Errorf("%w: monitoring event fact", evidence.ErrInvalidCanonicalPayload)
		}
		if !incidents.ValidMonitoringEventMetadata(incidents.ObjectType(event.ObjectType), incidents.EventType(event.EventType), incidents.Severity(event.Severity), event.Backfilled, event.Provenance, event.ProducerVersion, event.RuleVersion, event.PriorState, event.ResultingState, event.CorrectionOfEventID) {
			return fmt.Errorf("%w: monitoring event fact metadata", evidence.ErrInvalidCanonicalPayload)
		}
		if _, duplicate := seenEvents[event.EventID]; duplicate {
			return fmt.Errorf("%w: duplicate monitoring event", evidence.ErrInvalidCanonicalPayload)
		}
		seenEvents[event.EventID] = struct{}{}
		seenMetrics := make(map[string]struct{}, len(event.Metrics))
		for _, metric := range event.Metrics {
			if !validFieldAtom(metric.Metric) || !validUnit(metric.Unit) || math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) || math.IsNaN(metric.Threshold) || math.IsInf(metric.Threshold, 0) {
				return fmt.Errorf("%w: monitoring event metric", evidence.ErrInvalidCanonicalPayload)
			}
			if _, duplicate := seenMetrics[metric.Metric]; duplicate {
				return fmt.Errorf("%w: duplicate monitoring event metric", evidence.ErrInvalidCanonicalPayload)
			}
			if unit, exists := metricUnits[metric.Metric]; exists && unit != metric.Unit {
				return fmt.Errorf("%w: monitoring event metric unit drift", evidence.ErrInvalidCanonicalPayload)
			}
			seenMetrics[metric.Metric] = struct{}{}
			metricUnits[metric.Metric] = metric.Unit
			pointCount++
		}
		pointCount++
		if event.RecordedAt.After(latestRecorded) {
			latestRecorded = event.RecordedAt
		}
	}
	if pointCount > evidence.MaxSnapshotDataPoints || watermark.Before(latestRecorded) {
		return fmt.Errorf("%w: monitoring event coverage", evidence.ErrInvalidCanonicalPayload)
	}
	return nil
}

func monitoringEventDescriptor() evidence.Descriptor {
	normal := []string{"event_count", "events.event_id", "events.object_type", "events.object_id", "events.event_type", "events.severity", "events.summary", "events.event_at", "events.recorded_at", "events.backfilled", "events.provenance", "events.producer_version", "events.rule_version", "events.prior_state", "events.resulting_state", "events.correction_of_event_id", "events.metrics.metric", "events.metrics.unit", "events.metrics.value", "events.metrics.threshold"}
	forbidden := []string{"raw_json", "payload", "details", "stdout", "stderr", "token", "secret", "url"}
	return normalOnlyDescriptor(evidence.MonitoringEventV2Key(), "monitoring_event_v2", normal, forbidden)
}

type discreteEvaluation struct {
	resolved           ResolvedEvidenceSource
	actualWindow       evidence.TimeWindow
	observedAt         time.Time
	sourceRevision     string
	sourceWatermark    string
	producerVersion    string
	calculationVersion string
	units              evidence.UnitsSemantics
	quality            evidence.Quality
	payload            map[string]any
	canonical          evidence.CanonicalPayload
	redaction          []evidence.FieldDecision
}

func previewDiscreteEvidence(options AdapterOptions, descriptor evidence.Descriptor, selection evidence.Selection, evaluated discreteEvaluation) (evidence.Preview, error) {
	intentID, err := newAdapterIntentID(options)
	if err != nil {
		return evidence.Preview{}, err
	}
	now := adapterNow(options)
	return evidence.Preview{IntentID: intentID, Key: descriptor.Key, Selection: selection, Subject: evaluated.resolved.Subject, Source: evaluated.resolved.Source, RequestedWindow: selection.RequestedWindow, ActualWindow: evaluated.actualWindow, ObservedAt: evaluated.observedAt, SourceRevision: evaluated.sourceRevision, SourceWatermark: evaluated.sourceWatermark, ProducerVersion: evaluated.producerVersion, CalculationVersion: evaluated.calculationVersion, Units: evaluated.units, Quality: evaluated.quality, Sensitivity: evidence.SensitivityNormal, ActualPrecision: evidence.DurationSemantics{Applicable: false, Reason: "discrete authoritative facts"}, BucketWidth: evidence.DurationSemantics{Applicable: false, Reason: "discrete authoritative facts"}, QuotaOutcome: evidence.QuotaOutcome{Status: evidence.QuotaAllowed}, Retention: immutableEvidenceRetention(), Redaction: evaluated.redaction, EstimatedCanonicalBytes: evaluated.canonical.Size(), SourceDigest: evaluated.canonical.Hash(), RendererVersion: descriptor.Conformance.RendererVersion, PreviewedAt: now, ValidUntil: now.Add(evidence.CaptureIntentTTL)}, nil
}

func captureDiscreteEvidence(options AdapterOptions, descriptor evidence.Descriptor, selection evidence.Selection, evaluated discreteEvaluation) (evidence.CanonicalSnapshot, error) {
	now := adapterNow(options)
	redaction, err := evidence.NormalizeCaptureRedaction(descriptor, evaluated.redaction)
	if err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	envelope := evidence.SnapshotEnvelope{Key: descriptor.Key, Subject: evaluated.resolved.Subject, Source: evaluated.resolved.Source, Authorization: evaluated.resolved.Authorization, RequestedWindow: selection.RequestedWindow, ActualWindow: evaluated.actualWindow, ObservedAt: evaluated.observedAt, CapturedAt: now, ReferencedAt: now, SourceRevision: evaluated.sourceRevision, SourceWatermark: evaluated.sourceWatermark, SourceDigest: evaluated.canonical.Hash(), ProducerVersion: evaluated.producerVersion, CalculationVersion: evaluated.calculationVersion, Units: evaluated.units, Quality: evaluated.quality, Sensitivity: evidence.SensitivityNormal, ActualPrecision: evidence.DurationSemantics{Applicable: false, Reason: "discrete authoritative facts"}, BucketWidth: evidence.DurationSemantics{Applicable: false, Reason: "discrete authoritative facts"}, QuotaOutcome: evidence.QuotaOutcome{Status: evidence.QuotaAllowed}, Retention: immutableEvidenceRetention(), Redaction: redaction}
	snapshot, _, err := evidence.NewCanonicalSnapshot(descriptor, envelope, evaluated.payload, evidence.RedactionNormalOnly)
	return snapshot, err
}

func validateDiscreteIntent(key evidence.KindKey, intent evidence.Intent) error {
	if intent.Key != key || !evidence.ValidCaptureIntentID(intent.ID) || intent.PreviewDigest == [32]byte{} || intent.ValidUntil.IsZero() {
		return fmt.Errorf("%w: discrete evidence intent", evidence.ErrInvalidCanonicalPayload)
	}
	return nil
}

func compatibleDiscreteSnapshots(descriptor evidence.Descriptor, left, right evidence.CanonicalSnapshot, alignment evidence.Alignment) (bool, string) {
	if alignment.Mode != evidence.AlignmentExact || left.Validate(descriptor) != nil || right.Validate(descriptor) != nil {
		return false, "invalid or non-exact evidence alignment"
	}
	leftEnvelope, rightEnvelope := left.Envelope(), right.Envelope()
	if leftEnvelope.Key != descriptor.Key || rightEnvelope.Key != descriptor.Key || leftEnvelope.CalculationVersion != rightEnvelope.CalculationVersion || !reflect.DeepEqual(leftEnvelope.Units, rightEnvelope.Units) || leftEnvelope.RequestedWindow.End.Sub(leftEnvelope.RequestedWindow.Start) != rightEnvelope.RequestedWindow.End.Sub(rightEnvelope.RequestedWindow.Start) {
		return false, "incompatible evidence semantics"
	}
	return true, "exact compatible evidence semantics"
}

func normalOnlyDescriptor(key evidence.KindKey, renderer string, normal, forbidden []string) evidence.Descriptor {
	fields := make([]evidence.FieldDefinition, 0, len(normal)+len(forbidden))
	for _, path := range normal {
		fields = append(fields, evidence.FieldDefinition{Path: path, Sensitivity: evidence.SensitivityNormal})
	}
	for _, path := range forbidden {
		fields = append(fields, evidence.FieldDefinition{Path: path, Sensitivity: evidence.SensitivityForbidden})
	}
	return evidence.Descriptor{Key: key, Fields: fields, Conformance: evidence.ConformanceMetadata{CanonicalizationVersion: evidence.CanonicalizationVersionV1, ForbiddenCorpusVersion: evidence.ForbiddenCorpusVersionV1, RendererVersion: renderer, MaxCanonicalBytes: evidence.MaxCanonicalPayloadBytes}}
}

func validSourceIdentifier(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' && character != '.' && character != '/' {
			return false
		}
	}
	return true
}

func validVersionString(value string) bool { return validSourceIdentifier(value) }

func validEventState(value string) bool {
	return value != "" && safeActivityText(value, 128)
}
func validFieldAtom(value string) bool {
	return validSourceIdentifier(value) && !strings.Contains(value, "/")
}
func validUnit(value string) bool { return validFieldAtom(value) }

func parseCanonicalPostgresTimestamp(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC || parsed.Format(time.RFC3339Nano) != value || !postgresTimestampRepresentable(parsed) {
		return time.Time{}, evidence.ErrInvalidCanonicalPayload
	}
	return parsed, nil
}

func canonicalTask4Timestamp(value time.Time) bool {
	return postgresTimestampRepresentable(value) && value == value.UTC().Round(0)
}

func allowlistedObjects(value any, fields []string) []any {
	items, ok := value.([]any)
	if !ok {
		return []any{}
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		copy := make(map[string]any, len(fields))
		for _, field := range fields {
			if fieldValue, exists := object[field]; exists {
				copy[field] = fieldValue
			}
		}
		out = append(out, copy)
	}
	return out
}

func countNonEmptyField(value any, field string) uint64 {
	var count uint64
	for _, item := range allowlistedObjects(value, []string{field}) {
		object := item.(map[string]any)
		if text, ok := object[field].(string); ok && text != "" {
			count++
		}
	}
	return count
}

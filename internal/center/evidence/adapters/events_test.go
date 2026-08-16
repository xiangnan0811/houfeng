package adapters

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/recordauth"
)

var _ evidence.Kind = (*MonitoringEventAdapter)(nil)

func TestMonitoringEventAdapterFreezesCorrectionBackfillAndMetricContext(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	capture := validMonitoringEventCapture(window)
	adapter, err := NewMonitoringEventAdapter(
		staticMonitoringEventSource{capture: capture},
		monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"),
		AdapterOptions{Clock: func() time.Time { return now }, NewIntentID: func() (string, error) { return "evi_111111111111111111111111", nil }},
	)
	if err != nil {
		t.Fatalf("NewMonitoringEventAdapter() error = %v", err)
	}
	selection := evidence.Selection{Key: evidence.MonitoringEventV2Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: window}
	preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), selection)
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v", err)
	}
	if preview.Quality.SampleCount != 2 || preview.Quality.BackfilledCount != 1 || preview.Quality.DataPointCount != 4 {
		t.Fatalf("preview quality = %#v, want 2 events/1 backfill/4 points", preview.Quality)
	}
	snapshot, err := adapter.Capture(context.Background(), monitoringTestActor(t), evidence.Intent{ID: preview.IntentID, Key: selection.Key, Selection: selection, PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	for _, required := range []string{`"backfilled":true`, `"correction_of_event_id":"evt_0123456789abcdef"`, `"prior_state":"normal"`, `"resulting_state":"alert"`, `"rule_version":"incident-rules/v1"`, `"metric":"cpu_usage_pct"`} {
		if !containsBytes(snapshot.Bytes(), required) {
			t.Fatalf("snapshot = %s, want %s", snapshot.Bytes(), required)
		}
	}
	summary := adapter.Summarize(snapshot)
	if summary.ReadModel["version"] != "monitoring_event_read_model/v2" {
		t.Fatalf("summary read model = %#v, want versioned event model", summary.ReadModel)
	}
	comparison := adapter.Compare(snapshot, snapshot, evidence.Alignment{Mode: evidence.AlignmentExact})
	if !comparison.Compatible || comparison.Values["version"] != "monitoring_event_comparison/v2" {
		t.Fatalf("comparison = %#v, want compatible versioned event comparison", comparison)
	}
	if comparison.Values["correction_count_delta"] != int64(0) {
		t.Fatalf("comparison correction delta = %#v, want 0", comparison.Values["correction_count_delta"])
	}
	fixture := evidence.ConformanceFixture{Actor: monitoringTestActor(t), Selection: selection, Intent: evidence.Intent{ID: preview.IntentID, Key: selection.Key, Selection: selection, PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil}, Alignment: evidence.Alignment{Mode: evidence.AlignmentExact}, ExportMode: evidence.ExportModeSafe}
	if err := evidence.VerifyKindConformance(context.Background(), adapter, fixture); err != nil {
		t.Fatalf("VerifyKindConformance() error = %v", err)
	}
}

func TestMonitoringEventAdapterRejectsMalformedCustomSourceFacts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	tests := []struct {
		name   string
		mutate func(*MonitoringEventCapture)
	}{
		{name: "count drift", mutate: func(c *MonitoringEventCapture) { c.EventCount++ }},
		{name: "duplicate event", mutate: func(c *MonitoringEventCapture) { c.Events[1].EventID = c.Events[0].EventID }},
		{name: "source mismatch", mutate: func(c *MonitoringEventCapture) { c.Events[0].ObjectID = "mi_other" }},
		{name: "unknown capture producer", mutate: func(c *MonitoringEventCapture) { c.ProducerVersion = "future-store/v9" }},
		{name: "unknown event", mutate: func(c *MonitoringEventCapture) { c.Events[0].EventType = "arbitrary" }},
		{name: "unknown provenance", mutate: func(c *MonitoringEventCapture) { c.Events[0].Provenance = "mystery" }},
		{name: "unknown event producer", mutate: func(c *MonitoringEventCapture) { c.Events[0].ProducerVersion = "future-producer/v9" }},
		{name: "unknown event rule", mutate: func(c *MonitoringEventCapture) { c.Events[0].RuleVersion = "future-rules/v9" }},
		{name: "unknown state", mutate: func(c *MonitoringEventCapture) { c.Events[0].PriorState = "future-state" }},
		{name: "invented incident transition", mutate: func(c *MonitoringEventCapture) { c.Events[0].PriorState = "alert" }},
		{name: "severity state mismatch", mutate: func(c *MonitoringEventCapture) { c.Events[0].Severity = "严重" }},
		{name: "invented control transition", mutate: func(c *MonitoringEventCapture) {
			c.Events[0].EventType = string(incidents.EventMonitoringInstanceBindingReset)
			c.Events[0].Severity = ""
			c.Events[0].RuleVersion = incidents.MonitoringEventBindingRuleVersion
			c.Events[0].PriorState = "未绑定"
			c.Events[0].ResultingState = "已绑定"
		}},
		{name: "object rule family mismatch", mutate: func(c *MonitoringEventCapture) {
			c.Events[0].EventType = string(incidents.EventTargetPaused)
			c.Events[0].RuleVersion = incidents.MonitoringEventTargetRuleVersion
			c.Events[0].PriorState = "启用"
			c.Events[0].ResultingState = "暂停"
		}},
		{name: "correction identity on ordinary event", mutate: func(c *MonitoringEventCapture) { c.Events[1].EventType = string(incidents.EventIncidentEscalated) }},
		{name: "manual correction without identity", mutate: func(c *MonitoringEventCapture) { c.Events[1].CorrectionOfEventID = "" }},
		{name: "correction identity without manual provenance", mutate: func(c *MonitoringEventCapture) { c.Events[1].Provenance = incidents.MonitoringEventProvenanceCenter }},
		{name: "retention provenance without backfill", mutate: func(c *MonitoringEventCapture) {
			c.Events[0].Provenance = incidents.MonitoringEventProvenanceRetentionBackfill
		}},
		{name: "raw URL summary", mutate: func(c *MonitoringEventCapture) {
			c.Events[0].Summary = "https://user:pass@example.com/event?view=full"
		}},
		{name: "arbitrary JSON state", mutate: func(c *MonitoringEventCapture) {
			c.Events[0].PriorState = `{"state":"normal"}`
		}},
		{name: "oversized summary", mutate: func(c *MonitoringEventCapture) {
			c.Events[0].Summary = strings.Repeat("x", 2049)
		}},
		{name: "submicrosecond event time", mutate: func(c *MonitoringEventCapture) { c.Events[0].EventAt = c.Events[0].EventAt.Add(time.Nanosecond) }},
		{name: "non canonical UTC event time", mutate: func(c *MonitoringEventCapture) {
			c.Events[0].EventAt = c.Events[0].EventAt.In(time.FixedZone("offset", 8*60*60))
		}},
		{name: "recorded before event", mutate: func(c *MonitoringEventCapture) { c.Events[0].RecordedAt = c.Events[0].EventAt.Add(-time.Microsecond) }},
		{name: "invalid correction", mutate: func(c *MonitoringEventCapture) { c.Events[1].CorrectionOfEventID = c.Events[1].EventID }},
		{name: "duplicate metric", mutate: func(c *MonitoringEventCapture) {
			c.Events[0].Metrics = append(c.Events[0].Metrics, c.Events[0].Metrics[0])
		}},
		{name: "metric unit drift", mutate: func(c *MonitoringEventCapture) {
			c.Events[1].Metrics[0].Unit = "ratio"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := validMonitoringEventCapture(window)
			tt.mutate(&capture)
			adapter, err := NewMonitoringEventAdapter(staticMonitoringEventSource{capture: capture}, monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }})
			if err != nil {
				t.Fatalf("NewMonitoringEventAdapter() error = %v", err)
			}
			_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{Key: evidence.MonitoringEventV2Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: window})
			if !errors.Is(err, evidence.ErrInvalidCanonicalPayload) {
				t.Fatalf("PreviewCapture() error = %v, want ErrInvalidCanonicalPayload", err)
			}
		})
	}
}

func TestMonitoringEventAdapterCanonicalizesCustomSourceOrder(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	ordered := validMonitoringEventCapture(window)
	reversed := validMonitoringEventCapture(window)
	reversed.Events[0], reversed.Events[1] = reversed.Events[1], reversed.Events[0]
	selection := evidence.Selection{Key: evidence.MonitoringEventV2Key(), SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef", RequestedWindow: window}
	digests := make([][32]byte, 0, 2)
	for _, capture := range []MonitoringEventCapture{ordered, reversed} {
		adapter, err := NewMonitoringEventAdapter(staticMonitoringEventSource{capture: capture}, monitoringTestResolver(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"), AdapterOptions{Clock: func() time.Time { return now }})
		if err != nil {
			t.Fatalf("NewMonitoringEventAdapter() error = %v", err)
		}
		preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), selection)
		if err != nil {
			t.Fatalf("PreviewCapture() error = %v", err)
		}
		digests = append(digests, preview.SourceDigest)
	}
	if digests[0] != digests[1] {
		t.Fatalf("source digests differ by source order: %x != %x", digests[0], digests[1])
	}
}

type staticMonitoringEventSource struct{ capture MonitoringEventCapture }

func (source staticMonitoringEventSource) LoadMonitoringEventEvidence(context.Context, string, string, evidence.TimeWindow) (MonitoringEventCapture, error) {
	return source.capture, nil
}

func validMonitoringEventCapture(window evidence.TimeWindow) MonitoringEventCapture {
	first := window.Start.Add(10 * time.Minute)
	second := window.Start.Add(20 * time.Minute)
	return MonitoringEventCapture{
		EventCount: 2, ProducerVersion: "state-change-events/v2", SourceWatermark: second.Add(time.Minute).Format(time.RFC3339Nano),
		Events: []MonitoringEventFact{
			{EventID: "evt_0123456789abcdef", ObjectType: string(recordauth.SourceKindMonitoringInstance), ObjectID: "mi_0123456789abcdef", EventType: string(incidents.EventIncidentStarted), Severity: "告警", Summary: "CPU pressure", EventAt: first, RecordedAt: first.Add(time.Minute), Backfilled: false, Provenance: incidents.MonitoringEventProvenanceAgentSync, ProducerVersion: incidents.MonitoringEventProducerVersion, RuleVersion: incidents.MonitoringEventIncidentRuleVersion, PriorState: "normal", ResultingState: "alert", Metrics: []MonitoringEventMetric{{Metric: "cpu_usage_pct", Unit: "percent", Value: 92, Threshold: 90}}},
			{EventID: "evt_1123456789abcdef", ObjectType: string(recordauth.SourceKindMonitoringInstance), ObjectID: "mi_0123456789abcdef", EventType: string(incidents.EventCorrected), Severity: "严重", Summary: "CPU correction", EventAt: second, RecordedAt: second.Add(time.Minute), Backfilled: true, Provenance: incidents.MonitoringEventProvenanceManualCorrection, ProducerVersion: incidents.MonitoringEventProducerVersion, RuleVersion: incidents.MonitoringEventIncidentRuleVersion, PriorState: "alert", ResultingState: "critical", CorrectionOfEventID: "evt_0123456789abcdef", Metrics: []MonitoringEventMetric{{Metric: "cpu_usage_pct", Unit: "percent", Value: 98, Threshold: 95}}},
		},
	}
}

package store

import (
	"encoding/json"
	"testing"
	"time"

	"houfeng/internal/center/incidents"
)

func TestMarshalTask4MonitoringEventPayloadPreservesLegacyAndExplicitCorrectionMetadata(t *testing.T) {
	t.Parallel()

	eventAt := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	recordedAt := eventAt.Add(2 * time.Hour)
	encoded, err := marshalTask4MonitoringEventPayload(task4MonitoringEventPayload{
		ObjectType:          incidents.ObjectTypeMonitoringInstance,
		EventType:           incidents.EventCorrected,
		Severity:            incidents.SeverityCritical,
		EventAt:             eventAt,
		RecordedAt:          recordedAt,
		IsBackfilled:        true,
		Provenance:          monitoringEventProvenanceManualCorrection,
		ProducerVersion:     monitoringEventProducerVersion,
		RuleVersion:         monitoringEventIncidentRuleVersion,
		PriorState:          "alert",
		ResultingState:      "critical",
		CorrectionOfEventID: "evt_0123456789abcdef",
		IncidentID:          "inc_monitoring_instance_cpu",
		IncidentClass:       "monitoring_instance_resource_pressure",
	})
	if err != nil {
		t.Fatalf("marshalTask4MonitoringEventPayload() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	for field, want := range map[string]any{
		"event_at":               eventAt.Format(time.RFC3339Nano),
		"is_backfilled":          true,
		"provenance":             string(monitoringEventProvenanceManualCorrection),
		"producer_version":       monitoringEventProducerVersion,
		"rule_version":           monitoringEventIncidentRuleVersion,
		"prior_state":            "alert",
		"resulting_state":        "critical",
		"correction_of_event_id": "evt_0123456789abcdef",
		"incident_id":            "inc_monitoring_instance_cpu",
		"incident_class":         "monitoring_instance_resource_pressure",
	} {
		if got := payload[field]; got != want {
			t.Fatalf("payload[%q] = %#v, want %#v in %s", field, got, want, encoded)
		}
	}
	if _, exists := payload["recorded_at"]; exists {
		t.Fatalf("payload unexpectedly duplicates recorded_at instead of using state_change_events.created_at: %s", encoded)
	}
}

func TestMarshalTask4MonitoringEventPayloadFailsClosed(t *testing.T) {
	t.Parallel()

	eventAt := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	valid := task4MonitoringEventPayload{
		ObjectType:      incidents.ObjectTypeMonitoringInstance,
		EventType:       incidents.EventIncidentStarted,
		Severity:        incidents.SeverityAlert,
		EventAt:         eventAt,
		RecordedAt:      eventAt.Add(time.Minute),
		Provenance:      monitoringEventProvenanceCenter,
		ProducerVersion: monitoringEventProducerVersion,
		RuleVersion:     monitoringEventIncidentRuleVersion,
		PriorState:      "normal",
		ResultingState:  "alert",
		IncidentID:      "inc_monitoring_instance_cpu",
		IncidentClass:   "monitoring_instance_resource_pressure",
	}
	tests := []struct {
		name   string
		mutate func(*task4MonitoringEventPayload)
	}{
		{name: "missing event time", mutate: func(payload *task4MonitoringEventPayload) { payload.EventAt = time.Time{} }},
		{name: "recorded before event", mutate: func(payload *task4MonitoringEventPayload) {
			payload.RecordedAt = payload.EventAt.Add(-time.Microsecond)
		}},
		{name: "submicrosecond event", mutate: func(payload *task4MonitoringEventPayload) { payload.EventAt = payload.EventAt.Add(time.Nanosecond) }},
		{name: "unknown provenance", mutate: func(payload *task4MonitoringEventPayload) { payload.Provenance = "mystery" }},
		{name: "unknown producer", mutate: func(payload *task4MonitoringEventPayload) { payload.ProducerVersion = "future/v9" }},
		{name: "unknown rule", mutate: func(payload *task4MonitoringEventPayload) { payload.RuleVersion = "future-rules/v9" }},
		{name: "unknown prior state", mutate: func(payload *task4MonitoringEventPayload) { payload.PriorState = "future-state" }},
		{name: "unknown resulting state", mutate: func(payload *task4MonitoringEventPayload) { payload.ResultingState = "future-state" }},
		{name: "invented incident transition", mutate: func(payload *task4MonitoringEventPayload) { payload.PriorState = "alert" }},
		{name: "severity state mismatch", mutate: func(payload *task4MonitoringEventPayload) { payload.Severity = incidents.SeverityCritical }},
		{name: "missing prior state", mutate: func(payload *task4MonitoringEventPayload) { payload.PriorState = "" }},
		{name: "missing resulting state", mutate: func(payload *task4MonitoringEventPayload) { payload.ResultingState = "" }},
		{name: "rule family mismatch", mutate: func(payload *task4MonitoringEventPayload) { payload.RuleVersion = monitoringEventTargetRuleVersion }},
		{name: "object rule family mismatch", mutate: func(payload *task4MonitoringEventPayload) {
			payload.IncidentID, payload.IncidentClass = "", ""
			payload.EventType = incidents.EventTargetPaused
			payload.RuleVersion = monitoringEventTargetRuleVersion
			payload.PriorState = "启用"
			payload.ResultingState = "暂停"
			payload.RunStatus = "暂停"
		}},
		{name: "legacy resulting state mismatch", mutate: func(payload *task4MonitoringEventPayload) {
			payload.IncidentID, payload.IncidentClass = "", ""
			payload.RuleVersion = monitoringEventBindingRuleVersion
			payload.PriorState = "已绑定"
			payload.ResultingState = "未绑定"
			payload.BindingStatus = "已绑定"
		}},
		{name: "retention provenance without backfill", mutate: func(payload *task4MonitoringEventPayload) {
			payload.Provenance = monitoringEventProvenanceRetentionBackfill
		}},
		{name: "correction identity without manual correction", mutate: func(payload *task4MonitoringEventPayload) { payload.CorrectionOfEventID = "evt_0123456789abcdef" }},
		{name: "manual correction without identity", mutate: func(payload *task4MonitoringEventPayload) {
			payload.Provenance = monitoringEventProvenanceManualCorrection
		}},
		{name: "missing legacy contract", mutate: func(payload *task4MonitoringEventPayload) { payload.IncidentID, payload.IncidentClass = "", "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := valid
			tt.mutate(&payload)
			if _, err := marshalTask4MonitoringEventPayload(payload); err == nil {
				t.Fatal("marshalTask4MonitoringEventPayload() error = nil, want fail-closed rejection")
			}
		})
	}
}

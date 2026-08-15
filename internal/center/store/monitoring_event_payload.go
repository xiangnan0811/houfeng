package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"houfeng/internal/center/incidents"
)

type monitoringEventProvenance string

const (
	monitoringEventProvenanceAgentSync         monitoringEventProvenance = incidents.MonitoringEventProvenanceAgentSync
	monitoringEventProvenanceCenter            monitoringEventProvenance = incidents.MonitoringEventProvenanceCenter
	monitoringEventProvenanceWeb               monitoringEventProvenance = incidents.MonitoringEventProvenanceWeb
	monitoringEventProvenanceRetentionBackfill monitoringEventProvenance = incidents.MonitoringEventProvenanceRetentionBackfill
	monitoringEventProvenanceManualCorrection  monitoringEventProvenance = incidents.MonitoringEventProvenanceManualCorrection

	monitoringEventProducerVersion      = incidents.MonitoringEventProducerVersion
	monitoringEventIncidentRuleVersion  = incidents.MonitoringEventIncidentRuleVersion
	monitoringEventBindingRuleVersion   = incidents.MonitoringEventBindingRuleVersion
	monitoringEventLifecycleRuleVersion = incidents.MonitoringEventLifecycleRuleVersion
	monitoringEventRuntimeRuleVersion   = incidents.MonitoringEventRuntimeRuleVersion
	monitoringEventTargetRuleVersion    = incidents.MonitoringEventTargetRuleVersion
)

// task4MonitoringEventPayload is the single persistence contract shared by
// every state_change_events producer that can feed monitoring.event/v2.
// RecordedAt is persisted in state_change_events.created_at rather than
// duplicated in JSON, but remains part of the typed input so chronology is
// validated before any write.
type task4MonitoringEventPayload struct {
	ObjectType          incidents.ObjectType      `json:"-"`
	EventType           incidents.EventType       `json:"-"`
	Severity            incidents.Severity        `json:"-"`
	EventAt             time.Time                 `json:"event_at"`
	RecordedAt          time.Time                 `json:"-"`
	IsBackfilled        bool                      `json:"is_backfilled"`
	Provenance          monitoringEventProvenance `json:"provenance"`
	ProducerVersion     string                    `json:"producer_version"`
	RuleVersion         string                    `json:"rule_version"`
	PriorState          string                    `json:"prior_state"`
	ResultingState      string                    `json:"resulting_state"`
	CorrectionOfEventID string                    `json:"correction_of_event_id,omitempty"`

	IncidentID       string `json:"incident_id,omitempty"`
	IncidentClass    string `json:"incident_class,omitempty"`
	BindingStatus    string `json:"binding_status,omitempty"`
	LifecycleStatus  string `json:"lifecycle_status,omitempty"`
	MonitoringStatus string `json:"monitoring_status,omitempty"`
	RunStatus        string `json:"run_status,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

func marshalTask4MonitoringEventPayload(payload task4MonitoringEventPayload) ([]byte, error) {
	if !validMonitoringEventTimestamp(payload.EventAt) || !validMonitoringEventTimestamp(payload.RecordedAt) || payload.RecordedAt.Before(payload.EventAt) {
		return nil, fmt.Errorf("invalid monitoring event chronology")
	}
	if payload.CorrectionOfEventID != "" && !validMonitoringEventIdentifier(payload.CorrectionOfEventID) {
		return nil, fmt.Errorf("invalid monitoring event correction identity")
	}
	if !incidents.ValidMonitoringEventMetadata(
		payload.ObjectType,
		payload.EventType,
		payload.Severity,
		payload.IsBackfilled,
		string(payload.Provenance),
		payload.ProducerVersion,
		payload.RuleVersion,
		payload.PriorState,
		payload.ResultingState,
		payload.CorrectionOfEventID,
	) {
		return nil, fmt.Errorf("invalid monitoring event metadata")
	}
	if !validMonitoringEventLegacyFields(payload) {
		return nil, fmt.Errorf("invalid monitoring event legacy payload")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal monitoring event payload: %w", err)
	}
	return encoded, nil
}

func validMonitoringEventTimestamp(value time.Time) bool {
	return !value.IsZero() && value == value.UTC().Round(0) && value.Nanosecond()%1_000 == 0
}

func canonicalTask4MonitoringEventTimestamp(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func validMonitoringEventState(value string) bool {
	return validMonitoringEventText(value, 128)
}

func validMonitoringEventLegacyFields(payload task4MonitoringEventPayload) bool {
	families := 0
	if payload.IncidentID != "" || payload.IncidentClass != "" {
		if payload.RuleVersion != monitoringEventIncidentRuleVersion || !validMonitoringEventIdentifier(payload.IncidentID) || !validMonitoringEventIdentifier(payload.IncidentClass) {
			return false
		}
		families++
	}
	for _, field := range []struct {
		value string
		rule  string
	}{
		{value: payload.BindingStatus, rule: monitoringEventBindingRuleVersion},
		{value: payload.LifecycleStatus, rule: monitoringEventLifecycleRuleVersion},
		{value: payload.MonitoringStatus, rule: monitoringEventRuntimeRuleVersion},
		{value: payload.RunStatus, rule: monitoringEventTargetRuleVersion},
	} {
		value := field.value
		if value == "" {
			continue
		}
		if payload.RuleVersion != field.rule || !validMonitoringEventState(value) {
			return false
		}
		families++
	}
	if families != 1 || (payload.Reason != "" && !validMonitoringEventText(payload.Reason, 2048)) {
		return false
	}
	switch payload.RuleVersion {
	case monitoringEventBindingRuleVersion:
		return payload.BindingStatus == payload.ResultingState
	case monitoringEventLifecycleRuleVersion:
		return payload.LifecycleStatus == payload.ResultingState || (payload.EventType == incidents.EventMonitoringInstanceLifecycleUpdated && payload.PriorState == "unarchived" && payload.ResultingState == "archived")
	case monitoringEventRuntimeRuleVersion:
		return payload.MonitoringStatus == payload.ResultingState
	case monitoringEventTargetRuleVersion:
		return payload.RunStatus == payload.ResultingState
	default:
		return true
	}
}

func validMonitoringEventText(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func validMonitoringEventIdentifier(value string) bool {
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

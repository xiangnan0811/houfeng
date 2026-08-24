package vpsoverview

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOverviewMarshalJSONKeepsEmptyCollectionsAsArrays(t *testing.T) {
	payload, err := json.Marshal(Overview{
		GeneratedAt: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		Identity:    Identity{VPSID: "vps_7c2a4e18b09d5f31"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"anomalies", "facts", "relations", "capabilities"} {
		raw := string(decoded[key])
		if raw != "[]" {
			t.Fatalf("%s = %s, want []", key, raw)
		}
	}
}

func TestRelationSummaryMarshalJSONRequiresSectionState(t *testing.T) {
	payload, err := json.Marshal(RelationSummary{
		Kind: "services", Count: 0, Status: "unavailable", Label: "服务",
		Section: SectionState{State: SectionUnavailable, ReasonCode: "relation_unavailable"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	section, ok := decoded["section"]
	if !ok {
		t.Fatalf("required section missing from %s", payload)
	}
	var state SectionState
	if err := json.Unmarshal(section, &state); err != nil {
		t.Fatalf("section: %v", err)
	}
	if state.State != SectionUnavailable || state.ReasonCode != "relation_unavailable" {
		t.Fatalf("section = %#v", state)
	}
}

func TestRelationSummaryMarshalJSONOmitsEmptyRoute(t *testing.T) {
	payload, err := json.Marshal(RelationSummary{
		Kind: "services", Count: 0, Label: "服务",
		Section: SectionState{State: SectionReady},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, exists := decoded["route"]; exists {
		t.Fatalf("command-owned relation must omit route: %s", payload)
	}
	section, exists := decoded["section"]
	if !exists {
		t.Fatalf("required section missing from %s", payload)
	}
	var state SectionState
	if err := json.Unmarshal(section, &state); err != nil {
		t.Fatalf("section: %v", err)
	}
	if state.State != SectionReady || state.ReasonCode != "" {
		t.Fatalf("section = %#v, want ready with empty reason", state)
	}
}

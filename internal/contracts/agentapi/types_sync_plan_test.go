package agentapi_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func TestSyncResponseRoundTripWithPlan(t *testing.T) {
	acceptedAt, err := time.Parse(time.RFC3339, "2026-04-24T11:00:00Z")
	if err != nil {
		t.Fatalf("parse acceptedAt: %v", err)
	}

	t.Run("empty plan", func(t *testing.T) {
		original := agentapi.SyncResponse{
			AcceptedAt: acceptedAt,
			Status:     "accepted",
			Plan: &agentapi.SyncPlan{
				HostSampleFrequencyTier: agentapi.FrequencyTier1m,
			},
		}

		payload, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal sync response: %v", err)
		}

		var roundTrip agentapi.SyncResponse
		if err := json.Unmarshal(payload, &roundTrip); err != nil {
			t.Fatalf("unmarshal sync response: %v", err)
		}

		if roundTrip.AcceptedAt != acceptedAt {
			t.Fatalf("AcceptedAt = %v, want %v", roundTrip.AcceptedAt, acceptedAt)
		}
		if roundTrip.Status != "accepted" {
			t.Fatalf("Status = %q, want %q", roundTrip.Status, "accepted")
		}
		if roundTrip.Plan == nil {
			t.Fatal("Plan = nil, want non-nil")
		}
		if roundTrip.Plan.HostSampleFrequencyTier != agentapi.FrequencyTier1m {
			t.Fatalf("HostSampleFrequencyTier = %q, want %q", roundTrip.Plan.HostSampleFrequencyTier, agentapi.FrequencyTier1m)
		}
		if len(roundTrip.Plan.ProbeAssignments) != 0 {
			t.Fatalf("len(ProbeAssignments) = %d, want 0", len(roundTrip.Plan.ProbeAssignments))
		}
	})

	t.Run("mixed probe assignments", func(t *testing.T) {
		original := agentapi.SyncResponse{
			AcceptedAt: acceptedAt,
			Status:     "accepted",
			Plan: &agentapi.SyncPlan{
				HostSampleFrequencyTier: agentapi.FrequencyTier5m,
				ProbeAssignments: []agentapi.ProbeAssignment{
					{
						TargetID:           "target-http",
						TargetHost:         "api.example.test",
						TargetBasePort:     intPtr(443),
						MaintenanceContext: false,
						ProbeItemID:        "probe-http",
						ProbeKind:          agentapi.ProbeKindHTTP,
						FrequencyTier:      agentapi.FrequencyTier1m,
						TimeoutSeconds:     5,
						Config:             json.RawMessage(`{"path":"/healthz","method":"GET"}`),
					},
					{
						TargetID:           "target-tcp",
						TargetHost:         "db.internal.test",
						TargetBasePort:     intPtr(3306),
						MaintenanceContext: true,
						ProbeItemID:        "probe-tcp",
						ProbeKind:          agentapi.ProbeKindTCP,
						FrequencyTier:      agentapi.FrequencyTier15m,
						TimeoutSeconds:     3,
						Config:             json.RawMessage(`{"connect_mode":"plain"}`),
					},
					{
						TargetID:           "target-tls",
						TargetHost:         "tls.example.test",
						TargetBasePort:     intPtr(8443),
						MaintenanceContext: false,
						ProbeItemID:        "probe-tls",
						ProbeKind:          agentapi.ProbeKindTLS,
						FrequencyTier:      agentapi.FrequencyTier6h,
						TimeoutSeconds:     10,
						Config:             json.RawMessage(`{"server_name":"tls.example.test"}`),
					},
				},
			},
		}

		payload, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal sync response: %v", err)
		}

		var roundTrip agentapi.SyncResponse
		if err := json.Unmarshal(payload, &roundTrip); err != nil {
			t.Fatalf("unmarshal sync response: %v", err)
		}

		if roundTrip.Plan == nil {
			t.Fatal("Plan = nil, want non-nil")
		}
		if roundTrip.Plan.HostSampleFrequencyTier != agentapi.FrequencyTier5m {
			t.Fatalf("HostSampleFrequencyTier = %q, want %q", roundTrip.Plan.HostSampleFrequencyTier, agentapi.FrequencyTier5m)
		}
		if len(roundTrip.Plan.ProbeAssignments) != 3 {
			t.Fatalf("len(ProbeAssignments) = %d, want 3", len(roundTrip.Plan.ProbeAssignments))
		}

		probeKinds := []string{
			roundTrip.Plan.ProbeAssignments[0].ProbeKind,
			roundTrip.Plan.ProbeAssignments[1].ProbeKind,
			roundTrip.Plan.ProbeAssignments[2].ProbeKind,
		}
		wantProbeKinds := []string{agentapi.ProbeKindHTTP, agentapi.ProbeKindTCP, agentapi.ProbeKindTLS}
		for i := range wantProbeKinds {
			if probeKinds[i] != wantProbeKinds[i] {
				t.Fatalf("ProbeAssignments[%d].ProbeKind = %q, want %q", i, probeKinds[i], wantProbeKinds[i])
			}
		}

		if !bytes.Equal(roundTrip.Plan.ProbeAssignments[0].Config, json.RawMessage(`{"path":"/healthz","method":"GET"}`)) {
			t.Fatalf("HTTP config = %s, want %s", roundTrip.Plan.ProbeAssignments[0].Config, `{"path":"/healthz","method":"GET"}`)
		}
		if !bytes.Equal(roundTrip.Plan.ProbeAssignments[1].Config, json.RawMessage(`{"connect_mode":"plain"}`)) {
			t.Fatalf("TCP config = %s, want %s", roundTrip.Plan.ProbeAssignments[1].Config, `{"connect_mode":"plain"}`)
		}
		if !bytes.Equal(roundTrip.Plan.ProbeAssignments[2].Config, json.RawMessage(`{"server_name":"tls.example.test"}`)) {
			t.Fatalf("TLS config = %s, want %s", roundTrip.Plan.ProbeAssignments[2].Config, `{"server_name":"tls.example.test"}`)
		}
		if roundTrip.Plan.ProbeAssignments[1].MaintenanceContext != true {
			t.Fatalf("MaintenanceContext = %v, want true", roundTrip.Plan.ProbeAssignments[1].MaintenanceContext)
		}
		if roundTrip.Plan.ProbeAssignments[0].TimeoutSeconds != 5 {
			t.Fatalf("TimeoutSeconds = %d, want 5", roundTrip.Plan.ProbeAssignments[0].TimeoutSeconds)
		}
		if roundTrip.Plan.ProbeAssignments[2].TargetHost != "tls.example.test" {
			t.Fatalf("TargetHost = %q, want %q", roundTrip.Plan.ProbeAssignments[2].TargetHost, "tls.example.test")
		}
	})
}

func TestSyncResponseOmitsPlanWhenUnset(t *testing.T) {
	response := agentapi.SyncResponse{
		AcceptedAt: time.Date(2026, time.April, 24, 11, 0, 0, 0, time.UTC),
		Status:     "accepted",
	}

	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal sync response: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal sync response payload: %v", err)
	}
	if _, exists := got["plan"]; exists {
		t.Fatalf("plan should be omitted when unset: %s", payload)
	}

	var roundTrip agentapi.SyncResponse
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("unmarshal sync response: %v", err)
	}
	if roundTrip.Plan != nil {
		t.Fatalf("Plan = %#v, want nil", roundTrip.Plan)
	}
}

func TestSyncPlan(t *testing.T) {
	plan := agentapi.SyncPlan{
		HostSampleFrequencyTier: agentapi.FrequencyTier15m,
		ProbeAssignments: []agentapi.ProbeAssignment{{
			TargetID:           "target-nil-port",
			TargetHost:         "cache.internal.test",
			TargetBasePort:     nil,
			MaintenanceContext: true,
			ProbeItemID:        "probe-tcp",
			ProbeKind:          agentapi.ProbeKindTCP,
			FrequencyTier:      agentapi.FrequencyTier5m,
			TimeoutSeconds:     4,
			Config:             json.RawMessage(`{"connect_mode":"plain"}`),
		}},
	}

	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal sync plan: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal sync plan payload: %v", err)
	}

	assignments, ok := got["probe_assignments"].([]any)
	if !ok || len(assignments) != 1 {
		t.Fatalf("probe_assignments = %#v, want single assignment", got["probe_assignments"])
	}

	assignment, ok := assignments[0].(map[string]any)
	if !ok {
		t.Fatalf("assignment type = %T, want map[string]any", assignments[0])
	}
	if basePort, exists := assignment["target_base_port"]; !exists || basePort != nil {
		t.Fatalf("target_base_port = %#v, exists=%v, want explicit null", basePort, exists)
	}

	var roundTrip agentapi.SyncPlan
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("unmarshal sync plan: %v", err)
	}

	if len(roundTrip.ProbeAssignments) != 1 {
		t.Fatalf("len(ProbeAssignments) = %d, want 1", len(roundTrip.ProbeAssignments))
	}
	if roundTrip.ProbeAssignments[0].TargetBasePort != nil {
		t.Fatalf("TargetBasePort = %v, want nil", *roundTrip.ProbeAssignments[0].TargetBasePort)
	}
	if roundTrip.ProbeAssignments[0].FrequencyTier != agentapi.FrequencyTier5m {
		t.Fatalf("FrequencyTier = %q, want %q", roundTrip.ProbeAssignments[0].FrequencyTier, agentapi.FrequencyTier5m)
	}
}

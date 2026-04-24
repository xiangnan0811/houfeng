package agentapi_test

import (
	"encoding/json"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func TestSyncRequestRoundTripWithObservations(t *testing.T) {
	observedAt, err := time.Parse(time.RFC3339, "2026-04-24T10:00:00Z")
	if err != nil {
		t.Fatalf("parse observedAt: %v", err)
	}

	latencyMS := 83
	httpStatus := 200
	tlsExpiryDays := 30

	original := agentapi.SyncRequest{
		NodeID:    "nd_001",
		SyncToken: "sync-token",
		Heartbeats: []agentapi.NodeHeartbeat{{
			ObservedAt:   observedAt,
			AgentVersion: "v1.2.3",
			Fingerprint:  "fp_001",
			SyncBatchID:  "batch_001",
		}},
		HostSamples: []agentapi.HostSamplePayload{{
			ObservedAt:           observedAt,
			AgentVersion:         "v1.2.3",
			Fingerprint:          "fp_001",
			SyncBatchID:          "batch_001",
			CPUUsagePct:          12.5,
			Load1:                0.22,
			Load5:                0.18,
			Load15:               0.10,
			MemUsedPct:           61.3,
			MemAvailableBytes:    1073741824,
			SwapUsedPct:          5.1,
			DiskUsedPct:          71.2,
			InodeUsedPct:         44.8,
			NetInBytesPerSec:     1200,
			NetOutBytesPerSec:    2400,
			CPUIowaitPct:         1.2,
			CPUStealPct:          0.4,
			DiskReadBytesPerSec:  800,
			DiskWriteBytesPerSec: 1600,
			DiskBusyPct:          9.7,
			UptimeSeconds:        7200,
			MaintenanceContext:   true,
		}},
		ProbeObservations: []agentapi.ProbeObservationPayload{{
			TargetID:      "tg_001",
			ProbeItemID:   "pi_001",
			ProbeKind:     "http",
			ObservedAt:    observedAt,
			AgentVersion:  "v1.2.3",
			Fingerprint:   "fp_001",
			SyncBatchID:   "batch_001",
			ResultKind:    agentapi.ProbeResultSuccess,
			LatencyMS:     &latencyMS,
			HTTPStatus:    &httpStatus,
			TLSExpiryDays: &tlsExpiryDays,
		}},
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal sync request: %v", err)
	}

	var roundTrip agentapi.SyncRequest
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("unmarshal sync request: %v", err)
	}

	if len(roundTrip.HostSamples) != 1 {
		t.Fatalf("len(HostSamples) = %d, want %d", len(roundTrip.HostSamples), 1)
	}

	if len(roundTrip.ProbeObservations) != 1 {
		t.Fatalf("len(ProbeObservations) = %d, want %d", len(roundTrip.ProbeObservations), 1)
	}

	if roundTrip.ProbeObservations[0].TargetID != "tg_001" {
		t.Fatalf("ProbeObservations[0].TargetID = %q, want %q", roundTrip.ProbeObservations[0].TargetID, "tg_001")
	}
}

func TestSyncRequestOmitsObservationAdjunctsWithoutHeartbeatCarrier(t *testing.T) {
	payload, err := json.Marshal(agentapi.SyncRequest{NodeID: "nd_001", SyncToken: "sync-token"})
	if err != nil {
		t.Fatalf("marshal sync request: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if _, ok := got["heartbeats"]; ok {
		t.Fatalf("payload unexpectedly included heartbeats: %s", payload)
	}
	if _, ok := got["host_samples"]; ok {
		t.Fatalf("payload unexpectedly included host_samples: %s", payload)
	}
	if _, ok := got["probe_observations"]; ok {
		t.Fatalf("payload unexpectedly included probe_observations: %s", payload)
	}
}

func TestProbeObservationPayloadRoundTripPreservesSuccessSemantics(t *testing.T) {
	observedAt, err := time.Parse(time.RFC3339, "2026-04-24T10:05:00Z")
	if err != nil {
		t.Fatalf("parse observedAt: %v", err)
	}

	latencyMS := 83
	httpStatus := 200

	original := agentapi.ProbeObservationPayload{
		TargetID:     "tg_001",
		ProbeItemID:  "pi_001",
		ProbeKind:    "http",
		ObservedAt:   observedAt,
		AgentVersion: "v1.2.3",
		Fingerprint:  "fp_001",
		SyncBatchID:  "batch_001",
		ResultKind:   agentapi.ProbeResultSuccess,
		LatencyMS:    &latencyMS,
		HTTPStatus:   &httpStatus,
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal probe observation: %v", err)
	}

	var roundTrip agentapi.ProbeObservationPayload
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("unmarshal probe observation: %v", err)
	}

	if roundTrip.ProbeKind != "http" {
		t.Fatalf("ProbeKind = %q, want %q", roundTrip.ProbeKind, "http")
	}
	if roundTrip.ResultKind != agentapi.ProbeResultSuccess {
		t.Fatalf("ResultKind = %q, want %q", roundTrip.ResultKind, agentapi.ProbeResultSuccess)
	}
}

func TestProbeObservationPayloadRoundTripPreservesFailureErrorCode(t *testing.T) {
	observedAt, err := time.Parse(time.RFC3339, "2026-04-24T10:10:00Z")
	if err != nil {
		t.Fatalf("parse observedAt: %v", err)
	}

	original := agentapi.ProbeObservationPayload{
		TargetID:     "tg_001",
		ProbeItemID:  "pi_001",
		ProbeKind:    "tcp",
		ObservedAt:   observedAt,
		AgentVersion: "v1.2.3",
		Fingerprint:  "fp_001",
		SyncBatchID:  "batch_001",
		ResultKind:   agentapi.ProbeResultFailure,
		ErrorCode:    agentapi.ProbeErrorTimeout,
		ErrorSummary: "timeout waiting for dial",
		IsBackfilled: true,
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal probe observation: %v", err)
	}

	var roundTrip agentapi.ProbeObservationPayload
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("unmarshal probe observation: %v", err)
	}

	if roundTrip.ResultKind != agentapi.ProbeResultFailure {
		t.Fatalf("ResultKind = %q, want %q", roundTrip.ResultKind, agentapi.ProbeResultFailure)
	}
	if roundTrip.ErrorCode != agentapi.ProbeErrorTimeout {
		t.Fatalf("ErrorCode = %q, want %q", roundTrip.ErrorCode, agentapi.ProbeErrorTimeout)
	}
}

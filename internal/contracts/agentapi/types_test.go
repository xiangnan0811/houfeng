package agentapi_test

import (
	"encoding/json"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func TestSyncRequestRoundTrip(t *testing.T) {
	observedAt, err := time.Parse(time.RFC3339, "2026-04-22T12:00:00Z")
	if err != nil {
		t.Fatalf("parse observedAt: %v", err)
	}

	original := agentapi.SyncRequest{
		NodeID: "nd-local-01",
		Heartbeats: []agentapi.NodeHeartbeat{{
			ObservedAt:   observedAt,
			AgentVersion: "dev",
			Fingerprint:  "fp-001",
			SyncBatchID:  "batch-001",
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

	if roundTrip.NodeID != "nd-local-01" {
		t.Fatalf("NodeID = %q, want %q", roundTrip.NodeID, "nd-local-01")
	}

	if len(roundTrip.Heartbeats) != 1 {
		t.Fatalf("len(Heartbeats) = %d, want %d", len(roundTrip.Heartbeats), 1)
	}

	if roundTrip.Heartbeats[0].Fingerprint != "fp-001" {
		t.Fatalf("Fingerprint = %q, want %q", roundTrip.Heartbeats[0].Fingerprint, "fp-001")
	}
}

func TestSyncRequestOmitsEmptyHeartbeats(t *testing.T) {
	payload, err := json.Marshal(agentapi.SyncRequest{NodeID: "nd-local-01"})
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
}

package nodes

import (
	"encoding/json"
	"testing"
)

func TestRecordJSONDoesNotExposePrivateBindingSecrets(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(Record{
		NodeID:              "nd_123",
		BindingStatus:       BindingBound,
		BindingFingerprint:  "fp-private",
		EnrollmentTokenHash: "enroll-private",
		SyncTokenHash:       "sync-private",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if _, ok := payload["binding_fingerprint"]; ok {
		t.Fatalf("binding_fingerprint leaked in payload: %s", body)
	}
	if _, ok := payload["enrollment_token_hash"]; ok {
		t.Fatalf("enrollment_token_hash leaked in payload: %s", body)
	}
	if _, ok := payload["sync_token_hash"]; ok {
		t.Fatalf("sync_token_hash leaked in payload: %s", body)
	}
	if payload["node_id"] != "nd_123" {
		t.Fatalf("node_id = %#v, want %q", payload["node_id"], "nd_123")
	}
}

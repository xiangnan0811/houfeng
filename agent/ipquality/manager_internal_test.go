package ipquality

import (
	"encoding/json"
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func TestWithCollectRequestIDHandlesAnyDiagnosticsShape(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		``:                         `{"collect_request_id":"ipqc_001"}`,
		`null`:                     `{"collect_request_id":"ipqc_001"}`,
		" null \n":                 `{"collect_request_id":"ipqc_001"}`,
		`{"source_version":"v2"}`:  `{"collect_request_id":"ipqc_001","source_version":"v2"}`,
		`[1,2]`:                    `{"collect_request_id":"ipqc_001","diagnostics":[1,2]}`,
		`"text"`:                   `{"collect_request_id":"ipqc_001","diagnostics":"text"}`,
		`{"collect_request_id":1}`: `{"collect_request_id":"ipqc_001"}`,
	} {
		got := withCollectRequestID(json.RawMessage(raw), "ipqc_001")
		var gotValue, wantValue any
		if err := json.Unmarshal(got, &gotValue); err != nil {
			t.Fatalf("withCollectRequestID(%q) = %s, want JSON: %v", raw, got, err)
		}
		_ = json.Unmarshal([]byte(want), &wantValue)
		if gotJSON, _ := json.Marshal(gotValue); string(gotJSON) != want {
			t.Fatalf("withCollectRequestID(%q) = %s, want %s", raw, gotJSON, want)
		}
	}
	if agentapi.IPQualityDiagnosticsCollectRequestIDKey != "collect_request_id" {
		t.Fatal("diagnostics key must stay stable for center compatibility")
	}
}

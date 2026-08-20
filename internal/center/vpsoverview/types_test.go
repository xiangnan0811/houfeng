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

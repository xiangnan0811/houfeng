package agentapi_test

import (
	"encoding/json"
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func TestHostSamplePayloadNetworkRatesValidityRoundTripPreservesFalse(t *testing.T) {
	valid := false
	payload, err := json.Marshal(agentapi.HostSamplePayload{NetworkRatesValid: &valid})
	if err != nil {
		t.Fatalf("marshal host sample payload: %v", err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(payload, &encoded); err != nil {
		t.Fatalf("decode host sample payload: %v", err)
	}
	if got, ok := encoded["network_rates_valid"].(bool); !ok || got {
		t.Fatalf("encoded network_rates_valid = %#v, want false", encoded["network_rates_valid"])
	}
	var decoded agentapi.HostSamplePayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal host sample payload: %v", err)
	}
	if decoded.NetworkRatesValid == nil || *decoded.NetworkRatesValid {
		t.Fatalf("decoded NetworkRatesValid = %v, want false", decoded.NetworkRatesValid)
	}
}
func TestHostSamplePayloadAcceptsLegacyMissingNetworkRatesValidity(t *testing.T) {
	var payload agentapi.HostSamplePayload
	if err := json.Unmarshal([]byte(`{"observed_at":"2026-04-24T10:00:00Z","net_in_bytes_per_sec":0,"net_out_bytes_per_sec":0}`), &payload); err != nil {
		t.Fatalf("unmarshal legacy host sample payload: %v", err)
	}
	if payload.NetworkRatesValid != nil {
		t.Fatalf("legacy NetworkRatesValid = %v, want nil", payload.NetworkRatesValid)
	}
}

package vpsassets

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestIndependentValidityAndProviderCheck(t *testing.T) {
	date := "2027-01-31"
	now := time.Now().UTC()
	for _, tc := range []struct {
		name, mode string
		expiry     *string
		check      string
		checked    *time.Time
		valid      bool
	}{
		{"unknown", "unknown", nil, "unchecked", nil, true},
		{"fixed independent of subscription", "fixed", &date, "enabled", &now, true},
		{"unlimited", "unlimited", nil, "unsupported", &now, true},
		{"fixed missing date", "fixed", nil, "unchecked", nil, false},
		{"unknown cannot hide date", "unknown", &date, "unchecked", nil, false},
		{"check needs observation time", "fixed", &date, "disabled", nil, false},
		{"unchecked cannot have time", "fixed", &date, "unchecked", &now, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateIndependentFacts(tc.mode, tc.expiry, tc.check, tc.checked)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t error=%v", tc.valid, err)
			}
		})
	}
	input := NormalizeCreateInput(CreateInput{DisplayName: "server", RenewalDecision: RenewalCancel, AutoRenewCheck: "enabled", AutoRenewCheckedAt: &now, ValidityMode: "fixed", ExpiresAt: &date, UsageTags: []string{" 自定义用途 ", "自定义用途"}})
	if err := ValidateCreateInput(input); err != nil {
		t.Fatal(err)
	}
	if len(input.UsageTags) != 1 || input.AutoRenewCheck != "enabled" || input.RenewalDecision != RenewalCancel {
		t.Fatalf("independent facts changed: %+v", input)
	}
}

func TestExternalVPSModelRejectsLegacyUsage(t *testing.T) {
	for _, body := range []string{`{"display_name":"server","usage_status":"idle"}`, `{"usage_status":"in_use"}`} {
		decoder := json.NewDecoder(strings.NewReader(body))
		decoder.DisallowUnknownFields()
		var input CreateInput
		if err := decoder.Decode(&input); err == nil {
			t.Fatal("legacy usage_status accepted")
		}
	}
	for _, status := range []LifecycleStatus{LifecycleIdle, LifecycleTesting, LifecycleToMigrate, LifecycleToCancel, LifecycleCancelled} {
		if IsValidLifecycleStatus(status) {
			t.Fatalf("legacy lifecycle accepted: %s", status)
		}
	}
	for _, status := range []RenewalDecision{RenewalObserve, RenewalMigrate, RenewalAutoRenewCancelled, RenewalReplaced} {
		if IsValidRenewalDecision(status) {
			t.Fatalf("legacy decision accepted: %s", status)
		}
	}
}

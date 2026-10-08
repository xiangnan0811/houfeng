package monitoringinstances

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestManagementCountsEvidenceIncludesCommandActionAudit(t *testing.T) {
	counts := ManagementCounts{CommandActionAuditCount: 3}
	if got := counts.EvidenceCount(); got != 3 {
		t.Fatalf("EvidenceCount() = %d, want 3", got)
	}
}

func TestRecordJSONDoesNotExposePrivateBindingSecrets(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(Record{
		MonitoringInstanceID: "mi_123",
		BindingStatus:        BindingBound,
		BindingFingerprint:   "fp-private",
		EnrollmentTokenHash:  "enroll-private",
		SyncTokenHash:        "sync-private",
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
	if payload["monitoring_instance_id"] != "mi_123" {
		t.Fatalf("monitoring_instance_id = %#v, want %q", payload["monitoring_instance_id"], "mi_123")
	}
}

func TestValidateCreateInputMetadataMatchesWireMetadataLimits(t *testing.T) {
	t.Parallel()

	maxLabels := make([]string, LinkedCreateMaxLabelCount)
	for index := range maxLabels {
		maxLabels[index] = "label"
	}
	tests := []struct {
		name    string
		input   CreateInput
		wantErr bool
	}{
		{name: "maximum label count", input: CreateInput{Labels: maxLabels}},
		{name: "too many labels", input: CreateInput{Labels: append(append([]string(nil), maxLabels...), "label")}, wantErr: true},
		{name: "maximum Unicode label runes", input: CreateInput{Labels: []string{strings.Repeat("界", LinkedCreateMaxLabelRunes)}}},
		{name: "too many Unicode label runes", input: CreateInput{Labels: []string{strings.Repeat("界", LinkedCreateMaxLabelRunes+1)}}, wantErr: true},
		{name: "maximum Unicode note runes", input: CreateInput{Note: strings.Repeat("界", LinkedCreateMaxNoteRunes)}},
		{name: "too many Unicode note runes", input: CreateInput{Note: strings.Repeat("界", LinkedCreateMaxNoteRunes+1)}, wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateCreateInputMetadata(tt.input)
			if errors.Is(err, ErrInvalidCreateInput) != tt.wantErr {
				t.Fatalf("invalid metadata error class matches = %t, want %t", errors.Is(err, ErrInvalidCreateInput), tt.wantErr)
			}
		})
	}

}

func TestNormalizeLinkedCreateWireIdentityCanonicalizesClearFields(t *testing.T) {
	t.Parallel()

	empty := NormalizeLinkedCreateWireIdentity(LinkedCreateWireIdentity{ClearFields: []string{}})
	if empty.ClearFields != nil {
		t.Fatalf("empty clear fields = %#v, want nil", empty.ClearFields)
	}

	got := NormalizeLinkedCreateWireIdentity(LinkedCreateWireIdentity{
		ClearFields: []string{" provider ", "region", "region", " city "},
	})
	if strings.Join(got.ClearFields, "\x00") != "city\x00provider\x00region" {
		t.Fatalf("clear fields = %#v, want sorted unique fields", got.ClearFields)
	}
}

func TestValidateLinkedCreateWireIdentityRejectsUnknownAndConflictingClearFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input LinkedCreateWireIdentity
		valid bool
	}{
		{name: "empty values are valid", input: LinkedCreateWireIdentity{}, valid: true},
		{name: "clears empty region", input: LinkedCreateWireIdentity{ClearFields: []string{"region"}}, valid: true},
		{name: "clears empty city", input: LinkedCreateWireIdentity{ClearFields: []string{"city"}}, valid: true},
		{name: "clears empty provider", input: LinkedCreateWireIdentity{ClearFields: []string{"provider"}}, valid: true},
		{name: "unknown field", input: LinkedCreateWireIdentity{ClearFields: []string{"country"}}, valid: false},
		{name: "region conflict", input: LinkedCreateWireIdentity{Region: "manual", ClearFields: []string{"region"}}, valid: false},
		{name: "city conflict", input: LinkedCreateWireIdentity{City: "manual", ClearFields: []string{"city"}}, valid: false},
		{name: "provider conflict", input: LinkedCreateWireIdentity{Provider: "manual", ClearFields: []string{"provider"}}, valid: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := NormalizeLinkedCreateWireIdentity(tt.input)
			err := ValidateLinkedCreateWireIdentity(input)
			if (err == nil) != tt.valid {
				t.Fatalf("ValidateLinkedCreateWireIdentity() error = %v, valid = %t", err, tt.valid)
			}
			if err != nil && !errors.Is(err, ErrInvalidCreateInput) {
				t.Fatalf("ValidateLinkedCreateWireIdentity() error = %v, want ErrInvalidCreateInput", err)
			}
		})
	}
}

func TestValidateCreateInputAllowsUnknownLocationValues(t *testing.T) {
	t.Parallel()

	input := NormalizeCreateInput(CreateInput{
		DisplayName:     "Pending instance",
		LifecycleStatus: LifecyclePendingEnrollment,
	})
	if err := ValidateCreateInput(input); err != nil {
		t.Fatalf("ValidateCreateInput() error = %v, want nil for empty location/provider", err)
	}
}

func TestValidateCreateInputRetainsDisplayAndLifecycleRequirements(t *testing.T) {
	t.Parallel()

	tests := []CreateInput{
		{LifecycleStatus: LifecyclePendingEnrollment},
		{DisplayName: "Pending instance"},
		{DisplayName: "Pending instance", LifecycleStatus: "invalid"},
	}
	for _, input := range tests {
		if err := ValidateCreateInput(NormalizeCreateInput(input)); !errors.Is(err, ErrInvalidCreateInput) {
			t.Fatalf("ValidateCreateInput(%#v) error = %v, want ErrInvalidCreateInput", input, err)
		}
	}
}

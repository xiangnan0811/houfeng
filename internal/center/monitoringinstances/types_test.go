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

package vpsassets

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeCreateInputTrimsDefaultsAndLabels(t *testing.T) {
	providerID := " pv_001 "
	input := NormalizeCreateInput(CreateInput{
		DisplayName:     " Tokyo Edge ",
		ProviderID:      &providerID,
		ProviderName:    " Hetzner ",
		ProductName:     " CX22 ",
		OrderRef:        " order-123 ",
		Country:         " JP ",
		Region:          " Kanto ",
		City:            " Tokyo ",
		Datacenter:      " nrt1 ",
		IPv4:            " 192.0.2.10 ",
		IPv6:            " 2001:db8::10 ",
		SSHHost:         " edge.example.com ",
		SSHUser:         " root ",
		OSName:          " Debian ",
		Virtualization:  " kvm ",
		LifecycleStatus: " active ",
		UsageStatus:     " in_use ",
		Labels:          []string{" edge ", "", "core", "edge"},
		Note:            " production ",
	})

	if input.DisplayName != "Tokyo Edge" {
		t.Fatalf("DisplayName = %q, want Tokyo Edge", input.DisplayName)
	}
	if input.ProviderID == nil || *input.ProviderID != "pv_001" {
		t.Fatalf("ProviderID = %#v, want pv_001", input.ProviderID)
	}
	if input.SSHPort != DefaultSSHPort {
		t.Fatalf("SSHPort = %d, want default %d", input.SSHPort, DefaultSSHPort)
	}
	if input.RenewalDecision != DefaultRenewalDecision {
		t.Fatalf("RenewalDecision = %q, want %q", input.RenewalDecision, DefaultRenewalDecision)
	}
	if input.Importance != DefaultImportance {
		t.Fatalf("Importance = %q, want %q", input.Importance, DefaultImportance)
	}
	if input.LifecycleStatus != LifecycleActive || input.UsageStatus != UsageInUse {
		t.Fatalf("statuses = %q/%q, want normalized active/in_use", input.LifecycleStatus, input.UsageStatus)
	}
	if !reflect.DeepEqual(input.Labels, []string{"edge", "core"}) {
		t.Fatalf("Labels = %#v, want trimmed unique labels", input.Labels)
	}
	if input.ProviderName != "Hetzner" || input.City != "Tokyo" || input.Note != "production" {
		t.Fatalf("string fields were not trimmed: %#v", input)
	}

	defaulted := NormalizeCreateInput(CreateInput{DisplayName: "Defaulted Edge"})
	if defaulted.LifecycleStatus != DefaultLifecycleStatus || defaulted.UsageStatus != DefaultUsageStatus {
		t.Fatalf("defaulted statuses = %q/%q, want %q/%q", defaulted.LifecycleStatus, defaulted.UsageStatus, DefaultLifecycleStatus, DefaultUsageStatus)
	}
}

func TestValidateCreateInput(t *testing.T) {
	tests := []struct {
		name  string
		input CreateInput
		want  error
	}{
		{name: "valid", input: CreateInput{DisplayName: "Tokyo Edge", LifecycleStatus: LifecycleActive, UsageStatus: UsageInUse}},
		{name: "missing statuses default", input: CreateInput{DisplayName: "Tokyo Edge"}},
		{name: "zero ssh port defaults", input: CreateInput{DisplayName: "Tokyo Edge", LifecycleStatus: LifecycleActive, UsageStatus: UsageInUse, SSHPort: 0}},
		{name: "blank display name", input: CreateInput{DisplayName: " ", LifecycleStatus: LifecycleActive, UsageStatus: UsageInUse}, want: ErrInvalidVPSAssetInput},
		{name: "invalid lifecycle status", input: CreateInput{DisplayName: "Tokyo Edge", LifecycleStatus: "online", UsageStatus: UsageInUse}, want: ErrInvalidVPSAssetInput},
		{name: "invalid usage status", input: CreateInput{DisplayName: "Tokyo Edge", LifecycleStatus: LifecycleActive, UsageStatus: "busy"}, want: ErrInvalidVPSAssetInput},
		{name: "invalid renewal decision", input: CreateInput{DisplayName: "Tokyo Edge", LifecycleStatus: LifecycleActive, UsageStatus: UsageInUse, RenewalDecision: "later"}, want: ErrInvalidVPSAssetInput},
		{name: "low ssh port", input: CreateInput{DisplayName: "Tokyo Edge", LifecycleStatus: LifecycleActive, UsageStatus: UsageInUse, SSHPort: -1}, want: ErrInvalidVPSAssetInput},
		{name: "high ssh port", input: CreateInput{DisplayName: "Tokyo Edge", LifecycleStatus: LifecycleActive, UsageStatus: UsageInUse, SSHPort: 65536}, want: ErrInvalidVPSAssetInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCreateInput(NormalizeCreateInput(tt.input))
			if !errors.Is(err, tt.want) {
				t.Fatalf("ValidateCreateInput() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestPatchInputPresenceNormalizationAndNullableProvider(t *testing.T) {
	var input PatchInput
	if err := json.Unmarshal([]byte(`{
		"display_name":" Tokyo Edge ",
		"provider_id":null,
		"lifecycle_status":" archived ",
		"usage_status":" idle ",
		"renewal_decision":" observe ",
		"ssh_port":2222,
		"labels":[" edge ","","edge","backup"]
	}`), &input); err != nil {
		t.Fatalf("Unmarshal PatchInput: %v", err)
	}

	input = NormalizePatchInput(input)
	if !input.DisplayName.Set || input.DisplayName.Value != "Tokyo Edge" {
		t.Fatalf("DisplayName patch = %#v, want trimmed set value", input.DisplayName)
	}
	if !input.ProviderID.Set || input.ProviderID.Value != nil {
		t.Fatalf("ProviderID patch = %#v, want explicit nil", input.ProviderID)
	}
	if !input.LifecycleStatus.Set || input.LifecycleStatus.Value != LifecycleArchived {
		t.Fatalf("LifecycleStatus patch = %#v, want archived", input.LifecycleStatus)
	}
	if !input.UsageStatus.Set || input.UsageStatus.Value != UsageIdle {
		t.Fatalf("UsageStatus patch = %#v, want idle", input.UsageStatus)
	}
	if !input.RenewalDecision.Set || input.RenewalDecision.Value != RenewalObserve {
		t.Fatalf("RenewalDecision patch = %#v, want observe", input.RenewalDecision)
	}
	if !input.SSHPort.Set || input.SSHPort.Value != 2222 {
		t.Fatalf("SSHPort patch = %#v, want 2222", input.SSHPort)
	}
	if !input.Labels.Set || !reflect.DeepEqual(input.Labels.Values, []string{"edge", "backup"}) {
		t.Fatalf("Labels patch = %#v, want normalized labels", input.Labels)
	}
	if !input.HasChanges() {
		t.Fatal("HasChanges() = false, want true")
	}
	if err := ValidatePatchInput(input); err != nil {
		t.Fatalf("ValidatePatchInput() error = %v", err)
	}
}

func TestPatchInputProviderBlankClearsReference(t *testing.T) {
	providerID := "   "
	input := NormalizePatchInput(PatchInput{ProviderID: PatchNullableString(&providerID)})
	if !input.ProviderID.Set || input.ProviderID.Value != nil {
		t.Fatalf("ProviderID = %#v, want set nil after blank normalization", input.ProviderID)
	}
}

func TestValidatePatchInputRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		input PatchInput
	}{
		{name: "blank display name", input: PatchInput{DisplayName: PatchString(" ")}},
		{name: "invalid lifecycle", input: PatchInput{LifecycleStatus: PatchLifecycle("online")}},
		{name: "invalid usage", input: PatchInput{UsageStatus: PatchUsage("busy")}},
		{name: "invalid renewal", input: PatchInput{RenewalDecision: PatchRenewal("later")}},
		{name: "zero ssh port", input: PatchInput{SSHPort: PatchInt(0)}},
		{name: "high ssh port", input: PatchInput{SSHPort: PatchInt(65536)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePatchInput(NormalizePatchInput(tt.input))
			if !errors.Is(err, ErrInvalidVPSAssetInput) {
				t.Fatalf("ValidatePatchInput() error = %v, want ErrInvalidVPSAssetInput", err)
			}
		})
	}
}

func TestValidateListFilters(t *testing.T) {
	tests := []struct {
		name    string
		filters ListFilters
		want    error
	}{
		{name: "empty"},
		{name: "valid", filters: ListFilters{ProviderID: " pv_001 ", LifecycleStatus: " active ", UsageStatus: " idle ", RenewalDecision: " keep "}},
		{name: "invalid lifecycle", filters: ListFilters{LifecycleStatus: "online"}, want: ErrInvalidVPSAssetInput},
		{name: "invalid usage", filters: ListFilters{UsageStatus: "busy"}, want: ErrInvalidVPSAssetInput},
		{name: "invalid renewal", filters: ListFilters{RenewalDecision: "later"}, want: ErrInvalidVPSAssetInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters := NormalizeListFilters(tt.filters)
			err := ValidateListFilters(filters)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ValidateListFilters() error = %v, want %v", err, tt.want)
			}
			if tt.name == "valid" && filters.ProviderID != "pv_001" {
				t.Fatalf("ProviderID = %q, want trimmed pv_001", filters.ProviderID)
			}
		})
	}
}

func TestDeriveArchivedAtLifecycleSemantics(t *testing.T) {
	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	existing := now.Add(-time.Hour)

	if got := DeriveArchivedAt(LifecycleActive, &existing, now); got != nil {
		t.Fatalf("active archived_at = %#v, want nil", got)
	}
	if got := DeriveArchivedAt(LifecycleArchived, nil, now); got == nil || !got.Equal(now) {
		t.Fatalf("new archived_at = %#v, want now", got)
	}
	if got := DeriveArchivedAt(LifecycleArchived, &existing, now); got == nil || !got.Equal(existing) {
		t.Fatalf("existing archived_at = %#v, want preserved existing", got)
	}
}

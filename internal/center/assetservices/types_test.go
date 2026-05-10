package assetservices

import (
	"errors"
	"testing"
)

func TestNormalizeCreateInput(t *testing.T) {
	t.Parallel()

	targetID := " tg_001 "
	input := NormalizeCreateInput(CreateInput{
		VPSID:       " vps_001 ",
		TargetID:    &targetID,
		Name:        " Blog ",
		ServiceType: "",
		Status:      "",
		URL:         " https://example.com ",
		Labels:      []string{" web ", "", "prod", "web"},
		Note:        " primary site ",
	})

	if input.VPSID != "vps_001" {
		t.Fatalf("VPSID = %q, want trimmed", input.VPSID)
	}
	if input.TargetID == nil || *input.TargetID != "tg_001" {
		t.Fatalf("TargetID = %#v, want trimmed target", input.TargetID)
	}
	if input.Name != "Blog" {
		t.Fatalf("Name = %q, want Blog", input.Name)
	}
	if input.ServiceType != ServiceTypeOther {
		t.Fatalf("ServiceType = %q, want default other", input.ServiceType)
	}
	if input.Status != ServiceStatusActive {
		t.Fatalf("Status = %q, want default active", input.Status)
	}
	if input.URL != "https://example.com" {
		t.Fatalf("URL = %q, want trimmed", input.URL)
	}
	if len(input.Labels) != 2 || input.Labels[0] != "web" || input.Labels[1] != "prod" {
		t.Fatalf("Labels = %#v, want normalized labels", input.Labels)
	}
	if input.Note != "primary site" {
		t.Fatalf("Note = %q, want trimmed", input.Note)
	}
}

func TestNormalizeCreateInputClearsBlankTarget(t *testing.T) {
	t.Parallel()

	targetID := " "
	input := NormalizeCreateInput(CreateInput{TargetID: &targetID, ServiceType: ServiceTypeWeb, Status: ServiceStatusActive})
	if input.TargetID != nil {
		t.Fatalf("TargetID = %#v, want nil for blank target", input.TargetID)
	}
}

func TestValidateCreateInput(t *testing.T) {
	t.Parallel()

	validPort := 443
	tests := []struct {
		name  string
		input CreateInput
		valid bool
	}{
		{name: "valid", input: CreateInput{VPSID: "vps_001", Name: "Blog", ServiceType: ServiceTypeWeb, Status: ServiceStatusActive, Port: &validPort}, valid: true},
		{name: "blank vps", input: CreateInput{Name: "Blog", ServiceType: ServiceTypeWeb, Status: ServiceStatusActive}, valid: false},
		{name: "blank name", input: CreateInput{VPSID: "vps_001", ServiceType: ServiceTypeWeb, Status: ServiceStatusActive}, valid: false},
		{name: "invalid type", input: CreateInput{VPSID: "vps_001", Name: "Blog", ServiceType: "ssh", Status: ServiceStatusActive}, valid: false},
		{name: "invalid status", input: CreateInput{VPSID: "vps_001", Name: "Blog", ServiceType: ServiceTypeWeb, Status: "deleted"}, valid: false},
		{name: "invalid low port", input: CreateInput{VPSID: "vps_001", Name: "Blog", ServiceType: ServiceTypeWeb, Status: ServiceStatusActive, Port: intPtr(0)}, valid: false},
		{name: "invalid high port", input: CreateInput{VPSID: "vps_001", Name: "Blog", ServiceType: ServiceTypeWeb, Status: ServiceStatusActive, Port: intPtr(65536)}, valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCreateInput(tt.input)
			if tt.valid && err != nil {
				t.Fatalf("ValidateCreateInput() error = %v, want nil", err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidServiceInput) {
				t.Fatalf("ValidateCreateInput() error = %v, want ErrInvalidServiceInput", err)
			}
		})
	}
}

func TestValidateListFilters(t *testing.T) {
	t.Parallel()

	if err := ValidateListFilters(ListFilters{ServiceType: ServiceTypeAPI, Status: ServiceStatusPaused}); err != nil {
		t.Fatalf("ValidateListFilters() error = %v, want nil", err)
	}
	if err := ValidateListFilters(ListFilters{ServiceType: "ssh"}); !errors.Is(err, ErrInvalidServiceInput) {
		t.Fatalf("ValidateListFilters() error = %v, want ErrInvalidServiceInput", err)
	}
	if err := ValidateListFilters(ListFilters{Status: "deleted"}); !errors.Is(err, ErrInvalidServiceInput) {
		t.Fatalf("ValidateListFilters() error = %v, want ErrInvalidServiceInput", err)
	}
}

func intPtr(value int) *int {
	return &value
}

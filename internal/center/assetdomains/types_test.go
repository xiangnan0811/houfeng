package assetdomains

import (
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/subscriptions"
)

func TestNormalizeAndValidateCreateInput(t *testing.T) {
	expiresAt := subscriptions.NewDate(time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC))
	input := NormalizeCreateInput(CreateInput{
		VPSID:        " vps_001 ",
		ServiceID:    stringPtr(" svc_001 "),
		TargetID:     stringPtr(" tg_001 "),
		DomainName:   " WWW.Example.COM. ",
		Purpose:      " public site ",
		Registrar:    " NameSilo ",
		ExpiresAt:    &expiresAt,
		HTTPSEnabled: true,
		Labels:       []string{" prod ", "", "prod"},
		Note:         " primary ",
	})

	if err := ValidateCreateInput(input); err != nil {
		t.Fatalf("ValidateCreateInput() error = %v", err)
	}
	if input.VPSID != "vps_001" || input.ServiceID == nil || *input.ServiceID != "svc_001" || input.TargetID == nil || *input.TargetID != "tg_001" {
		t.Fatalf("normalized identity = %#v, want trimmed ids", input)
	}
	if input.DomainName != "www.example.com" {
		t.Fatalf("DomainName = %q, want normalized domain", input.DomainName)
	}
	if input.Status != DomainStatusActive {
		t.Fatalf("Status = %q, want default active", input.Status)
	}
	if input.Purpose != "public site" || input.Registrar != "NameSilo" || input.Note != "primary" {
		t.Fatalf("text fields = %#v, want trimmed", input)
	}
	if len(input.Labels) != 1 || input.Labels[0] != "prod" {
		t.Fatalf("Labels = %#v, want normalized unique labels", input.Labels)
	}
}

func TestValidateCreateInputRejectsInvalidDomains(t *testing.T) {
	tests := []struct {
		name       string
		domainName string
	}{
		{name: "blank", domainName: " "},
		{name: "url", domainName: "https://example.com"},
		{name: "path", domainName: "example.com/path"},
		{name: "bare host", domainName: "localhost"},
		{name: "double dot", domainName: "example..com"},
		{name: "underscore", domainName: "api_example.com"},
		{name: "leading hyphen", domainName: "-api.example.com"},
		{name: "trailing hyphen", domainName: "api-.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := NormalizeCreateInput(CreateInput{
				VPSID:      "vps_001",
				DomainName: tt.domainName,
				Status:     DomainStatusActive,
			})
			if err := ValidateCreateInput(input); !errors.Is(err, ErrInvalidDomainInput) {
				t.Fatalf("ValidateCreateInput() error = %v, want ErrInvalidDomainInput", err)
			}
		})
	}
}

func TestValidateCreateInputRejectsInvalidStatusAndMissingVPS(t *testing.T) {
	tests := []CreateInput{
		{DomainName: "example.com", Status: DomainStatusActive},
		{VPSID: "vps_001", DomainName: "example.com", Status: "deleted"},
	}

	for _, input := range tests {
		input = NormalizeCreateInput(input)
		if err := ValidateCreateInput(input); !errors.Is(err, ErrInvalidDomainInput) {
			t.Fatalf("ValidateCreateInput(%#v) error = %v, want ErrInvalidDomainInput", input, err)
		}
	}
}

func TestValidateListFiltersRejectsInvalidStatus(t *testing.T) {
	filters := NormalizeListFilters(ListFilters{Status: "deleted"})
	if err := ValidateListFilters(filters); !errors.Is(err, ErrInvalidDomainInput) {
		t.Fatalf("ValidateListFilters() error = %v, want ErrInvalidDomainInput", err)
	}
}

func stringPtr(value string) *string {
	return &value
}

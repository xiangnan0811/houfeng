package providers

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeCreateInputTrimsStringsAndLabels(t *testing.T) {
	rating := 4
	input := NormalizeCreateInput(CreateInput{
		Name:        "  Hetzner  ",
		Website:     " https://hetzner.com ",
		PanelURL:    " https://console.hetzner.cloud ",
		AccountHint: " ops@example.com ",
		Country:     " DE ",
		Note:        " production ",
		Rating:      &rating,
		Labels:      []string{" edge ", "", "core", "edge"},
	})

	if input.Name != "Hetzner" {
		t.Fatalf("Name = %q, want Hetzner", input.Name)
	}
	if input.Website != "https://hetzner.com" {
		t.Fatalf("Website = %q, want trimmed URL", input.Website)
	}
	if input.PanelURL != "https://console.hetzner.cloud" {
		t.Fatalf("PanelURL = %q, want trimmed URL", input.PanelURL)
	}
	if input.AccountHint != "ops@example.com" {
		t.Fatalf("AccountHint = %q, want trimmed hint", input.AccountHint)
	}
	if input.Country != "DE" {
		t.Fatalf("Country = %q, want DE", input.Country)
	}
	if input.Note != "production" {
		t.Fatalf("Note = %q, want production", input.Note)
	}
	if !reflect.DeepEqual(input.Labels, []string{"edge", "core"}) {
		t.Fatalf("Labels = %#v, want trimmed unique labels", input.Labels)
	}
}

func TestValidateCreateInput(t *testing.T) {
	tests := []struct {
		name  string
		input CreateInput
		want  error
	}{
		{name: "valid without rating", input: CreateInput{Name: "Hetzner"}},
		{name: "valid rating", input: CreateInput{Name: "Hetzner", Rating: intPtr(5)}},
		{name: "blank name", input: CreateInput{Name: "  "}, want: ErrInvalidProviderInput},
		{name: "low rating", input: CreateInput{Name: "Hetzner", Rating: intPtr(0)}, want: ErrInvalidProviderInput},
		{name: "high rating", input: CreateInput{Name: "Hetzner", Rating: intPtr(6)}, want: ErrInvalidProviderInput},
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

func TestPatchInputPresenceAndNormalization(t *testing.T) {
	var input PatchInput
	if err := json.Unmarshal([]byte(`{"name":"  Akamai ","rating":null,"labels":[" edge ","","edge","backup"]}`), &input); err != nil {
		t.Fatalf("Unmarshal PatchInput: %v", err)
	}

	input = NormalizePatchInput(input)
	if !input.Name.Set || input.Name.Value != "Akamai" {
		t.Fatalf("Name patch = %#v, want trimmed set value", input.Name)
	}
	if !input.Rating.Set || input.Rating.Value != nil {
		t.Fatalf("Rating patch = %#v, want explicit null", input.Rating)
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

func TestValidatePatchInputRejectsBlankNameAndInvalidRating(t *testing.T) {
	tests := []struct {
		name  string
		input PatchInput
	}{
		{name: "blank name", input: PatchInput{Name: PatchString(" ")}},
		{name: "invalid rating", input: PatchInput{Rating: PatchRating(intPtr(9))}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePatchInput(NormalizePatchInput(tt.input))
			if !errors.Is(err, ErrInvalidProviderInput) {
				t.Fatalf("ValidatePatchInput() error = %v, want ErrInvalidProviderInput", err)
			}
		})
	}
}

func intPtr(value int) *int {
	return &value
}

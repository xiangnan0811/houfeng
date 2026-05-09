package renewals

import (
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/vpsassets"
)

func TestNormalizeAndValidateCreateDecisionInput(t *testing.T) {
	decidedAt := time.Date(2026, time.May, 9, 21, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	fromDecision := vpsassets.RenewalDecision(" keep ")
	input := NormalizeCreateDecisionInput(CreateDecisionInput{
		VPSID:        " vps_001 ",
		FromDecision: &fromDecision,
		ToDecision:   " cancel ",
		Reason:       " too expensive ",
		DecidedAt:    &decidedAt,
	})

	if input.VPSID != "vps_001" || input.FromDecision == nil || *input.FromDecision != vpsassets.RenewalKeep || input.ToDecision != vpsassets.RenewalCancel || input.Reason != "too expensive" {
		t.Fatalf("NormalizeCreateDecisionInput() = %#v, want trimmed values", input)
	}
	if input.DecidedAt == nil || input.DecidedAt.Location() != time.UTC {
		t.Fatalf("DecidedAt = %#v, want UTC timestamp", input.DecidedAt)
	}
	if err := ValidateCreateDecisionInput(input); err != nil {
		t.Fatalf("ValidateCreateDecisionInput() error = %v, want nil", err)
	}
}

func TestValidateCreateDecisionInputRejectsInvalidValues(t *testing.T) {
	invalidFrom := vpsassets.RenewalDecision("later")
	tests := []struct {
		name  string
		input CreateDecisionInput
	}{
		{name: "blank vps", input: CreateDecisionInput{VPSID: " ", ToDecision: vpsassets.RenewalCancel}},
		{name: "invalid from", input: CreateDecisionInput{VPSID: "vps_001", FromDecision: &invalidFrom, ToDecision: vpsassets.RenewalCancel}},
		{name: "invalid to", input: CreateDecisionInput{VPSID: "vps_001", ToDecision: "later"}},
		{name: "zero decided at", input: CreateDecisionInput{VPSID: "vps_001", ToDecision: vpsassets.RenewalCancel, DecidedAt: &time.Time{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCreateDecisionInput(NormalizeCreateDecisionInput(tt.input)); !errors.Is(err, ErrInvalidRenewalDecisionInput) {
				t.Fatalf("ValidateCreateDecisionInput() error = %v, want ErrInvalidRenewalDecisionInput", err)
			}
		})
	}
}

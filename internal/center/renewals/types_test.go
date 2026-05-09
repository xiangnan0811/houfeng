package renewals

import (
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/subscriptions"
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

func TestNormalizeAndValidateCreatePriceHistoryInput(t *testing.T) {
	changedAt := time.Date(2026, time.May, 9, 21, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	fromRenewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 8, 0, 0, 0, time.UTC))
	toRenewAt := subscriptions.NewDate(time.Date(2026, time.December, 1, 8, 0, 0, 0, time.UTC))
	input := NormalizeCreatePriceHistoryInput(CreatePriceHistoryInput{
		From: subscriptions.Record{
			SubscriptionID:     " sub_001 ",
			VPSID:              " vps_001 ",
			Price:              120,
			Currency:           " usd ",
			BillingCycle:       " annual ",
			BillingMonths:      12,
			MonthlyPrice:       10,
			RenewAt:            &fromRenewAt,
			AutoRenew:          true,
			AutoRenewCancelled: false,
			Status:             " active ",
		},
		To: subscriptions.Record{
			SubscriptionID:     " sub_001 ",
			VPSID:              " vps_001 ",
			Price:              240,
			Currency:           " usd ",
			BillingCycle:       " biennial ",
			BillingMonths:      24,
			MonthlyPrice:       10,
			RenewAt:            &toRenewAt,
			AutoRenew:          false,
			AutoRenewCancelled: true,
			Status:             " paused ",
		},
		ChangedAt: &changedAt,
	})

	if input.To.SubscriptionID != "sub_001" || input.To.VPSID != "vps_001" || input.To.Currency != "USD" || input.To.BillingCycle != "biennial" {
		t.Fatalf("NormalizeCreatePriceHistoryInput() = %#v, want trimmed subscription values", input)
	}
	if input.ChangedAt == nil || input.ChangedAt.Location() != time.UTC {
		t.Fatalf("ChangedAt = %#v, want UTC timestamp", input.ChangedAt)
	}
	if err := ValidateCreatePriceHistoryInput(input); err != nil {
		t.Fatalf("ValidateCreatePriceHistoryInput() error = %v", err)
	}
}

func TestValidateCreatePriceHistoryInputRejectsInvalidValues(t *testing.T) {
	valid := CreatePriceHistoryInput{
		From: subscriptions.Record{SubscriptionID: "sub_001", VPSID: "vps_001", Price: 10, Currency: "USD", BillingMonths: 1, MonthlyPrice: 10, Status: subscriptions.StatusActive},
		To:   subscriptions.Record{SubscriptionID: "sub_001", VPSID: "vps_001", Price: 12, Currency: "USD", BillingMonths: 1, MonthlyPrice: 12, Status: subscriptions.StatusActive},
	}
	tests := []struct {
		name  string
		input CreatePriceHistoryInput
	}{
		{name: "blank subscription", input: CreatePriceHistoryInput{From: subscriptions.Record{SubscriptionID: " "}, To: valid.To}},
		{name: "changed subscription", input: CreatePriceHistoryInput{From: valid.From, To: subscriptions.Record{SubscriptionID: "sub_002", VPSID: "vps_001", Price: 12, Currency: "USD", BillingMonths: 1, Status: subscriptions.StatusActive}}},
		{name: "blank vps", input: CreatePriceHistoryInput{From: valid.From, To: subscriptions.Record{SubscriptionID: "sub_001", VPSID: " ", Price: 12, Currency: "USD", BillingMonths: 1, Status: subscriptions.StatusActive}}},
		{name: "invalid price", input: CreatePriceHistoryInput{From: valid.From, To: subscriptions.Record{SubscriptionID: "sub_001", VPSID: "vps_001", Price: -1, Currency: "USD", BillingMonths: 1, Status: subscriptions.StatusActive}}},
		{name: "invalid currency", input: CreatePriceHistoryInput{From: valid.From, To: subscriptions.Record{SubscriptionID: "sub_001", VPSID: "vps_001", Price: 12, Currency: "US", BillingMonths: 1, Status: subscriptions.StatusActive}}},
		{name: "zero changed at", input: CreatePriceHistoryInput{From: valid.From, To: valid.To, ChangedAt: &time.Time{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCreatePriceHistoryInput(NormalizeCreatePriceHistoryInput(tt.input)); !errors.Is(err, ErrInvalidAssetHistoryInput) {
				t.Fatalf("ValidateCreatePriceHistoryInput() error = %v, want ErrInvalidAssetHistoryInput", err)
			}
		})
	}
}

func TestNormalizeAndValidateIPAndSpecHistoryInputs(t *testing.T) {
	changedAt := time.Date(2026, time.May, 9, 21, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	ipInput := NormalizeCreateIPHistoryInput(CreateIPHistoryInput{
		VPSID:     " vps_001 ",
		FromIPv4:  " 192.0.2.1 ",
		ToIPv4:    " 198.51.100.5 ",
		FromIPv6:  " 2001:db8::1 ",
		ToIPv6:    " 2001:db8::5 ",
		ChangedAt: &changedAt,
	})
	if ipInput.VPSID != "vps_001" || ipInput.ToIPv4 != "198.51.100.5" || ipInput.ChangedAt == nil || ipInput.ChangedAt.Location() != time.UTC {
		t.Fatalf("NormalizeCreateIPHistoryInput() = %#v, want trimmed UTC values", ipInput)
	}
	if err := ValidateCreateIPHistoryInput(ipInput); err != nil {
		t.Fatalf("ValidateCreateIPHistoryInput() error = %v", err)
	}

	specInput := NormalizeCreateSpecSnapshotInput(CreateSpecSnapshotInput{
		VPSID:          " vps_001 ",
		ProductName:    " CPX31 ",
		SSHHost:        " edge.example ",
		SSHPort:        2222,
		SSHUser:        " deploy ",
		OSName:         " Ubuntu 24.04 ",
		Virtualization: " kvm ",
		CapturedAt:     &changedAt,
	})
	if specInput.VPSID != "vps_001" || specInput.ProductName != "CPX31" || specInput.SSHHost != "edge.example" || specInput.CapturedAt == nil || specInput.CapturedAt.Location() != time.UTC {
		t.Fatalf("NormalizeCreateSpecSnapshotInput() = %#v, want trimmed UTC values", specInput)
	}
	if err := ValidateCreateSpecSnapshotInput(specInput); err != nil {
		t.Fatalf("ValidateCreateSpecSnapshotInput() error = %v", err)
	}
}

func TestValidateIPAndSpecHistoryInputsRejectInvalidValues(t *testing.T) {
	if err := ValidateCreateIPHistoryInput(NormalizeCreateIPHistoryInput(CreateIPHistoryInput{VPSID: "vps_001", FromIPv4: "192.0.2.1", ToIPv4: "192.0.2.1"})); !errors.Is(err, ErrInvalidAssetHistoryInput) {
		t.Fatalf("ValidateCreateIPHistoryInput(unchanged) error = %v, want ErrInvalidAssetHistoryInput", err)
	}
	if err := ValidateCreateSpecSnapshotInput(NormalizeCreateSpecSnapshotInput(CreateSpecSnapshotInput{VPSID: "vps_001", SSHPort: 0})); !errors.Is(err, ErrInvalidAssetHistoryInput) {
		t.Fatalf("ValidateCreateSpecSnapshotInput(invalid ssh port) error = %v, want ErrInvalidAssetHistoryInput", err)
	}
}

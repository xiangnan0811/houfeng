package subscriptions

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeCreateInputTrimsDefaultsAndCalculatesMonthlyPrice(t *testing.T) {
	input := NormalizeCreateInput(CreateInput{
		VPSID:         " vps_001 ",
		Price:         120,
		Currency:      " usd ",
		BillingCycle:  " annual ",
		BillingMonths: 12,
		PaymentMethod: " stripe ",
		Note:          " production ",
	})

	if input.VPSID != "vps_001" {
		t.Fatalf("VPSID = %q, want vps_001", input.VPSID)
	}
	if input.Currency != "USD" {
		t.Fatalf("Currency = %q, want USD", input.Currency)
	}
	if input.Status != StatusActive {
		t.Fatalf("Status = %q, want default active", input.Status)
	}
	if input.BillingCycle != "annual" || input.PaymentMethod != "stripe" || input.Note != "production" {
		t.Fatalf("string fields were not trimmed: %#v", input)
	}
	if got := CalculateMonthlyPrice(input.Price, input.BillingMonths); got != 10 {
		t.Fatalf("CalculateMonthlyPrice() = %v, want 10", got)
	}
}

func TestValidateCreateInput(t *testing.T) {
	tests := []struct {
		name  string
		input CreateInput
		want  error
	}{
		{name: "valid", input: CreateInput{VPSID: "vps_001", Price: 12, Currency: "usd", BillingMonths: 1}},
		{name: "blank vps", input: CreateInput{VPSID: " ", Price: 12, Currency: "USD", BillingMonths: 1}, want: ErrInvalidSubscriptionInput},
		{name: "negative price", input: CreateInput{VPSID: "vps_001", Price: -0.01, Currency: "USD", BillingMonths: 1}, want: ErrInvalidSubscriptionInput},
		{name: "too many price decimals", input: CreateInput{VPSID: "vps_001", Price: 12.345, Currency: "USD", BillingMonths: 1}, want: ErrInvalidSubscriptionInput},
		{name: "price exceeds schema precision", input: CreateInput{VPSID: "vps_001", Price: 10000000000, Currency: "USD", BillingMonths: 1}, want: ErrInvalidSubscriptionInput},
		{name: "zero billing months", input: CreateInput{VPSID: "vps_001", Price: 12, Currency: "USD"}, want: ErrInvalidSubscriptionInput},
		{name: "invalid currency", input: CreateInput{VPSID: "vps_001", Price: 12, Currency: "US1", BillingMonths: 1}, want: ErrInvalidSubscriptionInput},
		{name: "invalid status", input: CreateInput{VPSID: "vps_001", Price: 12, Currency: "USD", BillingMonths: 1, Status: "online"}, want: ErrInvalidSubscriptionInput},
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

func TestPatchInputPresenceNormalizationAndDates(t *testing.T) {
	var input PatchInput
	if err := json.Unmarshal([]byte(`{
		"vps_id":" vps_002 ",
		"price":120,
		"currency":" eur ",
		"billing_cycle":" annual ",
		"billing_months":12,
		"started_at":null,
		"renew_at":"2026-06-01",
		"auto_renew":true,
		"auto_renew_cancelled":false,
		"status":" paused ",
		"payment_method":" card ",
		"note":" review "
	}`), &input); err != nil {
		t.Fatalf("Unmarshal PatchInput: %v", err)
	}

	input = NormalizePatchInput(input)
	if !input.VPSID.Set || input.VPSID.Value != "vps_002" {
		t.Fatalf("VPSID patch = %#v, want trimmed set value", input.VPSID)
	}
	if !input.Currency.Set || input.Currency.Value != "EUR" {
		t.Fatalf("Currency patch = %#v, want EUR", input.Currency)
	}
	if !input.StartedAt.Set || input.StartedAt.Value != nil {
		t.Fatalf("StartedAt patch = %#v, want explicit null", input.StartedAt)
	}
	if !input.RenewAt.Set || input.RenewAt.Value == nil || input.RenewAt.Value.Time.Format(DateLayout) != "2026-06-01" {
		t.Fatalf("RenewAt patch = %#v, want 2026-06-01", input.RenewAt)
	}
	if !input.AutoRenew.Set || !input.AutoRenew.Value {
		t.Fatalf("AutoRenew patch = %#v, want true", input.AutoRenew)
	}
	if !input.Status.Set || input.Status.Value != StatusPaused {
		t.Fatalf("Status patch = %#v, want paused", input.Status)
	}
	if input.BillingCycle.Value != "annual" || input.PaymentMethod.Value != "card" || input.Note.Value != "review" {
		t.Fatalf("string patches were not trimmed: %#v", input)
	}
	if !input.HasChanges() {
		t.Fatal("HasChanges() = false, want true")
	}
	if err := ValidatePatchInput(input); err != nil {
		t.Fatalf("ValidatePatchInput() error = %v", err)
	}
}

func TestValidatePatchInputRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		input PatchInput
	}{
		{name: "blank vps", input: PatchInput{VPSID: PatchString(" ")}},
		{name: "negative price", input: PatchInput{Price: PatchFloat(-1)}},
		{name: "too many price decimals", input: PatchInput{Price: PatchFloat(12.345)}},
		{name: "zero billing months", input: PatchInput{BillingMonths: PatchInt(0)}},
		{name: "invalid currency", input: PatchInput{Currency: PatchString("US1")}},
		{name: "invalid status", input: PatchInput{Status: PatchStatus("online")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePatchInput(NormalizePatchInput(tt.input))
			if !errors.Is(err, ErrInvalidSubscriptionInput) {
				t.Fatalf("ValidatePatchInput() error = %v, want ErrInvalidSubscriptionInput", err)
			}
		})
	}
}

func TestDateJSONUsesNullableDateSemantics(t *testing.T) {
	var date Date
	if err := json.Unmarshal([]byte(`"2026-05-09"`), &date); err != nil {
		t.Fatalf("Unmarshal Date: %v", err)
	}
	if date.Time.Format(DateLayout) != "2026-05-09" {
		t.Fatalf("Date = %s, want 2026-05-09", date.Time.Format(DateLayout))
	}

	body, err := json.Marshal(Record{StartedAt: &date})
	if err != nil {
		t.Fatalf("Marshal Record: %v", err)
	}
	if string(body) == "" || !strings.Contains(string(body), `"started_at":"2026-05-09"`) {
		t.Fatalf("Record JSON = %s, want date string", body)
	}

	var create struct {
		StartedAt *Date `json:"started_at"`
	}
	if err := json.Unmarshal([]byte(`{"started_at":null}`), &create); err != nil {
		t.Fatalf("Unmarshal nullable create date: %v", err)
	}
	if create.StartedAt != nil {
		t.Fatalf("StartedAt = %#v, want nil", create.StartedAt)
	}

	if _, err := ParseDate("2026-99-99"); !errors.Is(err, ErrInvalidSubscriptionInput) {
		t.Fatalf("ParseDate(invalid) error = %v, want ErrInvalidSubscriptionInput", err)
	}
}

func TestValidateListFilters(t *testing.T) {
	days := 30
	tests := []struct {
		name    string
		filters ListFilters
		want    error
	}{
		{name: "empty"},
		{name: "valid", filters: ListFilters{VPSID: " vps_001 ", Status: " active ", RenewWithinDays: &days, Sort: " renew_at ", Order: " DESC "}},
		{name: "invalid status", filters: ListFilters{Status: "online"}, want: ErrInvalidSubscriptionInput},
		{name: "negative renew within", filters: ListFilters{RenewWithinDays: intPtr(-1)}, want: ErrInvalidSubscriptionInput},
		{name: "invalid sort", filters: ListFilters{Sort: "price"}, want: ErrInvalidSubscriptionInput},
		{name: "invalid order", filters: ListFilters{Order: "later"}, want: ErrInvalidSubscriptionInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters := NormalizeListFilters(tt.filters)
			err := ValidateListFilters(filters)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ValidateListFilters() error = %v, want %v", err, tt.want)
			}
			if tt.name == "valid" {
				if filters.VPSID != "vps_001" || filters.Status != StatusActive || filters.Sort != SortRenewAt || filters.Order != OrderDesc {
					t.Fatalf("filters = %#v, want normalized filters", filters)
				}
			}
		})
	}
}

func TestDateConversionClonesDateOnlyValues(t *testing.T) {
	raw := time.Date(2026, time.May, 9, 23, 30, 0, 0, time.FixedZone("CST", 8*3600))
	date := NewDate(raw)
	if date.Time.Format(time.RFC3339) != "2026-05-09T00:00:00Z" {
		t.Fatalf("NewDate() = %s, want midnight UTC date", date.Time.Format(time.RFC3339))
	}
	if TimePtrFromDate(nil) != nil || DateFromTimePtr(nil) != nil {
		t.Fatal("nil date conversions should return nil")
	}
}

func intPtr(value int) *int {
	return &value
}

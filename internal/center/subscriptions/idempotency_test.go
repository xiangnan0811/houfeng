package subscriptions

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeIdempotencyKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     string
		want    string
		wantErr error
	}{
		{name: "trims", key: "  create-1  ", want: "create-1"},
		{name: "uuid", key: "550e8400-e29b-41d4-a716-446655440000", want: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "empty", key: "   ", wantErr: ErrInvalidIdempotencyKey},
		{name: "too short", key: "short", wantErr: ErrInvalidIdempotencyKey},
		{name: "too long", key: strings.Repeat("a", MaxIdempotencyKeyLength+1), wantErr: ErrInvalidIdempotencyKey},
		{name: "space inside", key: "create key", wantErr: ErrInvalidIdempotencyKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeIdempotencyKey(tt.key)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NormalizeIdempotencyKey(%q) error = %v, want %v", tt.key, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeIdempotencyKey(%q) unexpected error = %v", tt.key, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeIdempotencyKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestCreateRequestDigestIsStableForNormalizedEquivalentInput(t *testing.T) {
	t.Parallel()

	started := NewDate(time.Date(2026, time.January, 1, 15, 4, 5, 0, time.UTC))
	left, err := CreateRequestDigest(CreateInput{
		VPSID:         " vps_001 ",
		Price:         12,
		Currency:      " usd ",
		BillingMonths: 1,
		StartedAt:     &started,
		Note:          " billing ",
	})
	if err != nil {
		t.Fatalf("CreateRequestDigest(left) error = %v", err)
	}
	right, err := CreateRequestDigest(NormalizeCreateInput(CreateInput{
		VPSID:         "vps_001",
		Price:         12,
		Currency:      "USD",
		BillingMonths: 1,
		StartedAt:     &started,
		Note:          "billing",
	}))
	if err != nil {
		t.Fatalf("CreateRequestDigest(right) error = %v", err)
	}
	if left != right {
		t.Fatalf("digest mismatch for equivalent create input: %q vs %q", left, right)
	}
	if len(left) != 64 {
		t.Fatalf("digest length = %d, want 64 hex characters", len(left))
	}
}

func TestCreateRequestDigestChangesWhenVPSOrPriceChanges(t *testing.T) {
	t.Parallel()

	base := CreateInput{VPSID: "vps_001", Price: 12, Currency: "USD", BillingMonths: 1}
	left, err := CreateRequestDigest(base)
	if err != nil {
		t.Fatalf("CreateRequestDigest(base) error = %v", err)
	}
	changedVPS, err := CreateRequestDigest(CreateInput{VPSID: "vps_002", Price: 12, Currency: "USD", BillingMonths: 1})
	if err != nil {
		t.Fatalf("CreateRequestDigest(vps) error = %v", err)
	}
	changedPrice, err := CreateRequestDigest(CreateInput{VPSID: "vps_001", Price: 13, Currency: "USD", BillingMonths: 1})
	if err != nil {
		t.Fatalf("CreateRequestDigest(price) error = %v", err)
	}
	if left == changedVPS || left == changedPrice {
		t.Fatalf("digest did not change for distinct create fields: base=%q vps=%q price=%q", left, changedVPS, changedPrice)
	}
}

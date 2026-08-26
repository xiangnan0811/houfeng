package subscriptions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrInvalidIdempotencyKey = errors.New("invalid idempotency key")
	ErrIdempotencyKeyReused  = errors.New("idempotency key reused")
)

const (
	MinIdempotencyKeyLength = 8
	MaxIdempotencyKeyLength = 128
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

type createRequestDigestBody struct {
	VPSID               string   `json:"vps_id"`
	Price               float64  `json:"price"`
	Currency            string   `json:"currency"`
	BillingCycle        string   `json:"billing_cycle"`
	BillingMonths       int      `json:"billing_months"`
	BillingPeriodUnit   string   `json:"billing_period_unit"`
	BillingPeriodLength int      `json:"billing_period_length"`
	StartedAt           string   `json:"started_at"`
	RenewAt             string   `json:"renew_at"`
	AutoRenew           bool     `json:"auto_renew"`
	AutoRenewCancelled  bool     `json:"auto_renew_cancelled"`
	RenewalMode         string   `json:"renewal_mode"`
	Status              string   `json:"status"`
	PaymentMethod       string   `json:"payment_method"`
	DisplayName         string   `json:"display_name"`
	CostCategory        string   `json:"cost_category"`
	Labels              []string `json:"labels"`
	TrialEndsAt         string   `json:"trial_ends_at"`
	EndsAt              string   `json:"ends_at"`
	Note                string   `json:"note"`
}

func NormalizeIdempotencyKey(key string) (string, error) {
	normalized := strings.TrimSpace(key)
	if len(normalized) < MinIdempotencyKeyLength || len(normalized) > MaxIdempotencyKeyLength {
		return "", fmt.Errorf("%w: length must be between %d and %d", ErrInvalidIdempotencyKey, MinIdempotencyKeyLength, MaxIdempotencyKeyLength)
	}
	if !idempotencyKeyPattern.MatchString(normalized) {
		return "", fmt.Errorf("%w: contains unsupported characters", ErrInvalidIdempotencyKey)
	}
	return normalized, nil
}

func CreateRequestDigest(input CreateInput) (string, error) {
	input = NormalizeCreateInput(input)
	labels := input.Labels
	if labels == nil {
		labels = []string{}
	}
	payload, err := json.Marshal(createRequestDigestBody{
		VPSID:               input.VPSID,
		Price:               input.Price,
		Currency:            input.Currency,
		BillingCycle:        input.BillingCycle,
		BillingMonths:       input.BillingMonths,
		BillingPeriodUnit:   input.BillingPeriodUnit,
		BillingPeriodLength: input.BillingPeriodLength,
		StartedAt:           dateDigest(input.StartedAt),
		RenewAt:             dateDigest(input.RenewAt),
		AutoRenew:           input.AutoRenew,
		AutoRenewCancelled:  input.AutoRenewCancelled,
		RenewalMode:         input.RenewalMode,
		Status:              string(input.Status),
		PaymentMethod:       input.PaymentMethod,
		DisplayName:         input.DisplayName,
		CostCategory:        input.CostCategory,
		Labels:              labels,
		TrialEndsAt:         dateDigest(input.TrialEndsAt),
		EndsAt:              dateDigest(input.EndsAt),
		Note:                input.Note,
	})
	if err != nil {
		return "", fmt.Errorf("encode subscription create digest: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func dateDigest(value *Date) string {
	if value == nil {
		return ""
	}
	return value.Time.UTC().Format(DateLayout)
}

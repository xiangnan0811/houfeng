package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrProviderNotFound = errors.New("provider not found")
var ErrInvalidProviderInput = errors.New("invalid provider input")

type Record struct {
	ProviderID  string    `json:"provider_id"`
	Name        string    `json:"name"`
	Website     string    `json:"website"`
	PanelURL    string    `json:"panel_url"`
	AccountHint string    `json:"account_hint"`
	Country     string    `json:"country"`
	Note        string    `json:"note"`
	Rating      *int      `json:"rating"`
	Labels      []string  `json:"labels"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateInput struct {
	Name        string   `json:"name"`
	Website     string   `json:"website"`
	PanelURL    string   `json:"panel_url"`
	AccountHint string   `json:"account_hint"`
	Country     string   `json:"country"`
	Note        string   `json:"note"`
	Rating      *int     `json:"rating"`
	Labels      []string `json:"labels"`
}

type PatchInput struct {
	Name        OptionalString `json:"name"`
	Website     OptionalString `json:"website"`
	PanelURL    OptionalString `json:"panel_url"`
	AccountHint OptionalString `json:"account_hint"`
	Country     OptionalString `json:"country"`
	Note        OptionalString `json:"note"`
	Rating      OptionalRating `json:"rating"`
	Labels      OptionalLabels `json:"labels"`
}

type OptionalString struct {
	Set   bool
	Value string
}

type OptionalRating struct {
	Set   bool
	Value *int
}

type OptionalLabels struct {
	Set    bool
	Values []string
}

type Repository interface {
	ListProviders(context.Context) ([]Record, error)
	GetProvider(context.Context, string) (Record, error)
	CreateProvider(context.Context, CreateInput) (Record, error)
	PatchProvider(context.Context, string, PatchInput) (Record, error)
}

func PatchString(value string) OptionalString {
	return OptionalString{Set: true, Value: value}
}

func PatchRating(value *int) OptionalRating {
	return OptionalRating{Set: true, Value: cloneInt(value)}
}

func PatchLabels(values []string) OptionalLabels {
	return OptionalLabels{Set: true, Values: append([]string(nil), values...)}
}

func (v *OptionalString) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("string value cannot be null")
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = value
	return nil
}

func (v *OptionalRating) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		v.Value = nil
		return nil
	}

	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = &value
	return nil
}

func (v *OptionalLabels) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("labels cannot be null")
	}

	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	v.Values = values
	return nil
}

func NormalizeCreateInput(input CreateInput) CreateInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Website = strings.TrimSpace(input.Website)
	input.PanelURL = strings.TrimSpace(input.PanelURL)
	input.AccountHint = strings.TrimSpace(input.AccountHint)
	input.Country = strings.TrimSpace(input.Country)
	input.Note = strings.TrimSpace(input.Note)
	input.Labels = NormalizeLabels(input.Labels)
	return input
}

func ValidateCreateInput(input CreateInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidProviderInput)
	}
	if !isValidRating(input.Rating) {
		return fmt.Errorf("%w: rating must be between 1 and 5", ErrInvalidProviderInput)
	}
	return nil
}

func NormalizePatchInput(input PatchInput) PatchInput {
	input.Name = normalizeOptionalString(input.Name)
	input.Website = normalizeOptionalString(input.Website)
	input.PanelURL = normalizeOptionalString(input.PanelURL)
	input.AccountHint = normalizeOptionalString(input.AccountHint)
	input.Country = normalizeOptionalString(input.Country)
	input.Note = normalizeOptionalString(input.Note)
	if input.Labels.Set {
		input.Labels.Values = NormalizeLabels(input.Labels.Values)
	}
	return input
}

func ValidatePatchInput(input PatchInput) error {
	if input.Name.Set && input.Name.Value == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidProviderInput)
	}
	if input.Rating.Set && !isValidRating(input.Rating.Value) {
		return fmt.Errorf("%w: rating must be between 1 and 5", ErrInvalidProviderInput)
	}
	return nil
}

func (input PatchInput) HasChanges() bool {
	return input.Name.Set ||
		input.Website.Set ||
		input.PanelURL.Set ||
		input.AccountHint.Set ||
		input.Country.Set ||
		input.Note.Set ||
		input.Rating.Set ||
		input.Labels.Set
}

func NormalizeLabels(labels []string) []string {
	normalized := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))
	for _, raw := range labels {
		label := strings.TrimSpace(raw)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		normalized = append(normalized, label)
	}
	return normalized
}

func normalizeOptionalString(value OptionalString) OptionalString {
	if value.Set {
		value.Value = strings.TrimSpace(value.Value)
	}
	return value
}

func isValidRating(rating *int) bool {
	if rating == nil {
		return true
	}
	return *rating >= 1 && *rating <= 5
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

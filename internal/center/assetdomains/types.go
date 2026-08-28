package assetdomains

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"houfeng/internal/center/createidempotency"
	"houfeng/internal/center/subscriptions"
)

var ErrDomainNotFound = errors.New("asset domain not found")
var ErrDomainOwnerNotFound = errors.New("asset domain owner not found")
var ErrDomainServiceNotFound = errors.New("asset domain service not found")
var ErrDomainTargetNotFound = errors.New("asset domain target not found")
var ErrDomainConflict = errors.New("asset domain conflict")
var ErrInvalidDomainInput = errors.New("invalid asset domain input")

type Date = subscriptions.Date

type DomainStatus string

const (
	DomainStatusActive  DomainStatus = "active"
	DomainStatusPaused  DomainStatus = "paused"
	DomainStatusRetired DomainStatus = "retired"
	DomainStatusUnknown DomainStatus = "unknown"
)

type Record struct {
	DomainID     string       `json:"domain_id"`
	VPSID        string       `json:"vps_id"`
	ServiceID    *string      `json:"service_id"`
	TargetID     *string      `json:"target_id"`
	DomainName   string       `json:"domain_name"`
	Purpose      string       `json:"purpose"`
	Status       DomainStatus `json:"status"`
	Registrar    string       `json:"registrar"`
	ExpiresAt    *Date        `json:"expires_at"`
	AutoRenew    bool         `json:"auto_renew"`
	HTTPSEnabled bool         `json:"https_enabled"`
	Labels       []string     `json:"labels"`
	Note         string       `json:"note"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type CreateInput struct {
	VPSID        string       `json:"vps_id"`
	ServiceID    *string      `json:"service_id"`
	TargetID     *string      `json:"target_id"`
	DomainName   string       `json:"domain_name"`
	Purpose      string       `json:"purpose"`
	Status       DomainStatus `json:"status"`
	Registrar    string       `json:"registrar"`
	ExpiresAt    *Date        `json:"expires_at"`
	AutoRenew    bool         `json:"auto_renew"`
	HTTPSEnabled bool         `json:"https_enabled"`
	Labels       []string     `json:"labels"`
	Note         string       `json:"note"`
}

type ListFilters struct {
	VPSID     string
	ServiceID string
	TargetID  string
	Status    DomainStatus
}

type Repository interface {
	ListAssetDomains(context.Context, ListFilters) ([]Record, error)
	ListAssetDomainsForVPS(context.Context, string) ([]Record, error)
	CreateAssetDomain(context.Context, CreateInput) (Record, error)
}

type IdempotentRepository interface {
	CreateAssetDomainIdempotent(context.Context, CreateInput, string) (Record, bool, error)
}

var ErrInvalidIdempotencyKey = createidempotency.ErrInvalidIdempotencyKey
var ErrIdempotencyKeyReused = createidempotency.ErrIdempotencyKeyReused

func NormalizeCreateInput(input CreateInput) CreateInput {
	input.VPSID = strings.TrimSpace(input.VPSID)
	input.ServiceID = normalizeNullableString(input.ServiceID)
	input.TargetID = normalizeNullableString(input.TargetID)
	input.DomainName = NormalizeDomainName(input.DomainName)
	input.Purpose = strings.TrimSpace(input.Purpose)
	input.Status = DomainStatus(strings.TrimSpace(string(input.Status)))
	if input.Status == "" {
		input.Status = DomainStatusActive
	}
	input.Registrar = strings.TrimSpace(input.Registrar)
	input.Labels = NormalizeLabels(input.Labels)
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func ValidateCreateInput(input CreateInput) error {
	if strings.TrimSpace(input.VPSID) == "" {
		return fmt.Errorf("%w: vps_id is required", ErrInvalidDomainInput)
	}
	if !IsValidDomainName(input.DomainName) {
		return fmt.Errorf("%w: invalid domain_name", ErrInvalidDomainInput)
	}
	if !IsValidDomainStatus(input.Status) {
		return fmt.Errorf("%w: invalid status", ErrInvalidDomainInput)
	}
	return nil
}

func NormalizeListFilters(filters ListFilters) ListFilters {
	filters.VPSID = strings.TrimSpace(filters.VPSID)
	filters.ServiceID = strings.TrimSpace(filters.ServiceID)
	filters.TargetID = strings.TrimSpace(filters.TargetID)
	filters.Status = DomainStatus(strings.TrimSpace(string(filters.Status)))
	return filters
}

func ValidateListFilters(filters ListFilters) error {
	if filters.Status != "" && !IsValidDomainStatus(filters.Status) {
		return fmt.Errorf("%w: invalid status", ErrInvalidDomainInput)
	}
	return nil
}

func NormalizeDomainName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.TrimSuffix(value, ".")
}

func IsValidDomainName(value string) bool {
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "/:@?#[]\\") {
		return false
	}
	if strings.ContainsAny(value, " \t\r\n") || strings.Contains(value, "..") || !strings.Contains(value, ".") {
		return false
	}
	labels := strings.Split(value, ".")
	for _, label := range labels {
		if !isValidDomainLabel(label) {
			return false
		}
	}
	return true
}

func IsValidDomainStatus(value DomainStatus) bool {
	switch value {
	case DomainStatusActive, DomainStatusPaused, DomainStatusRetired, DomainStatusUnknown:
		return true
	default:
		return false
	}
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

func DateFromTimePtr(value *time.Time) *Date {
	return subscriptions.DateFromTimePtr(value)
}

func TimePtrFromDate(value *Date) *time.Time {
	return subscriptions.TimePtrFromDate(value)
}

func isValidDomainLabel(label string) bool {
	if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return false
	}
	for _, ch := range label {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func normalizeNullableString(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

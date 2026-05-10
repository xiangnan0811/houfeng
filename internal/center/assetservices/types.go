package assetservices

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrServiceNotFound = errors.New("asset service not found")
var ErrServiceOwnerNotFound = errors.New("asset service owner not found")
var ErrServiceTargetNotFound = errors.New("asset service target not found")
var ErrInvalidServiceInput = errors.New("invalid asset service input")

type ServiceType string

const (
	ServiceTypeWeb      ServiceType = "web"
	ServiceTypeAPI      ServiceType = "api"
	ServiceTypeDatabase ServiceType = "database"
	ServiceTypeWorker   ServiceType = "worker"
	ServiceTypeProxy    ServiceType = "proxy"
	ServiceTypeOther    ServiceType = "other"
)

type ServiceStatus string

const (
	ServiceStatusActive  ServiceStatus = "active"
	ServiceStatusPaused  ServiceStatus = "paused"
	ServiceStatusRetired ServiceStatus = "retired"
	ServiceStatusUnknown ServiceStatus = "unknown"
)

type Record struct {
	ServiceID   string        `json:"service_id"`
	VPSID       string        `json:"vps_id"`
	TargetID    *string       `json:"target_id"`
	Name        string        `json:"name"`
	ServiceType ServiceType   `json:"service_type"`
	Status      ServiceStatus `json:"status"`
	URL         string        `json:"url"`
	Port        *int          `json:"port"`
	Labels      []string      `json:"labels"`
	Note        string        `json:"note"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type CreateInput struct {
	VPSID       string        `json:"vps_id"`
	TargetID    *string       `json:"target_id"`
	Name        string        `json:"name"`
	ServiceType ServiceType   `json:"service_type"`
	Status      ServiceStatus `json:"status"`
	URL         string        `json:"url"`
	Port        *int          `json:"port"`
	Labels      []string      `json:"labels"`
	Note        string        `json:"note"`
}

type ListFilters struct {
	VPSID       string
	TargetID    string
	ServiceType ServiceType
	Status      ServiceStatus
}

type Repository interface {
	ListAssetServices(context.Context, ListFilters) ([]Record, error)
	ListAssetServicesForVPS(context.Context, string) ([]Record, error)
	CreateAssetService(context.Context, CreateInput) (Record, error)
}

func NormalizeCreateInput(input CreateInput) CreateInput {
	input.VPSID = strings.TrimSpace(input.VPSID)
	input.TargetID = normalizeNullableString(input.TargetID)
	input.Name = strings.TrimSpace(input.Name)
	input.ServiceType = ServiceType(strings.TrimSpace(string(input.ServiceType)))
	if input.ServiceType == "" {
		input.ServiceType = ServiceTypeOther
	}
	input.Status = ServiceStatus(strings.TrimSpace(string(input.Status)))
	if input.Status == "" {
		input.Status = ServiceStatusActive
	}
	input.URL = strings.TrimSpace(input.URL)
	input.Labels = NormalizeLabels(input.Labels)
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func ValidateCreateInput(input CreateInput) error {
	if strings.TrimSpace(input.VPSID) == "" {
		return fmt.Errorf("%w: vps_id is required", ErrInvalidServiceInput)
	}
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidServiceInput)
	}
	if !IsValidServiceType(input.ServiceType) {
		return fmt.Errorf("%w: invalid service_type", ErrInvalidServiceInput)
	}
	if !IsValidServiceStatus(input.Status) {
		return fmt.Errorf("%w: invalid status", ErrInvalidServiceInput)
	}
	if input.Port != nil && !IsValidPort(*input.Port) {
		return fmt.Errorf("%w: port must be between 1 and 65535", ErrInvalidServiceInput)
	}
	return nil
}

func NormalizeListFilters(filters ListFilters) ListFilters {
	filters.VPSID = strings.TrimSpace(filters.VPSID)
	filters.TargetID = strings.TrimSpace(filters.TargetID)
	filters.ServiceType = ServiceType(strings.TrimSpace(string(filters.ServiceType)))
	filters.Status = ServiceStatus(strings.TrimSpace(string(filters.Status)))
	return filters
}

func ValidateListFilters(filters ListFilters) error {
	if filters.ServiceType != "" && !IsValidServiceType(filters.ServiceType) {
		return fmt.Errorf("%w: invalid service_type", ErrInvalidServiceInput)
	}
	if filters.Status != "" && !IsValidServiceStatus(filters.Status) {
		return fmt.Errorf("%w: invalid status", ErrInvalidServiceInput)
	}
	return nil
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

func IsValidServiceType(value ServiceType) bool {
	switch value {
	case ServiceTypeWeb, ServiceTypeAPI, ServiceTypeDatabase, ServiceTypeWorker, ServiceTypeProxy, ServiceTypeOther:
		return true
	default:
		return false
	}
}

func IsValidServiceStatus(value ServiceStatus) bool {
	switch value {
	case ServiceStatusActive, ServiceStatusPaused, ServiceStatusRetired, ServiceStatusUnknown:
		return true
	default:
		return false
	}
}

func IsValidPort(port int) bool {
	return port >= 1 && port <= 65535
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

package ipquality

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidIPQualityReport = errors.New("invalid ip quality report")

type ProviderResultWrite struct {
	Provider     string
	Status       string
	SourceType   string
	LatencyMS    *int
	UsageType    string
	CompanyType  string
	RiskLevel    string
	RiskScore    string
	RegionCode   string
	RegionName   string
	IsProxy      *bool
	IsTor        *bool
	IsVPN        *bool
	IsServer     *bool
	IsAbuser     *bool
	IsRobot      *bool
	ErrorCode    string
	ErrorSummary string
	ExtraJSON    json.RawMessage
}

type ServiceUnlockWrite struct {
	Service      string
	Source       string
	Status       string
	ProbeStatus  string
	LatencyMS    *int
	Region       string
	UnlockType   string
	ErrorCode    string
	ErrorSummary string
	ExtraJSON    json.RawMessage
}

type ReportWrite struct {
	MonitoringInstanceID string
	ObservedAt           time.Time
	ReceivedAt           time.Time
	AgentVersion         string
	Fingerprint          string
	SyncBatchID          string
	IPAddress            string
	IPVersion            int
	Status               string
	ASN                  string
	Organization         string
	Latitude             *float64
	Longitude            *float64
	UseRegionCode        string
	UseRegionName        string
	RegisteredRegionCode string
	RegisteredRegionName string
	RiskLevel            string
	ErrorCode            string
	ErrorSummary         string
	IsBackfilled         bool
	RawJSON              json.RawMessage
	CoverageJSON         json.RawMessage
	DiagnosticsJSON      json.RawMessage
	ProviderResults      []ProviderResultWrite
	ServiceUnlocks       []ServiceUnlockWrite
}

type Repository interface {
	SaveReports(context.Context, []ReportWrite) error
	GetVPSIPQuality(context.Context, string) (VPSReport, error)
	GetVPSIPQualityReportDetail(context.Context, string, string) (VPSReport, error)
	GetLatestVPSIPQualitySummary(context.Context, string) (*Summary, error)
	ListLatestSummariesForVPS(context.Context, []string) (map[string]Summary, error)
}

type Summary struct {
	ReportID        string    `json:"report_id,omitempty"`
	VPSID           string    `json:"vps_id,omitempty"`
	ObservedAt      time.Time `json:"observed_at"`
	IPAddress       string    `json:"ip_address"`
	IPVersion       int       `json:"ip_version"`
	Status          string    `json:"status"`
	RiskLevel       string    `json:"risk_level,omitempty"`
	UseRegionCode   string    `json:"use_region_code,omitempty"`
	UseRegionName   string    `json:"use_region_name,omitempty"`
	ASN             string    `json:"asn,omitempty"`
	Organization    string    `json:"organization,omitempty"`
	Stale           bool      `json:"stale"`
	Ambiguous       bool      `json:"ambiguous"`
	AssignmentMode  string    `json:"assignment_mode,omitempty"`
	ErrorCode       string    `json:"error_code,omitempty"`
	ErrorSummary    string    `json:"error_summary,omitempty"`
	ProviderCount   int       `json:"provider_count"`
	UnlockableCount int       `json:"unlockable_count"`
	Coverage        *Coverage `json:"coverage,omitempty"`
}

type VPSReport struct {
	Summary         *Summary             `json:"summary,omitempty"`
	LatestReport    *Report              `json:"latest_report,omitempty"`
	ProviderResults []ProviderResultRead `json:"provider_results"`
	ServiceUnlocks  []ServiceUnlockRead  `json:"service_unlocks"`
	History         []Summary            `json:"history"`
}

type Report struct {
	ReportID             string          `json:"report_id"`
	MonitoringInstanceID string          `json:"monitoring_instance_id"`
	ObservedAt           time.Time       `json:"observed_at"`
	ReceivedAt           time.Time       `json:"received_at"`
	AgentVersion         string          `json:"agent_version"`
	Fingerprint          string          `json:"fingerprint"`
	SyncBatchID          string          `json:"sync_batch_id"`
	IPAddress            string          `json:"ip_address"`
	IPVersion            int             `json:"ip_version"`
	Status               string          `json:"status"`
	ASN                  string          `json:"asn,omitempty"`
	Organization         string          `json:"organization,omitempty"`
	Latitude             *float64        `json:"latitude,omitempty"`
	Longitude            *float64        `json:"longitude,omitempty"`
	UseRegionCode        string          `json:"use_region_code,omitempty"`
	UseRegionName        string          `json:"use_region_name,omitempty"`
	RegisteredRegionCode string          `json:"registered_region_code,omitempty"`
	RegisteredRegionName string          `json:"registered_region_name,omitempty"`
	RiskLevel            string          `json:"risk_level,omitempty"`
	ErrorCode            string          `json:"error_code,omitempty"`
	ErrorSummary         string          `json:"error_summary,omitempty"`
	IsBackfilled         bool            `json:"is_backfilled"`
	RawJSON              json.RawMessage `json:"raw_json,omitempty"`
	Coverage             *Coverage       `json:"coverage,omitempty"`
	DiagnosticsJSON      json.RawMessage `json:"diagnostics_json,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
}

type ProviderResultRead struct {
	Provider     string          `json:"provider"`
	Status       string          `json:"status,omitempty"`
	SourceType   string          `json:"source_type,omitempty"`
	LatencyMS    *int            `json:"latency_ms,omitempty"`
	UsageType    string          `json:"usage_type,omitempty"`
	CompanyType  string          `json:"company_type,omitempty"`
	RiskLevel    string          `json:"risk_level,omitempty"`
	RiskScore    string          `json:"risk_score,omitempty"`
	RegionCode   string          `json:"region_code,omitempty"`
	RegionName   string          `json:"region_name,omitempty"`
	IsProxy      *bool           `json:"is_proxy,omitempty"`
	IsTor        *bool           `json:"is_tor,omitempty"`
	IsVPN        *bool           `json:"is_vpn,omitempty"`
	IsServer     *bool           `json:"is_server,omitempty"`
	IsAbuser     *bool           `json:"is_abuser,omitempty"`
	IsRobot      *bool           `json:"is_robot,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorSummary string          `json:"error_summary,omitempty"`
	ExtraJSON    json.RawMessage `json:"extra_json,omitempty"`
}

type ServiceUnlockRead struct {
	Service      string          `json:"service"`
	Source       string          `json:"source,omitempty"`
	Status       string          `json:"status"`
	ProbeStatus  string          `json:"probe_status,omitempty"`
	LatencyMS    *int            `json:"latency_ms,omitempty"`
	Region       string          `json:"region,omitempty"`
	UnlockType   string          `json:"unlock_type,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorSummary string          `json:"error_summary,omitempty"`
	ExtraJSON    json.RawMessage `json:"extra_json,omitempty"`
}

type Coverage struct {
	ExpectedProviderCount      int `json:"expected_provider_count"`
	SuccessfulProviderCount    int `json:"successful_provider_count"`
	FailedProviderCount        int `json:"failed_provider_count"`
	SkippedProviderCount       int `json:"skipped_provider_count"`
	NotConfiguredProviderCount int `json:"not_configured_provider_count"`
	ExpectedServiceCount       int `json:"expected_service_count"`
	SuccessfulServiceCount     int `json:"successful_service_count"`
	FailedServiceCount         int `json:"failed_service_count"`
	SkippedServiceCount        int `json:"skipped_service_count"`
	NotConfiguredServiceCount  int `json:"not_configured_service_count"`
}

func ValidateReportWrite(report ReportWrite) error {
	if strings.TrimSpace(report.MonitoringInstanceID) == "" ||
		report.ObservedAt.IsZero() ||
		strings.TrimSpace(report.AgentVersion) == "" ||
		strings.TrimSpace(report.Fingerprint) == "" ||
		strings.TrimSpace(report.SyncBatchID) == "" ||
		strings.TrimSpace(report.IPAddress) == "" ||
		report.IPVersion == 0 ||
		strings.TrimSpace(report.Status) == "" {
		return ErrInvalidIPQualityReport
	}
	for _, provider := range report.ProviderResults {
		if strings.TrimSpace(provider.Provider) == "" {
			return ErrInvalidIPQualityReport
		}
		if strings.TrimSpace(provider.Status) != "" && !allowedSourceStatus(provider.Status) {
			return ErrInvalidIPQualityReport
		}
		if strings.TrimSpace(provider.SourceType) != "" && !allowedSourceType(provider.SourceType) {
			return ErrInvalidIPQualityReport
		}
	}
	for _, unlock := range report.ServiceUnlocks {
		if strings.TrimSpace(unlock.Service) == "" || strings.TrimSpace(unlock.Status) == "" {
			return ErrInvalidIPQualityReport
		}
		if strings.TrimSpace(unlock.ProbeStatus) != "" && !allowedSourceStatus(unlock.ProbeStatus) {
			return ErrInvalidIPQualityReport
		}
	}
	return nil
}

func allowedSourceStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "success", "failure", "skipped", "not_configured":
		return true
	default:
		return false
	}
}

func allowedSourceType(value string) bool {
	switch strings.TrimSpace(value) {
	case "default", "optional", "custom":
		return true
	default:
		return false
	}
}

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
}

type ServiceUnlockWrite struct {
	Service      string
	Status       string
	Region       string
	UnlockType   string
	ErrorCode    string
	ErrorSummary string
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
	ProviderResults      []ProviderResultWrite
	ServiceUnlocks       []ServiceUnlockWrite
}

type Repository interface {
	SaveReports(context.Context, []ReportWrite) error
	GetVPSIPQuality(context.Context, string) (VPSReport, error)
	ListLatestSummariesForVPS(context.Context, []string) (map[string]Summary, error)
}

type Summary struct {
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
	CreatedAt            time.Time       `json:"created_at"`
}

type ProviderResultRead struct {
	Provider     string `json:"provider"`
	UsageType    string `json:"usage_type,omitempty"`
	CompanyType  string `json:"company_type,omitempty"`
	RiskLevel    string `json:"risk_level,omitempty"`
	RiskScore    string `json:"risk_score,omitempty"`
	RegionCode   string `json:"region_code,omitempty"`
	RegionName   string `json:"region_name,omitempty"`
	IsProxy      *bool  `json:"is_proxy,omitempty"`
	IsTor        *bool  `json:"is_tor,omitempty"`
	IsVPN        *bool  `json:"is_vpn,omitempty"`
	IsServer     *bool  `json:"is_server,omitempty"`
	IsAbuser     *bool  `json:"is_abuser,omitempty"`
	IsRobot      *bool  `json:"is_robot,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorSummary string `json:"error_summary,omitempty"`
}

type ServiceUnlockRead struct {
	Service      string `json:"service"`
	Status       string `json:"status"`
	Region       string `json:"region,omitempty"`
	UnlockType   string `json:"unlock_type,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorSummary string `json:"error_summary,omitempty"`
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
	}
	for _, unlock := range report.ServiceUnlocks {
		if strings.TrimSpace(unlock.Service) == "" || strings.TrimSpace(unlock.Status) == "" {
			return ErrInvalidIPQualityReport
		}
	}
	return nil
}

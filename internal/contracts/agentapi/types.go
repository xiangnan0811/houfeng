package agentapi

import (
	"encoding/json"
	"time"
)

const (
	BindingStatusUnbound             = "未绑定"
	BindingStatusBound               = "已绑定"
	BindingStatusPendingConfirmation = "指纹变更待确认"
)

const (
	ErrorCodeInvalidRequest             = "invalid_request"
	ErrorCodeInvalidJSON                = "invalid_json"
	ErrorCodeInvalidEnrollmentToken     = "invalid_enrollment_token"
	ErrorCodeInvalidSyncToken           = "invalid_sync_token"
	ErrorCodeBindingNotAccepted         = "binding_not_accepted"
	ErrorCodeMethodNotAllowed           = "method_not_allowed"
	ErrorCodeMonitoringInstanceNotFound = "monitoring_instance_not_found"
	ErrorCodeInternalError              = "internal_error"
)

const (
	ProbeResultSuccess = "success"
	ProbeResultFailure = "failure"
)

const (
	ProbeKindTCP  = "tcp"
	ProbeKindHTTP = "http"
	ProbeKindTLS  = "tls"
)

const (
	ProbeErrorTimeout      = "timeout"
	ProbeErrorConnect      = "connect"
	ProbeErrorHTTPStatus   = "http_status"
	ProbeErrorTLSHandshake = "tls_handshake"
)

const (
	IPQualityStatusSuccess = "success"
	IPQualityStatusPartial = "partial"
	IPQualityStatusFailure = "failure"
)

const (
	FrequencyTier5s  = "5s"
	FrequencyTier1m  = "1m"
	FrequencyTier5m  = "5m"
	FrequencyTier15m = "15m"
	FrequencyTier6h  = "6h"
)

type EnrollmentRequest struct {
	Token       string `json:"token"`
	Fingerprint string `json:"fingerprint"`
}

type EnrollmentResponse struct {
	MonitoringInstanceID string `json:"monitoring_instance_id"`
	Status               string `json:"status"`
	BindingStatus        string `json:"binding_status"`
	SyncToken            string `json:"sync_token,omitempty"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type MonitoringInstanceHeartbeat struct {
	ObservedAt   time.Time `json:"observed_at"`
	AgentVersion string    `json:"agent_version"`
	Fingerprint  string    `json:"fingerprint"`
	SyncBatchID  string    `json:"sync_batch_id"`
	IsBackfilled bool      `json:"is_backfilled,omitempty"`
}

// ContainerInfo is a lightweight snapshot of a single Docker container as
// observed by the agent during host sample collection.
type ContainerInfo struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Image  string   `json:"image"`
	Status string   `json:"status"` // "running" / "exited" / etc.
	CPUPct *float64 `json:"cpu_pct,omitempty"`
	MemPct *float64 `json:"mem_pct,omitempty"`
}

type HostSamplePayload struct {
	ObservedAt           time.Time       `json:"observed_at"`
	AgentVersion         string          `json:"agent_version"`
	Fingerprint          string          `json:"fingerprint"`
	SyncBatchID          string          `json:"sync_batch_id"`
	CPUUsagePct          float64         `json:"cpu_usage_pct"`
	Load1                float64         `json:"load_1"`
	Load5                float64         `json:"load_5"`
	Load15               float64         `json:"load_15"`
	MemUsedPct           float64         `json:"mem_used_pct"`
	MemAvailableBytes    int64           `json:"mem_available_bytes"`
	MemTotalBytes        int64           `json:"mem_total_bytes"`
	SwapUsedPct          float64         `json:"swap_used_pct"`
	DiskUsedPct          float64         `json:"disk_used_pct"`
	DiskTotalBytes       int64           `json:"disk_total_bytes"`
	InodeUsedPct         float64         `json:"inode_used_pct"`
	NetInBytesPerSec     int64           `json:"net_in_bytes_per_sec"`
	NetOutBytesPerSec    int64           `json:"net_out_bytes_per_sec"`
	CPUIOWaitPct         float64         `json:"cpu_iowait_pct"`
	CPUStealPct          float64         `json:"cpu_steal_pct"`
	DiskReadBytesPerSec  int64           `json:"disk_read_bytes_per_sec"`
	DiskWriteBytesPerSec int64           `json:"disk_write_bytes_per_sec"`
	DiskBusyPct          float64         `json:"disk_busy_pct"`
	UptimeSeconds        int64           `json:"uptime_seconds"`
	MaintenanceContext   bool            `json:"maintenance_context,omitempty"`
	IsBackfilled         bool            `json:"is_backfilled,omitempty"`
	Containers           []ContainerInfo `json:"containers,omitempty"`
}

type ProbeObservationPayload struct {
	TargetID     string    `json:"target_id"`
	ProbeItemID  string    `json:"probe_item_id"`
	ProbeKind    string    `json:"probe_kind"`
	ObservedAt   time.Time `json:"observed_at"`
	AgentVersion string    `json:"agent_version"`
	Fingerprint  string    `json:"fingerprint"`
	SyncBatchID  string    `json:"sync_batch_id"`
	// ResultKind is "success" or "failure". Success observations may carry latency_ms,
	// plus http_status for HTTP probes and tls_expiry_days for TLS probes. Failures
	// should carry error_code and error_summary. probe_kind remains tcp/http/tls.
	ResultKind         string `json:"result_kind"`
	LatencyMS          *int   `json:"latency_ms,omitempty"`
	HTTPStatus         *int   `json:"http_status,omitempty"`
	TLSExpiryDays      *int   `json:"tls_expiry_days,omitempty"`
	ErrorCode          string `json:"error_code,omitempty"`
	ErrorSummary       string `json:"error_summary,omitempty"`
	MaintenanceContext bool   `json:"maintenance_context,omitempty"`
	IsBackfilled       bool   `json:"is_backfilled,omitempty"`
}

type IPQualityProviderResultPayload struct {
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

type IPQualityServiceUnlockPayload struct {
	Service      string `json:"service"`
	Status       string `json:"status"`
	Region       string `json:"region,omitempty"`
	UnlockType   string `json:"unlock_type,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorSummary string `json:"error_summary,omitempty"`
}

type IPQualityReportPayload struct {
	ObservedAt           time.Time                        `json:"observed_at"`
	AgentVersion         string                           `json:"agent_version"`
	Fingerprint          string                           `json:"fingerprint"`
	SyncBatchID          string                           `json:"sync_batch_id"`
	IPAddress            string                           `json:"ip_address"`
	IPVersion            int                              `json:"ip_version"`
	Status               string                           `json:"status"`
	ASN                  string                           `json:"asn,omitempty"`
	Organization         string                           `json:"organization,omitempty"`
	Latitude             *float64                         `json:"latitude,omitempty"`
	Longitude            *float64                         `json:"longitude,omitempty"`
	UseRegionCode        string                           `json:"use_region_code,omitempty"`
	UseRegionName        string                           `json:"use_region_name,omitempty"`
	RegisteredRegionCode string                           `json:"registered_region_code,omitempty"`
	RegisteredRegionName string                           `json:"registered_region_name,omitempty"`
	RiskLevel            string                           `json:"risk_level,omitempty"`
	ErrorCode            string                           `json:"error_code,omitempty"`
	ErrorSummary         string                           `json:"error_summary,omitempty"`
	IsBackfilled         bool                             `json:"is_backfilled,omitempty"`
	RawJSON              json.RawMessage                  `json:"raw_json,omitempty"`
	ProviderResults      []IPQualityProviderResultPayload `json:"provider_results,omitempty"`
	ServiceUnlocks       []IPQualityServiceUnlockPayload  `json:"service_unlocks,omitempty"`
}

// SyncRequest keeps heartbeat sync as the canonical carrier; host_samples and
// probe_observations are optional adjunct facts attached to the same sync batch.
// CommandResults carries back outputs from pending actions that were executed
// since the last sync.
type SyncRequest struct {
	MonitoringInstanceID string                        `json:"monitoring_instance_id"`
	SyncToken            string                        `json:"sync_token"`
	Heartbeats           []MonitoringInstanceHeartbeat `json:"heartbeats,omitempty"`
	HostSamples          []HostSamplePayload           `json:"host_samples,omitempty"`
	ProbeObservations    []ProbeObservationPayload     `json:"probe_observations,omitempty"`
	IPQualityReports     []IPQualityReportPayload      `json:"ip_quality_reports,omitempty"`
	CommandResults       []CommandResult               `json:"command_results,omitempty"`
}

type ProbeAssignment struct {
	TargetID   string `json:"target_id"`
	TargetHost string `json:"target_host"`
	// TargetBasePort stays non-omitempty so nil encodes as explicit JSON null.
	TargetBasePort     *int            `json:"target_base_port"`
	MaintenanceContext bool            `json:"maintenance_context"`
	ProbeItemID        string          `json:"probe_item_id"`
	ProbeKind          string          `json:"probe_kind"`
	FrequencyTier      string          `json:"frequency_tier"`
	TimeoutSeconds     int             `json:"timeout_seconds"`
	Config             json.RawMessage `json:"config"`
}

type SyncPlan struct {
	HostSampleFrequencyTier      string            `json:"host_sample_frequency_tier"`
	HostSampleMaintenanceContext bool              `json:"host_sample_maintenance_context"`
	ProbeAssignments             []ProbeAssignment `json:"probe_assignments,omitempty"`
	IPQualityPlan                *IPQualityPlan    `json:"ip_quality_plan,omitempty"`
	PendingAction                *PendingAction    `json:"pending_action,omitempty"`
}

type IPQualityPlan struct {
	Enabled          bool     `json:"enabled"`
	FrequencySeconds int      `json:"frequency_seconds"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
	Services         []string `json:"services,omitempty"`
}

// PendingAction describes a command the center wants the agent to execute.
// The agent validates the CommandID against its compiled-in whitelist before
// running anything.
type PendingAction struct {
	CommandID string `json:"command_id"`
	ActionID  string `json:"action_id"`
}

// CommandResult carries the output of an executed pending action back to
// the center in the next SyncRequest.
type CommandResult struct {
	ActionID  string `json:"action_id"`
	CommandID string `json:"command_id"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exit_code"`
}

type SyncResponse struct {
	AcceptedAt time.Time `json:"accepted_at"`
	Status     string    `json:"status"`
	Plan       *SyncPlan `json:"plan,omitempty"`
}

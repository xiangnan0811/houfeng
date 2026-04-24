package agentapi

import "time"

const (
	BindingStatusUnbound             = "未绑定"
	BindingStatusBound               = "已绑定"
	BindingStatusPendingConfirmation = "指纹变更待确认"
)

const (
	ErrorCodeInvalidRequest         = "invalid_request"
	ErrorCodeInvalidJSON            = "invalid_json"
	ErrorCodeInvalidEnrollmentToken = "invalid_enrollment_token"
	ErrorCodeInvalidSyncToken       = "invalid_sync_token"
	ErrorCodeBindingNotAccepted     = "binding_not_accepted"
	ErrorCodeMethodNotAllowed       = "method_not_allowed"
	ErrorCodeNodeNotFound           = "node_not_found"
	ErrorCodeInternalError          = "internal_error"
)

type EnrollmentRequest struct {
	Token       string `json:"token"`
	Fingerprint string `json:"fingerprint"`
}

type EnrollmentResponse struct {
	NodeID        string `json:"node_id"`
	Status        string `json:"status"`
	BindingStatus string `json:"binding_status"`
	SyncToken     string `json:"sync_token,omitempty"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type NodeHeartbeat struct {
	ObservedAt   time.Time `json:"observed_at"`
	AgentVersion string    `json:"agent_version"`
	Fingerprint  string    `json:"fingerprint"`
	SyncBatchID  string    `json:"sync_batch_id"`
}

type HostSamplePayload struct {
	ObservedAt           time.Time `json:"observed_at"`
	AgentVersion         string    `json:"agent_version"`
	Fingerprint          string    `json:"fingerprint"`
	SyncBatchID          string    `json:"sync_batch_id"`
	CPUUsagePct          float64   `json:"cpu_usage_pct"`
	Load1                float64   `json:"load_1"`
	Load5                float64   `json:"load_5"`
	Load15               float64   `json:"load_15"`
	MemUsedPct           float64   `json:"mem_used_pct"`
	MemAvailableBytes    int64     `json:"mem_available_bytes"`
	SwapUsedPct          float64   `json:"swap_used_pct"`
	DiskUsedPct          float64   `json:"disk_used_pct"`
	InodeUsedPct         float64   `json:"inode_used_pct"`
	NetInBytesPerSec     int64     `json:"net_in_bytes_per_sec"`
	NetOutBytesPerSec    int64     `json:"net_out_bytes_per_sec"`
	CPUIowaitPct         float64   `json:"cpu_iowait_pct"`
	CPUStealPct          float64   `json:"cpu_steal_pct"`
	DiskReadBytesPerSec  int64     `json:"disk_read_bytes_per_sec"`
	DiskWriteBytesPerSec int64     `json:"disk_write_bytes_per_sec"`
	DiskBusyPct          float64   `json:"disk_busy_pct"`
	UptimeSeconds        int64     `json:"uptime_seconds"`
	MaintenanceContext   bool      `json:"maintenance_context,omitempty"`
	IsBackfilled         bool      `json:"is_backfilled,omitempty"`
}

type ProbeObservationPayload struct {
	TargetID           string    `json:"target_id"`
	ProbeItemID        string    `json:"probe_item_id"`
	ObservedAt         time.Time `json:"observed_at"`
	AgentVersion       string    `json:"agent_version"`
	Fingerprint        string    `json:"fingerprint"`
	SyncBatchID        string    `json:"sync_batch_id"`
	ResultKind         string    `json:"result_kind"`
	LatencyMS          *int      `json:"latency_ms,omitempty"`
	HTTPStatus         *int      `json:"http_status,omitempty"`
	TLSExpiryDays      *int      `json:"tls_expiry_days,omitempty"`
	ErrorCode          string    `json:"error_code,omitempty"`
	ErrorSummary       string    `json:"error_summary,omitempty"`
	MaintenanceContext bool      `json:"maintenance_context,omitempty"`
	IsBackfilled       bool      `json:"is_backfilled,omitempty"`
}

type SyncRequest struct {
	NodeID            string                    `json:"node_id"`
	SyncToken         string                    `json:"sync_token"`
	Heartbeats        []NodeHeartbeat           `json:"heartbeats,omitempty"`
	HostSamples       []HostSamplePayload       `json:"host_samples,omitempty"`
	ProbeObservations []ProbeObservationPayload `json:"probe_observations,omitempty"`
}

type SyncResponse struct {
	AcceptedAt time.Time `json:"accepted_at"`
	Status     string    `json:"status"`
}

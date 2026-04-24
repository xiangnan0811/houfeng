package runtimefacts

import (
	"context"
	"time"
)

type HostSample struct {
	NodeID               string    `json:"node_id"`
	ObservedAt           time.Time `json:"observed_at"`
	ReceivedAt           time.Time `json:"received_at"`
	AgentVersion         string    `json:"agent_version"`
	Fingerprint          string    `json:"fingerprint"`
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
	CPUIOWaitPct         float64   `json:"cpu_iowait_pct"`
	CPUStealPct          float64   `json:"cpu_steal_pct"`
	DiskReadBytesPerSec  int64     `json:"disk_read_bytes_per_sec"`
	DiskWriteBytesPerSec int64     `json:"disk_write_bytes_per_sec"`
	DiskBusyPct          float64   `json:"disk_busy_pct"`
	UptimeSeconds        int64     `json:"uptime_seconds"`
	MaintenanceContext   bool      `json:"maintenance_context"`
	IsBackfilled         bool      `json:"is_backfilled"`
	SyncBatchID          string    `json:"sync_batch_id"`
}

type ProbeObservation struct {
	NodeID             string    `json:"node_id"`
	TargetID           string    `json:"target_id"`
	ProbeItemID        string    `json:"probe_item_id"`
	ProbeKind          string    `json:"probe_kind"`
	ObservedAt         time.Time `json:"observed_at"`
	ReceivedAt         time.Time `json:"received_at"`
	AgentVersion       string    `json:"agent_version"`
	Fingerprint        string    `json:"fingerprint"`
	ResultKind         string    `json:"result_kind"`
	LatencyMS          *int      `json:"latency_ms"`
	HTTPStatus         *int      `json:"http_status"`
	TLSExpiryDays      *int      `json:"tls_expiry_days"`
	ErrorCode          string    `json:"error_code,omitempty"`
	ErrorSummary       string    `json:"error_summary,omitempty"`
	MaintenanceContext bool      `json:"maintenance_context"`
	IsBackfilled       bool      `json:"is_backfilled"`
	SyncBatchID        string    `json:"sync_batch_id"`
}

type NodeRuntimeFacts struct {
	NodeID           string      `json:"node_id"`
	LatestHostSample *HostSample `json:"latest_host_sample"`
}

type TargetRuntimeFacts struct {
	TargetID                string             `json:"target_id"`
	LatestProbeObservations []ProbeObservation `json:"latest_probe_observations"`
}

type Repository interface {
	GetNodeRuntimeFacts(context.Context, string) (NodeRuntimeFacts, error)
	GetTargetRuntimeFacts(context.Context, string) (TargetRuntimeFacts, error)
}

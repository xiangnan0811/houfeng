package observations

import (
	"time"

	"houfeng/internal/contracts/agentapi"
)

type HostSampleWrite struct {
	NodeID               string
	ObservedAt           time.Time
	ReceivedAt           time.Time
	AgentVersion         string
	Fingerprint          string
	CPUUsagePct          float64
	Load1                float64
	Load5                float64
	Load15               float64
	MemUsedPct           float64
	MemAvailableBytes    int64
	SwapUsedPct          float64
	DiskUsedPct          float64
	InodeUsedPct         float64
	NetInBytesPerSec     int64
	NetOutBytesPerSec    int64
	CPUIOWaitPct         float64
	CPUStealPct          float64
	DiskReadBytesPerSec  int64
	DiskWriteBytesPerSec int64
	DiskBusyPct          float64
	UptimeSeconds        int64
	MaintenanceContext   bool
	IsBackfilled         bool
	SyncBatchID          string
	Containers           []agentapi.ContainerInfo
}

type ProbeObservationWrite struct {
	NodeID             string
	TargetID           string
	ProbeItemID        string
	ProbeKind          string
	ObservedAt         time.Time
	ReceivedAt         time.Time
	AgentVersion       string
	Fingerprint        string
	ResultKind         string
	LatencyMS          *int
	HTTPStatus         *int
	TLSExpiryDays      *int
	ErrorCode          string
	ErrorSummary       string
	MaintenanceContext bool
	IsBackfilled       bool
	SyncBatchID        string
}

type ProbeMetadata struct {
	TargetID  string
	ProbeKind string
}

type BatchWrite struct {
	NodeID            string
	HostSamples       []HostSampleWrite
	ProbeObservations []ProbeObservationWrite
}

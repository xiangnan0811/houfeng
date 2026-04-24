package observations

import "time"

type HostSampleWrite struct {
	ObservedAt           time.Time
	ReceivedAt           time.Time
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
}

type ProbeObservationWrite struct {
	TargetID           string
	ProbeItemID        string
	ProbeKind          string
	ObservedAt         time.Time
	ReceivedAt         time.Time
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

type BatchWrite struct {
	NodeID            string
	HostSamples       []HostSampleWrite
	ProbeObservations []ProbeObservationWrite
}

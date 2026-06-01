package enrollment

import "time"

type EnrollInput struct {
	Token       string
	Fingerprint string
}

type EnrollResult struct {
	MonitoringInstanceID string
	BindingStatus        string
	SyncToken            string
}

type HeartbeatPayload struct {
	ObservedAt   time.Time
	AgentVersion string
	Fingerprint  string
	SyncBatchID  string
	IsBackfilled bool
}

type SyncInput struct {
	MonitoringInstanceID string
	SyncToken            string
	Heartbeats           []HeartbeatPayload
}

type HeartbeatWrite struct {
	MonitoringInstanceID string
	ObservedAt           time.Time
	ReceivedAt           time.Time
	AgentVersion         string
	Fingerprint          string
	SyncBatchID          string
	IsBackfilled         bool
}

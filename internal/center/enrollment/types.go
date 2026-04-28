package enrollment

import "time"

type EnrollInput struct {
	Token       string
	Fingerprint string
}

type EnrollResult struct {
	NodeID        string
	BindingStatus string
	SyncToken     string
}

type HeartbeatPayload struct {
	ObservedAt   time.Time
	AgentVersion string
	Fingerprint  string
	SyncBatchID  string
	IsBackfilled bool
}

type SyncInput struct {
	NodeID     string
	SyncToken  string
	Heartbeats []HeartbeatPayload
}

type HeartbeatWrite struct {
	NodeID       string
	ObservedAt   time.Time
	ReceivedAt   time.Time
	AgentVersion string
	Fingerprint  string
	SyncBatchID  string
	IsBackfilled bool
}

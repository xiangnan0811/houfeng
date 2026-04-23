package enrollment

import "time"

type EnrollInput struct {
	Token        string `json:"token"`
	Fingerprint  string `json:"fingerprint"`
	AgentVersion string `json:"agent_version"`
}

type EnrollResult struct {
	NodeID        string `json:"node_id"`
	BindingStatus string `json:"binding_status"`
	Status        string `json:"status"`
}

type HeartbeatPayload struct {
	ObservedAt   time.Time `json:"observed_at"`
	AgentVersion string    `json:"agent_version"`
	Fingerprint  string    `json:"fingerprint"`
	SyncBatchID  string    `json:"sync_batch_id"`
}

type SyncInput struct {
	NodeID     string             `json:"node_id"`
	Heartbeats []HeartbeatPayload `json:"heartbeats"`
}

type BindingUpdate struct {
	NodeID             string
	BindingStatus      string
	BindingFingerprint string
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

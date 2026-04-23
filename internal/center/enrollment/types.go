package enrollment

import "time"

type EnrollInput struct {
	EnrollmentToken string    `json:"enrollment_token"`
	Fingerprint     string    `json:"fingerprint"`
	SyncedAt        time.Time `json:"synced_at"`
}

type EnrollResult struct {
	NodeID        string `json:"node_id"`
	BindingStatus string `json:"binding_status"`
	Status        string `json:"status"`
}

type HeartbeatPayload struct {
	ObservedAt   time.Time `json:"observed_at"`
	ReceivedAt   time.Time `json:"received_at"`
	AgentVersion string    `json:"agent_version"`
	Fingerprint  string    `json:"fingerprint"`
	IsBackfilled bool      `json:"is_backfilled"`
}

type SyncInput struct {
	NodeID      string             `json:"node_id"`
	SyncBatchID string             `json:"sync_batch_id"`
	Heartbeats  []HeartbeatPayload `json:"heartbeats"`
}

type BindingUpdate struct {
	NodeID             string
	BindingStatus      string
	BindingFingerprint string
	SyncedAt           time.Time
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

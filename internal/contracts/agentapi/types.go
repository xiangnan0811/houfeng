package agentapi

import "time"

type EnrollmentRequest struct {
	NodeName     string `json:"node_name"`
	Token        string `json:"token"`
	Fingerprint  string `json:"fingerprint"`
	AgentVersion string `json:"agent_version"`
}

type EnrollmentResponse struct {
	NodeID string `json:"node_id"`
	Status string `json:"status"`
}

type NodeHeartbeat struct {
	ObservedAt   time.Time `json:"observed_at"`
	AgentVersion string    `json:"agent_version"`
	Fingerprint  string    `json:"fingerprint"`
	SyncBatchID  string    `json:"sync_batch_id"`
}

type SyncRequest struct {
	NodeID     string          `json:"node_id"`
	Heartbeats []NodeHeartbeat `json:"heartbeats,omitempty"`
}

type SyncResponse struct {
	AcceptedAt time.Time `json:"accepted_at"`
	Status     string    `json:"status"`
}

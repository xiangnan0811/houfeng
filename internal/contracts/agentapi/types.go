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

type SyncRequest struct {
	NodeID     string          `json:"node_id"`
	SyncToken  string          `json:"sync_token"`
	Heartbeats []NodeHeartbeat `json:"heartbeats,omitempty"`
}

type SyncResponse struct {
	AcceptedAt time.Time `json:"accepted_at"`
	Status     string    `json:"status"`
}

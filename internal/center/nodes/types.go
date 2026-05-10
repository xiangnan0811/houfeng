package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	LifecyclePendingEnrollment = "待接入"
	LifecycleInUse             = "在用"
	LifecycleObserving         = "观察中"
	LifecycleNoRenewal         = "不续费"
	LifecycleRetired           = "已退役"

	MonitoringEnabled          = "启用"
	MonitoringMaintenance      = "维护中"
	MonitoringPaused           = "暂停"
	BindingUnbound             = "未绑定"
	BindingBound               = "已绑定"
	BindingPendingConfirmation = "指纹变更待确认"
	HealthNormal               = "正常"

	OnboardingPhaseNotStarted               = "未开始接入"
	OnboardingPhaseBoundAwaitingObservation = "已绑定，等待稳定观测"
	OnboardingPhaseCompleted                = "接入完成"
	OnboardingPhaseBindingConflict          = "绑定冲突待处理"
)

var ErrNodeNotFound = errors.New("node not found")
var ErrInvalidBindingTransition = errors.New("invalid binding transition")
var ErrNodeMetadataConflict = errors.New("node metadata conflict")

var allowedLifecycleStatuses = map[string]struct{}{
	LifecyclePendingEnrollment: {},
	LifecycleInUse:             {},
	LifecycleObserving:         {},
	LifecycleNoRenewal:         {},
	LifecycleRetired:           {},
}

// LastAction describes the queued, in-flight, or completed command action for
// the node. It is nil when no action has ever been requested.
type LastAction struct {
	ActionID  string `json:"action_id"`
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
}

type Record struct {
	NodeID                     string          `json:"node_id"`
	DisplayName                string          `json:"display_name"`
	Group                      string          `json:"group"`
	Region                     string          `json:"region"`
	City                       string          `json:"city"`
	Provider                   string          `json:"provider"`
	LifecycleStatus            string          `json:"lifecycle_status"`
	MonitoringStatus           string          `json:"monitoring_status"`
	BindingStatus              string          `json:"binding_status"`
	EnrollmentTokenHash        string          `json:"-"`
	EnrollmentTokenIssuedAt    *time.Time      `json:"-"`
	SyncTokenHash              string          `json:"-"`
	BindingFingerprint         string          `json:"-"`
	BindingEpochStartedAt      *time.Time      `json:"-"`
	PendingBindingFingerprint  string          `json:"-"`
	PendingBindingFirstSeenAt  *time.Time      `json:"-"`
	PendingBindingLastSeenAt   *time.Time      `json:"-"`
	PendingBindingAttemptCount int             `json:"-"`
	Labels                     []string        `json:"labels"`
	Note                       string          `json:"note"`
	CurrentHealthStatus        string          `json:"current_health_status"`
	LastHeartbeatAt            *time.Time      `json:"last_heartbeat_at,omitempty"`
	LastSyncAt                 *time.Time      `json:"last_sync_at,omitempty"`
	CurrentActiveIncidentCount int             `json:"current_active_incident_count"`
	CurrentPrimaryIssueSummary string          `json:"current_primary_issue_summary"`
	LastAction                 *LastAction     `json:"last_action,omitempty"`
	LastActionRaw              json.RawMessage `json:"-"`
	CreatedAt                  time.Time       `json:"created_at"`
	UpdatedAt                  time.Time       `json:"updated_at"`
}

type CreateInput struct {
	DisplayName     string   `json:"display_name"`
	Group           string   `json:"group"`
	Region          string   `json:"region"`
	City            string   `json:"city"`
	Provider        string   `json:"provider"`
	LifecycleStatus string   `json:"lifecycle_status"`
	Labels          []string `json:"labels"`
	Note            string   `json:"note"`
}

type UpdateMetadataInput struct {
	Group             *string    `json:"group,omitempty"`
	Labels            []string   `json:"labels"`
	Note              string     `json:"note"`
	ExpectedUpdatedAt *time.Time `json:"-"`
}

type Repository interface {
	ListNodes(context.Context) ([]Record, error)
	GetNode(context.Context, string) (Record, error)
	CreateNode(context.Context, CreateInput) (Record, error)
	UpdateNodeMetadata(context.Context, string, UpdateMetadataInput) (Record, error)
	SetPendingAction(context.Context, string, string, string) error
	GetPendingAction(context.Context, string) (actionID, commandID string, err error)
	ClearPendingAction(context.Context, string) error
	StoreActionResult(context.Context, string, []byte) error
}

type EnrollmentTokenIssue struct {
	Token    string    `json:"token"`
	IssuedAt time.Time `json:"issued_at"`
}

type PendingBindingMetadata struct {
	Fingerprint  string     `json:"fingerprint"`
	FirstSeenAt  *time.Time `json:"first_seen_at,omitempty"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	AttemptCount int        `json:"attempt_count"`
}

type OnboardingState struct {
	Record
	Phase                            string                  `json:"phase"`
	HasHostSample                    bool                    `json:"has_host_sample"`
	HasAcceptedObservation           bool                    `json:"has_accepted_observation"`
	EnrollmentTokenIssuedAt          *time.Time              `json:"enrollment_token_issued_at,omitempty"`
	CurrentBindingFingerprintSummary string                  `json:"current_binding_fingerprint_summary,omitempty"`
	PendingBinding                   *PendingBindingMetadata `json:"pending_binding,omitempty"`
}

func MaskFingerprintSummary(fingerprint string) string {
	if fingerprint == "" {
		return ""
	}
	if len(fingerprint) <= 14 {
		return fingerprint
	}
	return fingerprint[:8] + "…" + fingerprint[len(fingerprint)-6:]
}

type OnboardingRepository interface {
	IssueNodeEnrollmentToken(context.Context, string) (EnrollmentTokenIssue, error)
	GetNodeOnboarding(context.Context, string) (OnboardingState, error)
	ConfirmNodeRebind(context.Context, string) (Record, error)
	RejectPendingFingerprint(context.Context, string) (Record, error)
	ResetNodeBinding(context.Context, string) (Record, error)
}

func IsValidLifecycleStatus(status string) bool {
	_, ok := allowedLifecycleStatuses[status]
	return ok
}

func DeriveOnboardingPhase(record Record, hasHostSample, hasAcceptedObservation bool) string {
	switch record.BindingStatus {
	case BindingPendingConfirmation:
		return OnboardingPhaseBindingConflict
	case BindingBound:
		if record.LastHeartbeatAt != nil && (hasHostSample || hasAcceptedObservation) {
			return OnboardingPhaseCompleted
		}
		return OnboardingPhaseBoundAwaitingObservation
	default:
		return OnboardingPhaseNotStarted
	}
}

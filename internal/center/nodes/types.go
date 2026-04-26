package nodes

import (
	"context"
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

var allowedLifecycleStatuses = map[string]struct{}{
	LifecyclePendingEnrollment: {},
	LifecycleInUse:             {},
	LifecycleObserving:         {},
	LifecycleNoRenewal:         {},
	LifecycleRetired:           {},
}

type Record struct {
	NodeID                     string     `json:"node_id"`
	DisplayName                string     `json:"display_name"`
	Region                     string     `json:"region"`
	City                       string     `json:"city"`
	Provider                   string     `json:"provider"`
	LifecycleStatus            string     `json:"lifecycle_status"`
	MonitoringStatus           string     `json:"monitoring_status"`
	BindingStatus              string     `json:"binding_status"`
	EnrollmentTokenHash        string     `json:"-"`
	EnrollmentTokenIssuedAt    *time.Time `json:"-"`
	SyncTokenHash              string     `json:"-"`
	BindingFingerprint         string     `json:"-"`
	PendingBindingFingerprint  string     `json:"-"`
	PendingBindingFirstSeenAt  *time.Time `json:"-"`
	PendingBindingLastSeenAt   *time.Time `json:"-"`
	PendingBindingAttemptCount int        `json:"-"`
	Labels                     []string   `json:"labels"`
	Note                       string     `json:"note"`
	CurrentHealthStatus        string     `json:"current_health_status"`
	LastHeartbeatAt            *time.Time `json:"last_heartbeat_at,omitempty"`
	LastSyncAt                 *time.Time `json:"last_sync_at,omitempty"`
	CurrentActiveIncidentCount int        `json:"current_active_incident_count"`
	CurrentPrimaryIssueSummary string     `json:"current_primary_issue_summary"`
	CreatedAt                  time.Time  `json:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}

type CreateInput struct {
	DisplayName     string   `json:"display_name"`
	Region          string   `json:"region"`
	City            string   `json:"city"`
	Provider        string   `json:"provider"`
	LifecycleStatus string   `json:"lifecycle_status"`
	Labels          []string `json:"labels"`
	Note            string   `json:"note"`
}

type Repository interface {
	ListNodes(context.Context) ([]Record, error)
	GetNode(context.Context, string) (Record, error)
	CreateNode(context.Context, CreateInput) (Record, error)
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
	Phase                   string                  `json:"phase"`
	HasHostSample           bool                    `json:"has_host_sample"`
	HasAcceptedObservation  bool                    `json:"has_accepted_observation"`
	EnrollmentTokenIssuedAt *time.Time              `json:"enrollment_token_issued_at,omitempty"`
	PendingBinding          *PendingBindingMetadata `json:"pending_binding,omitempty"`
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

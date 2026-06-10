package monitoringinstances

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	EnrollmentTokenTTL = 30 * time.Minute

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

var ErrMonitoringInstanceNotFound = errors.New("monitoring instance not found")
var ErrInvalidBindingTransition = errors.New("invalid binding transition")
var ErrMonitoringInstanceMetadataConflict = errors.New("monitoring instance metadata conflict")
var ErrInvalidManagementInput = errors.New("invalid monitoring instance management input")
var ErrManagementActionBlocked = errors.New("monitoring instance management action blocked")
var ErrArchivedMonitoringInstance = errors.New("archived monitoring instance")

var allowedLifecycleStatuses = map[string]struct{}{
	LifecyclePendingEnrollment: {},
	LifecycleInUse:             {},
	LifecycleObserving:         {},
	LifecycleNoRenewal:         {},
	LifecycleRetired:           {},
}

type ListScope string

const (
	ListScopeActive   ListScope = "active"
	ListScopeArchived ListScope = "archived"
	ListScopeAll      ListScope = "all"
)

// LastAction describes the queued, in-flight, or completed command action for
// the monitoring instance. It is nil when no action has ever been requested.
type LastAction struct {
	ActionID  string `json:"action_id"`
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
}

type Record struct {
	MonitoringInstanceID       string          `json:"monitoring_instance_id"`
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
	ArchivedAt                 *time.Time      `json:"archived_at,omitempty"`
	ArchivedReason             string          `json:"archived_reason,omitempty"`
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

type ManagementVPSLink struct {
	LinkID          string    `json:"link_id"`
	VPSID           string    `json:"vps_id"`
	DisplayName     string    `json:"display_name"`
	LifecycleStatus string    `json:"lifecycle_status"`
	UsageStatus     string    `json:"usage_status"`
	LinkedAt        time.Time `json:"linked_at"`
	Note            string    `json:"note"`
}

type ManagementCounts struct {
	HeartbeatCount                int `json:"heartbeat_count"`
	HostSampleCount               int `json:"host_sample_count"`
	ProbeObservationCount         int `json:"probe_observation_count"`
	HostSampleDailyAggregateCount int `json:"host_sample_daily_aggregate_count"`
	IPQualityReportCount          int `json:"ip_quality_report_count"`
	ActiveIncidentCount           int `json:"active_incident_count"`
	StateChangeEventCount         int `json:"state_change_event_count"`
	NotificationRecordCount       int `json:"notification_record_count"`
	AssetLifecycleActionStepCount int `json:"asset_lifecycle_action_step_count"`
	ActiveVPSLinkCount            int `json:"active_vps_link_count"`
}

func (c ManagementCounts) EvidenceCount() int {
	return c.HeartbeatCount +
		c.HostSampleCount +
		c.ProbeObservationCount +
		c.HostSampleDailyAggregateCount +
		c.IPQualityReportCount +
		c.ActiveIncidentCount +
		c.StateChangeEventCount +
		c.NotificationRecordCount +
		c.AssetLifecycleActionStepCount
}

type ManagementActions struct {
	CanRetire           bool `json:"can_retire"`
	CanRestoreLifecycle bool `json:"can_restore_lifecycle"`
	CanArchive          bool `json:"can_archive"`
	CanRestoreArchive   bool `json:"can_restore_archive"`
	CanPermanentCleanup bool `json:"can_permanent_cleanup"`
}

type ManagementReview struct {
	Record                Record              `json:"record"`
	ActiveVPSLinks        []ManagementVPSLink `json:"active_vps_links"`
	Counts                ManagementCounts    `json:"counts"`
	Warnings              []string            `json:"warnings"`
	Blockers              []string            `json:"blockers"`
	Actions               ManagementActions   `json:"actions"`
	EmptyMistakeCandidate bool                `json:"empty_mistake_candidate"`
}

type LifecycleActionInput struct {
	Reason string `json:"reason"`
}

type ArchiveInput struct {
	Reason           string `json:"reason"`
	ConfirmationName string `json:"confirmation_name"`
}

type PermanentCleanupInput struct {
	Reason           string `json:"reason"`
	ConfirmationName string `json:"confirmation_name"`
}

type PermanentCleanupResult struct {
	MonitoringInstanceID  string           `json:"monitoring_instance_id"`
	Counts                ManagementCounts `json:"counts"`
	DeletedReferenceCount int              `json:"deleted_reference_count"`
	Deleted               bool             `json:"deleted"`
}

type Repository interface {
	ListMonitoringInstances(context.Context, ...ListScope) ([]Record, error)
	GetMonitoringInstance(context.Context, string) (Record, error)
	CreateMonitoringInstance(context.Context, CreateInput) (Record, error)
	UpdateMonitoringInstanceMetadata(context.Context, string, UpdateMetadataInput) (Record, error)
	SetPendingAction(context.Context, string, string, string) error
	GetPendingAction(context.Context, string) (actionID, commandID string, err error)
	ClearPendingAction(context.Context, string) error
	StoreActionResult(context.Context, string, []byte) error
}

type EnrollmentTokenIssue struct {
	Token     string    `json:"token"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type InstallCommandIssue struct {
	Command       string    `json:"command"`
	IssuedAt      time.Time `json:"issued_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	InstallerURL  string    `json:"installer_url"`
	PublicBaseURL string    `json:"public_base_url"`
	AgentVersion  string    `json:"agent_version"`
	ReleaseRepo   string    `json:"release_repo"`
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
	IssueMonitoringInstanceEnrollmentToken(context.Context, string) (EnrollmentTokenIssue, error)
	GetMonitoringInstanceOnboarding(context.Context, string) (OnboardingState, error)
	ConfirmMonitoringInstanceRebind(context.Context, string) (Record, error)
	RejectPendingFingerprint(context.Context, string) (Record, error)
	ResetMonitoringInstanceBinding(context.Context, string) (Record, error)
}

func IsValidLifecycleStatus(status string) bool {
	_, ok := allowedLifecycleStatuses[status]
	return ok
}

func NormalizeListScope(scope ListScope) (ListScope, bool) {
	switch scope {
	case "", ListScopeActive:
		return ListScopeActive, true
	case ListScopeArchived:
		return ListScopeArchived, true
	case ListScopeAll:
		return ListScopeAll, true
	default:
		return "", false
	}
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

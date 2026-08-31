package incidents

import (
	"errors"
	"time"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/targets"
)

var (
	ErrIncidentProjectionConflict       = errors.New("incident projection conflict")
	ErrIncidentProjectionObjectNotFound = errors.New("incident projection object not found")
)

type IncidentClass string

type Severity string

type ObjectType string

type EventType string

type NotificationReason string

type NotificationChannel string

type DeliveryStatus string

type IncidentRecord struct {
	IncidentID      string
	ObjectType      ObjectType
	ObjectID        string
	IncidentClass   IncidentClass
	Severity        Severity
	StartedAt       time.Time
	LastEvaluatedAt time.Time
	Status          string
	SourceSummary   string
}

type StateChangeEventRecord struct {
	IncidentID          string        `json:"incident_id"`
	IncidentClass       IncidentClass `json:"incident_class"`
	ObjectType          ObjectType    `json:"object_type"`
	ObjectID            string        `json:"object_id"`
	EventType           EventType     `json:"event_type"`
	Severity            Severity      `json:"severity"`
	Summary             string        `json:"summary"`
	CreatedAt           time.Time     `json:"created_at"`
	IsBackfilled        bool          `json:"is_backfilled,omitempty"`
	Provenance          string        `json:"provenance,omitempty"`
	ProducerVersion     string        `json:"producer_version,omitempty"`
	RuleVersion         string        `json:"rule_version,omitempty"`
	PriorState          string        `json:"prior_state,omitempty"`
	ResultingState      string        `json:"resulting_state,omitempty"`
	CorrectionOfEventID string        `json:"correction_of_event_id,omitempty"`
}

type NotificationDecision struct {
	ShouldSend bool
	Channel    NotificationChannel
	Reason     NotificationReason
	Severity   Severity
	Summary    string
}

type DashboardOverview struct {
	SnapshotGeneratedAt                      time.Time                            `json:"snapshot_generated_at"`
	TotalMonitoringInstanceCount             int                                  `json:"total_monitoring_instance_count"`
	TotalTargetCount                         int                                  `json:"total_target_count"`
	AbnormalMonitoringInstanceCount          int                                  `json:"abnormal_monitoring_instance_count"`
	AbnormalTargetCount                      int                                  `json:"abnormal_target_count"`
	SevereMonitoringInstanceCount            int                                  `json:"severe_monitoring_instance_count"`
	SevereTargetCount                        int                                  `json:"severe_target_count"`
	MaintenanceMonitoringInstanceCount       int                                  `json:"maintenance_monitoring_instance_count"`
	MaintenanceTargetCount                   int                                  `json:"maintenance_target_count"`
	PendingOnboardingMonitoringInstanceCount int                                  `json:"pending_onboarding_monitoring_instance_count"`
	PausedMonitoringInstanceCount            int                                  `json:"paused_monitoring_instance_count"`
	RetiredMonitoringInstanceCount           int                                  `json:"retired_monitoring_instance_count"`
	PausedTargetCount                        int                                  `json:"paused_target_count"`
	ArchivedTargetCount                      int                                  `json:"archived_target_count"`
	RecentNewIncidentCount                   int                                  `json:"recent_new_incident_count"`
	RecentRecoveryCount                      int                                  `json:"recent_recovery_count"`
	GroupSummaries                           []DashboardGroupSummary              `json:"group_summaries"`
	NotificationStatus                       DashboardNotificationStatus          `json:"notification_status"`
	AssetSummary                             DashboardAssetSummary                `json:"asset_summary"`
	AbnormalMonitoringInstances              []DashboardMonitoringInstanceSummary `json:"abnormal_monitoring_instances"`
	AbnormalTargets                          []DashboardTargetSummary             `json:"abnormal_targets"`
	RecentEvents                             []StateChangeEventRecord             `json:"recent_events"`
	// NewIncidentTrend24h is a 24-element array of per-hour incident_started
	// counts. Index 0 is 23 hours ago, index 23 is the current hour. Frontend
	// uses this to render the dashboard "新增异常" sparkline.
	NewIncidentTrend24h []int `json:"new_incident_trend_24h,omitempty"`
	// RecoveryTrend24h is a 24-element array of per-hour incident_recovered
	// counts. Same indexing as NewIncidentTrend24h.
	RecoveryTrend24h []int `json:"recovery_trend_24h,omitempty"`
}

type DashboardGroupSummary struct {
	Group                              string `json:"group"`
	MonitoringInstanceCount            int    `json:"monitoring_instance_count"`
	TargetCount                        int    `json:"target_count"`
	AbnormalMonitoringInstanceCount    int    `json:"abnormal_monitoring_instance_count"`
	AbnormalTargetCount                int    `json:"abnormal_target_count"`
	SevereMonitoringInstanceCount      int    `json:"severe_monitoring_instance_count"`
	SevereTargetCount                  int    `json:"severe_target_count"`
	MaintenanceMonitoringInstanceCount int    `json:"maintenance_monitoring_instance_count"`
	MaintenanceTargetCount             int    `json:"maintenance_target_count"`
}

type DashboardNotificationStatus struct {
	TelegramConfigured         bool `json:"telegram_configured"`
	TelegramRuntimeManaged     bool `json:"telegram_runtime_managed"`
	TelegramRuntimeApplyActive bool `json:"telegram_runtime_apply_active"`
	FeishuConfigured           bool `json:"feishu_configured"`
}

type DashboardAssetSummary struct {
	RenewalDue30dSubscriptionCount int                            `json:"renewal_due_30d_subscription_count"`
	RenewalDue30dVPSCount          int                            `json:"renewal_due_30d_vps_count"`
	UnreviewedVPSCount             int                            `json:"unreviewed_vps_count"`
	ToCancelVPSCount               int                            `json:"to_cancel_vps_count"`
	CancelledVPSCount              int                            `json:"cancelled_vps_count"`
	CancellationAttentionVPSCount  int                            `json:"cancellation_attention_vps_count"`
	RunningCancelledAssetCount     int                            `json:"running_cancelled_asset_count"`
	ToMigrateVPSCount              int                            `json:"to_migrate_vps_count"`
	UnlinkedVPSCount               int                            `json:"unlinked_vps_count"`
	AbnormalLinkedVPSCount         int                            `json:"abnormal_linked_vps_count"`
	CostByCurrency                 []DashboardAssetCostByCurrency `json:"cost_by_currency"`
}

type DashboardAssetCostByCurrency struct {
	Currency     string  `json:"currency"`
	MonthlyTotal float64 `json:"monthly_total"`
	YearlyTotal  float64 `json:"yearly_total"`
}

type DashboardMonitoringInstanceSummary struct {
	MonitoringInstanceID       string     `json:"monitoring_instance_id"`
	DisplayName                string     `json:"display_name"`
	Group                      string     `json:"group"`
	Region                     string     `json:"region"`
	City                       string     `json:"city"`
	Provider                   string     `json:"provider"`
	LifecycleStatus            string     `json:"lifecycle_status"`
	MonitoringStatus           string     `json:"monitoring_status"`
	CurrentHealthStatus        string     `json:"current_health_status"`
	LastHeartbeatAt            *time.Time `json:"last_heartbeat_at,omitempty"`
	CurrentActiveIncidentCount int        `json:"current_active_incident_count"`
	CurrentPrimaryIssueSummary string     `json:"current_primary_issue_summary"`
}

type DashboardTargetSummary struct {
	TargetID                   string     `json:"target_id"`
	Name                       string     `json:"name"`
	TargetType                 string     `json:"target_type"`
	Host                       string     `json:"host"`
	BasePort                   *int       `json:"base_port,omitempty"`
	RunStatus                  string     `json:"run_status"`
	Group                      string     `json:"group"`
	CurrentHealthStatus        string     `json:"current_health_status"`
	LastSuccessAt              *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt              *time.Time `json:"last_failure_at,omitempty"`
	CurrentActiveIncidentCount int        `json:"current_active_incident_count"`
	CurrentPrimaryIssueSummary string     `json:"current_primary_issue_summary"`
}

type NotificationRecordWrite struct {
	IncidentID     string
	ObjectType     ObjectType
	ObjectID       string
	Channel        NotificationChannel
	DeliveryStatus DeliveryStatus
	Summary        string
	SentAt         *time.Time
}

type IncidentMutation struct {
	ObjectType               ObjectType
	ObjectID                 string
	ExpectedObjectRowVersion string
	Active                   []IncidentRecord
	Events                   []StateChangeEventRecord
}

type MonitoringInstanceResourceSample struct {
	ObservedAt         time.Time
	ReceivedAt         time.Time
	CPUUsagePct        float64
	NormalizedLoad5    float64
	MemUsedPct         float64
	MemAvailableBytes  int64
	SwapUsedPct        float64
	CPUIOWaitPct       float64
	CPUStealPct        float64
	MaintenanceContext bool
	IsBackfilled       bool
}

// HeartbeatIncidentPolicy is the complete policy required to evaluate a
// monitoring-instance heartbeat incident. Callers must resolve it from the
// current settings snapshot instead of relying on evaluator defaults.
type HeartbeatIncidentPolicy struct {
	HeartbeatInterval      time.Duration
	MissingThreshold       int
	RecoverySuccesses      int
	RecoveryMaxIntervalGap time.Duration
}

// LiveHeartbeatReceipt is the minimal, server-owned recovery evidence for a
// heartbeat incident. The persistence reader guarantees that these receipts
// are non-backfilled and ordered by ReceivedAt descending.
type LiveHeartbeatReceipt struct {
	SyncBatchID string
	ReceivedAt  time.Time
}

type EvaluationTransition string

type EvaluationResult struct {
	Current      *IncidentRecord
	Transition   EvaluationTransition
	Event        *StateChangeEventRecord
	Notification *NotificationDecision
}

type MonitoringInstanceHostDailyAggregate struct {
	BucketDate             time.Time
	SampleCount            int
	AvgLoad5               float64
	AvgCPUIOWaitPct        float64
	AvgCPUStealPct         float64
	BackfilledSampleCount  int
	MaintenanceSampleCount int
}

type TargetProbeDailyAggregate struct {
	TargetID                    string
	ProbeItemID                 string
	BucketDate                  time.Time
	ObservationCount            int
	SuccessCount                int
	AvgLatencyMS                *float64
	P95LatencyMS                *float64
	BackfilledObservationCount  int
	MaintenanceObservationCount int
}

const (
	IncidentMonitoringInstanceHeartbeatMissing IncidentClass = "monitoring_instance_heartbeat_missing"
	IncidentMonitoringInstanceDiskPressure     IncidentClass = "monitoring_instance_disk_pressure"
	IncidentMonitoringInstanceInodePressure    IncidentClass = "monitoring_instance_inode_pressure"
	IncidentMonitoringInstanceResourcePressure IncidentClass = "monitoring_instance_resource_pressure"
	IncidentMonitoringInstanceTrendDegradation IncidentClass = "monitoring_instance_trend_degradation"
	IncidentTargetProbeFailure                 IncidentClass = "target_probe_failure"
	IncidentTargetTLSExpiry                    IncidentClass = "target_tls_expiry"
	IncidentTargetLatencyTrendDegradation      IncidentClass = "target_latency_trend_degradation"
)

const (
	SeverityNormal   Severity = "正常"
	SeverityNotice   Severity = "关注"
	SeverityAlert    Severity = "告警"
	SeverityCritical Severity = "严重"
)

const (
	ObjectTypeMonitoringInstance ObjectType = "monitoring_instance"
	ObjectTypeTarget             ObjectType = "target"
	ObjectTypeSubscription       ObjectType = "subscription"
	ObjectTypeVPS                ObjectType = "vps"
)

const (
	EventIncidentStarted                                EventType = "incident_started"
	EventIncidentEscalated                              EventType = "incident_escalated"
	EventIncidentRecovered                              EventType = "incident_recovered"
	EventMonitoringInstanceBindingRebindConfirmed       EventType = "monitoring_instance_binding_rebind_confirmed"
	EventMonitoringInstanceBindingPendingRejected       EventType = "monitoring_instance_binding_pending_rejected"
	EventMonitoringInstanceBindingReset                 EventType = "monitoring_instance_binding_reset"
	EventMonitoringInstanceMonitoringMaintenanceEntered EventType = "monitoring_instance_monitoring_maintenance_entered"
	EventMonitoringInstanceMonitoringMaintenanceExited  EventType = "monitoring_instance_monitoring_maintenance_exited"
	EventMonitoringInstanceMonitoringPaused             EventType = "monitoring_instance_monitoring_paused"
	EventMonitoringInstanceMonitoringResumed            EventType = "monitoring_instance_monitoring_resumed"
	EventMonitoringInstanceLifecycleUpdated             EventType = "monitoring_instance_lifecycle_updated"
	EventMonitoringInstanceRetired                      EventType = "monitoring_instance_retired"
	EventMonitoringInstanceRestoredToObserving          EventType = "monitoring_instance_restored_to_observing"
	EventTargetMaintenanceEntered                       EventType = "target_maintenance_entered"
	EventTargetMaintenanceExited                        EventType = "target_maintenance_exited"
	EventTargetPaused                                   EventType = "target_paused"
	EventTargetResumed                                  EventType = "target_resumed"
	EventTargetArchived                                 EventType = "target_archived"
	EventTargetRestoredToPaused                         EventType = "target_restored_to_paused"
	EventCorrected                                      EventType = "event_corrected"
)

const (
	MonitoringEventProvenanceAgentSync         = "agent_sync"
	MonitoringEventProvenanceCenter            = "center"
	MonitoringEventProvenanceWeb               = "web"
	MonitoringEventProvenanceRetentionBackfill = "retention_backfill"
	MonitoringEventProvenanceManualCorrection  = "manual_correction"

	MonitoringEventProducerVersion       = "center-monitoring-events/v1"
	MonitoringEventIncidentRuleVersion   = "incident-rules/v1"
	MonitoringEventBindingRuleVersion    = "monitoring-binding-rules/v1"
	MonitoringEventLifecycleRuleVersion  = "monitoring-lifecycle-rules/v1"
	MonitoringEventRuntimeRuleVersion    = "monitoring-runtime-rules/v1"
	MonitoringEventTargetRuleVersion     = "target-runtime-rules/v1"
	MonitoringEventEvidenceSourceVersion = "state-change-events/v2"
)

// ValidMonitoringEventMetadata is the closed semantic contract shared by
// persistence writers and the monitoring-event evidence adapter. It rejects
// new event, rule, state, provenance, and correction semantics until they are
// deliberately added here.
func ValidMonitoringEventMetadata(
	objectType ObjectType,
	eventType EventType,
	severity Severity,
	isBackfilled bool,
	provenance string,
	producerVersion string,
	ruleVersion string,
	priorState string,
	resultingState string,
	correctionOfEventID string,
) bool {
	switch provenance {
	case MonitoringEventProvenanceAgentSync,
		MonitoringEventProvenanceCenter,
		MonitoringEventProvenanceWeb,
		MonitoringEventProvenanceRetentionBackfill,
		MonitoringEventProvenanceManualCorrection:
	default:
		return false
	}
	if producerVersion != MonitoringEventProducerVersion || (provenance == MonitoringEventProvenanceRetentionBackfill && !isBackfilled) {
		return false
	}
	if eventType == EventCorrected {
		if provenance != MonitoringEventProvenanceManualCorrection || correctionOfEventID == "" {
			return false
		}
	} else if provenance == MonitoringEventProvenanceManualCorrection || correctionOfEventID != "" {
		return false
	}

	states := map[string]struct{}{}
	validEventType := false
	switch ruleVersion {
	case MonitoringEventIncidentRuleVersion:
		if objectType != ObjectTypeMonitoringInstance && objectType != ObjectTypeTarget {
			return false
		}
		validEventType = eventType == EventIncidentStarted || eventType == EventIncidentEscalated || eventType == EventIncidentRecovered || eventType == EventCorrected
		states = map[string]struct{}{"normal": {}, "notice": {}, "alert": {}, "critical": {}}
	case MonitoringEventBindingRuleVersion:
		if objectType != ObjectTypeMonitoringInstance {
			return false
		}
		validEventType = eventType == EventMonitoringInstanceBindingRebindConfirmed || eventType == EventMonitoringInstanceBindingPendingRejected || eventType == EventMonitoringInstanceBindingReset || eventType == EventCorrected
		states = map[string]struct{}{monitoringinstances.BindingUnbound: {}, monitoringinstances.BindingBound: {}, monitoringinstances.BindingPendingConfirmation: {}}
	case MonitoringEventLifecycleRuleVersion:
		if objectType != ObjectTypeMonitoringInstance {
			return false
		}
		validEventType = eventType == EventMonitoringInstanceLifecycleUpdated || eventType == EventMonitoringInstanceRetired || eventType == EventMonitoringInstanceRestoredToObserving || eventType == EventCorrected
		states = map[string]struct{}{
			monitoringinstances.LifecyclePendingEnrollment: {},
			monitoringinstances.LifecycleInUse:             {},
			monitoringinstances.LifecycleObserving:         {},
			monitoringinstances.LifecycleNoRenewal:         {},
			monitoringinstances.LifecycleRetired:           {},
			"unarchived":                                   {},
			"archived":                                     {},
		}
	case MonitoringEventRuntimeRuleVersion:
		if objectType != ObjectTypeMonitoringInstance {
			return false
		}
		validEventType = eventType == EventMonitoringInstanceMonitoringMaintenanceEntered || eventType == EventMonitoringInstanceMonitoringMaintenanceExited || eventType == EventMonitoringInstanceMonitoringPaused || eventType == EventMonitoringInstanceMonitoringResumed || eventType == EventCorrected
		states = map[string]struct{}{monitoringinstances.MonitoringEnabled: {}, monitoringinstances.MonitoringMaintenance: {}, monitoringinstances.MonitoringPaused: {}}
	case MonitoringEventTargetRuleVersion:
		if objectType != ObjectTypeTarget {
			return false
		}
		validEventType = eventType == EventTargetMaintenanceEntered || eventType == EventTargetMaintenanceExited || eventType == EventTargetPaused || eventType == EventTargetResumed || eventType == EventTargetArchived || eventType == EventTargetRestoredToPaused || eventType == EventCorrected
		states = map[string]struct{}{targets.RunStatusEnabled: {}, targets.RunStatusMaintenance: {}, targets.RunStatusPaused: {}, targets.RunStatusArchived: {}}
	default:
		return false
	}
	if !validEventType {
		return false
	}
	_, validPrior := states[priorState]
	_, validResult := states[resultingState]
	if !validPrior || !validResult {
		return false
	}
	if ruleVersion == MonitoringEventLifecycleRuleVersion && !validMonitoringEventLifecycleStateDomain(priorState, resultingState) {
		return false
	}
	if eventType == EventCorrected {
		if ruleVersion == MonitoringEventIncidentRuleVersion {
			return monitoringEventIncidentState(severity) == resultingState
		}
		return severity == ""
	}

	switch ruleVersion {
	case MonitoringEventIncidentRuleVersion:
		switch eventType {
		case EventIncidentStarted:
			return priorState == "normal" && resultingState != "normal" && monitoringEventIncidentState(severity) == resultingState
		case EventIncidentEscalated:
			return monitoringEventIncidentStateRank(resultingState) > monitoringEventIncidentStateRank(priorState) && monitoringEventIncidentState(severity) == resultingState
		case EventIncidentRecovered:
			return priorState != "normal" && resultingState == "normal" && monitoringEventIncidentState(severity) == priorState
		}
	case MonitoringEventBindingRuleVersion:
		if severity != "" {
			return false
		}
		switch eventType {
		case EventMonitoringInstanceBindingRebindConfirmed, EventMonitoringInstanceBindingPendingRejected:
			return priorState == monitoringinstances.BindingPendingConfirmation && resultingState == monitoringinstances.BindingBound
		case EventMonitoringInstanceBindingReset:
			return resultingState == monitoringinstances.BindingUnbound
		}
	case MonitoringEventLifecycleRuleVersion:
		if severity != "" {
			return false
		}
		switch eventType {
		case EventMonitoringInstanceLifecycleUpdated:
			return priorState != resultingState
		case EventMonitoringInstanceRetired:
			return resultingState == monitoringinstances.LifecycleRetired
		case EventMonitoringInstanceRestoredToObserving:
			return priorState == monitoringinstances.LifecycleRetired && resultingState == monitoringinstances.LifecycleObserving
		}
	case MonitoringEventRuntimeRuleVersion:
		if severity != "" {
			return false
		}
		switch eventType {
		case EventMonitoringInstanceMonitoringMaintenanceEntered:
			return priorState == monitoringinstances.MonitoringEnabled && resultingState == monitoringinstances.MonitoringMaintenance
		case EventMonitoringInstanceMonitoringMaintenanceExited:
			return priorState == monitoringinstances.MonitoringMaintenance && resultingState == monitoringinstances.MonitoringEnabled
		case EventMonitoringInstanceMonitoringPaused:
			return (priorState == monitoringinstances.MonitoringEnabled || priorState == monitoringinstances.MonitoringMaintenance) && resultingState == monitoringinstances.MonitoringPaused
		case EventMonitoringInstanceMonitoringResumed:
			return priorState == monitoringinstances.MonitoringPaused && resultingState == monitoringinstances.MonitoringEnabled
		}
	case MonitoringEventTargetRuleVersion:
		if severity != "" {
			return false
		}
		switch eventType {
		case EventTargetMaintenanceEntered:
			return priorState == targets.RunStatusEnabled && resultingState == targets.RunStatusMaintenance
		case EventTargetMaintenanceExited:
			return priorState == targets.RunStatusMaintenance && resultingState == targets.RunStatusEnabled
		case EventTargetPaused:
			return (priorState == targets.RunStatusEnabled || priorState == targets.RunStatusMaintenance) && resultingState == targets.RunStatusPaused
		case EventTargetResumed:
			return priorState == targets.RunStatusPaused && resultingState == targets.RunStatusEnabled
		case EventTargetArchived:
			return priorState != targets.RunStatusArchived && resultingState == targets.RunStatusArchived
		case EventTargetRestoredToPaused:
			return priorState == targets.RunStatusArchived && resultingState == targets.RunStatusPaused
		}
	}
	return false
}

func validMonitoringEventLifecycleStateDomain(priorState, resultingState string) bool {
	priorIsArchiveMarker := priorState == "unarchived" || priorState == "archived"
	resultIsArchiveMarker := resultingState == "unarchived" || resultingState == "archived"
	if !priorIsArchiveMarker && !resultIsArchiveMarker {
		return true
	}
	return priorState == "unarchived" && resultingState == "archived"
}

func monitoringEventIncidentStateRank(state string) int {
	switch state {
	case "normal":
		return 1
	case "notice":
		return 2
	case "alert":
		return 3
	case "critical":
		return 4
	default:
		return 0
	}
}

const (
	NotificationReasonStarted   NotificationReason = "started"
	NotificationReasonEscalated NotificationReason = "escalated"
	NotificationReasonRecovered NotificationReason = "recovered"
)

const (
	NotificationChannelTelegram NotificationChannel = "telegram"
	NotificationChannelFeishu   NotificationChannel = "feishu"
)

const (
	DeliveryStatusSent       DeliveryStatus = "sent"
	DeliveryStatusSuppressed DeliveryStatus = "suppressed"
	DeliveryStatusFailed     DeliveryStatus = "failed"
)

const (
	IncidentStatusActive = "active"
)

const (
	TransitionNoop      EvaluationTransition = "noop"
	TransitionStarted   EvaluationTransition = "started"
	TransitionEscalated EvaluationTransition = "escalated"
	TransitionRecovered EvaluationTransition = "recovered"
	TransitionSkipped   EvaluationTransition = "skipped"
)

// MetricThresholds holds the per-metric percentage thresholds used by the
// incident evaluator. Zero values are treated as "use defaults".
type MetricThresholds struct {
	CPUWarningPct     int
	CPUAlertPct       int
	CPUCriticalPct    int
	MemWarningPct     int
	MemAlertPct       int
	MemCriticalPct    int
	DiskWarningPct    int
	DiskAlertPct      int
	DiskCriticalPct   int
	InodeWarningPct   int
	InodeAlertPct     int
	InodeCriticalPct  int
	IOWaitWarningPct  int
	IOWaitAlertPct    int
	IOWaitCriticalPct int
	Load5Warning      float64
	Load5Alert        float64
	Load5Critical     float64
}

// DefaultMetricThresholds returns thresholds matching the original hardcoded
// evaluator values.
func DefaultMetricThresholds() MetricThresholds {
	return MetricThresholds{
		CPUWarningPct:     80,
		CPUAlertPct:       90,
		CPUCriticalPct:    95,
		MemWarningPct:     85,
		MemAlertPct:       92,
		MemCriticalPct:    95,
		DiskWarningPct:    85,
		DiskAlertPct:      92,
		DiskCriticalPct:   97,
		InodeWarningPct:   80,
		InodeAlertPct:     90,
		InodeCriticalPct:  95,
		IOWaitWarningPct:  20,
		IOWaitAlertPct:    35,
		IOWaitCriticalPct: 50,
		Load5Warning:      4.0,
		Load5Alert:        6.0,
		Load5Critical:     8.0,
	}
}

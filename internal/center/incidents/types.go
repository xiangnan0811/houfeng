package incidents

import "time"

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
	IncidentID    string        `json:"incident_id"`
	IncidentClass IncidentClass `json:"incident_class"`
	ObjectType    ObjectType    `json:"object_type"`
	ObjectID      string        `json:"object_id"`
	EventType     EventType     `json:"event_type"`
	Severity      Severity      `json:"severity"`
	Summary       string        `json:"summary"`
	CreatedAt     time.Time     `json:"created_at"`
}

type NotificationDecision struct {
	ShouldSend bool
	Channel    NotificationChannel
	Reason     NotificationReason
	Severity   Severity
	Summary    string
}

type DashboardOverview struct {
	SnapshotGeneratedAt        time.Time                   `json:"snapshot_generated_at"`
	TotalNodeCount             int                         `json:"total_node_count"`
	TotalTargetCount           int                         `json:"total_target_count"`
	AbnormalNodeCount          int                         `json:"abnormal_node_count"`
	AbnormalTargetCount        int                         `json:"abnormal_target_count"`
	SevereNodeCount            int                         `json:"severe_node_count"`
	SevereTargetCount          int                         `json:"severe_target_count"`
	MaintenanceNodeCount       int                         `json:"maintenance_node_count"`
	MaintenanceTargetCount     int                         `json:"maintenance_target_count"`
	PendingOnboardingNodeCount int                         `json:"pending_onboarding_node_count"`
	PausedNodeCount            int                         `json:"paused_node_count"`
	RetiredNodeCount           int                         `json:"retired_node_count"`
	PausedTargetCount          int                         `json:"paused_target_count"`
	ArchivedTargetCount        int                         `json:"archived_target_count"`
	RecentNewIncidentCount     int                         `json:"recent_new_incident_count"`
	RecentRecoveryCount        int                         `json:"recent_recovery_count"`
	GroupSummaries             []DashboardGroupSummary     `json:"group_summaries"`
	NotificationStatus         DashboardNotificationStatus `json:"notification_status"`
	AssetSummary               DashboardAssetSummary       `json:"asset_summary"`
	AbnormalNodes              []DashboardNodeSummary      `json:"abnormal_nodes"`
	AbnormalTargets            []DashboardTargetSummary    `json:"abnormal_targets"`
	RecentEvents               []StateChangeEventRecord    `json:"recent_events"`
	// NewIncidentTrend24h is a 24-element array of per-hour incident_started
	// counts. Index 0 is 23 hours ago, index 23 is the current hour. Frontend
	// uses this to render the dashboard "新增异常" sparkline.
	NewIncidentTrend24h []int `json:"new_incident_trend_24h,omitempty"`
	// RecoveryTrend24h is a 24-element array of per-hour incident_recovered
	// counts. Same indexing as NewIncidentTrend24h.
	RecoveryTrend24h []int `json:"recovery_trend_24h,omitempty"`
}

type DashboardGroupSummary struct {
	Group                  string `json:"group"`
	NodeCount              int    `json:"node_count"`
	TargetCount            int    `json:"target_count"`
	AbnormalNodeCount      int    `json:"abnormal_node_count"`
	AbnormalTargetCount    int    `json:"abnormal_target_count"`
	SevereNodeCount        int    `json:"severe_node_count"`
	SevereTargetCount      int    `json:"severe_target_count"`
	MaintenanceNodeCount   int    `json:"maintenance_node_count"`
	MaintenanceTargetCount int    `json:"maintenance_target_count"`
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

type DashboardNodeSummary struct {
	NodeID                     string     `json:"node_id"`
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
	ObjectType    ObjectType
	ObjectID      string
	Active        []IncidentRecord
	Events        []StateChangeEventRecord
	Notifications []NotificationRecordWrite
}

type NodeResourceSample struct {
	ObservedAt         time.Time
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

type EvaluationTransition string

type EvaluationResult struct {
	Current      *IncidentRecord
	Transition   EvaluationTransition
	Event        *StateChangeEventRecord
	Notification *NotificationDecision
}

type NodeHostDailyAggregate struct {
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
	IncidentNodeHeartbeatMissing          IncidentClass = "node_heartbeat_missing"
	IncidentNodeDiskPressure              IncidentClass = "node_disk_pressure"
	IncidentNodeInodePressure             IncidentClass = "node_inode_pressure"
	IncidentNodeResourcePressure          IncidentClass = "node_resource_pressure"
	IncidentNodeTrendDegradation          IncidentClass = "node_trend_degradation"
	IncidentTargetProbeFailure            IncidentClass = "target_probe_failure"
	IncidentTargetTLSExpiry               IncidentClass = "target_tls_expiry"
	IncidentTargetLatencyTrendDegradation IncidentClass = "target_latency_trend_degradation"
)

const (
	SeverityNormal   Severity = "正常"
	SeverityNotice   Severity = "关注"
	SeverityAlert    Severity = "告警"
	SeverityCritical Severity = "严重"
)

const (
	ObjectTypeNode   ObjectType = "node"
	ObjectTypeTarget ObjectType = "target"
)

const (
	EventIncidentStarted                  EventType = "incident_started"
	EventIncidentEscalated                EventType = "incident_escalated"
	EventIncidentRecovered                EventType = "incident_recovered"
	EventNodeBindingRebindConfirmed       EventType = "node_binding_rebind_confirmed"
	EventNodeBindingPendingRejected       EventType = "node_binding_pending_rejected"
	EventNodeBindingReset                 EventType = "node_binding_reset"
	EventNodeMonitoringMaintenanceEntered EventType = "node_monitoring_maintenance_entered"
	EventNodeMonitoringMaintenanceExited  EventType = "node_monitoring_maintenance_exited"
	EventNodeMonitoringPaused             EventType = "node_monitoring_paused"
	EventNodeMonitoringResumed            EventType = "node_monitoring_resumed"
	EventNodeRetired                      EventType = "node_retired"
	EventNodeRestoredToObserving          EventType = "node_restored_to_observing"
	EventTargetMaintenanceEntered         EventType = "target_maintenance_entered"
	EventTargetMaintenanceExited          EventType = "target_maintenance_exited"
	EventTargetPaused                     EventType = "target_paused"
	EventTargetResumed                    EventType = "target_resumed"
	EventTargetArchived                   EventType = "target_archived"
	EventTargetRestoredToPaused           EventType = "target_restored_to_paused"
)

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
	CPUWarningPct    int
	CPUAlertPct      int
	CPUCriticalPct   int
	MemWarningPct    int
	MemAlertPct      int
	MemCriticalPct   int
	DiskWarningPct   int
	DiskAlertPct     int
	DiskCriticalPct  int
	InodeWarningPct  int
	InodeAlertPct    int
	InodeCriticalPct int
}

// DefaultMetricThresholds returns thresholds matching the original hardcoded
// evaluator values.
func DefaultMetricThresholds() MetricThresholds {
	return MetricThresholds{
		CPUWarningPct:    80,
		CPUAlertPct:      90,
		CPUCriticalPct:   95,
		MemWarningPct:    85,
		MemAlertPct:      92,
		MemCriticalPct:   95,
		DiskWarningPct:   85,
		DiskAlertPct:     92,
		DiskCriticalPct:  97,
		InodeWarningPct:  80,
		InodeAlertPct:    90,
		InodeCriticalPct: 95,
	}
}

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
	ObjectType    ObjectType
	ObjectID      string
	Active        []IncidentRecord
	Events        []StateChangeEventRecord
	Notifications []NotificationRecordWrite
}

type MonitoringInstanceResourceSample struct {
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

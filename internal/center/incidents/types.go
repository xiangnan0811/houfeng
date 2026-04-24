package incidents

import "time"

type IncidentClass string

type Severity string

type ObjectType string

type EventType string

type NotificationReason string

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
	IncidentID    string
	IncidentClass IncidentClass
	ObjectType    ObjectType
	ObjectID      string
	EventType     EventType
	Severity      Severity
	Summary       string
	CreatedAt     time.Time
}

type NotificationDecision struct {
	ShouldSend bool
	Channel    string
	Reason     NotificationReason
	Severity   Severity
	Summary    string
}

type DashboardOverview struct {
	AbnormalNodeCount      int
	AbnormalTargetCount    int
	SevereNodeCount        int
	SevereTargetCount      int
	MaintenanceNodeCount   int
	MaintenanceTargetCount int
	RecentNewIncidentCount int
	RecentRecoveryCount    int
	RecentEvents           []StateChangeEventRecord
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

const (
	IncidentNodeHeartbeatMissing IncidentClass = "node_heartbeat_missing"
	IncidentNodeDiskPressure     IncidentClass = "node_disk_pressure"
	IncidentNodeInodePressure    IncidentClass = "node_inode_pressure"
	IncidentNodeResourcePressure IncidentClass = "node_resource_pressure"
	IncidentTargetProbeFailure   IncidentClass = "target_probe_failure"
	IncidentTargetTLSExpiry      IncidentClass = "target_tls_expiry"
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
	EventIncidentStarted   EventType = "incident_started"
	EventIncidentEscalated EventType = "incident_escalated"
	EventIncidentRecovered EventType = "incident_recovered"
)

const (
	NotificationReasonStarted   NotificationReason = "started"
	NotificationReasonEscalated NotificationReason = "escalated"
	NotificationReasonRecovered NotificationReason = "recovered"
)

const (
	DeliveryStatusSent       DeliveryStatus = "sent"
	DeliveryStatusSuppressed DeliveryStatus = "suppressed"
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

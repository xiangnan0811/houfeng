package vpsoverview

import (
	"encoding/json"
	"time"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/records"
)

const (
	SectionReady       = "ready"
	SectionStale       = "stale"
	SectionUnavailable = "unavailable"

	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityNotice   = "notice"
	SeverityInfo     = "info"

	RecentActivityLimit  = 5
	DefaultSectionBudget = 800 * time.Millisecond

	CapabilityRecordsV2Read = "records_v2_read"
)

// Overview is the request-scoped VPS detail read model.
type Overview struct {
	GeneratedAt    time.Time         `json:"generated_at"`
	Identity       Identity          `json:"identity"`
	Anomalies      []Anomaly         `json:"anomalies"`
	Summary        Summary           `json:"summary"`
	RecentActivity ActivitySection   `json:"recent_activity"`
	Facts          []Fact            `json:"facts"`
	Relations      []RelationSummary `json:"relations"`
	Capabilities   []string          `json:"capabilities"`
}

// MarshalJSON keeps every collection an array so empty overviews never force
// clients to special-case null slices.
func (overview Overview) MarshalJSON() ([]byte, error) {
	type wire Overview
	clone := wire(overview)
	if clone.Anomalies == nil {
		clone.Anomalies = []Anomaly{}
	}
	if clone.Facts == nil {
		clone.Facts = []Fact{}
	}
	if clone.Relations == nil {
		clone.Relations = []RelationSummary{}
	}
	if clone.Capabilities == nil {
		clone.Capabilities = []string{}
	}
	if clone.RecentActivity.Items == nil {
		clone.RecentActivity.Items = []activity.Event{}
	}
	return json.Marshal(clone)
}

// Identity is the fatal section: missing or unauthorized VPS collapses the whole
// response to 404 rather than a partial shell.
type Identity struct {
	VPSID           string    `json:"vps_id"`
	DisplayName     string    `json:"display_name"`
	ProviderName    string    `json:"provider_name"`
	ProductName     string    `json:"product_name"`
	Country         string    `json:"country"`
	Region          string    `json:"region"`
	City            string    `json:"city"`
	Datacenter      string    `json:"datacenter"`
	IPv4            string    `json:"ipv4"`
	IPv6            string    `json:"ipv6"`
	LifecycleStatus string    `json:"lifecycle_status"`
	UsageStatus     string    `json:"usage_status"`
	RenewalDecision string    `json:"renewal_decision"`
	Importance      string    `json:"importance"`
	Labels          []string  `json:"labels"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// SectionState is the safe per-section freshness envelope. It never carries
// global projector checkpoints or sequences.
type SectionState struct {
	State         string     `json:"state"`
	ObservedAt    *time.Time `json:"observed_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	ReasonCode    string     `json:"reason_code"`
}

// Summary is the 2×2 decision surface. Status fields here are derived; Facts
// must not repeat them.
type Summary struct {
	Overall    SummaryCell `json:"overall"`
	Monitoring SummaryCell `json:"monitoring"`
	IPQuality  SummaryCell `json:"ip_quality"`
	Renewal    SummaryCell `json:"renewal"`
}

// SummaryCell is one tile: a short status plus the section's own freshness.
type SummaryCell struct {
	Status  string       `json:"status"`
	Detail  string       `json:"detail,omitempty"`
	Section SectionState `json:"section"`
}

// ActivitySection is recent activity for this VPS, produced by the same activity
// service as the subject timeline (limit 5, fixed first-page watermark).
type ActivitySection struct {
	Section  SectionState     `json:"section"`
	Items    []activity.Event `json:"items"`
	Snapshot string           `json:"snapshot_cursor,omitempty"`
}

// Fact is stable identity/config. It must not duplicate Summary status fields.
type Fact struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// RelationSummary is a count + route card for linked resources.
type RelationSummary struct {
	Kind    string       `json:"kind"`
	Count   int          `json:"count"`
	Status  string       `json:"status,omitempty"`
	Route   string       `json:"route,omitempty"`
	Label   string       `json:"label"`
	Section SectionState `json:"section"`
}

// Anomaly is one versioned rule hit. Empty overview.Anomalies means healthy —
// there is deliberately no healthy_placeholder rule.
type Anomaly struct {
	RuleID      string          `json:"rule_id"`
	Severity    string          `json:"severity"`
	Title       string          `json:"title"`
	Detail      string          `json:"detail,omitempty"`
	Source      string          `json:"source"`
	EventAt     *time.Time      `json:"event_at,omitempty"`
	Primary     *AnomalyAction  `json:"primary_action,omitempty"`
	Secondaries []AnomalyAction `json:"secondary_actions"`
}

// MarshalJSON keeps secondary_actions an array.
func (anomaly Anomaly) MarshalJSON() ([]byte, error) {
	type wire Anomaly
	clone := wire(anomaly)
	if clone.Secondaries == nil {
		clone.Secondaries = []AnomalyAction{}
	}
	return json.Marshal(clone)
}

// AnomalyAction is a single operator next step.
type AnomalyAction struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Route string `json:"route,omitempty"`
}

// SubjectRef for activity recent is always this VPS.
func SubjectRef(vpsID string) activity.SubjectRef {
	return activity.SubjectRef{Kind: records.SubjectKindVPS, SourceID: vpsID}
}

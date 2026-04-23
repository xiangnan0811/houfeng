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

	MonitoringEnabled = "启用"
	BindingUnbound    = "未绑定"
	HealthNormal      = "正常"
)

var ErrNodeNotFound = errors.New("node not found")

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

func IsValidLifecycleStatus(status string) bool {
	_, ok := allowedLifecycleStatuses[status]
	return ok
}

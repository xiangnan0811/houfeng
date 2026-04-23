package targets

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	TargetTypeService        = "service"
	TargetTypeChinaReference = "china_reference"

	RunStatusEnabled     = "启用"
	RunStatusMaintenance = "维护中"
	RunStatusPaused      = "暂停"
	RunStatusArchived    = "已归档"

	HealthNormal = "正常"
)

var ErrTargetNotFound = errors.New("target not found")

var allowedTargetTypes = map[string]struct{}{
	TargetTypeService:        {},
	TargetTypeChinaReference: {},
}

var allowedRunStatuses = map[string]struct{}{
	RunStatusEnabled:     {},
	RunStatusMaintenance: {},
	RunStatusPaused:      {},
	RunStatusArchived:    {},
}

type TargetRecord struct {
	TargetID                   string     `json:"target_id"`
	Name                       string     `json:"name"`
	TargetType                 string     `json:"target_type"`
	Host                       string     `json:"host"`
	BasePort                   *int       `json:"base_port,omitempty"`
	ExecutionNodeLabels        []string   `json:"execution_node_labels"`
	RunStatus                  string     `json:"run_status"`
	Labels                     []string   `json:"labels"`
	Note                       string     `json:"note"`
	CurrentHealthStatus        string     `json:"current_health_status"`
	CurrentActiveIncidentCount int        `json:"current_active_incident_count"`
	LastSuccessAt              *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt              *time.Time `json:"last_failure_at,omitempty"`
	CurrentPrimaryIssueSummary string     `json:"current_primary_issue_summary"`
	CreatedAt                  time.Time  `json:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}

type ProbeItemRecord struct {
	ProbeItemID    string          `json:"probe_item_id"`
	TargetID       string          `json:"target_id"`
	ProbeKind      string          `json:"probe_kind"`
	Enabled        bool            `json:"enabled"`
	FrequencyTier  string          `json:"frequency_tier"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	Config         json.RawMessage `json:"config"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type CreateTargetInput struct {
	Name                string   `json:"name"`
	TargetType          string   `json:"target_type"`
	Host                string   `json:"host"`
	BasePort            *int     `json:"base_port,omitempty"`
	ExecutionNodeLabels []string `json:"execution_node_labels"`
	RunStatus           string   `json:"run_status"`
	Labels              []string `json:"labels"`
	Note                string   `json:"note"`
}

type CreateProbeItemInput struct {
	ProbeKind      string          `json:"probe_kind"`
	Enabled        bool            `json:"enabled"`
	FrequencyTier  string          `json:"frequency_tier"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	Config         json.RawMessage `json:"config"`
}

type Repository interface {
	ListTargets(context.Context) ([]TargetRecord, error)
	GetTarget(context.Context, string) (TargetRecord, error)
	CreateTarget(context.Context, CreateTargetInput) (TargetRecord, error)
	ListProbeItems(context.Context, string) ([]ProbeItemRecord, error)
	CreateProbeItem(context.Context, string, CreateProbeItemInput) (ProbeItemRecord, error)
}

func IsValidTargetType(targetType string) bool {
	_, ok := allowedTargetTypes[targetType]
	return ok
}

func IsValidRunStatus(status string) bool {
	_, ok := allowedRunStatuses[status]
	return ok
}

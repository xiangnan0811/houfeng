package assetlifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/nodes"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

var ErrInvalidLifecycleActionInput = errors.New("invalid lifecycle action input")
var ErrLifecycleActionBlocked = errors.New("lifecycle action blocked")

type ActionType string

const (
	ActionTypeCancelVPS ActionType = "cancel_vps"

	ActionStatusCompleted = "completed"
	ActionStatusFailed    = "failed"

	StepStatusCompleted = "completed"
	StepStatusSkipped   = "skipped"
	StepStatusFailed    = "failed"

	ObjectTypeVPS          = "vps"
	ObjectTypeSubscription = "subscription"
	ObjectTypeNode         = "node"
	ObjectTypeTarget       = "target"

	StepTypeVPSLifecycle       = "vps_lifecycle"
	StepTypeSubscriptionStatus = "subscription_status"
	StepTypeNodeLifecycle      = "node_lifecycle"
	StepTypeNodeMonitoring     = "node_monitoring"
	StepTypeTargetRunStatus    = "target_run_status"
)

type Repository interface {
	GetVPSCancellationPreview(context.Context, string) (CancellationPreview, error)
	ApplyVPSCancellation(context.Context, string, ApplyCancellationInput) (LifecycleActionResult, error)
	ListNodeAssetContexts(context.Context) ([]AssetContextForNode, error)
	ListTargetAssetContexts(context.Context) ([]AssetContextForTarget, error)
}

type CancellationPreview struct {
	VPS              vpsassets.Record           `json:"vps"`
	Subscriptions    []SubscriptionImpact       `json:"subscriptions"`
	NodeLinks        []assetlinks.NodeSummary   `json:"node_links"`
	Services         []assetservices.Record     `json:"services"`
	Domains          []assetdomains.Record      `json:"domains"`
	TargetLinks      []TargetImpact             `json:"target_links"`
	RecommendedSteps []RecommendedLifecycleStep `json:"recommended_steps"`
	Warnings         []string                   `json:"warnings"`
	Blockers         []string                   `json:"blockers"`
}

type SubscriptionImpact struct {
	Record            subscriptions.Record `json:"record"`
	Role              string               `json:"role"`
	RecommendedAction string               `json:"recommended_action"`
	Message           string               `json:"message"`
}

type TargetImpact struct {
	TargetID     string     `json:"target_id"`
	Name         string     `json:"name"`
	RunStatus    string     `json:"run_status"`
	ServiceIDs   []string   `json:"service_ids"`
	DomainIDs    []string   `json:"domain_ids"`
	LastLinkedAt *time.Time `json:"last_linked_at,omitempty"`
}

type RecommendedLifecycleStep struct {
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
	StepType   string `json:"step_type"`
	FromState  string `json:"from_state"`
	ToState    string `json:"to_state"`
	Required   bool   `json:"required"`
	Message    string `json:"message"`
}

type ApplyCancellationInput struct {
	Reason             string                    `json:"reason"`
	EffectiveDate      *subscriptions.Date       `json:"effective_date"`
	SubscriptionIDs    []string                  `json:"subscription_ids"`
	VPSLifecycleStatus vpsassets.LifecycleStatus `json:"vps_lifecycle_status"`
	NodeActions        []NodeActionInput         `json:"node_actions"`
	TargetActions      []TargetActionInput       `json:"target_actions"`
}

type NodeActionInput struct {
	NodeID           string `json:"node_id"`
	LifecycleStatus  string `json:"lifecycle_status"`
	MonitoringStatus string `json:"monitoring_status"`
}

type TargetActionInput struct {
	TargetID  string `json:"target_id"`
	RunStatus string `json:"run_status"`
}

type LifecycleActionResult struct {
	Action LifecycleActionRecord `json:"action"`
	Steps  []LifecycleActionStep `json:"steps"`
}

type LifecycleActionRecord struct {
	ActionID      string              `json:"action_id"`
	VPSID         string              `json:"vps_id"`
	ActionType    ActionType          `json:"action_type"`
	Status        string              `json:"status"`
	Reason        string              `json:"reason"`
	EffectiveDate *subscriptions.Date `json:"effective_date"`
	CreatedAt     time.Time           `json:"created_at"`
	ConfirmedAt   *time.Time          `json:"confirmed_at"`
	CompletedAt   *time.Time          `json:"completed_at"`
	Summary       map[string]any      `json:"summary"`
}

type LifecycleActionStep struct {
	StepID      string         `json:"step_id"`
	ActionID    string         `json:"action_id"`
	ObjectType  string         `json:"object_type"`
	ObjectID    string         `json:"object_id"`
	StepType    string         `json:"step_type"`
	Status      string         `json:"status"`
	BeforeState map[string]any `json:"before_state"`
	AfterState  map[string]any `json:"after_state"`
	Message     string         `json:"message"`
	ExecutedAt  *time.Time     `json:"executed_at"`
	CreatedAt   time.Time      `json:"created_at"`
}

type AssetContextForNode struct {
	NodeID                string             `json:"node_id"`
	LinkedVPSCount        int                `json:"linked_vps_count"`
	CancellationAttention bool               `json:"cancellation_attention"`
	Summaries             []LinkedVPSContext `json:"summaries"`
}

type AssetContextForTarget struct {
	TargetID              string             `json:"target_id"`
	LinkedVPSCount        int                `json:"linked_vps_count"`
	CancellationAttention bool               `json:"cancellation_attention"`
	Summaries             []LinkedVPSContext `json:"summaries"`
	ServiceIDs            []string           `json:"service_ids"`
	DomainIDs             []string           `json:"domain_ids"`
}

type LinkedVPSContext struct {
	VPSID             string                    `json:"vps_id"`
	DisplayName       string                    `json:"display_name"`
	LifecycleStatus   vpsassets.LifecycleStatus `json:"lifecycle_status"`
	RenewalDecision   vpsassets.RenewalDecision `json:"renewal_decision"`
	SubscriptionState string                    `json:"subscription_state"`
	Message           string                    `json:"message"`
}

func NormalizeApplyCancellationInput(input ApplyCancellationInput) ApplyCancellationInput {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.VPSLifecycleStatus == "" {
		input.VPSLifecycleStatus = vpsassets.LifecycleCancelled
	}
	input.VPSLifecycleStatus = vpsassets.LifecycleStatus(strings.TrimSpace(string(input.VPSLifecycleStatus)))
	input.SubscriptionIDs = normalizeStringList(input.SubscriptionIDs)
	for i := range input.NodeActions {
		input.NodeActions[i].NodeID = strings.TrimSpace(input.NodeActions[i].NodeID)
		input.NodeActions[i].LifecycleStatus = strings.TrimSpace(input.NodeActions[i].LifecycleStatus)
		input.NodeActions[i].MonitoringStatus = strings.TrimSpace(input.NodeActions[i].MonitoringStatus)
	}
	for i := range input.TargetActions {
		input.TargetActions[i].TargetID = strings.TrimSpace(input.TargetActions[i].TargetID)
		input.TargetActions[i].RunStatus = strings.TrimSpace(input.TargetActions[i].RunStatus)
	}
	return input
}

func ValidateApplyCancellationInput(input ApplyCancellationInput) error {
	if strings.TrimSpace(input.Reason) == "" {
		return fmt.Errorf("%w: reason is required", ErrInvalidLifecycleActionInput)
	}
	if input.VPSLifecycleStatus != vpsassets.LifecycleToCancel && input.VPSLifecycleStatus != vpsassets.LifecycleCancelled {
		return fmt.Errorf("%w: vps_lifecycle_status must be to_cancel or cancelled", ErrInvalidLifecycleActionInput)
	}
	seenSubscriptions := map[string]struct{}{}
	for _, subscriptionID := range input.SubscriptionIDs {
		if subscriptionID == "" {
			return fmt.Errorf("%w: subscription_id is required", ErrInvalidLifecycleActionInput)
		}
		if _, ok := seenSubscriptions[subscriptionID]; ok {
			return fmt.Errorf("%w: duplicate subscription_id", ErrInvalidLifecycleActionInput)
		}
		seenSubscriptions[subscriptionID] = struct{}{}
	}
	seenNodes := map[string]struct{}{}
	for _, action := range input.NodeActions {
		if action.NodeID == "" {
			return fmt.Errorf("%w: node_id is required", ErrInvalidLifecycleActionInput)
		}
		if action.LifecycleStatus == "" && action.MonitoringStatus == "" {
			return fmt.Errorf("%w: node action must include lifecycle_status or monitoring_status", ErrInvalidLifecycleActionInput)
		}
		if action.LifecycleStatus != "" && !nodes.IsValidLifecycleStatus(action.LifecycleStatus) {
			return fmt.Errorf("%w: invalid node lifecycle_status", ErrInvalidLifecycleActionInput)
		}
		if action.MonitoringStatus != "" && !isValidNodeMonitoringStatus(action.MonitoringStatus) {
			return fmt.Errorf("%w: invalid node monitoring_status", ErrInvalidLifecycleActionInput)
		}
		if _, ok := seenNodes[action.NodeID]; ok {
			return fmt.Errorf("%w: duplicate node_id", ErrInvalidLifecycleActionInput)
		}
		seenNodes[action.NodeID] = struct{}{}
	}
	seenTargets := map[string]struct{}{}
	for _, action := range input.TargetActions {
		if action.TargetID == "" {
			return fmt.Errorf("%w: target_id is required", ErrInvalidLifecycleActionInput)
		}
		if action.RunStatus == "" {
			return fmt.Errorf("%w: target action must include run_status", ErrInvalidLifecycleActionInput)
		}
		if !targets.IsValidRunStatus(action.RunStatus) {
			return fmt.Errorf("%w: invalid target run_status", ErrInvalidLifecycleActionInput)
		}
		if _, ok := seenTargets[action.TargetID]; ok {
			return fmt.Errorf("%w: duplicate target_id", ErrInvalidLifecycleActionInput)
		}
		seenTargets[action.TargetID] = struct{}{}
	}
	return nil
}

func isValidNodeMonitoringStatus(status string) bool {
	switch status {
	case nodes.MonitoringEnabled, nodes.MonitoringMaintenance, nodes.MonitoringPaused:
		return true
	default:
		return false
	}
}

func normalizeStringList(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

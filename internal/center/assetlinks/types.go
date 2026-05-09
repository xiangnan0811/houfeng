package assetlinks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrVPSNodeLinkNotFound = errors.New("vps node link not found")
var ErrVPSNodeLinkConflict = errors.New("vps node link conflict")
var ErrInvalidVPSNodeLinkInput = errors.New("invalid vps node link input")

type Record struct {
	LinkID     string     `json:"link_id"`
	VPSID      string     `json:"vps_id"`
	NodeID     string     `json:"node_id"`
	LinkedAt   time.Time  `json:"linked_at"`
	UnlinkedAt *time.Time `json:"unlinked_at,omitempty"`
	Note       string     `json:"note"`
}

type NodeSummary struct {
	NodeID                     string     `json:"node_id"`
	DisplayName                string     `json:"display_name"`
	Group                      string     `json:"group"`
	Region                     string     `json:"region"`
	City                       string     `json:"city"`
	Provider                   string     `json:"provider"`
	LifecycleStatus            string     `json:"lifecycle_status"`
	MonitoringStatus           string     `json:"monitoring_status"`
	BindingStatus              string     `json:"binding_status"`
	CurrentHealthStatus        string     `json:"current_health_status"`
	LastHeartbeatAt            *time.Time `json:"last_heartbeat_at,omitempty"`
	LastSyncAt                 *time.Time `json:"last_sync_at,omitempty"`
	CurrentActiveIncidentCount int        `json:"current_active_incident_count"`
	CurrentPrimaryIssueSummary string     `json:"current_primary_issue_summary"`
	LinkedAt                   time.Time  `json:"linked_at"`
	Note                       string     `json:"note"`
}

type VPSSummary struct {
	VPSID           string     `json:"vps_id"`
	DisplayName     string     `json:"display_name"`
	ProviderID      *string    `json:"provider_id"`
	ProviderName    string     `json:"provider_name"`
	Country         string     `json:"country"`
	Region          string     `json:"region"`
	City            string     `json:"city"`
	LifecycleStatus string     `json:"lifecycle_status"`
	UsageStatus     string     `json:"usage_status"`
	RenewalDecision string     `json:"renewal_decision"`
	Importance      string     `json:"importance"`
	Labels          []string   `json:"labels"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
	LinkedAt        time.Time  `json:"linked_at"`
	Note            string     `json:"note"`
}

type LinkInput struct {
	NodeID string `json:"node_id"`
	Note   string `json:"note"`
}

type UnlinkInput struct {
	NodeID string `json:"node_id"`
	Note   string `json:"note"`
}

type Repository interface {
	LinkNode(context.Context, string, LinkInput) (Record, error)
	UnlinkNode(context.Context, string, UnlinkInput) (Record, error)
	ListNodesForVPS(context.Context, string) ([]NodeSummary, error)
	ListVPSForNode(context.Context, string) ([]VPSSummary, error)
	CountActiveLinksForVPS(context.Context, string) (int, error)
}

func NormalizeLinkInput(input LinkInput) LinkInput {
	input.NodeID = strings.TrimSpace(input.NodeID)
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func ValidateLinkInput(input LinkInput) error {
	if strings.TrimSpace(input.NodeID) == "" {
		return fmt.Errorf("%w: node_id is required", ErrInvalidVPSNodeLinkInput)
	}
	return nil
}

func NormalizeUnlinkInput(input UnlinkInput) UnlinkInput {
	input.NodeID = strings.TrimSpace(input.NodeID)
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func ValidateUnlinkInput(input UnlinkInput) error {
	if strings.TrimSpace(input.NodeID) == "" {
		return fmt.Errorf("%w: node_id is required", ErrInvalidVPSNodeLinkInput)
	}
	return nil
}

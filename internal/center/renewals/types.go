package renewals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"houfeng/internal/center/vpsassets"
)

var ErrRenewalTimelineNotFound = errors.New("renewal timeline not found")
var ErrInvalidRenewalDecisionInput = errors.New("invalid renewal decision input")

type DecisionRecord struct {
	DecisionID   string                     `json:"decision_id"`
	VPSID        string                     `json:"vps_id"`
	FromDecision *vpsassets.RenewalDecision `json:"from_decision"`
	ToDecision   vpsassets.RenewalDecision  `json:"to_decision"`
	Reason       string                     `json:"reason"`
	DecidedAt    time.Time                  `json:"decided_at"`
	CreatedAt    time.Time                  `json:"created_at"`
}

type CreateDecisionInput struct {
	VPSID        string
	FromDecision *vpsassets.RenewalDecision
	ToDecision   vpsassets.RenewalDecision
	Reason       string
	DecidedAt    *time.Time
}

type VPSTimeline struct {
	VPSID            string           `json:"vps_id"`
	RenewalDecisions []DecisionRecord `json:"renewal_decisions"`
}

type Repository interface {
	CreateRenewalDecision(context.Context, CreateDecisionInput) (DecisionRecord, error)
	ListRenewalDecisionsForVPS(context.Context, string) ([]DecisionRecord, error)
	GetVPSTimeline(context.Context, string) (VPSTimeline, error)
}

type TimelineRepository interface {
	GetVPSTimeline(context.Context, string) (VPSTimeline, error)
}

func NormalizeCreateDecisionInput(input CreateDecisionInput) CreateDecisionInput {
	input.VPSID = NormalizeVPSID(input.VPSID)
	if input.FromDecision != nil {
		normalized := vpsassets.RenewalDecision(strings.TrimSpace(string(*input.FromDecision)))
		input.FromDecision = &normalized
	}
	input.ToDecision = vpsassets.RenewalDecision(strings.TrimSpace(string(input.ToDecision)))
	input.Reason = strings.TrimSpace(input.Reason)
	if input.DecidedAt != nil {
		decidedAt := input.DecidedAt.UTC()
		input.DecidedAt = &decidedAt
	}
	return input
}

func ValidateCreateDecisionInput(input CreateDecisionInput) error {
	if NormalizeVPSID(input.VPSID) == "" {
		return fmt.Errorf("%w: vps_id is required", ErrInvalidRenewalDecisionInput)
	}
	if input.FromDecision != nil && !vpsassets.IsValidRenewalDecision(*input.FromDecision) {
		return fmt.Errorf("%w: invalid from_decision", ErrInvalidRenewalDecisionInput)
	}
	if !vpsassets.IsValidRenewalDecision(input.ToDecision) {
		return fmt.Errorf("%w: invalid to_decision", ErrInvalidRenewalDecisionInput)
	}
	if input.DecidedAt != nil && input.DecidedAt.IsZero() {
		return fmt.Errorf("%w: decided_at is required", ErrInvalidRenewalDecisionInput)
	}
	return nil
}

func NormalizeVPSID(vpsID string) string {
	return strings.TrimSpace(vpsID)
}

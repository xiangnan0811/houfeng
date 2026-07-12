package commandaudits

import (
	"context"
	"time"
)

const RejectionReasonSensitiveConfirmationRequired = "sensitive_confirmation_required"

type Query struct {
	StartedFrom        *time.Time
	StartedTo          time.Time
	MonitoringInstance string
	CommandID          string
	Sensitivity        string
	Outcome            string
	Actor              string
	ActionID           string
	Limit              int
	BeforeStartedAt    *time.Time
	BeforeID           string
}

type MonitoringInstanceIdentity struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

type ActorIdentity struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

type Event struct {
	AuditID         string    `json:"audit_id"`
	EventType       string    `json:"event_type"`
	Source          string    `json:"source"`
	OccurredAt      time.Time `json:"occurred_at"`
	ExitCode        *int      `json:"exit_code,omitempty"`
	RejectionReason string    `json:"rejection_reason,omitempty"`
}

type Action struct {
	ID                 string                     `json:"id"`
	ActionID           string                     `json:"action_id,omitempty"`
	MonitoringInstance MonitoringInstanceIdentity `json:"monitoring_instance"`
	CommandID          string                     `json:"command_id"`
	Sensitivity        string                     `json:"sensitivity"`
	Outcome            string                     `json:"outcome"`
	Actor              *ActorIdentity             `json:"actor"`
	StartedAt          time.Time                  `json:"started_at"`
	Events             []Event                    `json:"events"`
}

type Page struct {
	Items   []Action
	HasMore bool
}

type Repository interface {
	ListCommandAudits(context.Context, Query) (Page, error)
}

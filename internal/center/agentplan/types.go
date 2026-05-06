package agentplan

import (
	"context"
	"encoding/json"
)

type ProbeAssignment struct {
	TargetID           string
	TargetHost         string
	TargetBasePort     *int
	MaintenanceContext bool
	ProbeItemID        string
	ProbeKind          string
	FrequencyTier      string
	TimeoutSeconds     int
	Config             json.RawMessage
}

// PendingAction describes a command the center wants the agent to execute.
type PendingAction struct {
	CommandID string
	ActionID  string
}

type SyncPlan struct {
	HostSampleFrequencyTier      string
	HostSampleMaintenanceContext bool
	ProbeAssignments             []ProbeAssignment
	PendingAction                *PendingAction
}

type Repository interface {
	BuildSyncPlan(context.Context, string) (SyncPlan, error)
}

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

type SyncPlan struct {
	HostSampleFrequencyTier string
	ProbeAssignments        []ProbeAssignment
}

type Repository interface {
	BuildSyncPlan(context.Context, string) (SyncPlan, error)
}

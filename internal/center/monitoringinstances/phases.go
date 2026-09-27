package monitoringinstances

import (
	"context"
	"time"
)

// Phase exposes durable enrollment history without disclosing credentials or
// the raw host fingerprint.
type Phase struct {
	SessionID           string     `json:"session_id"`
	Capability          string     `json:"capability"`
	FingerprintHash     string     `json:"fingerprint_hash"`
	StartedAt           time.Time  `json:"started_at"`
	EndedAt             *time.Time `json:"ended_at,omitempty"`
	LastTrustedOnlineAt *time.Time `json:"last_trusted_online_at,omitempty"`
	EverConnected       bool       `json:"ever_connected"`
}

type PhaseRepository interface {
	ListMonitoringInstancePhases(context.Context, string) ([]Phase, error)
}

package incidents

import (
	"fmt"
	"time"

	centersettings "houfeng/internal/center/settings"
)

// HeartbeatIncidentPolicyFromSettings resolves the server-owned heartbeat
// policy from the validated global incident defaults.
func HeartbeatIncidentPolicyFromSettings(settings centersettings.CenterSettings) (HeartbeatIncidentPolicy, error) {
	defaults, err := centersettings.ValidateIncidentDefaults(settings.IncidentDefaults)
	if err != nil {
		return HeartbeatIncidentPolicy{}, fmt.Errorf("validate heartbeat incident policy: %w", err)
	}
	interval := time.Duration(defaults.HeartbeatIntervalSeconds) * time.Second
	policy := HeartbeatIncidentPolicy{
		HeartbeatInterval:      interval,
		MissingThreshold:       defaults.StaleThresholdIntervals,
		RecoverySuccesses:      heartbeatRecoverySuccesses,
		RecoveryMaxIntervalGap: 2 * interval,
	}
	if !validHeartbeatIncidentPolicy(policy) {
		return HeartbeatIncidentPolicy{}, fmt.Errorf("validate heartbeat incident policy: invalid effective policy")
	}
	return policy, nil
}

// HeartbeatIsStale reports whether the latest observation has crossed the
// effective missing-heartbeat threshold. Invalid or future observations do not
// become stale here; callers surface invalid source timestamps separately.
func HeartbeatIsStale(now, lastHeartbeatAt time.Time, policy HeartbeatIncidentPolicy) bool {
	if lastHeartbeatAt.IsZero() || !validHeartbeatIncidentPolicy(policy) {
		return false
	}
	return heartbeatMissedIntervals(now, lastHeartbeatAt, policy.HeartbeatInterval) >= policy.MissingThreshold
}

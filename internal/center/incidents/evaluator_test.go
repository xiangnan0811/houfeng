package incidents

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/contracts/agentapi"
)

func TestEvaluateMonitoringInstanceHeartbeatMissingBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	policy := HeartbeatIncidentPolicy{
		HeartbeatInterval:      5 * time.Second,
		MissingThreshold:       12,
		RecoverySuccesses:      3,
		RecoveryMaxIntervalGap: 10 * time.Second,
	}
	tests := []struct {
		name     string
		missed   int
		severity Severity
		active   bool
	}{
		{name: "before first boundary", missed: 11},
		{name: "first boundary", missed: 12, severity: SeverityNotice, active: true},
		{name: "before alert boundary", missed: 23, severity: SeverityNotice, active: true},
		{name: "alert boundary", missed: 24, severity: SeverityAlert, active: true},
		{name: "before critical boundary", missed: 47, severity: SeverityAlert, active: true},
		{name: "critical boundary", missed: 48, severity: SeverityCritical, active: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lastHeartbeat := now.Add(-time.Duration(tt.missed) * policy.HeartbeatInterval)
			got := EvaluateMonitoringInstanceHeartbeatMissing(nil, "mi_boundary", now, &lastHeartbeat, policy, nil)
			if !tt.active {
				if got.Transition != TransitionNoop || got.Current != nil || got.Event != nil || got.Notification != nil {
					t.Fatalf("result = %#v, want inactive noop", got)
				}
				return
			}
			if got.Transition != TransitionStarted || got.Current == nil || got.Current.Severity != tt.severity {
				t.Fatalf("result = %#v, want started %q incident", got, tt.severity)
			}
		})
	}
}

func TestEvaluateMonitoringInstanceHeartbeatMissingCustomThreshold(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	policy := HeartbeatIncidentPolicy{
		HeartbeatInterval:      5 * time.Second,
		MissingThreshold:       20,
		RecoverySuccesses:      3,
		RecoveryMaxIntervalGap: 10 * time.Second,
	}
	tests := []struct {
		missed   int
		severity Severity
	}{
		{missed: 19},
		{missed: 20, severity: SeverityNotice},
		{missed: 40, severity: SeverityAlert},
		{missed: 80, severity: SeverityCritical},
	}
	for _, tt := range tests {
		lastHeartbeat := now.Add(-time.Duration(tt.missed) * policy.HeartbeatInterval)
		got := EvaluateMonitoringInstanceHeartbeatMissing(nil, "mi_custom", now, &lastHeartbeat, policy, nil)
		if tt.severity == "" {
			if got.Current != nil || got.Transition != TransitionNoop {
				t.Fatalf("missed %d result = %#v, want inactive noop", tt.missed, got)
			}
			continue
		}
		if got.Current == nil || got.Current.Severity != tt.severity {
			t.Fatalf("missed %d result = %#v, want %q", tt.missed, got, tt.severity)
		}
	}
}

func TestValidHeartbeatIncidentPolicyRejectsOverflowingDerivedBounds(t *testing.T) {
	t.Parallel()

	valid := HeartbeatIncidentPolicy{
		HeartbeatInterval:      5 * time.Second,
		MissingThreshold:       20,
		RecoverySuccesses:      3,
		RecoveryMaxIntervalGap: 10 * time.Second,
	}
	if !validHeartbeatIncidentPolicy(valid) {
		t.Fatal("validHeartbeatIncidentPolicy(N=20) = false, want true")
	}

	invalidInterval := valid
	invalidInterval.HeartbeatInterval = time.Duration(1<<63-1)/2 + 1
	if validHeartbeatIncidentPolicy(invalidInterval) {
		t.Fatal("validHeartbeatIncidentPolicy(overflowing 2*interval) = true, want false")
	}

	invalidThreshold := valid
	invalidThreshold.MissingThreshold = int(^uint(0)>>1)/4 + 1
	if validHeartbeatIncidentPolicy(invalidThreshold) {
		t.Fatal("validHeartbeatIncidentPolicy(overflowing 4*N) = true, want false")
	}

	for _, recoverySuccesses := range []int{heartbeatRecoverySuccesses - 1, heartbeatRecoverySuccesses + 1} {
		invalidRecoverySuccesses := valid
		invalidRecoverySuccesses.RecoverySuccesses = recoverySuccesses
		if validHeartbeatIncidentPolicy(invalidRecoverySuccesses) {
			t.Fatalf("validHeartbeatIncidentPolicy(recovery successes=%d) = true, want false", recoverySuccesses)
		}
	}

	for _, recoveryGap := range []time.Duration{valid.HeartbeatInterval, 3 * valid.HeartbeatInterval} {
		invalidRecoveryGap := valid
		invalidRecoveryGap.RecoveryMaxIntervalGap = recoveryGap
		if validHeartbeatIncidentPolicy(invalidRecoveryGap) {
			t.Fatalf("validHeartbeatIncidentPolicy(recovery gap=%v) = true, want false", recoveryGap)
		}
	}
}

func TestHeartbeatMissedIntervalsSaturatesAndHandlesClockSkew(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	if got := heartbeatMissedIntervals(now, now.Add(-100*time.Second), 5*time.Second); got != 20 {
		t.Fatalf("heartbeatMissedIntervals(normal) = %d, want 20", got)
	}
	if got := heartbeatMissedIntervals(now, now.Add(time.Second), time.Second); got != 0 {
		t.Fatalf("heartbeatMissedIntervals(clock skew) = %d, want 0", got)
	}
	if got := heartbeatMissedIntervals(now, now.Add(-time.Second), 0); got != 0 {
		t.Fatalf("heartbeatMissedIntervals(zero interval) = %d, want 0", got)
	}

	distantNow := time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)
	distantPast := time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)
	if got, want := heartbeatMissedIntervals(distantNow, distantPast, time.Nanosecond), int(^uint(0)>>1); got != want {
		t.Fatalf("heartbeatMissedIntervals(saturated) = %d, want %d", got, want)
	}
}

func TestEvaluateMonitoringInstanceHeartbeatMissingJumpStartsAtActualSeverity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	lastHeartbeat := now.Add(-48 * 5 * time.Second)
	policy := HeartbeatIncidentPolicy{HeartbeatInterval: 5 * time.Second, MissingThreshold: 12, RecoverySuccesses: 3, RecoveryMaxIntervalGap: 10 * time.Second}

	got := EvaluateMonitoringInstanceHeartbeatMissing(nil, "mi_jump", now, &lastHeartbeat, policy, nil)
	if got.Transition != TransitionStarted || got.Current == nil || got.Current.Severity != SeverityCritical {
		t.Fatalf("result = %#v, want one directly-started critical incident", got)
	}
	if got.Event == nil || got.Event.EventType != EventIncidentStarted || got.Event.Severity != SeverityCritical {
		t.Fatalf("Event = %#v, want one critical start event", got.Event)
	}
}

func TestEvaluateMonitoringInstanceHeartbeatMissingRecoveryRequiresStableLiveReceipts(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	now := startedAt.Add(time.Minute)
	lastHeartbeat := now.Add(-5 * time.Second)
	policy := HeartbeatIncidentPolicy{HeartbeatInterval: 5 * time.Second, MissingThreshold: 12, RecoverySuccesses: 3, RecoveryMaxIntervalGap: 10 * time.Second}
	previous := &IncidentRecord{
		IncidentID:      "inc_monitoring_instance_mi_recovery_monitoring_instance_heartbeat_missing",
		ObjectType:      ObjectTypeMonitoringInstance,
		ObjectID:        "mi_recovery",
		IncidentClass:   IncidentMonitoringInstanceHeartbeatMissing,
		Severity:        SeverityAlert,
		StartedAt:       startedAt,
		LastEvaluatedAt: startedAt.Add(30 * time.Second),
		Status:          IncidentStatusActive,
	}
	receipts := []LiveHeartbeatReceipt{
		{SyncBatchID: "batch-3", ReceivedAt: startedAt.Add(15 * time.Second)},
		{SyncBatchID: "batch-2", ReceivedAt: startedAt.Add(10 * time.Second)},
		{SyncBatchID: "batch-1", ReceivedAt: startedAt.Add(5 * time.Second)},
	}

	for count := 1; count <= 2; count++ {
		got := EvaluateMonitoringInstanceHeartbeatMissing(previous, previous.ObjectID, now, &lastHeartbeat, policy, receipts[:count])
		if got.Transition != TransitionNoop || got.Current == nil || got.Current.IncidentID != previous.IncidentID || got.Event != nil || got.Notification != nil {
			t.Fatalf("%d receipts result = %#v, want previous active incident preserved", count, got)
		}
	}

	got := EvaluateMonitoringInstanceHeartbeatMissing(previous, previous.ObjectID, now, &lastHeartbeat, policy, receipts)
	if got.Transition != TransitionRecovered || got.Current != nil {
		t.Fatalf("three receipts result = %#v, want recovered", got)
	}
	if got.Event == nil || got.Event.EventType != EventIncidentRecovered || got.Notification == nil || got.Notification.Reason != NotificationReasonRecovered {
		t.Fatalf("three receipts result = %#v, want one recovery event and notification", got)
	}
}

func TestEvaluateMonitoringInstanceHeartbeatMissingRecoveryRejectsInvalidEvidence(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	now := startedAt.Add(time.Minute)
	lastHeartbeat := now.Add(-5 * time.Second)
	validPolicy := HeartbeatIncidentPolicy{HeartbeatInterval: 5 * time.Second, MissingThreshold: 12, RecoverySuccesses: 3, RecoveryMaxIntervalGap: 10 * time.Second}
	previous := &IncidentRecord{
		IncidentID:      "inc_monitoring_instance_mi_recovery_monitoring_instance_heartbeat_missing",
		ObjectType:      ObjectTypeMonitoringInstance,
		ObjectID:        "mi_recovery",
		IncidentClass:   IncidentMonitoringInstanceHeartbeatMissing,
		Severity:        SeverityAlert,
		StartedAt:       startedAt,
		LastEvaluatedAt: startedAt.Add(30 * time.Second),
		Status:          IncidentStatusActive,
	}
	tests := []struct {
		name     string
		policy   HeartbeatIncidentPolicy
		receipts []LiveHeartbeatReceipt
	}{
		{
			name:   "duplicate batch",
			policy: validPolicy,
			receipts: []LiveHeartbeatReceipt{
				{SyncBatchID: "batch-2", ReceivedAt: startedAt.Add(15 * time.Second)},
				{SyncBatchID: "batch-1", ReceivedAt: startedAt.Add(10 * time.Second)},
				{SyncBatchID: "batch-1", ReceivedAt: startedAt.Add(5 * time.Second)},
			},
		},
		{
			name:   "pre incident",
			policy: validPolicy,
			receipts: []LiveHeartbeatReceipt{
				{SyncBatchID: "batch-3", ReceivedAt: startedAt.Add(10 * time.Second)},
				{SyncBatchID: "batch-2", ReceivedAt: startedAt.Add(5 * time.Second)},
				{SyncBatchID: "batch-1", ReceivedAt: startedAt},
			},
		},
		{
			name:   "gap too large",
			policy: validPolicy,
			receipts: []LiveHeartbeatReceipt{
				{SyncBatchID: "batch-3", ReceivedAt: startedAt.Add(30 * time.Second)},
				{SyncBatchID: "batch-2", ReceivedAt: startedAt.Add(10 * time.Second)},
				{SyncBatchID: "batch-1", ReceivedAt: startedAt.Add(5 * time.Second)},
			},
		},
		{
			name:     "missing receipts after raising threshold",
			policy:   HeartbeatIncidentPolicy{HeartbeatInterval: 5 * time.Second, MissingThreshold: 20, RecoverySuccesses: 3, RecoveryMaxIntervalGap: 10 * time.Second},
			receipts: nil,
		},
		{
			name:     "invalid policy",
			policy:   HeartbeatIncidentPolicy{HeartbeatInterval: 5 * time.Second, MissingThreshold: 12, RecoverySuccesses: 0, RecoveryMaxIntervalGap: 10 * time.Second},
			receipts: []LiveHeartbeatReceipt{{SyncBatchID: "batch-3", ReceivedAt: startedAt.Add(15 * time.Second)}, {SyncBatchID: "batch-2", ReceivedAt: startedAt.Add(10 * time.Second)}, {SyncBatchID: "batch-1", ReceivedAt: startedAt.Add(5 * time.Second)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateMonitoringInstanceHeartbeatMissing(previous, previous.ObjectID, now, &lastHeartbeat, tt.policy, tt.receipts)
			if got.Transition != TransitionNoop || got.Current == nil || got.Current.IncidentID != previous.IncidentID || got.Event != nil || got.Notification != nil {
				t.Fatalf("result = %#v, want previous active incident preserved", got)
			}
		})
	}
}

func TestEvaluateMonitoringInstanceHeartbeatMissingStartsAndEscalates(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	lastHeartbeat := now.Add(-3 * time.Minute)
	policy := HeartbeatIncidentPolicy{HeartbeatInterval: time.Minute, MissingThreshold: 3, RecoverySuccesses: 3, RecoveryMaxIntervalGap: 2 * time.Minute}

	started := EvaluateMonitoringInstanceHeartbeatMissing(nil, "mi_001", now, &lastHeartbeat, policy, nil)
	if started.Transition != TransitionStarted {
		t.Fatalf("Transition = %q, want %q", started.Transition, TransitionStarted)
	}
	if started.Current == nil || started.Current.Severity != SeverityNotice {
		t.Fatalf("Current = %#v, want notice incident", started.Current)
	}
	if started.Notification == nil || started.Notification.Reason != NotificationReasonStarted {
		t.Fatalf("Notification = %#v, want started notification", started.Notification)
	}
	if started.Event == nil || started.Event.Provenance != MonitoringEventProvenanceCenter || started.Event.ProducerVersion != MonitoringEventProducerVersion || started.Event.RuleVersion != MonitoringEventIncidentRuleVersion || started.Event.PriorState != "normal" || started.Event.ResultingState != "notice" || started.Event.IsBackfilled || started.Event.CorrectionOfEventID != "" {
		t.Fatalf("started Event = %#v, want explicit center incident transition metadata", started.Event)
	}

	previous := started.Current
	olderHeartbeat := now.Add(-12 * time.Minute)
	escalated := EvaluateMonitoringInstanceHeartbeatMissing(previous, "mi_001", now, &olderHeartbeat, policy, nil)
	if escalated.Transition != TransitionEscalated {
		t.Fatalf("Transition = %q, want %q", escalated.Transition, TransitionEscalated)
	}
	if escalated.Current == nil || escalated.Current.Severity != SeverityCritical {
		t.Fatalf("Current = %#v, want critical incident", escalated.Current)
	}
	if escalated.Notification == nil || escalated.Notification.Reason != NotificationReasonEscalated {
		t.Fatalf("Notification = %#v, want escalated notification", escalated.Notification)
	}
	if escalated.Event == nil || escalated.Event.PriorState != "notice" || escalated.Event.ResultingState != "critical" {
		t.Fatalf("escalated Event = %#v, want notice-to-critical transition", escalated.Event)
	}
}

func TestEvaluateMonitoringInstanceHeartbeatMissingRecovers(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	lastHeartbeat := now.Add(-30 * time.Second)
	previous := &IncidentRecord{IncidentID: "inc_monitoring_instance_mi_001_monitoring_instance_heartbeat_missing", ObjectType: ObjectTypeMonitoringInstance, ObjectID: "mi_001", IncidentClass: IncidentMonitoringInstanceHeartbeatMissing, Severity: SeverityAlert, StartedAt: now.Add(-time.Minute), LastEvaluatedAt: now.Add(-time.Minute)}
	policy := HeartbeatIncidentPolicy{HeartbeatInterval: time.Minute, MissingThreshold: 3, RecoverySuccesses: 3, RecoveryMaxIntervalGap: 2 * time.Minute}
	receipts := []LiveHeartbeatReceipt{{SyncBatchID: "batch-3", ReceivedAt: now.Add(-10 * time.Second)}, {SyncBatchID: "batch-2", ReceivedAt: now.Add(-20 * time.Second)}, {SyncBatchID: "batch-1", ReceivedAt: now.Add(-30 * time.Second)}}

	result := EvaluateMonitoringInstanceHeartbeatMissing(previous, "mi_001", now, &lastHeartbeat, policy, receipts)
	if result.Transition != TransitionRecovered {
		t.Fatalf("Transition = %q, want %q", result.Transition, TransitionRecovered)
	}
	if result.Notification == nil || result.Notification.Reason != NotificationReasonRecovered {
		t.Fatalf("Notification = %#v, want recovery notification", result.Notification)
	}
	if result.Event == nil || result.Event.IncidentClass != IncidentMonitoringInstanceHeartbeatMissing || result.Event.IncidentID != previous.IncidentID {
		t.Fatalf("Event = %#v, want incident identity on recovery", result.Event)
	}
}

func TestEvaluateMonitoringInstanceDiskAndInodePressureThresholds(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	diskSample := &runtimefacts.HostSample{ObservedAt: now, DiskUsedPct: 92}
	inodeSample := &runtimefacts.HostSample{ObservedAt: now, InodeUsedPct: 95}

	thresholds := DefaultMetricThresholds()
	disk := EvaluateMonitoringInstanceDiskPressure(nil, "mi_001", diskSample, thresholds)
	if disk.Current == nil || disk.Current.Severity != SeverityAlert {
		t.Fatalf("disk severity = %#v, want alert", disk.Current)
	}
	inode := EvaluateMonitoringInstanceInodePressure(nil, "mi_001", inodeSample, thresholds)
	if inode.Current == nil || inode.Current.Severity != SeverityCritical {
		t.Fatalf("inode severity = %#v, want critical", inode.Current)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureUsesSustainedWindow(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	samples := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 91, MemUsedPct: 93, NormalizedLoad5: 1.9, MemAvailableBytes: 700 * 1024 * 1024},
		{ObservedAt: now.Add(-8 * time.Minute), CPUUsagePct: 92, MemUsedPct: 94, NormalizedLoad5: 1.95, MemAvailableBytes: 650 * 1024 * 1024},
		{ObservedAt: now.Add(-15 * time.Minute), CPUUsagePct: 90, MemUsedPct: 92, NormalizedLoad5: 1.85, MemAvailableBytes: 620 * 1024 * 1024},
	}

	thresholds := DefaultMetricThresholds()
	result := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", samples, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if result.Current == nil || result.Current.Severity != SeverityAlert {
		t.Fatalf("Current = %#v, want alert resource incident", result.Current)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureRequiresFullWindowCoverage(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	samples := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 99, MemUsedPct: 97, NormalizedLoad5: 2.8, MemAvailableBytes: 400 * 1024 * 1024},
		{ObservedAt: now.Add(-7 * time.Minute), CPUUsagePct: 99, MemUsedPct: 97, NormalizedLoad5: 2.9, MemAvailableBytes: 390 * 1024 * 1024},
		{ObservedAt: now.Add(-14 * time.Minute), CPUUsagePct: 99, MemUsedPct: 97, NormalizedLoad5: 2.7, MemAvailableBytes: 380 * 1024 * 1024},
	}

	thresholds := DefaultMetricThresholds()
	result := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", samples, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if result.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q when 15m/30m coverage is incomplete", result.Transition, TransitionNoop)
	}
	if result.Current != nil {
		t.Fatalf("Current = %#v, want nil", result.Current)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureUsesLoadAndLowAvailableMemoryForSeverity(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	loadSamples := []MonitoringInstanceResourceSample{
		{ObservedAt: now, NormalizedLoad5: 6.2, MemAvailableBytes: 800 * 1024 * 1024},
		{ObservedAt: now.Add(-8 * time.Minute), NormalizedLoad5: 6.3, MemAvailableBytes: 780 * 1024 * 1024},
		{ObservedAt: now.Add(-15 * time.Minute), NormalizedLoad5: 6.1, MemAvailableBytes: 760 * 1024 * 1024},
	}
	thresholds := DefaultMetricThresholds()
	loadResult := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", loadSamples, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if loadResult.Current == nil || loadResult.Current.Severity != SeverityAlert {
		t.Fatalf("Current = %#v, want alert load-driven resource incident", loadResult.Current)
	}

	memorySamples := []MonitoringInstanceResourceSample{
		{ObservedAt: now, MemUsedPct: 96, MemAvailableBytes: 400 * 1024 * 1024},
		{ObservedAt: now.Add(-15 * time.Minute), MemUsedPct: 95, MemAvailableBytes: 420 * 1024 * 1024},
		{ObservedAt: now.Add(-30 * time.Minute), MemUsedPct: 97, MemAvailableBytes: 380 * 1024 * 1024},
	}
	memoryResult := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", memorySamples, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if memoryResult.Current == nil || memoryResult.Current.Severity != SeverityCritical {
		t.Fatalf("Current = %#v, want critical low-available-memory incident", memoryResult.Current)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureIgnoresSuppressedHistoryForActiveEvidence(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	samples := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 91, MemUsedPct: 93, NormalizedLoad5: 1.9, MemAvailableBytes: 700 * 1024 * 1024},
		{ObservedAt: now.Add(-8 * time.Minute), CPUUsagePct: 92, MemUsedPct: 94, NormalizedLoad5: 1.95, MemAvailableBytes: 650 * 1024 * 1024, MaintenanceContext: true},
		{ObservedAt: now.Add(-15 * time.Minute), CPUUsagePct: 90, MemUsedPct: 92, NormalizedLoad5: 1.85, MemAvailableBytes: 620 * 1024 * 1024, MaintenanceContext: true},
	}

	thresholds := DefaultMetricThresholds()
	result := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", samples, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if result.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q when only suppressed history spans the active window", result.Transition, TransitionNoop)
	}
	if result.Current != nil {
		t.Fatalf("Current = %#v, want nil when only one unsuppressed sample exists", result.Current)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureUsesSeveritySpecificCPUValidity(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	thresholds := DefaultMetricThresholds()
	cpuSample := func(observedAt time.Time, usage float64, ratesValid bool) MonitoringInstanceResourceSample {
		return MonitoringInstanceResourceSample{
			ObservedAt:    observedAt,
			CPUUsagePct:   usage,
			CPURatesValid: &ratesValid,
		}
	}
	withUnknown30MinuteCPU := []MonitoringInstanceResourceSample{
		cpuSample(now, 85, true),
		cpuSample(now.Add(-8*time.Minute), 85, true),
		cpuSample(now.Add(-15*time.Minute), 85, true),
		cpuSample(now.Add(-20*time.Minute), 85, false),
		cpuSample(now.Add(-30*time.Minute), 85, true),
	}
	without30MinuteHistory := []MonitoringInstanceResourceSample{
		cpuSample(now, 85, true),
		cpuSample(now.Add(-8*time.Minute), 85, true),
		cpuSample(now.Add(-15*time.Minute), 85, true),
	}

	tests := []struct {
		name               string
		previousSeverity   Severity
		samples            []MonitoringInstanceResourceSample
		wantCurrentUpdated bool
	}{
		{
			name:               "notice with invalid 20m sample and valid 30m endpoint",
			previousSeverity:   SeverityNotice,
			samples:            withUnknown30MinuteCPU,
			wantCurrentUpdated: true,
		},
		{
			name:               "alert with invalid 20m sample and valid 30m endpoint",
			previousSeverity:   SeverityAlert,
			samples:            withUnknown30MinuteCPU,
			wantCurrentUpdated: true,
		},
		{
			name:               "critical with invalid 20m sample and valid 30m endpoint",
			previousSeverity:   SeverityCritical,
			samples:            withUnknown30MinuteCPU,
			wantCurrentUpdated: false,
		},
		{
			name:               "notice without 30m history",
			previousSeverity:   SeverityNotice,
			samples:            without30MinuteHistory,
			wantCurrentUpdated: true,
		},
		{
			name:               "alert without 30m history",
			previousSeverity:   SeverityAlert,
			samples:            without30MinuteHistory,
			wantCurrentUpdated: true,
		},
		{
			name:               "critical without 30m history",
			previousSeverity:   SeverityCritical,
			samples:            without30MinuteHistory,
			wantCurrentUpdated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := &IncidentRecord{
				IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_resource_pressure",
				ObjectType:      ObjectTypeMonitoringInstance,
				ObjectID:        "mi_001",
				IncidentClass:   IncidentMonitoringInstanceResourcePressure,
				Severity:        tt.previousSeverity,
				StartedAt:       now.Add(-time.Hour),
				LastEvaluatedAt: now.Add(-time.Minute),
				SourceSummary:   "原始资源摘要",
				Status:          IncidentStatusActive,
			}

			result := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", tt.samples, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
			if result.Transition != TransitionNoop {
				t.Fatalf("Transition = %q, want %q", result.Transition, TransitionNoop)
			}
			if result.Current == nil {
				t.Fatal("Current = nil, want incident state")
			}

			if !tt.wantCurrentUpdated {
				if !reflect.DeepEqual(result.Current, previous) {
					t.Fatalf("Current = %#v, want exact noop(previous) %#v", result.Current, previous)
				}
				return
			}

			if result.Current.Severity != SeverityNotice {
				t.Fatalf("Current.Severity = %q, want %q", result.Current.Severity, SeverityNotice)
			}
			if result.Current.SourceSummary == previous.SourceSummary {
				t.Fatal("Current.SourceSummary retained the obsolete observation despite a complete recovery window")
			}
			if !result.Current.LastEvaluatedAt.Equal(now) {
				t.Fatalf("Current.LastEvaluatedAt = %s, want %s", result.Current.LastEvaluatedAt, now)
			}
			if result.Current.StartedAt != previous.StartedAt {
				t.Fatalf("Current.StartedAt = %s, want previous %s", result.Current.StartedAt, previous.StartedAt)
			}
		})
	}
}

func TestEvaluateMonitoringInstanceResourcePressureCPUValidityAndNonCPUUpgrade(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	thresholds := DefaultMetricThresholds()
	invalidCPU := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 99, CPUIOWaitPct: 99, CPUStealPct: 99, CPURatesValid: new(false)},
		{ObservedAt: now.Add(-8 * time.Minute), CPUUsagePct: 99, CPUIOWaitPct: 99, CPUStealPct: 99, CPURatesValid: new(false)},
		{ObservedAt: now.Add(-15 * time.Minute), CPUUsagePct: 99, CPUIOWaitPct: 99, CPUStealPct: 99, CPURatesValid: new(false)},
	}
	started := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", invalidCPU, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if started.Transition != TransitionNoop || started.Current != nil {
		t.Fatalf("invalid CPU result = %#v, want no CPU incident", started)
	}

	previous := &IncidentRecord{
		IncidentID:    "inc_monitoring_instance_mi_001_monitoring_instance_resource_pressure",
		ObjectType:    ObjectTypeMonitoringInstance,
		ObjectID:      "mi_001",
		IncidentClass: IncidentMonitoringInstanceResourcePressure,
		Severity:      SeverityAlert,
		SourceSummary: "原始资源摘要",
	}
	held := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", invalidCPU, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if held.Transition != TransitionNoop || !reflect.DeepEqual(held.Current, previous) {
		t.Fatalf("invalid CPU recovery result = %#v, want exact noop(previous)", held)
	}

	zeroCPU := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPURatesValid: new(true)},
		{ObservedAt: now.Add(-8 * time.Minute), CPURatesValid: new(true)},
		{ObservedAt: now.Add(-15 * time.Minute), CPURatesValid: new(true)},
	}
	recovered := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", zeroCPU, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if recovered.Transition != TransitionRecovered {
		t.Fatalf("valid zero CPU recovery = %#v, want recovered", recovered)
	}

	badBetweenGood := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 95, CPURatesValid: new(true)},
		{ObservedAt: now.Add(-8 * time.Minute), CPUUsagePct: 95, CPURatesValid: new(false)},
		{ObservedAt: now.Add(-15 * time.Minute), CPUUsagePct: 95, CPURatesValid: new(true)},
	}
	between := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", badBetweenGood, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if between.Transition != TransitionNoop || between.Current != nil {
		t.Fatalf("CPU gap between valid endpoints = %#v, want no CPU incident", between)
	}

	nonCPUAlert := []MonitoringInstanceResourceSample{
		{ObservedAt: now, NormalizedLoad5: 6.2, CPURatesValid: new(false)},
		{ObservedAt: now.Add(-8 * time.Minute), NormalizedLoad5: 6.2, CPURatesValid: new(false)},
		{ObservedAt: now.Add(-15 * time.Minute), NormalizedLoad5: 6.2, CPURatesValid: new(false)},
	}
	nonCPUStarted := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", nonCPUAlert, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if nonCPUStarted.Transition != TransitionStarted || nonCPUStarted.Current == nil || nonCPUStarted.Current.Severity != SeverityAlert {
		t.Fatalf("non-CPU pressure with invalid CPU = %#v, want alert start", nonCPUStarted)
	}
	nonCPUHeld := EvaluateMonitoringInstanceResourcePressure(nonCPUStarted.Current, "mi_001", nonCPUAlert, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if nonCPUHeld.Transition != TransitionNoop || !reflect.DeepEqual(nonCPUHeld.Current, nonCPUStarted.Current) {
		t.Fatalf("same-level non-CPU pressure with invalid CPU = %#v, want exact noop(previous)", nonCPUHeld)
	}
	nonCPUUpgrade := []MonitoringInstanceResourceSample{
		{ObservedAt: now, NormalizedLoad5: 8.5, CPURatesValid: new(false)},
		{ObservedAt: now.Add(-8 * time.Minute), NormalizedLoad5: 8.5, CPURatesValid: new(false)},
		{ObservedAt: now.Add(-15 * time.Minute), NormalizedLoad5: 8.5, CPURatesValid: new(false)},
		{ObservedAt: now.Add(-30 * time.Minute), NormalizedLoad5: 8.5, CPURatesValid: new(false)},
	}
	upgraded := EvaluateMonitoringInstanceResourcePressure(nonCPUStarted.Current, "mi_001", nonCPUUpgrade, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if upgraded.Transition != TransitionEscalated || upgraded.Current == nil || upgraded.Current.Severity != SeverityCritical {
		t.Fatalf("strict non-CPU upgrade with invalid CPU = %#v, want critical escalation", upgraded)
	}
}

func TestEvaluateTargetProbeFailureThresholdsAndRecovery(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	httpFailures := []runtimefacts.ProbeObservation{
		{ObservedAt: now, ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
		{ObservedAt: now.Add(-time.Minute), ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
		{ObservedAt: now.Add(-2 * time.Minute), ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
	}
	started := EvaluateTargetProbeFailure(nil, "tg_001", httpFailures)
	if started.Current == nil || started.Current.Severity != SeverityAlert {
		t.Fatalf("Current = %#v, want alert target probe failure", started.Current)
	}

	previous := started.Current
	recoveries := []runtimefacts.ProbeObservation{
		{ObservedAt: now.Add(time.Minute), ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess},
		{ObservedAt: now, ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess},
	}
	recovered := EvaluateTargetProbeFailure(previous, "tg_001", recoveries)
	if recovered.Transition != TransitionRecovered {
		t.Fatalf("Transition = %q, want %q", recovered.Transition, TransitionRecovered)
	}

	singleSuccess := []runtimefacts.ProbeObservation{
		{ObservedAt: now.Add(time.Minute), ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess},
	}
	noop := EvaluateTargetProbeFailure(previous, "tg_001", singleSuccess)
	if noop.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q after only one success", noop.Transition, TransitionNoop)
	}
	if noop.Current == nil || noop.Current.Severity != previous.Severity {
		t.Fatalf("Current = %#v, want previous incident preserved", noop.Current)
	}

	tcpFailures := []runtimefacts.ProbeObservation{
		{ObservedAt: now, ProbeKind: agentapi.ProbeKindTCP, ResultKind: agentapi.ProbeResultFailure},
		{ObservedAt: now.Add(-time.Minute), ProbeKind: agentapi.ProbeKindTCP, ResultKind: agentapi.ProbeResultFailure},
		{ObservedAt: now.Add(-2 * time.Minute), ProbeKind: agentapi.ProbeKindTCP, ResultKind: agentapi.ProbeResultFailure},
		{ObservedAt: now.Add(-3 * time.Minute), ProbeKind: agentapi.ProbeKindTCP, ResultKind: agentapi.ProbeResultFailure},
		{ObservedAt: now.Add(-4 * time.Minute), ProbeKind: agentapi.ProbeKindTCP, ResultKind: agentapi.ProbeResultFailure},
		{ObservedAt: now.Add(-5 * time.Minute), ProbeKind: agentapi.ProbeKindTCP, ResultKind: agentapi.ProbeResultFailure},
	}
	critical := EvaluateTargetProbeFailure(nil, "tg_001", tcpFailures)
	if critical.Current == nil || critical.Current.Severity != SeverityCritical {
		t.Fatalf("Current = %#v, want critical tcp incident", critical.Current)
	}

	tcpMultiMonitoringInstanceFailures := []runtimefacts.ProbeObservation{
		{ObservedAt: now, MonitoringInstanceID: "mi_001", ProbeItemID: "pb_tcp", ProbeKind: agentapi.ProbeKindTCP, ResultKind: agentapi.ProbeResultFailure},
		{ObservedAt: now.Add(-10 * time.Second), MonitoringInstanceID: "mi_002", ProbeItemID: "pb_tcp", ProbeKind: agentapi.ProbeKindTCP, ResultKind: agentapi.ProbeResultFailure},
	}
	tcpMultiMonitoringInstance := EvaluateTargetProbeFailure(nil, "tg_001", tcpMultiMonitoringInstanceFailures)
	if tcpMultiMonitoringInstance.Current == nil || tcpMultiMonitoringInstance.Current.Severity != SeverityCritical {
		t.Fatalf("Current = %#v, want critical multi-monitoringInstance tcp incident", tcpMultiMonitoringInstance.Current)
	}

	httpMultiProbeFailures := []runtimefacts.ProbeObservation{
		{ObservedAt: now, MonitoringInstanceID: "mi_001", ProbeItemID: "pb_http_1", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultFailure},
		{ObservedAt: now.Add(-10 * time.Second), MonitoringInstanceID: "mi_001", ProbeItemID: "pb_http_2", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultFailure},
	}
	httpMultiProbe := EvaluateTargetProbeFailure(nil, "tg_001", httpMultiProbeFailures)
	if httpMultiProbe.Current == nil || httpMultiProbe.Current.Severity != SeverityCritical {
		t.Fatalf("Current = %#v, want critical multi-probe http incident", httpMultiProbe.Current)
	}
}

func TestEvaluateTargetProbeFailureIgnoresSuppressedHistoryForActiveEvidence(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	recent := []runtimefacts.ProbeObservation{
		{ObservedAt: now, ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
		{ObservedAt: now.Add(-time.Minute), ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503", MaintenanceContext: true},
		{ObservedAt: now.Add(-2 * time.Minute), ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503", MaintenanceContext: true},
	}

	result := EvaluateTargetProbeFailure(nil, "tg_001", recent)
	if result.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q when only suppressed history reaches the failure threshold", result.Transition, TransitionNoop)
	}
	if result.Current != nil {
		t.Fatalf("Current = %#v, want nil when only one unsuppressed failure exists", result.Current)
	}
}

func TestEvaluateTargetTLSExpiryThresholds(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	warningDays := 30
	alertDays := 14
	criticalDays := 2

	warning := EvaluateTargetTLSExpiry(nil, "tg_001", []runtimefacts.ProbeObservation{{ObservedAt: now, ProbeKind: agentapi.ProbeKindTLS, ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &warningDays}})
	if warning.Current == nil || warning.Current.Severity != SeverityNotice {
		t.Fatalf("Current = %#v, want notice TLS incident", warning.Current)
	}
	alert := EvaluateTargetTLSExpiry(warning.Current, "tg_001", []runtimefacts.ProbeObservation{{ObservedAt: now.Add(time.Hour), ProbeKind: agentapi.ProbeKindTLS, ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &alertDays}})
	if alert.Current == nil || alert.Current.Severity != SeverityAlert {
		t.Fatalf("Current = %#v, want alert TLS incident", alert.Current)
	}
	critical := EvaluateTargetTLSExpiry(alert.Current, "tg_001", []runtimefacts.ProbeObservation{{ObservedAt: now.Add(2 * time.Hour), ProbeKind: agentapi.ProbeKindTLS, ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &criticalDays}})
	if critical.Current == nil || critical.Current.Severity != SeverityCritical {
		t.Fatalf("Current = %#v, want critical TLS incident", critical.Current)
	}

	safeDays := 45
	recovered := EvaluateTargetTLSExpiry(critical.Current, "tg_001", []runtimefacts.ProbeObservation{{ObservedAt: now.Add(3 * time.Hour), ProbeKind: agentapi.ProbeKindTLS, ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &safeDays}})
	if recovered.Transition != TransitionRecovered {
		t.Fatalf("Transition = %q, want %q", recovered.Transition, TransitionRecovered)
	}

	missingExpiry := EvaluateTargetTLSExpiry(critical.Current, "tg_001", []runtimefacts.ProbeObservation{{ObservedAt: now.Add(4 * time.Hour), ProbeKind: agentapi.ProbeKindTLS, ResultKind: agentapi.ProbeResultSuccess}})
	if missingExpiry.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q when tls_expiry_days is missing", missingExpiry.Transition, TransitionNoop)
	}
}

func TestMaintenanceAndBackfillSuppressesStartsButAllowsSilentRecovery(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	previous := &IncidentRecord{
		IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_disk_pressure",
		ObjectType:      ObjectTypeMonitoringInstance,
		ObjectID:        "mi_001",
		IncidentClass:   IncidentMonitoringInstanceDiskPressure,
		Severity:        SeverityAlert,
		LastEvaluatedAt: now,
	}

	thresholds := DefaultMetricThresholds()
	skipped := EvaluateMonitoringInstanceDiskPressure(previous, "mi_001", &runtimefacts.HostSample{ObservedAt: now, DiskUsedPct: 99, MaintenanceContext: true}, thresholds)
	if skipped.Transition != TransitionSkipped {
		t.Fatalf("Transition = %q, want %q", skipped.Transition, TransitionSkipped)
	}
	if skipped.Notification != nil {
		t.Fatalf("Notification = %#v, want nil", skipped.Notification)
	}
	if skipped.Current != nil {
		t.Fatalf("Current = %#v, want nil on maintenance short-circuit", skipped.Current)
	}

	recovered := EvaluateMonitoringInstanceDiskPressure(previous, "mi_001", &runtimefacts.HostSample{ObservedAt: now.Add(time.Minute), DiskUsedPct: 40, MaintenanceContext: true}, thresholds)
	if recovered.Transition != TransitionRecovered {
		t.Fatalf("Transition = %q, want %q", recovered.Transition, TransitionRecovered)
	}
	if recovered.Event == nil || recovered.Event.EventType != EventIncidentRecovered {
		t.Fatalf("Event = %#v, want recovered event", recovered.Event)
	}
	if recovered.Notification == nil {
		t.Fatal("Notification = nil, want suppressed recovery notification")
	}
	if recovered.Notification.ShouldSend {
		t.Fatalf("Notification.ShouldSend = %v, want false for maintenance recovery", recovered.Notification.ShouldSend)
	}

	probePrevious := &IncidentRecord{
		IncidentID:      "inc_target_tg_001_target_probe_failure",
		ObjectType:      ObjectTypeTarget,
		ObjectID:        "tg_001",
		IncidentClass:   IncidentTargetProbeFailure,
		Severity:        SeverityAlert,
		LastEvaluatedAt: now,
	}
	backfilledRecovery := []runtimefacts.ProbeObservation{
		{ObservedAt: now.Add(2 * time.Minute), ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess, IsBackfilled: true},
		{ObservedAt: now.Add(time.Minute), ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess, IsBackfilled: true},
	}
	probe := EvaluateTargetProbeFailure(probePrevious, "tg_001", backfilledRecovery)
	if probe.Transition != TransitionRecovered {
		t.Fatalf("Transition = %q, want %q", probe.Transition, TransitionRecovered)
	}
	if probe.Event == nil || probe.Event.EventType != EventIncidentRecovered {
		t.Fatalf("Event = %#v, want recovered event", probe.Event)
	}
	if !probe.Event.IsBackfilled || probe.Event.Provenance != MonitoringEventProvenanceAgentSync || probe.Event.PriorState != "alert" || probe.Event.ResultingState != "normal" {
		t.Fatalf("Event = %#v, want explicit backfilled recovery provenance and states", probe.Event)
	}
	if probe.Notification == nil {
		t.Fatal("Notification = nil, want suppressed recovery notification")
	}
	if probe.Notification.ShouldSend {
		t.Fatalf("Notification.ShouldSend = %v, want false for backfill recovery", probe.Notification.ShouldSend)
	}
}

func TestEmptyInputDoesNotForceRecovery(t *testing.T) {
	targetIncident := &IncidentRecord{ObjectType: ObjectTypeTarget, ObjectID: "tg_001", IncidentClass: IncidentTargetProbeFailure, Severity: SeverityAlert}
	probe := EvaluateTargetProbeFailure(targetIncident, "tg_001", nil)
	if probe.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q for empty probe input", probe.Transition, TransitionNoop)
	}
	if probe.Current == nil {
		t.Fatal("Current = nil, want previous incident preserved on empty input")
	}

	monitoringInstanceIncident := &IncidentRecord{ObjectType: ObjectTypeMonitoringInstance, ObjectID: "mi_001", IncidentClass: IncidentMonitoringInstanceResourcePressure, Severity: SeverityAlert}
	thresholds := DefaultMetricThresholds()
	resource := EvaluateMonitoringInstanceResourcePressure(monitoringInstanceIncident, "mi_001", nil, thresholds, resourcePressureTestPolicy(time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC), 15*time.Minute))
	if resource.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q for empty host input", resource.Transition, TransitionNoop)
	}
}

func TestNormalizeHostSamplesUsesReplaySafeStableLatestOrdering(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	received := observed.Add(time.Minute)
	input := []runtimefacts.HostSample{
		{SyncBatchID: "backfill", ObservedAt: observed, ReceivedAt: received.Add(time.Minute), IsBackfilled: true},
		{SyncBatchID: "live-older-receipt", ObservedAt: observed, ReceivedAt: received},
		{SyncBatchID: "live-newer-receipt-first", ObservedAt: observed, ReceivedAt: received.Add(time.Minute)},
		{SyncBatchID: "live-newer-receipt-second", ObservedAt: observed, ReceivedAt: received.Add(time.Minute)},
		{SyncBatchID: "older-observation", ObservedAt: observed.Add(-time.Minute), ReceivedAt: received.Add(10 * time.Minute)},
	}

	got := normalizeHostSamples(input)
	want := []string{"live-newer-receipt-first", "live-newer-receipt-second", "live-older-receipt", "backfill", "older-observation"}
	for i, batchID := range want {
		if got[i].SyncBatchID != batchID {
			t.Fatalf("normalizeHostSamples()[%d].SyncBatchID = %q, want %q; got %#v", i, got[i].SyncBatchID, batchID, got)
		}
	}
}

func TestNormalizeProbeObservationsUsesReplaySafeStableLatestOrdering(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	received := observed.Add(time.Minute)
	input := []runtimefacts.ProbeObservation{
		{SyncBatchID: "backfill", ObservedAt: observed, ReceivedAt: received.Add(time.Minute), IsBackfilled: true},
		{SyncBatchID: "live-older-receipt", ObservedAt: observed, ReceivedAt: received},
		{SyncBatchID: "live-newer-receipt-first", ObservedAt: observed, ReceivedAt: received.Add(time.Minute)},
		{SyncBatchID: "live-newer-receipt-second", ObservedAt: observed, ReceivedAt: received.Add(time.Minute)},
		{SyncBatchID: "older-observation", ObservedAt: observed.Add(-time.Minute), ReceivedAt: received.Add(10 * time.Minute)},
	}

	got := normalizeProbeObservations(input)
	want := []string{"live-newer-receipt-first", "live-newer-receipt-second", "live-older-receipt", "backfill", "older-observation"}
	for i, batchID := range want {
		if got[i].SyncBatchID != batchID {
			t.Fatalf("normalizeProbeObservations()[%d].SyncBatchID = %q, want %q; got %#v", i, got[i].SyncBatchID, batchID, got)
		}
	}
}

func TestEvaluateMonitoringInstanceHeartbeatMissingDoesNotRecoverWithoutUsableHeartbeatEvidence(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	previous := &IncidentRecord{
		IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_heartbeat_missing",
		ObjectType:      ObjectTypeMonitoringInstance,
		ObjectID:        "mi_001",
		IncidentClass:   IncidentMonitoringInstanceHeartbeatMissing,
		Severity:        SeverityAlert,
		LastEvaluatedAt: now.Add(-time.Minute),
	}

	policy := HeartbeatIncidentPolicy{HeartbeatInterval: time.Minute, MissingThreshold: 3, RecoverySuccesses: 3, RecoveryMaxIntervalGap: 2 * time.Minute}
	nilHeartbeat := EvaluateMonitoringInstanceHeartbeatMissing(previous, "mi_001", now, nil, policy, nil)
	if nilHeartbeat.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q for nil heartbeat", nilHeartbeat.Transition, TransitionNoop)
	}
	if nilHeartbeat.Current == nil || nilHeartbeat.Current.IncidentClass != IncidentMonitoringInstanceHeartbeatMissing {
		t.Fatalf("Current = %#v, want previous heartbeat incident preserved", nilHeartbeat.Current)
	}

	lastHeartbeat := now.Add(-5 * time.Minute)
	policy.HeartbeatInterval = 0
	invalidInterval := EvaluateMonitoringInstanceHeartbeatMissing(previous, "mi_001", now, &lastHeartbeat, policy, nil)
	if invalidInterval.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q for invalid interval", invalidInterval.Transition, TransitionNoop)
	}
	if invalidInterval.Current == nil || invalidInterval.Current.IncidentClass != IncidentMonitoringInstanceHeartbeatMissing {
		t.Fatalf("Current = %#v, want previous heartbeat incident preserved", invalidInterval.Current)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureRequiresRecoveryWindowBeforeClosing(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	previous := &IncidentRecord{
		IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_resource_pressure",
		ObjectType:      ObjectTypeMonitoringInstance,
		ObjectID:        "mi_001",
		IncidentClass:   IncidentMonitoringInstanceResourcePressure,
		Severity:        SeverityAlert,
		LastEvaluatedAt: now.Add(-time.Minute),
	}

	thresholds := DefaultMetricThresholds()
	insufficient := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 20, MemUsedPct: 40, NormalizedLoad5: 0.8},
		{ObservedAt: now.Add(-5 * time.Minute), CPUUsagePct: 22, MemUsedPct: 42, NormalizedLoad5: 0.9},
	}
	result := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", insufficient, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if result.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q for incomplete safe window", result.Transition, TransitionNoop)
	}

	safeWindow := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 20, MemUsedPct: 40, NormalizedLoad5: 0.8},
		{ObservedAt: now.Add(-8 * time.Minute), CPUUsagePct: 22, MemUsedPct: 42, NormalizedLoad5: 0.9},
		{ObservedAt: now.Add(-15 * time.Minute), CPUUsagePct: 24, MemUsedPct: 44, NormalizedLoad5: 1.0},
	}
	recovered := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", safeWindow, thresholds, resourcePressureTestPolicy(now, 15*time.Minute))
	if recovered.Transition != TransitionRecovered {
		t.Fatalf("Transition = %q, want %q for sustained safe window", recovered.Transition, TransitionRecovered)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureCriticalRecoveryRequiresThirtyMinutes(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	previous := &IncidentRecord{
		IncidentID:    "inc_monitoring_instance_mi_001_monitoring_instance_resource_pressure",
		ObjectType:    ObjectTypeMonitoringInstance,
		ObjectID:      "mi_001",
		IncidentClass: IncidentMonitoringInstanceResourcePressure,
		Severity:      SeverityCritical,
		SourceSummary: "原始资源摘要",
		Status:        IncidentStatusActive,
	}
	thresholds := DefaultMetricThresholds()
	policy := resourcePressureTestPolicy(now, 15*time.Minute)
	short := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 20, CPURatesValid: new(true)},
		{ObservedAt: now.Add(-8 * time.Minute), CPUUsagePct: 22, CPURatesValid: new(true)},
		{ObservedAt: now.Add(-15 * time.Minute), CPUUsagePct: 24, CPURatesValid: new(true)},
	}
	held := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", short, thresholds, policy)
	if held.Transition != TransitionNoop || !reflect.DeepEqual(held.Current, previous) {
		t.Fatalf("short critical recovery = %#v, want exact noop(previous)", held)
	}

	full := append(append([]MonitoringInstanceResourceSample{}, short...),
		MonitoringInstanceResourceSample{ObservedAt: now.Add(-22 * time.Minute), CPUUsagePct: 25, CPURatesValid: new(true)},
		MonitoringInstanceResourceSample{ObservedAt: now.Add(-30 * time.Minute), CPUUsagePct: 26, CPURatesValid: new(true)},
	)
	recovered := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", full, thresholds, policy)
	if recovered.Transition != TransitionRecovered {
		t.Fatalf("full critical recovery = %#v, want recovered", recovered)
	}

	full[3].CPURatesValid = new(false)
	invalid := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", full, thresholds, policy)
	if invalid.Transition != TransitionNoop || !reflect.DeepEqual(invalid.Current, previous) {
		t.Fatalf("invalid critical recovery = %#v, want exact noop(previous)", invalid)
	}
}

func TestBuildResourcePressureWindowUsesBoundedCoverage(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	policy := resourcePressureTestPolicy(now, 5*time.Minute)
	gapLimit := 2*policy.SampleInterval + 10*time.Millisecond
	tests := []struct {
		name    string
		samples []MonitoringInstanceResourceSample
		covered bool
	}{
		{
			name: "exact left boundary",
			samples: []MonitoringInstanceResourceSample{
				{ObservedAt: now},
				{ObservedAt: now.Add(-5 * time.Minute)},
				{ObservedAt: now.Add(-15 * time.Minute)},
			},
			covered: true,
		},
		{
			name: "inside boundary without predecessor",
			samples: []MonitoringInstanceResourceSample{
				{ObservedAt: now},
				{ObservedAt: now.Add(-14 * time.Minute)},
			},
		},
		{
			name: "gap equal to G passes",
			samples: []MonitoringInstanceResourceSample{
				{ObservedAt: now},
				{ObservedAt: now.Add(-5 * time.Minute)},
				{ObservedAt: now.Add(-5*time.Minute - gapLimit)},
			},
			covered: true,
		},
		{
			name: "gap greater than G by one nanosecond fails",
			samples: []MonitoringInstanceResourceSample{
				{ObservedAt: now},
				{ObservedAt: now.Add(-5 * time.Minute)},
				{ObservedAt: now.Add(-5*time.Minute - gapLimit - time.Nanosecond)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			window := buildResourcePressureWindow(tt.samples, now, 15*time.Minute, policy, false)
			if window.covered != tt.covered {
				t.Fatalf("covered = %v, want %v; window = %#v", window.covered, tt.covered, window)
			}
		})
	}
}

func TestEvaluateMonitoringInstanceResourcePressureUsesCadenceFreshnessAndSixHourUnknown(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	thresholds := DefaultMetricThresholds()
	dense := denseResourcePressureSamples(now, 5*time.Second+time.Millisecond, 45*time.Minute, 96, 0)

	started := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", dense, thresholds, resourcePressureTestPolicy(now, 5*time.Second))
	if started.Transition != TransitionStarted || started.Current == nil || started.Current.Severity != SeverityCritical {
		t.Fatalf("dense 5s result = %#v, want started critical", started)
	}

	short := denseResourcePressureSamples(now, 5*time.Second+time.Millisecond, 10*time.Minute, 96, 0)
	shortResult := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", short, thresholds, resourcePressureTestPolicy(now, 5*time.Second))
	if shortResult.Transition != TransitionNoop || shortResult.Current != nil {
		t.Fatalf("short window result = %#v, want unknown noop", shortResult)
	}

	stalePolicy := ResourcePressurePolicy{EvaluatedAt: now.Add(11 * time.Second), SampleInterval: 5 * time.Second}
	stale := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", dense, thresholds, stalePolicy)
	if stale.Transition != TransitionNoop || stale.Current != nil {
		t.Fatalf("stale result = %#v, want unknown noop", stale)
	}

	freshPolicy := ResourcePressurePolicy{EvaluatedAt: now.Add(5*time.Second + 100*time.Millisecond), SampleInterval: 5 * time.Second}
	fresh := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", dense, thresholds, freshPolicy)
	if fresh.Transition != TransitionStarted || fresh.Current == nil {
		t.Fatalf("fresh result = %#v, want evaluable result", fresh)
	}
	oneMinute := denseResourcePressureSamples(now, time.Minute+time.Millisecond, 16*time.Minute, 96, 0)
	oneMinuteResult := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", oneMinute, thresholds, resourcePressureTestPolicy(now, time.Minute))
	if oneMinuteResult.Transition != TransitionStarted || oneMinuteResult.Current == nil || oneMinuteResult.Current.Severity != SeverityAlert {
		t.Fatalf("1m result = %#v, want started alert", oneMinuteResult)
	}

	sixHour := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", dense, thresholds, resourcePressureTestPolicy(now, 6*time.Hour))
	if sixHour.Transition != TransitionNoop || sixHour.Current != nil {
		t.Fatalf("6h result = %#v, want unknown noop", sixHour)
	}
	futureSamples := append([]MonitoringInstanceResourceSample{{ObservedAt: now.Add(time.Second), CPUUsagePct: 96, CPURatesValid: new(true)}}, dense...)
	future := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", futureSamples, thresholds, resourcePressureTestPolicy(now, 5*time.Second))
	if future.Transition != TransitionNoop || future.Current != nil {
		t.Fatalf("future-anchor result = %#v, want unknown noop", future)
	}

}

func TestEvaluateMonitoringInstanceResourcePressureSingletonNeedsBoundarySupport(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	thresholds := DefaultMetricThresholds()
	policy := resourcePressureTestPolicy(now, 15*time.Minute)
	high := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 95, CPURatesValid: new(true)},
		{ObservedAt: now.Add(-20 * time.Minute), CPUUsagePct: 95, CPURatesValid: new(true)},
	}
	started := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", high, thresholds, policy)
	if started.Transition != TransitionStarted || started.Current == nil || started.Current.Severity != SeverityAlert {
		t.Fatalf("high singleton result = %#v, want alert start", started)
	}

	lowBoundary := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 95, CPURatesValid: new(true)},
		{ObservedAt: now.Add(-20 * time.Minute), CPUUsagePct: 20, CPURatesValid: new(true)},
	}
	lowBoundaryResult := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", lowBoundary, thresholds, policy)
	if lowBoundaryResult.Transition != TransitionNoop || lowBoundaryResult.Current != nil {
		t.Fatalf("low-boundary singleton result = %#v, want noop", lowBoundaryResult)
	}

	previous := started.Current
	lowCurrent := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 20, CPURatesValid: new(true)},
		{ObservedAt: now.Add(-20 * time.Minute), CPUUsagePct: 95, CPURatesValid: new(true)},
	}
	held := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", lowCurrent, thresholds, policy)
	if held.Transition != TransitionNoop || !reflect.DeepEqual(held.Current, previous) {
		t.Fatalf("high-boundary recovery result = %#v, want exact noop(previous)", held)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureChoosesEligibleNonCPUEscalation(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	thresholds := DefaultMetricThresholds()
	tests := []struct {
		name             string
		previousSeverity Severity
		cpuUsage         float64
		load5            float64
		wantSeverity     Severity
	}{
		{name: "alert to critical load", previousSeverity: SeverityAlert, cpuUsage: 96, load5: 8.5, wantSeverity: SeverityCritical},
		{name: "notice to alert load", previousSeverity: SeverityNotice, cpuUsage: 92, load5: 6.5, wantSeverity: SeverityAlert},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			samples := denseResourcePressureSamples(now, time.Minute, 30*time.Minute, tt.cpuUsage, 0)
			for index := range samples {
				samples[index].NormalizedLoad5 = tt.load5
			}
			samples = append(samples, MonitoringInstanceResourceSample{
				ObservedAt:      now.Add(-5 * time.Minute),
				CPUUsagePct:     tt.cpuUsage,
				NormalizedLoad5: tt.load5,
				CPURatesValid:   new(false),
				IsBackfilled:    true,
			})
			previous := &IncidentRecord{
				IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_resource_pressure",
				ObjectType:      ObjectTypeMonitoringInstance,
				ObjectID:        "mi_001",
				IncidentClass:   IncidentMonitoringInstanceResourcePressure,
				Severity:        tt.previousSeverity,
				StartedAt:       now.Add(-time.Hour),
				LastEvaluatedAt: now.Add(-time.Minute),
				SourceSummary:   "previous pressure",
				Status:          IncidentStatusActive,
			}
			result := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", samples, thresholds, resourcePressureTestPolicy(now, time.Minute))
			if result.Transition != TransitionEscalated || result.Current == nil || result.Current.Severity != tt.wantSeverity {
				t.Fatalf("result = %#v, want non-CPU escalation to %s", result, tt.wantSeverity)
			}
			if !strings.Contains(result.Current.SourceSummary, "Load5") {
				t.Fatalf("Current.SourceSummary = %q, want load-driven summary", result.Current.SourceSummary)
			}
			if result.Event == nil || result.Event.EventType != EventIncidentEscalated || result.Event.Severity != tt.wantSeverity {
				t.Fatalf("Event = %#v, want escalated event at %s", result.Event, tt.wantSeverity)
			}
			if result.Notification == nil || !result.Notification.ShouldSend || result.Notification.Reason != NotificationReasonEscalated || result.Notification.Severity != tt.wantSeverity {
				t.Fatalf("Notification = %#v, want escalated notification at %s", result.Notification, tt.wantSeverity)
			}
			if result.Current.StartedAt != previous.StartedAt {
				t.Fatalf("Current.StartedAt = %s, want previous %s", result.Current.StartedAt, previous.StartedAt)
			}
		})
	}
}

func TestEvaluateMonitoringInstanceResourcePressureDowngradeChecksThirtyMinuteSingletonBoundary(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	previous := &IncidentRecord{
		IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_resource_pressure",
		ObjectType:      ObjectTypeMonitoringInstance,
		ObjectID:        "mi_001",
		IncidentClass:   IncidentMonitoringInstanceResourcePressure,
		Severity:        SeverityAlert,
		StartedAt:       now.Add(-time.Hour),
		LastEvaluatedAt: now.Add(-time.Minute),
		SourceSummary:   "previous alert",
		Status:          IncidentStatusActive,
	}
	samples := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 85, CPUStealPct: 0, CPURatesValid: new(true)},
		{ObservedAt: now.Add(-30*time.Minute - time.Millisecond), CPUUsagePct: 85, CPUStealPct: 12, CPURatesValid: new(true)},
	}
	result := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", samples, DefaultMetricThresholds(), resourcePressureTestPolicy(now, 15*time.Minute))
	if result.Transition != TransitionNoop || !reflect.DeepEqual(result.Current, previous) {
		t.Fatalf("result = %#v, want exact noop(previous) when 30m boundary supports alert", result)
	}
	if result.Event != nil || result.Notification != nil {
		t.Fatalf("Event = %#v, Notification = %#v, want no downgrade event or notification", result.Event, result.Notification)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureChecksSingletonRawRecoveryAndBoundaryEvidence(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	previous := &IncidentRecord{
		IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_resource_pressure",
		ObjectType:      ObjectTypeMonitoringInstance,
		ObjectID:        "mi_001",
		IncidentClass:   IncidentMonitoringInstanceResourcePressure,
		Severity:        SeverityCritical,
		StartedAt:       now.Add(-time.Hour),
		LastEvaluatedAt: now.Add(-time.Minute),
		SourceSummary:   "previous critical",
		Status:          IncidentStatusActive,
	}
	tests := []struct {
		name        string
		currentCPU  float64
		previousCPU float64
	}{
		{name: "notice current with critical predecessor", currentCPU: 85, previousCPU: 96},
		{name: "safe current with critical predecessor", currentCPU: 10, previousCPU: 96},
		{name: "critical current with safe predecessor", currentCPU: 96, previousCPU: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			samples := []MonitoringInstanceResourceSample{
				{ObservedAt: now, CPUUsagePct: tt.currentCPU, CPURatesValid: new(true)},
				{ObservedAt: now.Add(-30*time.Minute - time.Millisecond), CPUUsagePct: tt.previousCPU, CPURatesValid: new(true)},
			}
			result := EvaluateMonitoringInstanceResourcePressure(previous, "mi_001", samples, DefaultMetricThresholds(), resourcePressureTestPolicy(now, 15*time.Minute))
			if result.Transition != TransitionNoop || !reflect.DeepEqual(result.Current, previous) {
				t.Fatalf("result = %#v, want exact noop(previous)", result)
			}
			if result.Event != nil || result.Notification != nil {
				t.Fatalf("Event = %#v, Notification = %#v, want none", result.Event, result.Notification)
			}
		})
	}
}

func TestEvaluateMonitoringInstanceResourcePressureSingletonMemoryNeedsAvailableMemoryOnBothSides(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	tests := []struct {
		name              string
		currentAvailable  int64
		previousAvailable int64
	}{
		{name: "current low predecessor high", currentAvailable: 400 * 1024 * 1024, previousAvailable: 800 * 1024 * 1024},
		{name: "current high predecessor low", currentAvailable: 800 * 1024 * 1024, previousAvailable: 400 * 1024 * 1024},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			samples := []MonitoringInstanceResourceSample{
				{ObservedAt: now, MemUsedPct: 96, MemAvailableBytes: tt.currentAvailable, CPURatesValid: new(true)},
				{ObservedAt: now.Add(-30*time.Minute - time.Millisecond), MemUsedPct: 96, MemAvailableBytes: tt.previousAvailable, CPURatesValid: new(true)},
			}
			result := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", samples, DefaultMetricThresholds(), resourcePressureTestPolicy(now, 15*time.Minute))
			if result.Transition != TransitionStarted || result.Current == nil || result.Current.Severity != SeverityAlert {
				t.Fatalf("result = %#v, want alert but not critical", result)
			}
		})
	}
}

func TestEvaluateMonitoringInstanceResourcePressureExcludesOutsidePredecessorFromStatistics(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	samples := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 20, MemUsedPct: 94, MemAvailableBytes: 700 * 1024 * 1024, CPURatesValid: new(true)},
		{ObservedAt: now.Add(-8 * time.Minute), CPUUsagePct: 20, MemUsedPct: 94, MemAvailableBytes: 700 * 1024 * 1024, CPURatesValid: new(true)},
		{ObservedAt: now.Add(-15 * time.Minute), CPUUsagePct: 20, MemUsedPct: 94, MemAvailableBytes: 700 * 1024 * 1024, CPURatesValid: new(true)},
		{ObservedAt: now.Add(-30*time.Minute - time.Millisecond), CPUUsagePct: 99, MemUsedPct: 100, MemAvailableBytes: 1, CPURatesValid: new(true)},
	}
	result := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", samples, DefaultMetricThresholds(), resourcePressureTestPolicy(now, 15*time.Minute))
	if result.Transition != TransitionStarted || result.Current == nil || result.Current.Severity != SeverityAlert {
		t.Fatalf("result = %#v, want in-window alert without predecessor contamination", result)
	}
	if !strings.Contains(result.Current.SourceSummary, "内存连续 15m") {
		t.Fatalf("SourceSummary = %q, want 15m in-window memory summary", result.Current.SourceSummary)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureDuplicateCPUValidityAndCoverage(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	dense := denseResourcePressureSamples(now, time.Minute, 30*time.Minute, 96, 0)
	dense = append(dense, MonitoringInstanceResourceSample{
		ObservedAt:    now.Add(-5 * time.Minute),
		CPUUsagePct:   96,
		CPURatesValid: new(false),
	})
	result := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", dense, DefaultMetricThresholds(), resourcePressureTestPolicy(now, time.Minute))
	if result.Transition != TransitionNoop || result.Current != nil {
		t.Fatalf("duplicate false CPU result = %#v, want no CPU incident", result)
	}

	duplicatesOnly := []MonitoringInstanceResourceSample{
		{ObservedAt: now, CPUUsagePct: 96, CPURatesValid: new(true)},
		{ObservedAt: now, CPUUsagePct: 96, CPURatesValid: new(true)},
	}
	duplicateOnlyResult := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", duplicatesOnly, DefaultMetricThresholds(), resourcePressureTestPolicy(now, 15*time.Minute))
	if duplicateOnlyResult.Transition != TransitionNoop || duplicateOnlyResult.Current != nil {
		t.Fatalf("duplicate-only timestamp result = %#v, want no incident", duplicateOnlyResult)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureOrdersCriticalIowaitAfterCPU(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	thresholds := DefaultMetricThresholds()
	samples := make([]MonitoringInstanceResourceSample, 0, 7)
	for index := 0; index <= 6; index++ {
		samples = append(samples, MonitoringInstanceResourceSample{
			ObservedAt:        now.Add(-time.Duration(index) * 5 * time.Minute),
			CPUUsagePct:       20,
			CPUIOWaitPct:      60,
			MemUsedPct:        93,
			MemAvailableBytes: 700 * 1024 * 1024,
			CPURatesValid:     new(true),
		})
	}
	iowait := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", samples, thresholds, resourcePressureTestPolicy(now, 5*time.Minute))
	if iowait.Current == nil || iowait.Current.Severity != SeverityCritical || !strings.Contains(iowait.Current.SourceSummary, "iowait") {
		t.Fatalf("iowait result = %#v, want iowait critical summary", iowait.Current)
	}

	for index := range samples {
		samples[index].CPUUsagePct = 96
	}
	cpu := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", samples, thresholds, resourcePressureTestPolicy(now, 5*time.Minute))
	if cpu.Current == nil || cpu.Current.Severity != SeverityCritical || !strings.Contains(cpu.Current.SourceSummary, "CPU") {
		t.Fatalf("cpu result = %#v, want CPU critical summary", cpu.Current)
	}
}

func TestEvaluateMonitoringInstanceResourcePressureSuppressedTimestampBreaksCoverage(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	samples := []MonitoringInstanceResourceSample{
		{ObservedAt: now, NormalizedLoad5: 6.5},
		{ObservedAt: now.Add(-5 * time.Minute), NormalizedLoad5: 6.5, MaintenanceContext: true},
		{ObservedAt: now.Add(-10 * time.Minute), NormalizedLoad5: 6.5},
		{ObservedAt: now.Add(-15 * time.Minute), NormalizedLoad5: 6.5},
	}
	result := EvaluateMonitoringInstanceResourcePressure(nil, "mi_001", samples, DefaultMetricThresholds(), resourcePressureTestPolicy(now, 5*time.Minute))
	if result.Transition != TransitionNoop || result.Current != nil {
		t.Fatalf("suppressed-gap result = %#v, want noop without bridged coverage", result)
	}
}

func TestEvaluateMonitoringInstanceTrendDegradationStartsAndEscalates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	started := EvaluateMonitoringInstanceTrendDegradation(nil, "mi_001",
		nodeTrendSamples(now, []float64{1.7, 1.8, 1.9}, []float64{4, 4, 4}, []float64{0.8, 0.9, 0.8}),
		[]MonitoringInstanceHostDailyAggregate{{BucketDate: now.AddDate(0, 0, -1), SampleCount: 288, AvgLoad5: 0.8, AvgCPUIOWaitPct: new(float64(2)), AvgCPUStealPct: new(0.5)}},
	)
	if started.Transition != TransitionStarted {
		t.Fatalf("Transition = %q, want %q", started.Transition, TransitionStarted)
	}
	if started.Current == nil || started.Current.IncidentClass != IncidentMonitoringInstanceTrendDegradation {
		t.Fatalf("Current = %#v, want monitoringInstance trend incident", started.Current)
	}
	if started.Current.Severity != SeverityNotice {
		t.Fatalf("Severity = %q, want %q", started.Current.Severity, SeverityNotice)
	}
	if started.Current.Severity == SeverityCritical {
		t.Fatal("trend degradation must not emit critical severity")
	}

	escalated := EvaluateMonitoringInstanceTrendDegradation(started.Current, "mi_001",
		nodeTrendSamples(now.Add(30*time.Minute), []float64{1.9, 2.0, 2.1}, []float64{11, 12, 13}, []float64{0.8, 0.9, 0.8}),
		[]MonitoringInstanceHostDailyAggregate{{BucketDate: now.AddDate(0, 0, -1), SampleCount: 288, AvgLoad5: 0.8, AvgCPUIOWaitPct: new(float64(2)), AvgCPUStealPct: new(0.5)}},
	)
	if escalated.Transition != TransitionEscalated {
		t.Fatalf("Transition = %q, want %q", escalated.Transition, TransitionEscalated)
	}
	if escalated.Current == nil || escalated.Current.Severity != SeverityAlert {
		t.Fatalf("Current = %#v, want alert trend incident", escalated.Current)
	}
	if escalated.Current.Severity == SeverityCritical {
		t.Fatal("trend degradation must not escalate to critical")
	}
}

func TestEvaluateMonitoringInstanceTrendDegradationSkipsSuppressedStartsAndRecoversConservatively(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	baselines := []MonitoringInstanceHostDailyAggregate{{BucketDate: now.AddDate(0, 0, -1), SampleCount: 288, AvgLoad5: 0.8, AvgCPUIOWaitPct: new(float64(2)), AvgCPUStealPct: new(0.5)}}
	previous := &IncidentRecord{IncidentID: "inc_monitoring_instance_mi_001_monitoring_instance_trend_degradation", ObjectType: ObjectTypeMonitoringInstance, ObjectID: "mi_001", IncidentClass: IncidentMonitoringInstanceTrendDegradation, Severity: SeverityAlert, StartedAt: now.Add(-24 * time.Hour), LastEvaluatedAt: now.Add(-time.Hour)}

	suppressed := EvaluateMonitoringInstanceTrendDegradation(nil, "mi_001",
		[]MonitoringInstanceResourceSample{
			{ObservedAt: now, NormalizedLoad5: 2.0, CPUIOWaitPct: 12, MaintenanceContext: true},
			{ObservedAt: now.Add(-10 * time.Minute), NormalizedLoad5: 2.0, CPUIOWaitPct: 12, MaintenanceContext: true},
			{ObservedAt: now.Add(-20 * time.Minute), NormalizedLoad5: 2.0, CPUIOWaitPct: 12, MaintenanceContext: true},
		},
		baselines,
	)
	if suppressed.Transition != TransitionSkipped {
		t.Fatalf("Transition = %q, want %q", suppressed.Transition, TransitionSkipped)
	}

	latestSuppressed := EvaluateMonitoringInstanceTrendDegradation(nil, "mi_001",
		[]MonitoringInstanceResourceSample{
			{ObservedAt: now, NormalizedLoad5: 0.7, CPUIOWaitPct: 2, IsBackfilled: true},
			{ObservedAt: now.Add(-10 * time.Minute), NormalizedLoad5: 2.0, CPUIOWaitPct: 12},
			{ObservedAt: now.Add(-20 * time.Minute), NormalizedLoad5: 2.0, CPUIOWaitPct: 12},
			{ObservedAt: now.Add(-30 * time.Minute), NormalizedLoad5: 2.0, CPUIOWaitPct: 12},
		},
		baselines,
	)
	if latestSuppressed.Transition != TransitionSkipped {
		t.Fatalf("Transition = %q, want %q when newest trend sample is suppressed", latestSuppressed.Transition, TransitionSkipped)
	}

	insufficientSafe := EvaluateMonitoringInstanceTrendDegradation(previous, "mi_001",
		nodeTrendSamples(now, []float64{0.7, 0.8}, []float64{2, 2}, []float64{0.4, 0.4}),
		baselines,
	)
	if insufficientSafe.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q", insufficientSafe.Transition, TransitionNoop)
	}
	if insufficientSafe.Current == nil || insufficientSafe.Current.IncidentClass != IncidentMonitoringInstanceTrendDegradation {
		t.Fatalf("Current = %#v, want previous incident preserved", insufficientSafe.Current)
	}

	briefSafe := EvaluateMonitoringInstanceTrendDegradation(previous, "mi_001",
		nodeTrendSamples(now.Add(time.Hour), []float64{0.7, 0.8, 0.9}, []float64{2, 2, 2}, []float64{0.4, 0.4, 0.4}),
		baselines,
	)
	if briefSafe.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q for a short safe trend window", briefSafe.Transition, TransitionNoop)
	}

	recovered := EvaluateMonitoringInstanceTrendDegradation(previous, "mi_001",
		nodeTrendSamples(now.Add(time.Hour), []float64{0.7, 0.8, 0.9, 0.8}, []float64{2, 2, 2, 2}, []float64{0.4, 0.4, 0.4, 0.4}),
		baselines,
	)
	if recovered.Transition != TransitionRecovered {
		t.Fatalf("Transition = %q, want %q", recovered.Transition, TransitionRecovered)
	}
	if recovered.Notification == nil || recovered.Notification.Reason != NotificationReasonRecovered {
		t.Fatalf("Notification = %#v, want recovered notification", recovered.Notification)
	}
}

func TestEvaluateMonitoringInstanceTrendDegradationPrefersLiveSampleAtEqualObservedTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	baselines := []MonitoringInstanceHostDailyAggregate{{
		BucketDate: now.AddDate(0, 0, -1), SampleCount: 288, AvgLoad5: 0.8, AvgCPUIOWaitPct: new(float64(2)), AvgCPUStealPct: new(0.5),
	}}

	result := EvaluateMonitoringInstanceTrendDegradation(nil, "mi_001",
		[]MonitoringInstanceResourceSample{
			{ObservedAt: now, NormalizedLoad5: 0.7, CPUIOWaitPct: 2, IsBackfilled: true},
			{ObservedAt: now, NormalizedLoad5: 2.0, CPUIOWaitPct: 12},
			{ObservedAt: now.Add(-10 * time.Minute), NormalizedLoad5: 2.0, CPUIOWaitPct: 12},
			{ObservedAt: now.Add(-20 * time.Minute), NormalizedLoad5: 2.0, CPUIOWaitPct: 12},
		},
		baselines,
	)

	if result.Transition != TransitionStarted {
		t.Fatalf("Transition = %q, want %q when equal-time live evidence outranks backfill", result.Transition, TransitionStarted)
	}
}
func TestEvaluateMonitoringInstanceTrendCPUValidityPreservesStateAndAllowsValidZeroRecovery(t *testing.T) {
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	baselines := []MonitoringInstanceHostDailyAggregate{{
		BucketDate: now.AddDate(0, 0, -1), SampleCount: 288, AvgLoad5: 0.8, AvgCPUIOWaitPct: new(float64(2)), AvgCPUStealPct: new(0.5),
	}}
	previous := &IncidentRecord{
		IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_trend_degradation",
		ObjectType:      ObjectTypeMonitoringInstance,
		ObjectID:        "mi_001",
		IncidentClass:   IncidentMonitoringInstanceTrendDegradation,
		Severity:        SeverityNotice,
		SourceSummary:   "原始趋势摘要",
		StartedAt:       now.Add(-time.Hour),
		LastEvaluatedAt: now.Add(-time.Minute),
	}

	invalidCPU := nodeTrendSamples(now, []float64{0.7, 0.8, 0.9, 0.8}, []float64{2, 2, 2, 2}, []float64{0.4, 0.4, 0.4, 0.4})
	for i := range invalidCPU {
		invalidCPU[i].CPURatesValid = new(false)
	}
	held := EvaluateMonitoringInstanceTrendDegradation(previous, "mi_001", invalidCPU, baselines)
	if held.Transition != TransitionNoop || !reflect.DeepEqual(held.Current, previous) {
		t.Fatalf("invalid CPU trend result = %#v, want exact noop(previous)", held)
	}

	badBetweenGood := nodeTrendSamples(now, []float64{0.7, 0.8, 0.9, 0.8}, []float64{2, 2, 2, 2}, []float64{0.4, 0.4, 0.4, 0.4})
	badBetweenGood[0].CPURatesValid = new(true)
	badBetweenGood[1].CPURatesValid = new(false)
	badBetweenGood[2].CPURatesValid = new(true)
	badBetweenGood[3].CPURatesValid = new(true)
	between := EvaluateMonitoringInstanceTrendDegradation(previous, "mi_001", badBetweenGood, baselines)
	if between.Transition != TransitionNoop || !reflect.DeepEqual(between.Current, previous) {
		t.Fatalf("CPU gap between valid trend endpoints = %#v, want exact noop(previous)", between)
	}

	zeroCPU := nodeTrendSamples(now, []float64{0.7, 0.8, 0.9, 0.8}, []float64{0, 0, 0, 0}, []float64{0, 0, 0, 0})
	for i := range zeroCPU {
		zeroCPU[i].CPURatesValid = new(true)
	}
	recovered := EvaluateMonitoringInstanceTrendDegradation(previous, "mi_001", zeroCPU, baselines)
	if recovered.Transition != TransitionRecovered {
		t.Fatalf("valid zero CPU trend recovery = %#v, want recovered", recovered)
	}
	noCPUBaseline := []MonitoringInstanceHostDailyAggregate{{SampleCount: 288, AvgLoad5: 0.8}}
	unknownBaseline := EvaluateMonitoringInstanceTrendDegradation(previous, "mi_001", zeroCPU, noCPUBaseline)
	if unknownBaseline.Transition != TransitionNoop || !reflect.DeepEqual(unknownBaseline.Current, previous) {
		t.Fatalf("missing CPU trend baseline = %#v, want exact noop(previous)", unknownBaseline)
	}
}

func TestWeightedMonitoringInstanceTrendBaselinesUseIndependentCPUCountsAndLegacyFallback(t *testing.T) {
	baselines := []MonitoringInstanceHostDailyAggregate{
		{
			SampleCount:                    100,
			AvgLoad5:                       1,
			AvgCPUIOWaitPct:                new(float64(2)),
			AvgCPUStealPct:                 new(0.5),
			CPUValidSampleCount:            new(1),
			CPUValidBackfilledSampleCount:  new(0),
			CPUValidMaintenanceSampleCount: new(0),
		},
		{
			SampleCount:                    100,
			AvgLoad5:                       3,
			AvgCPUIOWaitPct:                new(float64(10)),
			AvgCPUStealPct:                 new(1.5),
			CPUValidSampleCount:            new(9),
			CPUValidBackfilledSampleCount:  new(0),
			CPUValidMaintenanceSampleCount: new(0),
		},
	}
	load, iowait, steal := weightedMonitoringInstanceTrendBaselines(baselines)
	if load == nil || *load != 2 {
		t.Fatalf("load baseline = %v, want host-weighted 2", load)
	}
	if iowait == nil || *iowait != 9.2 {
		t.Fatalf("iowait baseline = %v, want CPU-count-weighted 9.2", iowait)
	}
	if steal == nil || *steal != 1.4 {
		t.Fatalf("steal baseline = %v, want CPU-count-weighted 1.4", steal)
	}

	legacy := []MonitoringInstanceHostDailyAggregate{{
		SampleCount:                    15,
		BackfilledSampleCount:          10,
		MaintenanceSampleCount:         10,
		AvgLoad5:                       0.8,
		AvgCPUIOWaitPct:                new(float64(2)),
		AvgCPUStealPct:                 new(0.5),
		CPUValidSampleCount:            new(5),
		CPUValidBackfilledSampleCount:  new(0),
		CPUValidMaintenanceSampleCount: new(0),
	}}
	legacyLoad, legacyIOWait, legacySteal := weightedMonitoringInstanceTrendBaselines(legacy)
	if legacyLoad != nil {
		t.Fatalf("load baseline = %v, want nil when host weight is non-positive", legacyLoad)
	}
	if legacyIOWait == nil || *legacyIOWait != 2 || legacySteal == nil || *legacySteal != 0.5 {
		t.Fatalf("independent CPU baselines = %v/%v, want usable CPU day despite host weight", legacyIOWait, legacySteal)
	}

	legacyFallback := []MonitoringInstanceHostDailyAggregate{{
		SampleCount:            15,
		BackfilledSampleCount:  10,
		MaintenanceSampleCount: 0,
		AvgCPUIOWaitPct:        new(float64(4)),
		AvgCPUStealPct:         new(float64(1)),
	}}
	_, fallbackIOWait, fallbackSteal := weightedMonitoringInstanceTrendBaselines(legacyFallback)
	if fallbackIOWait == nil || *fallbackIOWait != 4 || fallbackSteal == nil || *fallbackSteal != 1 {
		t.Fatalf("legacy CPU count fallback = %v/%v, want legacy sample counts", fallbackIOWait, fallbackSteal)
	}

	for _, invalid := range []float64{-1, 101, math.NaN(), math.Inf(1), math.Inf(-1)} {
		t.Run("invalid", func(t *testing.T) {
			baselines := []MonitoringInstanceHostDailyAggregate{{
				SampleCount:                    10,
				AvgCPUIOWaitPct:                new(invalid),
				AvgCPUStealPct:                 new(float64(1)),
				CPUValidSampleCount:            new(10),
				CPUValidBackfilledSampleCount:  new(0),
				CPUValidMaintenanceSampleCount: new(0),
			}}
			_, gotIOWait, gotSteal := weightedMonitoringInstanceTrendBaselines(baselines)
			if gotIOWait != nil || gotSteal == nil {
				t.Fatalf("CPU averages = %v/%v for %v, want iowait nil and valid steal", gotIOWait, gotSteal, invalid)
			}
		})
	}
}

func TestNormalizeMonitoringInstanceResourceSamplesUsesReplaySafeStableOrdering(t *testing.T) {
	t.Parallel()
	observedAt := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	receivedBase := observedAt.Add(time.Minute)

	got := normalizeMonitoringInstanceResourceSamples([]MonitoringInstanceResourceSample{
		{ObservedAt: observedAt, ReceivedAt: receivedBase, CPUUsagePct: 1},
		{ObservedAt: observedAt, ReceivedAt: receivedBase.Add(9 * time.Minute), CPUUsagePct: 2, IsBackfilled: true},
		{ObservedAt: observedAt, ReceivedAt: receivedBase.Add(2 * time.Minute), CPUUsagePct: 3},
		{ObservedAt: observedAt, ReceivedAt: receivedBase.Add(time.Minute), CPUUsagePct: 4},
		{ObservedAt: observedAt, ReceivedAt: receivedBase.Add(time.Minute), CPUUsagePct: 5},
	})

	want := []float64{3, 4, 5, 1, 2}
	for i := range want {
		if got[i].CPUUsagePct != want[i] {
			t.Fatalf("normalized CPU markers = %#v, want %#v", []float64{got[0].CPUUsagePct, got[1].CPUUsagePct, got[2].CPUUsagePct, got[3].CPUUsagePct, got[4].CPUUsagePct}, want)
		}
	}
}

func TestEvaluateTargetLatencyTrendStartsAndEscalatesWithoutCritical(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	baselines := []TargetProbeDailyAggregate{{TargetID: "tg_001", ProbeItemID: "pb_http_1", BucketDate: now.AddDate(0, 0, -1), ObservationCount: 96, SuccessCount: 96, AvgLatencyMS: new(float64(120))}}
	started := EvaluateTargetLatencyTrendDegradationAcrossSeries(nil, "tg_001",
		[]runtimefacts.ProbeObservation{
			targetLatencyObservation(now, "mi_001", "pb_http_1", 330),
			targetLatencyObservation(now.Add(-10*time.Minute), "mi_001", "pb_http_1", 340),
			targetLatencyObservation(now.Add(-20*time.Minute), "mi_001", "pb_http_1", 350),
		},
		baselines,
	)
	if started.Transition != TransitionStarted {
		t.Fatalf("Transition = %q, want %q", started.Transition, TransitionStarted)
	}
	if started.Current == nil || started.Current.IncidentClass != IncidentTargetLatencyTrendDegradation {
		t.Fatalf("Current = %#v, want target latency trend incident", started.Current)
	}
	if started.Current.Severity != SeverityNotice {
		t.Fatalf("Severity = %q, want %q", started.Current.Severity, SeverityNotice)
	}

	escalated := EvaluateTargetLatencyTrendDegradationAcrossSeries(started.Current, "tg_001",
		[]runtimefacts.ProbeObservation{
			targetLatencyObservation(now.Add(time.Hour), "mi_001", "pb_http_1", 360),
			targetLatencyObservation(now.Add(50*time.Minute), "mi_001", "pb_http_1", 340),
			targetLatencyObservation(now.Add(40*time.Minute), "mi_001", "pb_http_1", 350),
			targetLatencyObservation(now.Add(time.Hour), "mi_002", "pb_http_1", 365),
			targetLatencyObservation(now.Add(50*time.Minute), "mi_002", "pb_http_1", 345),
			targetLatencyObservation(now.Add(40*time.Minute), "mi_002", "pb_http_1", 355),
		},
		baselines,
	)
	if escalated.Transition != TransitionEscalated {
		t.Fatalf("Transition = %q, want %q", escalated.Transition, TransitionEscalated)
	}
	if escalated.Current == nil || escalated.Current.Severity != SeverityAlert {
		t.Fatalf("Current = %#v, want alert target latency trend incident", escalated.Current)
	}
	if escalated.Current.Severity == SeverityCritical {
		t.Fatal("target latency trend must not emit critical severity")
	}

	mixedContributors := EvaluateTargetLatencyTrendDegradationAcrossSeries(nil, "tg_001",
		[]runtimefacts.ProbeObservation{
			targetLatencyObservation(now.Add(2*time.Hour), "mi_001", "pb_http_1", 360),
			targetLatencyObservation(now.Add(110*time.Minute), "mi_002", "pb_http_1", 340),
			targetLatencyObservation(now.Add(100*time.Minute), "mi_003", "pb_http_1", 350),
		},
		baselines,
	)
	if mixedContributors.Current == nil || mixedContributors.Current.Severity != SeverityNotice {
		t.Fatalf("Current = %#v, want notice when one degraded aggregate lacks multiple degraded monitoringInstance perspectives", mixedContributors.Current)
	}
}

func TestEvaluateTargetLatencyTrendSkipsSuppressedStartsAndRecoversConservatively(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	baselines := []TargetProbeDailyAggregate{{TargetID: "tg_001", ProbeItemID: "pb_http_1", BucketDate: now.AddDate(0, 0, -1), ObservationCount: 96, SuccessCount: 96, AvgLatencyMS: new(float64(100))}}
	previous := &IncidentRecord{IncidentID: "inc_target_tg_001_target_latency_trend_degradation", ObjectType: ObjectTypeTarget, ObjectID: "tg_001", IncidentClass: IncidentTargetLatencyTrendDegradation, Severity: SeverityNotice, StartedAt: now.Add(-24 * time.Hour), LastEvaluatedAt: now.Add(-time.Hour)}

	suppressed := EvaluateTargetLatencyTrendDegradationAcrossSeries(nil, "tg_001",
		[]runtimefacts.ProbeObservation{
			{ObservedAt: now, MonitoringInstanceID: "mi_001", TargetID: "tg_001", ProbeItemID: "pb_http_1", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess, LatencyMS: new(320), IsBackfilled: true},
			{ObservedAt: now.Add(-10 * time.Minute), MonitoringInstanceID: "mi_001", TargetID: "tg_001", ProbeItemID: "pb_http_1", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess, LatencyMS: new(330), IsBackfilled: true},
			{ObservedAt: now.Add(-20 * time.Minute), MonitoringInstanceID: "mi_001", TargetID: "tg_001", ProbeItemID: "pb_http_1", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess, LatencyMS: new(340), IsBackfilled: true},
		},
		baselines,
	)
	if suppressed.Transition != TransitionSkipped {
		t.Fatalf("Transition = %q, want %q", suppressed.Transition, TransitionSkipped)
	}

	latestSuppressed := EvaluateTargetLatencyTrendDegradationAcrossSeries(nil, "tg_001",
		[]runtimefacts.ProbeObservation{
			{ObservedAt: now, MonitoringInstanceID: "mi_001", TargetID: "tg_001", ProbeItemID: "pb_http_1", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess, LatencyMS: new(120), MaintenanceContext: true},
			targetLatencyObservation(now.Add(-10*time.Minute), "mi_001", "pb_http_1", 330),
			targetLatencyObservation(now.Add(-20*time.Minute), "mi_001", "pb_http_1", 340),
			targetLatencyObservation(now.Add(-30*time.Minute), "mi_001", "pb_http_1", 350),
		},
		baselines,
	)
	if latestSuppressed.Transition != TransitionSkipped {
		t.Fatalf("Transition = %q, want %q when newest latency observation is suppressed", latestSuppressed.Transition, TransitionSkipped)
	}

	insufficientSafe := EvaluateTargetLatencyTrendDegradationAcrossSeries(previous, "tg_001",
		[]runtimefacts.ProbeObservation{
			targetLatencyObservation(now, "mi_001", "pb_http_1", 120),
			targetLatencyObservation(now.Add(-10*time.Minute), "mi_001", "pb_http_1", 125),
		},
		baselines,
	)
	if insufficientSafe.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q", insufficientSafe.Transition, TransitionNoop)
	}
	if insufficientSafe.Current == nil || insufficientSafe.Current.IncidentClass != IncidentTargetLatencyTrendDegradation {
		t.Fatalf("Current = %#v, want previous incident preserved", insufficientSafe.Current)
	}

	briefSafe := EvaluateTargetLatencyTrendDegradationAcrossSeries(previous, "tg_001",
		[]runtimefacts.ProbeObservation{
			targetLatencyObservation(now.Add(time.Hour), "mi_001", "pb_http_1", 120),
			targetLatencyObservation(now.Add(50*time.Minute), "mi_001", "pb_http_1", 125),
			targetLatencyObservation(now.Add(40*time.Minute), "mi_001", "pb_http_1", 130),
		},
		baselines,
	)
	if briefSafe.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q for a short safe latency window", briefSafe.Transition, TransitionNoop)
	}

	secondBaseline := append([]TargetProbeDailyAggregate(nil), baselines...)
	secondBaseline = append(secondBaseline, TargetProbeDailyAggregate{TargetID: "tg_001", ProbeItemID: "pb_http_2", BucketDate: now.AddDate(0, 0, -1), ObservationCount: 96, SuccessCount: 96, AvgLatencyMS: new(float64(100))})
	partialRecovery := EvaluateTargetLatencyTrendDegradationAcrossSeries(previous, "tg_001",
		[]runtimefacts.ProbeObservation{
			targetLatencyObservation(now.Add(time.Hour), "mi_001", "pb_http_1", 120),
			targetLatencyObservation(now.Add(50*time.Minute), "mi_001", "pb_http_1", 125),
			targetLatencyObservation(now.Add(40*time.Minute), "mi_001", "pb_http_1", 130),
			targetLatencyObservation(now.Add(30*time.Minute), "mi_001", "pb_http_1", 124),
			targetLatencyObservation(now.Add(time.Hour), "mi_001", "pb_http_2", 121),
			targetLatencyObservation(now.Add(55*time.Minute), "mi_001", "pb_http_2", 122),
			targetLatencyObservation(now.Add(50*time.Minute), "mi_001", "pb_http_2", 123),
		},
		secondBaseline,
	)
	if partialRecovery.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q while another comparable probe item lacks a sustained safe window", partialRecovery.Transition, TransitionNoop)
	}

	recovered := EvaluateTargetLatencyTrendDegradationAcrossSeries(previous, "tg_001",
		[]runtimefacts.ProbeObservation{
			targetLatencyObservation(now.Add(time.Hour), "mi_001", "pb_http_1", 120),
			targetLatencyObservation(now.Add(50*time.Minute), "mi_001", "pb_http_1", 125),
			targetLatencyObservation(now.Add(40*time.Minute), "mi_001", "pb_http_1", 130),
			targetLatencyObservation(now.Add(30*time.Minute), "mi_001", "pb_http_1", 124),
		},
		baselines,
	)
	if recovered.Transition != TransitionRecovered {
		t.Fatalf("Transition = %q, want %q", recovered.Transition, TransitionRecovered)
	}
	if recovered.Notification == nil || recovered.Notification.Reason != NotificationReasonRecovered {
		t.Fatalf("Notification = %#v, want recovered notification", recovered.Notification)
	}
}

func nodeTrendSamples(now time.Time, load5 []float64, iowait []float64, steal []float64) []MonitoringInstanceResourceSample {
	samples := make([]MonitoringInstanceResourceSample, 0, len(load5))
	for i := range load5 {
		sample := MonitoringInstanceResourceSample{ObservedAt: now.Add(-time.Duration(i) * 10 * time.Minute), NormalizedLoad5: load5[i]}
		if i < len(iowait) {
			sample.CPUIOWaitPct = iowait[i]
		}
		if i < len(steal) {
			sample.CPUStealPct = steal[i]
		}
		samples = append(samples, sample)
	}
	return samples
}

func denseResourcePressureSamples(now time.Time, interval, duration time.Duration, cpu, iowait float64) []MonitoringInstanceResourceSample {
	samples := make([]MonitoringInstanceResourceSample, 0, int(duration/interval)+1)
	for elapsed := time.Duration(0); elapsed <= duration; elapsed += interval {
		samples = append(samples, MonitoringInstanceResourceSample{
			ObservedAt:    now.Add(-elapsed),
			CPUUsagePct:   cpu,
			CPUIOWaitPct:  iowait,
			CPURatesValid: new(true),
		})
	}
	return samples
}

func resourcePressureTestPolicy(now time.Time, interval time.Duration) ResourcePressurePolicy {
	return ResourcePressurePolicy{
		EvaluatedAt:    now.Add(100 * time.Millisecond),
		SampleInterval: interval,
	}
}

func targetLatencyObservation(observedAt time.Time, monitoringInstanceID, probeItemID string, latencyMS int) runtimefacts.ProbeObservation {
	return runtimefacts.ProbeObservation{
		ObservedAt:           observedAt,
		MonitoringInstanceID: monitoringInstanceID,
		TargetID:             "tg_001",
		ProbeItemID:          probeItemID,
		ProbeKind:            agentapi.ProbeKindHTTP,
		ResultKind:           agentapi.ProbeResultSuccess,
		LatencyMS:            new(latencyMS),
	}
}

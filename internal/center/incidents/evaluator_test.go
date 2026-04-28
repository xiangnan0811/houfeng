package incidents

import (
	"testing"
	"time"

	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/contracts/agentapi"
)

func TestEvaluateNodeHeartbeatMissingStartsAndEscalates(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	lastHeartbeat := now.Add(-3 * time.Minute)

	started := EvaluateNodeHeartbeatMissing(nil, "nd_001", now, &lastHeartbeat, time.Minute)
	if started.Transition != TransitionStarted {
		t.Fatalf("Transition = %q, want %q", started.Transition, TransitionStarted)
	}
	if started.Current == nil || started.Current.Severity != SeverityAlert {
		t.Fatalf("Current = %#v, want alert incident", started.Current)
	}
	if started.Notification == nil || started.Notification.Reason != NotificationReasonStarted {
		t.Fatalf("Notification = %#v, want started notification", started.Notification)
	}

	previous := started.Current
	olderHeartbeat := now.Add(-5 * time.Minute)
	escalated := EvaluateNodeHeartbeatMissing(previous, "nd_001", now, &olderHeartbeat, time.Minute)
	if escalated.Transition != TransitionEscalated {
		t.Fatalf("Transition = %q, want %q", escalated.Transition, TransitionEscalated)
	}
	if escalated.Current == nil || escalated.Current.Severity != SeverityCritical {
		t.Fatalf("Current = %#v, want critical incident", escalated.Current)
	}
	if escalated.Notification == nil || escalated.Notification.Reason != NotificationReasonEscalated {
		t.Fatalf("Notification = %#v, want escalated notification", escalated.Notification)
	}
}

func TestEvaluateNodeHeartbeatMissingRecovers(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	lastHeartbeat := now.Add(-30 * time.Second)
	previous := &IncidentRecord{IncidentID: "inc_node_nd_001_node_heartbeat_missing", ObjectType: ObjectTypeNode, ObjectID: "nd_001", IncidentClass: IncidentNodeHeartbeatMissing, Severity: SeverityAlert, LastEvaluatedAt: now.Add(-time.Minute)}

	result := EvaluateNodeHeartbeatMissing(previous, "nd_001", now, &lastHeartbeat, time.Minute)
	if result.Transition != TransitionRecovered {
		t.Fatalf("Transition = %q, want %q", result.Transition, TransitionRecovered)
	}
	if result.Notification == nil || result.Notification.Reason != NotificationReasonRecovered {
		t.Fatalf("Notification = %#v, want recovery notification", result.Notification)
	}
	if result.Event == nil || result.Event.IncidentClass != IncidentNodeHeartbeatMissing || result.Event.IncidentID != previous.IncidentID {
		t.Fatalf("Event = %#v, want incident identity on recovery", result.Event)
	}
}

func TestEvaluateNodeDiskAndInodePressureThresholds(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	diskSample := &runtimefacts.HostSample{ObservedAt: now, DiskUsedPct: 92}
	inodeSample := &runtimefacts.HostSample{ObservedAt: now, InodeUsedPct: 95}

	disk := EvaluateNodeDiskPressure(nil, "nd_001", diskSample)
	if disk.Current == nil || disk.Current.Severity != SeverityAlert {
		t.Fatalf("disk severity = %#v, want alert", disk.Current)
	}
	inode := EvaluateNodeInodePressure(nil, "nd_001", inodeSample)
	if inode.Current == nil || inode.Current.Severity != SeverityCritical {
		t.Fatalf("inode severity = %#v, want critical", inode.Current)
	}
}

func TestEvaluateNodeResourcePressureUsesSustainedWindow(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	samples := []NodeResourceSample{
		{ObservedAt: now, CPUUsagePct: 91, MemUsedPct: 93, NormalizedLoad5: 1.9, MemAvailableBytes: 700 * 1024 * 1024},
		{ObservedAt: now.Add(-8 * time.Minute), CPUUsagePct: 92, MemUsedPct: 94, NormalizedLoad5: 1.95, MemAvailableBytes: 650 * 1024 * 1024},
		{ObservedAt: now.Add(-15 * time.Minute), CPUUsagePct: 90, MemUsedPct: 92, NormalizedLoad5: 1.85, MemAvailableBytes: 620 * 1024 * 1024},
	}

	result := EvaluateNodeResourcePressure(nil, "nd_001", samples)
	if result.Current == nil || result.Current.Severity != SeverityAlert {
		t.Fatalf("Current = %#v, want alert resource incident", result.Current)
	}
}

func TestEvaluateNodeResourcePressureRequiresFullWindowCoverage(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	samples := []NodeResourceSample{
		{ObservedAt: now, CPUUsagePct: 99, MemUsedPct: 97, NormalizedLoad5: 2.8, MemAvailableBytes: 400 * 1024 * 1024},
		{ObservedAt: now.Add(-7 * time.Minute), CPUUsagePct: 99, MemUsedPct: 97, NormalizedLoad5: 2.9, MemAvailableBytes: 390 * 1024 * 1024},
		{ObservedAt: now.Add(-14 * time.Minute), CPUUsagePct: 99, MemUsedPct: 97, NormalizedLoad5: 2.7, MemAvailableBytes: 380 * 1024 * 1024},
	}

	result := EvaluateNodeResourcePressure(nil, "nd_001", samples)
	if result.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q when 15m/30m coverage is incomplete", result.Transition, TransitionNoop)
	}
	if result.Current != nil {
		t.Fatalf("Current = %#v, want nil", result.Current)
	}
}

func TestEvaluateNodeResourcePressureUsesLoadAndLowAvailableMemoryForSeverity(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	loadSamples := []NodeResourceSample{
		{ObservedAt: now, NormalizedLoad5: 1.9, MemAvailableBytes: 800 * 1024 * 1024},
		{ObservedAt: now.Add(-8 * time.Minute), NormalizedLoad5: 2.0, MemAvailableBytes: 780 * 1024 * 1024},
		{ObservedAt: now.Add(-15 * time.Minute), NormalizedLoad5: 1.8, MemAvailableBytes: 760 * 1024 * 1024},
	}
	loadResult := EvaluateNodeResourcePressure(nil, "nd_001", loadSamples)
	if loadResult.Current == nil || loadResult.Current.Severity != SeverityAlert {
		t.Fatalf("Current = %#v, want alert load-driven resource incident", loadResult.Current)
	}

	memorySamples := []NodeResourceSample{
		{ObservedAt: now, MemUsedPct: 96, MemAvailableBytes: 400 * 1024 * 1024},
		{ObservedAt: now.Add(-15 * time.Minute), MemUsedPct: 95, MemAvailableBytes: 420 * 1024 * 1024},
		{ObservedAt: now.Add(-30 * time.Minute), MemUsedPct: 97, MemAvailableBytes: 380 * 1024 * 1024},
	}
	memoryResult := EvaluateNodeResourcePressure(nil, "nd_001", memorySamples)
	if memoryResult.Current == nil || memoryResult.Current.Severity != SeverityCritical {
		t.Fatalf("Current = %#v, want critical low-available-memory incident", memoryResult.Current)
	}
}

func TestEvaluateNodeResourcePressureIgnoresSuppressedHistoryForActiveEvidence(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	samples := []NodeResourceSample{
		{ObservedAt: now, CPUUsagePct: 91, MemUsedPct: 93, NormalizedLoad5: 1.9, MemAvailableBytes: 700 * 1024 * 1024},
		{ObservedAt: now.Add(-8 * time.Minute), CPUUsagePct: 92, MemUsedPct: 94, NormalizedLoad5: 1.95, MemAvailableBytes: 650 * 1024 * 1024, MaintenanceContext: true},
		{ObservedAt: now.Add(-15 * time.Minute), CPUUsagePct: 90, MemUsedPct: 92, NormalizedLoad5: 1.85, MemAvailableBytes: 620 * 1024 * 1024, MaintenanceContext: true},
	}

	result := EvaluateNodeResourcePressure(nil, "nd_001", samples)
	if result.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q when only suppressed history spans the active window", result.Transition, TransitionNoop)
	}
	if result.Current != nil {
		t.Fatalf("Current = %#v, want nil when only one unsuppressed sample exists", result.Current)
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

	tcpMultiNodeFailures := []runtimefacts.ProbeObservation{
		{ObservedAt: now, NodeID: "nd_001", ProbeItemID: "pb_tcp", ProbeKind: agentapi.ProbeKindTCP, ResultKind: agentapi.ProbeResultFailure},
		{ObservedAt: now.Add(-10 * time.Second), NodeID: "nd_002", ProbeItemID: "pb_tcp", ProbeKind: agentapi.ProbeKindTCP, ResultKind: agentapi.ProbeResultFailure},
	}
	tcpMultiNode := EvaluateTargetProbeFailure(nil, "tg_001", tcpMultiNodeFailures)
	if tcpMultiNode.Current == nil || tcpMultiNode.Current.Severity != SeverityCritical {
		t.Fatalf("Current = %#v, want critical multi-node tcp incident", tcpMultiNode.Current)
	}

	httpMultiProbeFailures := []runtimefacts.ProbeObservation{
		{ObservedAt: now, NodeID: "nd_001", ProbeItemID: "pb_http_1", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultFailure},
		{ObservedAt: now.Add(-10 * time.Second), NodeID: "nd_001", ProbeItemID: "pb_http_2", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultFailure},
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
		IncidentID:      "inc_node_nd_001_node_disk_pressure",
		ObjectType:      ObjectTypeNode,
		ObjectID:        "nd_001",
		IncidentClass:   IncidentNodeDiskPressure,
		Severity:        SeverityAlert,
		LastEvaluatedAt: now,
	}

	skipped := EvaluateNodeDiskPressure(previous, "nd_001", &runtimefacts.HostSample{ObservedAt: now, DiskUsedPct: 99, MaintenanceContext: true})
	if skipped.Transition != TransitionSkipped {
		t.Fatalf("Transition = %q, want %q", skipped.Transition, TransitionSkipped)
	}
	if skipped.Notification != nil {
		t.Fatalf("Notification = %#v, want nil", skipped.Notification)
	}
	if skipped.Current != nil {
		t.Fatalf("Current = %#v, want nil on maintenance short-circuit", skipped.Current)
	}

	recovered := EvaluateNodeDiskPressure(previous, "nd_001", &runtimefacts.HostSample{ObservedAt: now.Add(time.Minute), DiskUsedPct: 40, MaintenanceContext: true})
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

	nodeIncident := &IncidentRecord{ObjectType: ObjectTypeNode, ObjectID: "nd_001", IncidentClass: IncidentNodeResourcePressure, Severity: SeverityAlert}
	resource := EvaluateNodeResourcePressure(nodeIncident, "nd_001", nil)
	if resource.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q for empty host input", resource.Transition, TransitionNoop)
	}
}

func TestEvaluateNodeHeartbeatMissingDoesNotRecoverWithoutUsableHeartbeatEvidence(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 0, 0, 0, time.UTC)
	previous := &IncidentRecord{
		IncidentID:      "inc_node_nd_001_node_heartbeat_missing",
		ObjectType:      ObjectTypeNode,
		ObjectID:        "nd_001",
		IncidentClass:   IncidentNodeHeartbeatMissing,
		Severity:        SeverityAlert,
		LastEvaluatedAt: now.Add(-time.Minute),
	}

	nilHeartbeat := EvaluateNodeHeartbeatMissing(previous, "nd_001", now, nil, time.Minute)
	if nilHeartbeat.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q for nil heartbeat", nilHeartbeat.Transition, TransitionNoop)
	}
	if nilHeartbeat.Current == nil || nilHeartbeat.Current.IncidentClass != IncidentNodeHeartbeatMissing {
		t.Fatalf("Current = %#v, want previous heartbeat incident preserved", nilHeartbeat.Current)
	}

	lastHeartbeat := now.Add(-5 * time.Minute)
	invalidInterval := EvaluateNodeHeartbeatMissing(previous, "nd_001", now, &lastHeartbeat, 0)
	if invalidInterval.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q for invalid interval", invalidInterval.Transition, TransitionNoop)
	}
	if invalidInterval.Current == nil || invalidInterval.Current.IncidentClass != IncidentNodeHeartbeatMissing {
		t.Fatalf("Current = %#v, want previous heartbeat incident preserved", invalidInterval.Current)
	}
}

func TestEvaluateNodeResourcePressureRequiresRecoveryWindowBeforeClosing(t *testing.T) {
	now := time.Date(2026, time.April, 25, 10, 30, 0, 0, time.UTC)
	previous := &IncidentRecord{
		IncidentID:      "inc_node_nd_001_node_resource_pressure",
		ObjectType:      ObjectTypeNode,
		ObjectID:        "nd_001",
		IncidentClass:   IncidentNodeResourcePressure,
		Severity:        SeverityAlert,
		LastEvaluatedAt: now.Add(-time.Minute),
	}

	insufficient := []NodeResourceSample{
		{ObservedAt: now, CPUUsagePct: 20, MemUsedPct: 40, NormalizedLoad5: 0.8},
		{ObservedAt: now.Add(-5 * time.Minute), CPUUsagePct: 22, MemUsedPct: 42, NormalizedLoad5: 0.9},
	}
	result := EvaluateNodeResourcePressure(previous, "nd_001", insufficient)
	if result.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q for incomplete safe window", result.Transition, TransitionNoop)
	}

	safeWindow := []NodeResourceSample{
		{ObservedAt: now, CPUUsagePct: 20, MemUsedPct: 40, NormalizedLoad5: 0.8},
		{ObservedAt: now.Add(-8 * time.Minute), CPUUsagePct: 22, MemUsedPct: 42, NormalizedLoad5: 0.9},
		{ObservedAt: now.Add(-15 * time.Minute), CPUUsagePct: 24, MemUsedPct: 44, NormalizedLoad5: 1.0},
	}
	recovered := EvaluateNodeResourcePressure(previous, "nd_001", safeWindow)
	if recovered.Transition != TransitionRecovered {
		t.Fatalf("Transition = %q, want %q for sustained safe window", recovered.Transition, TransitionRecovered)
	}
}

func TestEvaluateNodeTrendDegradationStartsAndEscalates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	started := EvaluateNodeTrendDegradation(nil, "nd_001",
		nodeTrendSamples(now, []float64{1.7, 1.8, 1.9}, []float64{4, 4, 4}, []float64{0.8, 0.9, 0.8}),
		[]NodeHostDailyAggregate{{BucketDate: now.AddDate(0, 0, -1), SampleCount: 288, AvgLoad5: 0.8, AvgCPUIOWaitPct: 2, AvgCPUStealPct: 0.5}},
	)
	if started.Transition != TransitionStarted {
		t.Fatalf("Transition = %q, want %q", started.Transition, TransitionStarted)
	}
	if started.Current == nil || started.Current.IncidentClass != IncidentNodeTrendDegradation {
		t.Fatalf("Current = %#v, want node trend incident", started.Current)
	}
	if started.Current.Severity != SeverityNotice {
		t.Fatalf("Severity = %q, want %q", started.Current.Severity, SeverityNotice)
	}
	if started.Current.Severity == SeverityCritical {
		t.Fatal("trend degradation must not emit critical severity")
	}

	escalated := EvaluateNodeTrendDegradation(started.Current, "nd_001",
		nodeTrendSamples(now.Add(30*time.Minute), []float64{1.9, 2.0, 2.1}, []float64{11, 12, 13}, []float64{0.8, 0.9, 0.8}),
		[]NodeHostDailyAggregate{{BucketDate: now.AddDate(0, 0, -1), SampleCount: 288, AvgLoad5: 0.8, AvgCPUIOWaitPct: 2, AvgCPUStealPct: 0.5}},
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

func TestEvaluateNodeTrendDegradationSkipsSuppressedStartsAndRecoversConservatively(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	baselines := []NodeHostDailyAggregate{{BucketDate: now.AddDate(0, 0, -1), SampleCount: 288, AvgLoad5: 0.8, AvgCPUIOWaitPct: 2, AvgCPUStealPct: 0.5}}
	previous := &IncidentRecord{IncidentID: "inc_node_nd_001_node_trend_degradation", ObjectType: ObjectTypeNode, ObjectID: "nd_001", IncidentClass: IncidentNodeTrendDegradation, Severity: SeverityAlert, StartedAt: now.Add(-24 * time.Hour), LastEvaluatedAt: now.Add(-time.Hour)}

	suppressed := EvaluateNodeTrendDegradation(nil, "nd_001",
		[]NodeResourceSample{
			{ObservedAt: now, NormalizedLoad5: 2.0, CPUIOWaitPct: 12, MaintenanceContext: true},
			{ObservedAt: now.Add(-10 * time.Minute), NormalizedLoad5: 2.0, CPUIOWaitPct: 12, MaintenanceContext: true},
			{ObservedAt: now.Add(-20 * time.Minute), NormalizedLoad5: 2.0, CPUIOWaitPct: 12, MaintenanceContext: true},
		},
		baselines,
	)
	if suppressed.Transition != TransitionSkipped {
		t.Fatalf("Transition = %q, want %q", suppressed.Transition, TransitionSkipped)
	}

	insufficientSafe := EvaluateNodeTrendDegradation(previous, "nd_001",
		nodeTrendSamples(now, []float64{0.7, 0.8}, []float64{2, 2}, []float64{0.4, 0.4}),
		baselines,
	)
	if insufficientSafe.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q", insufficientSafe.Transition, TransitionNoop)
	}
	if insufficientSafe.Current == nil || insufficientSafe.Current.IncidentClass != IncidentNodeTrendDegradation {
		t.Fatalf("Current = %#v, want previous incident preserved", insufficientSafe.Current)
	}

	recovered := EvaluateNodeTrendDegradation(previous, "nd_001",
		nodeTrendSamples(now.Add(time.Hour), []float64{0.7, 0.8, 0.9}, []float64{2, 2, 2}, []float64{0.4, 0.4, 0.4}),
		baselines,
	)
	if recovered.Transition != TransitionRecovered {
		t.Fatalf("Transition = %q, want %q", recovered.Transition, TransitionRecovered)
	}
	if recovered.Notification == nil || recovered.Notification.Reason != NotificationReasonRecovered {
		t.Fatalf("Notification = %#v, want recovered notification", recovered.Notification)
	}
}

func TestEvaluateTargetLatencyTrendStartsAndEscalatesWithoutCritical(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	baselines := []TargetProbeDailyAggregate{{TargetID: "tg_001", ProbeItemID: "pb_http_1", BucketDate: now.AddDate(0, 0, -1), ObservationCount: 96, SuccessCount: 96, AvgLatencyMS: float64Ptr(120)}}
	started := EvaluateTargetLatencyTrendDegradationAcrossSeries(nil, "tg_001",
		[]runtimefacts.ProbeObservation{
			targetLatencyObservation(now, "nd_001", "pb_http_1", 330),
			targetLatencyObservation(now.Add(-10*time.Minute), "nd_001", "pb_http_1", 340),
			targetLatencyObservation(now.Add(-20*time.Minute), "nd_001", "pb_http_1", 350),
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
			targetLatencyObservation(now.Add(time.Hour), "nd_001", "pb_http_1", 360),
			targetLatencyObservation(now.Add(50*time.Minute), "nd_001", "pb_http_1", 340),
			targetLatencyObservation(now.Add(40*time.Minute), "nd_002", "pb_http_1", 350),
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
}

func TestEvaluateTargetLatencyTrendSkipsSuppressedStartsAndRecoversConservatively(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	baselines := []TargetProbeDailyAggregate{{TargetID: "tg_001", ProbeItemID: "pb_http_1", BucketDate: now.AddDate(0, 0, -1), ObservationCount: 96, SuccessCount: 96, AvgLatencyMS: float64Ptr(100)}}
	previous := &IncidentRecord{IncidentID: "inc_target_tg_001_target_latency_trend_degradation", ObjectType: ObjectTypeTarget, ObjectID: "tg_001", IncidentClass: IncidentTargetLatencyTrendDegradation, Severity: SeverityNotice, StartedAt: now.Add(-24 * time.Hour), LastEvaluatedAt: now.Add(-time.Hour)}

	suppressed := EvaluateTargetLatencyTrendDegradationAcrossSeries(nil, "tg_001",
		[]runtimefacts.ProbeObservation{
			{ObservedAt: now, NodeID: "nd_001", TargetID: "tg_001", ProbeItemID: "pb_http_1", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess, LatencyMS: intPtr(320), IsBackfilled: true},
			{ObservedAt: now.Add(-10 * time.Minute), NodeID: "nd_001", TargetID: "tg_001", ProbeItemID: "pb_http_1", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess, LatencyMS: intPtr(330), IsBackfilled: true},
			{ObservedAt: now.Add(-20 * time.Minute), NodeID: "nd_001", TargetID: "tg_001", ProbeItemID: "pb_http_1", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess, LatencyMS: intPtr(340), IsBackfilled: true},
		},
		baselines,
	)
	if suppressed.Transition != TransitionSkipped {
		t.Fatalf("Transition = %q, want %q", suppressed.Transition, TransitionSkipped)
	}

	insufficientSafe := EvaluateTargetLatencyTrendDegradationAcrossSeries(previous, "tg_001",
		[]runtimefacts.ProbeObservation{
			targetLatencyObservation(now, "nd_001", "pb_http_1", 120),
			targetLatencyObservation(now.Add(-10*time.Minute), "nd_001", "pb_http_1", 125),
		},
		baselines,
	)
	if insufficientSafe.Transition != TransitionNoop {
		t.Fatalf("Transition = %q, want %q", insufficientSafe.Transition, TransitionNoop)
	}
	if insufficientSafe.Current == nil || insufficientSafe.Current.IncidentClass != IncidentTargetLatencyTrendDegradation {
		t.Fatalf("Current = %#v, want previous incident preserved", insufficientSafe.Current)
	}

	recovered := EvaluateTargetLatencyTrendDegradationAcrossSeries(previous, "tg_001",
		[]runtimefacts.ProbeObservation{
			targetLatencyObservation(now.Add(time.Hour), "nd_001", "pb_http_1", 120),
			targetLatencyObservation(now.Add(50*time.Minute), "nd_001", "pb_http_1", 125),
			targetLatencyObservation(now.Add(40*time.Minute), "nd_001", "pb_http_1", 130),
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

func nodeTrendSamples(now time.Time, load5 []float64, iowait []float64, steal []float64) []NodeResourceSample {
	samples := make([]NodeResourceSample, 0, len(load5))
	for i := range load5 {
		sample := NodeResourceSample{ObservedAt: now.Add(-time.Duration(i) * 10 * time.Minute), NormalizedLoad5: load5[i]}
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

func targetLatencyObservation(observedAt time.Time, nodeID, probeItemID string, latencyMS int) runtimefacts.ProbeObservation {
	return runtimefacts.ProbeObservation{
		ObservedAt:  observedAt,
		NodeID:      nodeID,
		TargetID:    "tg_001",
		ProbeItemID: probeItemID,
		ProbeKind:   agentapi.ProbeKindHTTP,
		ResultKind:  agentapi.ProbeResultSuccess,
		LatencyMS:   intPtr(latencyMS),
	}
}

func intPtr(v int) *int { return &v }

func float64Ptr(v float64) *float64 { return &v }

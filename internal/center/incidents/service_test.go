package incidents

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"houfeng/internal/center/nodes"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

type fakeNodeRepo struct {
	getNodeResult   nodes.Record
	listNodesResult []nodes.Record
}

func (f *fakeNodeRepo) GetNode(context.Context, string) (nodes.Record, error) {
	return f.getNodeResult, nil
}
func (f *fakeNodeRepo) ListNodes(context.Context) ([]nodes.Record, error) {
	return f.listNodesResult, nil
}

type fakeSnapshotReader struct {
	activeByObject map[string][]IncidentRecord
	hostSamples    map[string][]runtimefacts.HostSample
	probeObs       map[string][]runtimefacts.ProbeObservation
}

func (f *fakeSnapshotReader) ListActiveIncidents(_ context.Context, objectType ObjectType, objectID string) ([]IncidentRecord, error) {
	return append([]IncidentRecord(nil), f.activeByObject[string(objectType)+":"+objectID]...), nil
}
func (f *fakeSnapshotReader) ListRecentHostSamples(_ context.Context, nodeID string, _ time.Time) ([]runtimefacts.HostSample, error) {
	return append([]runtimefacts.HostSample(nil), f.hostSamples[nodeID]...), nil
}
func (f *fakeSnapshotReader) ListRecentProbeObservations(_ context.Context, targetID string, _ time.Time) ([]runtimefacts.ProbeObservation, error) {
	return append([]runtimefacts.ProbeObservation(nil), f.probeObs[targetID]...), nil
}

type fakeMutationWriter struct{ mutations []IncidentMutation }

func (f *fakeMutationWriter) ApplyIncidentMutation(_ context.Context, mutation IncidentMutation) error {
	f.mutations = append(f.mutations, mutation)
	return nil
}

type fakeNotifier struct {
	messages []string
	err      error
}

func (f *fakeNotifier) Send(_ context.Context, summary string) error {
	f.messages = append(f.messages, summary)
	return f.err
}

func TestServiceAfterSuccessfulSyncEvaluatesNodeAndTouchedTargets(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{},
		hostSamples: map[string][]runtimefacts.HostSample{
			"nd_001": {{ObservedAt: now, DiskUsedPct: 92}},
		},
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
				{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
			},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(nodeRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		NodeID:       "nd_001",
		Observations: storeProbeBatch("tg_001"),
	}, syncing.Result{AcceptedAt: now})

	if len(writer.mutations) != 2 {
		t.Fatalf("len(mutations) = %d, want 2", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) == 0 && len(writer.mutations[1].Active) == 0 {
		t.Fatalf("mutations = %#v, want incident writes", writer.mutations)
	}
	if len(notifier.messages) == 0 {
		t.Fatal("notifier.messages = 0, want at least one notification")
	}
}

func TestServiceAfterSuccessfulSyncSuppressesNotificationsWithoutNotifier(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"nd_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	service := NewService(nodeRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now})
	if len(writer.mutations) != 1 || len(writer.mutations[0].Notifications) != 1 {
		t.Fatalf("mutations = %#v, want suppressed notification record", writer.mutations)
	}
	if writer.mutations[0].Notifications[0].DeliveryStatus != DeliveryStatusSuppressed {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.mutations[0].Notifications[0].DeliveryStatus, DeliveryStatusSuppressed)
	}
}

func TestServiceEvaluateStaleNodesCreatesHeartbeatIncident(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	stale := now.Add(-3 * time.Minute)
	nodeRepo := &fakeNodeRepo{listNodesResult: []nodes.Record{{NodeID: "nd_001", LastHeartbeatAt: &stale}}}
	snapshots := &fakeSnapshotReader{}
	writer := &fakeMutationWriter{}
	service := NewService(nodeRepo, snapshots, writer, nil, slog.Default(), time.Minute, time.Minute)

	if err := service.EvaluateStaleNodes(context.Background(), now); err != nil {
		t.Fatalf("EvaluateStaleNodes() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 1 || writer.mutations[0].Active[0].IncidentClass != IncidentNodeHeartbeatMissing {
		t.Fatalf("mutation = %#v, want heartbeat incident", writer.mutations[0])
	}
}

func TestServiceAfterSuccessfulSyncUsesStoredLoadForResourcePressure(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{
			"nd_001": {
				{ObservedAt: now, Load5: 1.9},
				{ObservedAt: now.Add(-8 * time.Minute), Load5: 2.0},
				{ObservedAt: now.Add(-15 * time.Minute), Load5: 1.8},
			},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(nodeRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now})
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	found := false
	for _, incident := range writer.mutations[0].Active {
		if incident.IncidentClass == IncidentNodeResourcePressure {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("mutation = %#v, want load-driven resource incident", writer.mutations[0])
	}
}

func TestServiceSkippedEvaluationClosesPriorIncidentWithoutNotification(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"node:nd_001": {{
				IncidentID:      "inc_node_nd_001_node_disk_pressure",
				ObjectType:      ObjectTypeNode,
				ObjectID:        "nd_001",
				IncidentClass:   IncidentNodeDiskPressure,
				Severity:        SeverityAlert,
				StartedAt:       now.Add(-time.Hour),
				LastEvaluatedAt: now.Add(-time.Minute),
				Status:          IncidentStatusActive,
				SourceSummary:   "磁盘使用率 92.0%",
			}},
		},
		hostSamples: map[string][]runtimefacts.HostSample{
			"nd_001": {{ObservedAt: now, DiskUsedPct: 99, MaintenanceContext: true}},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(nodeRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now})
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 0 {
		t.Fatalf("Active = %#v, want incident cleared on skipped maintenance evaluation", writer.mutations[0].Active)
	}
	if len(writer.mutations[0].Notifications) != 0 {
		t.Fatalf("Notifications = %#v, want none on skipped maintenance evaluation", writer.mutations[0].Notifications)
	}
	if len(writer.mutations[0].Events) != 1 || writer.mutations[0].Events[0].EventType != EventIncidentRecovered {
		t.Fatalf("Events = %#v, want silent recovery event for skipped maintenance evaluation", writer.mutations[0].Events)
	}
}

func storeProbeBatch(targetID string) observations.BatchWrite {
	latency := 83
	status := 503
	return observations.BatchWrite{
		ProbeObservations: []observations.ProbeObservationWrite{{
			NodeID:       "nd_001",
			TargetID:     targetID,
			ProbeItemID:  "pb_001",
			ProbeKind:    agentapi.ProbeKindHTTP,
			ResultKind:   agentapi.ProbeResultFailure,
			LatencyMS:    &latency,
			HTTPStatus:   &status,
			ErrorSummary: "503",
		}},
	}
}

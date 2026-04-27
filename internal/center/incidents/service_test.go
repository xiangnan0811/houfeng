package incidents

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"houfeng/internal/center/nodes"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/runtimefacts"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
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

type fakeTargetRepo struct{ listTargetsResult []targets.TargetRecord }

func (f *fakeTargetRepo) ListTargets(context.Context) ([]targets.TargetRecord, error) {
	return f.listTargetsResult, nil
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

type fakeMutationWriter struct {
	mutations     []IncidentMutation
	notifications [][]NotificationRecordWrite
}

func (f *fakeMutationWriter) ApplyIncidentMutation(_ context.Context, mutation IncidentMutation) error {
	f.mutations = append(f.mutations, mutation)
	return nil
}

func (f *fakeMutationWriter) AppendNotificationRecords(_ context.Context, records []NotificationRecordWrite) error {
	f.notifications = append(f.notifications, records)
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

type fakeSettingsRepository struct {
	getSettingsResult            centersettings.CenterSettings
	getSettingsErr               error
	persistedTelegram            centersettings.TelegramSettings
	persistedExists              bool
	persistedErr                 error
	persistedIncidentDefaults    centersettings.IncidentDefaults
	persistedIncidentExists      bool
	persistedIncidentDefaultsErr error
}

func (f *fakeSettingsRepository) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	if f.getSettingsErr != nil {
		return centersettings.CenterSettings{}, f.getSettingsErr
	}
	return f.getSettingsResult, nil
}

func (f *fakeSettingsRepository) PutSettings(context.Context, centersettings.CenterSettings) (centersettings.CenterSettings, error) {
	panic("unexpected PutSettings call")
}

func (f *fakeSettingsRepository) GetPersistedTelegramSettings(context.Context) (centersettings.TelegramSettings, bool, error) {
	if f.persistedErr != nil {
		return centersettings.TelegramSettings{}, false, f.persistedErr
	}
	if !f.persistedExists {
		return centersettings.TelegramSettings{}, false, nil
	}
	return f.persistedTelegram, true, nil
}

func (f *fakeSettingsRepository) GetPersistedIncidentDefaults(context.Context) (centersettings.IncidentDefaults, bool, error) {
	if f.persistedIncidentDefaultsErr != nil {
		return centersettings.IncidentDefaults{}, false, f.persistedIncidentDefaultsErr
	}
	if !f.persistedIncidentExists {
		return centersettings.IncidentDefaults{}, false, nil
	}
	return f.persistedIncidentDefaults, true, nil
}

func TestServiceAfterSuccessfulSyncEvaluatesNodeAndTouchedTargets(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{},
		hostSamples: map[string][]runtimefacts.HostSample{
			"nd_001": {{ObservedAt: now, DiskUsedPct: 92}},
		},
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", NodeID: "nd_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", NodeID: "nd_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
				{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", NodeID: "nd_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
			},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(nodeRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		NodeID:       "nd_001",
		Observations: storeProbeBatch("tg_001"),
	}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	if len(writer.mutations) != 2 {
		t.Fatalf("len(mutations) = %d, want 2", len(writer.mutations))
	}
	if len(writer.notifications) == 0 {
		t.Fatal("notifications were not recorded")
	}
	if len(notifier.messages) == 0 {
		t.Fatal("notifier.messages = 0, want at least one notification")
	}
}

func TestServiceAfterSuccessfulSyncSuppressesNotificationsWithoutNotifier(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"nd_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	service := NewService(nodeRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.notifications) != 1 || len(writer.notifications[0]) != 1 {
		t.Fatalf("notifications = %#v, want suppressed notification record", writer.notifications)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSuppressed {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSuppressed)
	}
}

func TestServiceRecordsFailedNotificationDelivery(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"nd_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{err: errors.New("send failed")}
	service := NewService(nodeRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusFailed {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusFailed)
	}
}

func TestSettingsAwareNotifierSuppressesWhenPersistedTelegramIsDisabled(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"nd_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	fallbackNotifier := &fakeNotifier{}
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult: centersettings.Default(),
		persistedTelegram: centersettings.TelegramSettings{RuntimeManaged: true},
		persistedExists:   true,
	}
	buildCalls := 0
	service := NewService(
		nodeRepo,
		targetRepo,
		snapshots,
		writer,
		NewSettingsAwareNotifier(settingsRepo, func(botToken, chatID string) Notifier {
			buildCalls++
			return &fakeNotifier{}
		}, fallbackNotifier),
		slog.Default(),
		30*time.Second,
		time.Minute,
	)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.notifications) != 1 || len(writer.notifications[0]) != 1 {
		t.Fatalf("notifications = %#v, want one suppressed notification record", writer.notifications)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSuppressed {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSuppressed)
	}
	if buildCalls != 0 {
		t.Fatalf("buildCalls = %d, want 0", buildCalls)
	}
	if len(fallbackNotifier.messages) != 0 {
		t.Fatalf("fallbackNotifier.messages = %#v, want no env-fallback send when persisted Telegram is disabled", fallbackNotifier.messages)
	}
}

func TestSettingsAwareNotifierUsesFallbackWhenFreshDBWouldAutoCreateDisabledDefaults(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"nd_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	fallbackNotifier := &fakeNotifier{}
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult: centersettings.Default(),
		persistedExists:   false,
	}
	service := NewService(
		nodeRepo,
		targetRepo,
		snapshots,
		writer,
		NewSettingsAwareNotifier(settingsRepo, func(botToken, chatID string) Notifier {
			return &fakeNotifier{}
		}, fallbackNotifier),
		slog.Default(),
		30*time.Second,
		time.Minute,
	)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(fallbackNotifier.messages) != 1 {
		t.Fatalf("fallbackNotifier.messages = %#v, want one env-fallback send for fresh DB behavior", fallbackNotifier.messages)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSent {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSent)
	}
}

func TestSettingsAwareNotifierUsesFallbackWhenPersistedTelegramIsNotManagingRuntime(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"nd_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	fallbackNotifier := &fakeNotifier{}
	settingsRepo := &fakeSettingsRepository{
		persistedTelegram: centersettings.TelegramSettings{
			BotToken:       "persisted-bot-token",
			ChatID:         "persisted-chat-id",
			RuntimeManaged: false,
		},
		persistedExists: true,
	}
	buildCalls := 0
	service := NewService(
		nodeRepo,
		targetRepo,
		snapshots,
		writer,
		NewSettingsAwareNotifier(settingsRepo, func(botToken, chatID string) Notifier {
			buildCalls++
			return &fakeNotifier{}
		}, fallbackNotifier),
		slog.Default(),
		30*time.Second,
		time.Minute,
	)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(fallbackNotifier.messages) != 1 {
		t.Fatalf("fallbackNotifier.messages = %#v, want one env-fallback send when persisted Telegram is not managing runtime", fallbackNotifier.messages)
	}
	if buildCalls != 0 {
		t.Fatalf("buildCalls = %d, want 0", buildCalls)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSent {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSent)
	}
}

func TestSettingsAwareNotifierUsesPersistedTelegramConfig(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"nd_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	liveNotifier := &fakeNotifier{}
	settings := centersettings.Default()
	settings.Telegram.BotToken = "persisted-bot-token"
	settings.Telegram.ChatID = "persisted-chat-id"
	settings.Telegram.RuntimeManaged = true
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult: settings,
		persistedTelegram: settings.Telegram,
		persistedExists:   true,
	}
	var gotBotToken string
	var gotChatID string
	service := NewService(
		nodeRepo,
		targetRepo,
		snapshots,
		writer,
		NewSettingsAwareNotifier(settingsRepo, func(botToken, chatID string) Notifier {
			gotBotToken = botToken
			gotChatID = chatID
			return liveNotifier
		}, &fakeNotifier{}),
		slog.Default(),
		30*time.Second,
		time.Minute,
	)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if gotBotToken != settings.Telegram.BotToken {
		t.Fatalf("got bot token = %q, want %q", gotBotToken, settings.Telegram.BotToken)
	}
	if gotChatID != settings.Telegram.ChatID {
		t.Fatalf("got chat id = %q, want %q", gotChatID, settings.Telegram.ChatID)
	}
	if len(liveNotifier.messages) != 1 {
		t.Fatalf("liveNotifier.messages = %#v, want one send", liveNotifier.messages)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSent {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSent)
	}
}

func TestSettingsBackedHeartbeatIntervalUsesPersistedSettings(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	stale := now.Add(-75 * time.Second)
	nodeRepo := &fakeNodeRepo{listNodesResult: []nodes.Record{{NodeID: "nd_001", LastHeartbeatAt: &stale}}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{}
	writer := &fakeMutationWriter{}
	settings := centersettings.Default()
	settings.IncidentDefaults.HeartbeatIntervalSeconds = 120
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult:         settings,
		persistedIncidentDefaults: settings.IncidentDefaults,
		persistedIncidentExists:   true,
	}
	service := NewSettingsBackedService(nodeRepo, targetRepo, snapshots, writer, nil, settingsRepo, slog.Default(), 30*time.Second, time.Minute)

	if err := service.EvaluateStaleNodes(context.Background(), now); err != nil {
		t.Fatalf("EvaluateStaleNodes() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 0 {
		t.Fatalf("Active = %#v, want no heartbeat incident when persisted heartbeat interval is longer", writer.mutations[0].Active)
	}
}

func TestSettingsBackedSweepIntervalUsesPersistedSettings(t *testing.T) {
	settings := centersettings.Default()
	settings.IncidentDefaults.SweepIntervalSeconds = 180
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult:         settings,
		persistedIncidentDefaults: settings.IncidentDefaults,
		persistedIncidentExists:   true,
	}
	service := NewSettingsBackedService(nil, nil, nil, nil, nil, settingsRepo, slog.Default(), 30*time.Second, time.Minute)

	if got := service.sweepIntervalFor(context.Background()); got != 3*time.Minute {
		t.Fatalf("sweepIntervalFor() = %v, want %v", got, 3*time.Minute)
	}
}

func TestIncidentTimingFallsBackWhenPersistedSettingsAbsentOrUnavailable(t *testing.T) {
	tests := []struct {
		name string
		repo *fakeSettingsRepository
	}{
		{
			name: "absent",
			repo: &fakeSettingsRepository{},
		},
		{
			name: "unavailable",
			repo: &fakeSettingsRepository{persistedIncidentDefaultsErr: errors.New("boom")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewSettingsBackedService(nil, nil, nil, nil, nil, tt.repo, slog.Default(), 45*time.Second, 2*time.Minute)

			if got := service.heartbeatIntervalFor(context.Background()); got != 45*time.Second {
				t.Fatalf("heartbeatIntervalFor() = %v, want %v", got, 45*time.Second)
			}
			if got := service.sweepIntervalFor(context.Background()); got != 2*time.Minute {
				t.Fatalf("sweepIntervalFor() = %v, want %v", got, 2*time.Minute)
			}
		})
	}
}

func TestServiceEvaluateStaleNodesCreatesHeartbeatIncident(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	stale := now.Add(-3 * time.Minute)
	nodeRepo := &fakeNodeRepo{listNodesResult: []nodes.Record{{NodeID: "nd_001", LastHeartbeatAt: &stale}}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{}
	writer := &fakeMutationWriter{}
	service := NewService(nodeRepo, targetRepo, snapshots, writer, nil, slog.Default(), time.Minute, time.Minute)

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
	targetRepo := &fakeTargetRepo{}
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
	service := NewService(nodeRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
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

func TestServiceSkippedEvaluationPreservesPriorIncidentDuringMaintenance(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
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
	service := NewService(nodeRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 1 || writer.mutations[0].Active[0].IncidentClass != IncidentNodeDiskPressure {
		t.Fatalf("Active = %#v, want prior incident preserved on skipped maintenance evaluation", writer.mutations[0].Active)
	}
	if len(writer.notifications) != 0 {
		t.Fatalf("Notifications = %#v, want none on skipped maintenance evaluation", writer.notifications)
	}
	if len(writer.mutations[0].Events) != 0 {
		t.Fatalf("Events = %#v, want none on skipped maintenance evaluation", writer.mutations[0].Events)
	}
}

func TestServiceMaintenanceRecoveryClosesIncidentSilently(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
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
			"nd_001": {{ObservedAt: now, DiskUsedPct: 40, MaintenanceContext: true}},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(nodeRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{NodeID: "nd_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 0 {
		t.Fatalf("Active = %#v, want recovered incident removed from active set", writer.mutations[0].Active)
	}
	if len(writer.mutations[0].Events) != 1 || writer.mutations[0].Events[0].EventType != EventIncidentRecovered {
		t.Fatalf("Events = %#v, want recovered event", writer.mutations[0].Events)
	}
	if len(writer.notifications) != 1 || len(writer.notifications[0]) != 1 {
		t.Fatalf("notifications = %#v, want one suppressed recovery record", writer.notifications)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusSuppressed {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusSuppressed)
	}
	if len(notifier.messages) != 0 {
		t.Fatalf("notifier.messages = %#v, want no Telegram send during maintenance recovery", notifier.messages)
	}
}

func TestServiceDoesNotRecoverTargetProbeFailureUntilAllSeriesRecover(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	previous := IncidentRecord{
		IncidentID:      "inc_target_tg_001_target_probe_failure",
		ObjectType:      ObjectTypeTarget,
		ObjectID:        "tg_001",
		IncidentClass:   IncidentTargetProbeFailure,
		Severity:        SeverityAlert,
		StartedAt:       now.Add(-time.Hour),
		LastEvaluatedAt: now.Add(-time.Minute),
		Status:          IncidentStatusActive,
		SourceSummary:   "HTTP 探针连续失败 3 次",
	}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"target:tg_001": {previous},
		},
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", NodeID: "nd_001", ResultKind: agentapi.ProbeResultSuccess},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", NodeID: "nd_001", ResultKind: agentapi.ProbeResultSuccess},
				{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", NodeID: "nd_001", ResultKind: agentapi.ProbeResultFailure},
				{ObservedAt: now.Add(-3 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", NodeID: "nd_001", ResultKind: agentapi.ProbeResultFailure},
			},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(nodeRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		NodeID:       "nd_001",
		Observations: storeProbeBatch("tg_001"),
	}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	if len(writer.mutations) < 2 {
		t.Fatalf("mutations = %#v, want node and target mutations", writer.mutations)
	}
	targetMutation := writer.mutations[1]
	if len(targetMutation.Active) != 1 || targetMutation.Active[0].IncidentClass != IncidentTargetProbeFailure {
		t.Fatalf("target mutation = %#v, want target probe failure to remain active", targetMutation)
	}
}

func TestServiceEvaluatesTLSExpiryFromTLSOnlySeries(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	expiry := 2
	snapshots := &fakeSnapshotReader{
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http", NodeID: "nd_001", ResultKind: agentapi.ProbeResultSuccess},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindTLS, ProbeItemID: "pb_tls", NodeID: "nd_001", ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &expiry},
			},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(nodeRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		NodeID:       "nd_001",
		Observations: storeProbeBatch("tg_001"),
	}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.mutations) < 2 {
		t.Fatalf("mutations = %#v, want target mutation", writer.mutations)
	}
	targetMutation := writer.mutations[1]
	found := false
	for _, incident := range targetMutation.Active {
		if incident.IncidentClass == IncidentTargetTLSExpiry && incident.Severity == SeverityCritical {
			found = true
		}
	}
	if !found {
		t.Fatalf("target mutation = %#v, want TLS expiry incident from TLS-only series", targetMutation)
	}
}

func TestServiceMaintenanceTargetRecoveriesAreSilent(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	nodeRepo := &fakeNodeRepo{getNodeResult: nodes.Record{NodeID: "nd_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	expirySafeDays := 45
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"target:tg_probe": {{
				IncidentID:      "inc_target_tg_probe_target_probe_failure",
				ObjectType:      ObjectTypeTarget,
				ObjectID:        "tg_probe",
				IncidentClass:   IncidentTargetProbeFailure,
				Severity:        SeverityAlert,
				StartedAt:       now.Add(-time.Hour),
				LastEvaluatedAt: now.Add(-time.Minute),
				Status:          IncidentStatusActive,
				SourceSummary:   "HTTP 探针连续失败 3 次",
			}},
			"target:tg_tls": {{
				IncidentID:      "inc_target_tg_tls_target_tls_expiry",
				ObjectType:      ObjectTypeTarget,
				ObjectID:        "tg_tls",
				IncidentClass:   IncidentTargetTLSExpiry,
				Severity:        SeverityAlert,
				StartedAt:       now.Add(-2 * time.Hour),
				LastEvaluatedAt: now.Add(-time.Minute),
				Status:          IncidentStatusActive,
				SourceSummary:   "TLS 证书剩余 14 天",
			}},
		},
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_probe": {
				{ObservedAt: now, TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", NodeID: "nd_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", NodeID: "nd_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
				{ObservedAt: now, TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", NodeID: "nd_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", NodeID: "nd_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
			},
			"tg_tls": {
				{ObservedAt: now, TargetID: "tg_tls", ProbeKind: agentapi.ProbeKindTLS, ProbeItemID: "pb_tls_1", NodeID: "nd_001", ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &expirySafeDays, IsBackfilled: true},
			},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(nodeRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.evaluateTarget(context.Background(), "tg_probe", now); err != nil {
		t.Fatalf("evaluateTarget(tg_probe) error = %v", err)
	}
	if err := service.evaluateTarget(context.Background(), "tg_tls", now); err != nil {
		t.Fatalf("evaluateTarget(tg_tls) error = %v", err)
	}

	if len(writer.mutations) != 2 {
		t.Fatalf("len(mutations) = %d, want 2", len(writer.mutations))
	}
	for _, mutation := range writer.mutations {
		if len(mutation.Active) != 0 {
			t.Fatalf("mutation.Active = %#v, want recovered incidents removed", mutation.Active)
		}
		if len(mutation.Events) != 1 || mutation.Events[0].EventType != EventIncidentRecovered {
			t.Fatalf("mutation.Events = %#v, want recovered event", mutation.Events)
		}
	}
	if len(writer.notifications) != 2 {
		t.Fatalf("notifications = %#v, want two suppressed recovery batches", writer.notifications)
	}
	for _, batch := range writer.notifications {
		if len(batch) != 1 {
			t.Fatalf("notification batch = %#v, want one record", batch)
		}
		if batch[0].DeliveryStatus != DeliveryStatusSuppressed {
			t.Fatalf("DeliveryStatus = %q, want %q", batch[0].DeliveryStatus, DeliveryStatusSuppressed)
		}
	}
	if len(notifier.messages) != 0 {
		t.Fatalf("notifier.messages = %#v, want no Telegram sends for maintenance/backfill recoveries", notifier.messages)
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

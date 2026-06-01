package incidents

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/runtimefacts"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
)

type fakeMonitoringInstanceRepo struct {
	getMonitoringInstanceResult   monitoringinstances.Record
	listMonitoringInstancesResult []monitoringinstances.Record
}

func (f *fakeMonitoringInstanceRepo) GetMonitoringInstance(context.Context, string) (monitoringinstances.Record, error) {
	return f.getMonitoringInstanceResult, nil
}
func (f *fakeMonitoringInstanceRepo) ListMonitoringInstances(context.Context) ([]monitoringinstances.Record, error) {
	return f.listMonitoringInstancesResult, nil
}

type fakeTargetRepo struct{ listTargetsResult []targets.TargetRecord }

func (f *fakeTargetRepo) ListTargets(context.Context) ([]targets.TargetRecord, error) {
	return f.listTargetsResult, nil
}

type fakeSnapshotReader struct {
	activeByObject               map[string][]IncidentRecord
	hostSamples                  map[string][]runtimefacts.HostSample
	probeObs                     map[string][]runtimefacts.ProbeObservation
	monitoringInstanceAggregates map[string][]MonitoringInstanceHostDailyAggregate
	targetAggregates             map[string][]TargetProbeDailyAggregate
}

func (f *fakeSnapshotReader) ListActiveIncidents(_ context.Context, objectType ObjectType, objectID string) ([]IncidentRecord, error) {
	return append([]IncidentRecord(nil), f.activeByObject[string(objectType)+":"+objectID]...), nil
}
func (f *fakeSnapshotReader) ListRecentHostSamples(_ context.Context, monitoringInstanceID string, _ time.Time) ([]runtimefacts.HostSample, error) {
	return append([]runtimefacts.HostSample(nil), f.hostSamples[monitoringInstanceID]...), nil
}
func (f *fakeSnapshotReader) ListRecentProbeObservations(_ context.Context, targetID string, _ time.Time) ([]runtimefacts.ProbeObservation, error) {
	return append([]runtimefacts.ProbeObservation(nil), f.probeObs[targetID]...), nil
}
func (f *fakeSnapshotReader) ListMonitoringInstanceHostDailyAggregates(_ context.Context, monitoringInstanceID string, _, _ time.Time) ([]MonitoringInstanceHostDailyAggregate, error) {
	return append([]MonitoringInstanceHostDailyAggregate(nil), f.monitoringInstanceAggregates[monitoringInstanceID]...), nil
}
func (f *fakeSnapshotReader) ListTargetProbeDailyAggregates(_ context.Context, targetID string, _, _ time.Time) ([]TargetProbeDailyAggregate, error) {
	return append([]TargetProbeDailyAggregate(nil), f.targetAggregates[targetID]...), nil
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

type fakeNotificationDispatcher struct {
	deliveries []NotificationDelivery
	channels   []NotificationChannel
	summaries  []string
}

func (f *fakeNotificationDispatcher) Dispatch(_ context.Context, summary string) []NotificationDelivery {
	f.summaries = append(f.summaries, summary)
	return append([]NotificationDelivery(nil), f.deliveries...)
}

func (f *fakeNotificationDispatcher) NotificationChannels(context.Context) []NotificationChannel {
	return append([]NotificationChannel(nil), f.channels...)
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

func TestServiceNotificationFlags(t *testing.T) {
	makeEvaluation := func(reason NotificationReason) classEvaluation {
		return classEvaluation{
			class: IncidentMonitoringInstanceDiskPressure,
			result: EvaluationResult{
				Current: &IncidentRecord{
					IncidentID:    "inc_monitoring_instance_mi_001_monitoring_instance_disk_pressure",
					ObjectType:    ObjectTypeMonitoringInstance,
					ObjectID:      "mi_001",
					IncidentClass: IncidentMonitoringInstanceDiskPressure,
				},
				Notification: &NotificationDecision{
					ShouldSend: true,
					Channel:    NotificationChannelTelegram,
					Reason:     reason,
					Summary:    "summary",
				},
			},
		}
	}

	tests := []struct {
		name                 string
		reason               NotificationReason
		persistedDefaults    centersettings.IncidentDefaults
		settingsDefaults     centersettings.IncidentDefaults
		expectStatus         DeliveryStatus
		expectSendCount      int
		persistedAvailable   bool
		persistedUnavailable bool
		settingsAvailable    bool
		useNilRepo           bool
	}{
		{
			name:              "started suppressed by persisted defaults",
			reason:            NotificationReasonStarted,
			persistedDefaults: centersettings.IncidentDefaults{NotifyOnStarted: false, NotifyOnEscalated: true, NotifyOnRecovered: true},
			expectStatus:      DeliveryStatusSuppressed,
		},
		{
			name:              "escalated suppressed by persisted defaults",
			reason:            NotificationReasonEscalated,
			persistedDefaults: centersettings.IncidentDefaults{NotifyOnStarted: true, NotifyOnEscalated: false, NotifyOnRecovered: true},
			expectStatus:      DeliveryStatusSuppressed,
		},
		{
			name:              "recovered suppressed by persisted defaults",
			reason:            NotificationReasonRecovered,
			persistedDefaults: centersettings.IncidentDefaults{NotifyOnStarted: true, NotifyOnEscalated: true, NotifyOnRecovered: false},
			expectStatus:      DeliveryStatusSuppressed,
		},
		{
			name:               "started enabled by persisted defaults sends notification",
			reason:             NotificationReasonStarted,
			persistedDefaults:  centersettings.IncidentDefaults{NotifyOnStarted: true, NotifyOnEscalated: false, NotifyOnRecovered: false},
			expectStatus:       DeliveryStatusSent,
			expectSendCount:    1,
			persistedAvailable: true,
		},
		{
			name:            "defaults apply when settings repo is nil",
			reason:          NotificationReasonRecovered,
			expectStatus:    DeliveryStatusSent,
			expectSendCount: 1,
			useNilRepo:      true,
		},
		{
			name:                 "get settings suppresses when persisted defaults are unavailable",
			reason:               NotificationReasonRecovered,
			settingsDefaults:     centersettings.IncidentDefaults{NotifyOnStarted: true, NotifyOnEscalated: true, NotifyOnRecovered: false},
			expectStatus:         DeliveryStatusSuppressed,
			persistedUnavailable: true,
			settingsAvailable:    true,
		},
		{
			name:              "get settings sends when persisted defaults row is absent",
			reason:            NotificationReasonRecovered,
			settingsDefaults:  centersettings.IncidentDefaults{NotifyOnStarted: false, NotifyOnEscalated: false, NotifyOnRecovered: true},
			expectStatus:      DeliveryStatusSent,
			expectSendCount:   1,
			settingsAvailable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeMutationWriter{}
			notifier := &fakeNotifier{}

			var settingsRepo SettingsRepository
			if !tt.useNilRepo {
				repo := &fakeSettingsRepository{}
				if tt.persistedUnavailable {
					repo.persistedIncidentDefaultsErr = errors.New("persisted defaults unavailable")
				} else if tt.persistedAvailable || tt.expectStatus == DeliveryStatusSuppressed {
					repo.persistedIncidentDefaults = tt.persistedDefaults
					repo.persistedIncidentExists = true
				}
				if tt.settingsAvailable {
					repo.getSettingsResult = centersettings.CenterSettings{IncidentDefaults: tt.settingsDefaults}
				}
				settingsRepo = repo
			}

			service := NewSettingsBackedService(nil, nil, nil, writer, notifier, settingsRepo, slog.Default(), 30*time.Second, time.Minute)
			service.now = func() time.Time {
				return time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
			}

			if err := service.appendNotificationRecords(context.Background(), ObjectTypeMonitoringInstance, "mi_001", []classEvaluation{makeEvaluation(tt.reason)}); err != nil {
				t.Fatalf("appendNotificationRecords() error = %v", err)
			}
			if len(writer.notifications) != 1 || len(writer.notifications[0]) != 1 {
				t.Fatalf("notifications = %#v, want one record", writer.notifications)
			}
			record := writer.notifications[0][0]
			if record.DeliveryStatus != tt.expectStatus {
				t.Fatalf("DeliveryStatus = %q, want %q", record.DeliveryStatus, tt.expectStatus)
			}
			if got := len(notifier.messages); got != tt.expectSendCount {
				t.Fatalf("len(notifier.messages) = %d, want %d", got, tt.expectSendCount)
			}
		})
	}
}

func TestServiceAfterSuccessfulSyncEvaluatesMonitoringInstanceAndTouchedTargets(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{},
		hostSamples: map[string][]runtimefacts.HostSample{
			"mi_001": {{ObservedAt: now, DiskUsedPct: 92}},
		},
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
				{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503"},
			},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations:         storeProbeBatch("tg_001"),
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
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
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
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{err: errors.New("send failed")}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if writer.notifications[0][0].DeliveryStatus != DeliveryStatusFailed {
		t.Fatalf("DeliveryStatus = %q, want %q", writer.notifications[0][0].DeliveryStatus, DeliveryStatusFailed)
	}
}

func TestServiceRecordsNotificationDeliveryPerChannel(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	dispatcher := &fakeNotificationDispatcher{
		deliveries: []NotificationDelivery{
			{Channel: NotificationChannelTelegram, Status: DeliveryStatusSent},
			{Channel: NotificationChannelFeishu, Status: DeliveryStatusFailed, Error: errors.New("feishu failed")},
		},
		channels: []NotificationChannel{NotificationChannelTelegram, NotificationChannelFeishu},
	}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.dispatcher = dispatcher
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(dispatcher.summaries) != 1 {
		t.Fatalf("dispatcher summaries = %#v, want one send", dispatcher.summaries)
	}
	if len(writer.notifications) != 1 || len(writer.notifications[0]) != 2 {
		t.Fatalf("notifications = %#v, want two channel records", writer.notifications)
	}
	gotByChannel := map[NotificationChannel]NotificationRecordWrite{}
	for _, record := range writer.notifications[0] {
		gotByChannel[record.Channel] = record
	}
	telegram := gotByChannel[NotificationChannelTelegram]
	if telegram.DeliveryStatus != DeliveryStatusSent || telegram.SentAt == nil {
		t.Fatalf("telegram record = %#v, want sent with sent_at", telegram)
	}
	feishu := gotByChannel[NotificationChannelFeishu]
	if feishu.DeliveryStatus != DeliveryStatusFailed || feishu.SentAt != nil {
		t.Fatalf("feishu record = %#v, want failed without sent_at", feishu)
	}
}

func TestServiceSuppressesNotificationPerResolvedChannel(t *testing.T) {
	writer := &fakeMutationWriter{}
	dispatcher := &fakeNotificationDispatcher{
		channels: []NotificationChannel{NotificationChannelTelegram, NotificationChannelFeishu},
	}
	settingsRepo := &fakeSettingsRepository{
		persistedIncidentDefaults: centersettings.IncidentDefaults{
			NotifyOnStarted:   false,
			NotifyOnEscalated: true,
			NotifyOnRecovered: true,
		},
		persistedIncidentExists: true,
	}
	service := NewSettingsBackedService(nil, nil, nil, writer, nil, settingsRepo, slog.Default(), 30*time.Second, time.Minute)
	service.dispatcher = dispatcher

	evaluation := classEvaluation{
		class: IncidentMonitoringInstanceDiskPressure,
		result: EvaluationResult{
			Current: &IncidentRecord{
				IncidentID:    "inc_monitoring_instance_mi_001_monitoring_instance_disk_pressure",
				ObjectType:    ObjectTypeMonitoringInstance,
				ObjectID:      "mi_001",
				IncidentClass: IncidentMonitoringInstanceDiskPressure,
			},
			Notification: &NotificationDecision{
				ShouldSend: true,
				Channel:    NotificationChannelTelegram,
				Reason:     NotificationReasonStarted,
				Summary:    "summary",
			},
		},
	}

	if err := service.appendNotificationRecords(context.Background(), ObjectTypeMonitoringInstance, "mi_001", []classEvaluation{evaluation}); err != nil {
		t.Fatalf("appendNotificationRecords() error = %v", err)
	}
	if len(dispatcher.summaries) != 0 {
		t.Fatalf("dispatcher summaries = %#v, want no sends when policy suppresses", dispatcher.summaries)
	}
	if len(writer.notifications) != 1 || len(writer.notifications[0]) != 2 {
		t.Fatalf("notifications = %#v, want two suppressed records", writer.notifications)
	}
	for _, record := range writer.notifications[0] {
		if record.DeliveryStatus != DeliveryStatusSuppressed {
			t.Fatalf("record = %#v, want suppressed", record)
		}
	}
}

func TestSettingsAwareNotifierSuppressesWhenPersistedTelegramIsDisabled(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
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
		monitoringInstanceRepo,
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

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
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
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
	}
	writer := &fakeMutationWriter{}
	fallbackNotifier := &fakeNotifier{}
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult: centersettings.Default(),
		persistedExists:   false,
	}
	service := NewService(
		monitoringInstanceRepo,
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

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
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
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
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
		monitoringInstanceRepo,
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

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
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
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{"mi_001": {{ObservedAt: now, DiskUsedPct: 92}}},
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
		monitoringInstanceRepo,
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

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
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

func TestSettingsAwareNotificationDispatcherSendsFeishuOnly(t *testing.T) {
	settings := centersettings.Default()
	settings.FeishuEnabled = true
	settings.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/test"
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult: settings,
		persistedTelegram: centersettings.TelegramSettings{RuntimeManaged: true},
		persistedExists:   true,
	}
	feishuNotifier := &fakeNotifier{}
	buildTelegramCalls := 0
	var gotWebhookURL string
	dispatcher := NewSettingsAwareNotificationDispatcher(
		settingsRepo,
		func(botToken, chatID string) Notifier {
			buildTelegramCalls++
			return &fakeNotifier{}
		},
		func(webhookURL string) Notifier {
			gotWebhookURL = webhookURL
			return feishuNotifier
		},
		&fakeNotifier{},
	)

	deliveries := dispatcher.Dispatch(context.Background(), "incident started")
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %#v, want one feishu delivery", deliveries)
	}
	gotByChannel := deliveriesByChannel(deliveries)
	if gotByChannel[NotificationChannelFeishu].Status != DeliveryStatusSent {
		t.Fatalf("feishu delivery = %#v, want sent", gotByChannel[NotificationChannelFeishu])
	}
	if gotWebhookURL != settings.FeishuWebhookURL {
		t.Fatalf("got webhook URL = %q, want settings webhook", gotWebhookURL)
	}
	if len(feishuNotifier.messages) != 1 || feishuNotifier.messages[0] != "incident started" {
		t.Fatalf("feishu messages = %#v, want one summary", feishuNotifier.messages)
	}
	if buildTelegramCalls != 0 {
		t.Fatalf("buildTelegramCalls = %d, want 0 for disabled persisted Telegram", buildTelegramCalls)
	}
}

func TestSettingsAwareNotificationDispatcherSendsTelegramAndFeishu(t *testing.T) {
	settings := centersettings.Default()
	settings.Telegram = centersettings.TelegramSettings{
		BotToken:       "persisted-bot-token",
		ChatID:         "persisted-chat-id",
		RuntimeManaged: true,
	}
	settings.FeishuEnabled = true
	settings.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/test"
	settingsRepo := &fakeSettingsRepository{
		getSettingsResult: settings,
		persistedTelegram: settings.Telegram,
		persistedExists:   true,
	}
	telegramNotifier := &fakeNotifier{}
	feishuNotifier := &fakeNotifier{err: errors.New("feishu failed")}
	dispatcher := NewSettingsAwareNotificationDispatcher(
		settingsRepo,
		func(botToken, chatID string) Notifier {
			if botToken != settings.Telegram.BotToken || chatID != settings.Telegram.ChatID {
				t.Fatalf("telegram config = %q/%q, want persisted settings", botToken, chatID)
			}
			return telegramNotifier
		},
		func(string) Notifier { return feishuNotifier },
		&fakeNotifier{},
	)

	deliveries := dispatcher.Dispatch(context.Background(), "incident started")
	if len(deliveries) != 2 {
		t.Fatalf("deliveries = %#v, want two channel deliveries", deliveries)
	}
	gotByChannel := deliveriesByChannel(deliveries)
	if gotByChannel[NotificationChannelTelegram].Status != DeliveryStatusSent {
		t.Fatalf("telegram delivery = %#v, want sent", gotByChannel[NotificationChannelTelegram])
	}
	feishu := gotByChannel[NotificationChannelFeishu]
	if feishu.Status != DeliveryStatusFailed || feishu.Error == nil {
		t.Fatalf("feishu delivery = %#v, want failed with error", feishu)
	}
	if len(telegramNotifier.messages) != 1 || len(feishuNotifier.messages) != 1 {
		t.Fatalf("telegram messages = %#v, feishu messages = %#v, want one each", telegramNotifier.messages, feishuNotifier.messages)
	}
}

func TestSettingsBackedHeartbeatIntervalUsesPersistedSettings(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	stale := now.Add(-75 * time.Second)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &stale}}}
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
	service := NewSettingsBackedService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, settingsRepo, slog.Default(), 30*time.Second, time.Minute)

	if err := service.EvaluateStaleMonitoringInstances(context.Background(), now); err != nil {
		t.Fatalf("EvaluateStaleMonitoringInstances() error = %v", err)
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

func TestServiceEvaluateStaleMonitoringInstancesCreatesHeartbeatIncident(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	stale := now.Add(-3 * time.Minute)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{listMonitoringInstancesResult: []monitoringinstances.Record{{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &stale}}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), time.Minute, time.Minute)

	if err := service.EvaluateStaleMonitoringInstances(context.Background(), now); err != nil {
		t.Fatalf("EvaluateStaleMonitoringInstances() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 1 || writer.mutations[0].Active[0].IncidentClass != IncidentMonitoringInstanceHeartbeatMissing {
		t.Fatalf("mutation = %#v, want heartbeat incident", writer.mutations[0])
	}
}

func TestServiceAfterSuccessfulSyncUsesStoredLoadForResourcePressure(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{
			"mi_001": {
				{ObservedAt: now, Load5: 1.9},
				{ObservedAt: now.Add(-8 * time.Minute), Load5: 2.0},
				{ObservedAt: now.Add(-15 * time.Minute), Load5: 1.8},
			},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	found := false
	for _, incident := range writer.mutations[0].Active {
		if incident.IncidentClass == IncidentMonitoringInstanceResourcePressure {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("mutation = %#v, want load-driven resource incident", writer.mutations[0])
	}
}

func TestServiceAfterSuccessfulSyncDispatchesMonitoringInstanceTrendDegradation(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		hostSamples: map[string][]runtimefacts.HostSample{
			"mi_001": {
				{ObservedAt: now, Load5: 2.1, CPUIOWaitPct: 12, CPUStealPct: 0.5},
				{ObservedAt: now.Add(-30 * time.Minute), Load5: 2.0, CPUIOWaitPct: 11, CPUStealPct: 0.4},
				{ObservedAt: now.Add(-23 * time.Hour), Load5: 1.9, CPUIOWaitPct: 13, CPUStealPct: 0.5},
			},
		},
		monitoringInstanceAggregates: map[string][]MonitoringInstanceHostDailyAggregate{
			"mi_001": {{
				BucketDate:      now.AddDate(0, 0, -1),
				SampleCount:     288,
				AvgLoad5:        0.7,
				AvgCPUIOWaitPct: 2,
				AvgCPUStealPct:  0.2,
			}},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if !mutationsContainIncident(writer.mutations, IncidentMonitoringInstanceTrendDegradation) {
		t.Fatalf("mutations = %#v, want monitoringInstance trend degradation incident", writer.mutations)
	}
}

func TestServiceAfterSuccessfulSyncDispatchesTargetLatencyTrendDegradation(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	latencyA := 450
	latencyB := 460
	latencyC := 470
	baselineLatency := 120.0
	snapshots := &fakeSnapshotReader{
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, LatencyMS: &latencyA},
				{ObservedAt: now.Add(-30 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_002", ResultKind: agentapi.ProbeResultSuccess, LatencyMS: &latencyB},
				{ObservedAt: now.Add(-23 * time.Hour), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_001", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, LatencyMS: &latencyC},
			},
		},
		targetAggregates: map[string][]TargetProbeDailyAggregate{
			"tg_001": {{
				TargetID:         "tg_001",
				ProbeItemID:      "pb_001",
				BucketDate:       now.AddDate(0, 0, -1),
				ObservationCount: 288,
				SuccessCount:     288,
				AvgLatencyMS:     &baselineLatency,
			}},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations:         storeProbeBatch("tg_001"),
	}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if !mutationsContainIncident(writer.mutations, IncidentTargetLatencyTrendDegradation) {
		t.Fatalf("mutations = %#v, want target latency trend degradation incident", writer.mutations)
	}
}

func TestServiceSkippedEvaluationPreservesPriorIncidentDuringMaintenance(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"monitoring_instance:mi_001": {{
				IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_disk_pressure",
				ObjectType:      ObjectTypeMonitoringInstance,
				ObjectID:        "mi_001",
				IncidentClass:   IncidentMonitoringInstanceDiskPressure,
				Severity:        SeverityAlert,
				StartedAt:       now.Add(-time.Hour),
				LastEvaluatedAt: now.Add(-time.Minute),
				Status:          IncidentStatusActive,
				SourceSummary:   "磁盘使用率 92.0%",
			}},
		},
		hostSamples: map[string][]runtimefacts.HostSample{
			"mi_001": {{ObservedAt: now, DiskUsedPct: 99, MaintenanceContext: true}},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(writer.mutations) != 1 {
		t.Fatalf("len(mutations) = %d, want 1", len(writer.mutations))
	}
	if len(writer.mutations[0].Active) != 1 || writer.mutations[0].Active[0].IncidentClass != IncidentMonitoringInstanceDiskPressure {
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
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		activeByObject: map[string][]IncidentRecord{
			"monitoring_instance:mi_001": {{
				IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_disk_pressure",
				ObjectType:      ObjectTypeMonitoringInstance,
				ObjectID:        "mi_001",
				IncidentClass:   IncidentMonitoringInstanceDiskPressure,
				Severity:        SeverityAlert,
				StartedAt:       now.Add(-time.Hour),
				LastEvaluatedAt: now.Add(-time.Minute),
				Status:          IncidentStatusActive,
				SourceSummary:   "磁盘使用率 92.0%",
			}},
		},
		hostSamples: map[string][]runtimefacts.HostSample{
			"mi_001": {{ObservedAt: now, DiskUsedPct: 40, MaintenanceContext: true}},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{MonitoringInstanceID: "mi_001"}, syncing.Result{AcceptedAt: now}); err != nil {
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
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
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
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess},
				{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure},
				{ObservedAt: now.Add(-3 * time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure},
			},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations:         storeProbeBatch("tg_001"),
	}, syncing.Result{AcceptedAt: now}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	if len(writer.mutations) < 2 {
		t.Fatalf("mutations = %#v, want monitoringInstance and target mutations", writer.mutations)
	}
	targetMutation := writer.mutations[1]
	if len(targetMutation.Active) != 1 || targetMutation.Active[0].IncidentClass != IncidentTargetProbeFailure {
		t.Fatalf("target mutation = %#v, want target probe failure to remain active", targetMutation)
	}
}

func TestServiceAfterSuccessfulSyncDoesNotNotifyForBackfilledProbeFailure(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	snapshots := &fakeSnapshotReader{
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeItemID: "pb_001", ProbeKind: agentapi.ProbeKindHTTP, MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503", IsBackfilled: true},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeItemID: "pb_001", ProbeKind: agentapi.ProbeKindHTTP, MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503", IsBackfilled: true},
				{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_001", ProbeItemID: "pb_001", ProbeKind: agentapi.ProbeKindHTTP, MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503", IsBackfilled: true},
			},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations: observations.BatchWrite{
			ProbeObservations: []observations.ProbeObservationWrite{{
				MonitoringInstanceID: "mi_001",
				TargetID:             "tg_001",
				ProbeItemID:          "pb_001",
				ProbeKind:            agentapi.ProbeKindHTTP,
				ObservedAt:           now,
				ResultKind:           agentapi.ProbeResultFailure,
				ErrorSummary:         "503",
				IsBackfilled:         true,
			}},
		},
	}, syncing.Result{AcceptedAt: now})
	if err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	if len(writer.mutations) < 2 {
		t.Fatalf("mutations = %#v, want monitoringInstance and target mutations", writer.mutations)
	}
	targetMutation := writer.mutations[1]
	if targetMutation.ObjectType != ObjectTypeTarget || targetMutation.ObjectID != "tg_001" {
		t.Fatalf("target mutation identity = %#v, want target tg_001", targetMutation)
	}
	if len(targetMutation.Active) != 0 {
		t.Fatalf("target mutation Active = %#v, want no active backfilled incident", targetMutation.Active)
	}
	if len(targetMutation.Events) != 0 {
		t.Fatalf("target mutation Events = %#v, want no notification-driving events", targetMutation.Events)
	}
	if len(writer.notifications) != 0 {
		t.Fatalf("notifications = %#v, want none for backfilled probe failure", writer.notifications)
	}
	if len(notifier.messages) != 0 {
		t.Fatalf("notifier.messages = %#v, want no sends for backfilled probe failure", notifier.messages)
	}
}

func TestServiceEvaluatesTLSExpiryFromTLSOnlySeries(t *testing.T) {
	now := time.Date(2026, time.April, 25, 14, 0, 0, 0, time.UTC)
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
	targetRepo := &fakeTargetRepo{}
	expiry := 2
	snapshots := &fakeSnapshotReader{
		probeObs: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeKind: agentapi.ProbeKindTLS, ProbeItemID: "pb_tls", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &expiry},
			},
		},
	}
	writer := &fakeMutationWriter{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, nil, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	if err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations:         storeProbeBatch("tg_001"),
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
	monitoringInstanceRepo := &fakeMonitoringInstanceRepo{getMonitoringInstanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LastHeartbeatAt: &now}}
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
				{ObservedAt: now, TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_1", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
				{ObservedAt: now, TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_probe", ProbeKind: agentapi.ProbeKindHTTP, ProbeItemID: "pb_http_2", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, MaintenanceContext: true},
			},
			"tg_tls": {
				{ObservedAt: now, TargetID: "tg_tls", ProbeKind: agentapi.ProbeKindTLS, ProbeItemID: "pb_tls_1", MonitoringInstanceID: "mi_001", ResultKind: agentapi.ProbeResultSuccess, TLSExpiryDays: &expirySafeDays, IsBackfilled: true},
			},
		},
	}
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	service := NewService(monitoringInstanceRepo, targetRepo, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
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
			MonitoringInstanceID: "mi_001",
			TargetID:             targetID,
			ProbeItemID:          "pb_001",
			ProbeKind:            agentapi.ProbeKindHTTP,
			ResultKind:           agentapi.ProbeResultFailure,
			LatencyMS:            &latency,
			HTTPStatus:           &status,
			ErrorSummary:         "503",
		}},
	}
}

func deliveriesByChannel(deliveries []NotificationDelivery) map[NotificationChannel]NotificationDelivery {
	out := make(map[NotificationChannel]NotificationDelivery, len(deliveries))
	for _, delivery := range deliveries {
		out[delivery.Channel] = delivery
	}
	return out
}

func mutationsContainIncident(mutations []IncidentMutation, class IncidentClass) bool {
	for _, mutation := range mutations {
		for _, incident := range mutation.Active {
			if incident.IncidentClass == class {
				return true
			}
		}
	}
	return false
}

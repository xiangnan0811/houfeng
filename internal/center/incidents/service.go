package incidents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/notify"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/runtimefacts"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
)

const defaultHeartbeatInterval = 5 * time.Second

var errNotificationSuppressed = errors.New("incident notification suppressed")

type MonitoringInstanceRepository interface {
	GetMonitoringInstance(context.Context, string) (monitoringinstances.Record, error)
	ListMonitoringInstances(context.Context, ...monitoringinstances.ListScope) ([]monitoringinstances.Record, error)
}

type TargetRepository interface {
	GetTarget(context.Context, string) (targets.TargetRecord, error)
	ListTargets(context.Context) ([]targets.TargetRecord, error)
}

type SnapshotReader interface {
	GetObjectRowVersion(context.Context, ObjectType, string) (string, error)
	ListActiveIncidents(context.Context, ObjectType, string) ([]IncidentRecord, error)
	ListRecentHostSamples(context.Context, string, time.Time) ([]runtimefacts.HostSample, error)
	ListRecentProbeObservations(context.Context, string, time.Time) ([]runtimefacts.ProbeObservation, error)
	ListMonitoringInstanceHostDailyAggregates(context.Context, string, time.Time, time.Time) ([]MonitoringInstanceHostDailyAggregate, error)
	ListTargetProbeDailyAggregates(context.Context, string, time.Time, time.Time) ([]TargetProbeDailyAggregate, error)
}

type MutationWriter interface {
	ApplyIncidentMutation(context.Context, IncidentMutation) error
	AppendNotificationRecords(context.Context, []NotificationRecordWrite) error
}

type SettingsRepository interface {
	GetSettings(context.Context) (centersettings.CenterSettings, error)
}

type persistedTelegramSettingsSource interface {
	GetPersistedTelegramSettings(context.Context) (centersettings.TelegramSettings, bool, error)
}

type persistedIncidentDefaultsSource interface {
	GetPersistedIncidentDefaults(context.Context) (centersettings.IncidentDefaults, bool, error)
}

type Notifier interface {
	Send(context.Context, string) error
}

type NotificationDelivery struct {
	Channel NotificationChannel
	Status  DeliveryStatus
	Error   error
}

type NotificationDispatcher interface {
	Dispatch(context.Context, string) []NotificationDelivery
}

type NotificationChannelResolver interface {
	NotificationChannels(context.Context) []NotificationChannel
}

type SettingsAwareNotifierFactory func(botToken, chatID string) Notifier
type FeishuNotifierFactory func(webhookURL string) Notifier

type settingsAwareNotifier struct {
	settingsRepo        SettingsRepository
	newTelegramNotifier SettingsAwareNotifierFactory
	newFeishuNotifier   FeishuNotifierFactory
	fallback            Notifier
}

func NewSettingsAwareNotifier(settingsRepo SettingsRepository, newNotifier SettingsAwareNotifierFactory, fallback Notifier) Notifier {
	if settingsRepo == nil {
		return fallback
	}
	return &settingsAwareNotifier{
		settingsRepo:        settingsRepo,
		newTelegramNotifier: newNotifier,
		newFeishuNotifier: func(webhookURL string) Notifier {
			return notify.NewFeishuNotifier(webhookURL)
		},
		fallback: fallback,
	}
}

func NewSettingsAwareNotificationDispatcher(settingsRepo SettingsRepository, newTelegramNotifier SettingsAwareNotifierFactory, newFeishuNotifier FeishuNotifierFactory, fallback Notifier) NotificationDispatcher {
	if settingsRepo == nil {
		return notifierDispatcher{channel: NotificationChannelTelegram, notifier: fallback}
	}
	return &settingsAwareNotifier{
		settingsRepo:        settingsRepo,
		newTelegramNotifier: newTelegramNotifier,
		newFeishuNotifier:   newFeishuNotifier,
		fallback:            fallback,
	}
}

func (n *settingsAwareNotifier) Send(ctx context.Context, summary string) error {
	return dispatchError(n.Dispatch(ctx, summary))
}

func (n *settingsAwareNotifier) NotificationChannels(ctx context.Context) []NotificationChannel {
	var telegramTelegram *centersettings.TelegramSettings
	var telegramExists bool

	if source, ok := n.settingsRepo.(persistedTelegramSettingsSource); ok {
		t, exists, err := source.GetPersistedTelegramSettings(ctx)
		if err == nil {
			telegramTelegram = &t
			telegramExists = exists
		}
	}

	settings, err := n.settingsRepo.GetSettings(ctx)
	if err != nil {
		if telegramTelegram != nil || n.fallback != nil {
			return []NotificationChannel{NotificationChannelTelegram}
		}
		return nil
	}
	if telegramTelegram != nil {
		settings.Telegram = *telegramTelegram
	}

	feishuConfigured := settings.FeishuEnabled && settings.FeishuWebhookURL != ""
	channels := make([]NotificationChannel, 0, 2)
	if telegramChannel(settings.Telegram, telegramExists, n.fallback != nil, feishuConfigured) {
		channels = append(channels, NotificationChannelTelegram)
	}
	if feishuConfigured {
		channels = append(channels, NotificationChannelFeishu)
	}
	return uniqueNotificationChannels(channels)
}

func (n *settingsAwareNotifier) Dispatch(ctx context.Context, summary string) []NotificationDelivery {
	var telegramTelegram *centersettings.TelegramSettings
	var telegramExists bool

	if source, ok := n.settingsRepo.(persistedTelegramSettingsSource); ok {
		t, exists, err := source.GetPersistedTelegramSettings(ctx)
		if err == nil {
			telegramTelegram = &t
			telegramExists = exists
		}
	}

	settings, err := n.settingsRepo.GetSettings(ctx)
	if err != nil {
		if telegramTelegram != nil {
			return []NotificationDelivery{n.dispatchTelegram(ctx, summary, *telegramTelegram, telegramExists)}
		}
		if n.fallback != nil {
			return []NotificationDelivery{dispatchWithNotifier(ctx, NotificationChannelTelegram, n.fallback, summary)}
		}
		return []NotificationDelivery{{Channel: NotificationChannelTelegram, Status: DeliveryStatusFailed, Error: fmt.Errorf("get persisted center settings: %w", err)}}
	}

	// Override Telegram settings with persisted-telegram source if available.
	if telegramTelegram != nil {
		settings.Telegram = *telegramTelegram
	}

	feishuConfigured := settings.FeishuEnabled && settings.FeishuWebhookURL != ""
	deliveries := make([]NotificationDelivery, 0, 2)

	telegramDelivery := n.dispatchTelegram(ctx, summary, settings.Telegram, true)
	if telegramDelivery.Status != DeliveryStatusSuppressed || (!feishuConfigured && (telegramExists || settings.Telegram.RuntimeManaged)) {
		deliveries = append(deliveries, telegramDelivery)
	}

	if feishuConfigured {
		deliveries = append(deliveries, n.dispatchFeishu(ctx, summary, settings.FeishuWebhookURL))
	}

	if len(deliveries) == 0 {
		return []NotificationDelivery{{Channel: NotificationChannelTelegram, Status: DeliveryStatusSuppressed, Error: errNotificationSuppressed}}
	}

	return deliveries
}

func (n *settingsAwareNotifier) dispatchTelegram(ctx context.Context, summary string, telegram centersettings.TelegramSettings, exists bool) NotificationDelivery {
	if !exists || !telegram.RuntimeManaged {
		if n.fallback != nil {
			return dispatchWithNotifier(ctx, NotificationChannelTelegram, n.fallback, summary)
		}
		return NotificationDelivery{Channel: NotificationChannelTelegram, Status: DeliveryStatusSuppressed, Error: errNotificationSuppressed}
	}
	if !telegram.Enabled() {
		return NotificationDelivery{Channel: NotificationChannelTelegram, Status: DeliveryStatusSuppressed, Error: errNotificationSuppressed}
	}
	if n.newTelegramNotifier == nil {
		return NotificationDelivery{Channel: NotificationChannelTelegram, Status: DeliveryStatusFailed, Error: errors.New("telegram notifier factory is nil")}
	}
	notifier := n.newTelegramNotifier(telegram.BotToken, telegram.ChatID)
	if notifier == nil {
		return NotificationDelivery{Channel: NotificationChannelTelegram, Status: DeliveryStatusFailed, Error: errors.New("telegram notifier is nil")}
	}
	return dispatchWithNotifier(ctx, NotificationChannelTelegram, notifier, summary)
}

func (n *settingsAwareNotifier) dispatchFeishu(ctx context.Context, summary, webhookURL string) NotificationDelivery {
	if n.newFeishuNotifier == nil {
		return NotificationDelivery{Channel: NotificationChannelFeishu, Status: DeliveryStatusFailed, Error: errors.New("feishu notifier factory is nil")}
	}
	notifier := n.newFeishuNotifier(webhookURL)
	if notifier == nil {
		return NotificationDelivery{Channel: NotificationChannelFeishu, Status: DeliveryStatusFailed, Error: errors.New("feishu notifier is nil")}
	}
	return dispatchWithNotifier(ctx, NotificationChannelFeishu, notifier, summary)
}

type notifierDispatcher struct {
	channel  NotificationChannel
	notifier Notifier
}

func (d notifierDispatcher) NotificationChannels(context.Context) []NotificationChannel {
	if d.notifier == nil {
		return nil
	}
	return []NotificationChannel{d.channel}
}

func (d notifierDispatcher) Dispatch(ctx context.Context, summary string) []NotificationDelivery {
	if d.notifier == nil {
		return []NotificationDelivery{{Channel: d.channel, Status: DeliveryStatusSuppressed, Error: errNotificationSuppressed}}
	}
	return []NotificationDelivery{dispatchWithNotifier(ctx, d.channel, d.notifier, summary)}
}

func dispatchWithNotifier(ctx context.Context, channel NotificationChannel, notifier Notifier, summary string) NotificationDelivery {
	if notifier == nil {
		return NotificationDelivery{Channel: channel, Status: DeliveryStatusSuppressed, Error: errNotificationSuppressed}
	}
	if err := notifier.Send(ctx, summary); err != nil {
		if errors.Is(err, errNotificationSuppressed) {
			return NotificationDelivery{Channel: channel, Status: DeliveryStatusSuppressed, Error: err}
		}
		return NotificationDelivery{Channel: channel, Status: DeliveryStatusFailed, Error: err}
	}
	return NotificationDelivery{Channel: channel, Status: DeliveryStatusSent}
}

func dispatchError(deliveries []NotificationDelivery) error {
	var lastErr error
	sent := false
	for _, delivery := range deliveries {
		switch delivery.Status {
		case DeliveryStatusFailed:
			lastErr = delivery.Error
		case DeliveryStatusSent:
			sent = true
		case DeliveryStatusSuppressed:
			if lastErr == nil {
				lastErr = delivery.Error
			}
		}
	}
	if sent && (lastErr == nil || errors.Is(lastErr, errNotificationSuppressed)) {
		return nil
	}
	return lastErr
}

func telegramChannel(settings centersettings.TelegramSettings, exists, hasFallback, feishuConfigured bool) bool {
	if !exists || !settings.RuntimeManaged {
		return hasFallback
	}
	return settings.Enabled() || !feishuConfigured
}

func notificationDispatcherFor(notifier Notifier) NotificationDispatcher {
	if notifier == nil {
		return nil
	}
	if dispatcher, ok := notifier.(NotificationDispatcher); ok {
		return dispatcher
	}
	return notifierDispatcher{channel: NotificationChannelTelegram, notifier: notifier}
}

type Service struct {
	monitoringInstances       MonitoringInstanceRepository
	targets                   TargetRepository
	snapshots                 SnapshotReader
	writer                    MutationWriter
	dispatcher                NotificationDispatcher
	settingsRepo              SettingsRepository
	logger                    *slog.Logger
	now                       func() time.Time
	fallbackHeartbeatInterval time.Duration
	fallbackSweepInterval     time.Duration
	projectionConflictWarnMu  sync.Mutex
	projectionConflictWarnAt  map[ObjectType]time.Time
}

func NewService(monitoringInstancesRepo MonitoringInstanceRepository, targetsRepo TargetRepository, snapshots SnapshotReader, writer MutationWriter, notifier Notifier, logger *slog.Logger, heartbeatInterval, sweepInterval time.Duration) *Service {
	return NewSettingsBackedService(monitoringInstancesRepo, targetsRepo, snapshots, writer, notifier, nil, logger, heartbeatInterval, sweepInterval)
}

func NewSettingsBackedService(monitoringInstancesRepo MonitoringInstanceRepository, targetsRepo TargetRepository, snapshots SnapshotReader, writer MutationWriter, notifier Notifier, settingsRepo SettingsRepository, logger *slog.Logger, heartbeatInterval, sweepInterval time.Duration) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultHeartbeatInterval
	}
	if sweepInterval <= 0 {
		sweepInterval = 5 * time.Second
	}
	return &Service{
		monitoringInstances:       monitoringInstancesRepo,
		targets:                   targetsRepo,
		snapshots:                 snapshots,
		writer:                    writer,
		dispatcher:                notificationDispatcherFor(notifier),
		settingsRepo:              settingsRepo,
		logger:                    logger,
		now:                       func() time.Time { return time.Now().UTC() },
		fallbackHeartbeatInterval: heartbeatInterval,
		fallbackSweepInterval:     sweepInterval,
		projectionConflictWarnAt:  make(map[ObjectType]time.Time),
	}
}

func (s *Service) AfterSuccessfulSync(ctx context.Context, batch syncing.Batch, result syncing.Result) error {
	now := result.AcceptedAt
	if now.IsZero() {
		now = s.now()
	}
	if err := s.evaluateMonitoringInstance(ctx, batch.MonitoringInstanceID, now); err != nil {
		s.logger.Error("evaluate monitoring instance incidents after sync failed", "monitoring_instance_id", batch.MonitoringInstanceID, "error", err)
	}
	for _, targetID := range uniqueTargetIDs(batch.Observations.ProbeObservations) {
		if err := s.evaluateTarget(ctx, targetID, now); err != nil {
			s.logger.Error("evaluate target incidents after sync failed", "target_id", targetID, "error", err)
		}
	}
	return nil
}

func (s *Service) Run(ctx context.Context) error {
	timer := time.NewTimer(s.sweepIntervalFor(ctx))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case tick := <-timer.C:
			if err := s.EvaluatePeriodicState(ctx, tick.UTC()); err != nil {
				return err
			}
			nextTick := tick.Add(s.sweepIntervalFor(ctx))
			delay := time.Until(nextTick)
			if delay < 0 {
				delay = 0
			}
			timer.Reset(delay)
		}
	}
}

func (s *Service) EvaluatePeriodicState(ctx context.Context, now time.Time) error {
	if err := s.EvaluateStaleMonitoringInstances(ctx, now); err != nil {
		return err
	}
	if s.targets == nil {
		return nil
	}
	targetRecords, err := s.targets.ListTargets(ctx)
	if err != nil {
		return fmt.Errorf("list targets for periodic sweep: %w", err)
	}
	for _, target := range targetRecords {
		if err := s.evaluateTarget(ctx, target.TargetID, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) EvaluateStaleMonitoringInstances(ctx context.Context, now time.Time) error {
	records, err := s.monitoringInstances.ListMonitoringInstances(ctx)
	if err != nil {
		return fmt.Errorf("list monitoring instances for stale sweep: %w", err)
	}
	for _, record := range records {
		if err := s.evaluateMonitoringInstanceHeartbeatOnly(ctx, record.MonitoringInstanceID, now); err != nil {
			return err
		}
	}
	return nil
}

func isInactiveMonitoringInstance(record monitoringinstances.Record) bool {
	if record.MonitoringStatus == monitoringinstances.MonitoringPaused || record.MonitoringStatus == monitoringinstances.MonitoringMaintenance {
		return true
	}
	if record.LifecycleStatus == monitoringinstances.LifecycleRetired {
		return true
	}
	if record.ArchivedAt != nil {
		return true
	}
	return false
}

func shouldRecoverIncidentsForTarget(record targets.TargetRecord) bool {
	switch record.RunStatus {
	case targets.RunStatusPaused, targets.RunStatusArchived:
		return true
	default:
		return false
	}
}

func (s *Service) evaluateMonitoringInstanceHeartbeatOnly(ctx context.Context, monitoringInstanceID string, now time.Time) error {
	return s.evaluateObjectWithRetry(ctx, ObjectTypeMonitoringInstance, monitoringInstanceID, now, func(attemptNow time.Time) evaluationAttemptResult {
		return s.evaluateMonitoringInstanceHeartbeatOnlyAttempt(ctx, monitoringInstanceID, attemptNow)
	})
}

func (s *Service) evaluateMonitoringInstance(ctx context.Context, monitoringInstanceID string, now time.Time) error {
	return s.evaluateObjectWithRetry(ctx, ObjectTypeMonitoringInstance, monitoringInstanceID, now, func(attemptNow time.Time) evaluationAttemptResult {
		return s.evaluateMonitoringInstanceAttempt(ctx, monitoringInstanceID, attemptNow)
	})
}

func (s *Service) evaluateMonitoringInstanceHeartbeatOnlyAttempt(ctx context.Context, monitoringInstanceID string, now time.Time) evaluationAttemptResult {
	rowVersion, err := s.objectRowVersion(ctx, ObjectTypeMonitoringInstance, monitoringInstanceID)
	if err != nil {
		return evaluationAttemptResult{err: err}
	}
	record, err := s.monitoringInstances.GetMonitoringInstance(ctx, monitoringInstanceID)
	if err != nil {
		if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			return evaluationAttemptResult{err: ErrIncidentProjectionObjectNotFound}
		}
		return evaluationAttemptResult{err: fmt.Errorf("get monitoring instance %q for incident evaluation: %w", monitoringInstanceID, err)}
	}
	previous, err := s.snapshots.ListActiveIncidents(ctx, ObjectTypeMonitoringInstance, monitoringInstanceID)
	if err != nil {
		return evaluationAttemptResult{err: fmt.Errorf("list previous monitoring instance incidents for %q: %w", monitoringInstanceID, err)}
	}
	if isInactiveMonitoringInstance(record) {
		if len(previous) == 0 {
			return evaluationAttemptResult{}
		}
		mutation := buildAdministrativeRecoveryMutation(ObjectTypeMonitoringInstance, monitoringInstanceID, rowVersion, previous, now, administrativeRecoverySummaryForMonitoringInstance(record))
		return s.applyEvaluationAttempt(ctx, mutation, nil, false)
	}
	previousByClass := incidentsByClass(previous)
	timing := s.incidentTimingFor(ctx)
	evaluations := []classEvaluation{{
		class:  IncidentMonitoringInstanceHeartbeatMissing,
		result: EvaluateMonitoringInstanceHeartbeatMissing(previousByClass[IncidentMonitoringInstanceHeartbeatMissing], monitoringInstanceID, now, record.LastHeartbeatAt, timing.heartbeatInterval, timing.staleThresholdIntervals),
	}}
	mutation := buildMutation(ObjectTypeMonitoringInstance, monitoringInstanceID, rowVersion, previous, evaluations)
	return s.applyEvaluationAttempt(ctx, mutation, evaluations, true)
}

func (s *Service) evaluateMonitoringInstanceAttempt(ctx context.Context, monitoringInstanceID string, now time.Time) evaluationAttemptResult {
	rowVersion, err := s.objectRowVersion(ctx, ObjectTypeMonitoringInstance, monitoringInstanceID)
	if err != nil {
		return evaluationAttemptResult{err: err}
	}
	record, err := s.monitoringInstances.GetMonitoringInstance(ctx, monitoringInstanceID)
	if err != nil {
		if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			return evaluationAttemptResult{err: ErrIncidentProjectionObjectNotFound}
		}
		return evaluationAttemptResult{err: fmt.Errorf("get monitoring instance %q for incident evaluation: %w", monitoringInstanceID, err)}
	}
	previous, err := s.snapshots.ListActiveIncidents(ctx, ObjectTypeMonitoringInstance, monitoringInstanceID)
	if err != nil {
		return evaluationAttemptResult{err: fmt.Errorf("list previous monitoring instance incidents for %q: %w", monitoringInstanceID, err)}
	}
	if isInactiveMonitoringInstance(record) {
		if len(previous) == 0 {
			return evaluationAttemptResult{}
		}
		mutation := buildAdministrativeRecoveryMutation(ObjectTypeMonitoringInstance, monitoringInstanceID, rowVersion, previous, now, administrativeRecoverySummaryForMonitoringInstance(record))
		return s.applyEvaluationAttempt(ctx, mutation, nil, false)
	}
	previousByClass := incidentsByClass(previous)
	heartbeatInterval := s.heartbeatIntervalFor(ctx)
	hostSamples, err := s.snapshots.ListRecentHostSamples(ctx, monitoringInstanceID, now.Add(-30*time.Minute))
	if err != nil {
		return evaluationAttemptResult{err: fmt.Errorf("list recent host samples for %q: %w", monitoringInstanceID, err)}
	}
	thresholds := s.metricThresholdsFor(ctx)
	trendHostSamples, err := s.snapshots.ListRecentHostSamples(ctx, monitoringInstanceID, now.Add(-24*time.Hour))
	if err != nil {
		return evaluationAttemptResult{err: fmt.Errorf("list trend host samples for %q: %w", monitoringInstanceID, err)}
	}
	baselineStart, baselineEnd := trendBaselineWindow(now)
	monitoringInstanceBaselines, err := s.snapshots.ListMonitoringInstanceHostDailyAggregates(ctx, monitoringInstanceID, baselineStart, baselineEnd)
	if err != nil {
		return evaluationAttemptResult{err: fmt.Errorf("list monitoring instance trend baselines for %q: %w", monitoringInstanceID, err)}
	}

	evaluations := []classEvaluation{{
		class:  IncidentMonitoringInstanceHeartbeatMissing,
		result: EvaluateMonitoringInstanceHeartbeatMissing(previousByClass[IncidentMonitoringInstanceHeartbeatMissing], monitoringInstanceID, now, record.LastHeartbeatAt, heartbeatInterval),
	}}
	if len(hostSamples) > 0 {
		latest := &hostSamples[0]
		resourceSamples := monitoringInstanceResourceSamplesFromHostSamples(hostSamples)
		evaluations = append(evaluations,
			classEvaluation{class: IncidentMonitoringInstanceDiskPressure, result: EvaluateMonitoringInstanceDiskPressure(previousByClass[IncidentMonitoringInstanceDiskPressure], monitoringInstanceID, latest, thresholds)},
			classEvaluation{class: IncidentMonitoringInstanceInodePressure, result: EvaluateMonitoringInstanceInodePressure(previousByClass[IncidentMonitoringInstanceInodePressure], monitoringInstanceID, latest, thresholds)},
			classEvaluation{class: IncidentMonitoringInstanceResourcePressure, result: EvaluateMonitoringInstanceResourcePressure(previousByClass[IncidentMonitoringInstanceResourcePressure], monitoringInstanceID, resourceSamples, thresholds)},
		)
	}
	evaluations = append(evaluations, classEvaluation{
		class:  IncidentMonitoringInstanceTrendDegradation,
		result: EvaluateMonitoringInstanceTrendDegradation(previousByClass[IncidentMonitoringInstanceTrendDegradation], monitoringInstanceID, monitoringInstanceResourceSamplesFromHostSamples(trendHostSamples), monitoringInstanceBaselines),
	})

	mutation := buildMutation(ObjectTypeMonitoringInstance, monitoringInstanceID, rowVersion, previous, evaluations)
	return s.applyEvaluationAttempt(ctx, mutation, evaluations, true)
}

func (s *Service) evaluateTarget(ctx context.Context, targetID string, now time.Time) error {
	return s.evaluateObjectWithRetry(ctx, ObjectTypeTarget, targetID, now, func(attemptNow time.Time) evaluationAttemptResult {
		return s.evaluateTargetAttempt(ctx, targetID, attemptNow)
	})
}

func (s *Service) evaluateTargetAttempt(ctx context.Context, targetID string, now time.Time) evaluationAttemptResult {
	rowVersion, err := s.objectRowVersion(ctx, ObjectTypeTarget, targetID)
	if err != nil {
		return evaluationAttemptResult{err: err}
	}
	target, err := s.targets.GetTarget(ctx, targetID)
	if err != nil {
		if errors.Is(err, targets.ErrTargetNotFound) {
			return evaluationAttemptResult{err: ErrIncidentProjectionObjectNotFound}
		}
		return evaluationAttemptResult{err: fmt.Errorf("get target %q for incident evaluation: %w", targetID, err)}
	}
	previous, err := s.snapshots.ListActiveIncidents(ctx, ObjectTypeTarget, targetID)
	if err != nil {
		return evaluationAttemptResult{err: fmt.Errorf("list previous target incidents for %q: %w", targetID, err)}
	}
	if shouldRecoverIncidentsForTarget(target) {
		if len(previous) == 0 {
			return evaluationAttemptResult{}
		}
		mutation := buildAdministrativeRecoveryMutation(ObjectTypeTarget, targetID, rowVersion, previous, now, administrativeRecoverySummaryForTarget(target))
		return s.applyEvaluationAttempt(ctx, mutation, nil, false)
	}
	previousByClass := incidentsByClass(previous)
	observations, err := s.snapshots.ListRecentProbeObservations(ctx, targetID, now.Add(-6*time.Hour))
	if err != nil {
		return evaluationAttemptResult{err: fmt.Errorf("list recent probe observations for %q: %w", targetID, err)}
	}
	trendObservations, err := s.snapshots.ListRecentProbeObservations(ctx, targetID, now.Add(-24*time.Hour))
	if err != nil {
		return evaluationAttemptResult{err: fmt.Errorf("list trend probe observations for %q: %w", targetID, err)}
	}
	baselineStart, baselineEnd := trendBaselineWindow(now)
	targetBaselines, err := s.snapshots.ListTargetProbeDailyAggregates(ctx, targetID, baselineStart, baselineEnd)
	if err != nil {
		return evaluationAttemptResult{err: fmt.Errorf("list target trend baselines for %q: %w", targetID, err)}
	}

	evaluations := []classEvaluation{
		{class: IncidentTargetProbeFailure, result: evaluateTargetProbeFailureAcrossSeries(previousByClass[IncidentTargetProbeFailure], targetID, observations)},
		{class: IncidentTargetTLSExpiry, result: evaluateTargetTLSExpiryAcrossSeries(previousByClass[IncidentTargetTLSExpiry], targetID, observations)},
	}
	evaluations = append(evaluations, classEvaluation{
		class:  IncidentTargetLatencyTrendDegradation,
		result: EvaluateTargetLatencyTrendDegradationAcrossSeries(previousByClass[IncidentTargetLatencyTrendDegradation], targetID, trendObservations, targetBaselines),
	})
	mutation := buildMutation(ObjectTypeTarget, targetID, rowVersion, previous, evaluations)
	return s.applyEvaluationAttempt(ctx, mutation, evaluations, true)
}

type evaluationAttemptResult struct {
	evaluations        []classEvaluation
	appendNotification bool
	conflict           bool
	err                error
}

func (s *Service) evaluateObjectWithRetry(ctx context.Context, objectType ObjectType, objectID string, triggerNow time.Time, attempt func(time.Time) evaluationAttemptResult) error {
	attemptNow := triggerNow
	for attemptNumber := 0; attemptNumber < 2; attemptNumber++ {
		result := attempt(attemptNow)
		if result.err == nil {
			if !result.appendNotification {
				return nil
			}
			return s.appendNotificationRecords(ctx, objectType, objectID, result.evaluations)
		}
		if errors.Is(result.err, ErrIncidentProjectionObjectNotFound) {
			return nil
		}
		if !result.conflict {
			return result.err
		}
		if attemptNumber == 0 {
			freshNow := s.now()
			if freshNow.After(triggerNow) {
				attemptNow = freshNow
			} else {
				attemptNow = triggerNow
			}
			continue
		}
		s.warnProjectionConflictRetryExhausted(objectType)
		return nil
	}
	return nil
}

func (s *Service) objectRowVersion(ctx context.Context, objectType ObjectType, objectID string) (string, error) {
	rowVersion, err := s.snapshots.GetObjectRowVersion(ctx, objectType, objectID)
	if err != nil {
		return "", fmt.Errorf("get %s incident projection row version: %w", objectType, err)
	}
	if rowVersion == "" {
		return "", fmt.Errorf("get %s incident projection row version: empty row version", objectType)
	}
	return rowVersion, nil
}

func (s *Service) applyEvaluationAttempt(ctx context.Context, mutation IncidentMutation, evaluations []classEvaluation, appendNotification bool) evaluationAttemptResult {
	err := s.writer.ApplyIncidentMutation(ctx, mutation)
	return evaluationAttemptResult{
		evaluations:        evaluations,
		appendNotification: appendNotification,
		conflict:           err != nil && errors.Is(err, ErrIncidentProjectionConflict),
		err:                err,
	}
}

func buildAdministrativeRecoveryMutation(objectType ObjectType, objectID, expectedObjectRowVersion string, previous []IncidentRecord, now time.Time, summary string) IncidentMutation {
	events := make([]StateChangeEventRecord, 0, len(previous))
	for _, incident := range previous {
		events = append(events, StateChangeEventRecord{
			IncidentID:          incident.IncidentID,
			IncidentClass:       incident.IncidentClass,
			ObjectType:          objectType,
			ObjectID:            objectID,
			EventType:           EventIncidentRecovered,
			Severity:            incident.Severity,
			Summary:             summary,
			CreatedAt:           canonicalMonitoringEventTime(now),
			IsBackfilled:        false,
			Provenance:          MonitoringEventProvenanceCenter,
			ProducerVersion:     MonitoringEventProducerVersion,
			RuleVersion:         MonitoringEventIncidentRuleVersion,
			PriorState:          monitoringEventIncidentState(incident.Severity),
			ResultingState:      "normal",
			CorrectionOfEventID: "",
		})
	}
	return IncidentMutation{
		ObjectType:               objectType,
		ObjectID:                 objectID,
		ExpectedObjectRowVersion: expectedObjectRowVersion,
		Active:                   []IncidentRecord{},
		Events:                   events,
	}
}

func (s *Service) warnProjectionConflictRetryExhausted(objectType ObjectType) {
	now := s.now()
	s.projectionConflictWarnMu.Lock()
	last := s.projectionConflictWarnAt[objectType]
	if !last.IsZero() && now.Before(last.Add(time.Minute)) {
		s.projectionConflictWarnMu.Unlock()
		return
	}
	s.projectionConflictWarnAt[objectType] = now
	s.projectionConflictWarnMu.Unlock()
	s.logger.Error(
		"incident projection conflict retry exhausted",
		"error", ErrIncidentProjectionConflict,
		"object_type", objectType,
		"classification", "concurrent_update",
	)
}

func administrativeRecoverySummaryForMonitoringInstance(record monitoringinstances.Record) string {
	if record.ArchivedAt != nil {
		return "监控实例已归档，当前异常按行政下线收敛"
	}
	if record.LifecycleStatus == monitoringinstances.LifecycleRetired {
		return "监控实例已退役，当前异常按行政下线收敛"
	}
	if record.MonitoringStatus == monitoringinstances.MonitoringMaintenance {
		return "监控实例处于维护中，当前异常按维护状态收敛"
	}
	if record.MonitoringStatus == monitoringinstances.MonitoringPaused {
		return "监控实例已暂停，当前异常按暂停状态收敛"
	}
	return "监控实例非运行态，当前异常按行政状态收敛"
}

func administrativeRecoverySummaryForTarget(record targets.TargetRecord) string {
	if record.RunStatus == targets.RunStatusArchived {
		return "Target 已归档，当前异常按行政下线收敛"
	}
	if record.RunStatus == targets.RunStatusPaused {
		return "Target 已暂停，当前异常按暂停状态收敛"
	}
	return "Target 非运行态，当前异常按行政状态收敛"
}

type incidentTiming struct {
	heartbeatInterval       time.Duration
	sweepInterval           time.Duration
	staleThresholdIntervals int
}

type notificationPolicy struct {
	notifyOnStarted   bool
	notifyOnEscalated bool
	notifyOnRecovered bool
}

func defaultNotificationPolicy() notificationPolicy {
	return notificationPolicyFromDefaults(centersettings.Default().IncidentDefaults)
}

// MetricThresholdsFromDefaults builds MetricThresholds from settings.IncidentDefaults.
// Zero or negative values fall back to the original hardcoded defaults.
func MetricThresholdsFromDefaults(defaults centersettings.IncidentDefaults) MetricThresholds {
	t := DefaultMetricThresholds()
	if defaults.CPUWarningPct > 0 {
		t.CPUWarningPct = defaults.CPUWarningPct
	}
	if defaults.CPUAlertPct > 0 {
		t.CPUAlertPct = defaults.CPUAlertPct
	}
	if defaults.CPUCriticalPct > 0 {
		t.CPUCriticalPct = defaults.CPUCriticalPct
	}
	if defaults.MemWarningPct > 0 {
		t.MemWarningPct = defaults.MemWarningPct
	}
	if defaults.MemAlertPct > 0 {
		t.MemAlertPct = defaults.MemAlertPct
	}
	if defaults.MemCriticalPct > 0 {
		t.MemCriticalPct = defaults.MemCriticalPct
	}
	if defaults.DiskWarningPct > 0 {
		t.DiskWarningPct = defaults.DiskWarningPct
	}
	if defaults.DiskAlertPct > 0 {
		t.DiskAlertPct = defaults.DiskAlertPct
	}
	if defaults.DiskCriticalPct > 0 {
		t.DiskCriticalPct = defaults.DiskCriticalPct
	}
	if defaults.InodeWarningPct > 0 {
		t.InodeWarningPct = defaults.InodeWarningPct
	}
	if defaults.InodeAlertPct > 0 {
		t.InodeAlertPct = defaults.InodeAlertPct
	}
	if defaults.InodeCriticalPct > 0 {
		t.InodeCriticalPct = defaults.InodeCriticalPct
	}
	if defaults.IOWaitWarningPct > 0 {
		t.IOWaitWarningPct = defaults.IOWaitWarningPct
	}
	if defaults.IOWaitCriticalPct > 0 {
		t.IOWaitCriticalPct = defaults.IOWaitCriticalPct
	}
	t.IOWaitAlertPct = midpointInt(t.IOWaitWarningPct, t.IOWaitCriticalPct)
	if defaults.Load5Warning > 0 {
		t.Load5Warning = defaults.Load5Warning
	}
	if defaults.Load5Critical > 0 {
		t.Load5Critical = defaults.Load5Critical
	}
	t.Load5Alert = midpointFloat(t.Load5Warning, t.Load5Critical)
	return t
}

func midpointInt(low, high int) int {
	if high <= low {
		return low
	}
	return low + (high-low)/2
}

func midpointFloat(low, high float64) float64 {
	if high <= low {
		return low
	}
	return low + (high-low)/2
}

func notificationPolicyFromDefaults(defaults centersettings.IncidentDefaults) notificationPolicy {
	return notificationPolicy{
		notifyOnStarted:   defaults.NotifyOnStarted,
		notifyOnEscalated: defaults.NotifyOnEscalated,
		notifyOnRecovered: defaults.NotifyOnRecovered,
	}
}

func (p notificationPolicy) enabled(reason NotificationReason) bool {
	switch reason {
	case NotificationReasonStarted:
		return p.notifyOnStarted
	case NotificationReasonEscalated:
		return p.notifyOnEscalated
	case NotificationReasonRecovered:
		return p.notifyOnRecovered
	default:
		return true
	}
}

func (s *Service) heartbeatIntervalFor(ctx context.Context) time.Duration {
	return s.incidentTimingFor(ctx).heartbeatInterval
}

func (s *Service) sweepIntervalFor(ctx context.Context) time.Duration {
	return s.incidentTimingFor(ctx).sweepInterval
}

func (s *Service) incidentTimingFor(ctx context.Context) incidentTiming {
	timing := incidentTiming{
		heartbeatInterval:       s.fallbackHeartbeatInterval,
		sweepInterval:           s.fallbackSweepInterval,
		staleThresholdIntervals: 3,
	}
	if s.settingsRepo == nil {
		return timing
	}
	if source, ok := s.settingsRepo.(persistedIncidentDefaultsSource); ok {
		defaults, exists, err := source.GetPersistedIncidentDefaults(ctx)
		if err != nil || !exists {
			return timing
		}
		return applyIncidentDefaults(timing, defaults)
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return timing
	}
	return applyIncidentDefaults(timing, settings.IncidentDefaults)
}

func applyIncidentDefaults(timing incidentTiming, defaults centersettings.IncidentDefaults) incidentTiming {
	if defaults.HeartbeatIntervalSeconds > 0 {
		timing.heartbeatInterval = time.Duration(defaults.HeartbeatIntervalSeconds) * time.Second
	}
	if defaults.SweepIntervalSeconds > 0 {
		timing.sweepInterval = time.Duration(defaults.SweepIntervalSeconds) * time.Second
	}
	if defaults.StaleThresholdIntervals > 0 {
		timing.staleThresholdIntervals = defaults.StaleThresholdIntervals
	}
	return timing
}

func (s *Service) metricThresholdsFor(ctx context.Context) MetricThresholds {
	if s.settingsRepo == nil {
		return DefaultMetricThresholds()
	}
	if source, ok := s.settingsRepo.(persistedIncidentDefaultsSource); ok {
		defaults, exists, err := source.GetPersistedIncidentDefaults(ctx)
		if err == nil && exists {
			return MetricThresholdsFromDefaults(defaults)
		}
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return DefaultMetricThresholds()
	}
	return MetricThresholdsFromDefaults(settings.IncidentDefaults)
}

func (s *Service) notificationPolicyFor(ctx context.Context) notificationPolicy {
	policy := defaultNotificationPolicy()
	if s.settingsRepo == nil {
		return policy
	}
	if source, ok := s.settingsRepo.(persistedIncidentDefaultsSource); ok {
		defaults, exists, err := source.GetPersistedIncidentDefaults(ctx)
		if err == nil && exists {
			return notificationPolicyFromDefaults(defaults)
		}
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return policy
	}
	return notificationPolicyFromDefaults(settings.IncidentDefaults)
}

type classEvaluation struct {
	class  IncidentClass
	result EvaluationResult
}

func buildMutation(objectType ObjectType, objectID, expectedObjectRowVersion string, previous []IncidentRecord, evaluations []classEvaluation) IncidentMutation {
	activeByClass := incidentsByClass(previous)
	mutation := IncidentMutation{
		ObjectType:               objectType,
		ObjectID:                 objectID,
		ExpectedObjectRowVersion: expectedObjectRowVersion,
		Active:                   make([]IncidentRecord, 0),
		Events:                   make([]StateChangeEventRecord, 0),
	}

	for _, evaluation := range evaluations {
		switch evaluation.result.Transition {
		case TransitionRecovered:
			delete(activeByClass, evaluation.class)
		case TransitionSkipped:
		default:
			if evaluation.result.Current != nil {
				activeByClass[evaluation.class] = evaluation.result.Current
			}
		}
		if evaluation.result.Event != nil {
			mutation.Events = append(mutation.Events, *evaluation.result.Event)
		}
	}

	for _, incident := range activeByClass {
		mutation.Active = append(mutation.Active, *incident)
	}
	sort.Slice(mutation.Active, func(i, j int) bool {
		return mutation.Active[i].IncidentClass < mutation.Active[j].IncidentClass
	})
	return mutation
}

func (s *Service) appendNotificationRecords(ctx context.Context, objectType ObjectType, objectID string, evaluations []classEvaluation) error {
	records := make([]NotificationRecordWrite, 0)
	policy := s.notificationPolicyFor(ctx)
	for _, evaluation := range evaluations {
		if evaluation.result.Notification == nil {
			continue
		}
		decision := evaluation.result.Notification
		base := notificationRecordBase(evaluation, objectType, objectID, decision)
		shouldSend := decision.ShouldSend && policy.enabled(decision.Reason)
		if shouldSend && s.dispatcher != nil {
			deliveries := s.dispatcher.Dispatch(ctx, decision.Summary)
			records = append(records, s.notificationRecordsFromDeliveries(base, deliveries)...)
			continue
		}
		records = append(records, s.suppressedNotificationRecords(ctx, base)...)
	}
	if len(records) == 0 {
		return nil
	}
	return s.writer.AppendNotificationRecords(ctx, records)
}

func notificationRecordBase(evaluation classEvaluation, objectType ObjectType, objectID string, decision *NotificationDecision) NotificationRecordWrite {
	channel := decision.Channel
	if channel == "" {
		channel = NotificationChannelTelegram
	}
	return NotificationRecordWrite{
		IncidentID:     incidentIdentity(evaluation),
		ObjectType:     objectType,
		ObjectID:       objectID,
		Channel:        channel,
		DeliveryStatus: DeliveryStatusSuppressed,
		Summary:        decision.Summary,
	}
}

func (s *Service) notificationRecordsFromDeliveries(base NotificationRecordWrite, deliveries []NotificationDelivery) []NotificationRecordWrite {
	if len(deliveries) == 0 {
		return []NotificationRecordWrite{base}
	}
	records := make([]NotificationRecordWrite, 0, len(deliveries))
	for _, delivery := range deliveries {
		record := base
		if delivery.Channel != "" {
			record.Channel = delivery.Channel
		}
		record.DeliveryStatus = delivery.Status
		if record.DeliveryStatus == "" {
			record.DeliveryStatus = DeliveryStatusFailed
		}
		if record.DeliveryStatus == DeliveryStatusSent {
			now := s.now()
			record.SentAt = &now
		}
		if record.DeliveryStatus == DeliveryStatusFailed && delivery.Error != nil {
			s.logger.Error("send incident notification failed", "object_type", base.ObjectType, "object_id", base.ObjectID, "channel", record.Channel, "error", delivery.Error)
		}
		records = append(records, record)
	}
	return records
}

func (s *Service) suppressedNotificationRecords(ctx context.Context, base NotificationRecordWrite) []NotificationRecordWrite {
	channels := []NotificationChannel(nil)
	if resolver, ok := s.dispatcher.(NotificationChannelResolver); ok {
		channels = resolver.NotificationChannels(ctx)
	}
	if len(channels) == 0 {
		channels = []NotificationChannel{base.Channel}
	}
	records := make([]NotificationRecordWrite, 0, len(channels))
	for _, channel := range uniqueNotificationChannels(channels) {
		record := base
		record.Channel = channel
		record.DeliveryStatus = DeliveryStatusSuppressed
		records = append(records, record)
	}
	return records
}

func uniqueNotificationChannels(channels []NotificationChannel) []NotificationChannel {
	seen := map[NotificationChannel]struct{}{}
	out := make([]NotificationChannel, 0, len(channels))
	for _, channel := range channels {
		if channel == "" {
			continue
		}
		if _, ok := seen[channel]; ok {
			continue
		}
		seen[channel] = struct{}{}
		out = append(out, channel)
	}
	return out
}

func incidentIdentity(evaluation classEvaluation) string {
	if evaluation.result.Current != nil {
		return evaluation.result.Current.IncidentID
	}
	if evaluation.result.Event != nil {
		return evaluation.result.Event.IncidentID
	}
	return ""
}

func incidentsByClass(records []IncidentRecord) map[IncidentClass]*IncidentRecord {
	out := make(map[IncidentClass]*IncidentRecord, len(records))
	for i := range records {
		out[records[i].IncidentClass] = &records[i]
	}
	return out
}

func monitoringInstanceResourceSamplesFromHostSamples(hostSamples []runtimefacts.HostSample) []MonitoringInstanceResourceSample {
	resourceSamples := make([]MonitoringInstanceResourceSample, 0, len(hostSamples))
	for _, sample := range hostSamples {
		resourceSamples = append(resourceSamples, MonitoringInstanceResourceSample{
			ObservedAt:         sample.ObservedAt,
			ReceivedAt:         sample.ReceivedAt,
			CPUUsagePct:        sample.CPUUsagePct,
			NormalizedLoad5:    sample.Load5,
			MemUsedPct:         sample.MemUsedPct,
			MemAvailableBytes:  sample.MemAvailableBytes,
			SwapUsedPct:        sample.SwapUsedPct,
			CPUIOWaitPct:       sample.CPUIOWaitPct,
			CPUStealPct:        sample.CPUStealPct,
			MaintenanceContext: sample.MaintenanceContext,
			IsBackfilled:       sample.IsBackfilled,
		})
	}
	return resourceSamples
}

func trendBaselineWindow(now time.Time) (time.Time, time.Time) {
	utc := now.UTC()
	end := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	return end.AddDate(0, 0, -7), end
}

func uniqueTargetIDs(items []observations.ProbeObservationWrite) []string {
	seen := map[string]struct{}{}
	targetIDs := make([]string, 0)
	for _, item := range items {
		if item.TargetID == "" {
			continue
		}
		if _, ok := seen[item.TargetID]; ok {
			continue
		}
		seen[item.TargetID] = struct{}{}
		targetIDs = append(targetIDs, item.TargetID)
	}
	sort.Strings(targetIDs)
	return targetIDs
}

func evaluateTargetProbeFailureAcrossSeries(previous *IncidentRecord, targetID string, observations []runtimefacts.ProbeObservation) EvaluationResult {
	grouped := groupProbeSeries(observations)
	activeResults := make([]EvaluationResult, 0)
	recoveryEligibleCount := 0
	recoverySuppressed := false
	recoveryBackfilled := false
	latestObservedAt := time.Time{}
	tcpMonitoringInstances := map[string]struct{}{}
	httpProbeItems := map[string]struct{}{}

	for _, series := range grouped {
		result := EvaluateTargetProbeFailure(nil, targetID, series)
		if len(series) > 0 && series[0].ObservedAt.After(latestObservedAt) {
			latestObservedAt = series[0].ObservedAt
		}
		if result.Current != nil {
			activeResults = append(activeResults, result)
			if series[0].ProbeKind == agentapi.ProbeKindTCP && series[0].MonitoringInstanceID != "" {
				tcpMonitoringInstances[series[0].MonitoringInstanceID] = struct{}{}
			}
			if series[0].ProbeKind == agentapi.ProbeKindHTTP && series[0].ProbeItemID != "" {
				httpProbeItems[series[0].ProbeItemID] = struct{}{}
			}
		}
		if consecutiveResults(series, agentapi.ProbeResultSuccess) >= 2 {
			recoveryEligibleCount++
			recoveryBackfilled = recoveryBackfilled || series[0].IsBackfilled
			if observationSeriesSuppressed(series) {
				recoverySuppressed = true
			}
		}
	}

	if len(activeResults) == 0 {
		if previous != nil && recoveryEligibleCount == len(grouped) && len(grouped) > 0 {
			result := recoverIfNeeded(previous, latestObservedAt, "探针已连续成功恢复", MonitoringEventProvenanceAgentSync, recoveryBackfilled)
			if recoverySuppressed {
				return suppressNotification(result)
			}
			return result
		}
		return noop(previous)
	}

	severity := activeResults[0].Current.Severity
	summary := activeResults[0].Current.SourceSummary
	for _, result := range activeResults[1:] {
		if severityRank(result.Current.Severity) > severityRank(severity) {
			severity = result.Current.Severity
			summary = result.Current.SourceSummary
		}
	}
	if len(tcpMonitoringInstances) >= 2 {
		severity = SeverityCritical
		summary = "TCP 探针在多个执行监控实例上持续失败"
	}
	if len(httpProbeItems) >= 2 {
		severity = SeverityCritical
		summary = "HTTP 多个 ProbeItem 同时异常"
	}
	return evaluateTransition(previous, ObjectTypeTarget, targetID, IncidentTargetProbeFailure, severity, latestObservedAt, summary, MonitoringEventProvenanceAgentSync)
}

func evaluateTargetTLSExpiryAcrossSeries(previous *IncidentRecord, targetID string, observations []runtimefacts.ProbeObservation) EvaluationResult {
	tlsObservations := make([]runtimefacts.ProbeObservation, 0)
	for _, observation := range observations {
		if observation.ProbeKind == agentapi.ProbeKindTLS {
			tlsObservations = append(tlsObservations, observation)
		}
	}
	if len(tlsObservations) == 0 {
		return noop(previous)
	}
	grouped := groupProbeSeries(tlsObservations)
	activeResults := make([]EvaluationResult, 0)
	recoveryEligibleCount := 0
	recoverySuppressed := false
	recoveryBackfilled := false
	latestObservedAt := time.Time{}

	for _, series := range grouped {
		result := EvaluateTargetTLSExpiry(nil, targetID, series)
		if len(series) > 0 && series[0].ObservedAt.After(latestObservedAt) {
			latestObservedAt = series[0].ObservedAt
		}
		if result.Current != nil {
			activeResults = append(activeResults, result)
		}
		if len(series) > 0 && series[0].TLSExpiryDays != nil && *series[0].TLSExpiryDays > 30 {
			recoveryEligibleCount++
			recoveryBackfilled = recoveryBackfilled || series[0].IsBackfilled
			if observationSeriesSuppressed(series) {
				recoverySuppressed = true
			}
		}
	}

	if len(activeResults) == 0 {
		if previous != nil && recoveryEligibleCount == len(grouped) && len(grouped) > 0 {
			result := recoverIfNeeded(previous, latestObservedAt, "TLS 到期风险解除", MonitoringEventProvenanceAgentSync, recoveryBackfilled)
			if recoverySuppressed {
				return suppressNotification(result)
			}
			return result
		}
		return noop(previous)
	}

	severity := activeResults[0].Current.Severity
	summary := activeResults[0].Current.SourceSummary
	for _, result := range activeResults[1:] {
		if severityRank(result.Current.Severity) > severityRank(severity) {
			severity = result.Current.Severity
			summary = result.Current.SourceSummary
		}
	}
	return evaluateTransition(previous, ObjectTypeTarget, targetID, IncidentTargetTLSExpiry, severity, latestObservedAt, summary, MonitoringEventProvenanceAgentSync)
}

func groupProbeSeries(observations []runtimefacts.ProbeObservation) [][]runtimefacts.ProbeObservation {
	grouped := map[string][]runtimefacts.ProbeObservation{}
	keys := make([]string, 0)
	for _, observation := range normalizeProbeObservations(observations) {
		key := observation.ProbeItemID + "|" + observation.MonitoringInstanceID
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], observation)
	}
	sort.Strings(keys)
	out := make([][]runtimefacts.ProbeObservation, 0, len(keys))
	for _, key := range keys {
		out = append(out, normalizeProbeObservations(grouped[key]))
	}
	return out
}

func observationSeriesSuppressed(series []runtimefacts.ProbeObservation) bool {
	if len(series) == 0 {
		return false
	}
	return series[0].MaintenanceContext || series[0].IsBackfilled
}

type PostgresSnapshotReader struct {
	db incidentSnapshotDB
}

type incidentSnapshotDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

const monitoringInstanceRowVersionSQL = `select xmin::text from monitoring_instances where monitoring_instance_id = $1`
const targetRowVersionSQL = `select xmin::text from targets where target_id = $1`

const incidentRecentHostSamplesSQL = `
	select
		monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
		cpu_usage_pct, load_1, load_5, load_15, mem_used_pct, mem_available_bytes, mem_total_bytes,
		swap_used_pct, disk_used_pct, disk_total_bytes, inode_used_pct, net_in_bytes_per_sec,
		net_out_bytes_per_sec, cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec,
		disk_write_bytes_per_sec, disk_busy_pct, uptime_seconds,
		maintenance_context, is_backfilled, sync_batch_id
	from host_samples
	where monitoring_instance_id = $1 and observed_at >= $2
	order by observed_at desc, is_backfilled asc, received_at desc, id desc`

const incidentRecentProbeObservationsSQL = `
	select
		po.monitoring_instance_id, po.target_id, po.probe_item_id, pi.probe_kind,
		po.observed_at, po.received_at, po.agent_version, po.fingerprint,
		po.result_kind, po.latency_ms, po.http_status, po.tls_expiry_days,
		coalesce(po.error_code, ''), coalesce(po.error_summary, ''),
		po.maintenance_context, po.is_backfilled, po.sync_batch_id
	from probe_observations po
	join probe_items pi on pi.probe_item_id = po.probe_item_id
	where po.target_id = $1 and po.observed_at >= $2
	order by po.observed_at desc, po.is_backfilled asc, po.received_at desc, po.id desc`

func NewPostgresSnapshotReader(db *pgxpool.Pool) *PostgresSnapshotReader {
	return &PostgresSnapshotReader{db: db}
}

func (r *PostgresSnapshotReader) GetObjectRowVersion(ctx context.Context, objectType ObjectType, objectID string) (string, error) {
	if strings.TrimSpace(objectID) == "" {
		return "", errors.New("incident object id is required for row-version read")
	}
	var query string
	switch objectType {
	case ObjectTypeMonitoringInstance:
		query = monitoringInstanceRowVersionSQL
	case ObjectTypeTarget:
		query = targetRowVersionSQL
	default:
		return "", fmt.Errorf("incident object type %q does not support row-version reads", objectType)
	}

	var rowVersion string
	if err := r.db.QueryRow(ctx, query, objectID).Scan(&rowVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("incident object row version not found: %w: %w", ErrIncidentProjectionObjectNotFound, err)
		}
		return "", fmt.Errorf("read incident object row version: %w", err)
	}
	if rowVersion == "" {
		return "", errors.New("incident object row version is empty")
	}
	return rowVersion, nil
}

func (r *PostgresSnapshotReader) ListActiveIncidents(ctx context.Context, objectType ObjectType, objectID string) ([]IncidentRecord, error) {
	rows, err := r.db.Query(ctx, `
		select incident_id, object_type, object_id, incident_class, severity, started_at, last_evaluated_at, status, source_summary
		from active_incidents
		where object_type = $1 and object_id = $2
		order by incident_class`, string(objectType), objectID)
	if err != nil {
		return nil, fmt.Errorf("query active incidents for %s %q: %w", objectType, objectID, err)
	}
	defer rows.Close()

	records := make([]IncidentRecord, 0)
	for rows.Next() {
		var record IncidentRecord
		var objectTypeValue, incidentClass, severity string
		if err := rows.Scan(&record.IncidentID, &objectTypeValue, &record.ObjectID, &incidentClass, &severity, &record.StartedAt, &record.LastEvaluatedAt, &record.Status, &record.SourceSummary); err != nil {
			return nil, fmt.Errorf("scan active incident: %w", err)
		}
		record.ObjectType = ObjectType(objectTypeValue)
		record.IncidentClass = IncidentClass(incidentClass)
		record.Severity = Severity(severity)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active incidents: %w", err)
	}
	return records, nil
}

func (r *PostgresSnapshotReader) ListRecentHostSamples(ctx context.Context, monitoringInstanceID string, since time.Time) ([]runtimefacts.HostSample, error) {
	rows, err := r.db.Query(ctx, incidentRecentHostSamplesSQL, monitoringInstanceID, since)
	if err != nil {
		return nil, fmt.Errorf("query host samples for %q: %w", monitoringInstanceID, err)
	}
	defer rows.Close()
	out := make([]runtimefacts.HostSample, 0)
	for rows.Next() {
		var sample runtimefacts.HostSample
		if err := rows.Scan(
			&sample.MonitoringInstanceID, &sample.ObservedAt, &sample.ReceivedAt, &sample.AgentVersion, &sample.Fingerprint,
			&sample.CPUUsagePct, &sample.Load1, &sample.Load5, &sample.Load15, &sample.MemUsedPct, &sample.MemAvailableBytes, &sample.MemTotalBytes,
			&sample.SwapUsedPct, &sample.DiskUsedPct, &sample.DiskTotalBytes, &sample.InodeUsedPct, &sample.NetInBytesPerSec,
			&sample.NetOutBytesPerSec, &sample.CPUIOWaitPct, &sample.CPUStealPct, &sample.DiskReadBytesPerSec,
			&sample.DiskWriteBytesPerSec, &sample.DiskBusyPct, &sample.UptimeSeconds,
			&sample.MaintenanceContext, &sample.IsBackfilled, &sample.SyncBatchID,
		); err != nil {
			return nil, fmt.Errorf("scan host sample: %w", err)
		}
		out = append(out, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate host samples: %w", err)
	}
	return out, nil
}

func (r *PostgresSnapshotReader) ListRecentProbeObservations(ctx context.Context, targetID string, since time.Time) ([]runtimefacts.ProbeObservation, error) {
	rows, err := r.db.Query(ctx, incidentRecentProbeObservationsSQL, targetID, since)
	if err != nil {
		return nil, fmt.Errorf("query probe observations for %q: %w", targetID, err)
	}
	defer rows.Close()
	out := make([]runtimefacts.ProbeObservation, 0)
	for rows.Next() {
		var observation runtimefacts.ProbeObservation
		if err := rows.Scan(
			&observation.MonitoringInstanceID, &observation.TargetID, &observation.ProbeItemID, &observation.ProbeKind,
			&observation.ObservedAt, &observation.ReceivedAt, &observation.AgentVersion, &observation.Fingerprint,
			&observation.ResultKind, &observation.LatencyMS, &observation.HTTPStatus, &observation.TLSExpiryDays,
			&observation.ErrorCode, &observation.ErrorSummary,
			&observation.MaintenanceContext, &observation.IsBackfilled, &observation.SyncBatchID,
		); err != nil {
			return nil, fmt.Errorf("scan probe observation: %w", err)
		}
		out = append(out, observation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate probe observations: %w", err)
	}
	return out, nil
}

func (r *PostgresSnapshotReader) ListMonitoringInstanceHostDailyAggregates(ctx context.Context, monitoringInstanceID string, since, before time.Time) ([]MonitoringInstanceHostDailyAggregate, error) {
	rows, err := r.db.Query(ctx, `
		select
			bucket_date,
			sample_count,
			avg_load_5,
			avg_cpu_iowait_pct,
			avg_cpu_steal_pct,
			backfilled_sample_count,
			maintenance_sample_count
		from monitoring_instance_host_sample_daily_aggregates
		where monitoring_instance_id = $1
			and bucket_date >= $2::date
			and bucket_date < $3::date
		order by bucket_date desc`, monitoringInstanceID, since, before)
	if err != nil {
		return nil, fmt.Errorf("query monitoring instance host daily aggregates for %q: %w", monitoringInstanceID, err)
	}
	defer rows.Close()
	out := make([]MonitoringInstanceHostDailyAggregate, 0)
	for rows.Next() {
		var aggregate MonitoringInstanceHostDailyAggregate
		if err := rows.Scan(
			&aggregate.BucketDate,
			&aggregate.SampleCount,
			&aggregate.AvgLoad5,
			&aggregate.AvgCPUIOWaitPct,
			&aggregate.AvgCPUStealPct,
			&aggregate.BackfilledSampleCount,
			&aggregate.MaintenanceSampleCount,
		); err != nil {
			return nil, fmt.Errorf("scan monitoring instance host daily aggregate: %w", err)
		}
		out = append(out, aggregate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monitoring instance host daily aggregates: %w", err)
	}
	return out, nil
}

func (r *PostgresSnapshotReader) ListTargetProbeDailyAggregates(ctx context.Context, targetID string, since, before time.Time) ([]TargetProbeDailyAggregate, error) {
	rows, err := r.db.Query(ctx, `
		select
			target_id,
			probe_item_id,
			bucket_date,
			observation_count,
			success_count,
			avg_latency_ms,
			p95_latency_ms,
			backfilled_observation_count,
			maintenance_observation_count
		from target_probe_daily_aggregates
		where target_id = $1
			and bucket_date >= $2::date
			and bucket_date < $3::date
		order by bucket_date desc, probe_item_id`, targetID, since, before)
	if err != nil {
		return nil, fmt.Errorf("query target probe daily aggregates for %q: %w", targetID, err)
	}
	defer rows.Close()
	out := make([]TargetProbeDailyAggregate, 0)
	for rows.Next() {
		var aggregate TargetProbeDailyAggregate
		if err := rows.Scan(
			&aggregate.TargetID,
			&aggregate.ProbeItemID,
			&aggregate.BucketDate,
			&aggregate.ObservationCount,
			&aggregate.SuccessCount,
			&aggregate.AvgLatencyMS,
			&aggregate.P95LatencyMS,
			&aggregate.BackfilledObservationCount,
			&aggregate.MaintenanceObservationCount,
		); err != nil {
			return nil, fmt.Errorf("scan target probe daily aggregate: %w", err)
		}
		out = append(out, aggregate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate target probe daily aggregates: %w", err)
	}
	return out, nil
}

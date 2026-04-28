package incidents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/nodes"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/runtimefacts"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
)

const defaultHeartbeatInterval = 30 * time.Second

var errNotificationSuppressed = errors.New("incident notification suppressed")

type NodeRepository interface {
	GetNode(context.Context, string) (nodes.Record, error)
	ListNodes(context.Context) ([]nodes.Record, error)
}

type TargetRepository interface {
	ListTargets(context.Context) ([]targets.TargetRecord, error)
}

type SnapshotReader interface {
	ListActiveIncidents(context.Context, ObjectType, string) ([]IncidentRecord, error)
	ListRecentHostSamples(context.Context, string, time.Time) ([]runtimefacts.HostSample, error)
	ListRecentProbeObservations(context.Context, string, time.Time) ([]runtimefacts.ProbeObservation, error)
	ListNodeHostDailyAggregates(context.Context, string, time.Time, time.Time) ([]NodeHostDailyAggregate, error)
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

type SettingsAwareNotifierFactory func(botToken, chatID string) Notifier

type settingsAwareNotifier struct {
	settingsRepo SettingsRepository
	newNotifier  SettingsAwareNotifierFactory
	fallback     Notifier
}

func NewSettingsAwareNotifier(settingsRepo SettingsRepository, newNotifier SettingsAwareNotifierFactory, fallback Notifier) Notifier {
	if settingsRepo == nil {
		return fallback
	}
	return &settingsAwareNotifier{
		settingsRepo: settingsRepo,
		newNotifier:  newNotifier,
		fallback:     fallback,
	}
}

func (n *settingsAwareNotifier) Send(ctx context.Context, summary string) error {
	if source, ok := n.settingsRepo.(persistedTelegramSettingsSource); ok {
		telegram, exists, err := source.GetPersistedTelegramSettings(ctx)
		if err != nil {
			if n.fallback == nil {
				return fmt.Errorf("get persisted telegram settings: %w", err)
			}
			return n.fallback.Send(ctx, summary)
		}
		return n.sendWithTelegramSettings(ctx, summary, telegram, exists)
	}

	settings, err := n.settingsRepo.GetSettings(ctx)
	if err != nil {
		if n.fallback == nil {
			return fmt.Errorf("get persisted center settings: %w", err)
		}
		return n.fallback.Send(ctx, summary)
	}
	return n.sendWithTelegramSettings(ctx, summary, settings.Telegram, true)
}

func (n *settingsAwareNotifier) sendWithTelegramSettings(ctx context.Context, summary string, telegram centersettings.TelegramSettings, exists bool) error {
	if !exists || !telegram.RuntimeManaged {
		if n.fallback != nil {
			return n.fallback.Send(ctx, summary)
		}
		return errNotificationSuppressed
	}
	if !telegram.Enabled() {
		return errNotificationSuppressed
	}
	if n.newNotifier == nil {
		return errors.New("telegram notifier factory is nil")
	}
	notifier := n.newNotifier(telegram.BotToken, telegram.ChatID)
	if notifier == nil {
		return errors.New("telegram notifier is nil")
	}
	return notifier.Send(ctx, summary)
}

type Service struct {
	nodes                     NodeRepository
	targets                   TargetRepository
	snapshots                 SnapshotReader
	writer                    MutationWriter
	notifier                  Notifier
	settingsRepo              SettingsRepository
	logger                    *slog.Logger
	now                       func() time.Time
	fallbackHeartbeatInterval time.Duration
	fallbackSweepInterval     time.Duration
}

func NewService(nodesRepo NodeRepository, targetsRepo TargetRepository, snapshots SnapshotReader, writer MutationWriter, notifier Notifier, logger *slog.Logger, heartbeatInterval, sweepInterval time.Duration) *Service {
	return NewSettingsBackedService(nodesRepo, targetsRepo, snapshots, writer, notifier, nil, logger, heartbeatInterval, sweepInterval)
}

func NewSettingsBackedService(nodesRepo NodeRepository, targetsRepo TargetRepository, snapshots SnapshotReader, writer MutationWriter, notifier Notifier, settingsRepo SettingsRepository, logger *slog.Logger, heartbeatInterval, sweepInterval time.Duration) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultHeartbeatInterval
	}
	if sweepInterval <= 0 {
		sweepInterval = time.Minute
	}
	return &Service{
		nodes:                     nodesRepo,
		targets:                   targetsRepo,
		snapshots:                 snapshots,
		writer:                    writer,
		notifier:                  notifier,
		settingsRepo:              settingsRepo,
		logger:                    logger,
		now:                       func() time.Time { return time.Now().UTC() },
		fallbackHeartbeatInterval: heartbeatInterval,
		fallbackSweepInterval:     sweepInterval,
	}
}

func (s *Service) AfterSuccessfulSync(ctx context.Context, batch syncing.Batch, result syncing.Result) error {
	now := result.AcceptedAt
	if now.IsZero() {
		now = s.now()
	}
	if err := s.evaluateNode(ctx, batch.NodeID, now); err != nil {
		s.logger.Error("evaluate node incidents after sync failed", "node_id", batch.NodeID, "error", err)
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
	if err := s.EvaluateStaleNodes(ctx, now); err != nil {
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

func (s *Service) EvaluateStaleNodes(ctx context.Context, now time.Time) error {
	heartbeatInterval := s.heartbeatIntervalFor(ctx)
	records, err := s.nodes.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("list nodes for stale sweep: %w", err)
	}
	for _, record := range records {
		if err := s.evaluateNodeHeartbeatOnly(ctx, record, now, heartbeatInterval); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) evaluateNodeHeartbeatOnly(ctx context.Context, record nodes.Record, now time.Time, heartbeatInterval time.Duration) error {
	previous, err := s.snapshots.ListActiveIncidents(ctx, ObjectTypeNode, record.NodeID)
	if err != nil {
		return fmt.Errorf("list previous node incidents for %q: %w", record.NodeID, err)
	}
	previousByClass := incidentsByClass(previous)
	evaluations := []classEvaluation{{
		class:  IncidentNodeHeartbeatMissing,
		result: EvaluateNodeHeartbeatMissing(previousByClass[IncidentNodeHeartbeatMissing], record.NodeID, now, record.LastHeartbeatAt, heartbeatInterval),
	}}
	return s.applyEvaluations(ctx, ObjectTypeNode, record.NodeID, previous, evaluations, now)
}

func (s *Service) evaluateNode(ctx context.Context, nodeID string, now time.Time) error {
	record, err := s.nodes.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("get node %q for incident evaluation: %w", nodeID, err)
	}
	previous, err := s.snapshots.ListActiveIncidents(ctx, ObjectTypeNode, nodeID)
	if err != nil {
		return fmt.Errorf("list previous node incidents for %q: %w", nodeID, err)
	}
	previousByClass := incidentsByClass(previous)
	heartbeatInterval := s.heartbeatIntervalFor(ctx)
	evaluations := []classEvaluation{{
		class:  IncidentNodeHeartbeatMissing,
		result: EvaluateNodeHeartbeatMissing(previousByClass[IncidentNodeHeartbeatMissing], nodeID, now, record.LastHeartbeatAt, heartbeatInterval),
	}}

	hostSamples, err := s.snapshots.ListRecentHostSamples(ctx, nodeID, now.Add(-30*time.Minute))
	if err != nil {
		return fmt.Errorf("list recent host samples for %q: %w", nodeID, err)
	}
	if len(hostSamples) > 0 {
		latest := &hostSamples[0]
		resourceSamples := nodeResourceSamplesFromHostSamples(hostSamples)
		evaluations = append(evaluations,
			classEvaluation{class: IncidentNodeDiskPressure, result: EvaluateNodeDiskPressure(previousByClass[IncidentNodeDiskPressure], nodeID, latest)},
			classEvaluation{class: IncidentNodeInodePressure, result: EvaluateNodeInodePressure(previousByClass[IncidentNodeInodePressure], nodeID, latest)},
			classEvaluation{class: IncidentNodeResourcePressure, result: EvaluateNodeResourcePressure(previousByClass[IncidentNodeResourcePressure], nodeID, resourceSamples)},
		)
	}

	trendHostSamples, err := s.snapshots.ListRecentHostSamples(ctx, nodeID, now.Add(-24*time.Hour))
	if err != nil {
		return fmt.Errorf("list trend host samples for %q: %w", nodeID, err)
	}
	baselineStart, baselineEnd := trendBaselineWindow(now)
	nodeBaselines, err := s.snapshots.ListNodeHostDailyAggregates(ctx, nodeID, baselineStart, baselineEnd)
	if err != nil {
		return fmt.Errorf("list node trend baselines for %q: %w", nodeID, err)
	}
	evaluations = append(evaluations, classEvaluation{
		class:  IncidentNodeTrendDegradation,
		result: EvaluateNodeTrendDegradation(previousByClass[IncidentNodeTrendDegradation], nodeID, nodeResourceSamplesFromHostSamples(trendHostSamples), nodeBaselines),
	})

	return s.applyEvaluations(ctx, ObjectTypeNode, nodeID, previous, evaluations, now)
}

func (s *Service) evaluateTarget(ctx context.Context, targetID string, now time.Time) error {
	previous, err := s.snapshots.ListActiveIncidents(ctx, ObjectTypeTarget, targetID)
	if err != nil {
		return fmt.Errorf("list previous target incidents for %q: %w", targetID, err)
	}
	previousByClass := incidentsByClass(previous)
	observations, err := s.snapshots.ListRecentProbeObservations(ctx, targetID, now.Add(-6*time.Hour))
	if err != nil {
		return fmt.Errorf("list recent probe observations for %q: %w", targetID, err)
	}

	evaluations := []classEvaluation{
		{class: IncidentTargetProbeFailure, result: evaluateTargetProbeFailureAcrossSeries(previousByClass[IncidentTargetProbeFailure], targetID, observations)},
		{class: IncidentTargetTLSExpiry, result: evaluateTargetTLSExpiryAcrossSeries(previousByClass[IncidentTargetTLSExpiry], targetID, observations)},
	}
	trendObservations, err := s.snapshots.ListRecentProbeObservations(ctx, targetID, now.Add(-24*time.Hour))
	if err != nil {
		return fmt.Errorf("list trend probe observations for %q: %w", targetID, err)
	}
	baselineStart, baselineEnd := trendBaselineWindow(now)
	targetBaselines, err := s.snapshots.ListTargetProbeDailyAggregates(ctx, targetID, baselineStart, baselineEnd)
	if err != nil {
		return fmt.Errorf("list target trend baselines for %q: %w", targetID, err)
	}
	evaluations = append(evaluations, classEvaluation{
		class:  IncidentTargetLatencyTrendDegradation,
		result: EvaluateTargetLatencyTrendDegradationAcrossSeries(previousByClass[IncidentTargetLatencyTrendDegradation], targetID, trendObservations, targetBaselines),
	})
	return s.applyEvaluations(ctx, ObjectTypeTarget, targetID, previous, evaluations, now)
}

func (s *Service) applyEvaluations(ctx context.Context, objectType ObjectType, objectID string, previous []IncidentRecord, evaluations []classEvaluation, now time.Time) error {
	mutation := buildMutation(objectType, objectID, previous, evaluations)
	if err := s.writer.ApplyIncidentMutation(ctx, mutation); err != nil {
		return err
	}
	return s.appendNotificationRecords(ctx, objectType, objectID, evaluations)
}

type incidentTiming struct {
	heartbeatInterval time.Duration
	sweepInterval     time.Duration
}

type notificationPolicy struct {
	notifyOnStarted   bool
	notifyOnEscalated bool
	notifyOnRecovered bool
}

func defaultNotificationPolicy() notificationPolicy {
	return notificationPolicyFromDefaults(centersettings.Default().IncidentDefaults)
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
		heartbeatInterval: s.fallbackHeartbeatInterval,
		sweepInterval:     s.fallbackSweepInterval,
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
	return timing
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

func buildMutation(objectType ObjectType, objectID string, previous []IncidentRecord, evaluations []classEvaluation) IncidentMutation {
	activeByClass := incidentsByClass(previous)
	mutation := IncidentMutation{
		ObjectType:    objectType,
		ObjectID:      objectID,
		Active:        make([]IncidentRecord, 0),
		Events:        make([]StateChangeEventRecord, 0),
		Notifications: make([]NotificationRecordWrite, 0),
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
		record := NotificationRecordWrite{
			IncidentID:     incidentIdentity(evaluation),
			ObjectType:     objectType,
			ObjectID:       objectID,
			Channel:        decision.Channel,
			DeliveryStatus: DeliveryStatusSuppressed,
			Summary:        decision.Summary,
		}
		shouldSend := decision.ShouldSend && policy.enabled(decision.Reason)
		if shouldSend && s.notifier != nil {
			if err := s.notifier.Send(ctx, decision.Summary); err != nil {
				if errors.Is(err, errNotificationSuppressed) {
					records = append(records, record)
					continue
				}
				s.logger.Error("send incident notification failed", "object_type", objectType, "object_id", objectID, "error", err)
				record.DeliveryStatus = DeliveryStatusFailed
			} else {
				record.DeliveryStatus = DeliveryStatusSent
				now := s.now()
				record.SentAt = &now
			}
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil
	}
	return s.writer.AppendNotificationRecords(ctx, records)
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

func nodeResourceSamplesFromHostSamples(hostSamples []runtimefacts.HostSample) []NodeResourceSample {
	resourceSamples := make([]NodeResourceSample, 0, len(hostSamples))
	for _, sample := range hostSamples {
		resourceSamples = append(resourceSamples, NodeResourceSample{
			ObservedAt:         sample.ObservedAt,
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
	latestObservedAt := time.Time{}
	tcpNodes := map[string]struct{}{}
	httpProbeItems := map[string]struct{}{}

	for _, series := range grouped {
		result := EvaluateTargetProbeFailure(nil, targetID, series)
		if len(series) > 0 && series[0].ObservedAt.After(latestObservedAt) {
			latestObservedAt = series[0].ObservedAt
		}
		if result.Current != nil {
			activeResults = append(activeResults, result)
			if series[0].ProbeKind == agentapi.ProbeKindTCP && series[0].NodeID != "" {
				tcpNodes[series[0].NodeID] = struct{}{}
			}
			if series[0].ProbeKind == agentapi.ProbeKindHTTP && series[0].ProbeItemID != "" {
				httpProbeItems[series[0].ProbeItemID] = struct{}{}
			}
		}
		if consecutiveResults(series, agentapi.ProbeResultSuccess) >= 2 {
			recoveryEligibleCount++
			if observationSeriesSuppressed(series) {
				recoverySuppressed = true
			}
		}
	}

	if len(activeResults) == 0 {
		if previous != nil && recoveryEligibleCount == len(grouped) && len(grouped) > 0 {
			result := recoverIfNeeded(previous, latestObservedAt, "探针已连续成功恢复")
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
	if len(tcpNodes) >= 2 {
		severity = SeverityCritical
		summary = "TCP 探针在多个执行节点上持续失败"
	}
	if len(httpProbeItems) >= 2 {
		severity = SeverityCritical
		summary = "HTTP 多个 ProbeItem 同时异常"
	}
	return evaluateTransition(previous, ObjectTypeTarget, targetID, IncidentTargetProbeFailure, severity, latestObservedAt, summary)
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
			if observationSeriesSuppressed(series) {
				recoverySuppressed = true
			}
		}
	}

	if len(activeResults) == 0 {
		if previous != nil && recoveryEligibleCount == len(grouped) && len(grouped) > 0 {
			result := recoverIfNeeded(previous, latestObservedAt, "TLS 到期风险解除")
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
	return evaluateTransition(previous, ObjectTypeTarget, targetID, IncidentTargetTLSExpiry, severity, latestObservedAt, summary)
}

func groupProbeSeries(observations []runtimefacts.ProbeObservation) [][]runtimefacts.ProbeObservation {
	grouped := map[string][]runtimefacts.ProbeObservation{}
	keys := make([]string, 0)
	for _, observation := range normalizeProbeObservations(observations) {
		key := observation.ProbeItemID + "|" + observation.NodeID
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
	db *pgxpool.Pool
}

func NewPostgresSnapshotReader(db *pgxpool.Pool) *PostgresSnapshotReader {
	return &PostgresSnapshotReader{db: db}
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

func (r *PostgresSnapshotReader) ListRecentHostSamples(ctx context.Context, nodeID string, since time.Time) ([]runtimefacts.HostSample, error) {
	rows, err := r.db.Query(ctx, `
		select
			node_id, observed_at, received_at, agent_version, fingerprint,
			cpu_usage_pct, load_1, load_5, load_15, mem_used_pct, mem_available_bytes,
			swap_used_pct, disk_used_pct, inode_used_pct, net_in_bytes_per_sec,
			net_out_bytes_per_sec, cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec,
			disk_write_bytes_per_sec, disk_busy_pct, uptime_seconds,
			maintenance_context, is_backfilled, sync_batch_id
		from host_samples
		where node_id = $1 and observed_at >= $2
		order by observed_at desc, id desc`, nodeID, since)
	if err != nil {
		return nil, fmt.Errorf("query host samples for %q: %w", nodeID, err)
	}
	defer rows.Close()
	out := make([]runtimefacts.HostSample, 0)
	for rows.Next() {
		var sample runtimefacts.HostSample
		if err := rows.Scan(
			&sample.NodeID, &sample.ObservedAt, &sample.ReceivedAt, &sample.AgentVersion, &sample.Fingerprint,
			&sample.CPUUsagePct, &sample.Load1, &sample.Load5, &sample.Load15, &sample.MemUsedPct, &sample.MemAvailableBytes,
			&sample.SwapUsedPct, &sample.DiskUsedPct, &sample.InodeUsedPct, &sample.NetInBytesPerSec,
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
	rows, err := r.db.Query(ctx, `
		select
			po.node_id, po.target_id, po.probe_item_id, pi.probe_kind,
			po.observed_at, po.received_at, po.agent_version, po.fingerprint,
			po.result_kind, po.latency_ms, po.http_status, po.tls_expiry_days,
			coalesce(po.error_code, ''), coalesce(po.error_summary, ''),
			po.maintenance_context, po.is_backfilled, po.sync_batch_id
		from probe_observations po
		join probe_items pi on pi.probe_item_id = po.probe_item_id
		where po.target_id = $1 and po.observed_at >= $2
		order by po.observed_at desc, po.id desc`, targetID, since)
	if err != nil {
		return nil, fmt.Errorf("query probe observations for %q: %w", targetID, err)
	}
	defer rows.Close()
	out := make([]runtimefacts.ProbeObservation, 0)
	for rows.Next() {
		var observation runtimefacts.ProbeObservation
		if err := rows.Scan(
			&observation.NodeID, &observation.TargetID, &observation.ProbeItemID, &observation.ProbeKind,
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

func (r *PostgresSnapshotReader) ListNodeHostDailyAggregates(ctx context.Context, nodeID string, since, before time.Time) ([]NodeHostDailyAggregate, error) {
	rows, err := r.db.Query(ctx, `
		select
			bucket_date,
			sample_count,
			avg_load_5,
			avg_cpu_iowait_pct,
			avg_cpu_steal_pct,
			backfilled_sample_count,
			maintenance_sample_count
		from node_host_sample_daily_aggregates
		where node_id = $1
			and bucket_date >= $2::date
			and bucket_date < $3::date
		order by bucket_date desc`, nodeID, since, before)
	if err != nil {
		return nil, fmt.Errorf("query node host daily aggregates for %q: %w", nodeID, err)
	}
	defer rows.Close()
	out := make([]NodeHostDailyAggregate, 0)
	for rows.Next() {
		var aggregate NodeHostDailyAggregate
		if err := rows.Scan(
			&aggregate.BucketDate,
			&aggregate.SampleCount,
			&aggregate.AvgLoad5,
			&aggregate.AvgCPUIOWaitPct,
			&aggregate.AvgCPUStealPct,
			&aggregate.BackfilledSampleCount,
			&aggregate.MaintenanceSampleCount,
		); err != nil {
			return nil, fmt.Errorf("scan node host daily aggregate: %w", err)
		}
		out = append(out, aggregate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node host daily aggregates: %w", err)
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

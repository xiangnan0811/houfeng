package incidents

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/contracts/agentapi"
)

const maxHeartbeatIncidentInterval = time.Duration(1<<63-1) / 2

func EvaluateMonitoringInstanceHeartbeatMissing(previous *IncidentRecord, monitoringInstanceID string, now time.Time, lastHeartbeatAt *time.Time, policy HeartbeatIncidentPolicy, recoveryReceipts []LiveHeartbeatReceipt) EvaluationResult {
	if lastHeartbeatAt == nil || !validHeartbeatIncidentPolicy(policy) {
		return noop(previous)
	}
	missed := heartbeatMissedIntervals(now, *lastHeartbeatAt, policy.HeartbeatInterval)
	severity, summary, active := heartbeatSeverity(missed, policy.MissingThreshold)
	if !active {
		if previous == nil || !hasStableHeartbeatRecoveryEvidence(previous, policy, recoveryReceipts) {
			return noop(previous)
		}
		return recoverIfNeeded(previous, now, "心跳已恢复", MonitoringEventProvenanceCenter, false)
	}
	return evaluateTransition(previous, ObjectTypeMonitoringInstance, monitoringInstanceID, IncidentMonitoringInstanceHeartbeatMissing, severity, now, summary, MonitoringEventProvenanceCenter)
}

func heartbeatMissedIntervals(now, lastHeartbeatAt time.Time, heartbeatInterval time.Duration) int {
	if heartbeatInterval <= 0 || !now.After(lastHeartbeatAt) {
		return 0
	}
	missed := now.Sub(lastHeartbeatAt) / heartbeatInterval
	if missed <= 0 {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	if uint64(missed) > uint64(maxInt) {
		return maxInt
	}
	return int(missed)
}

func validHeartbeatIncidentPolicy(policy HeartbeatIncidentPolicy) bool {
	return policy.HeartbeatInterval > 0 &&
		policy.HeartbeatInterval <= maxHeartbeatIncidentInterval &&
		policy.MissingThreshold > 0 &&
		policy.MissingThreshold <= int(^uint(0)>>1)/4 &&
		policy.RecoverySuccesses == heartbeatRecoverySuccesses &&
		policy.RecoveryMaxIntervalGap == 2*policy.HeartbeatInterval
}

func hasStableHeartbeatRecoveryEvidence(previous *IncidentRecord, policy HeartbeatIncidentPolicy, receipts []LiveHeartbeatReceipt) bool {
	if previous == nil || previous.StartedAt.IsZero() || len(receipts) < policy.RecoverySuccesses {
		return false
	}

	latestByBatch := make(map[string]time.Time, len(receipts))
	for _, receipt := range receipts {
		batchID := strings.TrimSpace(receipt.SyncBatchID)
		if batchID == "" || !receipt.ReceivedAt.After(previous.StartedAt) {
			continue
		}
		if current, ok := latestByBatch[batchID]; !ok || receipt.ReceivedAt.After(current) {
			latestByBatch[batchID] = receipt.ReceivedAt
		}
	}
	if len(latestByBatch) < policy.RecoverySuccesses {
		return false
	}

	receivedAt := make([]time.Time, 0, len(latestByBatch))
	for _, value := range latestByBatch {
		receivedAt = append(receivedAt, value)
	}
	sort.Slice(receivedAt, func(i, j int) bool { return receivedAt[i].After(receivedAt[j]) })
	receivedAt = receivedAt[:policy.RecoverySuccesses]
	sort.Slice(receivedAt, func(i, j int) bool { return receivedAt[i].Before(receivedAt[j]) })
	for i := 1; i < len(receivedAt); i++ {
		if receivedAt[i].Sub(receivedAt[i-1]) > policy.RecoveryMaxIntervalGap {
			return false
		}
	}
	return true
}

func EvaluateMonitoringInstanceDiskPressure(previous *IncidentRecord, monitoringInstanceID string, sample *runtimefacts.HostSample, thresholds MetricThresholds) EvaluationResult {
	if sample == nil {
		return noop(previous)
	}
	suppressed := sample.MaintenanceContext || sample.IsBackfilled
	if suppressed {
		result := evaluateMonitoringInstanceDiskPressure(previous, monitoringInstanceID, sample, thresholds)
		if result.Transition == TransitionRecovered {
			return suppressNotification(result)
		}
		return skip(previous)
	}
	return evaluateMonitoringInstanceDiskPressure(previous, monitoringInstanceID, sample, thresholds)
}

func EvaluateMonitoringInstanceInodePressure(previous *IncidentRecord, monitoringInstanceID string, sample *runtimefacts.HostSample, thresholds MetricThresholds) EvaluationResult {
	if sample == nil {
		return noop(previous)
	}
	suppressed := sample.MaintenanceContext || sample.IsBackfilled
	if suppressed {
		result := evaluateMonitoringInstanceInodePressure(previous, monitoringInstanceID, sample, thresholds)
		if result.Transition == TransitionRecovered {
			return suppressNotification(result)
		}
		return skip(previous)
	}
	return evaluateMonitoringInstanceInodePressure(previous, monitoringInstanceID, sample, thresholds)
}

func EvaluateMonitoringInstanceResourcePressure(previous *IncidentRecord, monitoringInstanceID string, samples []MonitoringInstanceResourceSample, thresholds MetricThresholds, policy ResourcePressurePolicy) EvaluationResult {
	samples = normalizeMonitoringInstanceResourceSamples(samples)
	if len(samples) == 0 || !validResourcePressurePolicy(policy) {
		return noop(previous)
	}

	referenceTime := samples[0].ObservedAt
	activeWindow15 := buildResourcePressureWindowSorted(samples, referenceTime, 15*time.Minute, policy, false)
	activeWindow30 := buildResourcePressureWindowSorted(samples, referenceTime, 30*time.Minute, policy, false)
	suppressed := resourcePressureLatestSuppressed(samples)

	var recoveryWindow15, recoveryWindow30 resourcePressureWindow
	if previous != nil {
		recoveryWindow15 = buildResourcePressureWindowSorted(samples, referenceTime, 15*time.Minute, policy, true)
		recoveryWindow30 = buildResourcePressureWindowSorted(samples, referenceTime, 30*time.Minute, policy, true)
	}
	recoveryWindow := recoveryWindow15
	if previous != nil && previous.Severity == SeverityCritical {
		recoveryWindow = recoveryWindow30
	}
	recoveryEvidenceUsable := previous == nil || (recoveryWindow.covered && recoveryWindow.cpuUsable)
	severity := resourcePressureSeverity(activeWindow15, activeWindow30, thresholds, recoveryEvidenceUsable)
	if !severity.active {
		if previous == nil {
			return noop(previous)
		}
		if !recoveryWindow.covered || !recoveryWindow.cpuUsable ||
			resourcePressureRecoveryHasPressure(recoveryWindow15, recoveryWindow30, thresholds, previous.Severity == SeverityCritical) {
			return noop(previous)
		}
		result := recoverIfNeeded(previous, referenceTime, "资源压力恢复到安全区间", MonitoringEventProvenanceAgentSync, samples[0].IsBackfilled)
		if suppressed {
			return suppressNotification(result)
		}
		return result
	}

	if previous != nil {
		if (!recoveryWindow.covered || !recoveryWindow.cpuUsable) &&
			(severity.cpu || severityRank(severity.severity) <= severityRank(previous.Severity)) {
			return noop(previous)
		}
		if severityRank(severity.severity) < severityRank(previous.Severity) &&
			resourcePressureBoundarySeverityHigher(recoveryWindow15, recoveryWindow30, thresholds, severity.severity, true) {
			return noop(previous)
		}
	}
	if suppressed {
		return skip(previous)
	}
	return evaluateTransition(previous, ObjectTypeMonitoringInstance, monitoringInstanceID, IncidentMonitoringInstanceResourcePressure, severity.severity, referenceTime, severity.summary, MonitoringEventProvenanceAgentSync)
}

func EvaluateTargetProbeFailure(previous *IncidentRecord, targetID string, recent []runtimefacts.ProbeObservation) EvaluationResult {
	recent = normalizeProbeObservations(recent)
	if len(recent) == 0 {
		return noop(previous)
	}
	suppressed := recent[0].MaintenanceContext || recent[0].IsBackfilled
	failureWindow := leadingUnsuppressedFailureObservations(recent)
	failureCount := len(failureWindow)
	successCount := consecutiveResults(recent, agentapi.ProbeResultSuccess)
	if previous != nil && successCount >= 2 {
		result := recoverIfNeeded(previous, recent[0].ObservedAt, "探针已连续成功恢复", MonitoringEventProvenanceAgentSync, recent[0].IsBackfilled)
		if suppressed {
			return suppressNotification(result)
		}
		return result
	}
	severity, active := probeFailureSeverity(failureWindow)
	if !active {
		return noop(previous)
	}
	if suppressed {
		return skip(previous)
	}
	summary := fmt.Sprintf("%s 探针连续失败 %d 次", recent[0].ProbeKind, failureCount)
	if recent[0].ErrorSummary != "" {
		summary = fmt.Sprintf("%s（%s）", summary, recent[0].ErrorSummary)
	}
	return evaluateTransition(previous, ObjectTypeTarget, targetID, IncidentTargetProbeFailure, severity, recent[0].ObservedAt, summary, MonitoringEventProvenanceAgentSync)
}

func EvaluateTargetTLSExpiry(previous *IncidentRecord, targetID string, recent []runtimefacts.ProbeObservation) EvaluationResult {
	recent = normalizeProbeObservations(recent)
	if len(recent) == 0 {
		return noop(previous)
	}
	suppressed := recent[0].MaintenanceContext || recent[0].IsBackfilled
	if recent[0].TLSExpiryDays == nil {
		return noop(previous)
	}
	severity, active := tlsExpirySeverity(*recent[0].TLSExpiryDays)
	if !active {
		result := recoverIfNeeded(previous, recent[0].ObservedAt, "TLS 到期风险解除", MonitoringEventProvenanceAgentSync, recent[0].IsBackfilled)
		if suppressed {
			return suppressNotification(result)
		}
		return result
	}
	if suppressed {
		return skip(previous)
	}
	summary := fmt.Sprintf("TLS 证书剩余 %d 天", *recent[0].TLSExpiryDays)
	return evaluateTransition(previous, ObjectTypeTarget, targetID, IncidentTargetTLSExpiry, severity, recent[0].ObservedAt, summary, MonitoringEventProvenanceAgentSync)
}

func EvaluateMonitoringInstanceTrendDegradation(previous *IncidentRecord, monitoringInstanceID string, samples []MonitoringInstanceResourceSample, baselines []MonitoringInstanceHostDailyAggregate) EvaluationResult {
	samples = normalizeMonitoringInstanceResourceSamples(samples)
	if len(samples) == 0 {
		return noop(previous)
	}
	if samples[0].MaintenanceContext || samples[0].IsBackfilled {
		return skip(previous)
	}
	usableCurrent := unsuppressedMonitoringInstanceResourceSamples(samples)
	if len(usableCurrent) < 3 || !spansMonitoringInstanceResourceWindow(usableCurrent, time.Second) {
		if previous != nil {
			return noop(previous)
		}
		return EvaluationResult{Transition: TransitionNoop}
	}

	referenceTime := usableCurrent[0].ObservedAt
	loadBaseline, iowaitBaseline, stealBaseline := weightedMonitoringInstanceTrendBaselines(baselines)
	loadCurrent := averageMonitoringInstanceResourceMetric(usableCurrent, func(sample MonitoringInstanceResourceSample) float64 { return sample.NormalizedLoad5 })
	cpuCurrentUsable := cpuWindowUsable(usableCurrent, time.Second)

	degradedMetrics := make([]string, 0, 3)
	if loadBaseline != nil && nodeTrendMetricDegraded(loadCurrent, *loadBaseline, 1.6, 0.6) {
		degradedMetrics = append(degradedMetrics, "load5")
	}
	if cpuCurrentUsable && iowaitBaseline != nil {
		iowaitCurrent := averageMonitoringInstanceResourceMetric(usableCurrent, func(sample MonitoringInstanceResourceSample) float64 { return sample.CPUIOWaitPct })
		if nodeTrendMetricDegraded(iowaitCurrent, *iowaitBaseline, 8, 4) {
			degradedMetrics = append(degradedMetrics, "iowait")
		}
	}
	if cpuCurrentUsable && stealBaseline != nil {
		stealCurrent := averageMonitoringInstanceResourceMetric(usableCurrent, func(sample MonitoringInstanceResourceSample) float64 { return sample.CPUStealPct })
		if nodeTrendMetricDegraded(stealCurrent, *stealBaseline, 3, 2) {
			degradedMetrics = append(degradedMetrics, "steal")
		}
	}
	if len(degradedMetrics) == 0 {
		if previous == nil {
			return EvaluationResult{Transition: TransitionNoop}
		}
		if !spansMonitoringInstanceResourceWindow(usableCurrent, 30*time.Minute) || !cpuCurrentUsable || loadBaseline == nil || iowaitBaseline == nil || stealBaseline == nil {
			return noop(previous)
		}
		return recoverIfNeeded(previous, referenceTime, "监控实例趋势已恢复到安全区间", MonitoringEventProvenanceAgentSync, false)
	}

	severity := SeverityNotice
	if len(degradedMetrics) >= 2 {
		severity = SeverityAlert
	}
	if previous != nil && (!cpuCurrentUsable || loadBaseline == nil || iowaitBaseline == nil || stealBaseline == nil) && severityRank(severity) <= severityRank(previous.Severity) {
		return noop(previous)
	}
	return evaluateTransition(previous, ObjectTypeMonitoringInstance, monitoringInstanceID, IncidentMonitoringInstanceTrendDegradation, severity, referenceTime, fmt.Sprintf("监控实例趋势劣化：%s", joinMetricLabels(degradedMetrics)), MonitoringEventProvenanceAgentSync)
}

func EvaluateTargetLatencyTrendDegradationAcrossSeries(previous *IncidentRecord, targetID string, observations []runtimefacts.ProbeObservation, baselines []TargetProbeDailyAggregate) EvaluationResult {
	observations = normalizeProbeObservations(observations)
	if len(observations) == 0 {
		return noop(previous)
	}
	if observations[0].MaintenanceContext || observations[0].IsBackfilled {
		return skip(previous)
	}
	usableBaselines := weightedTargetLatencyBaselines(baselines)
	series := usableLatencyObservationSeries(observations)
	if len(usableBaselines) == 0 || len(series) == 0 {
		if previous != nil {
			return noop(previous)
		}
		return EvaluationResult{Transition: TransitionNoop}
	}

	referenceTime := observations[0].ObservedAt
	degradedProbeItems := make([]string, 0)
	degradedMonitoringInstances := map[string]struct{}{}
	comparableSeries := 0
	for probeItemID, currentSeries := range series {
		baseline, ok := usableBaselines[probeItemID]
		if !ok || len(currentSeries) < 3 {
			continue
		}
		comparableSeries++
		currentAvg := averageProbeLatencyMS(currentSeries)
		degradedMonitoringInstancesForProbe := degradedTargetLatencyMonitoringInstances(currentSeries, baseline)
		if !targetLatencySeriesDegraded(currentAvg, baseline) && len(degradedMonitoringInstancesForProbe) == 0 {
			continue
		}
		degradedProbeItems = append(degradedProbeItems, probeItemID)
		for _, monitoringInstanceID := range degradedMonitoringInstancesForProbe {
			degradedMonitoringInstances[monitoringInstanceID] = struct{}{}
		}
	}
	if comparableSeries == 0 {
		return noop(previous)
	}
	if len(degradedProbeItems) == 0 {
		if previous != nil && !hasSustainedTargetLatencyRecoveryEvidence(series, usableBaselines, 30*time.Minute) {
			return noop(previous)
		}
		return recoverIfNeeded(previous, referenceTime, "目标延迟趋势已恢复到安全区间", MonitoringEventProvenanceAgentSync, false)
	}

	severity := SeverityNotice
	if len(degradedProbeItems) >= 2 || len(degradedMonitoringInstances) >= 2 {
		severity = SeverityAlert
	}
	sort.Strings(degradedProbeItems)
	return evaluateTransition(previous, ObjectTypeTarget, targetID, IncidentTargetLatencyTrendDegradation, severity, referenceTime, fmt.Sprintf("目标延迟趋势劣化：%s", joinMetricLabels(degradedProbeItems)), MonitoringEventProvenanceAgentSync)
}

func noop(previous *IncidentRecord) EvaluationResult {
	return EvaluationResult{Current: cloneIncident(previous), Transition: TransitionNoop}
}

func skip(_ *IncidentRecord) EvaluationResult {
	return EvaluationResult{Transition: TransitionSkipped}
}

func suppressNotification(result EvaluationResult) EvaluationResult {
	if result.Notification == nil {
		return result
	}
	result.Notification = &NotificationDecision{
		ShouldSend: false,
		Channel:    result.Notification.Channel,
		Reason:     result.Notification.Reason,
		Severity:   result.Notification.Severity,
		Summary:    result.Notification.Summary,
	}
	return result
}

func evaluateMonitoringInstanceDiskPressure(previous *IncidentRecord, monitoringInstanceID string, sample *runtimefacts.HostSample, thresholds MetricThresholds) EvaluationResult {
	severity, active := fastThresholdSeverity(sample.DiskUsedPct, float64(thresholds.DiskWarningPct), float64(thresholds.DiskAlertPct), float64(thresholds.DiskCriticalPct))
	if !active {
		return recoverIfNeeded(previous, sample.ObservedAt, "磁盘使用率恢复到安全区间", MonitoringEventProvenanceAgentSync, sample.IsBackfilled)
	}
	summary := fmt.Sprintf("磁盘使用率 %.1f%%", sample.DiskUsedPct)
	return evaluateTransition(previous, ObjectTypeMonitoringInstance, monitoringInstanceID, IncidentMonitoringInstanceDiskPressure, severity, sample.ObservedAt, summary, MonitoringEventProvenanceAgentSync)
}

func evaluateMonitoringInstanceInodePressure(previous *IncidentRecord, monitoringInstanceID string, sample *runtimefacts.HostSample, thresholds MetricThresholds) EvaluationResult {
	severity, active := fastThresholdSeverity(sample.InodeUsedPct, float64(thresholds.InodeWarningPct), float64(thresholds.InodeAlertPct), float64(thresholds.InodeCriticalPct))
	if !active {
		return recoverIfNeeded(previous, sample.ObservedAt, "inode 使用率恢复到安全区间", MonitoringEventProvenanceAgentSync, sample.IsBackfilled)
	}
	summary := fmt.Sprintf("inode 使用率 %.1f%%", sample.InodeUsedPct)
	return evaluateTransition(previous, ObjectTypeMonitoringInstance, monitoringInstanceID, IncidentMonitoringInstanceInodePressure, severity, sample.ObservedAt, summary, MonitoringEventProvenanceAgentSync)
}

func recoverIfNeeded(previous *IncidentRecord, when time.Time, summary, provenance string, isBackfilled bool) EvaluationResult {
	if previous == nil {
		return EvaluationResult{Transition: TransitionNoop}
	}
	if when.IsZero() {
		when = previous.LastEvaluatedAt
	}
	return EvaluationResult{
		Transition: TransitionRecovered,
		Event: &StateChangeEventRecord{
			IncidentID:          previous.IncidentID,
			IncidentClass:       previous.IncidentClass,
			ObjectType:          previous.ObjectType,
			ObjectID:            previous.ObjectID,
			EventType:           EventIncidentRecovered,
			Severity:            previous.Severity,
			Summary:             summary,
			CreatedAt:           canonicalMonitoringEventTime(when),
			IsBackfilled:        isBackfilled,
			Provenance:          provenance,
			ProducerVersion:     MonitoringEventProducerVersion,
			RuleVersion:         MonitoringEventIncidentRuleVersion,
			PriorState:          monitoringEventIncidentState(previous.Severity),
			ResultingState:      "normal",
			CorrectionOfEventID: "",
		},
		Notification: &NotificationDecision{
			ShouldSend: true,
			Channel:    NotificationChannelTelegram,
			Reason:     NotificationReasonRecovered,
			Severity:   previous.Severity,
			Summary:    summary,
		},
	}
}

func evaluateTransition(previous *IncidentRecord, objectType ObjectType, objectID string, class IncidentClass, severity Severity, when time.Time, summary, provenance string) EvaluationResult {
	current := &IncidentRecord{
		IncidentID:      incidentID(objectType, objectID, class),
		ObjectType:      objectType,
		ObjectID:        objectID,
		IncidentClass:   class,
		Severity:        severity,
		StartedAt:       when,
		LastEvaluatedAt: when,
		Status:          IncidentStatusActive,
		SourceSummary:   summary,
	}
	if previous == nil {
		return EvaluationResult{
			Current:      current,
			Transition:   TransitionStarted,
			Event:        newIncidentStateChangeEvent(current, EventIncidentStarted, severity, summary, when, provenance, "normal", monitoringEventIncidentState(severity)),
			Notification: &NotificationDecision{ShouldSend: true, Channel: NotificationChannelTelegram, Reason: NotificationReasonStarted, Severity: severity, Summary: summary},
		}
	}
	current.StartedAt = previous.StartedAt
	if severityRank(severity) > severityRank(previous.Severity) {
		return EvaluationResult{
			Current:      current,
			Transition:   TransitionEscalated,
			Event:        newIncidentStateChangeEvent(current, EventIncidentEscalated, severity, summary, when, provenance, monitoringEventIncidentState(previous.Severity), monitoringEventIncidentState(severity)),
			Notification: &NotificationDecision{ShouldSend: true, Channel: NotificationChannelTelegram, Reason: NotificationReasonEscalated, Severity: severity, Summary: summary},
		}
	}
	return EvaluationResult{Current: current, Transition: TransitionNoop}
}

func newIncidentStateChangeEvent(incident *IncidentRecord, eventType EventType, severity Severity, summary string, when time.Time, provenance, priorState, resultingState string) *StateChangeEventRecord {
	return &StateChangeEventRecord{
		IncidentID:          incident.IncidentID,
		IncidentClass:       incident.IncidentClass,
		ObjectType:          incident.ObjectType,
		ObjectID:            incident.ObjectID,
		EventType:           eventType,
		Severity:            severity,
		Summary:             summary,
		CreatedAt:           canonicalMonitoringEventTime(when),
		IsBackfilled:        false,
		Provenance:          provenance,
		ProducerVersion:     MonitoringEventProducerVersion,
		RuleVersion:         MonitoringEventIncidentRuleVersion,
		PriorState:          priorState,
		ResultingState:      resultingState,
		CorrectionOfEventID: "",
	}
}

func canonicalMonitoringEventTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func monitoringEventIncidentState(severity Severity) string {
	switch severity {
	case SeverityNormal:
		return "normal"
	case SeverityNotice:
		return "notice"
	case SeverityAlert:
		return "alert"
	case SeverityCritical:
		return "critical"
	default:
		return ""
	}
}

func incidentID(objectType ObjectType, objectID string, class IncidentClass) string {
	return fmt.Sprintf("inc_%s_%s_%s", objectType, objectID, class)
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityNotice:
		return 1
	case SeverityAlert:
		return 2
	case SeverityCritical:
		return 3
	default:
		return 0
	}
}

func heartbeatSeverity(missed, missingThreshold int) (Severity, string, bool) {
	alertThreshold := 2 * missingThreshold
	criticalThreshold := 4 * missingThreshold
	switch {
	case missed >= criticalThreshold:
		return SeverityCritical, fmt.Sprintf("最近 %d 个心跳周期未收到心跳", missed), true
	case missed >= alertThreshold:
		return SeverityAlert, fmt.Sprintf("最近 %d 个心跳周期未收到心跳", missed), true
	case missed >= missingThreshold:
		return SeverityNotice, fmt.Sprintf("最近 %d 个心跳周期未收到心跳", missed), true
	default:
		return SeverityNormal, "", false
	}
}

func fastThresholdSeverity(value, notice, alert, critical float64) (Severity, bool) {
	switch {
	case value >= critical:
		return SeverityCritical, true
	case value >= alert:
		return SeverityAlert, true
	case value >= notice:
		return SeverityNotice, true
	default:
		return SeverityNormal, false
	}
}

type resourcePressureSeverityResult struct {
	severity Severity
	summary  string
	active   bool
	cpu      bool
}

type resourcePressureWindowStats struct {
	count           int
	cpuUsable       bool
	cpuUsageTotal   float64
	loadTotal       float64
	memTotal        float64
	swapTotal       float64
	iowaitTotal     float64
	stealTotal      float64
	memAvailableMin float64
}

func resourcePressureSeverity(window15, window30 resourcePressureWindow, thresholds MetricThresholds, cpuCandidatesEnabled bool) resourcePressureSeverityResult {
	stats15 := resourcePressureWindowStatistics(window15)
	stats30 := resourcePressureWindowStatistics(window30)
	boundaryStats15 := resourcePressureWindowBoundaryStatistics(window15)
	boundaryStats30 := resourcePressureWindowBoundaryStatistics(window30)
	return resourcePressureSeverityFromStats(
		window15, stats15, boundaryStats15,
		window30, stats30, boundaryStats30,
		thresholds, true, cpuCandidatesEnabled,
	)
}

type resourcePressurePredicate uint8

const (
	resourcePressureCriticalCPU30 resourcePressurePredicate = iota
	resourcePressureCriticalLoad30
	resourcePressureCriticalMemory30
	resourcePressureCriticalIOWait30
	resourcePressureAlertCPU15
	resourcePressureAlertLoad15
	resourcePressureAlertMemory15
	resourcePressureAlertIOWait15
	resourcePressureAlertSteal30
	resourcePressureNoticeCPU15
	resourcePressureNoticeLoad15
	resourcePressureNoticeMemory15
	resourcePressureNoticeIOWait15
	resourcePressureNoticeSwap15
	resourcePressureNoticeIOWaitFloor15
	resourcePressureNoticeSteal15
)

func resourcePressurePredicateMatches(predicate resourcePressurePredicate, stats resourcePressureWindowStats, thresholds MetricThresholds) bool {
	switch predicate {
	case resourcePressureCriticalCPU30:
		return stats.cpuUsageTotal/float64(stats.count) >= float64(thresholds.CPUCriticalPct)
	case resourcePressureCriticalLoad30:
		return stats.loadTotal/float64(stats.count) >= thresholds.Load5Critical
	case resourcePressureCriticalMemory30:
		return stats.memTotal/float64(stats.count) >= float64(thresholds.MemCriticalPct) &&
			stats.memAvailableMin <= 512*1024*1024
	case resourcePressureCriticalIOWait30:
		return stats.iowaitTotal/float64(stats.count) >= float64(thresholds.IOWaitCriticalPct)
	case resourcePressureAlertCPU15:
		return stats.cpuUsageTotal/float64(stats.count) >= float64(thresholds.CPUAlertPct)
	case resourcePressureAlertLoad15:
		return stats.loadTotal/float64(stats.count) >= thresholds.Load5Alert
	case resourcePressureAlertMemory15:
		return stats.memTotal/float64(stats.count) >= float64(thresholds.MemAlertPct)
	case resourcePressureAlertIOWait15:
		return stats.iowaitTotal/float64(stats.count) >= float64(thresholds.IOWaitAlertPct)
	case resourcePressureAlertSteal30:
		return stats.stealTotal/float64(stats.count) >= 10
	case resourcePressureNoticeCPU15:
		return stats.cpuUsageTotal/float64(stats.count) >= float64(thresholds.CPUWarningPct)
	case resourcePressureNoticeLoad15:
		return stats.loadTotal/float64(stats.count) >= thresholds.Load5Warning
	case resourcePressureNoticeMemory15:
		return stats.memTotal/float64(stats.count) >= float64(thresholds.MemWarningPct)
	case resourcePressureNoticeIOWait15:
		return stats.iowaitTotal/float64(stats.count) >= float64(thresholds.IOWaitWarningPct)
	case resourcePressureNoticeSwap15:
		return stats.swapTotal/float64(stats.count) > 10
	case resourcePressureNoticeIOWaitFloor15:
		return stats.iowaitTotal/float64(stats.count) >= 10
	case resourcePressureNoticeSteal15:
		return stats.stealTotal/float64(stats.count) >= 5
	default:
		return false
	}
}

func resourcePressureSeverityFromStats(
	window15 resourcePressureWindow,
	stats15, boundaryStats15 resourcePressureWindowStats,
	window30 resourcePressureWindow,
	stats30, boundaryStats30 resourcePressureWindowStats,
	thresholds MetricThresholds,
	requireSingletonBoundary bool,
	cpuCandidatesEnabled bool,
) resourcePressureSeverityResult {
	switch {
	case cpuCandidatesEnabled && resourcePressureCandidateSupported(window30, stats30, boundaryStats30, true, requireSingletonBoundary, resourcePressureCriticalCPU30, thresholds):
		return resourcePressureSeverityResult{SeverityCritical, fmt.Sprintf("CPU 连续 30m 平均 %.1f%%", stats30.cpuUsageTotal/float64(stats30.count)), true, true}
	case resourcePressureCandidateSupported(window30, stats30, boundaryStats30, false, requireSingletonBoundary, resourcePressureCriticalLoad30, thresholds):
		return resourcePressureSeverityResult{SeverityCritical, fmt.Sprintf("归一化 Load5 连续 30m 平均 %.1f", stats30.loadTotal/float64(stats30.count)), true, false}
	case resourcePressureCandidateSupported(window30, stats30, boundaryStats30, false, requireSingletonBoundary, resourcePressureCriticalMemory30, thresholds):
		return resourcePressureSeverityResult{SeverityCritical, fmt.Sprintf("内存连续 30m 平均 %.1f%%，可用内存持续偏低", stats30.memTotal/float64(stats30.count)), true, false}
	case cpuCandidatesEnabled && resourcePressureCandidateSupported(window30, stats30, boundaryStats30, true, requireSingletonBoundary, resourcePressureCriticalIOWait30, thresholds):
		return resourcePressureSeverityResult{SeverityCritical, fmt.Sprintf("iowait 连续 30m 平均 %.1f%%", stats30.iowaitTotal/float64(stats30.count)), true, true}
	case cpuCandidatesEnabled && resourcePressureCandidateSupported(window15, stats15, boundaryStats15, true, requireSingletonBoundary, resourcePressureAlertCPU15, thresholds):
		return resourcePressureSeverityResult{SeverityAlert, fmt.Sprintf("CPU 连续 15m 平均 %.1f%%", stats15.cpuUsageTotal/float64(stats15.count)), true, true}
	case resourcePressureCandidateSupported(window15, stats15, boundaryStats15, false, requireSingletonBoundary, resourcePressureAlertLoad15, thresholds):
		return resourcePressureSeverityResult{SeverityAlert, fmt.Sprintf("归一化 Load5 连续 15m 平均 %.1f", stats15.loadTotal/float64(stats15.count)), true, false}
	case resourcePressureCandidateSupported(window15, stats15, boundaryStats15, false, requireSingletonBoundary, resourcePressureAlertMemory15, thresholds):
		return resourcePressureSeverityResult{SeverityAlert, fmt.Sprintf("内存连续 15m 平均 %.1f%%", stats15.memTotal/float64(stats15.count)), true, false}
	case cpuCandidatesEnabled && resourcePressureCandidateSupported(window15, stats15, boundaryStats15, true, requireSingletonBoundary, resourcePressureAlertIOWait15, thresholds):
		return resourcePressureSeverityResult{SeverityAlert, fmt.Sprintf("iowait 连续 15m 平均 %.1f%%", stats15.iowaitTotal/float64(stats15.count)), true, true}
	case cpuCandidatesEnabled && resourcePressureCandidateSupported(window30, stats30, boundaryStats30, true, requireSingletonBoundary, resourcePressureAlertSteal30, thresholds):
		return resourcePressureSeverityResult{SeverityAlert, fmt.Sprintf("steal 连续 30m 平均 %.1f%%", stats30.stealTotal/float64(stats30.count)), true, true}
	case cpuCandidatesEnabled && resourcePressureCandidateSupported(window15, stats15, boundaryStats15, true, requireSingletonBoundary, resourcePressureNoticeCPU15, thresholds):
		return resourcePressureSeverityResult{SeverityNotice, fmt.Sprintf("CPU 连续 15m 平均 %.1f%%", stats15.cpuUsageTotal/float64(stats15.count)), true, true}
	case resourcePressureCandidateSupported(window15, stats15, boundaryStats15, false, requireSingletonBoundary, resourcePressureNoticeLoad15, thresholds):
		return resourcePressureSeverityResult{SeverityNotice, fmt.Sprintf("归一化 Load5 连续 15m 平均 %.1f", stats15.loadTotal/float64(stats15.count)), true, false}
	case resourcePressureCandidateSupported(window15, stats15, boundaryStats15, false, requireSingletonBoundary, resourcePressureNoticeMemory15, thresholds):
		return resourcePressureSeverityResult{SeverityNotice, fmt.Sprintf("内存连续 15m 平均 %.1f%%", stats15.memTotal/float64(stats15.count)), true, false}
	case cpuCandidatesEnabled && resourcePressureCandidateSupported(window15, stats15, boundaryStats15, true, requireSingletonBoundary, resourcePressureNoticeIOWait15, thresholds):
		return resourcePressureSeverityResult{SeverityNotice, fmt.Sprintf("iowait 连续 15m 平均 %.1f%%", stats15.iowaitTotal/float64(stats15.count)), true, true}
	case resourcePressureCandidateSupported(window15, stats15, boundaryStats15, false, requireSingletonBoundary, resourcePressureNoticeSwap15, thresholds):
		return resourcePressureSeverityResult{SeverityNotice, fmt.Sprintf("swap 连续 15m 平均 %.1f%%", stats15.swapTotal/float64(stats15.count)), true, false}
	case cpuCandidatesEnabled && resourcePressureCandidateSupported(window15, stats15, boundaryStats15, true, requireSingletonBoundary, resourcePressureNoticeIOWaitFloor15, thresholds):
		return resourcePressureSeverityResult{SeverityNotice, fmt.Sprintf("iowait 连续 15m 平均 %.1f%%", stats15.iowaitTotal/float64(stats15.count)), true, true}
	case cpuCandidatesEnabled && resourcePressureCandidateSupported(window15, stats15, boundaryStats15, true, requireSingletonBoundary, resourcePressureNoticeSteal15, thresholds):
		return resourcePressureSeverityResult{SeverityNotice, fmt.Sprintf("steal 连续 15m 平均 %.1f%%", stats15.stealTotal/float64(stats15.count)), true, true}
	default:
		return resourcePressureSeverityResult{severity: SeverityNormal}
	}
}

func resourcePressureCandidateSupported(
	window resourcePressureWindow,
	stats, boundaryStats resourcePressureWindowStats,
	cpuRequired, requireSingletonBoundary bool,
	predicate resourcePressurePredicate,
	thresholds MetricThresholds,
) bool {
	if !window.covered || stats.count == 0 || (cpuRequired && !window.cpuUsable) ||
		!resourcePressurePredicateMatches(predicate, stats, thresholds) {
		return false
	}
	if !requireSingletonBoundary || !window.singleton {
		return true
	}
	if len(window.boundarySamples) == 0 || boundaryStats.count == 0 || (cpuRequired && !boundaryStats.cpuUsable) {
		return false
	}
	return resourcePressurePredicateMatches(predicate, boundaryStats, thresholds)
}

func probeFailureSeverity(failures []runtimefacts.ProbeObservation) (Severity, bool) {
	if len(failures) == 0 {
		return SeverityNormal, false
	}
	probeKind := failures[0].ProbeKind
	failureCount := len(failures)
	if probeKind == agentapi.ProbeKindTCP && uniqueNonEmptyMonitoringInstanceCount(failures) >= 2 {
		return SeverityCritical, true
	}
	if probeKind == agentapi.ProbeKindHTTP && uniqueNonEmptyProbeItemCount(failures) >= 2 {
		return SeverityCritical, true
	}
	severeThreshold := 5
	if probeKind == agentapi.ProbeKindTCP {
		severeThreshold = 6
	}
	switch {
	case failureCount >= severeThreshold:
		return SeverityCritical, true
	case failureCount >= 3:
		return SeverityAlert, true
	case failureCount >= 2:
		return SeverityNotice, true
	default:
		return SeverityNormal, false
	}
}

func tlsExpirySeverity(days int) (Severity, bool) {
	switch {
	case days <= 3:
		return SeverityCritical, true
	case days <= 14:
		return SeverityAlert, true
	case days <= 30:
		return SeverityNotice, true
	default:
		return SeverityNormal, false
	}
}

func consecutiveResults(observations []runtimefacts.ProbeObservation, want string) int {
	count := 0
	for _, observation := range observations {
		if observation.ResultKind != want {
			break
		}
		count++
	}
	return count
}

func leadingUnsuppressedFailureObservations(observations []runtimefacts.ProbeObservation) []runtimefacts.ProbeObservation {
	window := make([]runtimefacts.ProbeObservation, 0, len(observations))
	for _, observation := range observations {
		if observation.MaintenanceContext || observation.IsBackfilled {
			break
		}
		if observation.ResultKind != agentapi.ProbeResultFailure {
			break
		}
		window = append(window, observation)
	}
	return window
}

func uniqueNonEmptyMonitoringInstanceCount(observations []runtimefacts.ProbeObservation) int {
	seen := map[string]struct{}{}
	for _, observation := range observations {
		if observation.MonitoringInstanceID == "" {
			continue
		}
		seen[observation.MonitoringInstanceID] = struct{}{}
	}
	return len(seen)
}

func uniqueNonEmptyProbeItemCount(observations []runtimefacts.ProbeObservation) int {
	seen := map[string]struct{}{}
	for _, observation := range observations {
		if observation.ProbeItemID == "" {
			continue
		}
		seen[observation.ProbeItemID] = struct{}{}
	}
	return len(seen)
}

func unsuppressedMonitoringInstanceResourceSamples(samples []MonitoringInstanceResourceSample) []MonitoringInstanceResourceSample {
	filtered := make([]MonitoringInstanceResourceSample, 0, len(samples))
	for _, sample := range samples {
		if sample.MaintenanceContext || sample.IsBackfilled {
			continue
		}
		filtered = append(filtered, sample)
	}
	return filtered
}

func normalizeHostSamples(samples []runtimefacts.HostSample) []runtimefacts.HostSample {
	filtered := make([]runtimefacts.HostSample, 0, len(samples))
	for _, sample := range samples {
		filtered = append(filtered, sample)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].ObservedAt.Equal(filtered[j].ObservedAt) {
			return filtered[i].ObservedAt.After(filtered[j].ObservedAt)
		}
		if filtered[i].IsBackfilled != filtered[j].IsBackfilled {
			return !filtered[i].IsBackfilled
		}
		if !filtered[i].ReceivedAt.Equal(filtered[j].ReceivedAt) {
			return filtered[i].ReceivedAt.After(filtered[j].ReceivedAt)
		}
		return false
	})
	return filtered
}

func normalizeProbeObservations(observations []runtimefacts.ProbeObservation) []runtimefacts.ProbeObservation {
	filtered := make([]runtimefacts.ProbeObservation, 0, len(observations))
	for _, observation := range observations {
		filtered = append(filtered, observation)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].ObservedAt.Equal(filtered[j].ObservedAt) {
			return filtered[i].ObservedAt.After(filtered[j].ObservedAt)
		}
		if filtered[i].IsBackfilled != filtered[j].IsBackfilled {
			return !filtered[i].IsBackfilled
		}
		if !filtered[i].ReceivedAt.Equal(filtered[j].ReceivedAt) {
			return filtered[i].ReceivedAt.After(filtered[j].ReceivedAt)
		}
		return false
	})
	return filtered
}

type resourcePressureWindow struct {
	samples         []MonitoringInstanceResourceSample
	boundarySamples []MonitoringInstanceResourceSample
	covered         bool
	cpuUsable       bool
	singleton       bool
	allowSuppressed bool
}

func validResourcePressurePolicy(policy ResourcePressurePolicy) bool {
	if policy.EvaluatedAt.IsZero() {
		return false
	}
	switch policy.SampleInterval {
	case 5 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, 6 * time.Hour:
		return true
	default:
		return false
	}
}

func resourcePressureLatestSuppressed(samples []MonitoringInstanceResourceSample) bool {
	if len(samples) == 0 {
		return false
	}
	latestObservedAt := samples[0].ObservedAt
	for _, sample := range samples {
		if !sample.ObservedAt.Equal(latestObservedAt) {
			break
		}
		if !sample.MaintenanceContext && !sample.IsBackfilled {
			return false
		}
	}
	return true
}

func buildResourcePressureWindow(samples []MonitoringInstanceResourceSample, reference time.Time, width time.Duration, policy ResourcePressurePolicy, allowSuppressed bool) resourcePressureWindow {
	return buildResourcePressureWindowSorted(normalizeMonitoringInstanceResourceSamples(samples), reference, width, policy, allowSuppressed)
}

func buildResourcePressureWindowSorted(samples []MonitoringInstanceResourceSample, reference time.Time, width time.Duration, policy ResourcePressurePolicy, allowSuppressed bool) resourcePressureWindow {
	window := resourcePressureWindow{allowSuppressed: allowSuppressed}
	if width <= 0 || !validResourcePressurePolicy(policy) {
		return window
	}

	leftBoundary := reference.Add(-width)
	windowStart := -1
	windowEnd := -1
	boundaryStart := -1
	boundaryEnd := -1
	exactBoundary := false
	for index, sample := range samples {
		if sample.ObservedAt.After(reference) {
			continue
		}
		if sample.ObservedAt.Before(leftBoundary) {
			if windowStart >= 0 {
				boundaryStart = index
				boundaryEnd = index + 1
				for boundaryEnd < len(samples) && samples[boundaryEnd].ObservedAt.Equal(sample.ObservedAt) {
					boundaryEnd++
				}
			}
			break
		}
		if windowStart < 0 {
			windowStart = index
		}
		windowEnd = index + 1
		if sample.ObservedAt.Equal(leftBoundary) {
			exactBoundary = true
		}
	}
	if windowStart < 0 {
		return window
	}
	window.samples = samples[windowStart:windowEnd]
	if !exactBoundary && boundaryStart >= 0 {
		window.boundarySamples = samples[boundaryStart:boundaryEnd]
	}

	if policy.SampleInterval <= 0 || policy.SampleInterval > width {
		return window
	}
	gapLimit := 2*policy.SampleInterval + 10*time.Millisecond
	age := policy.EvaluatedAt.Sub(reference)
	if age < 0 || age > gapLimit {
		return window
	}

	distinctWindowTimes := resourcePressureDistinctEffectiveTimes(window.samples, allowSuppressed)
	window.singleton = distinctWindowTimes == 1
	if distinctWindowTimes == 0 {
		return window
	}

	coverageOK, cpuOK, distinctTimes, oldest := resourcePressureCoverage(
		window.samples,
		window.boundarySamples,
		allowSuppressed,
		gapLimit,
	)
	if !coverageOK || distinctTimes < 2 || oldest.After(leftBoundary) {
		return window
	}
	window.covered = true
	window.cpuUsable = cpuOK
	return window
}

func resourcePressureDistinctEffectiveTimes(samples []MonitoringInstanceResourceSample, allowSuppressed bool) int {
	distinct := 0
	for index := 0; index < len(samples); {
		end, include := resourcePressureEffectiveGroup(samples, index, allowSuppressed)
		if include {
			distinct++
		}
		index = end
	}
	return distinct
}

func resourcePressureEffectiveGroup(samples []MonitoringInstanceResourceSample, start int, allowSuppressed bool) (int, bool) {
	end := start + 1
	for end < len(samples) && samples[end].ObservedAt.Equal(samples[start].ObservedAt) {
		end++
	}
	if allowSuppressed {
		return end, true
	}
	for index := start; index < end; index++ {
		if !samples[index].MaintenanceContext && !samples[index].IsBackfilled {
			return end, true
		}
	}
	return end, false
}

func resourcePressureCoverage(
	samples []MonitoringInstanceResourceSample,
	boundarySamples []MonitoringInstanceResourceSample,
	allowSuppressed bool,
	gapLimit time.Duration,
) (bool, bool, int, time.Time) {
	if len(samples) == 0 {
		return false, false, 0, time.Time{}
	}
	cpuOK := true
	distinct := 0
	var previousObservedAt time.Time
	var oldest time.Time
	havePrevious := false
	for _, group := range [2][]MonitoringInstanceResourceSample{samples, boundarySamples} {
		for index := 0; index < len(group); {
			end, include := resourcePressureEffectiveGroup(group, index, allowSuppressed)
			if !include {
				return false, false, distinct, oldest
			}
			observedAt := group[index].ObservedAt
			if havePrevious && previousObservedAt.Sub(observedAt) > gapLimit {
				return false, false, distinct, oldest
			}
			previousObservedAt = observedAt
			havePrevious = true
			distinct++
			oldest = observedAt
			for sampleIndex := index; sampleIndex < end; sampleIndex++ {
				sample := group[sampleIndex]
				if !allowSuppressed && (sample.MaintenanceContext || sample.IsBackfilled) {
					continue
				}
				if !agentapi.CPURatesUsable(sample.CPURatesValid, sample.CPUUsagePct, sample.CPUIOWaitPct, sample.CPUStealPct) {
					cpuOK = false
				}
			}
			index = end
		}
	}
	return true, cpuOK, distinct, oldest
}

func resourcePressureWindowStatistics(window resourcePressureWindow) resourcePressureWindowStats {
	return resourcePressureSamplesStatistics(window.samples, window.allowSuppressed)
}

func resourcePressureWindowBoundaryStatistics(window resourcePressureWindow) resourcePressureWindowStats {
	return resourcePressureSamplesStatistics(window.boundarySamples, window.allowSuppressed)
}

func resourcePressureSamplesStatistics(samples []MonitoringInstanceResourceSample, allowSuppressed bool) resourcePressureWindowStats {
	stats := resourcePressureWindowStats{
		cpuUsable:       true,
		memAvailableMin: math.Inf(1),
	}
	for index := 0; index < len(samples); {
		end, include := resourcePressureEffectiveGroup(samples, index, allowSuppressed)
		if !include {
			index = end
			continue
		}
		for sampleIndex := index; sampleIndex < end; sampleIndex++ {
			sample := samples[sampleIndex]
			if !allowSuppressed && (sample.MaintenanceContext || sample.IsBackfilled) {
				continue
			}
			stats.count++
			stats.cpuUsageTotal += sample.CPUUsagePct
			stats.loadTotal += sample.NormalizedLoad5
			stats.memTotal += sample.MemUsedPct
			stats.swapTotal += sample.SwapUsedPct
			stats.iowaitTotal += sample.CPUIOWaitPct
			stats.stealTotal += sample.CPUStealPct
			if available := float64(sample.MemAvailableBytes); available < stats.memAvailableMin {
				stats.memAvailableMin = available
			}
			if !agentapi.CPURatesUsable(sample.CPURatesValid, sample.CPUUsagePct, sample.CPUIOWaitPct, sample.CPUStealPct) {
				stats.cpuUsable = false
			}
		}
		index = end
	}
	if stats.count == 0 {
		stats.cpuUsable = false
		stats.memAvailableMin = 0
	}
	return stats
}

func resourcePressureBoundarySeverity(window15, window30 resourcePressureWindow, thresholds MetricThresholds, include30 bool) resourcePressureSeverityResult {
	var boundaryWindow15, boundaryWindow30 resourcePressureWindow
	var boundaryStats15, boundaryStats30 resourcePressureWindowStats
	if window15.covered && window15.singleton && len(window15.boundarySamples) > 0 {
		boundaryStats15 = resourcePressureWindowBoundaryStatistics(window15)
		if boundaryStats15.count > 0 {
			boundaryWindow15 = resourcePressureWindow{covered: true, cpuUsable: boundaryStats15.cpuUsable}
		}
	}
	if include30 && window30.covered && window30.singleton && len(window30.boundarySamples) > 0 {
		boundaryStats30 = resourcePressureWindowBoundaryStatistics(window30)
		if boundaryStats30.count > 0 {
			boundaryWindow30 = resourcePressureWindow{covered: true, cpuUsable: boundaryStats30.cpuUsable}
		}
	}
	return resourcePressureSeverityFromStats(
		boundaryWindow15, boundaryStats15, resourcePressureWindowStats{},
		boundaryWindow30, boundaryStats30, resourcePressureWindowStats{},
		thresholds, false, true,
	)
}

func resourcePressureRecoveryHasPressure(window15, window30 resourcePressureWindow, thresholds MetricThresholds, include30 bool) bool {
	stats15 := resourcePressureWindowStatistics(window15)
	boundaryStats15 := resourcePressureWindowBoundaryStatistics(window15)
	if include30 {
		stats30 := resourcePressureWindowStatistics(window30)
		current := resourcePressureSeverityFromStats(window15, stats15, resourcePressureWindowStats{}, window30, stats30, resourcePressureWindowStats{}, thresholds, false, true)
		if current.active {
			return true
		}
		boundary := resourcePressureBoundarySeverity(window15, window30, thresholds, true)
		return boundary.active
	}
	var emptyWindow resourcePressureWindow
	current := resourcePressureSeverityFromStats(window15, stats15, resourcePressureWindowStats{}, emptyWindow, resourcePressureWindowStats{}, resourcePressureWindowStats{}, thresholds, false, true)
	if current.active {
		return true
	}
	if !window15.covered || !window15.singleton || len(window15.boundarySamples) == 0 || boundaryStats15.count == 0 {
		return false
	}
	boundaryWindow := resourcePressureWindow{covered: true, cpuUsable: boundaryStats15.cpuUsable}
	boundary := resourcePressureSeverityFromStats(boundaryWindow, boundaryStats15, resourcePressureWindowStats{}, emptyWindow, resourcePressureWindowStats{}, resourcePressureWindowStats{}, thresholds, false, true)
	return boundary.active
}

func resourcePressureBoundarySeverityHigher(window15, window30 resourcePressureWindow, thresholds MetricThresholds, current Severity, include30 bool) bool {
	boundary := resourcePressureBoundarySeverity(window15, window30, thresholds, include30)
	return boundary.active && severityRank(boundary.severity) > severityRank(current)
}

func averageMonitoringInstanceResourceMetric(samples []MonitoringInstanceResourceSample, selector func(MonitoringInstanceResourceSample) float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	total := 0.0
	for _, sample := range samples {
		total += selector(sample)
	}
	return total / float64(len(samples))
}

func minimumMonitoringInstanceResourceMetric(samples []MonitoringInstanceResourceSample, selector func(MonitoringInstanceResourceSample) float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	minimum := selector(samples[0])
	for _, sample := range samples[1:] {
		value := selector(sample)
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}

func weightedMonitoringInstanceTrendBaselines(baselines []MonitoringInstanceHostDailyAggregate) (load, iowait, steal *float64) {
	var loadWeight, loadTotal float64
	var iowaitWeight, iowaitTotal float64
	var stealWeight, stealTotal float64
	for _, baseline := range baselines {
		hostWeight := dailyEffectiveWeight(
			nonNegativeDailyCount(baseline.SampleCount),
			nonNegativeDailyCount(baseline.BackfilledSampleCount),
			nonNegativeDailyCount(baseline.MaintenanceSampleCount),
		)
		if hostWeight > 0 && finiteFloat64(baseline.AvgLoad5) {
			weight := float64(hostWeight)
			loadWeight += weight
			loadTotal += baseline.AvgLoad5 * weight
		}

		cpuValidWeight := dailyEffectiveWeight(
			dailyCountValue(baseline.CPUValidSampleCount, baseline.SampleCount),
			dailyCountValue(baseline.CPUValidBackfilledSampleCount, baseline.BackfilledSampleCount),
			dailyCountValue(baseline.CPUValidMaintenanceSampleCount, baseline.MaintenanceSampleCount),
		)
		if cpuValidWeight <= 0 {
			continue
		}
		weight := float64(cpuValidWeight)
		if baseline.AvgCPUIOWaitPct != nil && usableTrendCPUAverage(*baseline.AvgCPUIOWaitPct) {
			iowaitWeight += weight
			iowaitTotal += *baseline.AvgCPUIOWaitPct * weight
		}
		if baseline.AvgCPUStealPct != nil && usableTrendCPUAverage(*baseline.AvgCPUStealPct) {
			stealWeight += weight
			stealTotal += *baseline.AvgCPUStealPct * weight
		}
	}
	if loadWeight > 0 {
		value := loadTotal / loadWeight
		load = &value
	}
	if iowaitWeight > 0 {
		value := iowaitTotal / iowaitWeight
		iowait = &value
	}
	if stealWeight > 0 {
		value := stealTotal / stealWeight
		steal = &value
	}
	return
}

func nonNegativeDailyCount(value int) int64 {
	if value <= 0 {
		return 0
	}
	return int64(value)
}

func dailyCountValue(value *int, fallback int) int64 {
	if value == nil {
		return nonNegativeDailyCount(fallback)
	}
	return nonNegativeDailyCount(*value)
}

func dailyEffectiveWeight(total, backfilled, maintenance int64) int64 {
	if total <= 0 || backfilled >= total {
		return 0
	}
	remaining := total - backfilled
	if maintenance >= remaining {
		return 0
	}
	return remaining - maintenance
}

func finiteFloat64(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func usableTrendCPUAverage(value float64) bool {
	return finiteFloat64(value) && value >= 0 && value <= 100
}

func nodeTrendMetricDegraded(current, baseline, absoluteFloor, absoluteDelta float64) bool {
	if current < absoluteFloor {
		return false
	}
	guardBaseline := baseline
	if guardBaseline < 0.1 {
		guardBaseline = 0.1
	}
	if current < guardBaseline*1.8 {
		return false
	}
	return current >= baseline+absoluteDelta
}

func weightedTargetLatencyBaselines(baselines []TargetProbeDailyAggregate) map[string]float64 {
	type accumulator struct {
		total  float64
		weight float64
	}
	accumulators := make(map[string]accumulator)
	for _, baseline := range baselines {
		if baseline.AvgLatencyMS == nil || baseline.ProbeItemID == "" {
			continue
		}
		weight := float64(baseline.SuccessCount - baseline.BackfilledObservationCount - baseline.MaintenanceObservationCount)
		if weight <= 0 {
			continue
		}
		acc := accumulators[baseline.ProbeItemID]
		acc.total += (*baseline.AvgLatencyMS) * weight
		acc.weight += weight
		accumulators[baseline.ProbeItemID] = acc
	}
	weighted := make(map[string]float64, len(accumulators))
	for probeItemID, acc := range accumulators {
		if acc.weight == 0 {
			continue
		}
		weighted[probeItemID] = acc.total / acc.weight
	}
	return weighted
}

func degradedTargetLatencyMonitoringInstances(observations []runtimefacts.ProbeObservation, baselineAvg float64) []string {
	byMonitoringInstance := map[string][]runtimefacts.ProbeObservation{}
	monitoringInstanceIDs := make([]string, 0)
	for _, observation := range observations {
		if observation.MonitoringInstanceID == "" {
			continue
		}
		if _, ok := byMonitoringInstance[observation.MonitoringInstanceID]; !ok {
			monitoringInstanceIDs = append(monitoringInstanceIDs, observation.MonitoringInstanceID)
		}
		byMonitoringInstance[observation.MonitoringInstanceID] = append(byMonitoringInstance[observation.MonitoringInstanceID], observation)
	}
	degraded := make([]string, 0, len(monitoringInstanceIDs))
	for _, monitoringInstanceID := range monitoringInstanceIDs {
		series := byMonitoringInstance[monitoringInstanceID]
		if len(series) < 3 {
			continue
		}
		if targetLatencySeriesDegraded(averageProbeLatencyMS(series), baselineAvg) {
			degraded = append(degraded, monitoringInstanceID)
		}
	}
	return degraded
}

func hasSustainedTargetLatencyRecoveryEvidence(series map[string][]runtimefacts.ProbeObservation, baselines map[string]float64, window time.Duration) bool {
	comparableSeries := 0
	for probeItemID, observations := range series {
		baseline, ok := baselines[probeItemID]
		if !ok {
			continue
		}
		comparableSeries++
		if len(observations) < 3 {
			return false
		}
		if !spansProbeObservationWindow(observations, window) {
			return false
		}
		if targetLatencySeriesDegraded(averageProbeLatencyMS(observations), baseline) {
			return false
		}
	}
	return comparableSeries > 0
}

func usableLatencyObservationSeries(observations []runtimefacts.ProbeObservation) map[string][]runtimefacts.ProbeObservation {
	series := map[string][]runtimefacts.ProbeObservation{}
	for _, observation := range observations {
		if observation.MaintenanceContext || observation.IsBackfilled {
			continue
		}
		if observation.ResultKind != agentapi.ProbeResultSuccess || observation.LatencyMS == nil || observation.ProbeItemID == "" {
			continue
		}
		series[observation.ProbeItemID] = append(series[observation.ProbeItemID], observation)
	}
	return series
}

func averageProbeLatencyMS(observations []runtimefacts.ProbeObservation) float64 {
	if len(observations) == 0 {
		return 0
	}
	var total float64
	for _, observation := range observations {
		total += float64(*observation.LatencyMS)
	}
	return total / float64(len(observations))
}

func targetLatencySeriesDegraded(currentAvg, baselineAvg float64) bool {
	if baselineAvg <= 0 {
		return false
	}
	if currentAvg < baselineAvg*1.8 {
		return false
	}
	return currentAvg >= baselineAvg+100 || currentAvg >= 250
}

func joinMetricLabels(parts []string) string {
	sorted := append([]string(nil), parts...)
	sort.Strings(sorted)
	return strings.Join(sorted, "、")
}

func cloneIncident(record *IncidentRecord) *IncidentRecord {
	if record == nil {
		return nil
	}
	clone := *record
	return &clone
}

func spansMonitoringInstanceResourceWindow(samples []MonitoringInstanceResourceSample, window time.Duration) bool {
	if len(samples) == 0 {
		return false
	}
	newest := samples[0].ObservedAt
	oldest := samples[len(samples)-1].ObservedAt
	return newest.Sub(oldest) >= window
}
func cpuWindowUsable(samples []MonitoringInstanceResourceSample, window time.Duration) bool {
	if !spansMonitoringInstanceResourceWindow(samples, window) {
		return false
	}
	for _, sample := range samples {
		if !agentapi.CPURatesUsable(sample.CPURatesValid, sample.CPUUsagePct, sample.CPUIOWaitPct, sample.CPUStealPct) {
			return false
		}
	}
	return true
}

func spansProbeObservationWindow(observations []runtimefacts.ProbeObservation, window time.Duration) bool {
	if len(observations) == 0 {
		return false
	}
	newest := observations[0].ObservedAt
	oldest := observations[len(observations)-1].ObservedAt
	return newest.Sub(oldest) >= window
}

func normalizeMonitoringInstanceResourceSamples(samples []MonitoringInstanceResourceSample) []MonitoringInstanceResourceSample {
	filtered := make([]MonitoringInstanceResourceSample, 0, len(samples))
	for _, sample := range samples {
		filtered = append(filtered, sample)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left, right := filtered[i], filtered[j]
		if !left.ObservedAt.Equal(right.ObservedAt) {
			return left.ObservedAt.After(right.ObservedAt)
		}
		if left.IsBackfilled != right.IsBackfilled {
			return !left.IsBackfilled
		}
		if !left.ReceivedAt.Equal(right.ReceivedAt) {
			return left.ReceivedAt.After(right.ReceivedAt)
		}
		return false
	})
	return filtered
}

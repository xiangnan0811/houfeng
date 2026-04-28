package incidents

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/contracts/agentapi"
)

func EvaluateNodeHeartbeatMissing(previous *IncidentRecord, nodeID string, now time.Time, lastHeartbeatAt *time.Time, heartbeatInterval time.Duration) EvaluationResult {
	if lastHeartbeatAt == nil || heartbeatInterval <= 0 {
		return noop(previous)
	}
	missed := int(now.Sub(*lastHeartbeatAt) / heartbeatInterval)
	severity, summary, active := heartbeatSeverity(missed)
	if !active {
		return recoverIfNeeded(previous, now, "心跳已恢复")
	}
	return evaluateTransition(previous, ObjectTypeNode, nodeID, IncidentNodeHeartbeatMissing, severity, now, summary)
}

func EvaluateNodeDiskPressure(previous *IncidentRecord, nodeID string, sample *runtimefacts.HostSample) EvaluationResult {
	if sample == nil {
		return noop(previous)
	}
	suppressed := sample.MaintenanceContext || sample.IsBackfilled
	if suppressed {
		result := evaluateNodeDiskPressure(previous, nodeID, sample)
		if result.Transition == TransitionRecovered {
			return suppressNotification(result)
		}
		return skip(previous)
	}
	return evaluateNodeDiskPressure(previous, nodeID, sample)
}

func EvaluateNodeInodePressure(previous *IncidentRecord, nodeID string, sample *runtimefacts.HostSample) EvaluationResult {
	if sample == nil {
		return noop(previous)
	}
	suppressed := sample.MaintenanceContext || sample.IsBackfilled
	if suppressed {
		result := evaluateNodeInodePressure(previous, nodeID, sample)
		if result.Transition == TransitionRecovered {
			return suppressNotification(result)
		}
		return skip(previous)
	}
	return evaluateNodeInodePressure(previous, nodeID, sample)
}

func EvaluateNodeResourcePressure(previous *IncidentRecord, nodeID string, samples []NodeResourceSample) EvaluationResult {
	samples = normalizeNodeResourceSamples(samples)
	if len(samples) == 0 {
		return noop(previous)
	}
	suppressed := samples[0].MaintenanceContext || samples[0].IsBackfilled

	referenceTime := samples[0].ObservedAt
	activeSamples := unsuppressedNodeResourceSamples(samples)
	window15 := nodeResourceSamplesWithin(activeSamples, referenceTime, 15*time.Minute)
	window30 := nodeResourceSamplesWithin(activeSamples, referenceTime, 30*time.Minute)
	severity, summary, active := resourcePressureSeverity(window15, window30)
	if !active {
		recoveryWindow := 15 * time.Minute
		if previous != nil && previous.Severity == SeverityCritical {
			recoveryWindow = 30 * time.Minute
		}
		if previous != nil && !spansNodeResourceWindow(nodeResourceSamplesWithin(samples, referenceTime, recoveryWindow), recoveryWindow) {
			return noop(previous)
		}
		result := recoverIfNeeded(previous, referenceTime, "资源压力恢复到安全区间")
		if suppressed {
			return suppressNotification(result)
		}
		return result
	}
	if suppressed {
		return skip(previous)
	}
	return evaluateTransition(previous, ObjectTypeNode, nodeID, IncidentNodeResourcePressure, severity, referenceTime, summary)
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
		result := recoverIfNeeded(previous, recent[0].ObservedAt, "探针已连续成功恢复")
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
	return evaluateTransition(previous, ObjectTypeTarget, targetID, IncidentTargetProbeFailure, severity, recent[0].ObservedAt, summary)
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
		result := recoverIfNeeded(previous, recent[0].ObservedAt, "TLS 到期风险解除")
		if suppressed {
			return suppressNotification(result)
		}
		return result
	}
	if suppressed {
		return skip(previous)
	}
	summary := fmt.Sprintf("TLS 证书剩余 %d 天", *recent[0].TLSExpiryDays)
	return evaluateTransition(previous, ObjectTypeTarget, targetID, IncidentTargetTLSExpiry, severity, recent[0].ObservedAt, summary)
}

func EvaluateNodeTrendDegradation(previous *IncidentRecord, nodeID string, samples []NodeResourceSample, baselines []NodeHostDailyAggregate) EvaluationResult {
	samples = normalizeNodeResourceSamples(samples)
	if len(samples) == 0 {
		return noop(previous)
	}
	usableCurrent := unsuppressedNodeResourceSamples(samples)
	usableBaselines := usableNodeHostDailyAggregates(baselines)
	if len(usableCurrent) < 3 || len(usableBaselines) == 0 || !spansNodeResourceWindow(usableCurrent, time.Second) {
		if previous != nil {
			return noop(previous)
		}
		if hasSuppressedNodeTrendSamples(samples) {
			return skip(previous)
		}
		return EvaluationResult{Transition: TransitionNoop}
	}

	referenceTime := usableCurrent[0].ObservedAt
	loadBaseline, iowaitBaseline, stealBaseline := weightedNodeTrendBaselines(usableBaselines)
	loadCurrent := averageNodeResourceMetric(usableCurrent, func(sample NodeResourceSample) float64 { return sample.NormalizedLoad5 })
	iowaitCurrent := averageNodeResourceMetric(usableCurrent, func(sample NodeResourceSample) float64 { return sample.CPUIOWaitPct })
	stealCurrent := averageNodeResourceMetric(usableCurrent, func(sample NodeResourceSample) float64 { return sample.CPUStealPct })

	degradedMetrics := make([]string, 0, 3)
	if nodeTrendMetricDegraded(loadCurrent, loadBaseline, 1.6, 0.6) {
		degradedMetrics = append(degradedMetrics, "load5")
	}
	if nodeTrendMetricDegraded(iowaitCurrent, iowaitBaseline, 8, 4) {
		degradedMetrics = append(degradedMetrics, "iowait")
	}
	if nodeTrendMetricDegraded(stealCurrent, stealBaseline, 3, 2) {
		degradedMetrics = append(degradedMetrics, "steal")
	}
	if len(degradedMetrics) == 0 {
		return recoverIfNeeded(previous, referenceTime, "节点趋势已恢复到安全区间")
	}

	severity := SeverityNotice
	if len(degradedMetrics) >= 2 {
		severity = SeverityAlert
	}
	return evaluateTransition(previous, ObjectTypeNode, nodeID, IncidentNodeTrendDegradation, severity, referenceTime, fmt.Sprintf("节点趋势劣化：%s", joinMetricLabels(degradedMetrics)))
}

func EvaluateTargetLatencyTrendDegradationAcrossSeries(previous *IncidentRecord, targetID string, observations []runtimefacts.ProbeObservation, baselines []TargetProbeDailyAggregate) EvaluationResult {
	observations = normalizeProbeObservations(observations)
	if len(observations) == 0 {
		return noop(previous)
	}
	usableBaselines := weightedTargetLatencyBaselines(baselines)
	series := usableLatencyObservationSeries(observations)
	if len(usableBaselines) == 0 || len(series) == 0 {
		if previous != nil {
			return noop(previous)
		}
		if hasSuppressedLatencyTrendObservations(observations) {
			return skip(previous)
		}
		return EvaluationResult{Transition: TransitionNoop}
	}

	referenceTime := observations[0].ObservedAt
	degradedProbeItems := make([]string, 0)
	degradedNodes := map[string]struct{}{}
	comparableSeries := 0
	for probeItemID, currentSeries := range series {
		baseline, ok := usableBaselines[probeItemID]
		if !ok || len(currentSeries) < 3 {
			continue
		}
		comparableSeries++
		currentAvg := averageProbeLatencyMS(currentSeries)
		if !targetLatencySeriesDegraded(currentAvg, baseline) {
			continue
		}
		degradedProbeItems = append(degradedProbeItems, probeItemID)
		for _, observation := range currentSeries {
			if observation.NodeID == "" {
				continue
			}
			degradedNodes[observation.NodeID] = struct{}{}
		}
	}
	if comparableSeries == 0 {
		return noop(previous)
	}
	if len(degradedProbeItems) == 0 {
		return recoverIfNeeded(previous, referenceTime, "目标延迟趋势已恢复到安全区间")
	}

	severity := SeverityNotice
	if len(degradedProbeItems) >= 2 || len(degradedNodes) >= 2 {
		severity = SeverityAlert
	}
	sort.Strings(degradedProbeItems)
	return evaluateTransition(previous, ObjectTypeTarget, targetID, IncidentTargetLatencyTrendDegradation, severity, referenceTime, fmt.Sprintf("目标延迟趋势劣化：%s", joinMetricLabels(degradedProbeItems)))
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

func evaluateNodeDiskPressure(previous *IncidentRecord, nodeID string, sample *runtimefacts.HostSample) EvaluationResult {
	severity, active := fastThresholdSeverity(sample.DiskUsedPct, 85, 92, 97)
	if !active {
		return recoverIfNeeded(previous, sample.ObservedAt, "磁盘使用率恢复到安全区间")
	}
	summary := fmt.Sprintf("磁盘使用率 %.1f%%", sample.DiskUsedPct)
	return evaluateTransition(previous, ObjectTypeNode, nodeID, IncidentNodeDiskPressure, severity, sample.ObservedAt, summary)
}

func evaluateNodeInodePressure(previous *IncidentRecord, nodeID string, sample *runtimefacts.HostSample) EvaluationResult {
	severity, active := fastThresholdSeverity(sample.InodeUsedPct, 80, 90, 95)
	if !active {
		return recoverIfNeeded(previous, sample.ObservedAt, "inode 使用率恢复到安全区间")
	}
	summary := fmt.Sprintf("inode 使用率 %.1f%%", sample.InodeUsedPct)
	return evaluateTransition(previous, ObjectTypeNode, nodeID, IncidentNodeInodePressure, severity, sample.ObservedAt, summary)
}

func recoverIfNeeded(previous *IncidentRecord, when time.Time, summary string) EvaluationResult {
	if previous == nil {
		return EvaluationResult{Transition: TransitionNoop}
	}
	if when.IsZero() {
		when = previous.LastEvaluatedAt
	}
	return EvaluationResult{
		Transition: TransitionRecovered,
		Event: &StateChangeEventRecord{
			IncidentID:    previous.IncidentID,
			IncidentClass: previous.IncidentClass,
			ObjectType:    previous.ObjectType,
			ObjectID:      previous.ObjectID,
			EventType:     EventIncidentRecovered,
			Severity:      previous.Severity,
			Summary:       summary,
			CreatedAt:     when,
		},
		Notification: &NotificationDecision{
			ShouldSend: true,
			Channel:    "telegram",
			Reason:     NotificationReasonRecovered,
			Severity:   previous.Severity,
			Summary:    summary,
		},
	}
}

func evaluateTransition(previous *IncidentRecord, objectType ObjectType, objectID string, class IncidentClass, severity Severity, when time.Time, summary string) EvaluationResult {
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
			Event:        &StateChangeEventRecord{IncidentID: current.IncidentID, IncidentClass: class, ObjectType: objectType, ObjectID: objectID, EventType: EventIncidentStarted, Severity: severity, Summary: summary, CreatedAt: when},
			Notification: &NotificationDecision{ShouldSend: true, Channel: "telegram", Reason: NotificationReasonStarted, Severity: severity, Summary: summary},
		}
	}
	current.StartedAt = previous.StartedAt
	if severityRank(severity) > severityRank(previous.Severity) {
		return EvaluationResult{
			Current:      current,
			Transition:   TransitionEscalated,
			Event:        &StateChangeEventRecord{IncidentID: current.IncidentID, IncidentClass: class, ObjectType: objectType, ObjectID: objectID, EventType: EventIncidentEscalated, Severity: severity, Summary: summary, CreatedAt: when},
			Notification: &NotificationDecision{ShouldSend: true, Channel: "telegram", Reason: NotificationReasonEscalated, Severity: severity, Summary: summary},
		}
	}
	return EvaluationResult{Current: current, Transition: TransitionNoop}
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

func heartbeatSeverity(missed int) (Severity, string, bool) {
	switch {
	case missed >= 5:
		return SeverityCritical, fmt.Sprintf("最近 %d 个心跳周期未收到心跳", missed), true
	case missed >= 3:
		return SeverityAlert, fmt.Sprintf("最近 %d 个心跳周期未收到心跳", missed), true
	case missed >= 2:
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

func resourcePressureSeverity(window15, window30 []NodeResourceSample) (Severity, string, bool) {
	if len(window15) == 0 {
		return SeverityNormal, "", false
	}
	has15m := spansNodeResourceWindow(window15, 15*time.Minute)
	has30m := spansNodeResourceWindow(window30, 30*time.Minute)

	avg15CPU := averageNodeResourceMetric(window15, func(sample NodeResourceSample) float64 { return sample.CPUUsagePct })
	avg15Load := averageNodeResourceMetric(window15, func(sample NodeResourceSample) float64 { return sample.NormalizedLoad5 })
	avg15Mem := averageNodeResourceMetric(window15, func(sample NodeResourceSample) float64 { return sample.MemUsedPct })
	avg15Swap := averageNodeResourceMetric(window15, func(sample NodeResourceSample) float64 { return sample.SwapUsedPct })
	avg15Iowait := averageNodeResourceMetric(window15, func(sample NodeResourceSample) float64 { return sample.CPUIOWaitPct })
	avg15Steal := averageNodeResourceMetric(window15, func(sample NodeResourceSample) float64 { return sample.CPUStealPct })
	avg30CPU := averageNodeResourceMetric(window30, func(sample NodeResourceSample) float64 { return sample.CPUUsagePct })
	avg30Load := averageNodeResourceMetric(window30, func(sample NodeResourceSample) float64 { return sample.NormalizedLoad5 })
	avg30Mem := averageNodeResourceMetric(window30, func(sample NodeResourceSample) float64 { return sample.MemUsedPct })
	avg30Iowait := averageNodeResourceMetric(window30, func(sample NodeResourceSample) float64 { return sample.CPUIOWaitPct })
	avg30Steal := averageNodeResourceMetric(window30, func(sample NodeResourceSample) float64 { return sample.CPUStealPct })
	min30MemAvailable := minimumNodeResourceMetric(window30, func(sample NodeResourceSample) float64 { return float64(sample.MemAvailableBytes) })

	switch {
	case has30m && avg30CPU >= 95:
		return SeverityCritical, fmt.Sprintf("CPU 连续 30m 平均 %.1f%%", avg30CPU), true
	case has30m && avg30Load >= 2.5:
		return SeverityCritical, fmt.Sprintf("归一化 Load5 连续 30m 平均 %.1f", avg30Load), true
	case has30m && avg30Mem >= 95 && min30MemAvailable <= 512*1024*1024:
		return SeverityCritical, fmt.Sprintf("内存连续 30m 平均 %.1f%%，可用内存持续偏低", avg30Mem), true
	case has15m && avg15CPU >= 90:
		return SeverityAlert, fmt.Sprintf("CPU 连续 15m 平均 %.1f%%", avg15CPU), true
	case has15m && avg15Load >= 1.8:
		return SeverityAlert, fmt.Sprintf("归一化 Load5 连续 15m 平均 %.1f", avg15Load), true
	case has15m && avg15Mem >= 92:
		return SeverityAlert, fmt.Sprintf("内存连续 15m 平均 %.1f%%", avg15Mem), true
	case has30m && avg30Iowait >= 20:
		return SeverityAlert, fmt.Sprintf("iowait 连续 30m 平均 %.1f%%", avg30Iowait), true
	case has30m && avg30Steal >= 10:
		return SeverityAlert, fmt.Sprintf("steal 连续 30m 平均 %.1f%%", avg30Steal), true
	case has15m && avg15CPU >= 80:
		return SeverityNotice, fmt.Sprintf("CPU 连续 15m 平均 %.1f%%", avg15CPU), true
	case has15m && avg15Load >= 1.2:
		return SeverityNotice, fmt.Sprintf("归一化 Load5 连续 15m 平均 %.1f", avg15Load), true
	case has15m && avg15Mem >= 85:
		return SeverityNotice, fmt.Sprintf("内存连续 15m 平均 %.1f%%", avg15Mem), true
	case has15m && avg15Swap > 10:
		return SeverityNotice, fmt.Sprintf("swap 连续 15m 平均 %.1f%%", avg15Swap), true
	case has15m && avg15Iowait >= 10:
		return SeverityNotice, fmt.Sprintf("iowait 连续 15m 平均 %.1f%%", avg15Iowait), true
	case has15m && avg15Steal >= 5:
		return SeverityNotice, fmt.Sprintf("steal 连续 15m 平均 %.1f%%", avg15Steal), true
	default:
		return SeverityNormal, "", false
	}
}

func probeFailureSeverity(failures []runtimefacts.ProbeObservation) (Severity, bool) {
	if len(failures) == 0 {
		return SeverityNormal, false
	}
	probeKind := failures[0].ProbeKind
	failureCount := len(failures)
	if probeKind == agentapi.ProbeKindTCP && uniqueNonEmptyNodeCount(failures) >= 2 {
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

func uniqueNonEmptyNodeCount(observations []runtimefacts.ProbeObservation) int {
	seen := map[string]struct{}{}
	for _, observation := range observations {
		if observation.NodeID == "" {
			continue
		}
		seen[observation.NodeID] = struct{}{}
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

func unsuppressedNodeResourceSamples(samples []NodeResourceSample) []NodeResourceSample {
	filtered := make([]NodeResourceSample, 0, len(samples))
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
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ObservedAt.After(filtered[j].ObservedAt)
	})
	return filtered
}

func normalizeProbeObservations(observations []runtimefacts.ProbeObservation) []runtimefacts.ProbeObservation {
	filtered := make([]runtimefacts.ProbeObservation, 0, len(observations))
	for _, observation := range observations {
		filtered = append(filtered, observation)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ObservedAt.After(filtered[j].ObservedAt)
	})
	return filtered
}

func nodeResourceSamplesWithin(samples []NodeResourceSample, reference time.Time, window time.Duration) []NodeResourceSample {
	out := make([]NodeResourceSample, 0, len(samples))
	for _, sample := range samples {
		if reference.Sub(sample.ObservedAt) <= window {
			out = append(out, sample)
		}
	}
	return out
}

func averageNodeResourceMetric(samples []NodeResourceSample, selector func(NodeResourceSample) float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	total := 0.0
	for _, sample := range samples {
		total += selector(sample)
	}
	return total / float64(len(samples))
}

func minimumNodeResourceMetric(samples []NodeResourceSample, selector func(NodeResourceSample) float64) float64 {
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

func usableNodeHostDailyAggregates(baselines []NodeHostDailyAggregate) []NodeHostDailyAggregate {
	usable := make([]NodeHostDailyAggregate, 0, len(baselines))
	for _, baseline := range baselines {
		usableSamples := baseline.SampleCount - baseline.BackfilledSampleCount - baseline.MaintenanceSampleCount
		if usableSamples <= 0 {
			continue
		}
		usable = append(usable, baseline)
	}
	return usable
}

func weightedNodeTrendBaselines(baselines []NodeHostDailyAggregate) (float64, float64, float64) {
	var totalWeight float64
	var loadTotal float64
	var iowaitTotal float64
	var stealTotal float64
	for _, baseline := range baselines {
		weight := float64(baseline.SampleCount - baseline.BackfilledSampleCount - baseline.MaintenanceSampleCount)
		if weight <= 0 {
			continue
		}
		totalWeight += weight
		loadTotal += baseline.AvgLoad5 * weight
		iowaitTotal += baseline.AvgCPUIOWaitPct * weight
		stealTotal += baseline.AvgCPUStealPct * weight
	}
	if totalWeight == 0 {
		return 0, 0, 0
	}
	return loadTotal / totalWeight, iowaitTotal / totalWeight, stealTotal / totalWeight
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

func hasSuppressedNodeTrendSamples(samples []NodeResourceSample) bool {
	for _, sample := range samples {
		if sample.MaintenanceContext || sample.IsBackfilled {
			return true
		}
	}
	return false
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

func hasSuppressedLatencyTrendObservations(observations []runtimefacts.ProbeObservation) bool {
	for _, observation := range observations {
		if observation.MaintenanceContext || observation.IsBackfilled {
			return true
		}
	}
	return false
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

func spansNodeResourceWindow(samples []NodeResourceSample, window time.Duration) bool {
	if len(samples) == 0 {
		return false
	}
	newest := samples[0].ObservedAt
	oldest := samples[len(samples)-1].ObservedAt
	return newest.Sub(oldest) >= window
}

func normalizeNodeResourceSamples(samples []NodeResourceSample) []NodeResourceSample {
	filtered := make([]NodeResourceSample, 0, len(samples))
	for _, sample := range samples {
		filtered = append(filtered, sample)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ObservedAt.After(filtered[j].ObservedAt)
	})
	return filtered
}

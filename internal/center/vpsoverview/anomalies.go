package vpsoverview

import (
	"net/url"
	"sort"
	"strings"
	"time"
)

// Rule IDs are stable contracts for the web. Renaming one is a breaking change.
const (
	RuleMonitoringHealthAbnormal   = "monitoring.health.abnormal.v1"
	RuleMonitoringIncidentsOpen    = "monitoring.incidents.open.v1"
	RuleMonitoringUnlinked         = "monitoring.unlinked.v1"
	RuleIPQualityRiskElevated      = "ip_quality.risk.elevated.v1"
	RuleIPQualityStale             = "ip_quality.stale.v1"
	RuleIPQualityPartial           = "ip_quality.partial.v1"
	RuleIPQualityMissing           = "ip_quality.missing.v1"
	RuleRenewalDueSoon             = "renewal.due.soon.v1"
	RuleRenewalOverdue             = "renewal.overdue.v1"
	RuleRenewalSubscriptionMissing = "renewal.subscription.missing.v1"
	RuleLifecycleBlocker           = "lifecycle.blocker.v1"
	RuleSourceUnavailable          = "source.unavailable.v1"
)

const renewalDueWindowDays = 14

// Snapshot is the fact bag anomaly rules evaluate. Sections that timed out or
// failed are marked unavailable so rules can emit a single judgement-affecting
// source anomaly without inventing placeholder health rows.
type Snapshot struct {
	GeneratedAt time.Time
	VPSID       string
	Identity    Identity

	MonitoringAvailable  bool
	MonitoringInstanceID string
	MonitoringHealth     string
	MonitoringStatus     string
	MonitoringDetail     string
	ActiveIncidents      int
	MonitoringObserved   *time.Time

	IPAvailable  bool
	IPStatus     string
	IPRiskLevel  string
	IPStale      bool
	IPObservedAt *time.Time

	SubscriptionAvailable bool
	ActiveSubscriptions   int
	NextRenewAt           *time.Time
	RenewalDecision       string
	LifecycleStatus       string

	JudgementSourcesUnavailable []string
}

// EvaluateAnomalies returns only rule hits. Healthy snapshots yield a non-nil
// empty slice — never a placeholder row.
func EvaluateAnomalies(snapshot Snapshot) []Anomaly {
	anomalies := make([]Anomaly, 0)
	ipQualityRoute := "/vps/" + url.PathEscape(snapshot.VPSID) + "/ip-quality"

	if snapshot.MonitoringAvailable {
		status := strings.ToLower(strings.TrimSpace(snapshot.MonitoringStatus))
		if status == "unlinked" {
			anomalies = append(anomalies, Anomaly{
				RuleID:   RuleMonitoringUnlinked,
				Severity: SeverityNotice,
				Title:    "未关联监控实例",
				Detail:   firstNonEmpty(snapshot.MonitoringDetail, "缺少监控证据"),
				Source:   "monitoring",
				Primary: &AnomalyAction{
					ID: "open_monitoring_instances", Label: "关联监控",
				},
			})
		} else {
			health := strings.TrimSpace(snapshot.MonitoringHealth)
			if health != "" && health != "正常" {
				anomalies = append(anomalies, Anomaly{
					RuleID:   RuleMonitoringHealthAbnormal,
					Severity: healthSeverity(health),
					Title:    "监控健康异常",
					Detail:   firstNonEmpty(snapshot.MonitoringDetail, health),
					Source:   "monitoring",
					EventAt:  snapshot.MonitoringObserved,
					Primary: &AnomalyAction{
						ID: "open_monitoring", Label: "查看监控", Route: scopedMonitoringRoute(snapshot.MonitoringInstanceID),
					},
				})
			}
			if snapshot.ActiveIncidents > 0 {
				anomalies = append(anomalies, Anomaly{
					RuleID:   RuleMonitoringIncidentsOpen,
					Severity: SeverityWarning,
					Title:    "存在未关闭事件",
					Detail:   snapshot.MonitoringDetail,
					Source:   "monitoring",
					EventAt:  snapshot.MonitoringObserved,
					Primary: &AnomalyAction{
						ID: "open_incidents", Label: "查看事件", Route: scopedMonitoringEventsRoute(snapshot.MonitoringInstanceID),
					},
				})
			}
		}
	}

	if snapshot.IPAvailable {
		status := strings.ToLower(strings.TrimSpace(snapshot.IPStatus))
		if status == "missing" {
			anomalies = append(anomalies, Anomaly{
				RuleID:   RuleIPQualityMissing,
				Severity: SeverityNotice,
				Title:    "缺少 IP 质量证据",
				Source:   "ip_quality",
				Primary: &AnomalyAction{
					ID: "open_ip_quality", Label: "查看 IP 质量", Route: ipQualityRoute,
				},
			})
		} else if status != "not_configured" {
			if snapshot.IPStale {
				anomalies = append(anomalies, Anomaly{
					RuleID:   RuleIPQualityStale,
					Severity: SeverityNotice,
					Title:    "IP 质量证据过期",
					Source:   "ip_quality",
					EventAt:  snapshot.IPObservedAt,
					Primary: &AnomalyAction{
						ID: "open_ip_quality", Label: "查看 IP 质量", Route: ipQualityRoute,
					},
				})
			}
			if status == "partial" || status == "failure" {
				anomalies = append(anomalies, Anomaly{
					RuleID:   RuleIPQualityPartial,
					Severity: SeverityNotice,
					Title:    "IP 质量采集不完整",
					Detail:   snapshot.IPStatus,
					Source:   "ip_quality",
					EventAt:  snapshot.IPObservedAt,
					Primary: &AnomalyAction{
						ID: "open_ip_quality", Label: "查看 IP 质量", Route: ipQualityRoute,
					},
				})
			}
			if elevatedIPRisk(snapshot.IPRiskLevel) {
				anomalies = append(anomalies, Anomaly{
					RuleID:   RuleIPQualityRiskElevated,
					Severity: SeverityWarning,
					Title:    "IP 风险偏高",
					Detail:   snapshot.IPRiskLevel,
					Source:   "ip_quality",
					EventAt:  snapshot.IPObservedAt,
					Primary: &AnomalyAction{
						ID: "open_ip_quality", Label: "查看 IP 质量", Route: ipQualityRoute,
					},
				})
			}
		}
	}

	if snapshot.SubscriptionAvailable {
		lifecycle := snapshot.LifecycleStatus
		needsSubscription := lifecycle == "active" || lifecycle == "idle" || lifecycle == "testing"
		if needsSubscription && snapshot.ActiveSubscriptions == 0 {
			anomalies = append(anomalies, Anomaly{
				RuleID:   RuleRenewalSubscriptionMissing,
				Severity: SeverityWarning,
				Title:    "缺少有效订阅",
				Source:   "renewal",
				Primary: &AnomalyAction{
					ID: "open_subscription", Label: "管理订阅",
				},
			})
		}
		if snapshot.NextRenewAt != nil {
			renewDay := calendarDayUTC(*snapshot.NextRenewAt)
			generatedDay := calendarDayUTC(snapshot.GeneratedAt)
			dueInDays := int(renewDay.Sub(generatedDay).Hours() / 24)
			if dueInDays < 0 {
				anomalies = append(anomalies, Anomaly{
					RuleID:   RuleRenewalOverdue,
					Severity: SeverityWarning,
					Title:    "续费已逾期",
					Source:   "renewal",
					EventAt:  snapshot.NextRenewAt,
					Primary: &AnomalyAction{
						ID: "open_renewal_decision", Label: "查看续费",
					},
				})
			} else if dueInDays <= renewalDueWindowDays {
				anomalies = append(anomalies, Anomaly{
					RuleID:   RuleRenewalDueSoon,
					Severity: SeverityNotice,
					Title:    "续费临近",
					Source:   "renewal",
					EventAt:  snapshot.NextRenewAt,
					Primary: &AnomalyAction{
						ID: "open_renewal_decision", Label: "查看续费",
					},
				})
			}
		}
	}

	switch snapshot.LifecycleStatus {
	case "to_cancel", "to_migrate":
		anomalies = append(anomalies, Anomaly{
			RuleID:   RuleLifecycleBlocker,
			Severity: SeverityWarning,
			Title:    "生命周期待处理",
			Detail:   snapshot.LifecycleStatus,
			Source:   "lifecycle",
			Primary: &AnomalyAction{
				ID: "open_management", Label: "打开管理",
			},
		})
	}

	if len(snapshot.JudgementSourcesUnavailable) > 0 {
		sources := append([]string(nil), snapshot.JudgementSourcesUnavailable...)
		sort.Strings(sources)
		anomalies = append(anomalies, Anomaly{
			RuleID:   RuleSourceUnavailable,
			Severity: SeverityNotice,
			Title:    "判断依据暂不可用",
			Detail:   strings.Join(sources, ", "),
			Source:   "overview",
			Primary: &AnomalyAction{
				ID: "retry_overview", Label: "重试",
			},
		})
	}

	sort.SliceStable(anomalies, func(i, j int) bool {
		if severityRank(anomalies[i].Severity) != severityRank(anomalies[j].Severity) {
			return severityRank(anomalies[i].Severity) < severityRank(anomalies[j].Severity)
		}
		leftAt, rightAt := anomalyEventUnix(anomalies[i]), anomalyEventUnix(anomalies[j])
		if leftAt != rightAt {
			return leftAt > rightAt
		}
		return anomalies[i].RuleID < anomalies[j].RuleID
	})
	for i := range anomalies {
		if anomalies[i].Secondaries == nil {
			anomalies[i].Secondaries = []AnomalyAction{}
		}
	}
	return anomalies
}

func healthSeverity(health string) AnomalySeverity {
	switch health {
	case "严重":
		return SeverityCritical
	case "告警":
		return SeverityWarning
	case "关注":
		return SeverityNotice
	default:
		return SeverityWarning
	}
}

func elevatedIPRisk(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "high", "critical", "severe", "高", "严重":
		return true
	default:
		return false
	}
}

func severityRank(severity AnomalySeverity) int {
	switch severity {
	case SeverityCritical:
		return 0
	case SeverityWarning:
		return 1
	case SeverityNotice:
		return 2
	default:
		return 3
	}
}

func anomalyEventUnix(anomaly Anomaly) int64 {
	if anomaly.EventAt == nil {
		return 0
	}
	return anomaly.EventAt.UTC().Unix()
}

func calendarDayUTC(value time.Time) time.Time {
	year, month, day := value.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func scopedMonitoringRoute(instanceID string) string {
	id := strings.TrimSpace(instanceID)
	if id == "" {
		return "/monitoring?abnormal=1"
	}
	return "/monitoring/" + url.PathEscape(id)
}

func scopedMonitoringEventsRoute(instanceID string) string {
	route := "/events?object_type=monitoring_instance"
	id := strings.TrimSpace(instanceID)
	if id == "" {
		return route
	}
	return route + "&object_id=" + url.QueryEscape(id)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

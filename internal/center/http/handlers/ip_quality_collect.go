package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
)

// 立即采集不可用的原因，前端据此给出对应引导。
const (
	ipQualityCollectReasonDisabled             = "disabled"
	ipQualityCollectReasonNoMonitoringInstance = "no_monitoring_instance"
	ipQualityCollectReasonAgentNotBound        = "agent_not_bound"
	ipQualityCollectReasonMonitoringPaused     = "monitoring_paused"
)

type ipQualityCollectLinkRepository interface {
	ListMonitoringInstancesForVPS(context.Context, string) ([]assetlinks.MonitoringInstanceSummary, error)
}

type ipQualityCollectSettingsRepository interface {
	IPQualityEnabled(context.Context) (bool, error)
}

type IPQualityCollectRequests interface {
	Request(monitoringInstanceID string, now time.Time) (ipquality.CollectRequest, error)
	Latest(monitoringInstanceID string, now time.Time) (ipquality.CollectRequest, bool)
}

type ipQualityCollectStatus struct {
	Enabled              bool                      `json:"enabled"`
	Available            bool                      `json:"available"`
	UnavailableReason    string                    `json:"unavailable_reason,omitempty"`
	MonitoringInstanceID string                    `json:"monitoring_instance_id,omitempty"`
	AgentLastSyncAt      *time.Time                `json:"agent_last_sync_at,omitempty"`
	Request              *ipquality.CollectRequest `json:"request,omitempty"`
}

// VPSIPQualityCollect handles GET/POST /api/vps/{vps_id}/ip-quality/collect.
// GET 返回立即采集是否可用及最近一次请求进度；POST 登记一次立即采集，
// agent 在下一次 sync 收到计划后执行，不等待周期。
func VPSIPQualityCollect(links ipQualityCollectLinkRepository, settings ipQualityCollectSettingsRepository, requests IPQualityCollectRequests) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSIPQualityCollectPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		now := time.Now().UTC()
		status, err := resolveIPQualityCollectStatus(r.Context(), links, settings, vpsID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if r.Method == http.MethodGet {
			if status.MonitoringInstanceID != "" {
				if request, found := requests.Latest(status.MonitoringInstanceID, now); found {
					status.Request = &request
				}
			}
			writeJSON(w, http.StatusOK, status)
			return
		}
		if !status.Available {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":  ipQualityCollectUnavailableMessage(status.UnavailableReason),
				"reason": status.UnavailableReason,
			})
			return
		}
		request, err := requests.Request(status.MonitoringInstanceID, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		status.Request = &request
		writeJSON(w, http.StatusAccepted, status)
	})
}

func resolveIPQualityCollectStatus(ctx context.Context, links ipQualityCollectLinkRepository, settings ipQualityCollectSettingsRepository, vpsID string) (ipQualityCollectStatus, error) {
	enabled, err := settings.IPQualityEnabled(ctx)
	if err != nil {
		return ipQualityCollectStatus{}, err
	}
	summaries, err := links.ListMonitoringInstancesForVPS(ctx, vpsID)
	if err != nil {
		return ipQualityCollectStatus{}, err
	}
	status := ipQualityCollectStatus{Enabled: enabled}
	instance, found := currentIPQualityCollectInstance(summaries)
	if found {
		status.MonitoringInstanceID = instance.MonitoringInstanceID
		status.AgentLastSyncAt = instance.LastSyncAt
	}
	switch {
	case !enabled:
		status.UnavailableReason = ipQualityCollectReasonDisabled
	case !found:
		status.UnavailableReason = ipQualityCollectReasonNoMonitoringInstance
	case instance.LifecycleStatus == monitoringinstances.LifecyclePendingEnrollment || instance.BindingStatus != monitoringinstances.BindingBound:
		status.UnavailableReason = ipQualityCollectReasonAgentNotBound
	case instance.MonitoringStatus == monitoringinstances.MonitoringPaused:
		status.UnavailableReason = ipQualityCollectReasonMonitoringPaused
	default:
		status.Available = true
	}
	return status, nil
}

// currentIPQualityCollectInstance 选择与 sync plan 下发条件一致的当前监控实例：
// 未退役、未归档，且所属 VPS 未归档。
func currentIPQualityCollectInstance(summaries []assetlinks.MonitoringInstanceSummary) (assetlinks.MonitoringInstanceSummary, bool) {
	for _, summary := range summaries {
		if !summary.IsCurrent || summary.ArchivedAt != nil || summary.VPSLifecycleStatus == "archived" {
			continue
		}
		if summary.LifecycleStatus == monitoringinstances.LifecycleRetired {
			continue
		}
		return summary, true
	}
	return assetlinks.MonitoringInstanceSummary{}, false
}

func ipQualityCollectUnavailableMessage(reason string) string {
	switch reason {
	case ipQualityCollectReasonDisabled:
		return "ip quality collection is disabled"
	case ipQualityCollectReasonNoMonitoringInstance:
		return "vps has no active monitoring instance"
	case ipQualityCollectReasonAgentNotBound:
		return "monitoring instance agent not bound"
	case ipQualityCollectReasonMonitoringPaused:
		return "monitoring instance monitoring is paused"
	default:
		return "ip quality collection unavailable"
	}
}

func parseVPSIPQualityCollectPath(path string) (string, bool) {
	relative := strings.Trim(strings.TrimPrefix(path, "/api/vps/"), "/")
	if relative == path || relative == "" {
		return "", false
	}
	segments := strings.Split(relative, "/")
	if len(segments) != 3 || segments[0] == "" || segments[1] != "ip-quality" || segments[2] != "collect" {
		return "", false
	}
	return segments[0], true
}

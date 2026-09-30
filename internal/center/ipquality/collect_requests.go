package ipquality

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"houfeng/internal/contracts/agentapi"
)

// 立即采集请求状态。请求只保存在 center 进程内存中：center 重启后丢失，
// 用户可重新发起；agent 侧按请求 ID 去重，重复下发不会重复采集。
const (
	CollectRequestPending    = "pending"
	CollectRequestDispatched = "dispatched"
	CollectRequestCompleted  = "completed"
	CollectRequestExpired    = "expired"
)

const (
	// CollectRequestTTL 覆盖 agent 同步间隔、最长 300 秒的采集超时与上报排队。
	CollectRequestTTL = 10 * time.Minute
	// collectRequestRetention 让已完成/过期的请求在页面上保留一段时间后再清理。
	collectRequestRetention = 30 * time.Minute

	maxCollectRequestIDLength   = 64
	maxCollectErrorSummaryRunes = 200
)

type CollectRequest struct {
	RequestID            string     `json:"request_id"`
	MonitoringInstanceID string     `json:"monitoring_instance_id"`
	Status               string     `json:"status"`
	RequestedAt          time.Time  `json:"requested_at"`
	ExpiresAt            time.Time  `json:"expires_at"`
	DispatchedAt         *time.Time `json:"dispatched_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	ReportStatus         string     `json:"report_status,omitempty"`
	ErrorSummary         string     `json:"error_summary,omitempty"`
}

// Active 表示请求仍在等待 agent 执行或回传。
func (r CollectRequest) Active() bool {
	return r.Status == CollectRequestPending || r.Status == CollectRequestDispatched
}

type CollectRequests struct {
	mu      sync.Mutex
	entries map[string]CollectRequest
	newID   func() (string, error)
}

func NewCollectRequests(newID func() (string, error)) *CollectRequests {
	return &CollectRequests{entries: make(map[string]CollectRequest), newID: newID}
}

// Request 为监控实例登记一次立即采集；已有进行中的请求时直接返回它，避免重复触发外部查询。
func (c *CollectRequests) Request(monitoringInstanceID string, now time.Time) (CollectRequest, error) {
	now = now.UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(now)
	if current, ok := c.currentLocked(monitoringInstanceID, now); ok && current.Active() {
		return current, nil
	}
	requestID, err := c.newID()
	if err != nil {
		return CollectRequest{}, fmt.Errorf("generate ip quality collect request id: %w", err)
	}
	request := CollectRequest{
		RequestID:            requestID,
		MonitoringInstanceID: monitoringInstanceID,
		Status:               CollectRequestPending,
		RequestedAt:          now,
		ExpiresAt:            now.Add(CollectRequestTTL),
	}
	c.entries[monitoringInstanceID] = request
	return request, nil
}

// Latest 返回监控实例最近一次请求（含已完成/过期），供页面展示进度。
func (c *CollectRequests) Latest(monitoringInstanceID string, now time.Time) (CollectRequest, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentLocked(monitoringInstanceID, now.UTC())
}

// PendingRequestID 返回需要随 sync plan 下发的请求 ID，并把请求标记为已下发。
func (c *CollectRequests) PendingRequestID(monitoringInstanceID string, now time.Time) string {
	now = now.UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.currentLocked(monitoringInstanceID, now)
	if !ok || !current.Active() {
		return ""
	}
	if current.Status == CollectRequestPending {
		dispatchedAt := now
		current.Status = CollectRequestDispatched
		current.DispatchedAt = &dispatchedAt
		c.entries[monitoringInstanceID] = current
	}
	return current.RequestID
}

// ObserveReports 用已入库、且在 diagnostics_json 中带回同一请求 ID 的报告完成请求。
func (c *CollectRequests) ObserveReports(monitoringInstanceID string, reports []ReportWrite, now time.Time) {
	now = now.UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.currentLocked(monitoringInstanceID, now)
	if !ok || !current.Active() {
		return
	}
	for _, report := range reports {
		if report.CollectRequestID == "" || report.CollectRequestID != current.RequestID {
			continue
		}
		completedAt := now
		current.Status = CollectRequestCompleted
		current.CompletedAt = &completedAt
		current.ReportStatus = report.Status
		current.ErrorSummary = truncateRunes(strings.TrimSpace(report.ErrorSummary), maxCollectErrorSummaryRunes)
		c.entries[monitoringInstanceID] = current
		return
	}
}

func (c *CollectRequests) currentLocked(monitoringInstanceID string, now time.Time) (CollectRequest, bool) {
	current, ok := c.entries[monitoringInstanceID]
	if !ok {
		return CollectRequest{}, false
	}
	if current.Active() && !now.Before(current.ExpiresAt) {
		current.Status = CollectRequestExpired
		c.entries[monitoringInstanceID] = current
	}
	return current, true
}

func (c *CollectRequests) pruneLocked(now time.Time) {
	for key, entry := range c.entries {
		// 保留期长于 TTL，超过保留期的请求必然已完成或过期。
		if now.Sub(entry.RequestedAt) > collectRequestRetention {
			delete(c.entries, key)
		}
	}
}

// CollectRequestIDFromDiagnostics 从 agent 原始 diagnostics_json 读取回传的请求 ID；
// 非对象、缺失、超长或含非法字符时返回空串。
func CollectRequestIDFromDiagnostics(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	var value string
	if err := json.Unmarshal(fields[agentapi.IPQualityDiagnosticsCollectRequestIDKey], &value); err != nil {
		return ""
	}
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxCollectRequestIDLength {
		return ""
	}
	for _, r := range value {
		if !(r == '_' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return ""
		}
	}
	return value
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

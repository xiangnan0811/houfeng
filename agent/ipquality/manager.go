package ipquality

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"houfeng/internal/contracts/agentapi"
)

type Manager struct {
	store     StateStore
	collector Collector
	mu        sync.Mutex
	inFlight  bool
	// inFlightRequestID 是本轮采集要回传的立即采集请求；周期采集中途收到请求时直接认领，
	// 采集完成后不再重复执行。
	inFlightRequestID string
	// handledRequestID 是已产出报告的立即采集请求，只保存在内存：center 收到报告前会继续
	// 下发同一 ID，据此去重；进程重启或 Stop 后清空，最多多采一次，不会漏采。
	handledRequestID string
	// startState 是本轮启动时的持久状态，完成时在其基础上更新，避免丢失未改动字段。
	startState State
	reports    []agentapi.IPQualityReportPayload
	cancel     context.CancelFunc
}

func NewManager(store StateStore, collector Collector) *Manager {
	return &Manager{store: store, collector: collector}
}

func (m *Manager) MaybeStart(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time) error {
	if plan == nil || !plan.Enabled {
		return nil
	}
	requestID := strings.TrimSpace(plan.CollectRequestID)
	m.mu.Lock()
	if m.inFlight {
		if requestID != "" && m.inFlightRequestID == "" && requestID != m.handledRequestID {
			m.inFlightRequestID = requestID
		}
		m.mu.Unlock()
		return nil
	}
	manual := requestID != "" && requestID != m.handledRequestID
	m.mu.Unlock()

	state, err := m.store.Load(ctx)
	if err != nil {
		return fmt.Errorf("load ip quality state: %w", err)
	}
	if !manual && !Due(plan, state, observedAt) {
		return nil
	}
	state.LastAttemptedAt = observedAt.UTC()
	if err := m.store.Save(ctx, state); err != nil {
		return fmt.Errorf("save ip quality attempt state: %w", err)
	}

	m.mu.Lock()
	if m.inFlight {
		m.mu.Unlock()
		return nil
	}
	m.inFlight = true
	m.inFlightRequestID = ""
	if manual {
		m.inFlightRequestID = requestID
	}
	m.startState = state
	collectCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.cancel = cancel
	m.mu.Unlock()

	planCopy := clonePlan(plan)
	go m.collect(collectCtx, planCopy, observedAt.UTC())
	return nil
}

// Stop cancels any in-flight collection and discards its eventual output.
// 已处理的立即采集请求一并遗忘：报告可能随之丢弃，恢复后 center 若仍下发同一 ID 就重新采集。
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
	m.reports = nil
	m.handledRequestID = ""
}

func (m *Manager) DrainReports() []agentapi.IPQualityReportPayload {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]agentapi.IPQualityReportPayload(nil), m.reports...)
	m.reports = nil
	return out
}

func (m *Manager) collect(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time) {
	report := m.collector.Collect(ctx, plan, observedAt)

	// 认领请求、保存状态和入队报告在同一临界区内完成，避免与 MaybeStart 的认领竞争。
	// 被 Stop 取消时报告会被丢弃，也不把请求记为已处理。
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() == nil {
		state := m.startState
		state.LastAttemptedAt = observedAt
		state.LastStatus = report.Status
		if report.Status == agentapi.IPQualityStatusSuccess {
			state.LastSucceededAt = observedAt
		}
		if m.inFlightRequestID != "" {
			report.DiagnosticsJSON = withCollectRequestID(report.DiagnosticsJSON, m.inFlightRequestID)
			m.handledRequestID = m.inFlightRequestID
		}
		_ = m.store.Save(ctx, state)
		m.reports = append(m.reports, report)
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.inFlight = false
	m.inFlightRequestID = ""
}

func clonePlan(plan *agentapi.IPQualityPlan) *agentapi.IPQualityPlan {
	if plan == nil {
		return nil
	}
	return &agentapi.IPQualityPlan{
		Enabled:          plan.Enabled,
		FrequencySeconds: plan.FrequencySeconds,
		TimeoutSeconds:   plan.TimeoutSeconds,
		Services:         append([]string(nil), plan.Services...),
		CollectRequestID: plan.CollectRequestID,
	}
}

// withCollectRequestID 把立即采集请求 ID 写进 diagnostics_json。报告顶层不加字段：
// 旧 center 严格解码 sync 请求，未知字段会让整批被拒并被 agent 丢弃；diagnostics_json
// 是旧 center 已接受的自由 JSON。
func withCollectRequestID(raw json.RawMessage, requestID string) json.RawMessage {
	var fields map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &fields); err != nil {
			fields = map[string]json.RawMessage{"diagnostics": append(json.RawMessage(nil), raw...)}
		}
	}
	// JSON null（含前后空白）解码后仍是 nil map。
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	encodedID, err := json.Marshal(requestID)
	if err != nil {
		return raw
	}
	fields[agentapi.IPQualityDiagnosticsCollectRequestIDKey] = encodedID
	merged, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return merged
}

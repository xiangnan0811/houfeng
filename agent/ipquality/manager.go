package ipquality

import (
	"context"
	"fmt"
	"sync"
	"time"

	"houfeng/internal/contracts/agentapi"
)

type Manager struct {
	store     StateStore
	collector Collector
	mu        sync.Mutex
	inFlight  bool
	reports   []agentapi.IPQualityReportPayload
}

func NewManager(store StateStore, collector Collector) *Manager {
	return &Manager{store: store, collector: collector}
}

func (m *Manager) MaybeStart(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time) error {
	if plan == nil || !plan.Enabled {
		return nil
	}
	m.mu.Lock()
	if m.inFlight {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	state, err := m.store.Load(ctx)
	if err != nil {
		return fmt.Errorf("load ip quality state: %w", err)
	}
	if !Due(plan, state, observedAt) {
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
	m.mu.Unlock()

	planCopy := clonePlan(plan)
	go m.collect(context.WithoutCancel(ctx), planCopy, observedAt.UTC())
	return nil
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
	state := State{
		LastAttemptedAt: observedAt,
		LastStatus:      report.Status,
	}
	if report.Status == agentapi.IPQualityStatusSuccess {
		state.LastSucceededAt = observedAt
	}
	_ = m.store.Save(ctx, state)

	m.mu.Lock()
	m.reports = append(m.reports, report)
	m.inFlight = false
	m.mu.Unlock()
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
	}
}

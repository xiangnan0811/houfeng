import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import {
  decodeSubscriptionCostEvidenceReadModel,
  type CommandAuditEvidenceReadModel,
  type IPQualityEvidenceReadModel,
  type MonitoringEventEvidenceReadModel,
  type MonitoringEvidenceReadModel,
} from '../evidenceReadModels'
import { CommandAuditEvidenceRenderer } from './CommandAuditEvidenceRenderer'
import { IPQualityEvidenceRenderer } from './IPQualityEvidenceRenderer'
import { MonitoringEventEvidenceRenderer } from './MonitoringEventEvidenceRenderer'
import { MonitoringEvidenceRenderer } from './MonitoringEvidenceRenderer'
import { SubscriptionCostEvidenceRenderer } from './SubscriptionCostEvidenceRenderer'

function costModel(overrides: Record<string, unknown> = {}): unknown {
  return {
    version: 'subscription_cost_read_model/v1', subscription_id: 'sub_1', vps_id: 'vps_1', original_amount: 18.5,
    original_currency: 'USD', billing_period_unit: 'month', billing_period_length: 3, conversion_rate: 132.5 / 18.5,
    conversion_provider: 'fixer', rate_date: '2026-08-01', rate_fetched_at: '2026-08-16T00:00:00Z', rate_stale: true,
    base_amount: 132.5, base_currency: 'CNY', budget_source: 'subscription_monthly_budgets', budget_currency: 'CNY',
    budget_month: '2026-08', budget_monthly_limit: 1000, budget_warning_pct: 80, budget_status: 'ok',
    budget_actual_spend: 200, coverage_start: '2026-08-01T00:00:00Z', coverage_end: '2026-09-01T00:00:00Z',
    coverage_status: 'complete', covered_days: 31, total_days: 31, converted_subscription_count: 1, missing_rate_count: 0,
    ...overrides,
  }
}

const quality = {
  status: 'complete', partial: false, truncated: false, sample_count: 1, maintenance_count: 0,
  backfilled_count: 0, bucket_count: 1, gap_count: 0, peak_count: 0, data_point_count: 3,
} as const

describe('evidence renderers', () => {
  it('shows command outcomes in Chinese and only non-zero exit codes', () => {
    const audit = {
      audit_id: 'a1', action_id: 'act', monitoring_instance_id: 'mi', monitoring_instance_name: 'alpha',
      actor_user_id: 'u', actor_username: 'op', actor_display_name: '值班员', command_id: 'uptime',
      sensitivity: 'standard', event_type: 'completed', outcome: 'succeeded', source: 'agent_sync',
      exit_code: 0, occurred_at: '2026-08-17T13:45:00Z',
    }
    const model: CommandAuditEvidenceReadModel = {
      version: 'command_audit_read_model/v1', audit_count: 2, command_result_retention_seconds: 86400,
      command_result_payload_allowed: false,
      audits: [audit, { ...audit, audit_id: 'a2', command_id: 'df_h', outcome: 'failed', exit_code: 2 }],
    }
    render(<CommandAuditEvidenceRenderer model={model} />)
    expect(screen.getByText('成功')).toHaveClass('tone--normal')
    expect(screen.getByText('失败')).toHaveClass('tone--critical')
    expect(screen.getAllByText(/退出码/)).toHaveLength(1)
    expect(screen.getByText('退出码 2')).toBeInTheDocument()
    expect(screen.getAllByText('Agent 回传')).toHaveLength(2)
  })

  it('does not paint a recovered incident with its prior alert colour', () => {
    const event = {
      event_id: 'e1', object_type: 'monitoring_instance', object_id: 'mi', event_type: 'incident_recovered',
      severity: '严重', summary: '恢复正常', event_at: '2026-08-17T14:05:00Z', recorded_at: '2026-08-17T14:05:00Z',
      backfilled: true, provenance: 'center', producer_version: 'center-monitoring-events/v1',
      rule_version: 'incident-rules/v1', prior_state: 'critical', resulting_state: 'normal',
      correction_of_event_id: '', metrics: [],
    }
    const model: MonitoringEventEvidenceReadModel = {
      version: 'monitoring_event_read_model/v2', quality_status: 'complete', event_count: 1, backfilled_count: 1,
      events: [event],
    }
    render(<MonitoringEventEvidenceRenderer model={model} />)
    expect(screen.getByText('异常恢复')).toBeInTheDocument()
    expect(screen.getByText('严重')).not.toHaveClass('tone--critical')
    expect(screen.getByText('回填')).toBeInTheDocument()
  })

  it('summarizes IP quality with Chinese risk and service states', () => {
    const model: IPQualityEvidenceReadModel = {
      version: 'ip_quality_report_read_model/v1', report_id: 'ipq_1', observed_at: '2026-08-17T13:50:00Z',
      received_at: '2026-08-17T13:50:01Z', ip_version: 4, status: 'partial', stale: true, stale_after_seconds: 604800,
      risk_level: 'medium',
      coverage: { expected_provider_count: 2, successful_provider_count: 1, expected_service_count: 1, successful_service_count: 0 },
      providers: [{ provider: 'ipapi.is', status: 'success', risk_level: 'high' }, { provider: 'scamalytics', status: 'failure' }],
      services: [{ service: 'Reddit', source: 'default', status: 'unknown', probe_status: 'failure' }],
      quality: { ...quality, status: 'partial', partial: true },
    }
    render(<IPQualityEvidenceRenderer model={model} />)
    expect(screen.getByText('中')).toHaveClass('tone--notice')
    expect(screen.getByText('部分成功 · 已过期')).toBeInTheDocument()
    expect(screen.getByText('高风险')).toHaveClass('tone--alert')
    expect(screen.getByText('失败')).toBeInTheDocument()
    expect(screen.getByText('探测失败')).toBeInTheDocument()
  })

  it('shows cost, conversion and budget usage as tiles for a decodable over-budget model', () => {
    const model = decodeSubscriptionCostEvidenceReadModel(costModel({ budget_status: 'over', budget_actual_spend: 1200 }))
    expect(model).not.toBeNull()
    render(<SubscriptionCostEvidenceRenderer model={model!} />)
    expect(screen.getByText('每 3 月')).toBeInTheDocument()
    expect(screen.getByText(/Fixer 2026-08-01 · 汇率过期/)).toBeInTheDocument()
    expect(screen.getByText('超支')).toHaveClass('tone--critical')
    expect(screen.getByRole('progressbar', { name: '预算使用' })).toHaveAttribute('value', '1000')
    expect(screen.getByText('31/31 天')).toBeInTheDocument()
  })

  it.each([
    ['a missing exchange rate', { budget_status: 'unknown', missing_rate_count: 2, coverage_status: 'missing_rate' }, '有订阅缺少汇率'],
    ['no monthly budget', { budget_status: 'unknown', budget_monthly_limit: 0 }, '未设置月度预算'],
  ])('does not draw a budget ratio with %s', (_name, overrides, reason) => {
    const model = decodeSubscriptionCostEvidenceReadModel(costModel(overrides))
    expect(model).not.toBeNull()
    render(<SubscriptionCostEvidenceRenderer model={model!} />)
    expect(screen.getByText('无法判定')).toBeInTheDocument()
    expect(screen.getByText(reason)).toBeInTheDocument()
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  })

  it('renders hostile enum strings verbatim instead of crashing', () => {
    const model: IPQualityEvidenceReadModel = {
      version: 'ip_quality_report_read_model/v1', report_id: 'ipq_2', observed_at: '2026-08-17T13:50:00Z',
      received_at: '2026-08-17T13:50:01Z', ip_version: 4, status: 'success', stale: false, stale_after_seconds: 604800,
      risk_level: '__proto__',
      coverage: { expected_provider_count: 1, successful_provider_count: 1, expected_service_count: 0, successful_service_count: 0 },
      providers: [{ provider: 'p', status: 'success', risk_level: 'constructor' }],
      services: [],
      quality,
    }
    render(<IPQualityEvidenceRenderer model={model} />)
    expect(screen.getByText('__proto__')).toBeInTheDocument()
    expect(screen.getByText('constructor')).toBeInTheDocument()
  })
})

describe('monitoring evidence coverage window', () => {
  function monitoringModel(start: string, end: string): MonitoringEvidenceReadModel {
    return {
      version: 'monitoring_host_read_model/v1', requested_start: start, requested_end: end,
      coverage_start: start, coverage_end: end, actual_precision_seconds: 300,
      buckets: [{
        series_id: 'host', series_kind: 'host', start, end: new Date(Date.parse(start) + 300_000).toISOString().replace('.000Z', 'Z'),
        source_layer: 'raw', source_granularity_seconds: 300, sample_count: 1, maintenance_count: 0, backfilled_count: 0,
        metrics: [{ name: 'cpu_usage_pct', unit: 'percent', average: 10 }],
      }],
      gaps: [], peaks: [], quality,
    }
  }

  it('repeats only the time on the same local day and writes both ends across midnight', () => {
    const sameDay = new Date(2026, 7, 17, 21, 0)
    const sameDayEnd = new Date(2026, 7, 17, 22, 0)
    const { unmount } = render(<MonitoringEvidenceRenderer title="趋势" model={monitoringModel(sameDay.toISOString().replace('.000Z', 'Z'), sameDayEnd.toISOString().replace('.000Z', 'Z'))} />)
    expect(screen.getByText('2026/08/17 21:00 – 22:00')).toBeInTheDocument()
    unmount()
    const lateStart = new Date(2026, 7, 17, 23, 30)
    const nextDay = new Date(2026, 7, 18, 0, 30)
    render(<MonitoringEvidenceRenderer title="趋势" model={monitoringModel(lateStart.toISOString().replace('.000Z', 'Z'), nextDay.toISOString().replace('.000Z', 'Z'))} />)
    expect(screen.getByText('2026/08/17 23:30 – 2026/08/18 00:30')).toBeInTheDocument()
  })
})

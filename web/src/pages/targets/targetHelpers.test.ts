import { describe, expect, it } from 'vitest'

import type { TargetRecord } from '../../lib/types'
import { combineCurrentAndRetiredTargets, countAbnormalTargets, countArchivedTargets, countCoverageGapTargets, countPausedTargets, countUnobservedTargets, targetAttentionBadges, targetCoverageSummary, targetIssueSummary, targetTypeLabel, targetTypePresentation } from './targetHelpers'

function target(overrides: Partial<TargetRecord> = {}): TargetRecord {
  return {
    target_id: 'tg_001',
    name: 'API',
    target_type: 'service',
    host: 'api.example.com',
    execution_monitoring_instance_labels: ['edge'],
    lifecycle_status: 'active',
    run_status: '启用',
    group: 'edge',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    enabled_probe_count: 1,
    matching_executor_count: 1,
    last_success_at: '2026-04-26T08:30:00Z',
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-26T09:00:00Z',
    ...overrides,
  }
}

function withoutObservationTimes(record: TargetRecord): TargetRecord {
  const next: TargetRecord = { ...record }
  delete next.last_success_at
  delete next.last_failure_at
  return next
}

describe('combineCurrentAndRetiredTargets', () => {
  it('keeps the current collection ahead of retired history and drops id overlap', () => {
    const current = target({ target_id: 'tg_current', name: 'Current API' })
    const retired = target({ target_id: 'tg_retired', name: 'Retired History', lifecycle_status: 'retired', run_status: '暂停' })
    const staleCopy = target({ target_id: 'tg_current', name: 'Stale copy', lifecycle_status: 'retired' })
    const combined = combineCurrentAndRetiredTargets([current], [retired, staleCopy])
    expect(combined.map((item) => item.name)).toEqual(['Current API', 'Retired History'])
    expect(countAbnormalTargets(combined)).toBe(0)
    expect(countArchivedTargets(combined)).toBe(1)
  })
})

describe('target type labels', () => {
  it('translates known wire types and folds unknown values to a fixed label', () => {
    expect(targetTypeLabel('service')).toBe('服务')
    expect(targetTypeLabel('china_reference')).toBe('国内参考')
    expect(targetTypeLabel('future_type')).toBe('未知类型')
    expect(targetTypeLabel('国内参考 https://status.example/ref')).toBe('未知类型')
    expect(targetTypePresentation('国内参考 https://status.example/ref')).toEqual({
      label: '未知类型',
      raw: '国内参考 https://status.example/ref',
    })
  })
})

describe('targetAttentionBadges', () => {
  it('derives retired state independently of paused control and excludes it from operational counters', () => {
    const retired = withoutObservationTimes(target({ lifecycle_status: 'retired', run_status: '暂停', current_health_status: '严重', execution_monitoring_instance_labels: [], enabled_probe_count: 0, matching_executor_count: 0 }))
    expect(targetAttentionBadges(retired)).toEqual([{ label: '已退役', tone: 'offline' }])
    expect(countArchivedTargets([retired])).toBe(1)
    expect(countPausedTargets([retired])).toBe(0)
    expect(countAbnormalTargets([retired])).toBe(0)
    expect(countUnobservedTargets([retired])).toBe(0)
    expect(countCoverageGapTargets([retired])).toBe(0)
  })
  it('keeps control state independent of health and uses a neutral badge for missing observations', () => {
    expect(targetAttentionBadges(target())).toEqual([])
    expect(targetAttentionBadges(target({ run_status: '维护中' })).map((badge) => badge.label)).toEqual(['维护中'])
    expect(targetAttentionBadges(target({ run_status: '暂停', current_health_status: '正常' })).map((badge) => badge.label)).toEqual(['暂停'])
    expect(targetAttentionBadges(target({ run_status: '维护中', current_health_status: '告警' })).map((badge) => badge.label)).toEqual(['维护中', '告警'])
    expect(targetAttentionBadges(withoutObservationTimes(target({ current_health_status: '数据不可用' })))).toEqual([{ label: '数据不可用', tone: 'neutral' }])
  })

  it('counts observed attention health separately from unobserved and coverage facts', () => {
    const observedAlert = target({ current_health_status: '严重' })
    const unobserved = withoutObservationTimes(target({ current_health_status: '数据不可用', matching_executor_count: 2 }))
    const labeledButUnmatched = target({ execution_monitoring_instance_labels: ['future'], matching_executor_count: 0 })
    const pausedWithProbes = target({ run_status: '暂停', current_health_status: '暂停', enabled_probe_count: 2, matching_executor_count: 0 })
    expect(targetAttentionBadges(observedAlert).map((badge) => badge.label)).toEqual(['严重'])
    expect(countAbnormalTargets([observedAlert, unobserved, labeledButUnmatched, pausedWithProbes])).toBe(1)
    expect(countUnobservedTargets([observedAlert, unobserved, pausedWithProbes])).toBe(1)
    expect(countCoverageGapTargets([labeledButUnmatched, pausedWithProbes, target()])).toBe(1)
    expect(targetIssueSummary(target({ current_primary_issue_summary: 'HTTP 探测持续失败' }))).toBe('HTTP 探测持续失败')
    expect(targetIssueSummary(unobserved)).toBe('已匹配实例，尚无样本')
    expect(targetIssueSummary(labeledButUnmatched)).toBe('没有可接收该任务的实例')
    expect(targetCoverageSummary(pausedWithProbes)).toBe('启用探测项 2 · 可接收实例 0')
    expect(targetIssueSummary(target())).toBe('')
  })
})

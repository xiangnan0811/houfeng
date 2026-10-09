import { describe, expect, it } from 'vitest'

import { ApiError } from '../../lib/api'
import { targetObservationFixture } from '../../lib/targetObservationFixture'
import type { TargetObservationFreshness, TargetRecord } from '../../lib/types'
import { combineCurrentAndRetiredTargets, countAbnormalTargets, countArchivedTargets, countCoverageGapTargets, countPausedTargets, countStaleTargets, countUnobservedTargets, isStaleTarget, isTargetListInvalidatingError, targetAttentionBadges, targetCoverageSummary, targetFreshnessBadge, targetIssueSummary, targetKnownHealthNote, targetMatchesGroup, targetTypeLabel, targetTypePresentation } from './targetHelpers'


function freshness(overrides: Partial<TargetObservationFreshness> = {}): TargetObservationFreshness {
  return {
    state: 'fresh',
    evaluated_at: '2026-04-26T09:00:00Z',
    enabled_probe_count: 1,
    fresh_probe_count: 1,
    pending_probe_count: 0,
    stale_probe_count: 0,
    probes: [],
    ...overrides,
  }
}
function target(overrides: Partial<TargetRecord> = {}): TargetRecord {
  const targetId = overrides.target_id ?? 'tg_001'
  const runStatus = overrides.run_status ?? '启用'
  const lifecycleStatus = overrides.lifecycle_status ?? 'active'
  const enabledProbeCount = overrides.enabled_probe_count ?? 1
  const lastSuccessAt = 'last_success_at' in overrides ? overrides.last_success_at : '2026-04-26T08:30:00Z'
  const lastFailureAt = 'last_failure_at' in overrides ? overrides.last_failure_at : undefined
  const record: TargetRecord = {
    target_id: targetId,
    name: 'API',
    target_type: 'service',
    host: 'api.example.com',
    execution_monitoring_instance_labels: ['edge'],
    lifecycle_status: lifecycleStatus,
    run_status: runStatus,
    group: 'edge',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    enabled_probe_count: enabledProbeCount,
    matching_executor_count: 1,
    observation_freshness: targetObservationFixture({
      target_id: targetId,
      run_status: runStatus,
      lifecycle_status: lifecycleStatus,
      evaluated_at: '2026-04-26T09:00:00Z',
      enabled_probe_count: enabledProbeCount,
      last_success_at: lastSuccessAt,
      last_failure_at: lastFailureAt,
    }),
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-26T09:00:00Z',
    ...overrides,
  }
  if (lastSuccessAt !== undefined) record.last_success_at = lastSuccessAt
  if (lastFailureAt !== undefined) record.last_failure_at = lastFailureAt
  return record
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
    expect(countStaleTargets([retired])).toBe(0)
  })
  it('keeps control state independent of health and uses a neutral badge for missing observations', () => {
    expect(targetAttentionBadges(target())).toEqual([{ label: '观测新鲜', tone: 'normal' }])
    expect(targetAttentionBadges(target({ run_status: '维护中' })).map((badge) => badge.label)).toEqual(['维护中'])
    expect(targetAttentionBadges(target({ run_status: '暂停', current_health_status: '正常' })).map((badge) => badge.label)).toEqual(['暂停'])
    expect(targetAttentionBadges(target({ run_status: '维护中', current_health_status: '告警' })).map((badge) => badge.label)).toEqual(['维护中', '告警'])
    expect(targetAttentionBadges(withoutObservationTimes(target({
      current_health_status: '数据不可用',
      observation_freshness: freshness({ state: 'unobserved', fresh_probe_count: 0, pending_probe_count: 1, stale_probe_count: 0 }),
    })))).toEqual([
      { label: '数据不可用', tone: 'neutral' },
      { label: '尚无观测', tone: 'neutral' },
    ])
  })

  it('counts observed attention health separately from unobserved and coverage facts', () => {
    const observedAlert = target({ current_health_status: '严重' })
    const unobserved = withoutObservationTimes(target({ current_health_status: '数据不可用', matching_executor_count: 2 }))
    const labeledButUnmatched = target({ execution_monitoring_instance_labels: ['future'], matching_executor_count: 0 })
    const pausedWithProbes = target({ run_status: '暂停', current_health_status: '暂停', enabled_probe_count: 2, matching_executor_count: 0 })
    expect(targetAttentionBadges(observedAlert).map((badge) => badge.label)).toEqual(['严重', '观测新鲜'])
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

describe('observation freshness', () => {
  it('counts only enabled targets that already have a sample and a stale probe', () => {
    const fresh = target()
    const stale = target({
      target_id: 'tg_stale',
      current_health_status: '告警',
      current_primary_issue_summary: 'HTTP 探测持续失败',
      observation_freshness: freshness({ state: 'stale', fresh_probe_count: 0, stale_probe_count: 1 }),
    })
    const partialStale = target({
      target_id: 'tg_partial_stale',
      observation_freshness: freshness({ state: 'partial', fresh_probe_count: 1, stale_probe_count: 1 }),
    })
    const partialWaiting = target({
      target_id: 'tg_partial_wait',
      observation_freshness: freshness({ state: 'partial', fresh_probe_count: 1, pending_probe_count: 1, stale_probe_count: 0 }),
    })
    const pending = target({
      target_id: 'tg_pending',
      observation_freshness: freshness({ state: 'pending', fresh_probe_count: 0, pending_probe_count: 1, stale_probe_count: 0 }),
    })
    const paused = target({
      target_id: 'tg_paused',
      run_status: '暂停',
      observation_freshness: freshness({ state: 'inactive', fresh_probe_count: 0, stale_probe_count: 0 }),
    })
    const never = withoutObservationTimes(target({
      target_id: 'tg_never',
      current_health_status: '数据不可用',
      observation_freshness: freshness({ state: 'unobserved', fresh_probe_count: 0, stale_probe_count: 1 }),
    }))
    const rows = [fresh, stale, partialStale, partialWaiting, pending, paused, never]
    expect(rows.filter(isStaleTarget).map((item) => item.target_id)).toEqual(['tg_stale', 'tg_partial_stale'])
    expect(countStaleTargets(rows)).toBe(2)
    expect(countAbnormalTargets(rows)).toBe(1)
    expect(countUnobservedTargets(rows)).toBe(1)
    expect(countStaleTargets(rows) + countAbnormalTargets(rows)).toBe(3)
    expect(targetIssueSummary(stale)).toBe('HTTP 探测持续失败')
    expect(targetAttentionBadges(stale).map((badge) => badge.label)).toEqual(['告警', '观测已过期'])
    expect(targetKnownHealthNote(stale)).toBe('最近已知健康')
    expect(targetFreshnessBadge(partialStale)?.label).toBe('部分观测过期')
    expect(targetFreshnessBadge(partialWaiting)?.label).toBe('部分探测项等待观测')
    expect(targetKnownHealthNote(partialWaiting)).toBe('最近一次正常，当前证据不足')
    expect(targetKnownHealthNote(fresh)).toBeNull()
    expect(targetAttentionBadges(paused).map((badge) => badge.label)).toEqual(['暂停'])
    expect(targetFreshnessBadge(target({ observation_freshness: freshness({ state: 'uncovered', enabled_probe_count: 0, fresh_probe_count: 0 }) }))?.label).toBe('未配置启用探测项')
    expect(String(targetKnownHealthNote(partialWaiting))).not.toContain('当前正常')
  })

  it('matches a blank group to the dashboard name 未分组', () => {
    expect(targetMatchesGroup(target({ group: '' }), '未分组')).toBe(true)
    expect(targetMatchesGroup(target({ group: '   ' }), '未分组')).toBe(true)
    expect(targetMatchesGroup(target({ group: '未分组' }), '未分组')).toBe(true)
    expect(targetMatchesGroup(target({ group: 'edge' }), '未分组')).toBe(false)
    expect(targetMatchesGroup(target({ group: 'edge' }), 'edge')).toBe(true)
  })

  it('clears the list for authorization and not-found failures only', () => {
    expect(isTargetListInvalidatingError(new ApiError(401, 'unauthenticated'))).toBe(true)
    expect(isTargetListInvalidatingError(new ApiError(403, 'forbidden'))).toBe(true)
    expect(isTargetListInvalidatingError(new ApiError(404, 'missing'))).toBe(true)
    expect(isTargetListInvalidatingError(new ApiError(503, 'unavailable'))).toBe(false)
    expect(isTargetListInvalidatingError(new Error('network'))).toBe(false)
  })
})

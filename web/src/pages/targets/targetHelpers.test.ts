import { describe, expect, it } from 'vitest'

import type { TargetRecord } from '../../lib/types'
import { targetAttentionBadges, targetIssueSummary } from './targetHelpers'

function target(overrides: Partial<TargetRecord> = {}): TargetRecord {
  return {
    target_id: 'tg_001',
    name: 'API',
    target_type: 'service',
    host: 'api.example.com',
    execution_monitoring_instance_labels: ['edge'],
    run_status: '启用',
    group: 'edge',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-26T09:00:00Z',
    ...overrides,
  }
}

describe('targetAttentionBadges', () => {
  it('keeps 正常 silent and does not stack it with maintenance or pause', () => {
    expect(targetAttentionBadges(target())).toEqual([])
    expect(targetAttentionBadges(target({ run_status: '维护中' })).map((badge) => badge.label)).toEqual(['维护中'])
    expect(targetAttentionBadges(target({ run_status: '暂停', current_health_status: '正常' })).map((badge) => badge.label)).toEqual(['暂停'])
  })

  it('shows abnormal health with coverage gap occupying an otherwise quiet cell', () => {
    expect(targetAttentionBadges(target({ current_health_status: '严重' })).map((badge) => badge.label)).toEqual(['严重'])
    expect(targetAttentionBadges(target({ execution_monitoring_instance_labels: [] })).map((badge) => badge.label)).toEqual(['覆盖缺口'])
    expect(targetIssueSummary(target({ current_primary_issue_summary: 'HTTP 探测持续失败' }))).toBe('HTTP 探测持续失败')
    expect(targetIssueSummary(target())).toBe('')
  })
})

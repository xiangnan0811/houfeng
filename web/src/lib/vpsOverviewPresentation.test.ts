import { describe, expect, it } from 'vitest'
import type { VPSOverview } from './types'

import {
  overviewAnomalyDetailLabel,
  overviewAnomalySourceLabel,
  overviewIPQualityActionLabel,
  overviewLifecycleLabel,
  overviewLocationLabel,
  overviewMonitoringSupportingDetail,
  overviewOverallLabel,
  overviewOverallPresentation,
  overviewRelationStatusLabel,
  overviewSameAssetActionTitle,
  overviewSummaryCellLabel,
  overviewSummaryDetailLabel,
  overviewUsageLabel,
} from './vpsOverviewPresentation'



function completeObservation(): VPSOverview['summary'] {
  const ready = { state: 'ready' as const, observed_at: null, last_success_at: null, reason_code: '' }
  return {
    overall: { status: 'healthy', section: ready },
    monitoring: { status: '正常', section: ready },
    ip_quality: { status: 'low', section: ready },
    renewal: { status: 'keep', section: ready },
  }
}

describe('vpsOverviewPresentation', () => {
  it('maps real overview wire enums to Chinese labels', () => {
    expect(overviewLifecycleLabel('active')).toBe('在用')
    expect(overviewUsageLabel('in_use')).toBe('承载业务')
    expect(overviewSummaryCellLabel('renewal', 'keep')).toBe('保留')
    expect(overviewOverallLabel('healthy')).toBe('总体正常')
    expect(overviewSummaryCellLabel('ip_quality', 'low')).toBe('低风险')
    expect(overviewSummaryCellLabel('monitoring', 'unlinked')).toBe('未关联')
    expect(overviewSummaryCellLabel('ip_quality', 'missing')).toBe('缺少证据')
    expect(overviewAnomalySourceLabel('monitoring')).toBe('监控')
    expect(overviewRelationStatusLabel('unavailable')).toBe('暂不可用')
    expect(overviewLocationLabel(['JP', 'Tokyo', 'Tokyo'])).toBe('JP · Tokyo · Tokyo')
  })

  it('maps classified summary and anomaly details without leaking machine tokens', () => {
    expect(overviewSummaryDetailLabel('ip_quality', 'partial')).toBe('采集不完整')
    expect(overviewSummaryDetailLabel('ip_quality', 'high')).toBe('高风险')
    expect(overviewSummaryDetailLabel('renewal', 'to_cancel')).toBe('to_cancel')
    expect(overviewLifecycleLabel('to_cancel')).toBe('待取消')
    expect(overviewAnomalyDetailLabel('lifecycle.blocker.v1', 'to_cancel')).toBe('待取消')
    expect(overviewAnomalyDetailLabel('ip_quality.risk.elevated.v1', 'high')).toBe('高风险')
    expect(overviewAnomalyDetailLabel('source.unavailable.v1', 'ip_quality, monitoring, renewal'))
      .toBe('IP 质量、监控、续费')
    expect(overviewAnomalyDetailLabel('monitoring.health.abnormal.v1', 'probe timeout')).toBe('probe timeout')
    expect(overviewSummaryDetailLabel('monitoring', 'tcp connect failed')).toBe('tcp connect failed')
    expect(overviewSummaryDetailLabel('ip_quality', 'ip_quality_disabled_has_history'))
      .toBe('存在历史报告（当前未启用）')
  })

  it('does not present incomplete running evidence as generally healthy', () => {
    const ready = { state: 'ready' as const, observed_at: null, last_success_at: null, reason_code: '' }
    const presented = overviewOverallPresentation({
      overall: { status: 'healthy', section: ready },
      monitoring: { status: 'unlinked', section: ready },
      ip_quality: {
        status: 'unknown',
        section: { state: 'unavailable', observed_at: null, last_success_at: null, reason_code: 'ip_quality_unavailable' },
      },
      renewal: { status: 'keep', section: ready },
    })
    expect(presented.label).toBe('观测不完整')
    expect(presented.tone).toBe('unknown')
    expect(presented.explanation).not.toMatch(/原始摘要|normal|总体正常/)
    expect(presented.rawLabel).toBe('总体正常')
  })

  it.each(['monitoring', 'ip_quality'] as const)('does not infer healthy runtime from stale %s evidence', (source) => {
    const summary = completeObservation()
    summary[source].section = { ...summary[source].section, state: 'stale', reason_code: 'source_timestamp_invalid' }
    const presented = overviewOverallPresentation(summary)
    expect(presented.label).toBe('观测不完整')
    expect(presented.tone).toBe('unknown')
    expect(presented.explanation).toContain('陈旧')
    summary.overall.status = 'critical'
    expect(overviewOverallPresentation(summary)).toMatchObject({ label: '严重', tone: 'alert' })
  })

  it('keeps a healthy enabled-observation scope when optional IP quality is disabled', () => {
    const summary = completeObservation()
    summary.ip_quality.status = 'not_configured'
    const presented = overviewOverallPresentation(summary)
    expect(presented.label).toBe('总体正常')
    expect(presented.tone).toBe('ok')
    expect(presented.explanation).toContain('已启用')
    expect(presented.explanation).toContain('IP 质量未启用')
    summary.ip_quality.section = { ...summary.ip_quality.section, state: 'unavailable', reason_code: 'source_unavailable' }
    expect(overviewOverallPresentation(summary)).toMatchObject({ label: '观测不完整', tone: 'unknown' })
  })

  it('names a pending cancellation plan as extra attention basis without claiming completion', () => {
    const ready = { state: 'ready' as const, observed_at: null, last_success_at: null, reason_code: '' }
    const presented = overviewOverallPresentation({
      overall: { status: 'attention', section: ready },
      monitoring: { status: '正常', section: ready },
      ip_quality: {
        status: 'unknown',
        section: { state: 'unavailable', observed_at: null, last_success_at: null, reason_code: 'ip_quality_unavailable' },
      },
      renewal: { status: 'keep', section: ready },
    }, 'to_cancel')
    expect(presented.label).toBe('需要关注')
    expect(presented.tone).toBe('notice')
    expect(presented.explanation).toContain('IP 质量暂不可用')
    expect(presented.explanation).toContain('取消计划待处理')

    expect(presented.explanation).not.toContain('已取消')
    expect(overviewOverallPresentation({
      overall: { status: 'attention', section: ready },
      monitoring: { status: '正常', section: ready },
      ip_quality: {
        status: 'unknown',
        section: { state: 'unavailable', observed_at: null, last_success_at: null, reason_code: 'ip_quality_unavailable' },
      },
      renewal: { status: 'keep', section: ready },
    }).explanation).toBe('IP 质量暂不可用。')
  })

  it('strips an exact current-asset prefix only when the remainder is a same-object action', () => {
    const asset = '东京边缘生产节点 · Tokyo Edge Node 01'
    expect(overviewSameAssetActionTitle(`${asset} · 状态检查完成`, asset)).toBe('状态检查完成')
    expect(overviewSameAssetActionTitle(`${asset} · Nginx Ingress Edge 已更新`, asset, ['Nginx Ingress Edge']))
      .toBe(`${asset} · Nginx Ingress Edge 已更新`)
    expect(overviewSameAssetActionTitle('东京边缘月付', '东京边缘')).toBe('东京边缘月付')
    expect(overviewSameAssetActionTitle(asset, asset)).toBe(asset)
  })

  it('keeps monitoring instance count and drops only a generated duplicate status', () => {
    expect(overviewMonitoringSupportingDetail('1个实例 · 正常', '正常', 1)).toBe('1个实例')
    expect(overviewMonitoringSupportingDetail('2 个实例 · 正常', '正常', 2)).toBe('2个实例')
    expect(overviewMonitoringSupportingDetail('1个实例 · 告警', '正常')).toBe('1个实例 · 告警')
    expect(overviewMonitoringSupportingDetail('', '正常', 1)).toBe('1个实例')
  })

  it('composes relation count with independent monitoring issue detail', () => {
    expect(overviewMonitoringSupportingDetail('心跳上报延迟', '关注', 1)).toBe('1个实例 · 心跳上报延迟')
    expect(overviewMonitoringSupportingDetail('probe timeout', '正常', 2)).toBe('2个实例 · probe timeout')
    expect(overviewMonitoringSupportingDetail('心跳上报延迟', '关注')).toBe('心跳上报延迟')
  })

  it('names IP quality destinations from known report, history, or missing evidence', () => {
    const ready = { state: 'ready' as const, observed_at: null, last_success_at: null, reason_code: '' }
    expect(overviewIPQualityActionLabel({ status: 'low', section: ready })).toBe('查看 IP 质量报告')
    expect(overviewIPQualityActionLabel({
      status: 'low',
      section: { state: 'unavailable', observed_at: '2026-08-19T00:00:00Z', last_success_at: '2026-08-19T00:00:00Z' },
    })).toBe('查看历史报告')
    expect(overviewIPQualityActionLabel({ status: 'not_configured', section: ready })).toBe('查看 IP 质量结果')
    expect(overviewIPQualityActionLabel({
      status: 'unknown',
      section: { state: 'unavailable', observed_at: null, last_success_at: null },
    })).toBe('查看 IP 质量结果')
  })

})

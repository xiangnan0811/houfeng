import { describe, expect, it } from 'vitest'
import type { VPSOverview } from './types'

import {
  overviewAnomalyDetailLabel,
  overviewAnomalyDetailPresentation,
  overviewMonitoringSupportingPresentation,
  overviewSummaryDetailPresentation,
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
  overviewUnmatchedStatus,
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
  it.each(['待接入', '已退役', '维护中', '暂停', 'unavailable', '未启用'])('does not promote %s monitoring to healthy', (status) => {
    const summary = completeObservation()
    summary.monitoring.status = status
    expect(overviewOverallPresentation(summary)).toMatchObject({ label: '观测不完整', tone: 'unknown' })
  })

  it('shows archived lifecycle instead of historical healthy runtime', () => {
    expect(overviewOverallPresentation(completeObservation(), 'archived')).toMatchObject({ label: '已归档', tone: 'unknown' })
  })
  it('maps real overview wire enums to Chinese labels', () => {
    expect(overviewLifecycleLabel('active')).toBe('管理中')
    expect(overviewUsageLabel('承载业务')).toBe('承载业务')
    expect(overviewSummaryCellLabel('renewal', 'keep')).toBe('继续续费')
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
    expect(overviewLifecycleLabel('archived')).toBe('已归档')
    expect(overviewAnomalyDetailLabel('lifecycle.blocker.v1', 'archived')).toBe('已归档')
    expect(overviewAnomalyDetailLabel('ip_quality.risk.elevated.v1', 'high')).toBe('高风险')
    expect(overviewAnomalyDetailLabel('source.unavailable.v1', 'ip_quality, monitoring, renewal'))
      .toBe('IP 质量、监控、续费')
    expect(overviewAnomalyDetailLabel('monitoring.health.abnormal.v1', 'probe timeout')).toBe('')
    expect(overviewAnomalyDetailPresentation('monitoring.health.abnormal.v1', 'probe timeout').diagnostics
      .map((item) => item.detail)).toEqual(['probe timeout'])
    const arbitraryDetail = '心跳上报延迟 https://status.example/delay'
    const foldedDetail = overviewAnomalyDetailPresentation('monitoring.health.abnormal.v1', arbitraryDetail)
    expect(foldedDetail.summary ?? '').not.toContain(arbitraryDetail)
    expect(foldedDetail.diagnostics.map((item) => item.detail)).toContain(arbitraryDetail)
    expect(overviewSummaryCellLabel('monitoring', '心跳超时 https://status.example/down')).toBe('状态未知')
    expect(overviewUnmatchedStatus('monitoring', '心跳超时 https://status.example/down'))
      .toBe('心跳超时 https://status.example/down')
    expect(overviewAnomalyDetailLabel('monitoring.health.abnormal.v1', '心跳超时 https://status.example/down')).toBe('')
    expect(overviewSummaryDetailLabel('monitoring', 'tcp connect failed')).toBe('')
    expect(overviewSummaryDetailPresentation('monitoring', 'tcp connect failed').diagnostics)
      .toEqual([{ label: '监控详情', detail: 'tcp connect failed' }])
    expect(overviewSummaryDetailLabel('renewal', 'to_cancel')).toBe('')
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

  it('keeps a known instance count outside an arbitrary monitoring detail', () => {
    const raw = '心跳上报延迟 https://status.example/down'
    const counted = overviewMonitoringSupportingPresentation(raw, '关注', 1)
    expect(counted.text).toBe('1个实例')
    expect(counted.text).not.toContain(raw)
    expect(counted.diagnostics.map((item) => item.detail)).toContain(raw)
    const bare = overviewMonitoringSupportingPresentation(raw, '关注')
    expect(bare.text).not.toContain(raw)
    expect(bare.diagnostics.map((item) => item.detail)).toContain(raw)
    expect(overviewMonitoringSupportingDetail('probe timeout', '正常', 2)).toBe('2个实例')
    expect(overviewMonitoringSupportingPresentation('probe timeout', '正常', 2).diagnostics
      .map((item) => item.detail)).toEqual(['probe timeout'])
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

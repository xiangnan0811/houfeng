import { describe, expect, it } from 'vitest'

import {
  overviewAnomalyDetailLabel,
  overviewAnomalySourceLabel,
  overviewLifecycleLabel,
  overviewLocationLabel,
  overviewOverallLabel,
  overviewRelationStatusLabel,
  overviewSummaryCellLabel,
  overviewSummaryDetailLabel,
  overviewUsageLabel,
} from './vpsOverviewPresentation'

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
})

import { describe, expect, it } from 'vitest'

import {
  overviewAnomalySourceLabel,
  overviewLifecycleLabel,
  overviewLocationLabel,
  overviewOverallLabel,
  overviewRelationStatusLabel,
  overviewSummaryCellLabel,
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
})

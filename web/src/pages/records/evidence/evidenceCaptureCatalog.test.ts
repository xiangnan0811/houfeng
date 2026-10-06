import { describe, expect, it } from 'vitest'

import {
  EVIDENCE_CAPTURE_KINDS,
  defaultMonitoringPrecision,
  precisionOptionsForWindow,
  sortedSelection,
} from './evidenceCaptureCatalog'

describe('evidenceCaptureCatalog', () => {
  it('与服务端默认精度的窗口边界一致', () => {
    expect(defaultMonitoringPrecision(6 * 3600)).toBe(60)
    expect(defaultMonitoringPrecision(6 * 3600 + 1)).toBe(300)
    expect(defaultMonitoringPrecision(48 * 3600)).toBe(300)
    expect(defaultMonitoringPrecision(48 * 3600 + 1)).toBe(3600)
    expect(defaultMonitoringPrecision(30 * 86_400)).toBe(3600)
    expect(defaultMonitoringPrecision(30 * 86_400 + 1)).toBe(86_400)
  })

  it('精度选项保留“自动”，并去掉比默认精度更细的值', () => {
    expect(precisionOptionsForWindow(31 * 86_400).map((option) => option.seconds)).toEqual([0, 86_400])
  })

  it('每种证据的指标与敏感字段不重复，comparison.result 不可采集', () => {
    for (const kind of EVIDENCE_CAPTURE_KINDS) {
      const metricValues = kind.metrics.map((metric) => metric.value)
      expect(new Set(metricValues).size).toBe(metricValues.length)
      const fieldValues = kind.sensitiveFields.map((field) => field.value)
      expect(new Set(fieldValues).size).toBe(fieldValues.length)
      // 选择指标的类型至少有一个常用指标，默认即可预览。
      if (kind.metrics.length > 0) expect(kind.metrics.some((metric) => metric.common)).toBe(true)
    }
    expect(EVIDENCE_CAPTURE_KINDS.map((kind) => kind.kind)).not.toContain('comparison.result')
  })

  it('提交的选择去重并按字典序排序', () => {
    expect(sortedSelection(['mem_used_pct', 'cpu_usage_pct', 'mem_used_pct'])).toEqual(['cpu_usage_pct', 'mem_used_pct'])
  })
})

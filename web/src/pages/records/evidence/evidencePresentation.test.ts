import { describe, expect, it } from 'vitest'

import {
  eventTypeLabel,
  evidenceKindLabel,
  formatDuration,
  formatMetricAxisValue,
  formatMetricValue,
  identityTypeLabel,
  lookup,
  metricLabel,
  qualityTone,
  toneOf,
} from './evidencePresentation'

describe('evidencePresentation', () => {
  it('formats monitoring metrics by unit', () => {
    expect(formatMetricValue(71.25, 'percent')).toBe('71.3%')
    expect(formatMetricValue(182.4, 'ms')).toBe('182 ms')
    expect(formatMetricValue(0.985, 'ratio')).toBe('98.5%')
    expect(formatMetricValue(12, 'days')).toBe('12 天')
    expect(formatMetricValue(1.237, 'load')).toBe('1.24')
    expect(formatMetricValue(200, 'status_code')).toBe('200')
    expect(formatMetricValue(3, 'unknown_unit')).toBe('3 unknown_unit')
    expect(formatMetricValue(3, '')).toBe('3')
  })

  it('labels known metrics, identities and durations and passes unknown values through', () => {
    expect(metricLabel('cpu_usage_pct')).toBe('CPU 使用率')
    expect(metricLabel('custom_metric')).toBe('custom_metric')
    expect(identityTypeLabel('monitoring_instance')).toBe('监控实例')
    expect(identityTypeLabel('other')).toBe('other')
    expect(formatDuration(300)).toBe('5 分钟')
    expect(formatDuration(7200)).toBe('2 小时')
    expect(formatDuration(86_400)).toBe('1 天')
    expect(formatDuration(45)).toBe('45 秒')
  })

  it('formats compact axis ticks in the same unit as the values', () => {
    expect(formatMetricAxisValue(71.4, 'percent')).toBe('71%')
    expect(formatMetricAxisValue(0.98, 'ratio')).toBe('98%')
    expect(formatMetricAxisValue(182.4, 'ms')).toBe('182ms')
    expect(formatMetricAxisValue(1024, 'bytes_per_second')).toBe('1KB/s')
    expect(formatMetricAxisValue(1536, 'bytes_per_second')).toBe('1.5KB/s')
    expect(formatMetricAxisValue(999 * 1024, 'bytes_per_second')).toBe('999KB/s')
    // 1000 MB/s 进位到 GB，刻度不出现四位整数。
    expect(formatMetricAxisValue(1000 * 1024 * 1024, 'bytes_per_second')).toBe('1GB/s')
    expect(formatMetricAxisValue(8 * 1024 ** 3, 'bytes')).toBe('8GB')
    expect(formatMetricAxisValue(45, 'seconds')).toBe('45秒')
    expect(formatMetricAxisValue(90, 'seconds')).toBe('1.5分')
    expect(formatMetricAxisValue(5400, 'seconds')).toBe('1.5时')
    expect(formatMetricAxisValue(864_000, 'seconds')).toBe('10天')
    // 舍入跨过进位边界时按舍入后的值进位。
    expect(formatMetricAxisValue(999.49 * 1024, 'bytes_per_second')).toBe('999KB/s')
    expect(formatMetricAxisValue(999.5 * 1024, 'bytes_per_second')).toBe('1MB/s')
    expect(formatMetricAxisValue(999.9 * 1024 ** 2, 'bytes_per_second')).toBe('1GB/s')
    expect(formatMetricAxisValue(-12 * 1024 ** 2, 'bytes')).toBe('-12MB')
    // 负的时长刻度（图表向下扩展值域）同样换算单位。
    expect(formatMetricAxisValue(-3_153_600, 'seconds')).toBe('-37天')
    expect(formatMetricAxisValue(59.6, 'seconds')).toBe('1分')
    expect(formatMetricAxisValue(1000 * 86_400, 'seconds')).toBe('2.7年')
    const samples = [0, 0.5, 9.95, 59.6, 999, 999.49, 999.5, 999.9, 1000, 1023.9, 999_999, 1e6, 1e9, 1e12, 1e15]
    for (const value of [...samples, ...samples.map((sample) => -sample)]) {
      for (const unit of ['bytes', 'bytes_per_second']) {
        expect(formatMetricAxisValue(value, unit).length).toBeLessThanOrEqual(8)
        expect(formatMetricAxisValue(value, unit)).not.toMatch(/\d{4}/)
      }
      // 运行时长在 30 年量级内保持至多三位数。
      if (Math.abs(value) <= 1e9) expect(formatMetricAxisValue(value, 'seconds')).not.toMatch(/\d{4}/)
    }
    expect(formatMetricAxisValue(404, 'status_code')).toBe('404')
    expect(formatMetricAxisValue(1.234, 'load')).toBe('1.2')
  })

  it('never resolves server strings through prototype properties', () => {
    for (const hostile of ['__proto__', 'constructor', 'toString', 'hasOwnProperty']) {
      expect(lookup({ ok: '正常' }, hostile)).toBe(hostile)
      expect(toneOf({ ok: 'normal' }, hostile)).toBe('neutral')
      expect(identityTypeLabel(hostile)).toBe(hostile)
      expect(evidenceKindLabel(hostile)).toBe(hostile)
      expect(metricLabel(hostile)).toBe(hostile)
      expect(eventTypeLabel(hostile)).toBe(hostile)
    }
    expect(eventTypeLabel('monitoring_instance_binding_pending_rejected')).toBe('拒绝待确认指纹')
    expect(eventTypeLabel('event_corrected')).toBe('人工更正')
  })

  it('maps tones with neutral fallback', () => {
    expect(qualityTone('complete')).toBe('normal')
    expect(qualityTone('degraded')).toBe('alert')
    expect(toneOf({ ok: 'normal' }, 'missing')).toBe('neutral')
    expect(lookup({ ok: '正常' }, 'raw')).toBe('raw')
  })
})

import { describe, expect, it } from 'vitest'

import { formatDate, formatElapsedSince, formatMoney, formatOptional, formatUptime } from './format'

describe('format helpers', () => {
  it('formats optional values for dense operational tables', () => {
    expect(formatOptional(null)).toBe('—')
    expect(formatOptional(undefined)).toBe('—')
    expect(formatOptional('')).toBe('—')
    expect(formatOptional('Tokyo')).toBe('Tokyo')
    expect(formatOptional(22)).toBe('22')
  })

  it('formats date-only strings without timezone conversion', () => {
    expect(formatDate(null)).toBe('—')
    expect(formatDate('2026-05-09')).toBe('2026-05-09')
  })

  it('formats money with a stable currency prefix', () => {
    expect(formatMoney(12, 'USD')).toBe('USD 12.00')
    expect(formatMoney(Number.NaN, '')).toBe('--- 0.00')
  })

  it('formats a just-started uptime without a zero-minute reading', () => {
    expect(formatUptime(0)).toBe('不足 1 分钟')
    expect(formatUptime(59)).toBe('不足 1 分钟')
    expect(formatUptime(60)).toBe('1分钟')
    expect(formatUptime(3600)).toBe('1小时 0分钟')
  })

  it('formats elapsed duration without a trailing 前', () => {
    const now = new Date('2026-04-24T10:00:00Z')
    expect(formatElapsedSince('2026-04-24T09:59:50Z', now)).toBe('不足 1 分钟')
    expect(formatElapsedSince('2026-04-24T09:10:00Z', now)).toBe('50 分钟')
    expect(formatElapsedSince('2026-04-24T08:00:00Z', now)).toBe('2 小时')
    expect(formatElapsedSince('2026-04-22T10:00:00Z', now)).toBe('2 天')
    expect(formatElapsedSince(null, now)).toBe('—')
    expect(formatElapsedSince('not-a-date', now)).toBe('—')
  })
})

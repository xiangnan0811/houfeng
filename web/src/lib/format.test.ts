import { describe, expect, it } from 'vitest'

import { formatDate, formatMoney, formatOptional } from './format'

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
})

import { afterEach, describe, expect, it } from 'vitest'

import { daysUntilDate, renewalTimingLabel } from './assetPageUtils'

const originalTZ = process.env.TZ

afterEach(() => {
  if (originalTZ === undefined) delete process.env.TZ
  else process.env.TZ = originalTZ
})

describe('daysUntilDate', () => {
  it('reads YYYY-MM-DD renewal dates as local calendar days west of UTC', () => {
    process.env.TZ = 'America/Los_Angeles'
    const now = new Date(2026, 9, 2, 9, 0)
    // 时区切换必须真的生效：PDT 偏移 420 分钟，且原生解析会把日历日落到前一天。
    expect(now.getTimezoneOffset()).toBe(420)
    expect(new Date('2026-10-06').getDate()).toBe(5)
    expect(daysUntilDate('2026-10-02', now)).toBe(0)
    expect(daysUntilDate('2026-10-06', now)).toBe(4)
    expect(daysUntilDate('2026-09-30', now)).toBe(-2)
    expect(renewalTimingLabel(daysUntilDate('2026-10-02', now))).toBe('今天续费')
  })

  it('reads YYYY-MM-DD renewal dates as local calendar days east of UTC', () => {
    process.env.TZ = 'Asia/Shanghai'
    const now = new Date(2026, 9, 2, 1, 30)
    expect(now.getTimezoneOffset()).toBe(-480)
    expect(daysUntilDate('2026-10-02', now)).toBe(0)
    expect(daysUntilDate('2026-10-06', now)).toBe(4)
  })

  it('counts whole calendar days across daylight-saving transitions', () => {
    process.env.TZ = 'America/New_York'
    // 夏令时边界两侧的偏移必须不同，否则用例在无夏令时的时区里会假通过。
    expect(new Date(2026, 9, 31).getTimezoneOffset()).toBe(240)
    expect(new Date(2026, 10, 2).getTimezoneOffset()).toBe(300)
    expect(new Date(2026, 2, 7).getTimezoneOffset()).toBe(300)
    expect(new Date(2026, 2, 9).getTimezoneOffset()).toBe(240)
    // 11 月 1 日回拨一小时，当天有 25 小时。
    expect(daysUntilDate('2026-11-02', new Date(2026, 9, 31, 12, 0))).toBe(2)
    // 3 月 8 日拨快一小时，当天只有 23 小时。
    expect(daysUntilDate('2026-03-09', new Date(2026, 2, 7, 12, 0))).toBe(2)
  })

  it('keeps full timestamps on their local calendar day and rejects invalid input', () => {
    process.env.TZ = 'America/New_York'
    const now = new Date(2026, 9, 2, 9, 0)
    expect(daysUntilDate('2026-10-03T23:30:00-04:00', now)).toBe(1)
    expect(daysUntilDate('not-a-date', now)).toBeNull()
    expect(daysUntilDate(null, now)).toBeNull()
  })
})

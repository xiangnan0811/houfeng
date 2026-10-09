import { afterEach, describe, expect, it } from 'vitest'

import { daysUntilDate, renewalReferenceTime, renewalTimingLabel } from './assetPageUtils'

const originalTZ = process.env.TZ

afterEach(() => {
  if (originalTZ === undefined) delete process.env.TZ
  else process.env.TZ = originalTZ
})

describe('daysUntilDate', () => {
  // 剩余天数与后端 90 天窗口、30 天计数和逾期判定同口径：续费日与“今天”都取 UTC 日历日。
  it('uses the UTC calendar day west of UTC', () => {
    process.env.TZ = 'America/Los_Angeles'
    const now = new Date(2026, 9, 2, 9, 0)
    // 时区切换必须真的生效：PDT 偏移 420 分钟。
    expect(now.getTimezoneOffset()).toBe(420)
    expect(daysUntilDate('2026-10-02', now)).toBe(0)
    expect(daysUntilDate('2026-10-06', now)).toBe(4)
    expect(daysUntilDate('2026-09-30', now)).toBe(-2)
    expect(renewalTimingLabel(daysUntilDate('2026-10-02', now))).toBe('今天续费')
    // 洛杉矶晚上 18:00 已是 UTC 次日：后端的“今天”已经前进一天。
    expect(daysUntilDate('2026-10-02', new Date(2026, 9, 2, 18, 0))).toBe(-1)
  })

  it('uses the UTC calendar day east of UTC', () => {
    process.env.TZ = 'Asia/Shanghai'
    // 上海 10-02 01:30 仍是 UTC 10-01。
    const now = new Date(2026, 9, 2, 1, 30)
    expect(now.getTimezoneOffset()).toBe(-480)
    expect(daysUntilDate('2026-10-02', now)).toBe(1)
    expect(daysUntilDate('2026-10-01', now)).toBe(0)
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

  it('folds full timestamps to their UTC day and rejects invalid input', () => {
    process.env.TZ = 'America/New_York'
    const now = new Date(2026, 9, 2, 9, 0)
    // 10-03 23:30 -04:00 = UTC 10-04 03:30。
    expect(daysUntilDate('2026-10-03T23:30:00-04:00', now)).toBe(2)
    expect(daysUntilDate('2026-10-03T00:00:00Z', now)).toBe(1)
    expect(daysUntilDate('2026-02-30', now)).toBeNull()
    expect(daysUntilDate('2026-10-03junk', now)).toBeNull()
    expect(daysUntilDate('not-a-date', now)).toBeNull()
    expect(daysUntilDate(null, now)).toBeNull()
    expect(daysUntilDate('2026-10-03', Number.NaN)).toBeNull()
  })
})

describe('renewalReferenceTime', () => {
  const pageOpenedAt = Date.parse('2026-10-05T12:00:00Z')

  it('uses the overview generation time only while the overview is usable', () => {
    expect(renewalReferenceTime('2026-10-02T04:00:00Z', true, pageOpenedAt)).toBe(Date.parse('2026-10-02T04:00:00Z'))
    // 刷新失败时页面仍保留旧摘要，但它已不可用，不能再当作“今天”。
    expect(renewalReferenceTime('2026-10-02T04:00:00Z', false, pageOpenedAt)).toBe(pageOpenedAt)
    expect(renewalReferenceTime('invalid', true, pageOpenedAt)).toBe(pageOpenedAt)
    expect(renewalReferenceTime(undefined, true, pageOpenedAt)).toBe(pageOpenedAt)
  })
})

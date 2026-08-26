import { describe, expect, it } from 'vitest'

import { ApiError } from '../../lib/apiRequest'
import { describeManagementError } from './vpsManagementHelpers'

describe('describeManagementError', () => {
  it('keeps stale preview, blocked lifecycle, and CAS conflict distinct', () => {
    expect(describeManagementError(new ApiError(409, 'cancellation preview stale'), 'fallback'))
      .toBe('影响范围已变化，请重新加载预览后再确认')
    expect(describeManagementError(new ApiError(409, 'lifecycle action blocked'), 'fallback'))
      .toBe('当前生命周期状态不允许此操作')
    expect(describeManagementError(new ApiError(409, 'vps updated'), 'fallback'))
      .toBe('VPS 已被其他操作更新，请重新加载后确认')
  })
})

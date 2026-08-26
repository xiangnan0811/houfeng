import { describe, expect, it } from 'vitest'

import { ApiError } from '../../lib/apiRequest'
import {
  describeManagementError,
  isIdempotencyKeyReused,
  parseOverviewWorkbench,
  subscriptionLinkageAction,
} from './vpsManagementHelpers'

describe('describeManagementError', () => {
  it('branches only on allowlisted 409 codes', () => {
    expect(describeManagementError(new ApiError(409, 'cancellation preview stale', {
      code: 'cancellation_preview_stale',
    }), 'fallback')).toBe('影响范围已变化，请重新加载预览后再确认')
    expect(describeManagementError(new ApiError(409, 'lifecycle action blocked', {
      code: 'lifecycle_action_blocked',
    }), 'fallback')).toBe('当前生命周期状态不允许此操作')
    expect(describeManagementError(new ApiError(409, 'vps updated', {
      code: 'vps_asset_conflict',
    }), 'fallback')).toBe('VPS 已被其他操作更新，请先加载最新版本后再保存')
    expect(describeManagementError(new ApiError(409, 'cancellation preview stale'), 'fallback'))
      .toBe('cancellation preview stale')
  })
})

describe('isIdempotencyKeyReused', () => {
  it('matches only the coded 409 reused-key contract', () => {
    expect(isIdempotencyKeyReused(new ApiError(409, 'idempotency key reused', {
      code: 'idempotency_key_reused',
    }))).toBe(true)
    expect(isIdempotencyKeyReused(new ApiError(409, 'vps updated', {
      code: 'vps_asset_conflict',
    }))).toBe(false)
    expect(isIdempotencyKeyReused(new TypeError('Failed to fetch'))).toBe(false)
  })
})

describe('parseOverviewWorkbench', () => {
  it('allowlists cancellation and subscription only', () => {
    expect(parseOverviewWorkbench('cancellation')).toBe('cancellation')
    expect(parseOverviewWorkbench('subscription')).toBe('subscription')
    expect(parseOverviewWorkbench('decision')).toBeNull()
    expect(parseOverviewWorkbench('archive')).toBeNull()
    expect(parseOverviewWorkbench('facts')).toBeNull()
    expect(parseOverviewWorkbench(null)).toBeNull()
  })
})

describe('subscriptionLinkageAction', () => {
  it('offers continue-cancel after a cancellation-like renewal even when a subscription exists', () => {
    expect(subscriptionLinkageAction({
      status: 'subscription_updated',
      message: '已关联订阅',
      subscription_id: 'sub_001',
      candidate_count: 1,
      updated: true,
    }, 'vps_001', 'cancel')).toEqual({
      to: '/vps/vps_001?workbench=cancellation',
      label: '继续取消 / 退役',
      panel: 'cancellation',
    })
  })
})

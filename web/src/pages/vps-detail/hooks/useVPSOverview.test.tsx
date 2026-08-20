import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import * as recordsApi from '../../../lib/recordsApi'
import type { VPSOverview } from '../../../lib/types'
import { useVPSOverview } from './useVPSOverview'

function overview(): VPSOverview {
  return {
    generated_at: '2026-08-20T00:00:00Z',
    identity: {
      vps_id: 'vps_001',
      display_name: 'Edge',
      provider_name: 'Example',
      product_name: 'VPS',
      country: 'JP',
      region: 'Tokyo',
      city: 'Tokyo',
      datacenter: 'TK1',
      ipv4: '192.0.2.1',
      ipv6: '',
      lifecycle_status: '在用',
      usage_status: '生产',
      renewal_decision: '续费',
      importance: '高',
      labels: [],
      updated_at: '2026-08-20T00:00:00Z',
    },
    anomalies: [],
    summary: {
      overall: { status: '正常', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      monitoring: { status: '正常', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      ip_quality: { status: '低风险', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      renewal: { status: '续费', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
    },
    recent_activity: {
      section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      items: [],
    },
    facts: [],
    relations: [],
    capabilities: ['records_v2_read'],
  }
}

describe('useVPSOverview', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads overview and ignores stale responses', async () => {
    let resolveFirst!: (value: VPSOverview) => void
    const first = new Promise<VPSOverview>((resolve) => {
      resolveFirst = resolve
    })
    const get = vi.spyOn(recordsApi, 'getVPSOverview')
      .mockImplementationOnce(() => first)
      .mockResolvedValueOnce({
        ...overview(),
        identity: { ...overview().identity, display_name: 'Second' },
      })

    const { result, rerender } = renderHook(
      ({ id }) => useVPSOverview(id),
      { initialProps: { id: 'vps_001' } },
    )
    rerender({ id: 'vps_002' })
    resolveFirst(overview())

    await waitFor(() => expect(result.current.state.overview?.identity.display_name).toBe('Second'))
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('maps missing VPS to not_found without falling back', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockRejectedValue(
      new ApiError(404, 'missing', { code: 'resource_not_found' }),
    )
    const { result } = renderHook(() => useVPSOverview('vps_missing'))
    await waitFor(() => expect(result.current.state.status).toBe('not_found'))
  })

  it('seeds from an initial overview without an immediate refetch', async () => {
    const get = vi.spyOn(recordsApi, 'getVPSOverview')
    const seeded = overview()
    const { result } = renderHook(() => useVPSOverview('vps_001', seeded))
    expect(result.current.state.status).toBe('ready')
    expect(result.current.state.overview?.identity.display_name).toBe('Edge')
    await waitFor(() => expect(get).not.toHaveBeenCalled())
  })
})

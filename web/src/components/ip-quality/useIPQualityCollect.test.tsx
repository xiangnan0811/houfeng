import { act, renderHook, waitFor } from '@testing-library/react'
import { StrictMode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { IPQualityCollectStatus } from '../../lib/types'

const api = vi.hoisted(() => ({
  getVPSIPQualityCollectStatus: vi.fn(),
  requestVPSIPQualityCollect: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  getVPSIPQualityCollectStatus: api.getVPSIPQualityCollectStatus,
  requestVPSIPQualityCollect: api.requestVPSIPQualityCollect,
}))

import { COLLECT_POLL_INTERVAL_MS, useIPQualityCollect } from './useIPQualityCollect'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

function status(requestStatus?: 'pending' | 'dispatched' | 'completed' | 'expired', extra: Partial<IPQualityCollectStatus> = {}): IPQualityCollectStatus {
  return {
    enabled: true,
    available: true,
    monitoring_instance_id: 'mi_001',
    ...(requestStatus ? {
      request: {
        request_id: 'ipqc_001',
        monitoring_instance_id: 'mi_001',
        status: requestStatus,
        requested_at: '2026-09-30T08:00:00Z',
        expires_at: '2026-09-30T08:10:00Z',
      },
    } : {}),
    ...extra,
  }
}

describe('useIPQualityCollect', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    api.getVPSIPQualityCollectStatus.mockReset()
    api.requestVPSIPQualityCollect.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('ignores an initial status response that arrives after the POST result', async () => {
    const initial = deferred<IPQualityCollectStatus>()
    api.getVPSIPQualityCollectStatus.mockReturnValueOnce(initial.promise).mockResolvedValue(status('dispatched'))
    api.requestVPSIPQualityCollect.mockResolvedValue(status('pending'))
    const { result } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))

    act(() => result.current.start())
    await waitFor(() => expect(result.current.active).toBe(true))
    await act(async () => { initial.resolve(status()) })

    expect(result.current.active).toBe(true)
    expect(result.current.watchedRequestId).toBe('ipqc_001')
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    expect(result.current.status?.request?.status).toBe('dispatched')
  })

  it('refreshes once when the watched request completes and then stops polling', async () => {
    const onCompleted = vi.fn()
    api.getVPSIPQualityCollectStatus
      .mockResolvedValueOnce(status('dispatched'))
      .mockResolvedValue(status('completed'))
    const { result } = renderHook(() => useIPQualityCollect('vps_001', onCompleted))

    await waitFor(() => expect(result.current.active).toBe(true))
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(onCompleted).toHaveBeenCalledTimes(1))

    const calls = api.getVPSIPQualityCollectStatus.mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS * 3) })
    expect(api.getVPSIPQualityCollectStatus).toHaveBeenCalledTimes(calls)
    expect(onCompleted).toHaveBeenCalledTimes(1)
  })

  it('stops polling on expiry, when collection becomes unavailable, and on unmount', async () => {
    api.getVPSIPQualityCollectStatus
      .mockResolvedValueOnce(status('pending'))
      .mockResolvedValueOnce(status('pending', { available: false, unavailable_reason: 'monitoring_paused' }))
      .mockResolvedValue(status('expired'))
    const { result, unmount } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))

    await waitFor(() => expect(result.current.active).toBe(true))
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(result.current.active).toBe(false))
    const calls = api.getVPSIPQualityCollectStatus.mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS * 3) })
    expect(api.getVPSIPQualityCollectStatus).toHaveBeenCalledTimes(calls)
    unmount()
  })

  it('keeps polling after a transient poll failure', async () => {
    api.getVPSIPQualityCollectStatus
      .mockResolvedValueOnce(status('dispatched'))
      .mockRejectedValueOnce(new Error('network'))
      .mockResolvedValue(status('completed'))
    const onCompleted = vi.fn()
    const { result } = renderHook(() => useIPQualityCollect('vps_001', onCompleted))

    await waitFor(() => expect(result.current.active).toBe(true))
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(onCompleted).toHaveBeenCalledTimes(1))
  })

  it('does not read status until the report is ready and ignores repeated starts while busy', async () => {
    api.getVPSIPQualityCollectStatus.mockResolvedValue(status())
    api.requestVPSIPQualityCollect.mockReturnValue(new Promise(() => undefined))
    const { result, rerender } = renderHook(({ enabled }) => useIPQualityCollect('vps_001', vi.fn(), enabled), {
      initialProps: { enabled: false },
    })
    expect(api.getVPSIPQualityCollectStatus).not.toHaveBeenCalled()

    rerender({ enabled: true })
    await waitFor(() => expect(result.current.status?.available).toBe(true))
    act(() => result.current.start())
    act(() => result.current.start())
    expect(api.requestVPSIPQualityCollect).toHaveBeenCalledTimes(1)
  })

  it('keeps the button busy when an older GET returns while the POST is still pending', async () => {
    const initial = deferred<IPQualityCollectStatus>()
    const post = deferred<IPQualityCollectStatus>()
    api.getVPSIPQualityCollectStatus.mockReturnValueOnce(initial.promise).mockResolvedValue(status('pending'))
    api.requestVPSIPQualityCollect.mockReturnValue(post.promise)
    const { result } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))

    act(() => result.current.start())
    await act(async () => { initial.resolve(status()) })
    expect(result.current.submitting).toBe(true)
    act(() => result.current.start())
    expect(api.requestVPSIPQualityCollect).toHaveBeenCalledTimes(1)

    await act(async () => { post.resolve(status('pending')) })
    expect(result.current.submitting).toBe(false)
    expect(result.current.active).toBe(true)
  })

  it('does not read status again when a POST pending at unmount fails afterwards', async () => {
    let rejectPost!: (error: unknown) => void
    api.getVPSIPQualityCollectStatus.mockResolvedValue(status())
    api.requestVPSIPQualityCollect.mockReturnValue(new Promise<IPQualityCollectStatus>((_, reject) => { rejectPost = reject }))
    const { result, unmount } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))
    await waitFor(() => expect(api.getVPSIPQualityCollectStatus).toHaveBeenCalledTimes(1))

    act(() => result.current.start())
    unmount()
    await act(async () => {
      rejectPost(new Error('network down'))
      await Promise.resolve()
    })
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS * 2) })
    // 离开页面后失败的 POST 不再补读状态。
    expect(api.getVPSIPQualityCollectStatus).toHaveBeenCalledTimes(1)
  })

  it('keeps reading status and applying the POST result under StrictMode remounts', async () => {
    api.getVPSIPQualityCollectStatus.mockResolvedValue(status())
    api.requestVPSIPQualityCollect.mockResolvedValue(status('pending'))
    const { result } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()), { wrapper: StrictMode })
    await waitFor(() => expect(result.current.status).toEqual(status()))
    act(() => result.current.start())
    await waitFor(() => expect(result.current.status?.request?.status).toBe('pending'))
    expect(result.current.submitting).toBe(false)
    expect(result.current.watchedRequestId).toBe('ipqc_001')
  })

  it('ignores callbacks from the previous VPS after switching pages', async () => {
    const post = deferred<IPQualityCollectStatus>()
    const other = deferred<IPQualityCollectStatus>()
    api.getVPSIPQualityCollectStatus
      .mockResolvedValueOnce(status())
      .mockReturnValueOnce(other.promise)
      .mockResolvedValue(status('completed'))
    api.requestVPSIPQualityCollect.mockReturnValue(post.promise)
    const { result, rerender } = renderHook(({ vpsId }) => useIPQualityCollect(vpsId, vi.fn()), {
      initialProps: { vpsId: 'vps_001' },
    })
    await waitFor(() => expect(result.current.status?.available).toBe(true))
    act(() => result.current.start())

    rerender({ vpsId: 'vps_002' })
    const callsAfterSwitch = api.getVPSIPQualityCollectStatus.mock.calls.length
    await act(async () => { post.resolve(status('dispatched')) })
    expect(api.getVPSIPQualityCollectStatus.mock.calls.length).toBe(callsAfterSwitch)
    expect(result.current.status).toBeNull()
    expect(result.current.submitting).toBe(false)

    await act(async () => { other.resolve(status('dispatched', { monitoring_instance_id: 'mi_002' })) })
    expect(result.current.status?.monitoring_instance_id).toBe('mi_002')
    expect(result.current.active).toBe(true)
  })

  it('ignores callbacks from an earlier visit after returning to the same VPS', async () => {
    const firstPost = deferred<IPQualityCollectStatus>()
    const secondPost = deferred<IPQualityCollectStatus>()
    api.getVPSIPQualityCollectStatus.mockResolvedValue(status())
    api.requestVPSIPQualityCollect.mockReturnValueOnce(firstPost.promise).mockReturnValueOnce(secondPost.promise)
    const { result, rerender } = renderHook(({ vpsId }) => useIPQualityCollect(vpsId, vi.fn()), {
      initialProps: { vpsId: 'vps_001' },
    })
    await waitFor(() => expect(result.current.status?.available).toBe(true))
    act(() => result.current.start())

    rerender({ vpsId: 'vps_002' })
    await waitFor(() => expect(result.current.status?.available).toBe(true))
    rerender({ vpsId: 'vps_001' })
    await waitFor(() => expect(result.current.status?.available).toBe(true))
    act(() => result.current.start())
    expect(result.current.submitting).toBe(true)

    // 第一次访问的 POST 迟到：不能结束本次提交，也不能写入状态。
    await act(async () => { firstPost.resolve(status('dispatched')) })
    expect(result.current.submitting).toBe(true)
    expect(result.current.status?.request).toBeUndefined()

    await act(async () => { secondPost.resolve(status('pending')) })
    expect(result.current.submitting).toBe(false)
    expect(result.current.status?.request?.status).toBe('pending')
  })
})


import { act, renderHook, waitFor } from '@testing-library/react'
import { StrictMode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../lib/api'
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

function status(requestStatus?: 'pending' | 'dispatched' | 'completed' | 'expired' | 'failed', extra: Partial<IPQualityCollectStatus> = {}): IPQualityCollectStatus {
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

  it('blocks submission while the initial status read is pending and then applies its result', async () => {
    const initial = deferred<IPQualityCollectStatus>()
    api.getVPSIPQualityCollectStatus.mockReturnValueOnce(initial.promise).mockResolvedValue(status('dispatched'))
    api.requestVPSIPQualityCollect.mockResolvedValue(status('pending'))
    const { result } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))

    // 采集条件未知：检查中，start 不发 POST。
    expect(result.current.checking).toBe(true)
    act(() => result.current.start([4, 6]))
    expect(api.requestVPSIPQualityCollect).not.toHaveBeenCalled()
    expect(result.current.submitting).toBe(false)

    // 首次读取返回进行中的请求：直接接手，不再需要也不允许重复提交。
    await act(async () => { initial.resolve(status('dispatched')) })
    expect(result.current.checking).toBe(false)
    expect(result.current.active).toBe(true)
    expect(result.current.watchedRequestId).toBe('ipqc_001')
    act(() => result.current.start([4, 6]))
    expect(api.requestVPSIPQualityCollect).not.toHaveBeenCalled()
  })

  it('stops checking after a failed initial read and lets the POST give a definite result', async () => {
    api.getVPSIPQualityCollectStatus.mockRejectedValueOnce(new Error('network')).mockResolvedValue(status('pending'))
    api.requestVPSIPQualityCollect.mockRejectedValue(new ApiError(409, 'monitoring instance monitoring is paused'))
    const { result } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))

    await waitFor(() => expect(result.current.checking).toBe(false))
    // 读取失败：没有状态，也不再声称检查中；入口恢复可用。
    expect(result.current.status).toBeNull()
    act(() => result.current.start([4, 6]))
    expect(api.requestVPSIPQualityCollect).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(result.current.status?.request?.status).toBe('pending'))
  })

  it('does not let a failed read from the previous VPS end checking for the next VPS', async () => {
    let rejectFirst!: (error: unknown) => void
    const firstRead = new Promise<IPQualityCollectStatus>((_, reject) => { rejectFirst = reject })
    const second = deferred<IPQualityCollectStatus>()
    api.getVPSIPQualityCollectStatus.mockReturnValueOnce(firstRead).mockReturnValueOnce(second.promise)
    const { result, rerender } = renderHook(({ vpsId }) => useIPQualityCollect(vpsId, vi.fn()), { initialProps: { vpsId: 'vps_001' } })

    rerender({ vpsId: 'vps_002' })
    expect(result.current.checking).toBe(true)
    await act(async () => { rejectFirst(new Error('network')) })
    // 旧会话的失败不能解锁另一台 VPS 的入口。
    expect(result.current.checking).toBe(true)
    act(() => result.current.start([4, 6]))
    expect(api.requestVPSIPQualityCollect).not.toHaveBeenCalled()
    await act(async () => { second.resolve(status()) })
    expect(result.current.checking).toBe(false)
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
    // 完成回调拿到请求本身，页面据其族集合决定是否刷新当前族。
    expect(onCompleted).toHaveBeenCalledWith(expect.objectContaining({ request_id: 'ipqc_001', status: 'completed' }))

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
    act(() => result.current.start([4]))
    act(() => result.current.start([6]))
    expect(api.requestVPSIPQualityCollect).toHaveBeenCalledTimes(1)
  })

  it('keeps the button busy while the POST is pending and ignores a repeated start', async () => {
    const post = deferred<IPQualityCollectStatus>()
    api.getVPSIPQualityCollectStatus.mockResolvedValueOnce(status()).mockResolvedValue(status('pending'))
    api.requestVPSIPQualityCollect.mockReturnValue(post.promise)
    const { result } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))
    await waitFor(() => expect(result.current.checking).toBe(false))

    act(() => result.current.start([4, 6]))
    expect(result.current.submitting).toBe(true)
    act(() => result.current.start([4, 6]))
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

    act(() => result.current.start([4, 6]))
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
    act(() => result.current.start([4, 6]))
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
    act(() => result.current.start([4, 6]))

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
    act(() => result.current.start([4, 6]))

    rerender({ vpsId: 'vps_002' })
    await waitFor(() => expect(result.current.status?.available).toBe(true))
    rerender({ vpsId: 'vps_001' })
    await waitFor(() => expect(result.current.status?.available).toBe(true))
    act(() => result.current.start([4, 6]))
    expect(result.current.submitting).toBe(true)

    // 第一次访问的 POST 迟到：不能结束本次提交，也不能写入状态。
    await act(async () => { firstPost.resolve(status('dispatched')) })
    expect(result.current.submitting).toBe(true)
    expect(result.current.status?.request).toBeUndefined()

    await act(async () => { secondPost.resolve(status('pending')) })
    expect(result.current.submitting).toBe(false)
    expect(result.current.status?.request?.status).toBe('pending')
  })

  it('posts the chosen family set and stops polling on a failed request without refreshing', async () => {
    const onCompleted = vi.fn()
    api.getVPSIPQualityCollectStatus
      .mockResolvedValueOnce(status())
      .mockResolvedValue(status('failed', {}))
    api.requestVPSIPQualityCollect.mockResolvedValue(status('pending'))
    const { result } = renderHook(() => useIPQualityCollect('vps_001', onCompleted))
    await waitFor(() => expect(result.current.status?.available).toBe(true))

    act(() => result.current.start([6]))
    expect(api.requestVPSIPQualityCollect).toHaveBeenCalledWith('vps_001', [6])
    await waitFor(() => expect(result.current.active).toBe(true))
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(result.current.status?.request?.status).toBe('failed'))
    expect(result.current.active).toBe(false)
    const calls = api.getVPSIPQualityCollectStatus.mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS * 3) })
    expect(api.getVPSIPQualityCollectStatus).toHaveBeenCalledTimes(calls)
    expect(onCompleted).not.toHaveBeenCalled()
  })

  it('adopts the in-flight request of another scope after a 409 instead of showing a generic error', async () => {
    const inFlight = status('dispatched')
    inFlight.request!.requested_ip_versions = [4, 6]
    api.getVPSIPQualityCollectStatus.mockResolvedValueOnce(status()).mockResolvedValue(inFlight)
    api.requestVPSIPQualityCollect.mockRejectedValue(new ApiError(409, 'collection_scope_conflict'))
    const { result } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))
    await waitFor(() => expect(result.current.status?.available).toBe(true))

    act(() => result.current.start([4]))
    await waitFor(() => expect(result.current.status?.request?.requested_ip_versions).toEqual([4, 6]))
    expect(result.current.error).toBeNull()
    expect(result.current.active).toBe(true)
    expect(result.current.watchedRequestId).toBe('ipqc_001')
  })
})


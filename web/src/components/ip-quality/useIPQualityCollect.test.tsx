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
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
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
    // 接手服务端真实范围后继续轮询，不额外 POST。
    const reads = api.getVPSIPQualityCollectStatus.mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    expect(api.getVPSIPQualityCollectStatus.mock.calls.length).toBeGreaterThan(reads)
    expect(api.requestVPSIPQualityCollect).toHaveBeenCalledTimes(1)
  })

  it('treats an idle follow-up read after a 409 as a normal state without extra notices', async () => {
    api.getVPSIPQualityCollectStatus.mockResolvedValue(status())
    api.requestVPSIPQualityCollect.mockRejectedValue(new ApiError(409, 'collect_unavailable'))
    const { result } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))
    await waitFor(() => expect(result.current.status?.available).toBe(true))

    act(() => result.current.start([4, 6]))
    await waitFor(() => expect(api.getVPSIPQualityCollectStatus).toHaveBeenCalledTimes(2))
    await act(async () => { await Promise.resolve() })
    expect(result.current.submitting).toBe(false)
    expect(result.current.error).toBeNull()
    expect(result.current.active).toBe(false)
  })

  describe('POST 409 follow-up status read', () => {
    const CONFLICT_READ_FAILED = '本次采集未能发起，当前状态读取失败，请稍后重试'
    const conflict = () => new ApiError(409, 'collection_scope_conflict')
    function requestStatus(requestId: string, requestStatus: 'pending' | 'dispatched' | 'failed'): IPQualityCollectStatus {
      const next = status(requestStatus)
      next.request = { ...next.request!, request_id: requestId }
      return next
    }

    it.each([
      ['an idle status', 'idle', new Error('network')],
      ['an idle status', 'idle', new ApiError(503, 'service unavailable')],
      ['a failed initial read', 'null', new Error('network')],
      ['a failed initial read', 'null', new ApiError(503, 'service unavailable')],
    ] as const)('gives a safe recovery hint when the latest follow-up read fails after %s (%s)', async (_label, initial, readError) => {
      if (initial === 'idle') api.getVPSIPQualityCollectStatus.mockResolvedValueOnce(status())
      else api.getVPSIPQualityCollectStatus.mockRejectedValueOnce(new Error('network'))
      api.getVPSIPQualityCollectStatus.mockRejectedValueOnce(readError).mockResolvedValue(requestStatus('ipqc_retry', 'pending'))
      api.requestVPSIPQualityCollect.mockRejectedValueOnce(conflict()).mockResolvedValue(requestStatus('ipqc_retry', 'pending'))
      const { result } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))
      await waitFor(() => expect(result.current.checking).toBe(false))
      expect(result.current.status === null).toBe(initial === 'null')

      act(() => result.current.start([4]))
      await waitFor(() => expect(result.current.error).toBe(CONFLICT_READ_FAILED))
      expect(result.current.submitting).toBe(false)
      expect(result.current.checking).toBe(false)
      expect(result.current.active).toBe(false)
      // 不自动重试 POST；用户可手动再试，并接手新请求的进度。
      await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS * 2) })
      expect(api.requestVPSIPQualityCollect).toHaveBeenCalledTimes(1)
      act(() => result.current.start([4]))
      expect(result.current.error).toBeNull()
      await waitFor(() => expect(result.current.active).toBe(true))
      expect(result.current.watchedRequestId).toBe('ipqc_retry')
      expect(api.requestVPSIPQualityCollect).toHaveBeenCalledTimes(2)
    })

    it.each([
      ['pending', 'success'],
      ['pending', 'failure'],
      ['completed', 'success'],
      ['completed', 'failure'],
    ] as const)('never lets an older follow-up read overwrite a newer POST that is %s (old read %s)', async (phase, outcome) => {
      const followUpA = deferred<IPQualityCollectStatus>()
      const postB = deferred<IPQualityCollectStatus>()
      const statusB = requestStatus('ipqc_b', 'pending')
      api.getVPSIPQualityCollectStatus.mockResolvedValueOnce(status()).mockReturnValueOnce(followUpA.promise).mockResolvedValue(statusB)
      api.requestVPSIPQualityCollect.mockRejectedValueOnce(conflict()).mockReturnValueOnce(postB.promise)
      const { result } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))
      await waitFor(() => expect(result.current.status?.available).toBe(true))

      act(() => result.current.start([4]))
      await waitFor(() => expect(api.getVPSIPQualityCollectStatus).toHaveBeenCalledTimes(2))
      expect(result.current.submitting).toBe(false)
      act(() => result.current.start([6]))
      expect(result.current.submitting).toBe(true)
      if (phase === 'completed') {
        await act(async () => { postB.resolve(statusB) })
        expect(result.current.watchedRequestId).toBe('ipqc_b')
      }

      await act(async () => {
        if (outcome === 'success') followUpA.resolve(requestStatus('ipqc_a', 'failed'))
        else followUpA.reject(new ApiError(503, 'service unavailable'))
      })
      expect(result.current.error).toBeNull()
      if (phase === 'pending') {
        // B 仍在提交中，旧补读既不结束提交也不写入状态。
        expect(result.current.submitting).toBe(true)
        expect(result.current.status?.request).toBeUndefined()
        await act(async () => { postB.resolve(statusB) })
      }
      expect(result.current.submitting).toBe(false)
      expect(result.current.error).toBeNull()
      expect(result.current.status?.request?.request_id).toBe('ipqc_b')
      expect(result.current.watchedRequestId).toBe('ipqc_b')
      expect(result.current.active).toBe(true)
    })

    it.each(['switching to another VPS', 'returning to the same VPS'] as const)('keeps the current session untouched when the follow-up read fails after %s', async (move) => {
      const followUp = deferred<IPQualityCollectStatus>()
      api.getVPSIPQualityCollectStatus.mockResolvedValueOnce(status()).mockReturnValueOnce(followUp.promise).mockResolvedValue(status())
      api.requestVPSIPQualityCollect.mockRejectedValueOnce(conflict())
      const { result, rerender } = renderHook(({ vpsId }) => useIPQualityCollect(vpsId, vi.fn()), { initialProps: { vpsId: 'vps_001' } })
      await waitFor(() => expect(result.current.status?.available).toBe(true))
      act(() => result.current.start([4]))
      await waitFor(() => expect(api.getVPSIPQualityCollectStatus).toHaveBeenCalledTimes(2))

      rerender({ vpsId: 'vps_002' })
      await waitFor(() => expect(result.current.status?.available).toBe(true))
      if (move === 'returning to the same VPS') {
        rerender({ vpsId: 'vps_001' })
        await waitFor(() => expect(result.current.status?.available).toBe(true))
      }
      await act(async () => { followUp.reject(new Error('network')) })
      expect(result.current.error).toBeNull()
      expect(result.current.checking).toBe(false)
      expect(result.current.submitting).toBe(false)
    })

    it('does not issue further requests when the follow-up read fails after unmount', async () => {
      const followUp = deferred<IPQualityCollectStatus>()
      api.getVPSIPQualityCollectStatus.mockResolvedValueOnce(status()).mockReturnValueOnce(followUp.promise)
      api.requestVPSIPQualityCollect.mockRejectedValueOnce(conflict())
      const { result, unmount } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))
      await waitFor(() => expect(result.current.status?.available).toBe(true))
      act(() => result.current.start([4]))
      await waitFor(() => expect(api.getVPSIPQualityCollectStatus).toHaveBeenCalledTimes(2))

      unmount()
      await act(async () => { followUp.reject(new Error('network')) })
      await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS * 2) })
      expect(api.getVPSIPQualityCollectStatus).toHaveBeenCalledTimes(2)
      expect(api.requestVPSIPQualityCollect).toHaveBeenCalledTimes(1)
    })

    it.each([
      ['succeeds', () => Promise.resolve(status())],
      ['fails', () => Promise.reject(new Error('network'))],
      ['itself returns 409', () => Promise.reject(new ApiError(409, 'conflict'))],
    ] as const)('keeps the generic POST error after a non-409 failure when the follow-up read %s', async (_label, followUp) => {
      api.getVPSIPQualityCollectStatus.mockResolvedValueOnce(status()).mockImplementationOnce(followUp).mockResolvedValue(status())
      api.requestVPSIPQualityCollect.mockRejectedValueOnce(new ApiError(500, 'internal'))
      const { result } = renderHook(() => useIPQualityCollect('vps_001', vi.fn()))
      await waitFor(() => expect(result.current.status?.available).toBe(true))

      act(() => result.current.start([4, 6]))
      await waitFor(() => expect(api.getVPSIPQualityCollectStatus).toHaveBeenCalledTimes(2))
      await act(async () => { await Promise.resolve() })
      expect(result.current.error).toBe('发起采集失败，请稍后重试')
      expect(result.current.submitting).toBe(false)
    })
  })
})


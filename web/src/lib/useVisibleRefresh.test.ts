import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useVisibleRefresh, VISIBLE_REFRESH_INTERVAL_MS, type VisibleRefreshContext } from './useVisibleRefresh'

function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((inner) => {
    resolve = inner
  })
  return { promise, resolve }
}

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    get: () => state,
  })
}

describe('useVisibleRefresh', () => {
  afterEach(() => {
    setVisibility('visible')
    vi.useRealTimers()
  })

  it('does not read on mount and refreshes on the visible interval', async () => {
    vi.useFakeTimers()
    setVisibility('visible')
    const seen: VisibleRefreshContext[] = []
    const { result } = renderHook(() =>
      useVisibleRefresh(async (context) => {
        seen.push(context)
      }),
    )

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(seen).toHaveLength(0)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS - 1)
    })
    expect(seen).toHaveLength(0)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(seen).toHaveLength(1)
    expect(seen[0]?.isCurrent()).toBe(true)
    expect(result.current.refresh).toBeTypeOf('function')
  })

  it('skips hidden interval and focus wakes, then refreshes when shown or focused', async () => {
    vi.useFakeTimers()
    setVisibility('visible')
    let calls = 0
    renderHook(() =>
      useVisibleRefresh(async () => {
        calls += 1
      }),
    )

    setVisibility('hidden')
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
      window.dispatchEvent(new Event('focus'))
      await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS)
    })
    expect(calls).toBe(0)

    setVisibility('visible')
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(calls).toBe(1)

    await act(async () => {
      window.dispatchEvent(new Event('focus'))
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(calls).toBe(2)
  })

  it('coalesces a rapid focus and visibility wake into one request', async () => {
    vi.useFakeTimers()
    setVisibility('visible')
    const gate = deferred()
    let calls = 0
    renderHook(() =>
      useVisibleRefresh(async () => {
        calls += 1
        await gate.promise
      }),
    )

    await act(async () => {
      window.dispatchEvent(new Event('focus'))
      document.dispatchEvent(new Event('visibilitychange'))
      await Promise.resolve()
    })
    expect(calls).toBe(1)

    await act(async () => {
      gate.resolve()
      await Promise.resolve()
    })
    expect(calls).toBe(1)
  })

  it('keeps a single in-flight read when the interval fires again', async () => {
    vi.useFakeTimers()
    setVisibility('visible')
    const gate = deferred()
    let calls = 0
    renderHook(() =>
      useVisibleRefresh(async () => {
        calls += 1
        await gate.promise
      }),
    )

    await act(async () => {
      await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS)
      await Promise.resolve()
    })
    expect(calls).toBe(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS)
      await Promise.resolve()
    })
    expect(calls).toBe(1)

    await act(async () => {
      gate.resolve()
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(calls).toBe(1)
  })

  it('makes an in-flight read stale on unmount and does not start another', async () => {
    vi.useFakeTimers()
    const gate = deferred()
    let calls = 0
    const contexts: VisibleRefreshContext[] = []
    const { result, unmount } = renderHook(() =>
      useVisibleRefresh(async (context: VisibleRefreshContext) => {
        calls += 1
        contexts.push(context)
        await gate.promise
      }),
    )

    let pending!: Promise<void>
    await act(async () => {
      pending = result.current.refresh()
      await Promise.resolve()
    })
    expect(calls).toBe(1)
    expect(contexts).toHaveLength(1)
    unmount()
    expect(contexts[0]!.isCurrent()).toBe(false)

    await act(async () => {
      gate.resolve()
      await pending
      await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS)
    })
    expect(calls).toBe(1)
  })

  it('drops isCurrent when refreshKey changes and queues one refresh behind the obsolete read', async () => {
    vi.useFakeTimers()
    const first = deferred()
    const calls: VisibleRefreshContext[] = []
    const { result, rerender } = renderHook(
      ({ refreshKey }: { refreshKey: string }) =>
        useVisibleRefresh(
          async (context) => {
            calls.push(context)
            if (calls.length === 1) await first.promise
          },
          { refreshKey },
        ),
      { initialProps: { refreshKey: 'target-a' } },
    )

    let queued!: Promise<void>
    await act(async () => {
      void result.current.refresh()
      await Promise.resolve()
    })
    expect(calls).toHaveLength(1)
    rerender({ refreshKey: 'target-b' })
    expect(calls[0]?.isCurrent()).toBe(false)

    await act(async () => {
      queued = result.current.refresh()
      void result.current.refresh()
      await Promise.resolve()
    })
    expect(calls).toHaveLength(1)

    await act(async () => {
      first.resolve()
      await queued
    })
    expect(calls).toHaveLength(2)
    expect(calls[1]?.isCurrent()).toBe(true)
  })

  it('queues one post-invalidation refresh and keeps refresh identity stable', async () => {
    vi.useFakeTimers()
    const first = deferred()
    const calls: VisibleRefreshContext[] = []
    const { result, rerender } = renderHook(() =>
      useVisibleRefresh(async (context) => {
        calls.push(context)
        if (calls.length === 1) await first.promise
      }),
    )
    const refresh = result.current.refresh
    const invalidate = result.current.invalidate
    rerender()
    expect(result.current.refresh).toBe(refresh)
    expect(result.current.invalidate).toBe(invalidate)

    let queued!: Promise<void>
    await act(async () => {
      void result.current.refresh()
      await Promise.resolve()
    })
    act(() => {
      result.current.invalidate()
    })
    expect(calls[0]?.isCurrent()).toBe(false)

    await act(async () => {
      queued = result.current.refresh()
      void result.current.refresh()
      await Promise.resolve()
    })
    expect(calls).toHaveLength(1)

    await act(async () => {
      first.resolve()
      await queued
    })
    expect(calls).toHaveLength(2)
    expect(calls[0]?.isCurrent()).toBe(false)
    expect(calls[1]?.isCurrent()).toBe(true)
  })

  it('still runs a manual refresh when automatic wakes are disabled', async () => {
    vi.useFakeTimers()
    setVisibility('visible')
    let calls = 0
    const { result } = renderHook(() =>
      useVisibleRefresh(
        async () => {
          calls += 1
        },
        { enabled: false },
      ),
    )

    await act(async () => {
      window.dispatchEvent(new Event('focus'))
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(VISIBLE_REFRESH_INTERVAL_MS)
    })
    expect(calls).toBe(0)

    await act(async () => {
      await result.current.refresh()
    })
    expect(calls).toBe(1)
  })
})

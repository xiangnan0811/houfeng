import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useCopyToClipboard } from './useCopyToClipboard'

const ORIGINAL_CLIPBOARD = Object.getOwnPropertyDescriptor(
  globalThis.navigator,
  'clipboard',
)
const ORIGINAL_SECURE_CONTEXT = Object.getOwnPropertyDescriptor(window, 'isSecureContext')

function setClipboardWriteText(impl: (text: string) => Promise<void>) {
  const writeText = vi.fn(impl)
  Object.defineProperty(globalThis.navigator, 'clipboard', {
    configurable: true,
    value: { writeText },
  })
  return writeText
}

function setSecureContext(value: boolean) {
  Object.defineProperty(window, 'isSecureContext', {
    configurable: true,
    get: () => value,
  })
}

function restoreClipboard() {
  if (ORIGINAL_CLIPBOARD) {
    Object.defineProperty(globalThis.navigator, 'clipboard', ORIGINAL_CLIPBOARD)
  } else {
    // jsdom doesn't ship a clipboard by default; redefine as undefined to drop
    // the value we installed without violating TS strict's `delete` rules.
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      configurable: true,
      value: undefined,
    })
  }
}

function restoreSecureContext() {
  if (ORIGINAL_SECURE_CONTEXT) {
    Object.defineProperty(window, 'isSecureContext', ORIGINAL_SECURE_CONTEXT)
  }
}

describe('useCopyToClipboard', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.runOnlyPendingTimers()
    vi.useRealTimers()
    restoreClipboard()
    restoreSecureContext()
    vi.restoreAllMocks()
  })

  it('writes via navigator.clipboard and resets the copied flag after the timeout', async () => {
    setSecureContext(true)
    const writeText = setClipboardWriteText(async () => {})

    const { result } = renderHook(() => useCopyToClipboard(1500))

    expect(result.current.copied).toBe(false)

    let success: boolean | undefined
    await act(async () => {
      success = await result.current.copy('hello')
    })

    expect(success).toBe(true)
    expect(writeText).toHaveBeenCalledWith('hello')
    expect(result.current.copied).toBe(true)

    act(() => {
      vi.advanceTimersByTime(1500)
    })

    expect(result.current.copied).toBe(false)
  })

  it('falls back to document.execCommand when clipboard API is unavailable', async () => {
    // Force the secure-context branch to fail so we exercise the fallback path.
    setSecureContext(false)
    // jsdom does not implement document.execCommand by default, so we install
    // a writable stub before spying on it.
    const execSpy = vi.fn(() => true)
    Object.defineProperty(document, 'execCommand', {
      configurable: true,
      writable: true,
      value: execSpy,
    })

    const { result } = renderHook(() => useCopyToClipboard())

    let success: boolean | undefined
    await act(async () => {
      success = await result.current.copy('fallback-text')
    })

    expect(success).toBe(true)
    expect(execSpy).toHaveBeenCalledWith('copy')
    expect(result.current.copied).toBe(true)
  })

  it('reuses the timeout across rapid copies without leaking timers', async () => {
    setSecureContext(true)
    setClipboardWriteText(async () => {})

    const { result, unmount } = renderHook(() => useCopyToClipboard(1000))

    await act(async () => {
      await result.current.copy('first')
    })
    expect(result.current.copied).toBe(true)

    // Re-arm 200ms in: the timer should reset to a fresh 1000ms.
    act(() => {
      vi.advanceTimersByTime(200)
    })
    await act(async () => {
      await result.current.copy('second')
    })
    expect(result.current.copied).toBe(true)

    // Only 800ms has passed since the second copy → still copied.
    act(() => {
      vi.advanceTimersByTime(800)
    })
    expect(result.current.copied).toBe(true)

    // Now the second timer fires at 1000ms total since the second copy.
    act(() => {
      vi.advanceTimersByTime(200)
    })
    expect(result.current.copied).toBe(false)

    // Unmount must not throw even after the timer cleared itself.
    unmount()
  })
})

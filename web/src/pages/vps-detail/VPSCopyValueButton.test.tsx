import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { VPSCopyValueButton } from './VPSCopyValueButton'

const ORIGINAL_CLIPBOARD = Object.getOwnPropertyDescriptor(globalThis.navigator, 'clipboard')
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

describe('VPSCopyValueButton', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    setSecureContext(true)
  })

  afterEach(() => {
    vi.runOnlyPendingTimers()
    vi.useRealTimers()
    restoreClipboard()
    restoreSecureContext()
  })

  it('copies the raw value and announces success', async () => {
    const writeText = setClipboardWriteText(async () => undefined)
    render(<VPSCopyValueButton value=" 192.0.2.1 " label="IPv4" />)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '复制IPv4' }))
    })

    expect(writeText).toHaveBeenCalledWith('192.0.2.1')
    expect(screen.getByRole('button', { name: '复制IPv4' })).toHaveTextContent('已复制')
    expect(screen.getByText('IPv4已复制')).toBeInTheDocument()
  })

  it('announces failure without copying a substitute value', async () => {
    setClipboardWriteText(async () => {
      throw new Error('denied')
    })
    render(<VPSCopyValueButton value="root@192.0.2.1:22" label="SSH" />)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '复制SSH' }))
    })

    expect(screen.getByRole('button', { name: '复制SSH' })).toHaveTextContent('复制失败')
    expect(screen.getByText('SSH复制失败')).toBeInTheDocument()
  })

  it('replaces success with failure on a later denied click within the reset window', async () => {
    let denyNext = false
    setClipboardWriteText(async () => {
      if (denyNext) throw new Error('denied')
    })
    render(<VPSCopyValueButton value="192.0.2.10" label="IPv4" />)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '复制IPv4' }))
    })
    expect(screen.getByRole('button', { name: '复制IPv4' })).toHaveTextContent('已复制')
    expect(screen.getByText('IPv4已复制')).toBeInTheDocument()

    denyNext = true
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '复制IPv4' }))
    })

    const button = screen.getByRole('button', { name: '复制IPv4' })
    expect(button).toHaveTextContent('复制失败')
    expect(button).not.toHaveTextContent('已复制')
    expect(screen.getByText('IPv4复制失败')).toBeInTheDocument()
    expect(screen.queryByText('IPv4已复制')).not.toBeInTheDocument()
    expect(button.closest('.vps-copy-value')).not.toHaveTextContent('已复制IPv4复制失败')
  })


  it('does not render a copy control for empty or placeholder values', () => {
    const { rerender } = render(<VPSCopyValueButton value="   " label="IPv4" />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    rerender(<VPSCopyValueButton value="—" label="IPv6" />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})

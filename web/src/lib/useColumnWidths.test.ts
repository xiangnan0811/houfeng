import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { useColumnWidths } from './useColumnWidths'

describe('useColumnWidths', () => {
  afterEach(() => {
    window.localStorage.clear()
    document.body.classList.remove('is-col-resizing')
  })

  it('starts from defaults and persists a drag to localStorage', () => {
    const { result } = renderHook(() => useColumnWidths('test-table', [80, 120]))
    expect(result.current.widths).toEqual([80, 120])

    act(() => {
      result.current.startResize(0, 100)
    })
    expect(document.body.classList.contains('is-col-resizing')).toBe(true)

    act(() => {
      window.dispatchEvent(new PointerEvent('pointermove', { clientX: 140 }))
      window.dispatchEvent(new PointerEvent('pointerup'))
    })

    expect(result.current.widths[0]).toBe(120)
    expect(document.body.classList.contains('is-col-resizing')).toBe(false)
    expect(window.localStorage.getItem('houfeng.table-col-widths.test-table')).toBe(JSON.stringify([120, 120]))
  })

  it('restores stored widths on a later mount', () => {
    window.localStorage.setItem('houfeng.table-col-widths.test-table', JSON.stringify([64, 200]))
    const { result } = renderHook(() => useColumnWidths('test-table', [80, 120]))
    expect(result.current.widths).toEqual([64, 200])
  })
})

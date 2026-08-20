import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useVPSManagementController } from './useVPSManagementController'

describe('useVPSManagementController', () => {
  it('opens the menu and routes to a single management panel', () => {
    const { result } = renderHook(() => useVPSManagementController())
    expect(result.current.panel).toBeNull()

    act(() => result.current.openMenu())
    expect(result.current.menuOpen).toBe(true)

    act(() => result.current.openPanel('facts'))
    expect(result.current.panel).toBe('facts')
    expect(result.current.menuOpen).toBe(false)

    act(() => result.current.closePanel())
    expect(result.current.panel).toBeNull()
  })
})

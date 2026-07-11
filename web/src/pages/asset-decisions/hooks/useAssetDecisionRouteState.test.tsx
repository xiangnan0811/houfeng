import type { ReactNode } from 'react'
import { act, renderHook } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { useAssetDecisionRouteState } from './useAssetDecisionRouteState'

function routerWrapper(initialEntry: string) {
  return function RouterWrapper({ children }: { children: ReactNode }) {
    return <MemoryRouter initialEntries={[initialEntry]}>{children}</MemoryRouter>
  }
}

describe('useAssetDecisionRouteState', () => {
  it('parses the default portfolio route without an open selection', () => {
    const { result } = renderHook(() => useAssetDecisionRouteState(), {
      wrapper: routerWrapper('/asset-decisions'),
    })

    expect(result.current.state).toMatchObject({
      workbench: 'needs_decision',
      portfolioView: 'needs_decision',
      renewalWindow: 30,
      secondary: null,
      open: null,
      contextFilterChips: [],
    })
    expect(result.current.state.filter).toEqual({
      view: 'needs_decision',
      renew_within_days: 30,
      provider_id: undefined,
      vps_id: undefined,
      country: undefined,
      region: undefined,
      city: undefined,
      scenario: undefined,
    })
  })

  it('writes mutually exclusive open selections while retaining context filters', () => {
    const { result } = renderHook(() => useAssetDecisionRouteState(), {
      wrapper: routerWrapper('/asset-decisions?view=evidence&provider_id=pv_001&group_id=adg_1'),
    })

    act(() => result.current.commands.openEntity('record_id', 'adr_1'))

    expect(result.current.state.open).toEqual({ type: 'record_id', id: 'adr_1' })
    expect(result.current.state.filter.provider_id).toBe('pv_001')
    expect(result.current.state.workbench).toBe('evidence')
    expect(result.current.state.searchSignature).toContain('record_id=adr_1')
    expect(result.current.state.searchSignature).not.toContain('group_id=')
  })

  it('falls back to the URL-derived workbench when a manual selection is cleared', () => {
    const { result } = renderHook(() => useAssetDecisionRouteState(), {
      wrapper: routerWrapper('/asset-decisions?view=renewal&renew_within_days=60'),
    })

    expect(result.current.state.secondary).toBe('renewals')
    act(() => result.current.commands.setSecondary('records'))
    expect(result.current.state.secondary).toBe('records')
    act(() => result.current.commands.setSecondary(null))
    expect(result.current.state.secondary).toBe('renewals')
  })
})

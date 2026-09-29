import { act, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ThemeProvider } from '../../lib/theme-context'
import { ThemeSettingsSection } from './ThemeSettingsSection'

type Listener = (event: MediaQueryListEvent) => void

function stubSystemScheme(initialDark: boolean) {
  let dark = initialDark
  const listeners = new Set<Listener>()
  vi.stubGlobal('matchMedia', (query: string) => ({
    get matches() { return query.includes('dark') ? dark : !dark },
    media: query,
    addEventListener: (_: string, listener: Listener) => listeners.add(listener),
    removeEventListener: (_: string, listener: Listener) => listeners.delete(listener),
  }))
  return {
    set(nextDark: boolean) {
      dark = nextDark
      for (const listener of listeners) listener({ matches: nextDark } as MediaQueryListEvent)
    },
  }
}

function previewOf(name: string) {
  const button = within(screen.getByRole('group', { name: '主题风格' })).getByRole('button', { name })
  return button.querySelector('.theme-preset__preview')!
}

describe('ThemeSettingsSection', () => {
  beforeEach(() => {
    localStorage.clear()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('previews each preset with its own theme class and follows live system scheme changes', () => {
    const system = stubSystemScheme(true)
    localStorage.setItem('houfeng.theme.mode', 'system')
    render(<ThemeProvider><ThemeSettingsSection /></ThemeProvider>)

    expect(previewOf('候风')).toHaveClass('theme-houfeng-dark')
    expect(previewOf('精密')).toHaveClass('theme-precision-dark')
    expect(previewOf('观测台')).toHaveClass('theme-observatory-dark')

    act(() => system.set(false))
    expect(previewOf('候风')).toHaveClass('theme-houfeng-light')
    expect(previewOf('精密')).toHaveClass('theme-precision-light')
    // 仅深色的风格始终以深色预览。
    expect(previewOf('观测台')).toHaveClass('theme-observatory-dark')
  })

  it('uses the explicit mode when not following the system and exposes the dark-only limit as a description', () => {
    stubSystemScheme(true)
    localStorage.setItem('houfeng.theme.mode', 'light')
    render(<ThemeProvider><ThemeSettingsSection /></ThemeProvider>)

    expect(previewOf('候风')).toHaveClass('theme-houfeng-light')
    const group = screen.getByRole('group', { name: '主题风格' })
    expect(within(group).getByRole('button', { name: '观测台' })).toHaveAccessibleDescription('仅深色')
    expect(within(group).getByRole('button', { name: '候风' })).not.toHaveAttribute('aria-describedby')
  })
})

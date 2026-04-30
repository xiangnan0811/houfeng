import { describe, expect, it, beforeEach, vi } from 'vitest'
import {
  applyTheme,
  detectInitialTheme,
  preferredScheme,
  type Preset,
  type Mode,
  THEME_STORAGE_KEYS,
} from './theme'

function setLS(preset: Preset | null, mode: Mode | null) {
  if (preset === null) localStorage.removeItem(THEME_STORAGE_KEYS.preset)
  else localStorage.setItem(THEME_STORAGE_KEYS.preset, preset)
  if (mode === null) localStorage.removeItem(THEME_STORAGE_KEYS.mode)
  else localStorage.setItem(THEME_STORAGE_KEYS.mode, mode)
}

describe('theme runtime', () => {
  beforeEach(() => {
    document.documentElement.className = ''
    setLS(null, null)
  })

  it('applyTheme sets the matching html class', () => {
    applyTheme('houfeng', 'dark')
    expect(document.documentElement.classList.contains('theme-houfeng-dark')).toBe(true)

    applyTheme('classic', 'light')
    expect(document.documentElement.classList.contains('theme-classic-light')).toBe(true)
    expect(document.documentElement.classList.contains('theme-houfeng-dark')).toBe(false)
  })

  it('detectInitialTheme defaults to houfeng + system', () => {
    const t = detectInitialTheme()
    expect(t.preset).toBe('houfeng')
    expect(t.mode).toBe('system')
  })

  it('detectInitialTheme reads localStorage', () => {
    setLS('classic', 'dark')
    const t = detectInitialTheme()
    expect(t.preset).toBe('classic')
    expect(t.mode).toBe('dark')
  })

  it('detectInitialTheme falls back when localStorage has bogus values', () => {
    localStorage.setItem(THEME_STORAGE_KEYS.preset, 'mystery')
    localStorage.setItem(THEME_STORAGE_KEYS.mode, 'sunshine')
    const t = detectInitialTheme()
    expect(t.preset).toBe('houfeng')
    expect(t.mode).toBe('system')
  })

  it('preferredScheme returns dark when matchMedia matches', () => {
    const fake = (q: string): MediaQueryList => ({
      matches: q.includes('dark'),
      media: q,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList
    vi.stubGlobal('matchMedia', fake)
    expect(preferredScheme()).toBe('dark')
    vi.unstubAllGlobals()
  })
})

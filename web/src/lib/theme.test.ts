import { describe, expect, it, beforeEach, vi } from 'vitest'
import {
  applyTheme,
  detectInitialTheme,
  preferredScheme,
  themeClass,
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

    applyTheme('precision', 'light')
    expect(document.documentElement.className).toBe('theme-precision-light')

    applyTheme('observatory', 'dark')
    expect(document.documentElement.className).toBe('theme-observatory-dark')

    // 观测台仅深色：浅色回退候风浅色，且只保留一个 theme 类
    applyTheme('observatory', 'light')
    expect(document.documentElement.className).toBe('theme-houfeng-light')
  })

  it('themeClass maps every preset and scheme to a defined runtime theme', () => {
    expect(themeClass('houfeng', 'dark')).toBe('theme-houfeng-dark')
    expect(themeClass('houfeng', 'light')).toBe('theme-houfeng-light')
    expect(themeClass('precision', 'dark')).toBe('theme-precision-dark')
    expect(themeClass('precision', 'light')).toBe('theme-precision-light')
    expect(themeClass('observatory', 'dark')).toBe('theme-observatory-dark')
    expect(themeClass('observatory', 'light')).toBe('theme-houfeng-light')
  })

  it('detectInitialTheme defaults to houfeng + dark', () => {
    const t = detectInitialTheme()
    expect(t.preset).toBe('houfeng')
    expect(t.mode).toBe('dark')
  })

  it('detectInitialTheme reads localStorage', () => {
    setLS('precision', 'light')
    const t = detectInitialTheme()
    expect(t.preset).toBe('precision')
    expect(t.mode).toBe('light')
  })

  it('detectInitialTheme migrates the retired classic preset to houfeng', () => {
    localStorage.setItem(THEME_STORAGE_KEYS.preset, 'classic')
    localStorage.setItem(THEME_STORAGE_KEYS.mode, 'dark')
    const t = detectInitialTheme()
    expect(t.preset).toBe('houfeng')
    expect(t.mode).toBe('dark')
  })

  it('detectInitialTheme falls back when localStorage has bogus values', () => {
    localStorage.setItem(THEME_STORAGE_KEYS.preset, 'mystery')
    localStorage.setItem(THEME_STORAGE_KEYS.mode, 'sunshine')
    const t = detectInitialTheme()
    expect(t.preset).toBe('houfeng')
    expect(t.mode).toBe('dark')
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
    })
    vi.stubGlobal('matchMedia', fake)
    expect(preferredScheme()).toBe('dark')
    vi.unstubAllGlobals()
  })
})

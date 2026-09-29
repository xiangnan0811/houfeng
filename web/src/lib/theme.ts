export type Preset = 'houfeng' | 'precision' | 'observatory'
export type Mode = 'dark' | 'light' | 'system'
export type Scheme = 'dark' | 'light'

export const THEME_STORAGE_KEYS = {
  preset: 'houfeng.theme.preset',
  mode: 'houfeng.theme.mode',
} as const

export interface ThemeChoice {
  preset: Preset
  mode: Mode
}

// 已下线的 'classic' 等旧值不在白名单内，读取时回落到默认预设。
const PRESET_VALUES: ReadonlySet<Preset> = new Set(['houfeng', 'precision', 'observatory'])
// 观测台只有深色版本；浅色时回退候风浅色。
const DARK_ONLY_PRESETS: ReadonlySet<Preset> = new Set(['observatory'])
const MODE_VALUES: ReadonlySet<Mode> = new Set(['dark', 'light', 'system'])

export function detectInitialTheme(): ThemeChoice {
  const presetRaw = safeLocalStorage(THEME_STORAGE_KEYS.preset)
  const modeRaw = safeLocalStorage(THEME_STORAGE_KEYS.mode)
  return {
    preset: PRESET_VALUES.has(presetRaw as Preset) ? (presetRaw as Preset) : 'houfeng',
    mode: MODE_VALUES.has(modeRaw as Mode) ? (modeRaw as Mode) : 'dark',
  }
}

export function preferredScheme(): Scheme {
  if (typeof window === 'undefined' || !window.matchMedia) return 'dark'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function resolveScheme(mode: Mode): Scheme {
  return mode === 'system' ? preferredScheme() : mode
}

export function isDarkOnlyPreset(preset: Preset): boolean {
  return DARK_ONLY_PRESETS.has(preset)
}

export function themeClass(preset: Preset, scheme: Scheme): string {
  return scheme === 'light' && isDarkOnlyPreset(preset)
    ? 'theme-houfeng-light'
    : `theme-${preset}-${scheme}`
}

export function applyTheme(preset: Preset, mode: Mode | Scheme): void {
  const scheme: Scheme = mode === 'system' ? preferredScheme() : mode
  const cls = themeClass(preset, scheme)
  const html = document.documentElement
  for (const c of Array.from(html.classList)) {
    if (c.startsWith('theme-')) html.classList.remove(c)
  }
  html.classList.add(cls)
}

export function persistTheme(choice: ThemeChoice): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEYS.preset, choice.preset)
    localStorage.setItem(THEME_STORAGE_KEYS.mode, choice.mode)
  } catch {
    /* private mode etc. — ignore */
  }
}

export function subscribeSystemScheme(cb: (s: Scheme) => void): () => void {
  if (typeof window === 'undefined' || !window.matchMedia) return () => {}
  const mql = window.matchMedia('(prefers-color-scheme: dark)')
  const listener = (e: MediaQueryListEvent) => cb(e.matches ? 'dark' : 'light')
  mql.addEventListener('change', listener)
  return () => mql.removeEventListener('change', listener)
}

function safeLocalStorage(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import {
  applyTheme,
  detectInitialTheme,
  persistTheme,
  subscribeSystemScheme,
  type Preset,
  type Mode,
} from './theme'

interface ThemeContextValue {
  preset: Preset
  mode: Mode
  setPreset: (p: Preset) => void
  setMode: (m: Mode) => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [{ preset, mode }, setChoice] = useState(detectInitialTheme)

  useEffect(() => {
    applyTheme(preset, mode)
  }, [preset, mode])

  useEffect(() => {
    if (mode !== 'system') return
    return subscribeSystemScheme(() => applyTheme(preset, 'system'))
  }, [mode, preset])

  const setPreset = useCallback((next: Preset) => {
    setChoice((prev) => {
      const updated = { ...prev, preset: next }
      persistTheme(updated)
      return updated
    })
  }, [])

  const setMode = useCallback((next: Mode) => {
    setChoice((prev) => {
      const updated = { ...prev, mode: next }
      persistTheme(updated)
      return updated
    })
  }, [])

  return (
    <ThemeContext.Provider value={{ preset, mode, setPreset, setMode }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be inside <ThemeProvider>')
  return ctx
}

/** Like useTheme but returns null when no provider is present (test ergonomics). */
export function useThemeOptional(): ThemeContextValue | null {
  return useContext(ThemeContext)
}

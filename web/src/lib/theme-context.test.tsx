import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, beforeEach } from 'vitest'
import { ThemeProvider, useTheme } from './theme-context'

function Probe() {
  const { preset, mode, setPreset, setMode } = useTheme()
  return (
    <div>
      <span data-testid="state">
        {preset}/{mode}
      </span>
      <button onClick={() => setPreset('classic')}>classic</button>
      <button onClick={() => setMode('light')}>light</button>
      <button onClick={() => setMode('dark')}>dark</button>
    </div>
  )
}

describe('ThemeProvider', () => {
  beforeEach(() => {
    document.documentElement.className = ''
    localStorage.clear()
  })

  it('starts at houfeng/dark and applies html class', () => {
    render(
      <ThemeProvider>
        <Probe />
      </ThemeProvider>,
    )
    expect(screen.getByTestId('state')).toHaveTextContent('houfeng/dark')
    expect(document.documentElement.classList.contains('theme-houfeng-dark')).toBe(true)
  })

  it('switching preset updates storage and class', () => {
    render(
      <ThemeProvider>
        <Probe />
      </ThemeProvider>,
    )
    fireEvent.click(screen.getByText('classic'))
    expect(localStorage.getItem('houfeng.theme.preset')).toBe('classic')
    expect(document.documentElement.className).toMatch(/^theme-classic-/)
  })

  it('switching mode to light overrides system', () => {
    render(
      <ThemeProvider>
        <Probe />
      </ThemeProvider>,
    )
    fireEvent.click(screen.getByText('light'))
    expect(localStorage.getItem('houfeng.theme.mode')).toBe('light')
    expect(document.documentElement.classList.contains('theme-houfeng-light')).toBe(true)
  })

  it('switching mode back to dark overrides previous light', () => {
    render(
      <ThemeProvider>
        <Probe />
      </ThemeProvider>,
    )
    fireEvent.click(screen.getByText('light'))
    fireEvent.click(screen.getByText('dark'))
    expect(document.documentElement.classList.contains('theme-houfeng-dark')).toBe(true)
    expect(document.documentElement.classList.contains('theme-houfeng-light')).toBe(false)
  })
})

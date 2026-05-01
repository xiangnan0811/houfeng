import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/react'
import { StatusGlyph } from './StatusGlyph'

describe('StatusGlyph', () => {
  it('renders an SVG with the state class', () => {
    const { container } = render(<StatusGlyph state="critical" />)
    const svg = container.querySelector('svg')
    expect(svg).toBeTruthy()
    expect(svg!.classList.contains('status-glyph--critical')).toBe(true)
  })

  it('uses default Chinese aria label per state', () => {
    const { container } = render(<StatusGlyph state="normal" />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('aria-label')).toBe('正常')
  })

  it('overrides aria label when provided', () => {
    const { container } = render(<StatusGlyph state="alert" ariaLabel="自定义" />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('aria-label')).toBe('自定义')
  })

  it('renders sm size at 10px', () => {
    const { container } = render(<StatusGlyph state="maintenance" size="sm" />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('width')).toBe('10')
  })

  it('produces a different shape per state', () => {
    const states = ['normal', 'notice', 'alert', 'critical', 'maintenance', 'offline'] as const
    const innerHTMLs = states.map((s) => {
      const { container } = render(<StatusGlyph state={s} />)
      return container.querySelector('svg')!.innerHTML
    })
    const unique = new Set(innerHTMLs)
    expect(unique.size).toBe(states.length)
  })
})

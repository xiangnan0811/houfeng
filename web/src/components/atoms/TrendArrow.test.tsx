import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { TrendArrow } from './TrendArrow'

describe('TrendArrow', () => {
  it('renders up arrow for positive delta', () => {
    render(<TrendArrow delta={3.2} unit="%" />)
    const root = screen.getByLabelText(/上升/)
    expect(root.className).toContain('trend--up')
    expect(root.textContent).toContain('+3.20%')
  })

  it('renders down arrow for negative delta', () => {
    render(<TrendArrow delta={-1.5} unit="ms" />)
    const root = screen.getByLabelText(/下降/)
    expect(root.className).toContain('trend--down')
    expect(root.textContent).toContain('-1.50ms')
  })

  it('renders flat arrow for zero (within threshold)', () => {
    render(<TrendArrow delta={0} />)
    const root = screen.getByLabelText('持平')
    expect(root.className).toContain('trend--flat')
    expect(root.className).toContain('trend--tone-muted')
  })

  it('inverse flips tone semantics (up=bad)', () => {
    render(<TrendArrow delta={5} inverse />)
    const root = screen.getByLabelText(/上升/)
    expect(root.className).toContain('trend--tone-critical')
  })
})

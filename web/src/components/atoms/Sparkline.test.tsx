import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/react'
import { Sparkline } from './Sparkline'

describe('Sparkline', () => {
  it('renders a polyline with given values', () => {
    const { container } = render(<Sparkline values={[1, 2, 3, 4]} />)
    const polyline = container.querySelector('polyline')
    expect(polyline).toBeTruthy()
    const points = polyline!.getAttribute('points')!
    expect(points.split(' ')).toHaveLength(4)
  })

  it('renders an empty placeholder when values is empty', () => {
    const { container } = render(<Sparkline values={[]} />)
    expect(container.querySelector('polyline')).toBeNull()
    expect(container.querySelector('.sparkline--empty')).toBeTruthy()
  })

  it('applies tone class', () => {
    const { container } = render(<Sparkline values={[1, 2]} tone="critical" />)
    expect(container.querySelector('.sparkline--critical')).toBeTruthy()
  })

  it('handles single value gracefully', () => {
    const { container } = render(<Sparkline values={[5]} />)
    const polyline = container.querySelector('polyline')
    expect(polyline).toBeTruthy()
  })
})

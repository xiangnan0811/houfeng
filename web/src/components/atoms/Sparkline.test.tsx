import { describe, expect, it } from 'vitest'
import { fireEvent, render } from '@testing-library/react'
import { Sparkline, type SparklineSample } from './Sparkline'

describe('Sparkline', () => {
  it('renders a polyline with given values', () => {
    const { container } = render(<Sparkline values={[1, 2, 3, 4]} />)
    const polyline = container.querySelector('polyline')
    expect(polyline).toBeTruthy()
    const points = polyline!.getAttribute('points')!
    expect(points.split(' ')).toHaveLength(4)
  })

  it('renders an empty placeholder with text when values is empty', () => {
    const { container, getByText } = render(<Sparkline values={[]} />)
    expect(container.querySelector('polyline')).toBeNull()
    expect(container.querySelector('.sparkline--empty')).toBeTruthy()
    expect(getByText('暂无数据')).toBeInTheDocument()
  })

  it('applies tone class to the svg', () => {
    const { container } = render(<Sparkline values={[1, 2]} tone="critical" />)
    expect(container.querySelector('svg.sparkline--critical')).toBeTruthy()
  })

  it('handles single value by skipping the polyline and showing a hint', () => {
    const { container, getByText } = render(<Sparkline values={[5]} />)
    expect(container.querySelector('polyline')).toBeNull()
    // Single-sample still draws the end dot
    expect(container.querySelector('circle')).toBeTruthy()
    expect(getByText('样本不足')).toBeInTheDocument()
  })

  it('accepts samples with timestamps and renders polyline points', () => {
    const samples: SparklineSample[] = [
      { value: 1, observedAt: '2026-04-24T08:00:00Z' },
      { value: 2, observedAt: '2026-04-24T09:00:00Z' },
      { value: 3, observedAt: '2026-04-24T10:00:00Z' },
    ]
    const { container } = render(<Sparkline samples={samples} />)
    const polyline = container.querySelector('polyline')
    expect(polyline).toBeTruthy()
    expect(polyline!.getAttribute('points')!.split(' ')).toHaveLength(3)
  })

  it('shows a tooltip after a hover when interactive', () => {
    const samples: SparklineSample[] = [
      { value: 10, observedAt: '2026-04-24T08:00:00Z' },
      { value: 20, observedAt: '2026-04-24T09:00:00Z' },
      { value: 30, observedAt: '2026-04-24T10:00:00Z' },
    ]
    const { container } = render(
      <Sparkline samples={samples} interactive width={300} height={60} />,
    )

    const svg = container.querySelector('svg')!
    // jsdom returns 0 width from getBoundingClientRect by default; stub it.
    svg.getBoundingClientRect = () => ({
      x: 0, y: 0, top: 0, left: 0, right: 300, bottom: 60,
      width: 300, height: 60, toJSON: () => ({}),
    })

    fireEvent.mouseMove(svg, { clientX: 150 })

    const tooltip = container.querySelector('.sparkline__tooltip')
    expect(tooltip).toBeTruthy()
    // mid-point should snap to the middle sample (value 20)
    expect(container.querySelector('.sparkline__tooltip-value')?.textContent).toBe('20.00')
  })

  it('renders expand mode with width=100% on the svg', () => {
    const { container } = render(<Sparkline values={[1, 2, 3]} expand width={200} height={40} />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('width')).toBe('100%')
    expect(svg.getAttribute('preserveAspectRatio')).toBe('none')
  })

  it('uses the provided formatValue for tooltip rendering', () => {
    const samples: SparklineSample[] = [
      { value: 0.5, observedAt: '2026-04-24T08:00:00Z' },
      { value: 0.75, observedAt: '2026-04-24T09:00:00Z' },
    ]
    const { container } = render(
      <Sparkline
        samples={samples}
        interactive
        width={200}
        height={40}
        formatValue={(v) => `${(v * 100).toFixed(1)}%`}
      />,
    )

    const svg = container.querySelector('svg')!
    svg.getBoundingClientRect = () => ({
      x: 0, y: 0, top: 0, left: 0, right: 200, bottom: 40,
      width: 200, height: 40, toJSON: () => ({}),
    })

    fireEvent.mouseMove(svg, { clientX: 200 })
    expect(container.querySelector('.sparkline__tooltip-value')?.textContent).toBe('75.0%')
  })
})

import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render } from '@testing-library/react'
import {
  MetricChart,
  type MetricChartMaintenanceWindow,
  type MetricChartSample,
  type MetricChartThreshold,
} from './MetricChart'

function makeSamples(count: number, base = 50, stepMs = 60_000): MetricChartSample[] {
  const start = new Date('2026-04-30T08:00:00Z').getTime()
  return Array.from({ length: count }, (_, i) => ({
    value: base + i * 5,
    observedAt: new Date(start + i * stepMs).toISOString(),
  }))
}

describe('MetricChart', () => {
  it('renders polyline + thresholds + axis ticks for typical inputs', () => {
    const samples = makeSamples(5)
    const thresholds: MetricChartThreshold[] = [
      { value: 80, tone: 'notice' },
      { value: 95, tone: 'critical' },
    ]
    const { container } = render(
      <MetricChart
        samples={samples}
        thresholds={thresholds}
        yMin={0}
        yMax={100}
        formatValue={(v) => `${v.toFixed(0)}%`}
      />,
    )

    const polyline = container.querySelector('polyline')
    expect(polyline).toBeTruthy()
    expect(polyline!.getAttribute('points')!.split(' ')).toHaveLength(5)

    // End-point dot
    expect(container.querySelectorAll('circle').length).toBeGreaterThanOrEqual(1)

    // Threshold lines: one <line> + one <text> per threshold (in addition to grid lines)
    const thresholdGroups = container.querySelectorAll('.metric-chart__threshold')
    expect(thresholdGroups).toHaveLength(2)

    // Y-axis labels (formatted with provided formatValue)
    const yTickGroups = container.querySelectorAll('.metric-chart__y-tick')
    expect(yTickGroups.length).toBeGreaterThanOrEqual(3)

    // X-axis labels are HH:mm
    const axisTexts = Array.from(container.querySelectorAll('.metric-chart__axis-text'))
    const xLabels = axisTexts
      .map((node) => node.textContent ?? '')
      .filter((t) => /^\d{2}:\d{2}$/.test(t))
    expect(xLabels.length).toBeGreaterThan(0)
  })

  it('renders an empty placeholder when samples is empty', () => {
    const { container, getByText } = render(<MetricChart samples={[]} />)
    expect(container.querySelector('polyline')).toBeNull()
    expect(container.querySelector('.metric-chart--empty')).toBeTruthy()
    expect(getByText('暂无观测数据')).toBeInTheDocument()
  })

  it('handles a single sample by skipping the polyline and showing a hint', () => {
    const samples = makeSamples(1, 42)
    const { container, getByText } = render(<MetricChart samples={samples} />)
    expect(container.querySelector('polyline')).toBeNull()
    // Single-sample still renders the end-point dot
    expect(container.querySelectorAll('circle').length).toBeGreaterThanOrEqual(1)
    expect(getByText('样本不足')).toBeInTheDocument()
  })

  it('renders maintenance window rectangles within the data span', () => {
    const samples = makeSamples(10)
    // Maintenance window covers samples 3..6
    const start = samples[3].observedAt
    const end = samples[6].observedAt
    const windows: MetricChartMaintenanceWindow[] = [{ startedAt: start, endedAt: end }]
    const { container } = render(
      <MetricChart samples={samples} maintenanceWindows={windows} tone="maintenance" />,
    )

    const maintGroups = container.querySelectorAll('.metric-chart__maintenance')
    expect(maintGroups).toHaveLength(1)
    // Should contain both the band rect and the top-edge marker triangle
    expect(maintGroups[0].querySelector('rect')).toBeTruthy()
    expect(maintGroups[0].querySelector('polygon')).toBeTruthy()
  })

  it('shows a crosshair tooltip with second-level time while axis stays minute-level', () => {
    const samples = makeSamples(5, 10, 5_000) // 5-second cadence, values 10, 15, 20, 25, 30
    const { container } = render(
      <MetricChart samples={samples} width={300} height={140} formatValue={(v) => v.toFixed(1)} />,
    )

    const svg = container.querySelector('svg')!
    // jsdom returns 0-width rects by default; stub it
    svg.getBoundingClientRect = () => ({
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 300,
      bottom: 140,
      width: 300,
      height: 140,
      toJSON: () => ({}),
    })

    // Hover near the middle (clientX=150 → mid sample after accounting for padding)
    fireEvent.mouseMove(svg, { clientX: 150 })

    const tooltip = container.querySelector('.metric-chart__tooltip')
    expect(tooltip).toBeTruthy()
    expect((tooltip as HTMLElement).style.top).not.toBe('')
    expect((tooltip as HTMLElement).style.left).not.toBe('')
    const valueNode = container.querySelector('.metric-chart__tooltip-value')
    expect(valueNode).toBeTruthy()
    // Tooltip should display one of the sample values formatted by formatValue
    const formattedValues = samples.map((s) => s.value.toFixed(1))
    expect(formattedValues).toContain(valueNode!.textContent)

    const tooltipTime = container.querySelector('.metric-chart__tooltip-time')
    expect(tooltipTime?.textContent).toMatch(/^\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2}$/)
    expect(tooltipTime?.textContent).toMatch(/:0[5-9]$|:[1-5]\d$/)

    const axisTexts = Array.from(container.querySelectorAll('.metric-chart__axis-text'))
    const xLabels = axisTexts
      .map((node) => node.textContent ?? '')
      .filter((text) => /^\d{2}:\d{2}$/.test(text))
    expect(xLabels.length).toBeGreaterThan(0)
    expect(xLabels.every((text) => !/^\d{2}:\d{2}:\d{2}$/.test(text))).toBe(true)

    // Crosshair line should be visible
    expect(container.querySelector('.metric-chart__cursor')).toBeTruthy()
  })

  it('accepts custom time formatters for coarse-grained series', () => {
    const samples: MetricChartSample[] = [
      { value: 80, observedAt: '2025-07-01T00:00:00Z' },
      { value: 90, observedAt: '2025-08-01T00:00:00Z' },
      { value: 110, observedAt: '2025-09-01T00:00:00Z' },
    ]
    const { container, getByText } = render(
      <MetricChart
        samples={samples}
        width={300}
        height={140}
        formatTime={(observedAt) => observedAt.slice(2, 7).replace('-', '/')}
        formatTooltipTime={(observedAt) => `${observedAt.slice(0, 7)} month`}
      />,
    )

    expect(getByText('25/07')).toBeInTheDocument()
    expect(getByText('25/08')).toBeInTheDocument()
    expect(getByText('25/09')).toBeInTheDocument()
    expect(container).not.toHaveTextContent('00:00')

    const svg = container.querySelector('svg')!
    svg.getBoundingClientRect = () => ({
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 300,
      bottom: 140,
      width: 300,
      height: 140,
      toJSON: () => ({}),
    })
    fireEvent.mouseMove(svg, { clientX: 150 })
    expect(container.querySelector('.metric-chart__tooltip-time')).toHaveTextContent(/2025-0[78] month/)
  })

  it('renders a local tooltip for controlled hover by default', () => {
    const samples = makeSamples(5, 10, 5_000)
    const { container } = render(
      <MetricChart
        samples={samples}
        width={300}
        height={140}
        hoveredAt={samples[2].observedAt}
        formatValue={(v) => `${v.toFixed(1)}%`}
      />,
    )

    const tooltip = container.querySelector('.metric-chart__tooltip')
    expect(tooltip).toBeTruthy()
    expect(tooltip).toHaveTextContent('20.0%')
    expect((tooltip as HTMLElement).style.top).not.toBe('')
    expect(container.querySelector('.metric-chart__cursor')).toBeTruthy()
  })

  it('can suppress the local tooltip for controlled hover', () => {
    const samples = makeSamples(5, 10, 5_000)
    const onHoverAtChange = vi.fn()
    const { container } = render(
      <MetricChart
        samples={samples}
        width={300}
        height={140}
        hoveredAt={samples[2].observedAt}
        onHoverAtChange={onHoverAtChange}
        showTooltip={false}
      />,
    )

    expect(container.querySelector('.metric-chart__cursor')).toBeTruthy()
    expect(container.querySelector('.metric-chart__tooltip')).toBeNull()

    const svg = container.querySelector('svg')!
    svg.getBoundingClientRect = () => ({
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 300,
      bottom: 140,
      width: 300,
      height: 140,
      toJSON: () => ({}),
    })
    fireEvent.mouseMove(svg, { clientX: 300 })
    expect(onHoverAtChange).toHaveBeenCalledWith(samples[4].observedAt)
    fireEvent.mouseLeave(svg)
    expect(onHoverAtChange).toHaveBeenCalledWith(null)
  })

  it('renders threshold lines even when value lies outside the Y range (clamped)', () => {
    // Data values 0..5; threshold at 50 — well outside range
    const samples: MetricChartSample[] = [
      { value: 1, observedAt: '2026-04-30T08:00:00Z' },
      { value: 3, observedAt: '2026-04-30T09:00:00Z' },
      { value: 5, observedAt: '2026-04-30T10:00:00Z' },
    ]
    const thresholds: MetricChartThreshold[] = [{ value: 50, tone: 'critical', label: '50%' }]
    const { container } = render(
      <MetricChart samples={samples} thresholds={thresholds} yMin={0} yMax={10} />,
    )

    const groups = container.querySelectorAll('.metric-chart__threshold')
    expect(groups).toHaveLength(1)
    const label = container.querySelector('.metric-chart__threshold-label')
    expect(label?.textContent).toBe('50%')
  })

  it('uses tone variant on the polyline stroke', () => {
    const samples = makeSamples(3)
    const { container } = render(<MetricChart samples={samples} tone="critical" />)
    const polyline = container.querySelector('polyline')
    expect(polyline).toBeTruthy()
    expect(polyline!.getAttribute('stroke')).toBe('var(--color-state-critical)')
  })
})

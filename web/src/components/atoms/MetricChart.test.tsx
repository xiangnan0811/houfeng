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

function sampleAt(samples: MetricChartSample[], index: number): MetricChartSample {
  const sample = samples[index]
  if (!sample) throw new Error(`fixture must include sample ${index}`)
  return sample
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

  it('uses fewer x-axis labels when the plot is narrow', () => {
    const samples = makeSamples(20)
    const wide = render(<MetricChart samples={samples} width={360} height={140} />)
    const narrow = render(<MetricChart samples={samples} width={200} height={140} />)
    const xLabels = (container: HTMLElement) =>
      Array.from(container.querySelectorAll('.metric-chart__axis-text'))
        .map((node) => node.textContent ?? '')
        .filter((text) => /^\d{2}:\d{2}$/.test(text))
    expect(xLabels(wide.container).length).toBe(5)
    expect(xLabels(narrow.container).length).toBe(3)
  })

  it('can render only domain-end Y ticks with units', () => {
    const samples: MetricChartSample[] = [
      { value: 50, observedAt: '2026-04-30T08:00:00Z' },
      { value: 200, observedAt: '2026-04-30T09:00:00Z' },
    ]
    const { container } = render(
      <MetricChart
        samples={samples}
        yMin={0}
        yMax={200}
        yTickCount={2}
        width={300}
        height={90}
        formatAxisValue={(value) => `${value} B/s`}
      />,
    )
    const yLabels = Array.from(container.querySelectorAll('.metric-chart__y-tick text')).map((node) => node.textContent)
    expect(yLabels).toEqual(['0 B/s', '200 B/s'])
  })

  it('renders an empty placeholder when samples is empty', () => {
    const { container, getByText } = render(<MetricChart samples={[]} />)
    expect(container.querySelector('polyline')).toBeNull()
    const emptyChart = container.querySelector('svg.metric-chart--empty')
    expect(emptyChart).toBeTruthy()
    expect(emptyChart).toHaveAttribute('width', '100%')
    expect(emptyChart).toHaveAttribute('height', '160')
    expect(emptyChart).not.toHaveAttribute('style')
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
    const start = sampleAt(samples, 3).observedAt
    const end = sampleAt(samples, 6).observedAt
    const windows: MetricChartMaintenanceWindow[] = [{ startedAt: start, endedAt: end }]
    const { container } = render(
      <MetricChart samples={samples} maintenanceWindows={windows} tone="maintenance" />,
    )

    const maintGroups = container.querySelectorAll('.metric-chart__maintenance')
    expect(maintGroups).toHaveLength(1)
    const maintenanceGroup = maintGroups[0]
    if (!maintenanceGroup) throw new Error('chart must render the maintenance group')
    // Should contain both the band rect and the top-edge marker triangle
    expect(maintenanceGroup.querySelector('rect')).toBeTruthy()
    expect(maintenanceGroup.querySelector('polygon')).toBeTruthy()
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
    const tooltipFrame = container.querySelector('.metric-chart__tooltip-frame')
    expect(tooltipFrame).toHaveAttribute('x')
    expect(tooltipFrame).toHaveAttribute('y')
    expect(tooltip).not.toHaveAttribute('style')
    const valueNode = container.querySelector('.metric-chart__tooltip-value')
    expect(valueNode).toBeTruthy()
    // Tooltip should display one of the sample values formatted by formatValue
    const formattedValues = samples.map((s) => s.value == null ? "—" : s.value.toFixed(1))
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
        hoveredAt={sampleAt(samples, 2).observedAt}
        formatValue={(v) => `${v.toFixed(1)}%`}
      />,
    )

    const tooltip = container.querySelector('.metric-chart__tooltip')
    expect(tooltip).toBeTruthy()
    expect(tooltip).toHaveTextContent('20.0%')
    expect(container.querySelector('.metric-chart__tooltip-frame')).toHaveAttribute('y')
    expect(container.querySelector('[style]')).not.toBeInTheDocument()
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
        hoveredAt={sampleAt(samples, 2).observedAt}
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
    expect(onHoverAtChange).toHaveBeenCalledWith(sampleAt(samples, 4).observedAt)
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

  it('keeps default auto-scale independent of thresholds unless opted in', () => {
    const samples: MetricChartSample[] = [
      { value: 1, observedAt: '2026-04-30T08:00:00Z' },
      { value: 2, observedAt: '2026-04-30T09:00:00Z' },
    ]
    const thresholds: MetricChartThreshold[] = [
      { value: 4, tone: 'notice', label: '4' },
      { value: 8, tone: 'critical', label: '8' },
    ]
    const { container } = render(
      <MetricChart samples={samples} thresholds={thresholds} yMin={0} width={300} height={140} />,
    )
    const ys = Array.from(container.querySelectorAll('.metric-chart__threshold line')).map((line) => line.getAttribute('y1'))
    expect(ys.length).toBe(2)
    expect(ys[0]).toBe(ys[1])
  })

  it('can fit unlocked Y scale to threshold values without changing locked domains', () => {
    const samples: MetricChartSample[] = [
      { value: 1, observedAt: '2026-04-30T08:00:00Z' },
      { value: 2, observedAt: '2026-04-30T09:00:00Z' },
    ]
    const thresholds: MetricChartThreshold[] = [
      { value: 4, tone: 'notice', label: '4' },
      { value: 8, tone: 'critical', label: '8' },
    ]
    const { container } = render(
      <MetricChart
        samples={samples}
        thresholds={thresholds}
        yMin={0}
        includeThresholdsInScale
        thresholdLabelPlacement="gutter"
        width={300}
        height={140}
      />,
    )
    const lines = Array.from(container.querySelectorAll('.metric-chart__threshold line'))
    const ys = lines.map((line) => line.getAttribute('y1'))
    expect(ys.length).toBe(2)
    expect(ys[0]).not.toBe(ys[1])
    const labels = Array.from(container.querySelectorAll('.metric-chart__threshold-label'))
    expect(labels.map((node) => node.getAttribute('x'))).toEqual(['298', '298'])
    expect(labels[0]?.getAttribute('y')).not.toBe(labels[1]?.getAttribute('y'))
    expect(container.querySelectorAll('.metric-chart__threshold-leader').length).toBe(2)


    const locked = render(
      <MetricChart samples={samples} thresholds={thresholds} yMin={0} yMax={10} includeThresholdsInScale width={300} height={140} />,
    )
    const lockedYs = Array.from(locked.container.querySelectorAll('.metric-chart__threshold line')).map((line) => line.getAttribute('y1'))
    expect(lockedYs.length).toBe(2)
    expect(lockedYs[0]).not.toBe(lockedYs[1])
    expect(locked.container.querySelector('.metric-chart__threshold line[y1="8"]')).toBeNull()
  })

  it('connects packed gutter labels to their threshold lines', () => {
    const samples: MetricChartSample[] = [
      { value: 10, observedAt: '2026-04-30T08:00:00Z' },
      { value: 40, observedAt: '2026-04-30T09:00:00Z' },
    ]
    const thresholds: MetricChartThreshold[] = [
      { value: 85, tone: 'notice', label: '85%' },
      { value: 92, tone: 'alert', label: '92%' },
      { value: 95, tone: 'critical', label: '95%' },
    ]
    const { container } = render(
      <MetricChart
        samples={samples}
        thresholds={thresholds}
        yMin={0}
        yMax={100}
        thresholdLabelPlacement="gutter"
        width={300}
        height={140}
      />,
    )
    expect(container.querySelectorAll('.metric-chart__threshold-label')).toHaveLength(3)
    const leaders = Array.from(container.querySelectorAll('.metric-chart__threshold-leader'))
    expect(leaders).toHaveLength(3)
    const lineYs = Array.from(container.querySelectorAll('.metric-chart__threshold line')).map((line) => line.getAttribute('y2'))
    const labelYs = Array.from(container.querySelectorAll('.metric-chart__threshold-label')).map((node) => node.getAttribute('y'))
    expect(leaders[1]?.getAttribute('points')).toContain(`,${lineYs[1]}`)
    expect(leaders[1]?.getAttribute('points')).toContain(`,${labelYs[1]}`)
    expect(labelYs[1]).not.toBe(lineYs[1])
  })

  it('uses tone variant on the polyline stroke', () => {
    const samples = makeSamples(3)
    const { container } = render(<MetricChart samples={samples} tone="critical" />)
    const polyline = container.querySelector('polyline')
    expect(polyline).toBeTruthy()
    expect(polyline!.getAttribute('stroke')).toBe('var(--color-state-critical)')
  })

  it('splits the trend at explicit gaps without synthesizing zero-valued samples', () => {
    const samples: MetricChartSample[] = [
      { value: 12, observedAt: '2026-04-30T08:00:00Z' },
      { value: 18, observedAt: '2026-04-30T08:05:00Z' },
      { value: 31, observedAt: '2026-04-30T08:20:00Z', gapBefore: true },
      { value: 27, observedAt: '2026-04-30T08:25:00Z' },
    ]

    const { container } = render(<MetricChart samples={samples} width={360} />)

    const polylines = Array.from(container.querySelectorAll('polyline'))
    expect(polylines).toHaveLength(2)
    expect(polylines.map((line) => new Set(line.getAttribute('points')?.split(' ')).size)).toEqual([2, 2])
    expect(container).not.toHaveTextContent('0.0')
  })

  it('keeps full time buckets with null values as gaps without synthesizing zero', () => {
    const samples: MetricChartSample[] = [
      { value: 12, observedAt: '2026-04-30T08:00:00Z' },
      { value: null, observedAt: '2026-04-30T08:05:00Z' },
      { value: 0, observedAt: '2026-04-30T08:10:00Z' },
      { value: 27, observedAt: '2026-04-30T08:15:00Z' },
    ]
    const { container } = render(<MetricChart samples={samples} width={360} />)
    const polylines = Array.from(container.querySelectorAll('polyline'))
    expect(polylines).toHaveLength(2)
    expect(container).not.toHaveTextContent('0.0')
    expect(container.querySelectorAll('.metric-chart__axis-text').length).toBeGreaterThan(4)
  })

  it('keeps isolated samples visible when gaps create one-point segments', () => {
    const samples: MetricChartSample[] = [
      { value: 12, observedAt: '2026-04-30T08:00:00Z' },
      { value: 31, observedAt: '2026-04-30T08:20:00Z', gapBefore: true },
      { value: 27, observedAt: '2026-04-30T08:25:00Z' },
    ]

    const { container } = render(<MetricChart samples={samples} width={360} />)

    const polylines = Array.from(container.querySelectorAll('polyline'))
    expect(polylines).toHaveLength(2)
    const isolatedPoints = polylines[0]?.getAttribute('points')?.split(' ')
    expect(isolatedPoints).toHaveLength(2)
    expect(new Set(isolatedPoints).size).toBe(1)
  })

  it('keeps default single-series behavior: no second line, default left gutter', () => {
    const samples = makeSamples(5)
    const { container } = render(<MetricChart samples={samples} yMin={0} yMax={100} />)

    expect(container.querySelectorAll('polyline')).toHaveLength(1)
    expect(container.querySelector('.metric-chart__line--secondary')).toBeNull()
    expect(container.querySelector('.metric-chart__alert-band')).toBeNull()
    expect(container.querySelector('.metric-chart__alert-band-line')).toBeNull()
    // PADDING_BASE.left (32) - 4
    expect(container.querySelector('.metric-chart__axis-text')?.getAttribute('x')).toBe('28')
  })

  it('widens the left gutter only when paddingLeft is passed', () => {
    const samples = makeSamples(5)
    const { container } = render(
      <MetricChart samples={samples} paddingLeft={64} yMin={0} yMax={100} />,
    )

    expect(container.querySelector('.metric-chart__axis-text')?.getAttribute('x')).toBe('60')
  })

  it('draws an equal-length second series with its own tone', () => {
    const samples = makeSamples(4)
    const secondary = samples.map((sample, i) => ({ ...sample, value: 10 + i }))

    const { container } = render(
      <MetricChart samples={samples} secondarySamples={secondary} yMin={0} yMax={100} />,
    )

    const secondaryLines = container.querySelectorAll('.metric-chart__line--secondary')
    expect(secondaryLines).toHaveLength(1)
    expect(secondaryLines[0]!.getAttribute('stroke')).toBe('var(--accent-2)')
    expect(secondaryLines[0]!.getAttribute('points')!.split(' ')).toHaveLength(4)
  })

  it('ignores a second series with mismatched length or timestamps', () => {
    const samples = makeSamples(4)
    const shortSeries = samples.slice(0, 3).map((sample) => ({ ...sample, value: 10 }))
    const shiftedSeries = samples.map((sample, i) => ({
      value: 10 + i,
      observedAt: new Date(new Date(sample.observedAt).getTime() + 60_000).toISOString(),
    }))

    const lengthMismatch = render(
      <MetricChart samples={samples} secondarySamples={shortSeries} />,
    )
    expect(lengthMismatch.container.querySelector('.metric-chart__line--secondary')).toBeNull()

    const timeMismatch = render(
      <MetricChart samples={samples} secondarySamples={shiftedSeries} />,
    )
    expect(timeMismatch.container.querySelector('.metric-chart__line--secondary')).toBeNull()
  })

  it('draws an aligned third series as a dashed muted line', () => {
    const samples = makeSamples(4)
    const secondary = samples.map((sample, i) => ({ ...sample, value: 10 + i }))
    const tertiary = samples.map((sample, i) => ({ ...sample, value: 4 + i }))

    const { container } = render(
      <MetricChart
        samples={samples}
        secondarySamples={secondary}
        tertiarySamples={tertiary}
        yMin={0}
        yMax={100}
      />,
    )

    const tertiaryLines = container.querySelectorAll('.metric-chart__line--tertiary')
    expect(tertiaryLines).toHaveLength(1)
    expect(tertiaryLines[0]!.getAttribute('stroke')).toBe('var(--text-secondary)')
    expect(tertiaryLines[0]!.getAttribute('stroke-dasharray')).toBe('4 3')
  })

  it('counts only a validated second series towards the empty state', () => {
    const allNull = makeSamples(3).map((sample) => ({ ...sample, value: null }))
    const secondary = allNull.map((sample, i) => ({ ...sample, value: 20 + i }))

    // Neither series has data → still empty.
    const ignored = render(
      <MetricChart samples={allNull} secondarySamples={allNull.map((s) => ({ ...s, value: null }))} />,
    )
    expect(ignored.container.querySelector('.metric-chart--empty')).toBeTruthy()

    // Primary all-null but the valid second series has data → chart is rendered.
    const drawn = render(<MetricChart samples={allNull} secondarySamples={secondary} />)
    expect(drawn.container.querySelector('.metric-chart--empty')).toBeNull()
    expect(drawn.container.querySelector('.metric-chart__line--secondary')).toBeTruthy()
  })

  it('does not count an ignored second series towards the empty state', () => {
    const allNull = makeSamples(3).map((sample) => ({ ...sample, value: null }))
    const invalidSecondary = allNull
      .slice(0, 2)
      .map((sample, i) => ({ ...sample, value: 20 + i }))

    const { container } = render(
      <MetricChart samples={allNull} secondarySamples={invalidSecondary} />,
    )

    expect(container.querySelector('.metric-chart--empty')).toBeTruthy()
  })

  it('draws a warning line at the threshold and does not fill the plot', () => {
    const samples = makeSamples(4)
    const { container } = render(
      <MetricChart samples={samples} yMin={0} yMax={100} alertBandFrom={80} />,
    )

    expect(container.querySelector('.metric-chart__alert-band')).toBeNull()
    const line = container.querySelector('.metric-chart__alert-band-line')
    expect(line).toBeTruthy()
    expect(line!.getAttribute('stroke')).toBe('var(--color-state-notice)')
    // 80% of a 0..100 axis sits 20% down from the plot top (padding.top=8, innerH=128).
    expect(Number(line!.getAttribute('y1'))).toBeCloseTo(8 + 128 * 0.2, 1)
  })

  it('omits the warning line when the value sits above the upper bound', () => {
    const samples = makeSamples(4)
    const { container } = render(
      <MetricChart samples={samples} yMin={0} yMax={100} alertBandFrom={120} />,
    )

    expect(container.querySelector('.metric-chart__alert-band-line')).toBeNull()
  })

  it('keeps the single-point hint unless a single point is opted in', () => {
    const single: MetricChartSample[] = [{ value: 42, observedAt: '2026-04-30T08:00:00Z' }]

    const defaultRender = render(<MetricChart samples={single} yMin={0} yMax={100} />)
    expect(defaultRender.container.querySelector('.metric-chart__hint')).toHaveTextContent('样本不足')
    expect(defaultRender.container.querySelectorAll('polyline')).toHaveLength(0)

    const optedIn = render(
      <MetricChart samples={single} yMin={0} yMax={100} allowSinglePoint />,
    )
    expect(optedIn.container.querySelector('.metric-chart__hint')).toBeNull()
    expect(optedIn.container.querySelectorAll('polyline')).toHaveLength(0)
    const dot = optedIn.container.querySelector('circle')
    expect(dot).toBeTruthy()
    expect(dot!.getAttribute('r')).toBe('3')
  })

  it('does not draw a single-point dot when the lone value is a gap', () => {
    const single: MetricChartSample[] = [{ value: null, observedAt: '2026-04-30T08:00:00Z' }]
    const { container } = render(<MetricChart samples={single} allowSinglePoint />)

    expect(container.querySelector('.metric-chart--empty')).toBeTruthy()
  })

  it('draws an empty-label threshold line without any label text', () => {
    const samples = makeSamples(5)
    const { container } = render(
      <MetricChart
        samples={samples}
        yMin={0}
        yMax={100}
        thresholds={[
          { value: 80, tone: 'notice', label: '' },
          { value: 90, tone: 'alert', label: '告警 90' },
        ]}
      />,
    )

    expect(container.querySelectorAll('.metric-chart__threshold')).toHaveLength(2)
    const labels = Array.from(container.querySelectorAll('.metric-chart__threshold-label'))
    expect(labels).toHaveLength(1)
    expect(labels[0]).toHaveTextContent('告警 90')
  })

  it('falls back to formatValue when a threshold label is omitted', () => {
    const samples = makeSamples(5)
    const { container } = render(
      <MetricChart
        samples={samples}
        yMin={0}
        yMax={100}
        formatValue={(v) => `${v.toFixed(0)} pct`}
        thresholds={[{ value: 80, tone: 'notice' }]}
      />,
    )

    expect(container.querySelector('.metric-chart__threshold-label')).toHaveTextContent('80 pct')
  })
})

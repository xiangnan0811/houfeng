import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ComparisonTrendChart } from './ComparisonTrendChart'

describe('ComparisonTrendChart', () => {
  it('draws one polyline per gap and stays empty for other kinds', () => {
    const { container, rerender } = render(
      <ComparisonTrendChart
        kind="monitoring.host/v1"
        series={[{
          item_index: 0,
          metric_id: 'cpu',
          unit: '%',
          segments: [[{ start: 'a', end: 'b', value: 1 }], [{ start: 'c', end: 'd', value: 0 }]],
        }]}
      />,
    )
    expect(container.querySelectorAll('polyline[data-segment]')).toHaveLength(2)
    rerender(<ComparisonTrendChart
      kind="monitoring.host/v1"
      metric="cpu_usage_pct"
      series={[
        {
          item_index: 0,
          metric_id: 'cpu',
          unit: '%',
          segments: [[{ start: 'a', end: 'b', value: 1 }]],
        },
        {
          item_index: 0,
          metric_id: 'cpu_usage_pct',
          unit: '%',
          segments: [[{ start: 'a', end: 'b', value: 1 }]],
        },
      ]}
    />)
    expect(container.querySelectorAll('polyline[data-segment]')).toHaveLength(1)
    rerender(<ComparisonTrendChart kind="command.audit/v1" series={[{
      item_index: 0,
      metric_id: 'cpu',
      unit: '%',
      segments: [[{ start: 'a', end: 'b', value: 1 }]],
    }]} />)
    expect(container.querySelectorAll('polyline[data-segment]')).toHaveLength(0)
  })
})

describe('ComparisonTrendChart overlay', () => {
  it('overlays items on a relative time axis with a legend and dots for single buckets', () => {
    const { container } = render(
      <ComparisonTrendChart
        kind="monitoring.host/v1"
        metric="cpu"
        series={[
          {
            item_index: 0,
            metric_id: 'cpu',
            unit: '%',
            segments: [[
              { start: '2026-07-01T00:00:00Z', end: '2026-07-01T01:00:00Z', value: 10 },
              { start: '2026-07-01T01:00:00Z', end: '2026-07-01T02:00:00Z', value: 20 },
            ]],
          },
          {
            item_index: 1,
            metric_id: 'cpu',
            unit: '%',
            segments: [[{ start: '2026-07-08T00:00:00Z', end: '2026-07-08T01:00:00Z', value: 30 }]],
          },
        ]}
      />,
    )
    expect(screen.getByRole('img', { name: 'cpu 趋势：第 1 项 1 段，第 2 项 1 段' })).toBeInTheDocument()
    expect(screen.getAllByRole('listitem').map((item) => item.textContent)).toEqual(['第 1 项', '第 2 项'])
    const lines = container.querySelectorAll('polyline[data-segment]')
    expect(lines[0]).toHaveClass('record-compare-chart__line')
    // 第 2 项与第 1 项同样从相对起点开始，而不是被画到一周之后。
    expect(lines[1]).toHaveClass('record-compare-chart__dot')
    expect(lines[1]?.getAttribute('points')?.split(' ')[0]?.startsWith('0,')).toBe(true)
    expect(screen.getByText('+1 时')).toBeInTheDocument()
  })
})

describe('ComparisonTrendChart scale', () => {
  it('places labels, gridlines and data on one padded value domain', () => {
    const { container } = render(
      <ComparisonTrendChart
        kind="monitoring.host/v1"
        metric="cpu"
        series={[{
          item_index: 0,
          metric_id: 'cpu',
          unit: '%',
          segments: [[
            { start: '2026-07-01T00:00:00Z', end: '2026-07-01T01:00:00Z', value: 0 },
            { start: '2026-07-01T01:00:00Z', end: '2026-07-01T02:00:00Z', value: 100 },
          ]],
        }]}
      />,
    )
    // 0–100 外扩 8%：标签即网格所在值，数据点落在网格之间。
    expect([...container.querySelectorAll('.record-compare-chart__y span')].map((node) => node.textContent))
      .toEqual(['108', '50', '-8'])
    expect([...container.querySelectorAll('line.record-compare-chart__grid')].map((line) => Number(line.getAttribute('y1'))))
      .toEqual([0, 100, 200])
    const points = container.querySelector('polyline[data-segment]')?.getAttribute('points')?.split(' ') ?? []
    expect(points.map((point) => Math.round(Number(point.split(',')[1])))).toEqual([186, 14])
  })

  it('falls back to bucket order for every series when any timestamp is unreadable', () => {
    render(
      <ComparisonTrendChart
        kind="monitoring.host/v1"
        metric="cpu"
        series={[
          { item_index: 0, metric_id: 'cpu', unit: '', segments: [[
            { start: '2026-07-01T00:00:00Z', end: '2026-07-01T01:00:00Z', value: 1 },
            { start: '2026-07-01T05:00:00Z', end: '2026-07-01T06:00:00Z', value: 2 },
          ]] },
          { item_index: 1, metric_id: 'cpu', unit: '', segments: [[{ start: 'bad', end: 'bad', value: 3 }]] },
          { item_index: 2, metric_id: 'cpu', unit: '', segments: [[{ start: 'bad', end: 'bad', value: 4 }]] },
        ]}
      />,
    )
    expect(screen.getAllByText('#2').length).toBeGreaterThan(0)
    expect(screen.queryByText(/时$/)).not.toBeInTheDocument()
    expect(screen.getAllByRole('listitem').map((item) => item.className)).toEqual([
      'record-compare-series--1', 'record-compare-series--2', 'record-compare-series--3',
    ])
  })
})

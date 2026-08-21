import { render } from '@testing-library/react'
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
    expect(container.querySelectorAll('polyline')).toHaveLength(2)
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
    expect(container.querySelectorAll('polyline')).toHaveLength(1)
    rerender(<ComparisonTrendChart kind="command.audit/v1" series={[{
      item_index: 0,
      metric_id: 'cpu',
      unit: '%',
      segments: [[{ start: 'a', end: 'b', value: 1 }]],
    }]} />)
    expect(container.querySelectorAll('polyline')).toHaveLength(0)
  })
})

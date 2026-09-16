import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { SubscriptionSeriesPoint } from '../../lib/types'
import { BudgetCostTrendChart } from './BudgetCostTrendChart'

describe('BudgetCostTrendChart component', () => {
  const sampleBuckets: SubscriptionSeriesPoint[] = [
    {
      bucket: '2025-07',
      monthly_cost: 60,
      budget_limit: 80,
      budget_currency: 'CNY',
      renewal_count: 0,
      data_insufficient: false,
    },
    {
      bucket: '2025-08',
      monthly_cost: 110,
      budget_limit: 90,
      budget_currency: 'CNY',
      renewal_count: 0,
      data_insufficient: false,
    },
    {
      bucket: '2025-09',
      monthly_cost: 95,
      budget_limit: null, // missing budget gap
      budget_currency: 'CNY',
      renewal_count: 0,
      data_insufficient: false,
    },
    {
      bucket: '2025-10',
      monthly_cost: 85,
      budget_limit: 85,
      budget_currency: 'CNY',
      renewal_count: 0,
      data_insufficient: false,
    },
  ]

  it('renders SVG chart with accessible name and defaults active readout to the latest month', () => {
    const { container } = render(
      <BudgetCostTrendChart buckets={sampleBuckets} baseCurrency="CNY" />,
    )

    const svg = screen.getByRole('region', { name: /月度成本与预算趋势图表/ })
    expect(svg).toBeInTheDocument()
    expect(svg).toHaveAttribute('tabindex', '0')

    // Readout defaults to last month (2025-10) using real formatMoney
    const readout = screen.getByRole('status')
    expect(readout).toBeInTheDocument()
    expect(readout).toHaveTextContent('2025-10')
    expect(readout).toHaveTextContent('CNY 85.00')
    expect(readout).toHaveTextContent('持平')
    expect(readout).toHaveTextContent('0.0%')

    // Only active marks are rendered (1 hairline, at most 2 circles), NOT all-month circles
    const circles = container.querySelectorAll('circle.subscription-trend-chart__point')
    expect(circles.length).toBeLessThanOrEqual(2)
    expect(container.querySelector('.subscription-trend-chart__hairline')).toBeInTheDocument()
  })

  it('keeps fractional axis ticks consistent with an adaptive small-cost scale', () => {
    render(
      <BudgetCostTrendChart
        buckets={sampleBuckets.slice(0, 2).map((bucket) => ({ ...bucket, monthly_cost: 4, budget_limit: null }))}
        baseCurrency="USD"
      />,
    )
    // Adaptive domain for flat cost 4: [3.6, 4, 4.4] (non-zero yMin, truthful fractions)
    expect(screen.getByText('3.6')).toBeInTheDocument()
    expect(screen.getByText('4.4')).toBeInTheDocument()
    expect(screen.queryByText('0')).not.toBeInTheDocument()
  })

  it('navigates through months using keyboard (ArrowLeft, ArrowRight, Home, End)', () => {
    render(<BudgetCostTrendChart buckets={sampleBuckets} baseCurrency="CNY" />)

    const svg = screen.getByRole('region', { name: /月度成本与预算趋势图表/ })
    const readout = screen.getByRole('status')

    // Initial: 2025-10
    expect(readout).toHaveTextContent('2025-10')

    // ArrowLeft -> 2025-09 (missing budget gap)
    fireEvent.keyDown(svg, { key: 'ArrowLeft' })
    expect(readout).toHaveTextContent('2025-09')
    expect(readout).toHaveTextContent('CNY 95.00')
    expect(readout).toHaveTextContent('—') // budget is null
    expect(readout).not.toHaveTextContent('超预算')
    expect(readout).not.toHaveTextContent('%')

    // ArrowLeft -> 2025-08 (over budget)
    fireEvent.keyDown(svg, { key: 'ArrowLeft' })
    expect(readout).toHaveTextContent('2025-08')
    expect(readout).toHaveTextContent('CNY 110.00')
    expect(readout).toHaveTextContent('CNY 90.00')
    expect(readout).toHaveTextContent('+CNY 20.00')
    expect(readout).toHaveTextContent('超预算')
    expect(readout).toHaveTextContent('+22.2%')

    // Home -> 2025-07 (under budget)
    fireEvent.keyDown(svg, { key: 'Home' })
    expect(readout).toHaveTextContent('2025-07')
    expect(readout).toHaveTextContent('CNY 60.00')
    expect(readout).toHaveTextContent('CNY 80.00')
    expect(readout).toHaveTextContent('-CNY 20.00')
    expect(readout).toHaveTextContent('低于预算')
    expect(readout).toHaveTextContent('-25.0%')

    // End -> 2025-10
    fireEvent.keyDown(svg, { key: 'End' })
    expect(readout).toHaveTextContent('2025-10')
  })

  it('steps keyboard navigation safely even if bucket list shrinks', () => {
    const { rerender } = render(
      <BudgetCostTrendChart buckets={sampleBuckets} baseCurrency="CNY" />,
    )
    const svg = screen.getByRole('region', { name: /月度成本与预算趋势图表/ })
    const readout = screen.getByRole('status')
    expect(readout).toHaveTextContent('2025-10')

    // Rerender with fewer buckets (indices 0 and 1: 2025-07 and 2025-08)
    rerender(
      <BudgetCostTrendChart buckets={sampleBuckets.slice(0, 2)} baseCurrency="CNY" />,
    )
    // Clamps to safeIndex (2025-08)
    expect(readout).toHaveTextContent('2025-08')

    // ArrowLeft steps cleanly from safeIndex (1 -> 0)
    fireEvent.keyDown(svg, { key: 'ArrowLeft' })
    expect(readout).toHaveTextContent('2025-07')
  })

  it('updates active month via pointer hit columns', () => {
    const { container } = render(
      <BudgetCostTrendChart buckets={sampleBuckets} baseCurrency="CNY" />,
    )

    const hitColumns = container.querySelectorAll<SVGRectElement>('.subscription-trend-chart__hit-column')
    expect(hitColumns.length).toBe(sampleBuckets.length)

    // Click on 2025-08 column (index 1)
    fireEvent.click(hitColumns[1]!)
    const readout = screen.getByRole('status')
    expect(readout).toHaveTextContent('2025-08')
    expect(readout).toHaveTextContent('超预算')
  })

  it('handles zero budget properly without NaN or divide by zero', () => {
    const zeroBudgetBuckets: SubscriptionSeriesPoint[] = [
      {
        bucket: '2025-07',
        monthly_cost: 50,
        budget_limit: 0,
        budget_currency: 'CNY',
        renewal_count: 0,
        data_insufficient: false,
      },
      {
        bucket: '2025-08',
        monthly_cost: 70,
        budget_limit: 0,
        budget_currency: 'CNY',
        renewal_count: 0,
        data_insufficient: false,
      },
    ]

    render(<BudgetCostTrendChart buckets={zeroBudgetBuckets} baseCurrency="CNY" />)

    const readout = screen.getByRole('status')
    expect(readout).toHaveTextContent('CNY 70.00')
    expect(readout).toHaveTextContent('CNY 0.00')
    expect(readout).toHaveTextContent('+CNY 70.00')
    expect(readout).toHaveTextContent('超预算')
    expect(readout).not.toHaveTextContent('%')
    expect(readout).not.toHaveTextContent('NaN')
    expect(readout).not.toHaveTextContent('Infinity')
  })

  it('renders difference areas and segmented budget line with gaps', () => {
    const { container } = render(
      <BudgetCostTrendChart buckets={sampleBuckets} baseCurrency="CNY" />,
    )

    const costLine = container.querySelector('.subscription-trend-chart__line--cost')
    expect(costLine).toBeInTheDocument()

    const budgetLine = container.querySelector('.subscription-trend-chart__line--budget')
    expect(budgetLine).toBeInTheDocument()

    const overAreas = container.querySelectorAll('.subscription-trend-chart__area--over')
    const underAreas = container.querySelectorAll('.subscription-trend-chart__area--under')
    expect(overAreas.length + underAreas.length).toBeGreaterThan(0)
  })
})

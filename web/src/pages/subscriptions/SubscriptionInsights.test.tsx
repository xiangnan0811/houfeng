import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SubscriptionOverview, SubscriptionRenewalQueueItem } from '../../lib/types'
import { SubscriptionInsights } from './SubscriptionInsights'

function overviewWith(patch: Partial<SubscriptionOverview>): SubscriptionOverview {
  return {
    snapshot_generated_at: '2026-09-26T00:00:00Z', base_currency: 'CNY', total_monthly_cost: 25, total_yearly_cost: 300,
    active_subscription_count: 1, renewal_due_14d_count: 0, renewal_due_30d_count: 0, budget_risk_count: 0,
    exchange_rate_stale_count: 0, decision_attention_count: 0, missing_subscription_vps_count: 0,
    upcoming_renewals: [], provider_breakdown: [], currency_breakdown: [], category_breakdown: [], budget_risks: [], vps_costs: [], missing_subscription_assets: [],
    archived_potential_costs: [], archived_potential_monthly_cost: 0, archived_unknown_amount_count: 0,
    archived_missing_subscription_assets: [],
    ...patch,
  }
}

function renderInsights(overview: SubscriptionOverview, onSelectVPS = vi.fn()) {
  render(<MemoryRouter><SubscriptionInsights overview={overview} overviewLoading={false} overviewError={null} statistics={null} statisticsLoading={false} statisticsError={null} onRetryStatistics={vi.fn()} baseCurrency="CNY" breakdownKind="provider" onBreakdownKindChange={vi.fn()} onSelectVPS={onSelectVPS} /></MemoryRouter>)
  return onSelectVPS
}

function renewal(id: string, renewAt: string, decision: string, patch: Partial<SubscriptionRenewalQueueItem> = {}): SubscriptionRenewalQueueItem {
  return {
    subscription_id: `sub_${id}`, vps_id: `vps_${id}`, vps_display_name: `VPS ${id}`, display_name: `Plan ${id}`,
    provider_name: 'Example Cloud', renew_at: renewAt, monthly_price_base: 42, yearly_price_base: 504,
    base_currency: 'CNY', currency: 'USD', renewal_decision: decision, lifecycle_status: 'active', exchange_rate_stale: false,
    ...patch,
  }
}

describe('archived potential charges', () => {
  it('keeps missing amounts explicit and provides an archived VPS review link', () => {
    renderInsights(overviewWith({
      archived_potential_monthly_cost: null, archived_unknown_amount_count: 1,
      archived_missing_subscription_assets: [{ vps_id: 'vps_old', display_name: '旧服务器', provider_name: 'Example', lifecycle_status: 'archived', renewal_decision: 'cancel' }],
    }))
    const charges = screen.getByRole('region', { name: '已归档资产潜在扣费' })
    expect(within(charges).getByText('1 项金额未知，未按零费用处理。')).toBeInTheDocument()
    expect(within(charges).getByText('缺少账单 · 金额待核对')).toBeInTheDocument()
    expect(within(charges).getByRole('link', { name: '核对扣费' })).toHaveAttribute('href', '/vps/vps_old')
    expect(charges).toHaveTextContent('不计入当前成本 · 金额待核对')
    expect(charges).not.toHaveTextContent('¥0.00')
  })
})

describe('renewal queue', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows days left, highlights near renewals and labels each renewal decision', () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 9, 2, 10, 0))
    const onSelectVPS = renderInsights(overviewWith({
      upcoming_renewals: [
        renewal('a', '2026-09-30', 'unreviewed'),
        renewal('b', '2026-10-06', 'keep', { exchange_rate_stale: true }),
        renewal('c', '2026-10-23', 'cancel'),
        renewal('d', '2026-11-09', 'mystery'),
      ],
    }))
    const rows = within(screen.getByRole('region', { name: '续费队列' })).getAllByRole('button')
    expect(rows.map((row) => row.getAttribute('data-urgency'))).toEqual(['overdue', 'soon', 'later', 'later'])
    expect(rows[0]).toHaveTextContent('已过期 2 天')
    expect(rows[0]).toHaveTextContent('待决定')
    expect(rows[1]).toHaveTextContent('4 天后')
    expect(rows[1]).toHaveTextContent('汇率过期')
    expect(rows[2]).toHaveTextContent('决定不续费')
    // 未知决策原样显示，不猜测含义。
    expect(rows[3]).toHaveTextContent('mystery')
    expect(rows[3]).toHaveTextContent('VPS d · Example Cloud')
    fireEvent.click(rows[2]!)
    expect(onSelectVPS).toHaveBeenCalledWith('vps_c')
  })

  it('collapses an empty queue into a single compact line', () => {
    renderInsights(overviewWith({}))
    expect(screen.queryByRole('region', { name: '续费队列' })).not.toBeInTheDocument()
    expect(screen.getByText('暂无临近续费')).toBeInTheDocument()
  })
})

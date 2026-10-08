import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SubscriptionCostRow, SubscriptionOverview, SubscriptionRenewalQueueItem } from '../../lib/types'
import { SubscriptionInsights } from './SubscriptionInsights'

function overviewWith(patch: Partial<SubscriptionOverview>): SubscriptionOverview {
  return {
    snapshot_generated_at: '2026-09-26T00:00:00Z', base_currency: 'CNY', total_monthly_cost: 25, total_yearly_cost: 300,
    active_subscription_count: 1, renewal_due_14d_count: 0, renewal_due_30d_count: 0, budget_risk_count: 0,
    current_missing_rate_count: 0, current_stale_rate_count: 0, current_unknown_amount_count: 0, decision_attention_count: 0, missing_subscription_vps_count: 0,
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
    base_currency: 'CNY', currency: 'USD', renewal_decision: decision, lifecycle_status: 'active', exchange_rate_status: 'fresh',
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
        renewal('b', '2026-10-06', 'keep', { exchange_rate_status: 'stale' }),
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
    expect(rows[1]).toHaveTextContent('CNY 42.00')
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

function costRow(patch: Partial<SubscriptionCostRow> = {}): SubscriptionCostRow {
  return {
    subscription_id: 'sub_cost',
    vps_id: 'vps_cost',
    vps_display_name: 'Known Box',
    provider_id: 'pv_cost',
    provider_name: 'Example Cloud',
    display_name: 'Known plan',
    cost_category: 'compute',
    labels: [],
    price: 12,
    currency: 'USD',
    monthly_price: 12,
    monthly_price_base: 84,
    yearly_price_base: 1008,
    base_currency: 'CNY',
    exchange_rate_status: 'fresh',
    status: 'active',
    payment_method: 'card',
    country: 'JP',
    region: 'Kanto',
    lifecycle_status: 'active',
    renewal_decision: 'keep',
    budget_status: 'ok',
    ...patch,
  }
}

describe('known amount charts', () => {
  it('keeps a true zero in the ranking instead of treating it as missing', () => {
    renderInsights(overviewWith({
      active_subscription_count: 1,
      total_monthly_cost: 0,
      current_unknown_amount_count: 0,
      vps_costs: [costRow({ display_name: 'Zero plan', price: 0, monthly_price: 0, monthly_price_base: 0, yearly_price_base: 0, exchange_rate_status: 'identity' })],
    }))
    fireEvent.click(screen.getByRole('tab', { name: '排行' }))
    const ranking = screen.getByRole('region', { name: '月成本排行' })
    expect(within(ranking).getByText('Zero plan')).toBeInTheDocument()
    expect(ranking).toHaveTextContent('CNY 0.00')
    expect(ranking).not.toHaveTextContent('金额待核对')
  })

  it('shows a known subtotal and limits chart share to known money', () => {
    renderInsights(overviewWith({
      active_subscription_count: 2,
      total_monthly_cost: 84,
      current_unknown_amount_count: 1,
      current_missing_rate_count: 1,
      vps_costs: [
        costRow(),
        costRow({ subscription_id: 'sub_missing', vps_id: 'vps_missing', display_name: 'Missing plan', monthly_price_base: null, yearly_price_base: null, exchange_rate_status: 'missing' }),
      ],
    }))
    expect(screen.getAllByText('已知金额小计（另有 1 项待核对），占比仅指已知金额')).toHaveLength(2)
    const pie = screen.getByRole('region', { name: '月成本饼图' })
    expect(pie).toHaveTextContent('CNY 84.00')
    expect(pie).not.toHaveTextContent('Missing plan')
  })

  it('does not draw an all-unknown total as zero money', () => {
    renderInsights(overviewWith({
      active_subscription_count: 1,
      total_monthly_cost: 0,
      current_unknown_amount_count: 1,
      current_missing_rate_count: 1,
      vps_costs: [costRow({ monthly_price_base: null, yearly_price_base: null, exchange_rate_status: 'missing' })],
    }))
    const month = screen.getByRole('heading', { name: '月成本' }).closest('.subscription-insight-panel')
    const composition = screen.getByRole('heading', { name: '成本构成' }).closest('.subscription-insight-panel')
    expect(month).toHaveTextContent('金额待核对')
    expect(month).not.toHaveTextContent('0.00')
    expect(composition).toHaveTextContent('金额待核对')
    expect(composition).not.toHaveTextContent('0.00')
    expect(screen.queryByRole('region', { name: '月成本饼图' })).not.toBeInTheDocument()
  })

  it('keeps a stale numeric amount in the ranking and on an archived charge', () => {
    renderInsights(overviewWith({
      total_monthly_cost: 84,
      current_stale_rate_count: 1,
      vps_costs: [costRow({ exchange_rate_status: 'stale', display_name: 'Stale plan' })],
      archived_potential_costs: [costRow({
        subscription_id: 'sub_archived',
        vps_id: 'vps_archived',
        vps_display_name: 'Old Box',
        exchange_rate_status: 'stale',
        monthly_price_base: 18,
      })],
      archived_potential_monthly_cost: 18,
      archived_unknown_amount_count: 0,
    }))
    fireEvent.click(screen.getByRole('tab', { name: '排行' }))
    const ranking = screen.getByRole('region', { name: '月成本排行' })
    expect(ranking).toHaveTextContent('Stale plan')
    expect(ranking).toHaveTextContent('CNY 84.00')
    expect(ranking).toHaveTextContent('汇率过期')
    const charges = screen.getByRole('region', { name: '已归档资产潜在扣费' })
    expect(charges).toHaveTextContent('CNY 18.00')
    expect(charges).toHaveTextContent('汇率过期')
    expect(charges).not.toHaveTextContent('金额待核对')
  })
})

describe('cost trend gap', () => {
  it('describes a missing history in Chinese without the bucket field name', () => {
    renderInsights(overviewWith({}))
    expect(screen.getByText('历史成本数据不足')).toBeInTheDocument()
    expect(screen.getByText('后端未返回足够的历史月成本与月预算分档。')).toBeInTheDocument()
    expect(screen.queryByText(/bucket/i)).not.toBeInTheDocument()
  })
})

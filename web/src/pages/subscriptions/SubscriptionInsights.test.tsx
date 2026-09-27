import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import type { SubscriptionOverview } from '../../lib/types'
import { SubscriptionInsights } from './SubscriptionInsights'

describe('archived potential charges', () => {
  it('keeps missing amounts explicit and provides an archived VPS review link', () => {
    const overview: SubscriptionOverview = {
      snapshot_generated_at: '2026-09-26T00:00:00Z', base_currency: 'CNY', total_monthly_cost: 25, total_yearly_cost: 300,
      active_subscription_count: 1, renewal_due_14d_count: 0, renewal_due_30d_count: 0, budget_risk_count: 0,
      exchange_rate_stale_count: 0, decision_attention_count: 0, missing_subscription_vps_count: 0,
      upcoming_renewals: [], provider_breakdown: [], currency_breakdown: [], category_breakdown: [], budget_risks: [], vps_costs: [], missing_subscription_assets: [],
      archived_potential_costs: [], archived_potential_monthly_cost: null, archived_unknown_amount_count: 1,
      archived_missing_subscription_assets: [{ vps_id: 'vps_old', display_name: '旧服务器', provider_name: 'Example', lifecycle_status: 'archived', renewal_decision: 'cancel' }],
    }
    render(<MemoryRouter><SubscriptionInsights overview={overview} overviewLoading={false} overviewError={null} statistics={null} statisticsLoading={false} statisticsError={null} onRetryStatistics={vi.fn()} baseCurrency="CNY" breakdownKind="provider" onBreakdownKindChange={vi.fn()} onSelectVPS={vi.fn()} /></MemoryRouter>)
    const charges = screen.getByRole('region', { name: '已归档资产潜在扣费' })
    expect(within(charges).getByText('1 项金额未知，未按零费用处理。')).toBeInTheDocument()
    expect(within(charges).getByText('缺少账单 · 金额待核对')).toBeInTheDocument()
    expect(within(charges).getByRole('link', { name: '核对扣费' })).toHaveAttribute('href', '/vps/vps_old')
    expect(charges).not.toHaveTextContent('¥0.00')
  })
})

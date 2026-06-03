import { type KeyboardEvent } from 'react'

import { MetricChart, StatusGlyph, Tabs, type HealthState } from '../../components/atoms'
import { formatMoney } from '../../lib/format'
import type {
  SubscriptionBreakdownItem,
  SubscriptionCostRow,
  SubscriptionOverview,
  SubscriptionSeriesPoint,
  SubscriptionStatistics,
} from '../../lib/types'

export type SubscriptionBreakdownKind = 'provider' | 'category' | 'currency'

type DonutItem = {
  key: string
  label: string
  cost: number
  vpsID: string | null
  isOther: boolean
}

type SubscriptionInsightsProps = {
  overview: SubscriptionOverview | null
  statistics: SubscriptionStatistics | null
  statisticsLoading: boolean
  statisticsError: string | null
  baseCurrency: string
  breakdownKind: SubscriptionBreakdownKind
  onBreakdownKindChange: (kind: SubscriptionBreakdownKind) => void
  onSelectVPS: (vpsID: string) => void
}

const DONUT_COLORS = [
  'var(--accent)',
  'var(--color-state-normal)',
  'var(--color-state-notice)',
  'var(--accent-2)',
  'var(--color-state-maintenance)',
  'var(--text-muted)',
]

const BREAKDOWN_TABS = [
  { value: 'provider', label: '服务商' },
  { value: 'category', label: '分类' },
  { value: 'currency', label: '币种' },
] as const

function money(value?: number | null, currency = 'CNY'): string {
  if (value == null || Number.isNaN(value)) return '-'
  return formatMoney(value, currency)
}

function compactAmount(value: number): string {
  if (!Number.isFinite(value)) return '-'
  if (Math.abs(value) >= 1000) return `${(value / 1000).toFixed(1)}k`
  return value.toFixed(value >= 100 ? 0 : 2)
}

function barWidth(value: number, max: number): string {
  if (!Number.isFinite(value) || !Number.isFinite(max) || max <= 0) return '0%'
  return `${Math.max(4, Math.min(100, (value / max) * 100))}%`
}

function monthLabel(bucket: string): string {
  const [year, month] = bucket.split('-')
  if (!year || !month) return bucket
  return `${year.slice(2)}/${month}`
}

function monthTooltipLabel(observedAt: string): string {
  const bucket = observedAt.slice(0, 7)
  const [year, month] = bucket.split('-')
  if (!year || !month) return observedAt
  return `${year} 年 ${month} 月`
}

function budgetTone(status?: string | null): HealthState {
  if (status === 'over') return 'critical'
  if (status === 'warning') return 'alert'
  if (status === 'ok') return 'normal'
  if (status === 'disabled') return 'maintenance'
  return 'notice'
}

function budgetStatusLabel(status?: string | null): string {
  const map: Record<string, string> = {
    disabled: '已停用',
    ok: '预算内',
    warning: '接近上限',
    over: '已超预算',
    unknown: '未匹配',
  }
  return map[status ?? ''] ?? (status || '-')
}

function buildDonutItems(rows: SubscriptionCostRow[]): DonutItem[] {
  const sorted = rows
    .map((row) => ({
      key: row.vps_id,
      label: row.display_name || row.vps_display_name || row.vps_id,
      cost: row.monthly_price_base ?? 0,
      vpsID: row.vps_id,
      isOther: false,
    }))
    .filter((item) => item.cost > 0)
    .sort((left, right) => right.cost - left.cost)

  const top = sorted.slice(0, 5)
  const other = sorted.slice(5)
  if (other.length === 0) return top
  const otherCost = other.reduce((sum, item) => sum + item.cost, 0)
  return [
    ...top,
    { key: 'other', label: '其他', cost: otherCost, vpsID: null, isOther: true },
  ]
}

function breakdownItems(statistics: SubscriptionStatistics | null, kind: SubscriptionBreakdownKind): SubscriptionBreakdownItem[] {
  if (!statistics) return []
  if (kind === 'category') return statistics.category_breakdown
  if (kind === 'currency') return statistics.currency_breakdown
  return statistics.provider_breakdown
}

function trendSamples(buckets: SubscriptionSeriesPoint[]) {
  return buckets.map((bucket) => ({
    value: bucket.monthly_cost,
    observedAt: `${bucket.bucket}-01T00:00:00Z`,
  }))
}

function handleKeyActivate(event: KeyboardEvent, run: () => void) {
  if (event.key !== 'Enter' && event.key !== ' ') return
  event.preventDefault()
  run()
}

export function SubscriptionInsights({
  overview,
  statistics,
  statisticsLoading,
  statisticsError,
  baseCurrency,
  breakdownKind,
  onBreakdownKindChange,
  onSelectVPS,
}: SubscriptionInsightsProps) {
  const donutItems = buildDonutItems(overview?.vps_costs ?? [])
  const donutTotal = donutItems.reduce((sum, item) => sum + item.cost, 0)
  const circumference = 2 * Math.PI * 52
  const donutSegments = donutItems.map((item, index) => {
    const priorCost = donutItems.slice(0, index).reduce((sum, prior) => sum + prior.cost, 0)
    const length = donutTotal > 0 ? (item.cost / donutTotal) * circumference : 0
    const dashOffset = donutTotal > 0 ? -((priorCost / donutTotal) * circumference) : 0
    return { item, index, length, dashOffset }
  })
  const costBuckets = statistics?.cost_month_buckets ?? []
  const hasInsufficientTrendData = costBuckets.some((bucket) => bucket.data_insufficient)
  const hasTrend = !hasInsufficientTrendData && costBuckets.length >= 2 && costBuckets.some((bucket) => bucket.monthly_cost > 0)
  const renewalBuckets = (statistics?.renewal_month_buckets ?? []).filter((bucket) => bucket.renewal_count > 0 || bucket.monthly_cost > 0)
  const currentBreakdown = breakdownItems(statistics, breakdownKind)
  const breakdownMax = Math.max(...currentBreakdown.map((item) => item.monthly_cost), 0)

  return (
    <section className="subscription-insights animate-in" aria-label="订阅成本洞察">
      <div className="section-heading">
        <p className="section-heading__eyebrow">Cost Insights</p>
        <h2 className="section-heading__title">成本洞察</h2>
      </div>
      <div className="subscription-insights__grid">
        <div className="page-panel subscription-insight-panel subscription-insight-panel--occupancy">
          <div className="section-heading section-heading--inline">
            <div>
              <p className="section-heading__eyebrow">This Month</p>
              <h3 className="section-heading__title">本月 VPS 成本占用</h3>
            </div>
            <span className="section-heading__meta">{money(donutTotal, baseCurrency)}</span>
          </div>
          {donutItems.length === 0 ? (
            <p className="asset-table-empty-state">
              <strong>暂无可展示成本</strong>
              <span>当前没有可换算为基准货币的 VPS 订阅成本。</span>
            </p>
          ) : (
            <div className="subscription-donut-layout">
              <svg className="subscription-donut" viewBox="0 0 140 140" role="img" aria-label={`本月 VPS 成本占用，总计 ${money(donutTotal, baseCurrency)}`}>
                <circle className="subscription-donut__track" cx="70" cy="70" r="52" />
                {donutSegments.map(({ item, index, length, dashOffset }) => {
                  const activate = () => {
                    if (item.vpsID) onSelectVPS(item.vpsID)
                  }
                  return (
                    <circle
                      key={item.key}
                      className="subscription-donut__segment"
                      cx="70"
                      cy="70"
                      r="52"
                      stroke={DONUT_COLORS[index % DONUT_COLORS.length]}
                      strokeDasharray={`${length} ${Math.max(0, circumference - length)}`}
                      strokeDashoffset={dashOffset}
                      role="button"
                      tabIndex={item.vpsID ? 0 : -1}
                      aria-label={item.vpsID ? `筛选 ${item.label}，本月成本 ${money(item.cost, baseCurrency)}` : `其他 VPS 成本 ${money(item.cost, baseCurrency)}`}
                      onClick={activate}
                      onKeyDown={(event) => handleKeyActivate(event, activate)}
                    />
                  )
                })}
                <text x="70" y="62" className="subscription-donut__center-label">{baseCurrency}</text>
                <text x="70" y="78" className="subscription-donut__center-value">{compactAmount(donutTotal)}</text>
                <text x="70" y="92" className="subscription-donut__center-label">本月</text>
              </svg>
              <div className="subscription-donut-legend">
                {donutItems.map((item, index) => {
                  const percent = donutTotal > 0 ? (item.cost / donutTotal) * 100 : 0
                  const activate = () => {
                    if (item.vpsID) onSelectVPS(item.vpsID)
                  }
                  return (
                    <button
                      key={item.key}
                      type="button"
                      className="subscription-donut-legend__item"
                      disabled={item.isOther}
                      title={item.isOther ? '其他项只展示汇总，不应用模糊筛选' : undefined}
                      onClick={activate}
                    >
                      <span className="subscription-donut-legend__swatch" style={{ background: DONUT_COLORS[index % DONUT_COLORS.length] }} />
                      <span>
                        <strong>{item.label}</strong>
                        <small>{money(item.cost, baseCurrency)} · {percent.toFixed(1)}%</small>
                      </span>
                    </button>
                  )
                })}
              </div>
            </div>
          )}
        </div>

        <div className="page-panel subscription-insight-panel subscription-insight-panel--trend">
          <div className="section-heading section-heading--inline">
            <div>
              <p className="section-heading__eyebrow">Year</p>
              <h3 className="section-heading__title">年度趋势与风险</h3>
            </div>
            {statisticsLoading ? <span className="section-heading__meta">加载中</span> : null}
          </div>
          {statisticsError ? (
            <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{statisticsError}</p>
          ) : null}
          {!statisticsError && hasTrend ? (
            <MetricChart
              samples={trendSamples(costBuckets)}
              height={190}
              tone="accent"
              yMin={0}
              ariaLabel="最近一年订阅月成本趋势"
              formatValue={(value) => money(value, baseCurrency)}
              formatAxisValue={(value) => value >= 1000 ? `${(value / 1000).toFixed(1)}k` : value.toFixed(0)}
              formatTime={(observedAt) => monthLabel(observedAt.slice(0, 7))}
              formatTooltipTime={monthTooltipLabel}
              showTooltip
            />
          ) : !statisticsError ? (
            <p className="asset-table-empty-state">
              <strong>历史成本数据不足</strong>
              <span>{hasInsufficientTrendData ? '部分历史月份缺少可用汇率，暂不绘制可能误导的趋势曲线。' : '后端未返回足够的历史月成本 bucket，暂不绘制趋势曲线。'}</span>
            </p>
          ) : null}

          <div className="subscription-risk-grid">
            <div className="subscription-risk-card">
              <span><StatusGlyph state={overview?.budget_risk_count ? 'alert' : 'normal'} size="sm" />预算风险</span>
              <strong>{overview?.budget_risk_count ?? 0}</strong>
              <small>{overview?.budget_risks?.[0]?.name ? `${overview.budget_risks[0].name} · ${budgetStatusLabel(overview.budget_risks[0].status)}` : '无预算风险'}</small>
            </div>
            <div className="subscription-risk-card">
              <span><StatusGlyph state={(overview?.exchange_rate_stale_count ?? 0) > 0 ? 'notice' : 'normal'} size="sm" />汇率状态</span>
              <strong>{overview?.exchange_rate_stale_count ?? 0}</strong>
              <small>过期汇率项</small>
            </div>
            <div className="subscription-risk-card">
              <span><StatusGlyph state={(overview?.renewal_due_30d_count ?? 0) > 0 ? 'notice' : 'normal'} size="sm" />续费月份</span>
              <strong>{renewalBuckets.length}</strong>
              <small>{renewalBuckets[0] ? `${monthLabel(renewalBuckets[0].bucket)} · ${renewalBuckets[0].renewal_count} 项` : '无续费压力'}</small>
            </div>
          </div>

          {overview?.budget_risks?.length ? (
            <div className="subscription-budget-risk-list">
              {overview.budget_risks.slice(0, 3).map((budget) => (
                <div key={budget.budget_id} className="subscription-budget-risk-list__item">
                  <StatusGlyph state={budgetTone(budget.status)} size="sm" ariaLabel={budgetStatusLabel(budget.status)} />
                  <span>{budget.name}</span>
                  <strong>{money(budget.current_monthly_spend, budget.base_currency)}</strong>
                </div>
              ))}
            </div>
          ) : null}

          <div className="subscription-breakdown-panel">
            <div className="subscription-breakdown-panel__header">
              <div>
                <p className="section-heading__eyebrow">Composition</p>
                <h4>成本构成</h4>
              </div>
              <Tabs variant="pill" value={breakdownKind} onChange={onBreakdownKindChange} items={BREAKDOWN_TABS} />
            </div>
            <div className="subscription-breakdown-list">
              {currentBreakdown.length === 0 ? (
                <p className="asset-table-empty-state">
                  <strong>暂无构成数据</strong>
                  <span>当前统计窗口没有可展示的成本构成。</span>
                </p>
              ) : currentBreakdown.slice(0, 8).map((item) => (
                <div key={item.key} className="subscription-breakdown-row">
                  <div>
                    <strong>{item.label}</strong>
                    <small>{item.subscription_count} 项订阅</small>
                  </div>
                  <div className="subscription-breakdown-bar">
                    <span style={{ width: barWidth(item.monthly_cost, breakdownMax) }} />
                  </div>
                  <span className="mono">{money(item.monthly_cost, baseCurrency)}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}

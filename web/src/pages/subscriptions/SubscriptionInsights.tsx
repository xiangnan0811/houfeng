import { type KeyboardEvent, useState } from 'react'

import { StatusGlyph, TabPanel, Tabs } from '../../components/atoms'
import { formatDate, formatMoney } from '../../lib/format'
import type {
  SubscriptionBreakdownItem,
  SubscriptionCostRow,
  SubscriptionOverview,
  SubscriptionRenewalQueueItem,
  SubscriptionStatistics,
} from '../../lib/types'
import { BudgetCostTrendChart } from './BudgetCostTrendChart'

export type SubscriptionBreakdownKind = 'provider' | 'category' | 'currency' | 'payment' | 'region'

type MonthCostView = 'pie' | 'ranking'

type DonutItem = {
  key: string
  label: string
  cost: number
  originalPrice: string
  vpsID: string | null
  isOther: boolean
  share: number
}

export type SubscriptionInsightsProps = {
  overview: SubscriptionOverview | null
  overviewLoading: boolean
  overviewError: string | null
  statistics: SubscriptionStatistics | null
  statisticsLoading: boolean
  statisticsError: string | null
  onRetryStatistics: () => void
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
  { value: 'payment', label: '支付方式' },
  { value: 'region', label: '区域' },
] as const

const MONTH_COST_TABS = [
  { value: 'pie', label: '饼图' },
  { value: 'ranking', label: '排行' },
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

function buildMonthlyRows(rows: SubscriptionCostRow[]): SubscriptionCostRow[] {
  return rows
    .filter((row) => (row.monthly_price_base ?? 0) > 0)
    .sort((left, right) => (right.monthly_price_base ?? 0) - (left.monthly_price_base ?? 0))
}

function buildDonutItems(rows: SubscriptionCostRow[]): DonutItem[] {
  const sorted = buildMonthlyRows(rows)
  const total = sorted.reduce((sum, row) => sum + (row.monthly_price_base ?? 0), 0)
  const top = sorted.slice(0, 5).map((row) => {
    const cost = row.monthly_price_base ?? 0
    return {
      key: row.vps_id,
      label: row.display_name || row.vps_display_name || row.vps_id,
      cost,
      originalPrice: money(row.price, row.currency),
      vpsID: row.vps_id,
      isOther: false,
      share: total > 0 ? (cost / total) * 100 : 0,
    }
  })
  const other = sorted.slice(5)
  if (other.length === 0) return top
  const otherCost = other.reduce((sum, row) => sum + (row.monthly_price_base ?? 0), 0)
  return [
    ...top,
    {
      key: 'other',
      label: '其他',
      cost: otherCost,
      originalPrice: `${other.length} 项订阅`,
      vpsID: null,
      isOther: true,
      share: total > 0 ? (otherCost / total) * 100 : 0,
    },
  ]
}

function breakdownItems(statistics: SubscriptionStatistics | null, kind: SubscriptionBreakdownKind): SubscriptionBreakdownItem[] {
  if (!statistics) return []
  if (kind === 'category') return statistics.category_breakdown
  if (kind === 'currency') return statistics.currency_breakdown
  if (kind === 'payment') return statistics.payment_breakdown ?? []
  if (kind === 'region') return statistics.region_breakdown ?? []
  return statistics.provider_breakdown
}

function handleKeyActivate(event: KeyboardEvent, run: () => void) {
  if (event.key !== 'Enter' && event.key !== ' ') return
  event.preventDefault()
  run()
}

function RenewalQueue({
  items,
  baseCurrency,
  onSelectVPS,
}: {
  items: SubscriptionRenewalQueueItem[]
  baseCurrency: string
  onSelectVPS: (vpsID: string) => void
}) {
  if (items.length === 0) {
    return (
      <p className="asset-table-empty-state">
        <strong>暂无临近续费</strong>
        <span>未来 90 天没有需要处理的订阅续费。</span>
      </p>
    )
  }
  return (
    <div className="subscription-renewal-queue subscription-panel-scroll" role="region" tabIndex={0} aria-label="续费队列">
      {items.map((item) => {
        const isStale = item.exchange_rate_stale
        return (
          <button key={item.subscription_id} type="button" className={`subscription-renewal-row ${isStale ? 'subscription-renewal-row--stale' : ''}`} onClick={() => onSelectVPS(item.vps_id)}>
            <span>
              <strong><StatusGlyph state={isStale ? 'notice' : 'normal'} size="sm" />{item.display_name || item.vps_display_name}</strong>
              <small>{item.provider_name || '未记录服务商'} · {item.currency}</small>
            </span>
            <span className="mono">{formatDate(item.renew_at)}</span>
            <span className="mono">{money(item.monthly_price_base, item.base_currency || baseCurrency)}/月</span>
          </button>
        )
      })}
    </div>
  )
}

export function SubscriptionInsights({
  overview,
  overviewLoading,
  overviewError,
  statistics,
  statisticsLoading,
  statisticsError,
  onRetryStatistics,
  baseCurrency,
  breakdownKind,
  onBreakdownKindChange,
  onSelectVPS,
}: SubscriptionInsightsProps) {
  const [monthCostView, setMonthCostView] = useState<MonthCostView>('pie')
  const [activeDonutKey, setActiveDonutKey] = useState<string | null>(null)
  const overviewReady = !overviewLoading && overviewError == null
  const monthlyRows = buildMonthlyRows(overviewReady ? overview?.vps_costs ?? [] : [])
  const donutItems = buildDonutItems(overviewReady ? overview?.vps_costs ?? [] : [])
  const donutTotal = donutItems.reduce((sum, item) => sum + item.cost, 0)
  const activeDonutItem = donutItems.find((item) => item.key === activeDonutKey) ?? null
  const circumference = 2 * Math.PI * 52
  const donutSegments = donutItems.map((item, index) => {
    const priorCost = donutItems.slice(0, index).reduce((sum, prior) => sum + prior.cost, 0)
    const length = donutTotal > 0 ? (item.cost / donutTotal) * circumference : 0
    const dashOffset = donutTotal > 0 ? -((priorCost / donutTotal) * circumference) : 0
    return { item, index, length, dashOffset }
  })
  const costBuckets = statistics?.cost_month_buckets ?? []
  const hasInsufficientTrendData = costBuckets.some((bucket) => bucket.data_insufficient)
  const hasTrend = !hasInsufficientTrendData &&
    costBuckets.length >= 2 &&
    costBuckets.some((bucket) => bucket.monthly_cost > 0 || (bucket.budget_limit ?? 0) > 0)
  const currentBreakdown = breakdownItems(statistics, breakdownKind)
  const breakdownMax = Math.max(...currentBreakdown.map((item) => item.monthly_cost), 0)
  const rankingMax = Math.max(...monthlyRows.map((row) => row.monthly_price_base ?? 0), 0)

  return (
    <section className="subscription-insights" aria-label="订阅成本洞察">
      <div className="subscription-insights__grid">
        {/* 1. Primary full-width trend panel first */}
        <div className="subscription-insight-panel subscription-insight-panel--trend">
          <div className="subscription-panel-header">
            <h3 className="subscription-panel-title">月成本与月预算</h3>
            <span className="subscription-panel-meta">
              {statisticsLoading
                ? '加载中'
                : `全量订阅 · 最近 ${costBuckets.length} 个月`}
            </span>
          </div>
          {statisticsError ? (
            <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
              {statisticsError}{' '}
              <button type="button" className="btn sm secondary" onClick={onRetryStatistics}>重试统计</button>
            </p>
          ) : null}
          {statisticsError ? null : statisticsLoading && !statistics ? (
            <p className="asset-table-empty-state" role="status">
              <strong>正在加载年度统计</strong>
            </p>
          ) : hasTrend ? (
            <BudgetCostTrendChart
              buckets={costBuckets}
              baseCurrency={baseCurrency}
            />
          ) : statisticsLoading ? null : (
            <p className="asset-table-empty-state">
              <strong>历史成本数据不足</strong>
              <span>{hasInsufficientTrendData ? '部分历史月份缺少可用汇率或预算币种不一致，暂不绘制可能误导的趋势曲线。' : '后端未返回足够的历史月成本与月预算 bucket。'}</span>
            </p>
          )}
        </div>

        {/* 2. Secondary monthly cost (pie / ranking) */}
        <div className="subscription-insight-panel subscription-insight-panel--month">
          <div className="subscription-panel-header">
            <h3 className="subscription-panel-title">月成本</h3>
            <Tabs
              label="月成本展示"
              idBase="subscription-month-cost"
              variant="pill"
              value={monthCostView}
              onChange={setMonthCostView}
              items={MONTH_COST_TABS}
            />
          </div>
          <TabPanel
            idBase="subscription-month-cost"
            value={monthCostView}
            className="subscription-insight-panel__tab-panel"
          >
            <span className="subscription-panel-total">{overviewReady ? money(donutTotal, baseCurrency) : '—'}</span>
            {overviewLoading ? (
              <p className="asset-table-empty-state" role="status">
                <strong>正在加载月成本</strong>
              </p>
            ) : overviewError ? (
              <p className="asset-table-empty-state">
                <strong>月成本不可用</strong>
                <span>{overviewError}</span>
              </p>
            ) : monthlyRows.length === 0 ? (
              <p className="asset-table-empty-state">
                <strong>暂无可展示成本</strong>
                <span>当前没有可换算为基准货币的 VPS 订阅成本。</span>
              </p>
            ) : monthCostView === 'pie' ? (
              <div className="subscription-donut-layout subscription-panel-scroll" role="region" tabIndex={0} aria-label="月成本饼图">
                <svg className="subscription-donut" viewBox="0 0 140 140" role="img" aria-label={`本月 VPS 成本占用，总计 ${money(donutTotal, baseCurrency)}`}>
                  <circle className="subscription-donut__track" cx="70" cy="70" r="52" />
                  {donutSegments.map(({ item, index, length, dashOffset }) => {
                    const activate = () => {
                      setActiveDonutKey(item.key)
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
                        tabIndex={0}
                        aria-label={item.vpsID ? `筛选 ${item.label}，本月成本 ${money(item.cost, baseCurrency)}，占比 ${item.share.toFixed(1)}%` : `其他 VPS 成本 ${money(item.cost, baseCurrency)}，不应用模糊筛选`}
                        onMouseEnter={() => setActiveDonutKey(item.key)}
                        onMouseLeave={() => setActiveDonutKey(null)}
                        onFocus={() => setActiveDonutKey(item.key)}
                        onBlur={() => setActiveDonutKey(null)}
                        onClick={activate}
                        onKeyDown={(event) => handleKeyActivate(event, activate)}
                      />
                    )
                  })}
                  <text x="70" y="62" className="subscription-donut__center-label">{baseCurrency}</text>
                  <text x="70" y="78" className="subscription-donut__center-value">{compactAmount(donutTotal)}</text>
                  <text x="70" y="92" className="subscription-donut__center-label">本月</text>
                </svg>
                <ul className="subscription-donut-legend">
                  {donutItems.map((item, index) => (
                    <li key={item.key} data-tone={String(index % DONUT_COLORS.length)}>
                      <i aria-hidden="true" />
                      <span>{item.label}</span>
                      <small>{item.share.toFixed(1)}%</small>
                    </li>
                  ))}
                </ul>
                {activeDonutItem ? (
                  <div className="subscription-donut-popover" role="status" aria-live="polite">
                    <strong>{activeDonutItem.label}</strong>
                    <span>原始付费：{activeDonutItem.originalPrice}</span>
                    <span>基准月成本：{money(activeDonutItem.cost, baseCurrency)}</span>
                    <span>月付费占比：{activeDonutItem.share.toFixed(1)}%</span>
                    {activeDonutItem.isOther ? <small>其他项仅展示汇总，不应用筛选。</small> : null}
                  </div>
                ) : null}
              </div>
            ) : (
              <div className="subscription-ranking-list subscription-panel-scroll" role="region" tabIndex={0} aria-label="月成本排行">
                {monthlyRows.map((row) => {
                  const cost = row.monthly_price_base ?? 0
                  const share = donutTotal > 0 ? (cost / donutTotal) * 100 : 0
                  return (
                    <button key={row.subscription_id} type="button" className="subscription-ranking-row" onClick={() => onSelectVPS(row.vps_id)}>
                      <div>
                        <strong>{row.display_name || row.vps_display_name || row.vps_id}</strong>
                        <small>{money(row.price, row.currency)} · {share.toFixed(1)}%</small>
                      </div>
                      <div className="subscription-breakdown-bar">
                        <progress
                          aria-label={`${row.display_name || row.vps_display_name || row.vps_id} 月成本`}
                          max={rankingMax || 1}
                          value={cost}
                        />
                      </div>
                      <span className="mono">{money(cost, baseCurrency)}</span>
                    </button>
                  )
                })}
              </div>
            )}
          </TabPanel>
        </div>

        {/* 3. Secondary cost composition */}
        <div className="subscription-insight-panel subscription-insight-panel--composition">
          <div className="subscription-panel-header">
            <h3 className="subscription-panel-title">成本构成</h3>
            <label className="subscription-panel-select">
              <span>构成维度</span>
              <select
                value={breakdownKind}
                onChange={(event) => onBreakdownKindChange(event.target.value as SubscriptionBreakdownKind)}
              >
                {BREAKDOWN_TABS.map((item) => (
                  <option key={item.value} value={item.value}>{item.label}</option>
                ))}
              </select>
            </label>
          </div>
          <div className="subscription-breakdown-list subscription-panel-scroll" role="region" tabIndex={0} aria-label="成本构成">
            {statisticsError ? (
              <p className="asset-table-empty-state">
                <strong>构成数据不可用</strong>
                <span>年度统计未加载，成本构成暂不展示。</span>
              </p>
            ) : statisticsLoading && currentBreakdown.length === 0 ? (
              <p className="asset-table-empty-state" role="status">
                <strong>正在加载构成数据</strong>
              </p>
            ) : currentBreakdown.length === 0 ? (
              <p className="asset-table-empty-state">
                <strong>暂无构成数据</strong>
                <span>当前统计窗口没有可展示的成本构成。</span>
              </p>
            ) : currentBreakdown.map((item) => (
              <div key={item.key} className="subscription-breakdown-row">
                <div>
                  <strong>{item.label}</strong>
                  <small>{item.subscription_count} 项订阅</small>
                </div>
                <div className="subscription-breakdown-bar">
                  <progress
                    aria-label={`${item.label} 月成本`}
                    max={breakdownMax || 1}
                    value={item.monthly_cost}
                  />
                </div>
                <span className="mono">{money(item.monthly_cost, baseCurrency)}</span>
              </div>
            ))}
          </div>
        </div>

        {/* 4. Full-width renewal queue */}
        <div className="subscription-insight-panel subscription-insight-panel--renewal">
          <div className="subscription-panel-header">
            <h3 className="subscription-panel-title">续费队列</h3>
            <span className="subscription-panel-meta">{overviewReady ? `${overview?.upcoming_renewals?.length ?? 0} 项` : (overviewLoading ? '加载中' : '不可用')}</span>
          </div>
          {overviewLoading ? (
            <p className="asset-table-empty-state" role="status">
              <strong>正在加载续费队列</strong>
            </p>
          ) : overviewError ? (
            <p className="asset-table-empty-state">
              <strong>续费队列不可用</strong>
              <span>{overviewError}</span>
            </p>
          ) : (
            <RenewalQueue items={overview?.upcoming_renewals ?? []} baseCurrency={baseCurrency} onSelectVPS={onSelectVPS} />
          )}
        </div>
      </div>
    </section>
  )
}

import { type KeyboardEvent, useState } from 'react'

import { StatusGlyph, Tabs } from '../../components/atoms'
import { formatDate, formatMoney } from '../../lib/format'
import type {
  SubscriptionBreakdownItem,
  SubscriptionCostRow,
  SubscriptionOverview,
  SubscriptionRenewalQueueItem,
  SubscriptionSeriesPoint,
  SubscriptionStatistics,
} from '../../lib/types'

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

type TrendPoint = {
  bucket: string
  x: number
  cost: number
  budget: number | null
  yCost: number
  yBudget: number | null
}

type DifferenceArea = {
  tone: 'over' | 'under'
  points: string
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
  { value: 'payment', label: '支付方式' },
  { value: 'region', label: '国家/地区' },
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

function barWidth(value: number, max: number): string {
  if (!Number.isFinite(value) || !Number.isFinite(max) || max <= 0) return '0%'
  return `${Math.max(4, Math.min(100, (value / max) * 100))}%`
}

function monthLabel(bucket: string): string {
  const [year, month] = bucket.split('-')
  if (!year || !month) return bucket
  return `${year.slice(2)}/${month}`
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

function pathFrom(points: Array<{ x: number; y: number | null }>): string {
  const valid = points.filter((point): point is { x: number; y: number } => point.y != null)
  if (valid.length === 0) return ''
  return valid.map((point, index) => `${index === 0 ? 'M' : 'L'} ${point.x.toFixed(2)} ${point.y.toFixed(2)}`).join(' ')
}

function pointOnSegment(left: TrendPoint, right: TrendPoint, t: number): { x: number; y: number } {
  return {
    x: left.x + (right.x - left.x) * t,
    y: left.yCost + (right.yCost - left.yCost) * t,
  }
}

function polygon(points: Array<{ x: number; y: number }>): string {
  return points.map((point) => `${point.x.toFixed(2)},${point.y.toFixed(2)}`).join(' ')
}

function buildDifferenceAreas(points: TrendPoint[]): DifferenceArea[] {
  const areas: DifferenceArea[] = []
  for (let index = 0; index < points.length - 1; index += 1) {
    const left = points[index]
    const right = points[index + 1]
    if (left.budget == null || right.budget == null || left.yBudget == null || right.yBudget == null) continue
    const leftDelta = left.cost - left.budget
    const rightDelta = right.cost - right.budget
    if (leftDelta === 0 && rightDelta === 0) continue
    if (leftDelta === 0 || rightDelta === 0 || Math.sign(leftDelta) === Math.sign(rightDelta)) {
      const tone: DifferenceArea['tone'] = (leftDelta || rightDelta) > 0 ? 'over' : 'under'
      areas.push({
        tone,
        points: polygon([
          { x: left.x, y: left.yCost },
          { x: right.x, y: right.yCost },
          { x: right.x, y: right.yBudget },
          { x: left.x, y: left.yBudget },
        ]),
      })
      continue
    }
    const t = Math.abs(leftDelta) / (Math.abs(leftDelta) + Math.abs(rightDelta))
    const intersection = pointOnSegment(left, right, t)
    areas.push({
      tone: leftDelta > 0 ? 'over' : 'under',
      points: polygon([
        { x: left.x, y: left.yCost },
        intersection,
        { x: left.x, y: left.yBudget },
      ]),
    })
    areas.push({
      tone: rightDelta > 0 ? 'over' : 'under',
      points: polygon([
        intersection,
        { x: right.x, y: right.yCost },
        { x: right.x, y: right.yBudget },
      ]),
    })
  }
  return areas
}

function BudgetCostTrendChart({
  buckets,
  baseCurrency,
}: {
  buckets: SubscriptionSeriesPoint[]
  baseCurrency: string
}) {
  const width = 720
  const height = 220
  const pad = { left: 38, right: 14, top: 16, bottom: 28 }
  const values = buckets.flatMap((bucket) => [
    bucket.monthly_cost,
    bucket.budget_limit ?? 0,
  ])
  const yMax = Math.max(...values, 1)
  const chartWidth = width - pad.left - pad.right
  const chartHeight = height - pad.top - pad.bottom
  const xFor = (index: number) => pad.left + (buckets.length <= 1 ? chartWidth / 2 : (index / (buckets.length - 1)) * chartWidth)
  const yFor = (value: number) => pad.top + chartHeight - (Math.max(0, value) / yMax) * chartHeight
  const points: TrendPoint[] = buckets.map((bucket, index) => ({
    bucket: bucket.bucket,
    x: xFor(index),
    cost: bucket.monthly_cost,
    budget: bucket.budget_limit ?? null,
    yCost: yFor(bucket.monthly_cost),
    yBudget: bucket.budget_limit == null ? null : yFor(bucket.budget_limit),
  }))
  const costPath = pathFrom(points.map((point) => ({ x: point.x, y: point.yCost })))
  const budgetPath = pathFrom(points.map((point) => ({ x: point.x, y: point.yBudget })))
  const areas = buildDifferenceAreas(points)
  const ticks = [0, yMax / 2, yMax]

  return (
    <svg className="subscription-trend-chart" viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`最近一年${baseCurrency}月成本与月预算趋势`}>
      {ticks.map((tick) => {
        const y = yFor(tick)
        return (
          <g key={tick}>
            <line className="subscription-trend-chart__grid" x1={pad.left} y1={y} x2={width - pad.right} y2={y} />
            <text className="subscription-trend-chart__axis" x={pad.left - 8} y={y + 3} textAnchor="end">{compactAmount(tick)}</text>
          </g>
        )
      })}
      {areas.map((area, index) => (
        <polygon key={`${area.tone}-${index}`} className={`subscription-trend-chart__area subscription-trend-chart__area--${area.tone}`} points={area.points} />
      ))}
      <path className="subscription-trend-chart__line subscription-trend-chart__line--cost" d={costPath} />
      {budgetPath ? <path className="subscription-trend-chart__line subscription-trend-chart__line--budget" d={budgetPath} /> : null}
      {points.map((point) => (
        <g key={point.bucket}>
          <circle className="subscription-trend-chart__point subscription-trend-chart__point--cost" cx={point.x} cy={point.yCost} r="3.5" />
          {point.yBudget == null ? null : <circle className="subscription-trend-chart__point subscription-trend-chart__point--budget" cx={point.x} cy={point.yBudget} r="3.5" />}
        </g>
      ))}
      {points.map((point, index) => (
        index % Math.max(1, Math.ceil(points.length / 5)) === 0 || index === points.length - 1 ? (
          <text key={point.bucket} className="subscription-trend-chart__axis" x={point.x} y={height - 8} textAnchor="middle">{monthLabel(point.bucket)}</text>
        ) : null
      ))}
    </svg>
  )
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
    <div className="subscription-renewal-queue subscription-panel-scroll">
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
  statistics,
  statisticsLoading,
  statisticsError,
  baseCurrency,
  breakdownKind,
  onBreakdownKindChange,
  onSelectVPS,
}: SubscriptionInsightsProps) {
  const [monthCostView, setMonthCostView] = useState<MonthCostView>('pie')
  const [activeDonutKey, setActiveDonutKey] = useState<string | null>(null)
  const monthlyRows = buildMonthlyRows(overview?.vps_costs ?? [])
  const donutItems = buildDonutItems(overview?.vps_costs ?? [])
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
    <section className="subscription-insights animate-in" aria-label="订阅成本洞察">
      <div className="section-heading">
        <p className="section-heading__eyebrow">Cost Insights</p>
        <h2 className="section-heading__title">成本洞察</h2>
      </div>
      <div className="subscription-insights__grid">
        <div className="page-panel subscription-insight-panel subscription-insight-panel--month">
          <div className="subscription-panel-header">
            <div>
              <p className="section-heading__eyebrow">This Month</p>
              <h3 className="section-heading__title">月成本</h3>
            </div>
            <Tabs variant="pill" value={monthCostView} onChange={setMonthCostView} items={MONTH_COST_TABS} />
          </div>
          <span className="subscription-panel-total">{money(donutTotal, baseCurrency)}</span>
          {monthlyRows.length === 0 ? (
            <p className="asset-table-empty-state">
              <strong>暂无可展示成本</strong>
              <span>当前没有可换算为基准货币的 VPS 订阅成本。</span>
            </p>
          ) : monthCostView === 'pie' ? (
            <div className="subscription-donut-layout subscription-donut-layout--compact">
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
            <div className="subscription-ranking-list subscription-panel-scroll">
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
                      <span style={{ width: barWidth(cost, rankingMax) }} />
                    </div>
                    <span className="mono">{money(cost, baseCurrency)}</span>
                  </button>
                )
              })}
            </div>
          )}
        </div>

        <div className="page-panel subscription-insight-panel subscription-insight-panel--trend">
          <div className="subscription-panel-header">
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
            <>
              <BudgetCostTrendChart buckets={costBuckets} baseCurrency={baseCurrency} />
              <div className="subscription-trend-legend" aria-label="趋势图图例">
                <span><i className="subscription-trend-legend__line subscription-trend-legend__line--cost" />月成本</span>
                <span><i className="subscription-trend-legend__line subscription-trend-legend__line--budget" />月预算</span>
                <span><i className="subscription-trend-legend__area subscription-trend-legend__area--over" />超预算区间</span>
                <span><i className="subscription-trend-legend__area subscription-trend-legend__area--under" />低于预算区间</span>
              </div>
            </>
          ) : !statisticsError ? (
            <p className="asset-table-empty-state">
              <strong>历史成本数据不足</strong>
              <span>{hasInsufficientTrendData ? '部分历史月份缺少可用汇率或预算币种不一致，暂不绘制可能误导的趋势曲线。' : '后端未返回足够的历史月成本与月预算 bucket。'}</span>
            </p>
          ) : null}
        </div>

        <div className="page-panel subscription-insight-panel subscription-insight-panel--composition">
          <div className="subscription-panel-header">
            <div>
              <p className="section-heading__eyebrow">Composition</p>
              <h3 className="section-heading__title">成本构成</h3>
            </div>
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
          <div className="subscription-breakdown-list subscription-panel-scroll">
            {currentBreakdown.length === 0 ? (
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
                  <span style={{ width: barWidth(item.monthly_cost, breakdownMax) }} />
                </div>
                <span className="mono">{money(item.monthly_cost, baseCurrency)}</span>
              </div>
            ))}
          </div>
        </div>

        <div className="page-panel subscription-insight-panel subscription-insight-panel--renewal">
          <div className="subscription-panel-header">
            <div>
              <p className="section-heading__eyebrow">Renewal Queue</p>
              <h3 className="section-heading__title">续费队列</h3>
            </div>
            <span className="section-heading__meta">{overview?.upcoming_renewals?.length ?? 0} 项</span>
          </div>
          <RenewalQueue items={overview?.upcoming_renewals ?? []} baseCurrency={baseCurrency} onSelectVPS={onSelectVPS} />
        </div>
      </div>
    </section>
  )
}

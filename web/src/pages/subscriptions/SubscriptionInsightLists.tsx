import { Link } from 'react-router-dom'

import { Badge, type BadgeTone } from '../../components/atoms'
import { formatDate } from '../../lib/format'
import type { SubscriptionOverview, SubscriptionRenewalQueueItem } from '../../lib/types'
import { daysUntilDate, renewalLabel, renewalTimingLabel, renewalUrgency } from '../assetPageUtils'
import { money } from './insightFormat'

const DECISION_TONES: Readonly<Record<string, BadgeTone>> = {
  keep: 'normal',
  unreviewed: 'notice',
  cancel: 'alert',
}

/** 面板内的空 / 加载 / 不可用状态：单行，不撑高面板。 */
export function InsightEmpty({ title, detail, busy = false }: { title: string; detail?: string; busy?: boolean }) {
  return (
    <p className="subscription-insight-empty" role={busy ? 'status' : undefined}>
      <strong>{title}</strong>
      {detail ? <span>{detail}</span> : null}
    </p>
  )
}

function decisionTone(value: string): BadgeTone {
  return Object.hasOwn(DECISION_TONES, value) ? DECISION_TONES[value]! : 'neutral'
}

export function RenewalQueue({
  items,
  baseCurrency,
  onSelectVPS,
}: {
  items: SubscriptionRenewalQueueItem[]
  baseCurrency: string
  onSelectVPS: (vpsID: string) => void
}) {
  if (items.length === 0) {
    return <InsightEmpty title="暂无临近续费" detail="未来 90 天没有需要处理的订阅续费。" />
  }
  return (
    <div className="subscription-renewal-queue subscription-panel-scroll" role="region" tabIndex={0} aria-label="续费队列">
      {items.map((item) => {
        const days = daysUntilDate(item.renew_at)
        const title = item.display_name || item.vps_display_name || item.vps_id
        const meta = [
          item.vps_display_name && item.vps_display_name !== title ? item.vps_display_name : '',
          item.provider_name || '未记录服务商',
        ].filter(Boolean).join(' · ')
        return (
          <button
            key={item.subscription_id}
            type="button"
            className="subscription-renewal-row"
            data-urgency={renewalUrgency(days)}
            onClick={() => onSelectVPS(item.vps_id)}
          >
            <span className="subscription-renewal-row__when">
              <time className="mono tnum" dateTime={item.renew_at ?? undefined}>{formatDate(item.renew_at)}</time>
              <small>{renewalTimingLabel(days)}</small>
            </span>
            <span className="subscription-renewal-row__name">
              <strong>{title}</strong>
              <small>{meta}</small>
            </span>
            <Badge variant="state" tone={decisionTone(item.renewal_decision)}>{renewalLabel(item.renewal_decision)}</Badge>
            <span className="subscription-renewal-row__amount mono tnum">
              {item.exchange_rate_stale ? <small>汇率过期</small> : null}
              {money(item.monthly_price_base, item.base_currency || baseCurrency)}/月
            </span>
          </button>
        )
      })}
    </div>
  )
}

export function ArchivedCharges({ overview, baseCurrency }: { overview: SubscriptionOverview; baseCurrency: string }) {
  const unknownCount = overview.archived_unknown_amount_count ?? 0
  const costs = overview.archived_potential_costs ?? []
  const missing = overview.archived_missing_subscription_assets ?? []
  const total = unknownCount > 0 ? '金额待核对' : `${money(overview.archived_potential_monthly_cost, baseCurrency)}/月`
  return (
    <section className="subscription-insight-panel subscription-insight-panel--archived" aria-label="已归档资产潜在扣费">
      <div className="subscription-panel-header">
        <h3 className="subscription-panel-title">已归档资产潜在扣费</h3>
        <span className="subscription-panel-meta">不计入当前成本 · <span className="mono tnum">{total}</span></span>
      </div>
      {unknownCount > 0 ? <p className="subscription-insight-note" role="status">{unknownCount} 项金额未知，未按零费用处理。</p> : null}
      {costs.length === 0 && missing.length === 0 ? (
        <InsightEmpty title="暂无待核对的归档扣费" />
      ) : (
        <ul className="subscription-archived-list">
          {costs.map((row) => (
            <li className="subscription-archived-row" key={row.subscription_id}>
              <strong>{row.vps_display_name || row.vps_id}</strong>
              <small>{row.auto_renew_check === 'enabled' ? '服务商自动续费已开启' : '服务商自动续费待核对'}</small>
              <span className="subscription-archived-row__amount mono tnum">
                {row.monthly_price_base == null ? '金额待核对' : `${money(row.monthly_price_base, baseCurrency)}/月`}
              </span>
              <Link className="text-link" to={`/vps/${encodeURIComponent(row.vps_id)}`}>核对扣费</Link>
            </li>
          ))}
          {missing.map((asset) => (
            <li className="subscription-archived-row" key={asset.vps_id}>
              <strong>{asset.display_name || asset.vps_id}</strong>
              <small>缺少账单 · 金额待核对</small>
              <span className="subscription-archived-row__amount mono">—</span>
              <Link className="text-link" to={`/vps/${encodeURIComponent(asset.vps_id)}`}>核对扣费</Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

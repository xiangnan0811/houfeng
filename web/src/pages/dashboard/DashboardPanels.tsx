import { Link } from 'react-router-dom'

import { StatusGlyph, Timestamp } from '../../components/atoms'
import { formatMoney } from '../../lib/format'
import { DASHBOARD_LINKS } from './dashboardLinks'
import {
  RENEWAL_QUEUE_WINDOW_DAYS,
  type DashboardActivityItem,
  type DashboardRenewalItem,
  type DashboardRenewalPanel,
} from './dashboardPanels'

const RENEWAL_SOON_DAYS = 7

function renewalDaysLabel(daysLeft: number): string {
  if (daysLeft < 0) return `逾期 ${Math.abs(daysLeft)} 天`
  if (daysLeft === 0) return '今天'
  return `${daysLeft} 天`
}

function renewalPriceLabel(item: DashboardRenewalItem): string {
  if (item.monthlyPrice == null || item.rateStatus === 'missing') return '金额待核对'
  const amount = `${formatMoney(item.monthlyPrice, item.currency)}/月`
  if (item.rateStatus === 'stale') return `汇率过期 · ${amount}`
  return amount
}

function PanelHeader({ id, title, meta, metaTitle, link }: { id: string; title: string; meta?: string; metaTitle?: string; link: { to: string; label: string } }) {
  return (
    <div className="dashboard-panel__header">
      <h2 id={id}>{title}</h2>
      {meta ? <span className="dashboard-panel__meta" {...(metaTitle ? { title: metaTitle } : {})}>{meta}</span> : null}
      <Link className="text-link text-link--action dashboard-panel__link" to={link.to}>{link.label}</Link>
    </div>
  )
}

export function DashboardRenewalsPanel({ panel }: { panel: DashboardRenewalPanel }) {
  return (
    <section className="dashboard-panel" aria-labelledby="dashboard-renewals-title">
      <PanelHeader
        id="dashboard-renewals-title"
        title="即将续费"
        {...(panel.status === 'ready'
          ? {
            meta: `未来 ${RENEWAL_QUEUE_WINDOW_DAYS} 天 · ${panel.capped ? `至少 ${panel.count}` : panel.count} 项${panel.estimated ? ' · 天数按接收时间估算' : ''}`,
            // 窗口与天数按 UTC 日计算；可见文字不再挂“（UTC）”，悬停说明。
            metaTitle: '按 UTC 日计算',
          }
          : {})}
        link={{ to: `${DASHBOARD_LINKS.subscriptions}?view=details`, label: '订阅明细' }}
      />
      {panel.status === 'loading' ? (
        <p className="dashboard-panel__empty" role="status">正在读取订阅摘要…</p>
      ) : panel.status === 'unavailable' ? (
        <p className="dashboard-panel__empty">续费队列暂不可用：{panel.error}</p>
      ) : (
        <>
          {panel.overdueCount > 0 ? (
            <div className="dashboard-renewals__overdue">
              <p className="dashboard-renewals__overdue-title">
                <span>已逾期 {panel.overdueCount} 项</span>
                <Link className="text-link" to={DASHBOARD_LINKS.subscriptions}>续费队列</Link>
              </p>
              <RenewalList items={panel.overdue} label="已逾期的订阅续费" />
            </div>
          ) : null}
          {panel.items.length === 0 ? (
            <p className="dashboard-panel__empty">未来 {RENEWAL_QUEUE_WINDOW_DAYS} 天内没有待续费的订阅。</p>
          ) : (
            <RenewalList items={panel.items} label="即将续费的订阅" />
          )}
        </>
      )}
    </section>
  )
}

function RenewalList({ items, label }: { items: DashboardRenewalItem[]; label: string }) {
  return (
    <ul className="dashboard-renewals" aria-label={label}>
      {items.map((item) => {
        const overdue = item.daysLeft < 0
        const soon = !overdue && item.daysLeft <= RENEWAL_SOON_DAYS
        const daysClass = overdue ? ' dashboard-renewal__days--overdue' : soon ? ' dashboard-renewal__days--soon' : ''
        return (
          <li key={item.key}>
            <Link className="dashboard-renewal" to={item.to}>
              <span className="dashboard-renewal__name">
                <strong>{item.name}</strong>
                <small>{item.provider || '未记录服务商'}</small>
              </span>
              <span className={`dashboard-renewal__days${daysClass}`}>
                {renewalDaysLabel(item.daysLeft)}
              </span>
              <span className="dashboard-renewal__meta mono">
                {item.renewDate} · {renewalPriceLabel(item)}
              </span>
            </Link>
          </li>
        )
      })}
    </ul>
  )
}

export function DashboardActivityPanel({ items }: { items: DashboardActivityItem[] }) {
  return (
    <section className="dashboard-panel" aria-labelledby="dashboard-activity-title">
      <PanelHeader id="dashboard-activity-title" title="最近动态" link={{ to: DASHBOARD_LINKS.events, label: '事件流' }} />
      {items.length === 0 ? (
        <p className="dashboard-panel__empty">摘要中没有最近的状态变化。</p>
      ) : (
        <ol className="dashboard-activity" aria-label="最近状态变化">
          {items.map((item) => (
            <li key={item.key} className={`dashboard-activity__item dashboard-activity__item--${item.tone}`}>
              <span className="dashboard-activity__glyph" aria-hidden="true">
                <StatusGlyph state={item.tone} size="sm" />
              </span>
              <span className="dashboard-activity__copy">
                <strong>{item.label}</strong>
                <span>{item.summary}</span>
              </span>
              <Timestamp value={item.createdAt} mode="relative" className="dashboard-activity__time" />
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}

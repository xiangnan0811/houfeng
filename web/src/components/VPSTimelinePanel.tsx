import type { ReactNode } from 'react'

import { Badge, MonoDigits, Timestamp } from './atoms'
import {
  SUBSCRIPTION_STATUS_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  type SubscriptionStatus,
  type VPSRenewalDecision,
  type VPSTimeline,
} from '../lib/types'
import { formatDate, formatMoney, formatOptional } from '../lib/format'

type VPSTimelinePanelProps = {
  timeline: VPSTimeline
}

type TimelineMetaItem = {
  label: string
  value: ReactNode
}

function renewalLabel(value?: VPSRenewalDecision | string | null): string {
  if (!value) return '首次记录'
  return VPS_RENEWAL_DECISION_LABELS[value as VPSRenewalDecision] ?? value
}

function subscriptionStatusLabel(value: SubscriptionStatus | string): string {
  return SUBSCRIPTION_STATUS_LABELS[value as SubscriptionStatus] ?? value
}

function booleanLabel(value: boolean): string {
  return value ? '是' : '否'
}

function changedValue(
  from: string | number | null | undefined,
  to: string | number | null | undefined,
): string {
  return `${formatOptional(from)} -> ${formatOptional(to)}`
}

function TimelineEmpty({ children }: { children: string }) {
  return <p className="asset-timeline-empty">{children}</p>
}

function TimelineCard({
  title,
  subtitle,
  time,
  tone = 'neutral',
  meta,
}: {
  title: string
  subtitle: string
  time: string
  tone?: 'normal' | 'notice' | 'maintenance' | 'critical' | 'neutral'
  meta: TimelineMetaItem[]
}) {
  return (
    <article className="asset-timeline-card">
      <div className="asset-timeline-card__rail" aria-hidden>
        <span className={`asset-timeline-card__dot asset-timeline-card__dot--${tone}`} />
      </div>
      <div className="asset-timeline-card__body">
        <header className="asset-timeline-card__header">
          <div>
            <h3>{title}</h3>
            <p>{subtitle}</p>
          </div>
          <Timestamp value={time} mode="absolute" />
        </header>
        <dl className="asset-timeline-card__meta">
          {meta.map((item) => (
            <div key={item.label}>
              <dt>{item.label}</dt>
              <dd>{item.value}</dd>
            </div>
          ))}
        </dl>
      </div>
    </article>
  )
}

function timelineCount(timeline: VPSTimeline): number {
  return (
    timeline.renewal_decisions.length +
    timeline.price_histories.length +
    timeline.ip_histories.length +
    timeline.spec_snapshots.length
  )
}

type TimelineTone = 'normal' | 'notice' | 'maintenance' | 'critical' | 'neutral'

function decisionTone(value: VPSRenewalDecision): TimelineTone {
  if (value === 'keep') return 'normal'
  if (value === 'unreviewed' || value === 'observe') return 'notice'
  if (value === 'migrate') return 'maintenance'
  if (value === 'cancel' || value === 'auto_renew_cancelled' || value === 'replaced') return 'critical'
  return 'neutral'
}

export function VPSTimelinePanel({ timeline }: VPSTimelinePanelProps) {
  return (
    <section className="page-panel asset-timeline">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">TIMELINE</p>
          <h2>资产历史</h2>
        </div>
        <Badge variant="count" tone="neutral">
          <MonoDigits>{timelineCount(timeline)}</MonoDigits> 条记录
        </Badge>
      </div>

      <div className="asset-timeline-grid">
        <section className="asset-timeline-group" aria-label="续费决策历史">
          <header className="asset-timeline-group__header">
            <h3>续费决策</h3>
            <span><MonoDigits>{timeline.renewal_decisions.length}</MonoDigits></span>
          </header>
          {timeline.renewal_decisions.length === 0 ? (
            <TimelineEmpty>暂无续费决策历史</TimelineEmpty>
          ) : (
            <div className="asset-timeline-list">
              {timeline.renewal_decisions.map((decision) => (
                <TimelineCard
                  key={decision.decision_id}
                  tone={decisionTone(decision.to_decision)}
                  title={`${renewalLabel(decision.from_decision)} -> ${renewalLabel(
                    decision.to_decision,
                  )}`}
                  subtitle={decision.reason || '未记录原因'}
                  time={decision.decided_at}
                  meta={[
                    { label: 'Decision ID', value: decision.decision_id },
                    { label: 'VPS ID', value: decision.vps_id },
                    {
                      label: '创建时间',
                      value: <Timestamp value={decision.created_at} mode="absolute" />,
                    },
                  ]}
                />
              ))}
            </div>
          )}
        </section>

        <section className="asset-timeline-group" aria-label="价格历史">
          <header className="asset-timeline-group__header">
            <h3>价格变化</h3>
            <span><MonoDigits>{timeline.price_histories.length}</MonoDigits></span>
          </header>
          {timeline.price_histories.length === 0 ? (
            <TimelineEmpty>暂无价格变化历史</TimelineEmpty>
          ) : (
            <div className="asset-timeline-list">
              {timeline.price_histories.map((history) => (
                <TimelineCard
                  key={history.price_history_id}
                  tone="notice"
                  title={`${formatMoney(history.from_price, history.from_currency)} -> ${formatMoney(
                    history.to_price,
                    history.to_currency,
                  )}`}
                  subtitle={`订阅 ${history.subscription_id}`}
                  time={history.changed_at}
                  meta={[
                    {
                      label: '月付折算',
                      value: `${formatMoney(
                        history.from_monthly_price,
                        history.from_currency,
                      )} -> ${formatMoney(history.to_monthly_price, history.to_currency)}`,
                    },
                    {
                      label: '计费周期',
                      value: changedValue(
                        history.from_billing_cycle,
                        history.to_billing_cycle,
                      ),
                    },
                    {
                      label: '计费月数',
                      value: changedValue(
                        history.from_billing_months,
                        history.to_billing_months,
                      ),
                    },
                    {
                      label: '续费日',
                      value: `${formatDate(history.from_renew_at)} -> ${formatDate(
                        history.to_renew_at,
                      )}`,
                    },
                    {
                      label: '自动续费',
                      value: `${booleanLabel(history.from_auto_renew)} -> ${booleanLabel(
                        history.to_auto_renew,
                      )}`,
                    },
                    {
                      label: '自动续费取消',
                      value: `${booleanLabel(
                        history.from_auto_renew_cancelled,
                      )} -> ${booleanLabel(history.to_auto_renew_cancelled)}`,
                    },
                    {
                      label: '订阅状态',
                      value: `${subscriptionStatusLabel(
                        history.from_status,
                      )} -> ${subscriptionStatusLabel(history.to_status)}`,
                    },
                  ]}
                />
              ))}
            </div>
          )}
        </section>

        <section className="asset-timeline-group" aria-label="IP 历史">
          <header className="asset-timeline-group__header">
            <h3>IP 变化</h3>
            <span><MonoDigits>{timeline.ip_histories.length}</MonoDigits></span>
          </header>
          {timeline.ip_histories.length === 0 ? (
            <TimelineEmpty>暂无 IP 变化历史</TimelineEmpty>
          ) : (
            <div className="asset-timeline-list">
              {timeline.ip_histories.map((history) => (
                <TimelineCard
                  key={history.ip_history_id}
                  tone="maintenance"
                  title="IP 地址变更"
                  subtitle={history.ip_history_id}
                  time={history.changed_at}
                  meta={[
                    {
                      label: 'IPv4',
                      value: changedValue(history.from_ipv4, history.to_ipv4),
                    },
                    {
                      label: 'IPv6',
                      value: changedValue(history.from_ipv6, history.to_ipv6),
                    },
                    { label: 'VPS ID', value: history.vps_id },
                  ]}
                />
              ))}
            </div>
          )}
        </section>

        <section className="asset-timeline-group" aria-label="规格快照">
          <header className="asset-timeline-group__header">
            <h3>规格快照</h3>
            <span><MonoDigits>{timeline.spec_snapshots.length}</MonoDigits></span>
          </header>
          {timeline.spec_snapshots.length === 0 ? (
            <TimelineEmpty>暂无规格快照</TimelineEmpty>
          ) : (
            <div className="asset-timeline-list">
              {timeline.spec_snapshots.map((snapshot) => (
                <TimelineCard
                  key={snapshot.snapshot_id}
                  tone="neutral"
                  title={snapshot.product_name || '规格快照'}
                  subtitle={snapshot.snapshot_id}
                  time={snapshot.captured_at}
                  meta={[
                    {
                      label: 'SSH',
                      value: `${snapshot.ssh_user || 'root'}@${snapshot.ssh_host || '—'}:${
                        snapshot.ssh_port
                      }`,
                    },
                    { label: '操作系统', value: formatOptional(snapshot.os_name) },
                    { label: '虚拟化', value: formatOptional(snapshot.virtualization) },
                    { label: 'VPS ID', value: snapshot.vps_id },
                  ]}
                />
              ))}
            </div>
          )}
        </section>
      </div>
    </section>
  )
}

import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { paymentMethodLabel } from '../../lib/assetOptions'
import { Badge, DataTable, MonoDigits, Timestamp, type BadgeTone } from '../../components/atoms'
import { formatMoney } from '../../lib/format'
import {
  ASSET_DOMAIN_STATUS_LABELS,
  ASSET_SERVICE_STATUS_LABELS,
  ASSET_SERVICE_TYPE_LABELS,
  VPS_EXPERIENCE_CATEGORY_LABELS,
  VPS_EXPERIENCE_SEVERITY_LABELS,
  type ArchiveReview,
  type AssetDomainRecord,
  type AssetServiceRecord,
  type SubscriptionRecord,
  type VPSExperienceLogRecord,
} from '../../lib/types'
import { SubscriptionStatusBadge } from '../assetPageBadges'
import { archiveDayLabel, type ArchiveTimelineEntry } from './archivePageHelpers'

export function ArchiveEmpty({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="archive-empty">
      <p className="archive-empty__title">{title}</p>
      {hint ? <p className="archive-empty__hint">{hint}</p> : null}
    </div>
  )
}

export function ArchiveSubsection({
  title,
  count,
  children,
}: {
  title: string
  count?: number
  children: ReactNode
}) {
  return (
    <section className="archive-subsection">
      <header className="archive-subsection__head">
        <h3>{title}</h3>
        {count === undefined ? null : <span className="archive-subsection__count"><MonoDigits>{count}</MonoDigits></span>}
      </header>
      {children}
    </section>
  )
}

const SEVERITY_TONE: Record<string, BadgeTone> = { info: 'neutral', warning: 'notice', critical: 'critical' }

export function ArchiveUserRecords({ records }: { records: VPSExperienceLogRecord[] }) {
  if (records.length === 0) {
    return (
      <ArchiveEmpty
        title="暂无用户记录"
        hint="归档前在 VPS 详情「记录经验」写下的网络、稳定性与服务支持感受会出现在这里，供再次选购时回看。"
      />
    )
  }
  return (
    <ol className="archive-records">
      {records.map((record) => (
        <li key={record.experience_log_id} className={`archive-record archive-record--${record.severity}`}>
          <header className="archive-record__head">
            <h3>{record.summary}</h3>
            <Timestamp value={record.occurred_at} mode="absolute" />
          </header>
          {record.details ? <p className="archive-record__body">{record.details}</p> : null}
          <div className="archive-record__tags">
            <Badge variant="info" tone="neutral">{VPS_EXPERIENCE_CATEGORY_LABELS[record.category] ?? record.category}</Badge>
            <Badge variant="state" tone={SEVERITY_TONE[record.severity] ?? 'neutral'}>
              {VPS_EXPERIENCE_SEVERITY_LABELS[record.severity] ?? record.severity}
            </Badge>
          </div>
        </li>
      ))}
    </ol>
  )
}

export function SubscriptionTable({ subscriptions }: { subscriptions: SubscriptionRecord[] }) {
  return (
    <DataTable
      className="archive-detail-subscription-table"
      rows={subscriptions}
      rowKey={(subscription) => subscription.subscription_id}
      emptyContent={<span className="empty-inline">暂无历史订阅</span>}
      columns={[
        {
          key: 'identity',
          label: '订阅',
          width: '180px',
          render: (subscription) => (
            <div className="asset-table__identity">
              <strong>{subscription.display_name || '未命名订阅'}</strong>
              <small className="mono-text">{subscription.subscription_id}</small>
            </div>
          ),
        },
        {
          key: 'period',
          label: '周期与费用',
          width: '200px',
          render: (subscription) => (
            <div className="asset-subscription-cell">
              <strong>{formatMoney(subscription.monthly_price, subscription.currency)}/月</strong>
              <span>{archiveDayLabel(subscription.started_at)} {'→'} {archiveDayLabel(subscription.renew_at)}</span>
            </div>
          ),
        },
        {
          key: 'status',
          label: '账单状态',
          width: '112px',
          render: (subscription) => <SubscriptionStatusBadge value={subscription.status} />,
        },
        {
          key: 'note',
          label: '支付方式与说明',
          render: (subscription) => (
            <div className="asset-table__stack">
              <span>{paymentMethodLabel(subscription.payment_method) || '—'}</span>
              {subscription.note ? <small>{subscription.note}</small> : null}
            </div>
          ),
        },
      ]}
    />
  )
}

export function ServicesTable({ services, onCorrect }: { services: AssetServiceRecord[]; onCorrect: (service: AssetServiceRecord) => void }) {
  return (
    <DataTable
      className="archive-detail-service-table"
      rows={services}
      rowKey={(service) => service.service_id}
      emptyContent={<span className="empty-inline">暂无服务记录</span>}
      columns={[
        {
          key: 'service',
          label: '服务',
          width: '220px',
          render: (service) => (
            <div className="asset-table__identity">
              <strong>{service.name}</strong>
              <small>{service.service_id}</small>
            </div>
          ),
        },
        {
          key: 'type',
          label: '类型 / 状态',
          width: '160px',
          render: (service) => (
            <div className="badge-row badge-row--wrap">
              <Badge variant="info" tone="neutral">{ASSET_SERVICE_TYPE_LABELS[service.service_type] ?? service.service_type}</Badge>
              <Badge variant="state" tone={service.status === 'active' ? 'normal' : 'offline'}>{ASSET_SERVICE_STATUS_LABELS[service.status] ?? service.status}</Badge>
            </div>
          ),
        },
        {
          key: 'entry',
          label: '入口',
          render: (service) => service.url || (service.port ? `端口 ${service.port}` : '—'),
        },
        {
          key: 'correct',
          label: '状态',
          width: '112px',
          render: (service) => (
            <button className="btn sm ghost" type="button" onClick={() => onCorrect(service)}>更正状态</button>
          ),
        },
      ]}
    />
  )
}

export function DomainsTable({ domains, onCorrect }: { domains: AssetDomainRecord[]; onCorrect: (domain: AssetDomainRecord) => void }) {
  return (
    <DataTable
      className="archive-detail-domain-table"
      rows={domains}
      rowKey={(domain) => domain.domain_id}
      emptyContent={<span className="empty-inline">暂无域名记录</span>}
      columns={[
        {
          key: 'domain',
          label: '域名',
          width: '220px',
          render: (domain) => (
            <div className="asset-table__identity">
              <strong>{domain.domain_name}</strong>
              <small>{domain.domain_id}</small>
            </div>
          ),
        },
        {
          key: 'status',
          label: '状态',
          width: '112px',
          render: (domain) => (
            <Badge variant="state" tone={domain.status === 'active' ? 'normal' : 'offline'}>
              {ASSET_DOMAIN_STATUS_LABELS[domain.status] ?? domain.status}
            </Badge>
          ),
        },
        {
          key: 'purpose',
          label: '用途',
          render: (domain) => domain.purpose || domain.registrar || '—',
        },
        {
          key: 'correct',
          label: '更正',
          width: '112px',
          render: (domain) => (
            <button className="btn sm ghost" type="button" onClick={() => onCorrect(domain)}>更正状态</button>
          ),
        },
      ]}
    />
  )
}

export function MonitoringHistoryTable({ vpsId, links }: { vpsId: string; links: ArchiveReview['monitoring_instance_links'] }) {
  return (
    <DataTable
      className="archive-detail-monitoring-table"
      rows={links}
      rowKey={(item) => item.monitoring_instance_id}
      emptyContent={<span className="empty-inline">暂无监控关联历史</span>}
      columns={[
        {
          key: 'identity',
          label: '监控实例',
          width: '220px',
          render: (item) => (
            <div className="asset-table__identity">
              <strong><Link to={`/monitoring/${encodeURIComponent(item.monitoring_instance_id)}?return_vps=${encodeURIComponent(vpsId)}`}>{item.display_name}</Link></strong>
              <small>{item.monitoring_instance_id}</small>
            </div>
          ),
        },
        {
          key: 'status',
          label: '状态',
          width: '168px',
          render: (item) => `${item.lifecycle_status || '未知'} / ${item.monitoring_status || '未知'}`,
        },
        {
          key: 'health',
          label: '历史健康',
          render: (item) => item.current_primary_issue_summary || item.current_health_status || '—',
        },
      ]}
    />
  )
}

export function TargetHistoryTable({ targets }: { targets: ArchiveReview['target_links'] }) {
  return (
    <DataTable
      className="archive-detail-target-table"
      rows={targets}
      rowKey={(target) => target.target_id}
      emptyContent={<span className="empty-inline">暂无入口探测关联历史</span>}
      columns={[
        {
          key: 'identity',
          label: '入口探测',
          width: '220px',
          render: (target) => (
            <div className="asset-table__identity">
              <strong>{target.name || target.target_id}</strong>
              <small>{target.target_id}</small>
            </div>
          ),
        },
        {
          key: 'status',
          label: '状态',
          width: '120px',
          render: (target) => target.run_status || '未知',
        },
        {
          key: 'links',
          label: '关联',
          render: (target) => `服务 ${target.service_ids.length} · 域名 ${target.domain_ids.length}`,
        },
      ]}
    />
  )
}

export function ArchiveTimeline({ entries }: { entries: ArchiveTimelineEntry[] }) {
  if (entries.length === 0) {
    return <ArchiveEmpty title="暂无变更记录" hint="续费决策、价格、规格和 IP 的变化会按时间汇总在这里。" />
  }
  return (
    <ol className="archive-timeline">
      {entries.map((entry) => (
        <li key={`${entry.kind}:${entry.key}`} className={`archive-timeline__item archive-timeline__item--${entry.kind}`}>
          <span className="archive-timeline__dot" aria-hidden="true" />
          <div className="archive-timeline__body">
            <header>
              <span className="archive-timeline__kind">{entry.kindLabel}</span>
              <Timestamp value={entry.time} mode="absolute" />
            </header>
            <p className="archive-timeline__title">{entry.title}</p>
            <p className="archive-timeline__detail">{entry.detail}</p>
          </div>
        </li>
      ))}
    </ol>
  )
}

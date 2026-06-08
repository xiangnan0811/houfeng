import { useEffect, useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { Badge, DataTable, Modal, MonoDigits, Timestamp } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import {
  ApiError,
  getVPSArchiveReview,
  getVPSTimeline,
  listSubscriptions,
  restoreVPSFromArchive,
} from '../lib/api'
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
  type TargetImpact,
  type VPSExperienceLogRecord,
  type VPSIPHistoryRecord,
  type VPSPriceHistoryRecord,
  type VPSSpecSnapshotRecord,
  type VPSTimeline,
} from '../lib/types'
import { formatDate, formatDateTime, formatMoney, formatOptional } from '../lib/format'
import { LifecycleBadge, RenewalBadge, SubscriptionStatusBadge, UsageBadge } from './assetPageBadges'
import { lifecycleLabel, renewalLabel, subscriptionStatusLabel, vpsAccessLabel, vpsLocationLabel } from './assetPageUtils'
import { subscriptionMonthlySummary } from './archive/archivePageHelpers'

type PageState = {
  loading: boolean
  error: string | null
  review: ArchiveReview | null
  timeline: VPSTimeline | null
  subscriptions: SubscriptionRecord[]
}

const INITIAL_STATE: PageState = {
  loading: true,
  error: null,
  review: null,
  timeline: null,
  subscriptions: [],
}

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function SectionCard({
  title,
  eyebrow,
  children,
}: {
  title: string
  eyebrow: string
  children: ReactNode
}) {
  return (
    <section className="page-panel archive-detail-card" aria-label={title}>
      <div className="archive-detail-card__heading">
        <p>{eyebrow}</p>
        <h2>{title}</h2>
      </div>
      {children}
    </section>
  )
}

function SummaryCard({
  title,
  eyebrow,
  children,
}: {
  title: string
  eyebrow: string
  children: ReactNode
}) {
  return (
    <section className="archive-detail-summary-card">
      <div className="archive-detail-summary-card__heading">
        <p>{eyebrow}</p>
        <h2>{title}</h2>
      </div>
      {children}
    </section>
  )
}

function DetailList({ items }: { items: Array<{ label: string; value: ReactNode }> }) {
  return (
    <dl className="archive-detail-list">
      {items.map((item) => (
        <div key={item.label}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
        </div>
      ))}
    </dl>
  )
}

function HistoryList({
  empty,
  children,
}: {
  empty: string
  children: ReactNode[]
}) {
  if (children.length === 0) {
    return <p className="empty-inline">{empty}</p>
  }
  return <div className="archive-detail-history-list">{children}</div>
}

function TimelineItem({
  title,
  subtitle,
  time,
  meta,
}: {
  title: string
  subtitle: string
  time: string
  meta: Array<{ label: string; value: ReactNode }>
}) {
  return (
    <article className="archive-detail-history-item">
      <header>
        <div>
          <h3>{title}</h3>
          <p>{subtitle}</p>
        </div>
        <Timestamp value={time} mode="absolute" />
      </header>
      <DetailList items={meta} />
    </article>
  )
}

function UserRecordsSection({ records }: { records: VPSExperienceLogRecord[] }) {
  return (
    <section className="page-panel archive-detail-card archive-detail-user-records" aria-label="用户记录">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">USER RECORDS</p>
          <h2>用户记录</h2>
          <p className="section-heading__description">归档后最重要的回看材料，优先展示自身使用体验、感受和问题判断。</p>
        </div>
        <Badge variant="count" tone="neutral">
          <MonoDigits>{records.length}</MonoDigits> 条
        </Badge>
      </div>
      <HistoryList empty="暂无用户记录">
        {records.map((record) => (
          <TimelineItem
            key={record.experience_log_id}
            title={record.summary}
            subtitle={record.details || (VPS_EXPERIENCE_CATEGORY_LABELS[record.category] ?? record.category)}
            time={record.occurred_at}
            meta={[
              { label: '分类', value: VPS_EXPERIENCE_CATEGORY_LABELS[record.category] ?? record.category },
              { label: '级别', value: VPS_EXPERIENCE_SEVERITY_LABELS[record.severity] ?? record.severity },
              { label: '记录 ID', value: record.experience_log_id },
            ]}
          />
        ))}
      </HistoryList>
    </section>
  )
}

function CompactTimelineSections({ timeline }: { timeline: VPSTimeline }) {
  return (
    <section className="page-panel archive-detail-card" aria-label="续费、价格、规格与 IP 历史">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">FACT HISTORY</p>
          <h2>续费、价格、规格与 IP 历史</h2>
          <p className="section-heading__description">这些是辅助判断材料，保留为归档 VPS 的事实变化证据。</p>
        </div>
      </div>
      <div className="archive-detail-history-grid">
        <HistoryGroup title="续费决策" count={timeline.renewal_decisions.length}>
          <HistoryList empty="暂无续费决策历史">
            {timeline.renewal_decisions.map((record) => (
              <TimelineItem
                key={record.decision_id}
                title={`${renewalLabel(record.from_decision ?? 'unreviewed')} -> ${renewalLabel(record.to_decision)}`}
                subtitle={record.reason || '未记录原因'}
                time={record.decided_at}
                meta={[
                  { label: 'Decision ID', value: record.decision_id },
                  { label: '创建时间', value: <Timestamp value={record.created_at} mode="absolute" /> },
                ]}
              />
            ))}
          </HistoryList>
        </HistoryGroup>
        <HistoryGroup title="价格历史" count={timeline.price_histories.length}>
          <HistoryList empty="暂无价格历史">
            {timeline.price_histories.map((record) => renderPriceHistory(record))}
          </HistoryList>
        </HistoryGroup>
        <HistoryGroup title="规格快照" count={timeline.spec_snapshots.length}>
          <HistoryList empty="暂无规格快照">
            {timeline.spec_snapshots.map((record) => renderSpecSnapshot(record))}
          </HistoryList>
        </HistoryGroup>
        <HistoryGroup title="IP 历史" count={timeline.ip_histories.length}>
          <HistoryList empty="暂无 IP 历史">
            {timeline.ip_histories.map((record) => renderIPHistory(record))}
          </HistoryList>
        </HistoryGroup>
      </div>
    </section>
  )
}

function HistoryGroup({
  title,
  count,
  children,
}: {
  title: string
  count: number
  children: ReactNode
}) {
  return (
    <section className="archive-detail-history-group">
      <header>
        <h3>{title}</h3>
        <span><MonoDigits>{count}</MonoDigits></span>
      </header>
      {children}
    </section>
  )
}

function renderPriceHistory(record: VPSPriceHistoryRecord) {
  return (
    <TimelineItem
      key={record.price_history_id}
      title={`${formatMoney(record.from_price, record.from_currency)} -> ${formatMoney(record.to_price, record.to_currency)}`}
      subtitle={`订阅 ${record.subscription_id}`}
      time={record.changed_at}
      meta={[
        { label: '月成本', value: `${formatMoney(record.from_monthly_price, record.from_currency)} -> ${formatMoney(record.to_monthly_price, record.to_currency)}` },
        { label: '状态', value: `${subscriptionStatusLabel(record.from_status)} -> ${subscriptionStatusLabel(record.to_status)}` },
      ]}
    />
  )
}

function renderSpecSnapshot(record: VPSSpecSnapshotRecord) {
  return (
    <TimelineItem
      key={record.snapshot_id}
      title={record.product_name || '规格快照'}
      subtitle={`${record.ssh_user || 'root'}@${record.ssh_host || '—'}:${record.ssh_port}`}
      time={record.captured_at}
      meta={[
        { label: '操作系统', value: formatOptional(record.os_name) },
        { label: '虚拟化', value: formatOptional(record.virtualization) },
      ]}
    />
  )
}

function renderIPHistory(record: VPSIPHistoryRecord) {
  return (
    <TimelineItem
      key={record.ip_history_id}
      title="IP 地址变更"
      subtitle={record.ip_history_id}
      time={record.changed_at}
      meta={[
        { label: 'IPv4', value: `${formatOptional(record.from_ipv4)} -> ${formatOptional(record.to_ipv4)}` },
        { label: 'IPv6', value: `${formatOptional(record.from_ipv6)} -> ${formatOptional(record.to_ipv6)}` },
      ]}
    />
  )
}

function SubscriptionTable({ subscriptions }: { subscriptions: SubscriptionRecord[] }) {
  return (
    <DataTable
      className="archive-detail-subscription-table"
      rows={subscriptions}
      rowKey={(subscription) => subscription.subscription_id}
      emptyContent={<span className="empty-inline">暂无历史订阅</span>}
      columns={[
        {
          key: 'period',
          label: '周期',
          width: '176px',
          render: (subscription) => (
            <div className="asset-subscription-cell">
              <strong>{formatMoney(subscription.monthly_price, subscription.currency)}/月</strong>
              <span>{formatDate(subscription.started_at)} {'->'} {formatDate(subscription.renew_at)}</span>
            </div>
          ),
        },
        {
          key: 'status',
          label: '状态',
          width: '112px',
          render: (subscription) => <SubscriptionStatusBadge value={subscription.status} />,
        },
        {
          key: 'note',
          label: '备注',
          render: (subscription) => subscription.note || subscription.payment_method || '—',
        },
      ]}
    />
  )
}

function ServicesTable({ services }: { services: AssetServiceRecord[] }) {
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
      ]}
    />
  )
}

function DomainsTable({ domains }: { domains: AssetDomainRecord[] }) {
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
      ]}
    />
  )
}

function MonitoringHistory({ review }: { review: ArchiveReview }) {
  return (
    <section className="page-panel archive-detail-card archive-detail__full-width" aria-label="监控历史">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">MONITORING HISTORY</p>
          <h2>监控历史</h2>
          <p className="section-heading__description">归档前保留在 VPS 台账里的监控实例证据，只读用于服务商质量回看。</p>
        </div>
        <Badge variant="count" tone="neutral"><MonoDigits>{review.monitoring_instance_links.length}</MonoDigits> 个关联</Badge>
      </div>
      <DataTable
        className="archive-detail-monitoring-table"
        rows={review.monitoring_instance_links}
        rowKey={(item) => item.monitoring_instance_id}
        emptyContent={<span className="empty-inline">暂无监控关联历史</span>}
        columns={[
          {
            key: 'identity',
            label: '监控实例',
            width: '220px',
            render: (item) => (
              <div className="asset-table__identity">
                <strong>{item.display_name}</strong>
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
    </section>
  )
}

function TargetHistory({ targets }: { targets: TargetImpact[] }) {
  return (
    <section className="page-panel archive-detail-card archive-detail__full-width" aria-label="Target 历史">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">TARGET HISTORY</p>
          <h2>Target 历史</h2>
          <p className="section-heading__description">来自归档 review 的服务/域名关联图，不依赖普通 Target 列表过滤。</p>
        </div>
        <Badge variant="count" tone="neutral"><MonoDigits>{targets.length}</MonoDigits> 个 Target</Badge>
      </div>
      <DataTable
        className="archive-detail-target-table"
        rows={targets}
        rowKey={(target) => target.target_id}
        emptyContent={<span className="empty-inline">暂无 Target 关联历史</span>}
        columns={[
          {
            key: 'identity',
            label: 'Target',
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
    </section>
  )
}

export function ArchiveDetailPage() {
  const { vpsId } = useParams()
  const navigate = useNavigate()
  const [state, setState] = useState<PageState>(INITIAL_STATE)
  const [restoreOpen, setRestoreOpen] = useState(false)
  const [restoreSubmitting, setRestoreSubmitting] = useState(false)
  const [restoreError, setRestoreError] = useState<string | null>(null)

  useEffect(() => {
    if (!vpsId) return
    let cancelled = false

    getVPSArchiveReview(vpsId)
      .then(async (review) => {
        if (cancelled) return
        const lifecycleStatus = review.vps.lifecycle_status
        if (lifecycleStatus !== 'archived' && lifecycleStatus !== 'cancelled') {
          navigate(`/vps/${encodeURIComponent(review.vps.vps_id)}`, { replace: true })
          return
        }
        const [timeline, subscriptions] = await Promise.all([
          getVPSTimeline(vpsId),
          listSubscriptions({ vps_id: vpsId, sort: 'renew_at', order: 'asc', asset_scope: 'all' }),
        ])
        if (cancelled) return
        setState({ loading: false, error: null, review, timeline, subscriptions })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState({
          loading: false,
          error: describeError(error, '加载归档详情失败'),
          review: null,
          timeline: null,
          subscriptions: [],
        })
      })

    return () => {
      cancelled = true
    }
  }, [navigate, vpsId])

  async function handleRestore() {
    if (!state.review) return
    setRestoreSubmitting(true)
    setRestoreError(null)
    try {
      await restoreVPSFromArchive(state.review.vps.vps_id)
      navigate(`/vps/${encodeURIComponent(state.review.vps.vps_id)}`, { replace: true })
    } catch (error: unknown) {
      setRestoreError(describeError(error, '恢复归档 VPS 失败'))
    } finally {
      setRestoreSubmitting(false)
    }
  }

  if (!vpsId) {
    return (
      <PageStateView
        kind="error"
        title="缺少归档 VPS ID"
        action={<Link className="btn sm secondary" to="/archive">返回归档列表</Link>}
      />
    )
  }

  if (state.loading) {
    return <PageStateView kind="loading" title="正在加载归档详情" />
  }

  if (state.error || !state.review || !state.timeline) {
    return (
      <PageStateView
        kind="error"
        title="归档详情加载失败"
        technicalSummary={state.error ?? 'missing archive detail'}
        action={<Link className="btn sm secondary" to="/archive">返回归档列表</Link>}
      />
    )
  }

  const { review, timeline, subscriptions } = state
  const vps = review.vps
  const isArchived = vps.lifecycle_status === 'archived'
  const isCancelled = vps.lifecycle_status === 'cancelled'

  return (
    <div className="page-stack archive-detail-page animate-in">
      <header className="watchtower-header archive-detail-header" role="banner" aria-label="归档 VPS 身份">
        <div className="watchtower-header__row1">
          <div className="watchtower-header__title-block">
            <p className="archive-detail-header__eyebrow">只读归档详情</p>
            <h1>{vps.display_name}</h1>
            <div className="badge-row">
              <LifecycleBadge value={vps.lifecycle_status} />
              <UsageBadge value={vps.usage_status} />
              <RenewalBadge value={vps.renewal_decision} />
            </div>
          </div>
          <div className="watchtower-header__actions">
            <Link className="btn sm secondary" to="/archive">归档列表</Link>
            <Link className="btn sm ghost" to="/vps">VPS 列表</Link>
            {isArchived ? (
              <button className="btn sm primary" type="button" onClick={() => setRestoreOpen(true)}>
                恢复为闲置
              </button>
            ) : null}
          </div>
        </div>
        <div className="watchtower-header__row2">
          <span className="watchtower-header__meta-item">{formatOptional(vps.provider_name)}</span>
          <span className="watchtower-header__meta-sep" aria-hidden>·</span>
          <span className="watchtower-header__meta-item">{vpsLocationLabel(vps)}</span>
        </div>
      </header>

      <section className="page-panel archive-detail-notice">
        <p>已归档资产不会进入 VPS、订阅、监控或资产组合决策主流程。</p>
        {isArchived ? (
          <p>恢复会把 VPS 放回闲置状态，但不会删除、重建或改写历史关联。</p>
        ) : isCancelled ? (
          <p>已取消资产不可恢复，仅保留历史回看。</p>
        ) : (
          <p>当前资产处于只读归档视图。</p>
        )}
      </section>

      <div className="archive-detail-summary-grid">
        <SummaryCard title="基础信息" eyebrow="IDENTITY">
          <DetailList items={[
            { label: '服务商', value: formatOptional(vps.provider_name) },
            { label: '产品', value: formatOptional(vps.product_name) },
            { label: '位置', value: vpsLocationLabel(vps) },
            { label: '归档时间', value: formatDateTime(vps.archived_at ?? vps.updated_at) },
          ]}
          />
        </SummaryCard>
        <SummaryCard title="访问入口" eyebrow="ACCESS">
          <DetailList items={[
            { label: '主入口', value: vpsAccessLabel(vps) },
            { label: 'IPv4', value: formatOptional(vps.ipv4) },
            { label: 'IPv6', value: formatOptional(vps.ipv6) },
            { label: 'SSH', value: `${vps.ssh_user || 'root'}@${vps.ssh_host || '—'}:${vps.ssh_port}` },
          ]}
          />
        </SummaryCard>
        <SummaryCard title="订阅历史" eyebrow="BILLING">
          <DetailList items={[
            { label: '历史次数', value: <MonoDigits>{subscriptions.length}</MonoDigits> },
            { label: '最近状态', value: subscriptions[0] ? <SubscriptionStatusBadge value={subscriptions[0].status} /> : '—' },
            { label: '最近续费日', value: formatDate(subscriptions[0]?.renew_at) },
          ]}
          />
        </SummaryCard>
        <SummaryCard title="月成本" eyebrow="MONTHLY">
          <strong className="archive-detail-summary-card__metric">{subscriptionMonthlySummary(subscriptions)}</strong>
        </SummaryCard>
        <SummaryCard title="服务" eyebrow="SERVICES">
          <DetailList items={[
            { label: '服务数量', value: <MonoDigits>{review.services.length}</MonoDigits> },
            { label: '首个服务', value: review.services[0]?.name ?? '—' },
          ]}
          />
        </SummaryCard>
        <SummaryCard title="域名" eyebrow="DOMAINS">
          <DetailList items={[
            { label: '域名数量', value: <MonoDigits>{review.domains.length}</MonoDigits> },
            { label: '首个域名', value: review.domains[0]?.domain_name ?? '—' },
          ]}
          />
        </SummaryCard>
        <SummaryCard title="续费判断" eyebrow="DECISION">
          <DetailList items={[
            { label: '当前判断', value: lifecycleLabel(vps.lifecycle_status) },
            { label: '续费决策', value: renewalLabel(vps.renewal_decision) },
            { label: '归档资格', value: review.eligible ? '可归档' : '归档动作不可用' },
          ]}
          />
        </SummaryCard>
        <SummaryCard title="资产历史" eyebrow="TIMELINE">
          <DetailList items={[
            { label: '用户记录', value: <MonoDigits>{timeline.experience_logs.length}</MonoDigits> },
            { label: '续费历史', value: <MonoDigits>{timeline.renewal_decisions.length}</MonoDigits> },
            { label: '价格历史', value: <MonoDigits>{timeline.price_histories.length}</MonoDigits> },
            { label: 'IP / 规格', value: `${timeline.ip_histories.length} / ${timeline.spec_snapshots.length}` },
          ]}
          />
        </SummaryCard>
      </div>

      <UserRecordsSection records={timeline.experience_logs} />
      <CompactTimelineSections timeline={timeline} />

      <SectionCard title="订阅明细" eyebrow="SUBSCRIPTIONS">
        <SubscriptionTable subscriptions={subscriptions} />
      </SectionCard>

      <div className="archive-detail-two-col">
        <SectionCard title="服务资产" eyebrow="SERVICES">
          <ServicesTable services={review.services} />
        </SectionCard>
        <SectionCard title="域名资产" eyebrow="DOMAINS">
          <DomainsTable domains={review.domains} />
        </SectionCard>
      </div>

      <MonitoringHistory review={review} />
      <TargetHistory targets={review.target_links} />

      <Modal
        open={restoreOpen}
        onClose={() => setRestoreOpen(false)}
        title="确认恢复归档 VPS"
        dialogRole="alertdialog"
        size="md"
      >
        <div className="asset-lifecycle-confirm">
          <p className="asset-lifecycle-confirm__eyebrow">RESTORE</p>
          <h4>恢复后进入闲置状态，关联订阅、监控、服务、域名和历史记录会保留。</h4>
          <p className="asset-lifecycle-confirm__callouts">恢复不会把它还原到归档前的精确生命周期，只会变为闲置。</p>
          {restoreError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{restoreError}</p> : null}
          <div className="page-form-actions">
            <button className="btn md secondary" type="button" disabled={restoreSubmitting} onClick={() => setRestoreOpen(false)}>取消</button>
            <button className="btn md primary" type="button" disabled={restoreSubmitting} onClick={() => void handleRestore()}>
              {restoreSubmitting ? '恢复中…' : '确认恢复'}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}

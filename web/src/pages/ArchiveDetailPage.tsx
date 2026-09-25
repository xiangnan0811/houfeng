import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'

import { Badge, DataTable, Input, Modal, MonoDigits, Timestamp } from '../components/atoms'
import { ArchiveBlockerDetails } from '../components/ArchiveBlockerDetails'
import { DependencyStatusCorrection } from '../components/DependencyStatusCorrection'
import { PageState as PageStateView } from '../components/PageState'
import { VPSCancellationWorkbench } from '../components/VPSCancellationWorkbench'
import {
  ApiError,
  archiveVPS,
  applyVPSCancellation,
  getVPSArchiveReview,
  getVPSCancellationPreview,
  getVPSTimeline,
  listSubscriptions,
  restoreVPSFromArchive,
} from '../lib/api'
import { isArchiveReview, isManagementReviewStale } from '../lib/assetLifecycle'
import type { ApplyCancellationInput, CancellationPreview } from '../lib/types'
import {
  ASSET_DOMAIN_STATUS_LABELS,
  ASSET_SERVICE_STATUS_LABELS,
  ASSET_SERVICE_TYPE_LABELS,
  VPS_EXPERIENCE_CATEGORY_LABELS,
  VPS_EXPERIENCE_SEVERITY_LABELS,
  type ArchiveBlockerDetail,
  type ArchiveReview,
  type AssetDomainRecord,
  type AssetServiceRecord,
  type SubscriptionRecord,
  type VPSExperienceLogRecord,
  type VPSIPHistoryRecord,
  type VPSPriceHistoryRecord,
  type VPSSpecSnapshotRecord,
  type VPSTimeline,
} from '../lib/types'
import { formatDate, formatDateTime, formatMoney, formatOptional } from '../lib/format'
import { LifecycleBadge, RenewalBadge, SubscriptionStatusBadge, UsageBadge } from './assetPageBadges'
import { renewalLabel, subscriptionStatusLabel, vpsAccessLabel, vpsLocationLabel } from './assetPageUtils'
import { subscriptionMonthlySummary } from './archive/archivePageHelpers'

type AsyncState<T> = {
  loading: boolean
  error: string | null
  data: T
}

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function ResidualHandlingDialog({
  vpsId,
  onClose,
  onApplied,
}: {
  vpsId: string
  onClose: () => void
  onApplied: () => void
}) {
  const [preview, setPreview] = useState<CancellationPreview | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const generation = useRef(0)

  useEffect(() => {
    const current = ++generation.current
    void getVPSCancellationPreview(vpsId)
      .then((next) => {
        if (current !== generation.current) return
        setPreview(next)
      })
      .catch((caught: unknown) => {
        if (current !== generation.current) return
        setError(describeError(caught, '加载残留预览失败'))
      })
    return () => {
      generation.current += 1
    }
  }, [vpsId])

  async function submit(input: ApplyCancellationInput) {
    const current = generation.current
    setSubmitting(true)
    setError(null)
    try {
      await applyVPSCancellation(vpsId, input)
      if (current !== generation.current) return
      onApplied()
      onClose()
    } catch (caught: unknown) {
      if (current !== generation.current) return
      if (isManagementReviewStale(caught)) {
        try {
          const next = await getVPSCancellationPreview(vpsId)
          if (current !== generation.current) return
          setPreview(next)
          setError('影响范围已变化，共享确认已清除，不会自动重新提交。')
        } catch (refreshError: unknown) {
          if (current !== generation.current) return
          setError(describeError(refreshError, '影响范围已变化，但预览刷新失败'))
        }
        return
      }
      setError(describeError(caught, '处理残留失败'))
    } finally {
      if (current === generation.current) setSubmitting(false)
    }
  }

  return (
    <Modal open onClose={onClose} title="处理残留" size="xl">
      {error ? <p role="alert">{error}</p> : null}
      {preview ? (
        <VPSCancellationWorkbench
          preview={preview}
          submitting={submitting}
          error={error}
          onCancel={onClose}
          onSubmit={submit}
        />
      ) : <p role="status">正在加载残留预览…</p>}
    </Modal>
  )
}

function CancelledArchiveDialog({
  vpsId,
  displayName,
  review,
  onClose,
  onArchived,
  onInline,
}: {
  vpsId: string
  displayName: string
  review: ArchiveReview
  onClose: () => void
  onArchived: () => void
  onInline: (detail: ArchiveBlockerDetail, kind: 'service-status' | 'domain-status' | 'residual' | 'restore') => void
}) {
  const [reason, setReason] = useState('')
  const [confirmationName, setConfirmationName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [conflict, setConflict] = useState<ArchiveReview | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const shown = conflict ?? review
  const nameMatches = confirmationName.trim() === displayName

  async function submit() {
    if (!reason.trim() || !nameMatches || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      await archiveVPS(vpsId, { reason: reason.trim(), confirmation_name: confirmationName.trim() })
      onArchived()
      onClose()
    } catch (caught: unknown) {
      const nextReview = caught instanceof ApiError && isArchiveReview(caught.review) ? caught.review : null
      if (nextReview) setConflict(nextReview)
      setError(describeError(caught, '归档失败'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal open onClose={onClose} title="受控归档" size="lg">
      <p>归档成功和当前可归档是两件事。下面只展示这次审查看到的阻塞，不会把“不可归档”写成“已归档”。</p>
      {shown.blocker_details.length > 0 ? (
        <ArchiveBlockerDetails details={shown.blocker_details} vpsId={vpsId} onInline={onInline} />
      ) : <p role="status">这次审查没有列出归档阻塞。</p>}
      {error ? <p role="alert">{error}</p> : null}
      <Input label="归档原因" value={reason} onChange={(event) => setReason(event.target.value)} />
      <Input label="输入 VPS 名称确认" value={confirmationName} onChange={(event) => setConfirmationName(event.target.value)} />
      <div className="page-form-actions">
        <button className="btn sm secondary" type="button" onClick={onClose}>取消</button>
        <button className="btn sm primary" type="button" disabled={submitting || !reason.trim() || !nameMatches} onClick={() => void submit()}>
          {submitting ? '归档中…' : '确认归档'}
        </button>
      </div>
    </Modal>
  )
}

function parseEventTime(t?: string | null): number {
  if (!t) return -Infinity
  const parsed = Date.parse(t)
  return Number.isNaN(parsed) ? -Infinity : parsed
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
    <section className="page-panel archive-detail-card archive-detail-user-records" role="region" aria-label="用户记录">
      <div className="section-heading">
        <div>
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
  const userPart = record.ssh_user ? `${record.ssh_user}@` : ''
  const hostPart = record.ssh_host || '—'
  const portPart = record.ssh_port ? `:${record.ssh_port}` : ''
  const sshSubtitle = record.ssh_host ? `${userPart}${hostPart}${portPart}` : '—'

  return (
    <TimelineItem
      key={record.snapshot_id}
      title={record.product_name || '规格快照'}
      subtitle={sshSubtitle}
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
              <span>{formatDate(subscription.started_at)} {'->'} {formatDate(subscription.renew_at)}</span>
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
              <span>{subscription.payment_method || '—'}</span>
              {subscription.note ? <small>{subscription.note}</small> : null}
            </div>
          ),
        },
      ]}
    />
  )
}

function ServicesTable({ services, onCorrect }: { services: AssetServiceRecord[]; onCorrect?: (service: AssetServiceRecord) => void }) {
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
        ...(onCorrect ? [{
          key: 'correct',
          label: '状态',
          width: '112px',
          render: (service: AssetServiceRecord) => (
            <button className="btn sm ghost" type="button" onClick={() => onCorrect(service)}>更正状态</button>
          ),
        }] : []),
      ]}
    />
  )
}

function DomainsTable({ domains, onCorrect }: { domains: AssetDomainRecord[]; onCorrect?: (domain: AssetDomainRecord) => void }) {
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
        ...(onCorrect ? [{
          key: 'correct',
          label: '更正',
          width: '112px',
          render: (domain: AssetDomainRecord) => (
            <button className="btn sm ghost" type="button" onClick={() => onCorrect(domain)}>更正状态</button>
          ),
        }] : []),
      ]}
    />
  )
}

export function ArchiveDetailPage() {
  const { vpsId } = useParams()
  return (
    <ArchiveDetailPageContent
      key={vpsId ?? 'missing-vps-id'}
      {...(vpsId === undefined ? {} : { vpsId })}
    />
  )
}

function ArchiveDetailPageContent({ vpsId }: { vpsId?: string }) {
  const navigate = useNavigate()
  const location = useLocation()

  const [reviewState, setReviewState] = useState<AsyncState<ArchiveReview | null>>({
    loading: true,
    error: null,
    data: null,
  })
  const [timelineState, setTimelineState] = useState<AsyncState<VPSTimeline | null>>({
    loading: true,
    error: null,
    data: null,
  })
  const [subscriptionsState, setSubscriptionsState] = useState<AsyncState<SubscriptionRecord[] | null>>({
    loading: true,
    error: null,
    data: null,
  })
  const [restoreSubmitting, setRestoreSubmitting] = useState(false)
  const [restoreError, setRestoreError] = useState<string | null>(null)
  const [restoreOpen, setRestoreOpen] = useState(false)
  const [restoreReason, setRestoreReason] = useState('')
  const [residualOpen, setResidualOpen] = useState(false)
  const [archiveOpen, setArchiveOpen] = useState(false)
  const [archiveNotice, setArchiveNotice] = useState<string | null>(null)
  const [statusTarget, setStatusTarget] = useState<{ kind: 'service' | 'domain'; id: string; name: string; status: string } | null>(null)
  const restoreTriggerRef = useRef<HTMLButtonElement>(null)
  const restoreSubmittingRef = useRef(false)
  function handleInlineBlocker(
    detail: ArchiveBlockerDetail,
    kind: 'service-status' | 'domain-status' | 'residual' | 'restore',
  ) {
    if (kind === 'service-status') {
      setStatusTarget({
        kind: 'service',
        id: detail.object_id,
        name: detail.display_name || detail.object_id,
        status: detail.current_state,
      })
    } else if (kind === 'domain-status') {
      setStatusTarget({
        kind: 'domain',
        id: detail.object_id,
        name: detail.display_name || detail.object_id,
        status: detail.current_state,
      })
    } else if (kind === 'residual') {
      setResidualOpen(true)
    } else if (kind === 'restore') {
      setRestoreOpen(true)
    }
  }


  const reviewGenRef = useRef(0)
  const timelineGenRef = useRef(0)
  const subsGenRef = useRef(0)

  const fetchTimeline = useCallback((id: string, gen: number) => {
    getVPSTimeline(id)
      .then((timeline) => {
        if (gen === timelineGenRef.current) {
          setTimelineState({ loading: false, error: null, data: timeline })
        }
      })
      .catch((error: unknown) => {
        if (gen === timelineGenRef.current) {
          setTimelineState({
            loading: false,
            error: describeError(error, '加载变更历史失败'),
            data: null,
          })
        }
      })
  }, [])

  const fetchSubscriptions = useCallback((id: string, gen: number) => {
    listSubscriptions({ vps_id: id, sort: 'renew_at', order: 'asc', asset_scope: 'all' })
      .then((subscriptions) => {
        if (gen === subsGenRef.current) {
          setSubscriptionsState({ loading: false, error: null, data: subscriptions })
        }
      })
      .catch((error: unknown) => {
        if (gen === subsGenRef.current) {
          setSubscriptionsState((prev) => ({
            loading: false,
            error: describeError(error, '加载历史订阅失败'),
            data: prev.data,
          }))
        }
      })
  }, [])

  const fetchReview = useCallback((id: string, gen: number) => {
    getVPSArchiveReview(id)
      .then((review) => {
        if (gen !== reviewGenRef.current) return
        const lifecycleStatus = review.vps.lifecycle_status
        if (lifecycleStatus !== 'archived' && lifecycleStatus !== 'cancelled') {
          navigate('/vps/' + encodeURIComponent(review.vps.vps_id), { replace: true, state: location.state })
          return
        }
        setReviewState({ loading: false, error: null, data: review })
        fetchTimeline(id, ++timelineGenRef.current)
        fetchSubscriptions(id, ++subsGenRef.current)
      })
      .catch((error: unknown) => {
        if (gen !== reviewGenRef.current) return
        setReviewState({
          loading: false,
          error: describeError(error, '加载归档详情失败'),
          data: null,
        })
      })
  }, [navigate, location.state, fetchTimeline, fetchSubscriptions])

  useEffect(() => {
    if (!vpsId) return
    reviewGenRef.current += 1
    timelineGenRef.current += 1
    subsGenRef.current += 1
    fetchReview(vpsId, reviewGenRef.current)
    return () => {
      ++reviewGenRef.current
      ++timelineGenRef.current
      ++subsGenRef.current
    }
  }, [vpsId, fetchReview])

  const handleRetryReview = useCallback(() => {
    if (!vpsId) return
    setReviewState({ loading: true, error: null, data: null })
    fetchReview(vpsId, ++reviewGenRef.current)
  }, [vpsId, fetchReview])

  const handleRetryTimeline = useCallback(() => {
    if (!vpsId) return
    const nextGen = ++timelineGenRef.current
    setTimelineState({ loading: true, error: null, data: null })
    fetchTimeline(vpsId, nextGen)
  }, [vpsId, fetchTimeline])

  const handleRetrySubscriptions = useCallback(() => {
    if (!vpsId) return
    const nextGen = ++subsGenRef.current
    setSubscriptionsState((prev) => ({ ...prev, loading: true, error: null }))
    fetchSubscriptions(vpsId, nextGen)
  }, [vpsId, fetchSubscriptions])

  async function handleRestore() {
    if (restoreSubmittingRef.current || !reviewState.data) return
    const reason = restoreReason.trim()
    if (!reason) {
      setRestoreError('需要填写恢复原因。')
      return
    }
    const targetVpsId = reviewState.data.vps.vps_id
    const currentGen = reviewGenRef.current

    restoreSubmittingRef.current = true
    setRestoreSubmitting(true)
    setRestoreError(null)

    try {
      await restoreVPSFromArchive(targetVpsId, { reason })
      if (currentGen !== reviewGenRef.current) return
      navigate(`/vps/${encodeURIComponent(targetVpsId)}?reorganize=1`, { replace: true, state: location.state })
    } catch (error: unknown) {
      if (currentGen !== reviewGenRef.current) return
      setRestoreError(describeError(error, '恢复归档 VPS 失败'))
    } finally {
      if (currentGen === reviewGenRef.current) {
        restoreSubmittingRef.current = false
        setRestoreSubmitting(false)
      }
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

  if (reviewState.loading) {
    return <PageStateView kind="loading" title="正在加载归档详情" />
  }

  if (reviewState.error || !reviewState.data) {
    return (
      <PageStateView
        kind="error"
        title="归档详情加载失败"
        technicalSummary={reviewState.error ?? 'missing archive detail'}
        action={
          <div className="page-state__actions">
            <button className="btn sm primary" type="button" onClick={handleRetryReview}>
              重试加载详情
            </button>
            <Link className="btn sm secondary" to="/archive">返回归档列表</Link>
          </div>
        }
      />
    )
  }

  const review = reviewState.data
  const vps = review.vps
  const isArchived = vps.lifecycle_status === 'archived'
  const isCancelled = vps.lifecycle_status === 'cancelled'

  const reviewSnapshotSubscriptions = review.subscriptions.map((s) => s.record)
  const effectiveSubscriptions = subscriptionsState.data !== null
    ? subscriptionsState.data
    : reviewSnapshotSubscriptions
  const isUsingSnapshotFallback = subscriptionsState.data === null &&
    reviewSnapshotSubscriptions.length > 0 &&
    subscriptionsState.error !== null

  const sortedSubscriptions = [...effectiveSubscriptions].sort((a, b) => (
    parseEventTime(b.renew_at || b.started_at) - parseEventTime(a.renew_at || a.started_at)
  ))
  const latestSubscription = sortedSubscriptions[0]

  const sortedExperienceLogs = timelineState.data
    ? [...timelineState.data.experience_logs].sort((a, b) => (
        parseEventTime(b.occurred_at || b.created_at) - parseEventTime(a.occurred_at || a.created_at)
      ))
    : []

  const sortedDecisions = timelineState.data
    ? [...timelineState.data.renewal_decisions].sort((a, b) => (
        parseEventTime(b.decided_at || b.created_at) - parseEventTime(a.decided_at || a.created_at)
      ))
    : []

  const sortedPriceHistories = timelineState.data
    ? [...timelineState.data.price_histories].sort((a, b) => (
        parseEventTime(b.changed_at || b.created_at) - parseEventTime(a.changed_at || a.created_at)
      ))
    : []

  const sortedSpecSnapshots = timelineState.data
    ? [...timelineState.data.spec_snapshots].sort((a, b) => (
        parseEventTime(b.captured_at || b.created_at) - parseEventTime(a.captured_at || a.created_at)
      ))
    : []

  const sortedIPHistories = timelineState.data
    ? [...timelineState.data.ip_histories].sort((a, b) => (
        parseEventTime(b.changed_at || b.created_at) - parseEventTime(a.changed_at || a.created_at)
      ))
    : []

  const sshUser = vps.ssh_user ? `${vps.ssh_user}@` : ''
  const sshPort = vps.ssh_port ? `:${vps.ssh_port}` : ''
  const sshConnection = vps.ssh_host ? `${sshUser}${vps.ssh_host}${sshPort}` : '—'

  return (
    <div className="page archive-detail-page">
      <header className="page__head" role="banner" aria-label="归档 VPS 身份">
        <div>
          <h1 className="page__title">
            <span>{vps.display_name}</span>
            <small className="archive-detail-head-id mono-text">{vps.vps_id}</small>
          </h1>
          <p className="page-sub">{isArchived ? '已归档' : isCancelled ? '已取消，未归档' : '历史资产'}</p>
          <p className="page-sub">
            {formatOptional(vps.provider_name)}
            {' · '}
            {vpsLocationLabel(vps)}
            {vps.datacenter ? ` (${vps.datacenter})` : ''}
          </p>
          <div className="badge-row">
            <LifecycleBadge value={vps.lifecycle_status} />
            <UsageBadge value={vps.usage_status} />
            <RenewalBadge value={vps.renewal_decision} />
          </div>
        </div>
        <div className="page__actions">
          <Link className="btn sm secondary" to="/archive">归档列表</Link>
          <Link className="btn sm ghost" to="/vps">VPS 列表</Link>
          {isArchived ? (
            <button
              ref={restoreTriggerRef}
              className="btn sm primary"
              type="button"
              onClick={() => {
                setRestoreError(null)
                setRestoreOpen(true)
              }}
            >
              恢复为闲置
            </button>
          ) : null}
          {isCancelled ? (
            <>
              <button className="btn sm secondary" type="button" onClick={() => setResidualOpen(true)}>处理残留</button>
              <button className="btn sm primary" type="button" onClick={() => setArchiveOpen(true)}>受控归档</button>
            </>
          ) : null}
        </div>
      </header>

      <section className="page-panel archive-detail-notice">
        {archiveNotice ? <p role="status">{archiveNotice}</p> : null}
        {isArchived ? (
          <p>已归档资产不会进入当前工作集。恢复只回到闲置，不展示再次归档资格。</p>
        ) : isCancelled ? (
          <p>已取消，未归档。处理残留和受控归档是分开的动作；当前审查不可归档不等于已经归档。</p>
        ) : (
          <p>当前资产处于只读历史视图。</p>
        )}
        {isCancelled && review.blocker_details.length > 0 ? (
          <ArchiveBlockerDetails details={review.blocker_details} vpsId={vps.vps_id} onInline={handleInlineBlocker} />
        ) : null}
      {residualOpen && isCancelled ? (
        <ResidualHandlingDialog
          vpsId={vps.vps_id}
          onClose={() => setResidualOpen(false)}
          onApplied={() => {
            setArchiveNotice('残留处理已提交。这不是归档成功。')
            fetchReview(vps.vps_id, ++reviewGenRef.current)
          }}
        />
      ) : null}
      {statusTarget ? (
        <DependencyStatusCorrection
          open
          kind={statusTarget.kind}
          objectId={statusTarget.id}
          displayName={statusTarget.name}
          currentStatus={statusTarget.status}
          parentLifecycle={vps.lifecycle_status}
          onClose={() => setStatusTarget(null)}
          onCompleted={() => {
            setStatusTarget(null)
            setArchiveNotice('依赖状态已更正。归档资格以刷新后的审查为准。')
            fetchReview(vps.vps_id, ++reviewGenRef.current)
          }}
        />
      ) : null}
      {archiveOpen && isCancelled ? (
        <CancelledArchiveDialog
          vpsId={vps.vps_id}
          displayName={vps.display_name}
          review={review}
          onClose={() => setArchiveOpen(false)}
          onInline={handleInlineBlocker}
          onArchived={() => {
            setArchiveNotice('归档已提交。页面将按最新审查刷新，不会用归档前的可归档资格代替成功结果。')
            fetchReview(vps.vps_id, ++reviewGenRef.current)
          }}
        />
      ) : null}
      </section>

      {/* 历史身份与访问事实 */}
      <section className="page-panel archive-detail-card">
        <div className="section-heading">
          <div>
            <h2>历史身份与访问事实</h2>
            <p className="section-heading__description">资产在归档前记录的规格、归档时间及网络连接入口。</p>
          </div>
        </div>
        <div className="archive-detail-identity-grid">
          <div className="archive-detail-fact-block">
            <h3 className="archive-detail-block-title">基础与归档事实</h3>
            <DetailList
              items={[
                { label: '服务商', value: formatOptional(vps.provider_name) },
                { label: '产品型号', value: formatOptional(vps.product_name) },
                { label: '位置与机房', value: `${vpsLocationLabel(vps)}${vps.datacenter ? ` · ${vps.datacenter}` : ''}` },
                {
                  label: vps.archived_at ? '归档时间' : '更新时间',
                  value: vps.archived_at ? (
                    <MonoDigits>{formatDateTime(vps.archived_at)}</MonoDigits>
                  ) : (
                    <span>
                      <span className="text-muted">未记录归档时间</span>
                      {' · '}
                      <small className="text-muted">更新于 <MonoDigits>{formatDateTime(vps.updated_at)}</MonoDigits></small>
                    </span>
                  ),
                },
                { label: '续费决策', value: renewalLabel(vps.renewal_decision) },
                { label: '当前审查', value: review.eligible ? '当前审查未列出归档阻塞' : '当前审查不可归档' },
                { label: '备注', value: formatOptional(vps.note) },
              ]}
            />
          </div>
          <div className="archive-detail-fact-block">
            <h3 className="archive-detail-block-title">网络与访问入口</h3>
            <DetailList
              items={[
                { label: '主入口', value: vpsAccessLabel(vps) },
                { label: 'IPv4', value: formatOptional(vps.ipv4) },
                { label: 'IPv6', value: formatOptional(vps.ipv6) },
                { label: 'SSH 连接', value: <MonoDigits>{sshConnection}</MonoDigits> },
              ]}
            />
          </div>
        </div>
      </section>

      {/* 历史账单与订阅 */}
      <section className="page-panel archive-detail-card">
        <div className="section-heading">
          <div>
            <h2>历史账单与订阅</h2>
            <p className="section-heading__description">记录已归档资产的历史续费与账单记录。</p>
          </div>
        </div>

        {subscriptionsState.loading && effectiveSubscriptions.length === 0 ? (
          <p className="empty-inline">正在加载历史订阅…</p>
        ) : subscriptionsState.error && effectiveSubscriptions.length === 0 ? (
          <div className="archive-detail-local-error" role="alert">
            <p>历史订阅加载失败：{subscriptionsState.error}</p>
            <button
              type="button"
              className="btn sm secondary"
              onClick={handleRetrySubscriptions}
            >
              重试加载订阅
            </button>
          </div>
        ) : (
          <>
            <div className="archive-detail-billing-meta">
              <div>
                <span>历史月成本：</span>
                <strong>{subscriptionMonthlySummary(effectiveSubscriptions, '无历史订阅')}</strong>
              </div>
              <div>
                <span>订阅笔数：</span>
                <strong><MonoDigits>{effectiveSubscriptions.length}</MonoDigits> 笔</strong>
              </div>
              {latestSubscription?.renew_at ? (
                <div>
                  <span>最近续费计划：</span>
                  <strong><MonoDigits>{formatDate(latestSubscription.renew_at)}</MonoDigits></strong>
                </div>
              ) : null}
              {isUsingSnapshotFallback ? (
                <span className="text-muted">来自本次归档审查数据</span>
              ) : null}
            </div>

            {subscriptionsState.loading ? (
              <p className="empty-inline" role="status">正在更新历史订阅，保留已知账单…</p>
            ) : null}
            {subscriptionsState.error ? (
              <div className="archive-detail-local-error" role="alert">
                <p>订阅重读失败：{subscriptionsState.error}，当前保留归档审查时的已知快照。</p>
                <button
                  type="button"
                  className="btn sm secondary"
                  onClick={handleRetrySubscriptions}
                  disabled={subscriptionsState.loading}
                >
                  重试加载订阅
                </button>
              </div>
            ) : null}

            <div
              className="page-panel--scroll-x"
              role="region"
              aria-label="订阅明细"
              tabIndex={0}
            >
              <SubscriptionTable subscriptions={sortedSubscriptions} />
            </div>
          </>
        )}
      </section>

      {/* 监控历史 */}
      <section className="page-panel archive-detail-card archive-detail__full-width">
        <div className="section-heading">
          <div>
            <h2>监控历史</h2>
            <p className="section-heading__description">归档前保留在 VPS 台账里的监控实例证据，只读用于服务商质量回看。</p>
          </div>
          <Badge variant="count" tone="neutral"><MonoDigits>{review.monitoring_instance_links.length}</MonoDigits> 个关联</Badge>
        </div>
        <div
          className="page-panel--scroll-x"
          role="region"
          aria-label="监控历史"
          tabIndex={0}
        >
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
        </div>
      </section>

      {/* 入口探测历史 */}
      <section className="page-panel archive-detail-card archive-detail__full-width">
        <div className="section-heading">
          <div>
            <h2>入口探测历史</h2>
            <p className="section-heading__description">已归档资产的服务与域名关联入口探测。</p>
          </div>
          <Badge variant="count" tone="neutral"><MonoDigits>{review.target_links.length}</MonoDigits> 个入口探测</Badge>
        </div>
        <div
          className="page-panel--scroll-x"
          role="region"
          aria-label="入口探测历史"
          tabIndex={0}
        >
          <DataTable
            className="archive-detail-target-table"
            rows={review.target_links}
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
        </div>
      </section>

      {/* 服务与域名资产 */}
      <div className="archive-detail-two-col">
        <section className="page-panel archive-detail-card">
          <div className="section-heading">
            <div>
              <h2>服务资产</h2>
              <p className="section-heading__description">归档 VPS 保留的服务记录与端点事实。</p>
            </div>
            <Badge variant="count" tone="neutral"><MonoDigits>{review.services.length}</MonoDigits> 个服务</Badge>
          </div>
          <div
            className="page-panel--scroll-x"
            role="region"
            aria-label="服务资产"
            tabIndex={0}
          >
            <ServicesTable
              services={review.services}
              onCorrect={(service) => setStatusTarget({ kind: 'service', id: service.service_id, name: service.name, status: service.status })}
            />
          </div>
        </section>

        <section className="page-panel archive-detail-card">
          <div className="section-heading">
            <div>
              <h2>域名资产</h2>
              <p className="section-heading__description">归档 VPS 保留的域名、证书与注册事实。</p>
            </div>
            <Badge variant="count" tone="neutral"><MonoDigits>{review.domains.length}</MonoDigits> 个域名</Badge>
          </div>
          <div
            className="page-panel--scroll-x"
            role="region"
            aria-label="域名资产"
            tabIndex={0}
          >
            <DomainsTable
              domains={review.domains}
              onCorrect={(domain) => setStatusTarget({ kind: 'domain', id: domain.domain_id, name: domain.domain_name, status: domain.status })}
            />
          </div>
        </section>
      </div>

      {/* 变更记录与用户体验 */}
      {timelineState.loading ? (
        <section className="page-panel archive-detail-card">
          <p className="empty-inline">正在加载资产变更记录与用户体验…</p>
        </section>
      ) : timelineState.error ? (
        <section className="page-panel archive-detail-card">
          <div className="archive-detail-local-error" role="alert">
            <p>变更历史加载失败：{timelineState.error}</p>
            <button
              className="btn sm secondary"
              type="button"
              onClick={handleRetryTimeline}
            >
              重试加载变更历史
            </button>
          </div>
        </section>
      ) : timelineState.data ? (
        <>
          <UserRecordsSection records={sortedExperienceLogs} />
          <section className="page-panel archive-detail-card" role="region" aria-label="续费、价格、规格与 IP 历史">
            <div className="section-heading">
              <div>
                <h2>续费、价格、规格与 IP 历史</h2>
                <p className="section-heading__description">辅助判断材料，保留为归档 VPS 的事实变化证据。</p>
              </div>
            </div>
            <div className="archive-detail-history-grid">
              <HistoryGroup title="续费决策" count={sortedDecisions.length}>
                <HistoryList empty="暂无续费决策历史">
                  {sortedDecisions.map((record) => (
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
              <HistoryGroup title="价格历史" count={sortedPriceHistories.length}>
                <HistoryList empty="暂无价格历史">
                  {sortedPriceHistories.map((record) => renderPriceHistory(record))}
                </HistoryList>
              </HistoryGroup>
              <HistoryGroup title="规格快照" count={sortedSpecSnapshots.length}>
                <HistoryList empty="暂无规格快照">
                  {sortedSpecSnapshots.map((record) => renderSpecSnapshot(record))}
                </HistoryList>
              </HistoryGroup>
              <HistoryGroup title="IP 历史" count={sortedIPHistories.length}>
                <HistoryList empty="暂无 IP 历史">
                  {sortedIPHistories.map((record) => renderIPHistory(record))}
                </HistoryList>
              </HistoryGroup>
            </div>
          </section>
        </>
      ) : null}

      {/* 恢复确认弹窗 */}
      <Modal
        open={restoreOpen}
        onClose={() => {
          if (restoreSubmittingRef.current) return
          setRestoreOpen(false)
          restoreTriggerRef.current?.focus()
        }}
        title="确认恢复归档 VPS"
        dialogRole="alertdialog"
        size="md"
        persistent={restoreSubmitting}
      >
        <div className="asset-lifecycle-confirm">
          <p className="asset-lifecycle-confirm__eyebrow">恢复</p>
          <h4>恢复后进入闲置状态，关联订阅、监控、服务、域名和历史记录会保留。</h4>
          <p className="asset-lifecycle-confirm__callouts">恢复不会启用监控，也不会改写旧关联。成功后回到当前详情整理恢复记录。</p>
          <label className="input-field">
            <span className="input-field__label">恢复原因</span>
            <input className="input" aria-label="恢复原因" value={restoreReason} onChange={(event) => setRestoreReason(event.target.value)} />
          </label>
          {restoreError ? (
            <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
              {restoreError}
            </p>
          ) : null}
          <div className="page-form-actions">
            <button
              className="btn md secondary"
              type="button"
              disabled={restoreSubmitting}
              onClick={() => {
                if (restoreSubmittingRef.current) return
                setRestoreOpen(false)
                restoreTriggerRef.current?.focus()
              }}
            >
              取消
            </button>
            <button
              className="btn md primary"
              type="button"
              disabled={restoreSubmitting}
              onClick={() => void handleRestore()}
            >
              {restoreSubmitting ? '恢复中…' : '确认恢复'}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}

import { VPSArchivedAmendment } from './vps-detail/VPSArchivedAmendment'
import { VPSCopyValueButton } from './vps-detail/VPSCopyValueButton'
import { VPSLifecycleWorkspace } from './vps-detail/VPSLifecycleWorkspace'
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'

import { Modal, MonoDigits, TabPanel, Tabs } from '../components/atoms'
import { DependencyStatusCorrection } from '../components/DependencyStatusCorrection'
import { PageState as PageStateView } from '../components/PageState'
import {
  ApiError,
  getVPSArchiveReview,
  getVPSTimeline,
  listSubscriptions,
  restoreVPSFromArchive,
} from '../lib/api'
import { VPS_AUTO_RENEW_CHECK_LABELS, type ArchiveReview, type SubscriptionRecord, type VPSTimeline } from '../lib/types'
import { formatDateTime, formatOptional } from '../lib/format'
import { LifecycleBadge } from './assetPageBadges'
import { renewalLabel, vpsLocationLabel } from './assetPageUtils'
import {
  archiveClosingSubscriptions,
  archiveDayLabel,
  archiveServiceSpanLabel,
  archiveTimelineEntries,
  subscriptionMonthlySummary,
} from './archive/archivePageHelpers'
import {
  ArchiveEmpty,
  ArchiveSubsection,
  ArchiveTimeline,
  ArchiveUserRecords,
  DomainsTable,
  MonitoringHistoryTable,
  ServicesTable,
  SubscriptionTable,
  TargetHistoryTable,
} from './archive/ArchiveReviewPanels'

type AsyncState<T> = {
  loading: boolean
  error: string | null
  data: T
}

type ArchiveTab = 'records' | 'billing' | 'assets' | 'monitoring' | 'timeline'

const ARCHIVE_TABS_ID = 'archive-review'

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function parseEventTime(t?: string | null): number {
  if (!t) return -Infinity
  const parsed = Date.parse(t)
  return Number.isNaN(parsed) ? -Infinity : parsed
}

function SummaryItem({ label, value, sub, tone }: { label: string; value: ReactNode; sub?: ReactNode | undefined; tone?: 'notice' | 'alert' | undefined }) {
  return (
    <div className={`archive-summary__item${tone ? ` archive-summary__item--${tone}` : ''}`}>
      <dt>{label}</dt>
      <dd className="archive-summary__value">{value}</dd>
      {sub ? <dd className="archive-summary__sub">{sub}</dd> : null}
    </div>
  )
}

function FactRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="archive-facts__row">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
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
  const [archiveNotice, setArchiveNotice] = useState<string | null>(null)
  const [statusTarget, setStatusTarget] = useState<{ kind: 'service' | 'domain'; id: string; name: string; status: string } | null>(null)
  const restoreTriggerRef = useRef<HTMLButtonElement>(null)
  const restoreSubmittingRef = useRef(false)
  const [tab, setTab] = useState<ArchiveTab>('records')

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
        if (lifecycleStatus !== 'archived') {
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

  const review: ArchiveReview = reviewState.data
  const vps = review.vps
  const isArchived = vps.lifecycle_status === 'archived'

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
  const earliestStart = effectiveSubscriptions
    .map((subscription) => subscription.started_at)
    .filter((value) => parseEventTime(value) > -Infinity)
    .sort((a, b) => parseEventTime(a) - parseEventTime(b))[0]

  const timeline: VPSTimeline | null = timelineState.data
  const sortedExperienceLogs = timeline
    ? [...timeline.experience_logs].sort((a, b) => (
        parseEventTime(b.occurred_at || b.created_at) - parseEventTime(a.occurred_at || a.created_at)
      ))
    : []
  const latestDecision = timeline
    ? [...timeline.renewal_decisions].sort((a, b) => (
        parseEventTime(b.decided_at || b.created_at) - parseEventTime(a.decided_at || a.created_at)
      ))[0]
    : undefined
  const timelineEntries = timeline ? archiveTimelineEntries(timeline) : []

  const serviceStart = earliestStart ?? vps.created_at
  const serviceEnd = vps.archived_at ?? vps.updated_at
  const serviceSpan = archiveServiceSpanLabel(serviceStart, serviceEnd)
  const autoRenewCheck = vps.auto_renew_check ?? 'unchecked'
  const autoRenewRisk = autoRenewCheck === 'unchecked' || autoRenewCheck === 'enabled'
  const monthlyCost = subscriptionsState.loading && effectiveSubscriptions.length === 0
    ? '加载中…'
    : subscriptionsState.error && effectiveSubscriptions.length === 0
      ? '加载失败'
      : subscriptionMonthlySummary(archiveClosingSubscriptions(effectiveSubscriptions, vps.archived_at), '无订阅记录')

  const sshUser = vps.ssh_user ? `${vps.ssh_user}@` : ''
  const sshPort = vps.ssh_port ? ` -p ${vps.ssh_port}` : ''
  const sshCommand = vps.ssh_host ? `ssh ${sshUser}${vps.ssh_host}${sshPort}` : ''
  const locationLabel = `${vpsLocationLabel(vps)}${vps.datacenter ? ` · ${vps.datacenter}` : ''}`
  const system = [vps.os_name, vps.virtualization].filter(Boolean).join(' · ')

  const timelineStatus = timelineState.loading ? (
    <p className="empty-inline" role="status">正在加载用户记录与变更历史…</p>
  ) : timelineState.error ? (
    <div className="archive-detail-local-error" role="alert">
      <p>变更历史加载失败：{timelineState.error}</p>
      <button className="btn sm secondary" type="button" onClick={handleRetryTimeline}>
        重试加载变更历史
      </button>
    </div>
  ) : null

  const tabs = [
    { value: 'records' as const, label: '用户记录', count: sortedExperienceLogs.length },
    { value: 'billing' as const, label: '账单与订阅', count: effectiveSubscriptions.length },
    { value: 'assets' as const, label: '服务与域名', count: review.services.length + review.domains.length },
    { value: 'monitoring' as const, label: '监控与探测', count: review.monitoring_instance_links.length + review.target_links.length },
    { value: 'timeline' as const, label: '变更时间线', count: timelineEntries.length },
  ]

  return (
    <div className="page archive-detail-page">
      <header className="page__head archive-hero" role="banner" aria-label="归档 VPS 身份">
        <div className="archive-hero__lead">
          <div className="archive-hero__eyebrow">
            <LifecycleBadge value={vps.lifecycle_status} />
            {vps.archived_at ? (
              <span>归档于 <MonoDigits>{formatDateTime(vps.archived_at)}</MonoDigits></span>
            ) : (
              <span className="text-muted">未记录归档时间</span>
            )}
          </div>
          <h1 className="page__title">
            <span>{vps.display_name}</span>
            <small className="archive-hero__id mono-text">{vps.vps_id}</small>
          </h1>
          <p className="archive-hero__meta">
            {[formatOptional(vps.provider_name), locationLabel, vps.product_name].filter(Boolean).join(' · ')}
          </p>
        </div>
        <div className="page__actions">
          <Link className="btn md secondary" to="/archive">归档列表</Link>
          {isArchived ? (
            <button
              ref={restoreTriggerRef}
              className="btn md primary"
              type="button"
              onClick={() => {
                setRestoreError(null)
                setRestoreOpen(true)
              }}
            >
              恢复管理
            </button>
          ) : null}
        </div>
      </header>

      {archiveNotice ? <p className="archive-detail-status" role="status">{archiveNotice}</p> : null}

      <dl className="archive-summary" aria-label="归档摘要">
        <SummaryItem
          label="服役时长"
          value={serviceSpan ?? '—'}
          sub={<MonoDigits>{archiveDayLabel(serviceStart)} → {archiveDayLabel(serviceEnd)}</MonoDigits>}
        />
        <SummaryItem
          label="末期月费"
          value={monthlyCost}
          tone={subscriptionsState.error && effectiveSubscriptions.length === 0 ? 'alert' : undefined}
          sub={effectiveSubscriptions.length > 0 ? (
            <>
              <MonoDigits>{effectiveSubscriptions.length}</MonoDigits> 笔订阅
              {latestSubscription?.renew_at ? <> · 末次到期 <MonoDigits>{archiveDayLabel(latestSubscription.renew_at)}</MonoDigits></> : null}
            </>
          ) : undefined}
        />
        <SummaryItem
          label="续费决策"
          value={renewalLabel(vps.renewal_decision)}
          sub={latestDecision?.reason || undefined}
        />
        <SummaryItem
          label="服务商自动续费"
          value={VPS_AUTO_RENEW_CHECK_LABELS[autoRenewCheck]}
          tone={autoRenewRisk ? 'notice' : undefined}
          sub={autoRenewRisk ? '可能仍在扣费' : vps.auto_renew_checked_at ? <>核对于 <MonoDigits>{archiveDayLabel(vps.auto_renew_checked_at)}</MonoDigits></> : undefined}
        />
        <SummaryItem
          label="留存资产"
          value={<><MonoDigits>{review.services.length}</MonoDigits> 服务 · <MonoDigits>{review.domains.length}</MonoDigits> 域名</>}
          sub={<><MonoDigits>{review.monitoring_instance_links.length}</MonoDigits> 监控 · <MonoDigits>{review.target_links.length}</MonoDigits> 入口探测</>}
        />
      </dl>

      <div className="archive-layout">
        <section className="page-panel archive-review" aria-label="归档回看">
          <Tabs label="归档回看" idBase={ARCHIVE_TABS_ID} items={tabs} value={tab} onChange={setTab} />

          {tab === 'records' ? (
            <TabPanel idBase={ARCHIVE_TABS_ID} value="records" className="archive-review__panel">
              <div role="region" aria-label="用户记录" className="archive-review__region">
                <p className="archive-review__caption">自身使用体验、感受和问题判断，是再次选择服务商时最有价值的回看材料。</p>
                {timelineStatus ?? <ArchiveUserRecords records={sortedExperienceLogs} />}
              </div>
            </TabPanel>
          ) : null}

          {tab === 'billing' ? (
            <TabPanel idBase={ARCHIVE_TABS_ID} value="billing" className="archive-review__panel">
              {subscriptionsState.loading && effectiveSubscriptions.length === 0 ? (
                <p className="empty-inline" role="status">正在加载历史订阅…</p>
              ) : subscriptionsState.error && effectiveSubscriptions.length === 0 ? (
                <div className="archive-detail-local-error" role="alert">
                  <p>历史订阅加载失败：{subscriptionsState.error}</p>
                  <button type="button" className="btn sm secondary" onClick={handleRetrySubscriptions}>
                    重试加载订阅
                  </button>
                </div>
              ) : (
                <>
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
                  {isUsingSnapshotFallback ? <p className="archive-review__caption">以下账单来自本次归档审查数据。</p> : null}
                  <div className="page-panel--scroll-x" role="region" aria-label="订阅明细" tabIndex={0}>
                    {effectiveSubscriptions.length === 0 ? (
                      <ArchiveEmpty title="暂无历史订阅" hint="如仍有账单或退款需要留档，可在右侧「补录账单」。" />
                    ) : (
                      <SubscriptionTable subscriptions={sortedSubscriptions} />
                    )}
                  </div>
                </>
              )}
            </TabPanel>
          ) : null}

          {tab === 'assets' ? (
            <TabPanel idBase={ARCHIVE_TABS_ID} value="assets" className="archive-review__panel">
              <ArchiveSubsection title="服务资产" count={review.services.length}>
                {review.services.length === 0 ? <ArchiveEmpty title="暂无服务记录" /> : (
                  <div className="page-panel--scroll-x" role="region" aria-label="服务资产" tabIndex={0}>
                    <ServicesTable
                      services={review.services}
                      onCorrect={(service) => setStatusTarget({ kind: 'service', id: service.service_id, name: service.name, status: service.status })}
                    />
                  </div>
                )}
              </ArchiveSubsection>
              <ArchiveSubsection title="域名资产" count={review.domains.length}>
                {review.domains.length === 0 ? <ArchiveEmpty title="暂无域名记录" /> : (
                  <div className="page-panel--scroll-x" role="region" aria-label="域名资产" tabIndex={0}>
                    <DomainsTable
                      domains={review.domains}
                      onCorrect={(domain) => setStatusTarget({ kind: 'domain', id: domain.domain_id, name: domain.domain_name, status: domain.status })}
                    />
                  </div>
                )}
              </ArchiveSubsection>
              <div className="archive-review__split">
                <ArchiveSubsection title="服务关联历史">
                  <VPSLifecycleWorkspace vpsId={vps.vps_id} archived kind="service" />
                </ArchiveSubsection>
                <ArchiveSubsection title="域名关联历史">
                  <VPSLifecycleWorkspace vpsId={vps.vps_id} archived kind="domain" />
                </ArchiveSubsection>
              </div>
              <p className="archive-review__caption">对象身份独立于 VPS；结束关联保留历史快照，共享对象及其他 VPS 的关联继续存在。</p>
            </TabPanel>
          ) : null}

          {tab === 'monitoring' ? (
            <TabPanel idBase={ARCHIVE_TABS_ID} value="monitoring" className="archive-review__panel">
              <ArchiveSubsection title="监控历史" count={review.monitoring_instance_links.length}>
                {review.monitoring_instance_links.length === 0 ? (
                  <ArchiveEmpty title="暂无监控关联历史" hint="这台 VPS 归档前没有接入 Agent 监控。" />
                ) : (
                  <div className="page-panel--scroll-x" role="region" aria-label="监控历史" tabIndex={0}>
                    <MonitoringHistoryTable vpsId={vps.vps_id} links={review.monitoring_instance_links} />
                  </div>
                )}
              </ArchiveSubsection>
              <ArchiveSubsection title="入口探测历史" count={review.target_links.length}>
                {review.target_links.length === 0 ? <ArchiveEmpty title="暂无入口探测关联历史" /> : (
                  <div className="page-panel--scroll-x" role="region" aria-label="入口探测历史" tabIndex={0}>
                    <TargetHistoryTable targets={review.target_links} />
                  </div>
                )}
              </ArchiveSubsection>
              <p className="archive-review__caption">历史监控保持退役，仅用于服务商质量回看；恢复管理后需显式重新接入。</p>
            </TabPanel>
          ) : null}

          {tab === 'timeline' ? (
            <TabPanel idBase={ARCHIVE_TABS_ID} value="timeline" className="archive-review__panel">
              <div role="region" aria-label="变更时间线" className="archive-review__region">
                {timelineStatus ?? <ArchiveTimeline entries={timelineEntries} />}
              </div>
            </TabPanel>
          ) : null}
        </section>

        <aside className="archive-aside" aria-label="归档后处理与访问事实">
          <section className="page-panel archive-aside__card archive-aside__card--todo">
            <header className="archive-aside__head">
              <h2>归档后待办</h2>
              <p>核对扣费、补充账单与迁移结果，每次保存都保留修订记录。</p>
            </header>
            <VPSArchivedAmendment vps={vps} onChanged={() => fetchReview(vps.vps_id, ++reviewGenRef.current)} />
            <div className="archive-aside__divider" />
            <h3 className="archive-aside__subhead">跟进事项</h3>
            <VPSLifecycleWorkspace vpsId={vps.vps_id} archived kind="followups" />
          </section>

          <section className="page-panel archive-aside__card">
            <header className="archive-aside__head">
              <h2>访问与规格</h2>
            </header>
            <dl className="archive-facts">
              <FactRow label="服务商">{formatOptional(vps.provider_name)}</FactRow>
              <FactRow label="产品">{formatOptional(vps.product_name)}</FactRow>
              <FactRow label="位置">{locationLabel}</FactRow>
              {system ? <FactRow label="系统">{system}</FactRow> : null}
              <FactRow label="IPv4"><MonoDigits>{formatOptional(vps.ipv4)}</MonoDigits></FactRow>
              {vps.ipv6 ? <FactRow label="IPv6"><MonoDigits>{vps.ipv6}</MonoDigits></FactRow> : null}
              <FactRow label="SSH">
                {sshCommand ? (
                  <span className="archive-facts__copy">
                    <MonoDigits>{sshCommand}</MonoDigits>
                    <VPSCopyValueButton value={sshCommand} label="SSH 命令" />
                  </span>
                ) : '—'}
              </FactRow>
              {vps.usage_tags?.length ? <FactRow label="用途">{vps.usage_tags.join('、')}</FactRow> : null}
              {vps.note ? <FactRow label="备注">{vps.note}</FactRow> : null}
            </dl>
          </section>

          <p className="archive-aside__footnote">
            已归档资产不进入当前工作集。恢复管理后用途为闲置，历史监控保持退役，需显式重新接入。
          </p>
        </aside>
      </div>

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

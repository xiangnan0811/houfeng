import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { AssetDecisionRenewalTable } from '../components/AssetDecisionRenewalTable'
import {
  AssetDecisionWorkPanel,
  type AssetDecisionDraft,
} from '../components/AssetDecisionWorkPanel'
import { Modal, MonoDigits, Tabs } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import { ApiError, listSubscriptions, listVPSAssets, updateVPSAsset } from '../lib/api'
import { formatMoney, formatOptional } from '../lib/format'
import {
  type VPSAssetRecord,
  type VPSRenewalDecision,
  type SubscriptionRecord,
} from '../lib/types'
import { RenewalBadge } from './assetPageBadges'
import {
  buildVPSQualityIssues,
  daysUntilDate,
  groupSubscriptionsByVPS,
  isSubscriptionInRenewalWindow,
  renewalLabel,
  selectPrimarySubscription,
  vpsLocationLabel,
  type AssetQualityIssue,
} from './assetPageUtils'

type RenewalWindow = 30 | 60 | 90
type DecisionQueueView =
  | 'all'
  | 'unreviewed'
  | 'renewal'
  | 'migrate'
  | 'cancel'
  | 'unlinked'
  | 'missing_subscription'

type DecisionQueueItem = {
  vps: VPSAssetRecord
  subscription: SubscriptionRecord | null
  qualityIssues: AssetQualityIssue[]
  renewalDue: boolean
  priority: number
}

type PageState = {
  renewalsLoading: boolean
  renewalsError: string | null
  vpsLoading: boolean
  vpsError: string | null
  renewals: SubscriptionRecord[]
  subscriptions: SubscriptionRecord[]
  unreviewed: VPSAssetRecord[]
  migrate: VPSAssetRecord[]
  cancel: VPSAssetRecord[]
}

const RENEWAL_WINDOWS: RenewalWindow[] = [30, 60, 90]
const DECISION_QUEUE_VALUES: VPSRenewalDecision[] = ['unreviewed', 'migrate', 'cancel']
const INITIAL_DECISION_DRAFT: AssetDecisionDraft = {
  renewalDecision: 'unreviewed',
  reason: '',
}
const INITIAL_PAGE_STATE: PageState = {
  renewalsLoading: true,
  renewalsError: null,
  vpsLoading: true,
  vpsError: null,
  renewals: [],
  subscriptions: [],
  unreviewed: [],
  migrate: [],
  cancel: [],
}

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function parseRenewalWindow(value: string): RenewalWindow {
  const parsed = Number.parseInt(value, 10)
  return RENEWAL_WINDOWS.includes(parsed as RenewalWindow) ? (parsed as RenewalWindow) : 30
}

function updateDecisionQueues(
  state: PageState,
  updated: VPSAssetRecord,
): Pick<PageState, 'unreviewed' | 'migrate' | 'cancel'> {
  const next = {
    unreviewed: state.unreviewed.filter((vps) => vps.vps_id !== updated.vps_id),
    migrate: state.migrate.filter((vps) => vps.vps_id !== updated.vps_id),
    cancel: state.cancel.filter((vps) => vps.vps_id !== updated.vps_id),
  }

  if (updated.renewal_decision === 'unreviewed') next.unreviewed = [updated, ...next.unreviewed]
  if (updated.renewal_decision === 'migrate') next.migrate = [updated, ...next.migrate]
  if (updated.renewal_decision === 'cancel') next.cancel = [updated, ...next.cancel]

  return next
}

function renewalQueueLabel(value: VPSRenewalDecision): string {
  return DECISION_QUEUE_VALUES.includes(value) ? renewalLabel(value) : '已处理'
}
function buildDecisionQueue(
  vpsRows: VPSAssetRecord[],
  subscriptionsByVPS: Map<string, SubscriptionRecord[]>,
  renewalWindow: RenewalWindow,
): DecisionQueueItem[] {
  const uniqueRows = new Map<string, VPSAssetRecord>()
  for (const vps of vpsRows) uniqueRows.set(vps.vps_id, vps)

  return [...uniqueRows.values()]
    .map((vps) => {
      const subscription = selectPrimarySubscription(subscriptionsByVPS, vps.vps_id)
      const qualityIssues = buildVPSQualityIssues(vps, subscription)
      const renewalDue = isSubscriptionInRenewalWindow(subscription, renewalWindow)
      return {
        vps,
        subscription,
        qualityIssues,
        renewalDue,
        priority: queuePriority(vps, subscription, qualityIssues, renewalDue),
      }
    })
    .sort((left, right) => {
      if (left.priority !== right.priority) return right.priority - left.priority
      const leftDays = daysUntilDate(left.subscription?.renew_at) ?? Number.POSITIVE_INFINITY
      const rightDays = daysUntilDate(right.subscription?.renew_at) ?? Number.POSITIVE_INFINITY
      if (leftDays !== rightDays) return leftDays - rightDays
      return left.vps.display_name.localeCompare(right.vps.display_name)
    })
}

function queuePriority(
  vps: VPSAssetRecord,
  subscription: SubscriptionRecord | null,
  qualityIssues: AssetQualityIssue[],
  renewalDue: boolean,
): number {
  let priority = 0
  if (vps.renewal_decision === 'unreviewed') priority += 500
  if (renewalDue) priority += 300
  if (vps.renewal_decision === 'migrate' || vps.renewal_decision === 'cancel') priority += 180
  if (vps.active_node_link_count <= 0) priority += 90
  if (!subscription) priority += 80
  return priority + qualityIssues.length * 8
}

function filterDecisionQueue(
  rows: DecisionQueueItem[],
  view: DecisionQueueView,
): DecisionQueueItem[] {
  if (view === 'all') return rows
  if (view === 'renewal') return rows.filter((row) => row.renewalDue)
  if (view === 'unlinked') return rows.filter((row) => row.vps.active_node_link_count <= 0)
  if (view === 'missing_subscription') return rows.filter((row) => !row.subscription)
  return rows.filter((row) => row.vps.renewal_decision === view)
}
export function AssetDecisionsPage() {
  const navigate = useNavigate()
  const [renewalWindow, setRenewalWindow] = useState<RenewalWindow>(30)
  const [queueView, setQueueView] = useState<DecisionQueueView>('all')
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [selectedVPS, setSelectedVPS] = useState<VPSAssetRecord | null>(null)
  const [decisionDraft, setDecisionDraft] = useState<AssetDecisionDraft>(INITIAL_DECISION_DRAFT)
  const [decisionSubmitting, setDecisionSubmitting] = useState(false)
  const [decisionError, setDecisionError] = useState<string | null>(null)
  const [decisionNotice, setDecisionNotice] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    listSubscriptions({
      renew_within_days: renewalWindow,
      sort: 'renew_at',
      order: 'asc',
    })
      .then((renewals) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          renewalsLoading: false,
          renewalsError: null,
          renewals,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          renewalsLoading: false,
          renewalsError: describeError(error, '加载续费候选失败'),
          renewals: [],
        }))
      })
    return () => { cancelled = true }
  }, [renewalWindow])

  useEffect(() => {
    let cancelled = false
    Promise.all([
      listSubscriptions({ sort: 'renew_at', order: 'asc' }),
      listVPSAssets({ renewal_decision: 'unreviewed' }),
      listVPSAssets({ renewal_decision: 'migrate' }),
      listVPSAssets({ renewal_decision: 'cancel' }),
    ])
      .then(([subscriptions, unreviewed, migrate, cancel]) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          vpsLoading: false,
          vpsError: null,
          subscriptions,
          unreviewed,
          migrate,
          cancel,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          vpsLoading: false,
          vpsError: describeError(error, '加载 VPS 决策队列失败'),
          subscriptions: [],
          unreviewed: [],
          migrate: [],
          cancel: [],
        }))
      })
    return () => { cancelled = true }
  }, [])
  const subscriptionsByVPS = useMemo(
    () => groupSubscriptionsByVPS(state.subscriptions),
    [state.subscriptions],
  )
  const vpsByID = useMemo(() => {
    const rows = new Map<string, VPSAssetRecord>()
    for (const vps of [...state.unreviewed, ...state.migrate, ...state.cancel]) rows.set(vps.vps_id, vps)
    return rows
  }, [state.cancel, state.migrate, state.unreviewed])
  const decisionQueue = useMemo(
    () =>
      buildDecisionQueue(
        [...state.unreviewed, ...state.migrate, ...state.cancel],
        subscriptionsByVPS,
        renewalWindow,
      ),
    [state.cancel, state.migrate, state.unreviewed, subscriptionsByVPS, renewalWindow],
  )
  const visibleDecisionQueue = useMemo(
    () => filterDecisionQueue(decisionQueue, queueView),
    [decisionQueue, queueView],
  )
  const renewalDueQueueCount = decisionQueue.filter((item) => item.renewalDue).length
  const missingSubscriptionCount = decisionQueue.filter((item) => !item.subscription).length
  const unlinkedCount = decisionQueue.filter((item) => item.vps.active_node_link_count <= 0).length
  const priorityDecisionCount = decisionQueue.filter(
    (item) => item.renewalDue && item.vps.renewal_decision === 'unreviewed',
  ).length
  const totalDecisionQueue = decisionQueue.length

  const queueTabs = [
    { value: 'all', label: '全部', count: totalDecisionQueue },
    { value: 'unreviewed', label: '待评估', count: state.unreviewed.length },
    { value: 'renewal', label: `${renewalWindow}天续费`, count: renewalDueQueueCount },
    { value: 'migrate', label: '迁移', count: state.migrate.length },
    { value: 'cancel', label: '取消', count: state.cancel.length },
    { value: 'unlinked', label: '未关联', count: unlinkedCount },
    { value: 'missing_subscription', label: '缺订阅', count: missingSubscriptionCount },
  ] satisfies Array<{ value: DecisionQueueView; label: string; count: number }>
  function selectVPS(vps: VPSAssetRecord) {
    setSelectedVPS(vps)
    setDecisionDraft({ renewalDecision: vps.renewal_decision, reason: '' })
    setDecisionError(null)
    setDecisionNotice(null)
  }

  function navigateToVPS(vps: VPSAssetRecord) {
    navigate(`/vps/${vps.vps_id}`)
  }

  function closeDecisionDrawer() {
    setSelectedVPS(null)
    setDecisionDraft(INITIAL_DECISION_DRAFT)
    setDecisionError(null)
  }

  function changeRenewalWindow(value: string) {
    setState((current) => ({ ...current, renewalsLoading: true, renewalsError: null }))
    setRenewalWindow(parseRenewalWindow(value))
  }

  function handleDecisionSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selectedVPS) return
    setDecisionError(null)
    setDecisionNotice(null)

    if (decisionDraft.renewalDecision === selectedVPS.renewal_decision) {
      setDecisionError('请选择一个不同的续费决策')
      return
    }

    const reason = decisionDraft.reason.trim()
    setDecisionSubmitting(true)
    updateVPSAsset(selectedVPS.vps_id, {
      renewal_decision: decisionDraft.renewalDecision,
      ...(reason ? { renewal_reason: reason } : {}),
    })
      .then((updated) => {
        setState((current) => ({
          ...current,
          ...updateDecisionQueues(current, updated),
          subscriptions: current.subscriptions.map((subscription) =>
            updated.renewal_subscription_linkage?.updated && subscription.subscription_id === updated.renewal_subscription_linkage.subscription_id
              ? { ...subscription, auto_renew: false, auto_renew_cancelled: true }
              : subscription,
          ),
          renewals: current.renewals.map((subscription) =>
            updated.renewal_subscription_linkage?.updated && subscription.subscription_id === updated.renewal_subscription_linkage.subscription_id
              ? { ...subscription, auto_renew: false, auto_renew_cancelled: true }
              : subscription,
          ),
        }))
        closeDecisionDrawer()
        const baseNotice = `续费决策已保存：${updated.display_name} -> ${renewalQueueLabel(updated.renewal_decision)}`
        const linkageMessage = updated.renewal_subscription_linkage?.message
        setDecisionNotice(linkageMessage ? `${baseNotice}。${linkageMessage}` : baseNotice)
      })
      .catch((error: unknown) => {
        setDecisionError(describeError(error, '更新续费决策失败'))
      })
      .finally(() => setDecisionSubmitting(false))
  }
  return (
    <div className="animate-in">
      <div className="page-header">
        <div>
          <h1 className="page-title">资产决策</h1>
          <p className="page-sub">续费窗口与待评估资产</p>
        </div>
        <div className="header-actions">
          <Link className="btn md secondary" to="/vps">VPS 库存</Link>
          <Link className="btn md secondary" to="/subscriptions">订阅列表</Link>
        </div>
      </div>

      {decisionNotice && (
        <div className="inline-alert ok" role="status">{decisionNotice}</div>
      )}

      <div className="section-grid animate-in d1">
        <div className="card">
          <div className="section-title">
            优先处理{' '}
            <span className={`section-count${priorityDecisionCount > 0 ? ' section-count--warn' : ''}`}>
              {priorityDecisionCount}
            </span>
          </div>
          <p className="text-sm text-muted">
            {renewalWindow} 天续费窗口 + 未评估
          </p>
        </div>
        <div className="card">
          <div className="section-title">
            缺口{' '}
            <span className="section-count">
              缺订阅 {missingSubscriptionCount} / 未关联 {unlinkedCount}
            </span>
          </div>
          <p className="text-sm text-muted">
            迁移 {state.migrate.length} / 取消 {state.cancel.length}
          </p>
        </div>
      </div>

      <div className="card animate-in d2 mb-5">
        <div className="section-title flex-row gap-2">
          续费窗口{' '}
          <select
            className="input filter-select--inline"
            value={String(renewalWindow)}
            onChange={(event) => changeRenewalWindow(event.target.value)}
          >
            {RENEWAL_WINDOWS.map((value) => (
              <option key={value} value={value}>未来 {value} 天</option>
            ))}
          </select>
          <span className={`section-count${state.renewals.length > 0 ? ' section-count--warn' : ''}`}>
            {state.renewalsLoading ? '...' : state.renewalsError ? '不可用' : `${state.renewals.length} 条`}
          </span>
        </div>
        <AssetDecisionRenewalTable
          loading={state.renewalsLoading}
          error={state.renewalsError}
          renewals={state.renewals}
          vpsByID={vpsByID}
          renderVPSReference={(subscription, vps) => (
            <Link className="name" to={`/vps/${subscription.vps_id}`}>
              {vps?.display_name ?? subscription.vps_id}
            </Link>
          )}
          renderActions={(subscription) => (
            <>
              <Link className="btn-text sm secondary" to={`/vps/${subscription.vps_id}`}>VPS</Link>
              <Link className="btn-text sm secondary" to={`/subscriptions?vps_id=${subscription.vps_id}&renew_within_days=${renewalWindow}`}>订阅</Link>
            </>
          )}
        />
      </div>
      <div className="card animate-in d3">
        <div className="section-title">
          决策队列{' '}
          <span className="section-count">
            {state.vpsLoading ? '...' : `${visibleDecisionQueue.length} / ${totalDecisionQueue}`}
          </span>
        </div>
        <div className="mb-3">
          <Tabs items={queueTabs} value={queueView} onChange={setQueueView} variant="pill" />
        </div>
        {state.vpsLoading ? (
          <PageStateView
            kind="loading"
            title="正在加载决策队列…"
            surface="empty"
            compact
          />
        ) : state.vpsError ? (
          <PageStateView
            kind="error"
            title="决策队列不可用"
            description={<>{state.vpsError}</>}
            technicalSummary={state.vpsError}
            surface="empty"
            compact
          />
        ) : visibleDecisionQueue.length === 0 ? (
          <PageStateView
            kind="empty"
            title="当前视图暂无待处理 VPS"
            description="可回到全部、库存或订阅。"
            action={
              <div className="flex-row gap-2">
                {queueView !== 'all' && (
                  <button className="btn sm secondary" onClick={() => setQueueView('all')}>查看全部</button>
                )}
                <Link className="btn sm ghost" to="/vps">VPS 库存</Link>
                <Link className="btn sm ghost" to="/subscriptions">补充订阅</Link>
              </div>
            }
            surface="empty"
            compact
          />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>VPS</th>
                <th>供应商</th>
                <th>决策</th>
                <th>订阅</th>
                <th>Node</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {visibleDecisionQueue.map((item) => {
                const vps = item.vps
                const sub = item.subscription
                const daysLeft = sub ? daysUntilDate(sub.renew_at) : null
                const isUrgent = item.renewalDue && vps.renewal_decision === 'unreviewed'
                return (
                  <tr
                    key={vps.vps_id}
                    className={isUrgent ? 'row-urgent row-clickable' : 'row-clickable'}
                    onClick={() => navigateToVPS(vps)}
                  >
                    <td className="name">{vps.display_name}</td>
                    <td className="text-sm text-secondary">
                      {formatOptional(vps.provider_name)}{' '}
                      {vpsLocationLabel(vps)}
                    </td>
                    <td><RenewalBadge value={vps.renewal_decision} /></td>
                    <td>
                      {sub ? (
                        <span className="mono">
                          {formatMoney(sub.monthly_price, sub.currency)}/月
                          {daysLeft != null && (
                            <span className={daysLeft <= 30 ? 'days-urgent' : 'days-normal'}>
                              {daysLeft}天
                            </span>
                          )}
                        </span>
                      ) : (
                        <span className="badge badge-warn">缺订阅</span>
                      )}
                    </td>
                    <td>
                      {vps.active_node_link_count > 0 ? (
                        <span><MonoDigits>{vps.active_node_link_count}</MonoDigits> 关联</span>
                      ) : (
                        <span className="text-muted">未关联</span>
                      )}
                    </td>
                    <td>
                      <button
                        className="btn sm primary"
                        onClick={(e) => { e.stopPropagation(); selectVPS(vps) }}
                      >
                        处理
                      </button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>

      <Modal
        open={selectedVPS != null}
        onClose={closeDecisionDrawer}
        title={selectedVPS ? `处理 ${selectedVPS.display_name}` : '处理续费决策'}
        ariaLabel="续费决策处理"
      >
        <AssetDecisionWorkPanel
          surface="drawer"
          selectedVPS={selectedVPS}
          decisionDraft={decisionDraft}
          submitting={decisionSubmitting}
          error={decisionError}
          notice={null}
          onDraftChange={setDecisionDraft}
          onSubmit={handleDecisionSubmit}
          onCancel={closeDecisionDrawer}
        />
      </Modal>
    </div>
  )
}

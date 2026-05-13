import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { AssetDecisionRenewalTable } from '../components/AssetDecisionRenewalTable'
import {
  AssetDecisionWorkPanel,
  type AssetDecisionDraft,
} from '../components/AssetDecisionWorkPanel'
import { Badge, Button, Drawer, MonoDigits, Tabs } from '../components/atoms'
import { ApiError, listSubscriptions, listVPSAssets, updateVPSAsset } from '../lib/api'
import { formatDate, formatMoney, formatOptional } from '../lib/format'
import {
  type VPSAssetRecord,
  type VPSRenewalDecision,
  type SubscriptionRecord,
} from '../lib/types'
import { LifecycleBadge, RenewalBadge, UsageBadge } from './assetPageBadges'
import {
  buildVPSQualityIssues,
  daysUntilDate,
  groupSubscriptionsByVPS,
  isSubscriptionInRenewalWindow,
  renewalLabel,
  renewalTimingLabel,
  selectPrimarySubscription,
  vpsAccessLabel,
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

function queueRowClass(item: DecisionQueueItem): string {
  if (!item.subscription || (item.renewalDue && item.vps.renewal_decision === 'unreviewed')) {
    return 'asset-decision-row--critical'
  }
  if (
    item.renewalDue ||
    item.vps.renewal_decision === 'migrate' ||
    item.vps.renewal_decision === 'cancel' ||
    item.vps.active_node_link_count <= 0
  ) {
    return 'asset-decision-row--alert'
  }
  return 'asset-decision-row--notice'
}

function queueReasonLabel(item: DecisionQueueItem): string {
  if (item.vps.renewal_decision === 'unreviewed' && item.renewalDue) return '续费待评估'
  if (item.vps.renewal_decision === 'unreviewed') return '待评估'
  if (item.vps.renewal_decision === 'migrate') return '待迁移'
  if (item.vps.renewal_decision === 'cancel') return '待取消'
  if (item.renewalDue) return '续费窗口'
  return '核对'
}

function renderQualityIssues(issues: AssetQualityIssue[]) {
  if (issues.length === 0) {
    return <Badge tone="normal">资料完整</Badge>
  }

  return issues.map((issue) => (
    <Badge key={issue.key} tone={issue.tone}>
      {issue.label}
    </Badge>
  ))
}

function renderSubscriptionSignal(subscription: SubscriptionRecord | null) {
  if (!subscription) {
    return (
      <div className="asset-decision-signal asset-decision-signal--critical">
        <span>订阅</span>
        <strong>缺订阅</strong>
        <small>无法判断续费成本</small>
      </div>
    )
  }

  const days = daysUntilDate(subscription.renew_at)
  const renewalMeta = subscription.renew_at
    ? `${formatDate(subscription.renew_at)} · ${renewalTimingLabel(days)}`
    : '续费日缺失'
  const autoRenewLabel = subscription.auto_renew_cancelled
    ? '已取消自动续费'
    : subscription.auto_renew
      ? '自动续费'
      : '手动续费'

  return (
    <div className="asset-decision-signal">
      <span>订阅</span>
      <strong>{formatMoney(subscription.monthly_price, subscription.currency)}/月</strong>
      <small>{renewalMeta}</small>
      <Badge tone={subscription.auto_renew_cancelled ? 'alert' : 'neutral'}>{autoRenewLabel}</Badge>
    </div>
  )
}

function renderDecisionQueueItem(
  item: DecisionQueueItem,
  index: number,
  onSelect: (vps: VPSAssetRecord) => void,
) {
  const vps = item.vps
  return (
    <li className={['asset-decision-row', queueRowClass(item)].join(' ')} key={vps.vps_id}>
      <div className="asset-decision-row__rank">
        <strong>P{index + 1}</strong>
        <span>{queueReasonLabel(item)}</span>
      </div>
      <div className="asset-decision-row__main">
        <div className="asset-decision-row__title">
          <strong>{vps.display_name}</strong>
          <MonoDigits>{vps.vps_id}</MonoDigits>
        </div>
        <div className="asset-decision-row__meta">
          <span>{formatOptional(vps.provider_name)}</span>
          <span>{vpsLocationLabel(vps)}</span>
          <span>{vpsAccessLabel(vps)}</span>
        </div>
        <div className="asset-status-stack">
          <LifecycleBadge value={vps.lifecycle_status} />
          <UsageBadge value={vps.usage_status} />
          <RenewalBadge value={vps.renewal_decision} />
        </div>
      </div>
      <div className="asset-decision-row__signals">
        {renderSubscriptionSignal(item.subscription)}
        <div className={['asset-decision-signal', vps.active_node_link_count <= 0 && 'asset-decision-signal--alert'].filter(Boolean).join(' ')}>
          <span>Node</span>
          <strong>
            {vps.active_node_link_count > 0 ? (
              <>
                <MonoDigits>{vps.active_node_link_count}</MonoDigits> 已关联
              </>
            ) : (
              '未关联'
            )}
          </strong>
          <small>仅显示关联数量</small>
        </div>
        <div className="asset-decision-quality" aria-label={`${vps.display_name} 数据质量`}>
          {renderQualityIssues(item.qualityIssues)}
        </div>
      </div>
      <div className="asset-decision-actions">
        <Link className="text-link" to={`/vps/${vps.vps_id}`}>详情</Link>
        <Button
          size="sm"
          variant="secondary"
          aria-label={`处理 ${vps.vps_id}`}
          onClick={() => onSelect(vps)}
        >
          处理
        </Button>
      </div>
    </li>
  )
}

export function AssetDecisionsPage() {
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

    return () => {
      cancelled = true
    }
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

    return () => {
      cancelled = true
    }
  }, [])

  const subscriptionsByVPS = useMemo(
    () => groupSubscriptionsByVPS(state.subscriptions),
    [state.subscriptions],
  )
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
  const qualityGapCount = decisionQueue.filter((item) => item.qualityIssues.length > 0).length
  const lifecycleActionCount = state.migrate.length + state.cancel.length
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
  const focusItems = [
    {
      label: '优先处理',
      value: `${priorityDecisionCount}`,
      meta: `${renewalWindow} 天内续费且未评估`,
      tone: priorityDecisionCount > 0 ? 'critical' : 'normal',
    },
    {
      label: '统一队列',
      value: `${totalDecisionQueue}`,
      meta: visibleDecisionQueue.length === totalDecisionQueue
        ? '当前显示全部'
        : `当前视图 ${visibleDecisionQueue.length} 台`,
      tone: totalDecisionQueue > 0 ? 'notice' : 'normal',
    },
    {
      label: '资料缺口',
      value: `${qualityGapCount}`,
      meta: `缺订阅 ${missingSubscriptionCount} / 未关联 ${unlinkedCount}`,
      tone: qualityGapCount > 0 ? 'alert' : 'normal',
    },
    {
      label: '生命周期动作',
      value: `${lifecycleActionCount}`,
      meta: `迁移 ${state.migrate.length} / 取消 ${state.cancel.length}`,
      tone: lifecycleActionCount > 0 ? 'alert' : 'normal',
    },
    {
      label: '续费证据',
      value: `${state.renewals.length}`,
      meta: state.renewalsError ? '续费窗口读取失败' : `${renewalWindow} 天窗口订阅`,
      tone: state.renewalsError ? 'notice' : state.renewals.length > 0 ? 'normal' : 'neutral',
    },
  ] satisfies Array<{ label: string; value: string; meta: string; tone: 'normal' | 'notice' | 'alert' | 'critical' | 'neutral' }>

  function selectVPS(vps: VPSAssetRecord) {
    setSelectedVPS(vps)
    setDecisionDraft({ renewalDecision: vps.renewal_decision, reason: '' })
    setDecisionError(null)
    setDecisionNotice(null)
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
        }))
        closeDecisionDrawer()
        setDecisionNotice(`续费决策已保存：${updated.display_name} -> ${renewalQueueLabel(updated.renewal_decision)}`)
      })
      .catch((error: unknown) => {
        setDecisionError(describeError(error, '更新续费决策失败'))
      })
      .finally(() => setDecisionSubmitting(false))
  }

  return (
    <div className="page-stack asset-page asset-decisions-page">
      <section className="page-panel page-panel--inline">
        <div>
          <div className="page-panel__eyebrow">ASSET LEDGER</div>
          <h1 className="page-panel__title">资产决策</h1>
          <p className="page-panel__description">
            以工作队列集中处理续费、迁移、取消和资料缺口。先判断哪些 VPS 现在最需要动作，再进入详情补充证据。
          </p>
        </div>
        <div className="page-panel__actions">
          <Link className="btn btn--secondary btn--md" to="/vps">VPS 库存</Link>
          <Link className="btn btn--secondary btn--md" to="/subscriptions">订阅列表</Link>
        </div>
      </section>

      <dl className="asset-workbench-summary" aria-label="资产决策指标">
        <div className="asset-workbench-summary__item">
          <dt>{renewalWindow} 天续费</dt>
          <dd><MonoDigits>{state.renewals.length}</MonoDigits> 条订阅</dd>
        </div>
        <div className="asset-workbench-summary__item">
          <dt>统一队列</dt>
          <dd><MonoDigits>{totalDecisionQueue}</MonoDigits> 台 VPS</dd>
        </div>
        <div className="asset-workbench-summary__item">
          <dt>缺订阅 / 未关联</dt>
          <dd><MonoDigits>{missingSubscriptionCount}</MonoDigits> / <MonoDigits>{unlinkedCount}</MonoDigits></dd>
        </div>
        <div className="asset-workbench-summary__item">
          <dt>迁移 / 取消</dt>
          <dd><MonoDigits>{state.migrate.length + state.cancel.length}</MonoDigits> 台 VPS</dd>
        </div>
      </dl>

      <section className="page-panel asset-decision-board" aria-label="资产决策工作队列">
        <div className="asset-decision-board__header">
          <div>
            <p className="section-heading__eyebrow">DECISION QUEUE</p>
            <h2>资产决策工作队列</h2>
            <p>按未评估、续费窗口、迁移/取消、Node 关联和订阅缺口排序。</p>
          </div>
          <label className="asset-decision-window">
            <span>续费窗口</span>
            <select
              className="input"
              value={String(renewalWindow)}
              onChange={(event) => changeRenewalWindow(event.target.value)}
            >
              {RENEWAL_WINDOWS.map((value) => (
                <option key={value} value={value}>未来 {value} 天</option>
              ))}
            </select>
          </label>
        </div>
        <Tabs items={queueTabs} value={queueView} onChange={setQueueView} variant="pill" />
        <div className="asset-decision-focus" aria-label="资产决策处理焦点">
          {focusItems.map((item) => (
            <article
              key={item.label}
              className={['asset-decision-focus__item', `asset-decision-focus__item--${item.tone}`].join(' ')}
            >
              <span>{item.label}</span>
              <strong>{item.value}</strong>
              <small>{item.meta}</small>
            </article>
          ))}
        </div>
        {decisionNotice && <p className="asset-operation-feedback">{decisionNotice}</p>}
        {state.vpsLoading ? (
          <div className="empty-state">正在加载 VPS 决策队列…</div>
        ) : state.vpsError ? (
          <div className="empty-state">{state.vpsError}</div>
        ) : visibleDecisionQueue.length === 0 ? (
          <div className="empty-state">当前视图暂无待处理 VPS</div>
        ) : (
          <ol className="asset-decision-queue" aria-label="资产决策队列列表">
            {visibleDecisionQueue.map((item, index) => renderDecisionQueueItem(item, index, selectVPS))}
          </ol>
        )}
      </section>

      <section className="page-panel page-panel--scroll-x asset-renewal-evidence">
        <div className="section-heading">
          <div>
            <p className="section-heading__eyebrow">RENEWAL EVIDENCE</p>
            <h2>续费候选证据</h2>
          </div>
          <span className="section-heading__meta">
            <MonoDigits>{state.renewals.length}</MonoDigits> 条订阅进入 {renewalWindow} 天窗口
          </span>
        </div>
        <AssetDecisionRenewalTable
          loading={state.renewalsLoading}
          error={state.renewalsError}
          renewals={state.renewals}
          renderActions={(subscription) => (
            <span className="asset-decision-actions">
              <Link className="text-link" to={`/vps/${subscription.vps_id}`}>VPS</Link>
              <Link className="text-link" to={`/subscriptions?renew_within_days=${renewalWindow}`}>订阅</Link>
            </span>
          )}
        />
      </section>

      <Drawer
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
      </Drawer>
    </div>
  )
}

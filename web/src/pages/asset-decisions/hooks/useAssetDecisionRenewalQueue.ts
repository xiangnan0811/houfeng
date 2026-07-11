import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  listSubscriptions,
  listVPSAssets,
  updateVPSAsset,
} from '../../../lib/api'
import type {
  SubscriptionRecord,
  VPSAssetRecord,
  VPSAssetUpdateResult,
} from '../../../lib/types'
import { groupSubscriptionsByVPS } from '../../assetPageUtils'
import {
  buildDecisionQueue,
  filterDecisionQueue,
  hasCancellationAttention,
  updateDecisionQueues,
} from '../businessLogic'
import { INITIAL_DECISION_DRAFT } from '../constants'
import { renewalQueueLabel } from '../formatters'
import type {
  AssetDecisionDraft,
  DecisionQueueView,
  DecisionQueueItem,
  QueueState,
  RenewalWindow,
} from '../types'
import { describeError } from '../utils'
import type { AssetDecisionInvalidationEvent } from './invalidation'

type SettledRenewals = Readonly<{
  renewalWindow: RenewalWindow
  revision: number
  retryRevision: number
  error: string | null
  renewals: SubscriptionRecord[]
}>

type SettledQueue = Readonly<{
  revision: number
  retryRevision: number
  error: string | null
  subscriptions: SubscriptionRecord[]
  unreviewed: VPSAssetRecord[]
  migrate: VPSAssetRecord[]
  cancel: VPSAssetRecord[]
}>

export type AssetDecisionRenewalQueueState = Readonly<{
  queue: Readonly<QueueState>
  decisionQueue: DecisionQueueItem[]
  visibleDecisionQueue: DecisionQueueItem[]
  queueView: DecisionQueueView
  selectedVPS: VPSAssetRecord | null
  draft: Readonly<AssetDecisionDraft>
  submitting: boolean
  error: string | null
  totalDecisionQueue: number
  renewalDueQueueCount: number
  missingSubscriptionCount: number
  unlinkedCount: number
  cancellationAttentionCount: number
}>

export type AssetDecisionRenewalQueueCommands = Readonly<{
  reloadRenewals: () => void
  reloadQueue: () => void
  selectQueueView: (view: DecisionQueueView) => void
  selectVPS: (vps: VPSAssetRecord) => void
  closeVPS: () => void
  updateDraft: (patch: Partial<AssetDecisionDraft>) => void
  submitRenewal: () => Promise<VPSAssetUpdateResult | null>
}>

type UseAssetDecisionRenewalQueueInput = Readonly<{
  renewalWindow: RenewalWindow
  revision: number
  contextKey?: string
  onNotice: (notice: string) => void
  onInvalidate: (event: AssetDecisionInvalidationEvent) => void
}>

export function useAssetDecisionRenewalQueue({
  renewalWindow,
  revision,
  contextKey = '',
  onNotice,
  onInvalidate,
}: UseAssetDecisionRenewalQueueInput): {
  state: AssetDecisionRenewalQueueState
  commands: AssetDecisionRenewalQueueCommands
} {
  const [renewalsRetryRevision, setRenewalsRetryRevision] = useState(0)
  const [queueRetryRevision, setQueueRetryRevision] = useState(0)
  const [settledRenewals, setSettledRenewals] = useState<SettledRenewals | null>(null)
  const [settledQueue, setSettledQueue] = useState<SettledQueue | null>(null)
  const [queueView, setQueueView] = useState<DecisionQueueView>('all')
  const [selectedVPS, setSelectedVPS] = useState<VPSAssetRecord | null>(null)
  const [selectedContextKey, setSelectedContextKey] = useState<string | null>(null)
  const [draft, setDraft] = useState<AssetDecisionDraft>(INITIAL_DECISION_DRAFT)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const areRenewalsCurrent = settledRenewals?.renewalWindow === renewalWindow &&
    settledRenewals.revision === revision &&
    settledRenewals.retryRevision === renewalsRetryRevision
  const isQueueCurrent = settledQueue?.revision === revision &&
    settledQueue.retryRevision === queueRetryRevision
  const selectionIsCurrent = selectedContextKey === contextKey

  useEffect(() => {
    let cancelled = false
    listSubscriptions({
      renew_within_days: renewalWindow,
      sort: 'renew_at',
      order: 'asc',
    })
      .then((renewals) => {
        if (cancelled) return
        setSettledRenewals({
          renewalWindow,
          revision,
          retryRevision: renewalsRetryRevision,
          error: null,
          renewals,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setSettledRenewals({
          renewalWindow,
          revision,
          retryRevision: renewalsRetryRevision,
          error: describeError(error, '加载续费 evidence 失败'),
          renewals: [],
        })
      })
    return () => { cancelled = true }
  }, [renewalWindow, renewalsRetryRevision, revision])

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
        setSettledQueue({
          revision,
          retryRevision: queueRetryRevision,
          error: null,
          subscriptions,
          unreviewed,
          migrate,
          cancel,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setSettledQueue({
          revision,
          retryRevision: queueRetryRevision,
          error: describeError(error, '加载 VPS 单台队列失败'),
          subscriptions: [],
          unreviewed: [],
          migrate: [],
          cancel: [],
        })
      })
    return () => { cancelled = true }
  }, [queueRetryRevision, revision])

  const previousContextKeyRef = useRef(contextKey)
  useEffect(() => {
    if (previousContextKeyRef.current === contextKey) return
    previousContextKeyRef.current = contextKey
    let cancelled = false
    queueMicrotask(() => {
      if (cancelled) return
      setSelectedVPS(null)
      setSelectedContextKey(null)
      setDraft(INITIAL_DECISION_DRAFT)
      setError(null)
    })
    return () => { cancelled = true }
  }, [contextKey])

  const queue = useMemo<Readonly<QueueState>>(() => ({
    renewalsLoading: !areRenewalsCurrent,
    renewalsError: areRenewalsCurrent ? settledRenewals.error : null,
    renewals: settledRenewals?.renewals ?? [],
    queueLoading: !isQueueCurrent,
    queueError: isQueueCurrent ? settledQueue.error : null,
    subscriptions: settledQueue?.subscriptions ?? [],
    unreviewed: settledQueue?.unreviewed ?? [],
    migrate: settledQueue?.migrate ?? [],
    cancel: settledQueue?.cancel ?? [],
  }), [areRenewalsCurrent, isQueueCurrent, settledQueue, settledRenewals])
  const subscriptionsByVPS = useMemo(
    () => groupSubscriptionsByVPS(queue.subscriptions),
    [queue.subscriptions],
  )
  const decisionQueue = useMemo(
    () => buildDecisionQueue(
      [...queue.unreviewed, ...queue.migrate, ...queue.cancel],
      subscriptionsByVPS,
      renewalWindow,
    ),
    [queue.cancel, queue.migrate, queue.unreviewed, renewalWindow, subscriptionsByVPS],
  )
  const visibleDecisionQueue = useMemo(
    () => filterDecisionQueue(decisionQueue, queueView),
    [decisionQueue, queueView],
  )
  const renewalDueQueueCount = decisionQueue.filter((item) => item.renewalDue).length
  const missingSubscriptionCount = decisionQueue.filter((item) => !item.subscription).length
  const unlinkedCount = decisionQueue.filter((item) => item.vps.active_monitoring_instance_link_count <= 0).length
  const cancellationAttentionCount = decisionQueue.filter(hasCancellationAttention).length

  const selectQueueView = useCallback((view: DecisionQueueView) => {
    setQueueView(view)
  }, [])

  const selectVPS = useCallback((vps: VPSAssetRecord) => {
    setSelectedVPS(vps)
    setSelectedContextKey(contextKey)
    setDraft({ renewalDecision: vps.renewal_decision, reason: '' })
    setError(null)
  }, [contextKey])

  const closeVPS = useCallback(() => {
    setSelectedVPS(null)
    setSelectedContextKey(null)
    setDraft(INITIAL_DECISION_DRAFT)
    setError(null)
  }, [])

  const updateDraft = useCallback((patch: Partial<AssetDecisionDraft>) => {
    setDraft((current) => ({ ...current, ...patch }))
  }, [])

  const submitRenewal = useCallback(async (): Promise<VPSAssetUpdateResult | null> => {
    if (!selectionIsCurrent || !selectedVPS) return null
    setError(null)
    if (draft.renewalDecision === selectedVPS.renewal_decision) {
      setError('请选择一个不同的续费决策')
      return null
    }

    const reason = draft.reason.trim()
    setSubmitting(true)
    try {
      const updated = await updateVPSAsset(selectedVPS.vps_id, {
        renewal_decision: draft.renewalDecision,
        ...(reason ? { renewal_reason: reason } : {}),
      })
      setSettledQueue((current) => current ? {
        ...current,
        ...updateDecisionQueues(queue, updated),
        subscriptions: current.subscriptions.map((subscription) => (
          updated.renewal_subscription_linkage?.updated &&
          subscription.subscription_id === updated.renewal_subscription_linkage.subscription_id
            ? { ...subscription, auto_renew: false, auto_renew_cancelled: true }
            : subscription
        )),
      } : current)
      setSettledRenewals((current) => current ? {
        ...current,
        renewals: current.renewals.map((subscription) => (
          updated.renewal_subscription_linkage?.updated &&
          subscription.subscription_id === updated.renewal_subscription_linkage.subscription_id
            ? { ...subscription, auto_renew: false, auto_renew_cancelled: true }
            : subscription
        )),
      } : current)
      setSelectedVPS(null)
      setSelectedContextKey(null)
      setDraft(INITIAL_DECISION_DRAFT)
      const baseNotice = `续费决策已保存：${updated.display_name} -> ${renewalQueueLabel(updated.renewal_decision)}`
      const linkageMessage = updated.renewal_subscription_linkage?.message
      onNotice(linkageMessage ? `${baseNotice}。${linkageMessage}` : baseNotice)
      onInvalidate({ type: 'renewal-decision-saved', vpsID: updated.vps_id })
      return updated
    } catch (submitError) {
      setError(describeError(submitError, '更新续费决策失败'))
      return null
    } finally {
      setSubmitting(false)
    }
  }, [draft.reason, draft.renewalDecision, onInvalidate, onNotice, queue, selectedVPS, selectionIsCurrent])

  return {
    state: {
      queue,
      decisionQueue,
      visibleDecisionQueue,
      queueView,
      selectedVPS: selectionIsCurrent ? selectedVPS : null,
      draft: selectionIsCurrent ? draft : INITIAL_DECISION_DRAFT,
      submitting: selectionIsCurrent && submitting,
      error: selectionIsCurrent ? error : null,
      totalDecisionQueue: decisionQueue.length,
      renewalDueQueueCount,
      missingSubscriptionCount,
      unlinkedCount,
      cancellationAttentionCount,
    },
    commands: {
      reloadRenewals: () => setRenewalsRetryRevision((current) => current + 1),
      reloadQueue: () => setQueueRetryRevision((current) => current + 1),
      selectQueueView,
      selectVPS,
      closeVPS,
      updateDraft,
      submitRenewal,
    },
  }
}

import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { ActionConfirmationCard } from '../components/ActionConfirmationCard'
import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import { IncidentList } from '../components/IncidentList'
import { Card } from '../components/atoms/Card'
import {
  Drawer,
  Hostname,
  MonoDigits,
  Tabs,
  Timestamp,
} from '../components/atoms'
import { Button } from '../components/atoms/Button'
import {
  NodeLabelsAndNote,
  NodeWatchtowerHeader,
  NodeWatchtowerMetrics,
  type NodeRuntimeAction,
} from '../components/node-detail'
import { StatusBadge } from '../components/StatusBadge'
import {
  ApiError,
  confirmNodeRebind,
  enterNodeMaintenance,
  exitNodeMaintenance,
  getNode,
  getNodeOnboarding,
  getNodeRuntimeFacts,
  listEvents,
  listHistoricalIncidents,
  listIncidents,
  pauseNodeMonitoring,
  rejectPendingNodeBinding,
  resetNodeBinding,
  restoreRetiredNodeToObserving,
  retireNode,
  resumeNodeMonitoring,
  updateNodeMetadata,
} from '../lib/api'
import type {
  ActiveIncidentRecord,
  NodeOnboardingState,
  NodeRecord,
  NodeRuntimeFacts,
  PendingBindingMetadata,
  StateChangeEventRecord,
} from '../lib/types'

type State = {
  requestedNodeId: string | null
  error: string | null
  node: NodeRecord | null
  runtimeFacts: NodeRuntimeFacts | null
  requestedActivityNodeId: string | null
  incidents: ActiveIncidentRecord[]
  incidentsError: string | null
  events: StateChangeEventRecord[]
  eventsError: string | null
}

const NODE_BINDING_CONFLICT_LOAD_ERROR = '绑定冲突详情暂不可用'
const NODE_BINDING_ACTION_ERROR = '更新绑定冲突状态失败'
const NODE_LIFECYCLE_ACTION_ERROR = '节点生命周期操作失败'
const NODE_BINDING_CONFLICT_STATUS = '指纹变更待确认'
const NODE_BINDING_CONFIRM_REBIND_LABEL = '确认重绑定'
const NODE_BINDING_REJECT_PENDING_LABEL = '拒绝新指纹'
const NODE_BINDING_RESET_LABEL = '重置绑定'
const NODE_LIFECYCLE_RETIRED = '已退役'
const NODE_LIFECYCLE_V1_LIMITATION_COPY =
  '已退役节点在 V1 中只能先恢复到观察中，不能直接恢复为在用。'

type BindingConflictState = {
  requestedNodeId: string | null
  onboarding: NodeOnboardingState | null
  loading: boolean
  error: string | null
}

type BindingConflictAction = 'confirm' | 'reject' | 'reset'
type NodeLifecycleAction = 'retire' | 'restore-to-observing'
type PendingRuntimeConfirmation = {
  action: 'pause'
}

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function maskFingerprint(value?: string | null) {
  if (!value) return '尚无'
  const normalized = value.trim()
  if (!normalized) return '尚无'
  if (normalized.length <= 14) return normalized
  return `${normalized.slice(0, 8)}…${normalized.slice(-6)}`
}

function currentFingerprintSummary(onboarding: NodeOnboardingState | null) {
  if (onboarding?.current_binding_fingerprint_summary?.trim()) {
    return onboarding.current_binding_fingerprint_summary.trim()
  }
  return '服务端当前未提供已绑定指纹摘要'
}

function pendingBindingMetadata(onboarding: NodeOnboardingState | null): PendingBindingMetadata | null {
  return onboarding?.pending_binding ?? null
}

function nodeRuntimeActions(node: NodeRecord): Array<{ action: NodeRuntimeAction; label: string }> {
  if (node.monitoring_status === '启用') {
    return [
      { action: 'enter-maintenance', label: '进入维护' },
      { action: 'pause', label: '暂停监控' },
    ]
  }

  if (node.monitoring_status === '维护中') {
    return [
      { action: 'exit-maintenance', label: '退出维护' },
      { action: 'pause', label: '暂停监控' },
    ]
  }

  if (node.monitoring_status === '暂停') {
    return [{ action: 'resume', label: '恢复监控' }]
  }

  return []
}

function pauseConfirmationCurrent(node: NodeRecord) {
  return node.monitoring_status === '维护中'
    ? '当前：监控运行状态为维护中。'
    : '当前：监控运行状态为启用。'
}

function parseLabels(value: string) {
  const result: string[] = []
  const seen = new Set<string>()

  for (const label of value.split(/[,，]/).map((item) => item.trim()).filter(Boolean)) {
    if (seen.has(label)) continue
    seen.add(label)
    result.push(label)
  }

  return result
}

function mergeNonMetadataNodeRecord<T extends NodeRecord>(current: NodeRecord, updated: T): T {
  return {
    ...updated,
    labels: current.labels,
    note: current.note,
  }
}

export function NodeDetailPage() {
  const { nodeId } = useParams()
  return <NodeDetailPageContent key={nodeId ?? 'missing-node'} nodeId={nodeId} />
}

function NodeDetailPageContent({ nodeId }: { nodeId?: string }) {
  const [state, setState] = useState<State>({
    requestedNodeId: null,
    error: null,
    node: null,
    runtimeFacts: null,
    requestedActivityNodeId: null,
    incidents: [],
    incidentsError: null,
    events: [],
    eventsError: null,
  })
  const [runtimeSubmitting, setRuntimeSubmitting] = useState(false)
  const [runtimeError, setRuntimeError] = useState<string | null>(null)
  const [pendingRuntimeConfirmation, setPendingRuntimeConfirmation] =
    useState<PendingRuntimeConfirmation | null>(null)
  const [lifecycleSubmitting, setLifecycleSubmitting] = useState<NodeLifecycleAction | null>(null)
  const [lifecycleError, setLifecycleError] = useState<string | null>(null)
  const [showRetireConfirmation, setShowRetireConfirmation] = useState(false)
  const [bindingConflictState, setBindingConflictState] = useState<BindingConflictState>({
    requestedNodeId: null,
    onboarding: null,
    loading: false,
    error: null,
  })
  const [bindingAction, setBindingAction] = useState<BindingConflictAction | null>(null)
  const [metadataEditing, setMetadataEditing] = useState(false)
  const [metadataLabelDraft, setMetadataLabelDraft] = useState('')
  const [metadataNoteDraft, setMetadataNoteDraft] = useState('')
  const [metadataSubmitting, setMetadataSubmitting] = useState(false)
  const [metadataError, setMetadataError] = useState<string | null>(null)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [historyTab, setHistoryTab] = useState<'events' | 'incidents'>('events')
  const [historyIncidents, setHistoryIncidents] = useState<ActiveIncidentRecord[] | null>(null)
  const [historyIncidentsLoading, setHistoryIncidentsLoading] = useState(false)
  const [historyIncidentsError, setHistoryIncidentsError] = useState<string | null>(null)
  const currentRouteNodeIdRef = useRef<string | null>(nodeId ?? null)
  const currentRequestedNodeIdRef = useRef<string | null>(null)
  const isMountedRef = useRef(true)
  const actionButtonRefs = useRef<Record<NodeRuntimeAction, HTMLButtonElement | null>>({
    'enter-maintenance': null,
    'exit-maintenance': null,
    pause: null,
    resume: null,
  })
  const pendingFocusRestoreRef = useRef<NodeRuntimeAction | null>(null)

  useEffect(() => {
    currentRouteNodeIdRef.current = nodeId ?? null
  }, [nodeId])

  useEffect(() => {
    currentRequestedNodeIdRef.current = state.requestedNodeId
  }, [state.requestedNodeId])

  useEffect(() => {
    if (pendingRuntimeConfirmation) return

    const action = pendingFocusRestoreRef.current
    if (!action) return

    const preferred = actionButtonRefs.current[action]
    const fallback = action === 'pause' ? actionButtonRefs.current.resume : null
    const target = [preferred, fallback].find((element) => element?.isConnected)

    target?.focus()
    pendingFocusRestoreRef.current = null
  }, [pendingRuntimeConfirmation, state.node])

  useEffect(
    () => () => {
      isMountedRef.current = false
    },
    [],
  )

  useEffect(() => {
    let cancelled = false
    if (!nodeId) return

    Promise.all([getNode(nodeId), getNodeRuntimeFacts(nodeId)])
      .then(([node, runtimeFacts]) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          requestedNodeId: nodeId,
          error: null,
          node,
          runtimeFacts,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message =
          error instanceof ApiError && error.status === 404
            ? '节点不存在'
            : describeError(error, '加载节点详情失败')
        setState((current) => ({
          ...current,
          requestedNodeId: nodeId,
          error: message,
          node: null,
          runtimeFacts: null,
        }))
      })

    return () => {
      cancelled = true
    }
  }, [nodeId])

  useEffect(() => {
    let cancelled = false
    if (!nodeId) {
      setBindingConflictState({
        requestedNodeId: null,
        onboarding: null,
        loading: false,
        error: null,
      })
      return
    }

    if (state.requestedNodeId !== nodeId || !state.node) {
      return
    }

    if (state.node.binding_status !== NODE_BINDING_CONFLICT_STATUS) {
      setBindingConflictState({
        requestedNodeId: nodeId,
        onboarding: null,
        loading: false,
        error: null,
      })
      return
    }

    setBindingConflictState((current) => ({
      requestedNodeId: nodeId,
      onboarding: current.requestedNodeId === nodeId ? current.onboarding : null,
      loading: true,
      error: null,
    }))

    getNodeOnboarding(nodeId)
      .then((onboarding) => {
        if (cancelled) return
        setBindingConflictState({
          requestedNodeId: nodeId,
          onboarding,
          loading: false,
          error: null,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setBindingConflictState({
          requestedNodeId: nodeId,
          onboarding: null,
          loading: false,
          error: describeError(error, NODE_BINDING_CONFLICT_LOAD_ERROR),
        })
      })

    return () => {
      cancelled = true
    }
  }, [nodeId, state.node, state.requestedNodeId])

  useEffect(() => {
    let cancelled = false
    if (!nodeId) return

    Promise.allSettled([
      listIncidents({ object_type: 'node', object_id: nodeId }),
      listEvents({ object_type: 'node', object_id: nodeId }),
    ]).then(([incidentsResult, eventsResult]) => {
      if (cancelled) return
      setState((current) => ({
        ...current,
        requestedActivityNodeId: nodeId,
        incidents:
          incidentsResult.status === 'fulfilled' ? incidentsResult.value : [],
        incidentsError:
          incidentsResult.status === 'fulfilled'
            ? null
            : describeError(incidentsResult.reason, '加载活跃异常失败'),
        events: eventsResult.status === 'fulfilled' ? eventsResult.value : [],
        eventsError:
          eventsResult.status === 'fulfilled'
            ? null
            : describeError(eventsResult.reason, '加载相关事件失败'),
      }))
    })

    return () => {
      cancelled = true
    }
  }, [nodeId])

  // Reset historical incidents when navigating between nodes so the drawer never
  // shows stale data from the previous node when reopened.
  useEffect(() => {
    setHistoryIncidents(null)
    setHistoryIncidentsError(null)
    setHistoryIncidentsLoading(false)
  }, [nodeId])

  // Lazy-load historical incidents the first time the user opens the drawer
  // and switches to the "历史异常" tab. Subsequent opens reuse the cached set
  // (cleared on node id change via the reset effect above). We use refs so
  // setState calls inside the effect do not re-trigger it (which would cancel
  // the in-flight promise).
  const historyFetchRef = useRef<{
    nodeId: string | null
    inFlight: boolean
    fetched: boolean
  }>({ nodeId: null, inFlight: false, fetched: false })

  useEffect(() => {
    if (historyFetchRef.current.nodeId !== nodeId) {
      historyFetchRef.current = { nodeId: nodeId ?? null, inFlight: false, fetched: false }
    }
  }, [nodeId])

  const wantsHistoryIncidents = historyOpen && historyTab === 'incidents'

  useEffect(() => {
    if (!nodeId) return
    if (!wantsHistoryIncidents) return
    if (historyFetchRef.current.inFlight || historyFetchRef.current.fetched) return

    let cancelled = false
    const targetNodeId = nodeId
    historyFetchRef.current = { nodeId: targetNodeId, inFlight: true, fetched: false }
    setHistoryIncidentsLoading(true)
    setHistoryIncidentsError(null)

    listHistoricalIncidents('node', targetNodeId)
      .then((records) => {
        if (cancelled) return
        setHistoryIncidents(records)
        historyFetchRef.current = { nodeId: targetNodeId, inFlight: false, fetched: true }
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setHistoryIncidentsError(describeError(error, '加载历史异常失败'))
        historyFetchRef.current = { nodeId: targetNodeId, inFlight: false, fetched: false }
      })
      .finally(() => {
        if (cancelled) return
        setHistoryIncidentsLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [nodeId, wantsHistoryIncidents])

  const missingNodeId = !nodeId
  const isCurrentNode = state.requestedNodeId === nodeId
  const hasCurrentActivity = state.requestedActivityNodeId === nodeId
  const error = isCurrentNode ? state.error : null
  const node = isCurrentNode ? state.node : null
  const runtimeFacts = isCurrentNode ? state.runtimeFacts : null
  const incidents = hasCurrentActivity ? state.incidents : []
  const events = hasCurrentActivity ? state.events : []
  const eventsError = hasCurrentActivity ? state.eventsError : null

  async function handleRuntimeAction(action: NodeRuntimeAction, confirmed = false) {
    if (!node) return
    if (action === 'pause' && !confirmed) {
      setPendingRuntimeConfirmation({ action })
      return
    }

    const actionNodeId = node.node_id
    setRuntimeSubmitting(true)
    setRuntimeError(null)

    try {
      const updated =
        action === 'enter-maintenance'
          ? await enterNodeMaintenance(actionNodeId)
          : action === 'exit-maintenance'
            ? await exitNodeMaintenance(actionNodeId)
            : action === 'pause'
              ? await pauseNodeMonitoring(actionNodeId)
              : await resumeNodeMonitoring(actionNodeId)
      if (
        !isMountedRef.current ||
        currentRouteNodeIdRef.current !== actionNodeId ||
        currentRequestedNodeIdRef.current !== actionNodeId
      ) {
        return
      }
      setState((current) => ({
        ...current,
        node:
          current.requestedNodeId === actionNodeId && current.node
            ? mergeNonMetadataNodeRecord(current.node, updated)
            : current.node,
      }))
      pendingFocusRestoreRef.current = action
      setPendingRuntimeConfirmation((current) => (current?.action === action ? null : current))
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteNodeIdRef.current !== actionNodeId ||
        currentRequestedNodeIdRef.current !== actionNodeId
      ) {
        return
      }
      setRuntimeError(describeError(error, '节点运行控制操作失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteNodeIdRef.current === actionNodeId &&
        currentRequestedNodeIdRef.current === actionNodeId
      ) {
        setRuntimeSubmitting(false)
      }
    }
  }

  async function handleLifecycleAction(action: NodeLifecycleAction) {
    if (!node) return
    const actionNodeId = node.node_id
    setLifecycleSubmitting(action)
    setLifecycleError(null)

    try {
      const updated =
        action === 'retire'
          ? await retireNode(actionNodeId)
          : await restoreRetiredNodeToObserving(actionNodeId)
      if (
        !isMountedRef.current ||
        currentRouteNodeIdRef.current !== actionNodeId ||
        currentRequestedNodeIdRef.current !== actionNodeId
      ) {
        return
      }
      setState((current) => ({
        ...current,
        node:
          current.requestedNodeId === actionNodeId && current.node
            ? mergeNonMetadataNodeRecord(current.node, updated)
            : current.node,
      }))
      setShowRetireConfirmation(false)
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteNodeIdRef.current !== actionNodeId ||
        currentRequestedNodeIdRef.current !== actionNodeId
      ) {
        return
      }
      setLifecycleError(describeError(error, NODE_LIFECYCLE_ACTION_ERROR))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteNodeIdRef.current === actionNodeId &&
        currentRequestedNodeIdRef.current === actionNodeId
      ) {
        setLifecycleSubmitting(null)
      }
    }
  }

  function applyOnboardingToNode(actionNodeId: string, onboarding: NodeOnboardingState) {
    setState((current) => {
      if (current.requestedNodeId !== actionNodeId) return current
      return {
        ...current,
        node: current.node ? mergeNonMetadataNodeRecord(current.node, onboarding) : onboarding,
      }
    })
    setBindingConflictState({
      requestedNodeId: actionNodeId,
      onboarding: onboarding.binding_status === NODE_BINDING_CONFLICT_STATUS ? onboarding : null,
      loading: false,
      error: null,
    })
  }

  async function handleBindingAction(
    action: BindingConflictAction,
    request: (targetNodeId: string) => Promise<NodeOnboardingState>,
  ) {
    if (!node || !bindingConflict || bindingConflictLoading) return
    const actionNodeId = node.node_id
    setBindingAction(action)
    setBindingConflictState((current) => ({
      ...current,
      requestedNodeId: actionNodeId,
      error: null,
    }))

    try {
      const nextOnboarding = await request(actionNodeId)
      if (
        !isMountedRef.current ||
        currentRouteNodeIdRef.current !== actionNodeId ||
        currentRequestedNodeIdRef.current !== actionNodeId
      ) {
        return
      }
      applyOnboardingToNode(actionNodeId, nextOnboarding)
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteNodeIdRef.current !== actionNodeId ||
        currentRequestedNodeIdRef.current !== actionNodeId
      ) {
        return
      }
      setBindingConflictState((current) => ({
        ...current,
        requestedNodeId: actionNodeId,
        error: describeError(error, NODE_BINDING_ACTION_ERROR),
      }))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteNodeIdRef.current === actionNodeId &&
        currentRequestedNodeIdRef.current === actionNodeId
      ) {
        setBindingAction(null)
      }
    }
  }

  if (!missingNodeId && !isCurrentNode) {
    return <section className="page-panel">正在加载节点详情…</section>
  }

  if (missingNodeId || error || !node) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">节点详情</p>
        <h2 className="page-panel__title">节点详情不可用</h2>
        <p className="page-panel__description">{error ?? '未找到节点'}</p>
        <Link className="text-link" to="/nodes">
          返回节点列表
        </Link>
      </section>
    )
  }

  const sample = runtimeFacts?.latest_host_sample ?? null
  const recentSamples = runtimeFacts?.recent_host_samples ?? []
  const isMaintenance = node.monitoring_status === '维护中'
  const showBindingConflict = node.binding_status === NODE_BINDING_CONFLICT_STATUS
  const isRetiredNode = node.lifecycle_status === NODE_LIFECYCLE_RETIRED
  const hasCurrentBindingConflictState = bindingConflictState.requestedNodeId === nodeId
  const bindingConflict = hasCurrentBindingConflictState ? bindingConflictState.onboarding : null
  const bindingConflictError = hasCurrentBindingConflictState ? bindingConflictState.error : null
  const bindingConflictLoading =
    hasCurrentBindingConflictState && bindingConflictState.loading && !bindingConflict
  const bindingActionsDisabled = bindingAction !== null || bindingConflictLoading || !bindingConflict
  const runtimeActions = nodeRuntimeActions(node)
  const showDangerZone = node.current_active_incident_count > 0
  const firstIncident =
    incidents.length > 0
      ? [...incidents].sort(
          (a, b) => new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
        )[0]
      : null

  function registerActionRef(action: NodeRuntimeAction, element: HTMLButtonElement | null) {
    actionButtonRefs.current[action] = element
  }

  function retryHistoryIncidents() {
    historyFetchRef.current = { nodeId: nodeId ?? null, inFlight: false, fetched: false }
    setHistoryIncidents(null)
    setHistoryIncidentsError(null)
  }

  function openHistory(tab: 'events' | 'incidents' = 'events') {
    setHistoryTab(tab)
    setHistoryOpen(true)
  }

  async function handleMetadataSave() {
    if (!node) return

    const actionNodeId = node.node_id
    setMetadataSubmitting(true)
    setMetadataError(null)

    try {
      const updated = await updateNodeMetadata(
        actionNodeId,
        {
          labels: parseLabels(metadataLabelDraft),
          note: metadataNoteDraft.trim(),
        },
        {
          expectedUpdatedAt: node.updated_at,
        },
      )
      if (
        !isMountedRef.current ||
        currentRouteNodeIdRef.current !== actionNodeId ||
        currentRequestedNodeIdRef.current !== actionNodeId
      ) {
        return
      }
      setState((current) => ({
        ...current,
        node:
          current.requestedNodeId === actionNodeId && current.node
            ? {
                ...current.node,
                labels: updated.labels,
                note: updated.note,
                updated_at: updated.updated_at,
              }
            : current.node,
      }))
      setMetadataEditing(false)
      setMetadataLabelDraft('')
      setMetadataNoteDraft('')
    } catch (metadataError) {
      if (
        !isMountedRef.current ||
        currentRouteNodeIdRef.current !== actionNodeId ||
        currentRequestedNodeIdRef.current !== actionNodeId
      ) {
        return
      }
      setMetadataError(describeError(metadataError, '标签或备注更新失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteNodeIdRef.current === actionNodeId &&
        currentRequestedNodeIdRef.current === actionNodeId
      ) {
        setMetadataSubmitting(false)
      }
    }
  }

  return (
    <div className="page-stack">
      <NodeWatchtowerHeader
        node={node}
        latestSample={sample}
        runtimeActions={runtimeActions}
        runtimeSubmitting={runtimeSubmitting}
        onRuntimeAction={(action) => void handleRuntimeAction(action)}
        registerActionRef={registerActionRef}
        onOpenHistory={() => openHistory('events')}
      />

      {showBindingConflict ? (
        <DetailSection eyebrow="绑定冲突" title="绑定冲突处置" aside="高优先级">
          <article className="metric-card" aria-label="高优先级：绑定冲突待处理">
            <h3>高优先级：绑定冲突待处理</h3>
            <p>同一台机器重装或合法替换 agent 后，通常会出现新的指纹接入请求。请先核对这次变更。</p>
            <dl>
              <div>
                <dt>当前已绑定指纹</dt>
                <dd>
                  <Hostname>{currentFingerprintSummary(bindingConflict)}</Hostname>
                </dd>
              </div>
              <div>
                <dt>待确认指纹</dt>
                <dd>
                  <Hostname>
                    {maskFingerprint(pendingBindingMetadata(bindingConflict)?.fingerprint)}
                  </Hostname>
                </dd>
              </div>
              <div>
                <dt>首次出现</dt>
                <dd>
                  <Timestamp
                    value={pendingBindingMetadata(bindingConflict)?.first_seen_at}
                    mode="absolute"
                  />
                </dd>
              </div>
              <div>
                <dt>最近出现</dt>
                <dd>
                  <Timestamp
                    value={pendingBindingMetadata(bindingConflict)?.last_seen_at}
                    mode="absolute"
                  />
                </dd>
              </div>
              <div>
                <dt>尝试次数</dt>
                <dd>
                  <MonoDigits>
                    {pendingBindingMetadata(bindingConflict)?.attempt_count ?? 0}
                  </MonoDigits>
                </dd>
              </div>
            </dl>
            {bindingConflictLoading ? <p>正在加载绑定冲突详情…</p> : null}
            {bindingConflictError ? <p role="alert">{bindingConflictError}</p> : null}
            <div className="badge-row badge-row--wrap">
              <button
                type="button"
                disabled={bindingActionsDisabled}
                onClick={() => void handleBindingAction('confirm', confirmNodeRebind)}
              >
                {bindingAction === 'confirm' ? '正在确认…' : NODE_BINDING_CONFIRM_REBIND_LABEL}
              </button>
              <button
                type="button"
                disabled={bindingActionsDisabled}
                onClick={() => void handleBindingAction('reject', rejectPendingNodeBinding)}
              >
                {bindingAction === 'reject' ? '正在拒绝…' : NODE_BINDING_REJECT_PENDING_LABEL}
              </button>
              <button
                type="button"
                disabled={bindingActionsDisabled}
                onClick={() => void handleBindingAction('reset', resetNodeBinding)}
              >
                {bindingAction === 'reset' ? '正在重置…' : NODE_BINDING_RESET_LABEL}
              </button>
            </div>
            <Link className="text-link" to={`/nodes/${node.node_id}/onboarding`}>
              打开接入工作台
            </Link>
          </article>
        </DetailSection>
      ) : null}

      {showDangerZone ? (
        <Card cardRole="warning" className="watchtower-danger" aria-label="当前主问题">
          <p className="watchtower-danger__eyebrow">当前主问题</p>
          <h2 className="watchtower-danger__summary">
            {node.current_primary_issue_summary || '存在活跃异常'}
          </h2>
          <p className="watchtower-danger__meta">
            活跃异常 <MonoDigits>{node.current_active_incident_count}</MonoDigits> 个 · 健康状态{' '}
            <StatusBadge label={node.current_health_status} />
            {firstIncident?.started_at ? (
              <> · 持续 <Timestamp value={firstIncident.started_at} mode="relative" /></>
            ) : null}
          </p>
          <div className="watchtower-danger__actions">
            <Button variant="ghost" size="sm" onClick={() => openHistory('events')}>
              查看完整时间线 →
            </Button>
          </div>
        </Card>
      ) : null}

      <NodeWatchtowerMetrics
        sample={sample}
        samples={recentSamples}
        isMaintenance={isMaintenance}
      />

      {pendingRuntimeConfirmation?.action === 'pause' ? (
        <ActionConfirmationCard
          title="确认暂停节点监控"
          current={pauseConfirmationCurrent(node)}
          result="操作后：监控运行状态变为暂停。"
          impact="会停止主机指标采集，并停止该节点承担的探针执行。趋势图会从此开始出现数据空档。"
          unchanged="不会删除历史事件、观测记录或 agent 绑定关系。"
          confirmLabel="确认暂停监控"
          disabled={runtimeSubmitting}
          onConfirm={() => void handleRuntimeAction('pause', true)}
          onCancel={() => {
            pendingFocusRestoreRef.current = 'pause'
            setPendingRuntimeConfirmation(null)
          }}
        />
      ) : null}
      {runtimeError ? <p className="watchtower-runtime-error" role="alert">{runtimeError}</p> : null}


      <details className="watchtower-secondary">
        <summary>标签与备注</summary>
        <div className="watchtower-secondary__body">
          <NodeLabelsAndNote
            node={node}
            editing={metadataEditing}
            labelDraft={metadataLabelDraft}
            noteDraft={metadataNoteDraft}
            submitting={metadataSubmitting}
            error={metadataError}
            onLabelDraftChange={setMetadataLabelDraft}
            onNoteDraftChange={setMetadataNoteDraft}
            onStartEdit={() => {
              setMetadataEditing(true)
              setMetadataLabelDraft(node.labels.join(', '))
              setMetadataNoteDraft(node.note)
              setMetadataError(null)
            }}
            onCancelEdit={() => {
              setMetadataEditing(false)
              setMetadataLabelDraft('')
              setMetadataNoteDraft('')
              setMetadataError(null)
            }}
            onSave={() => void handleMetadataSave()}
          />
        </div>
      </details>

      <details className="watchtower-secondary">
        <summary>生命周期</summary>
        <div className="watchtower-secondary__body">
          <div className="page-stack">
            {isRetiredNode ? <p>{NODE_LIFECYCLE_V1_LIMITATION_COPY}</p> : null}
            <div className="badge-row badge-row--wrap">
              {isRetiredNode ? (
                <button
                  type="button"
                  disabled={lifecycleSubmitting !== null}
                  onClick={() => void handleLifecycleAction('restore-to-observing')}
                >
                  {lifecycleSubmitting === 'restore-to-observing' ? '正在恢复…' : '恢复到观察中'}
                </button>
              ) : (
                <button
                  type="button"
                  disabled={lifecycleSubmitting !== null}
                  onClick={() => {
                    setShowRetireConfirmation(true)
                    setLifecycleError(null)
                  }}
                >
                  退役节点
                </button>
              )}
            </div>
            {!isRetiredNode && showRetireConfirmation ? (
              <div className="page-stack">
                <p>退役会让节点退出当前工作集，但会保留历史记录。这不是删除，也不会清空事件、观测记录或 agent 绑定历史。</p>
                <div className="badge-row badge-row--wrap">
                  <button
                    type="button"
                    disabled={lifecycleSubmitting !== null}
                    onClick={() => void handleLifecycleAction('retire')}
                  >
                    {lifecycleSubmitting === 'retire' ? '正在退役…' : '确认退役'}
                  </button>
                  <button
                    type="button"
                    disabled={lifecycleSubmitting !== null}
                    onClick={() => {
                      setShowRetireConfirmation(false)
                      setLifecycleError(null)
                    }}
                  >
                    取消
                  </button>
                </div>
              </div>
            ) : null}
            {lifecycleError ? <p role="alert">{lifecycleError}</p> : null}
          </div>
        </div>
      </details>

      <details className="watchtower-secondary">
        <summary>接入凭证状态</summary>
        <div className="watchtower-secondary__body">
          <p>
            当前绑定状态：<StatusBadge label={node.binding_status} />
          </p>
          <p>
            <Link className="text-link" to={`/nodes/${node.node_id}/onboarding`}>
              查看接入工作台 →
            </Link>
          </p>
        </div>
      </details>

      <p className="watchtower-snapshot-meta">
        数据快照时间：<Timestamp value={new Date().toISOString()} mode="absolute" />
        ，刷新页面获取最新。
      </p>

      <Drawer
        open={historyOpen}
        onClose={() => setHistoryOpen(false)}
        title={`${node.display_name} · 历史`}
        ariaLabel="节点历史抽屉"
      >
        <Tabs<'events' | 'incidents'>
          variant="pill"
          value={historyTab}
          onChange={setHistoryTab}
          items={[
            { value: 'events', label: '事件时间线' },
            { value: 'incidents', label: '历史异常' },
          ]}
        />
        {historyTab === 'events' ? (
          eventsError ? (
            <div className="empty-state">
              <h3>事件时间线暂不可用</h3>
              <p>{eventsError}</p>
            </div>
          ) : events.length === 0 ? (
            <div className="empty-state">
              <h3>近期无状态变更事件</h3>
              <p>该节点近期没有发生过被记录的状态变更事件。</p>
            </div>
          ) : (
            <EventList events={events} />
          )
        ) : historyIncidentsLoading ? (
          <p>正在加载历史异常…</p>
        ) : historyIncidentsError ? (
          <Card cardRole="warning">
            <p>
              加载历史异常失败：<MonoDigits>{historyIncidentsError}</MonoDigits>
            </p>
            <Button variant="secondary" size="sm" onClick={retryHistoryIncidents}>
              重试
            </Button>
          </Card>
        ) : historyIncidents && historyIncidents.length > 0 ? (
          <IncidentList
            incidents={historyIncidents}
            emptyTitle="近期无异常发生"
            emptyDescription="该节点近期没有触发过被记录的异常。"
          />
        ) : (
          <div className="empty-state">
            <h3>近期无异常发生</h3>
            <p>该节点近期没有触发过被记录的异常。</p>
          </div>
        )}
      </Drawer>
    </div>
  )
}

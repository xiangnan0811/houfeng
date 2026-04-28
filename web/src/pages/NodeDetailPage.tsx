import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { ActionConfirmationCard } from '../components/ActionConfirmationCard'
import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import { IncidentList } from '../components/IncidentList'
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
  listIncidents,
  pauseNodeMonitoring,
  rejectPendingNodeBinding,
  resetNodeBinding,
  restoreRetiredNodeToObserving,
  retireNode,
  resumeNodeMonitoring,
  updateNodeMetadata,
} from '../lib/api'
import {
  formatBytes,
  formatBytesPerSecond,
  formatDateTime,
  formatLabelList,
  formatNumber,
  formatPercent,
  formatUptime,
} from '../lib/format'
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

type NodeRuntimeAction = 'enter-maintenance' | 'exit-maintenance' | 'pause' | 'resume'
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

  const missingNodeId = !nodeId
  const isCurrentNode = state.requestedNodeId === nodeId
  const hasCurrentActivity = state.requestedActivityNodeId === nodeId
  const error = isCurrentNode ? state.error : null
  const node = isCurrentNode ? state.node : null
  const runtimeFacts = isCurrentNode ? state.runtimeFacts : null
  const incidents = hasCurrentActivity ? state.incidents : []
  const incidentsError = hasCurrentActivity ? state.incidentsError : null
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
        node: updated,
      }))
      pendingFocusRestoreRef.current = action
      setPendingRuntimeConfirmation((current) => (current?.action === action ? null : current))
    } catch (error) {
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
        node: updated,
      }))
      setShowRetireConfirmation(false)
    } catch (error) {
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
        node: onboarding,
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
    if (!node) return
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
        <p className="page-panel__eyebrow">Node Detail</p>
        <h2 className="page-panel__title">节点详情不可用</h2>
        <p className="page-panel__description">{error ?? '未找到节点'}</p>
        <Link className="text-link" to="/nodes">
          返回节点列表
        </Link>
      </section>
    )
  }

  const sample = runtimeFacts?.latest_host_sample ?? null
  const showBindingConflict = node.binding_status === NODE_BINDING_CONFLICT_STATUS
  const isRetiredNode = node.lifecycle_status === NODE_LIFECYCLE_RETIRED
  const hasCurrentBindingConflictState = bindingConflictState.requestedNodeId === nodeId
  const bindingConflict = hasCurrentBindingConflictState ? bindingConflictState.onboarding : null
  const bindingConflictError = hasCurrentBindingConflictState ? bindingConflictState.error : null
  const bindingConflictLoading =
    hasCurrentBindingConflictState && bindingConflictState.loading && !bindingConflict

  async function handleMetadataSave() {
    if (!node) return

    const actionNodeId = node.node_id
    setMetadataSubmitting(true)
    setMetadataError(null)

    try {
      const updated = await updateNodeMetadata(actionNodeId, {
        labels: parseLabels(metadataLabelDraft),
        note: metadataNoteDraft.trim(),
      })
      if (
        !isMountedRef.current ||
        currentRouteNodeIdRef.current !== actionNodeId ||
        currentRequestedNodeIdRef.current !== actionNodeId
      ) {
        return
      }
      setState((current) => ({
        ...current,
        node: updated,
      }))
      setMetadataEditing(false)
      setMetadataLabelDraft('')
      setMetadataNoteDraft('')
    } catch (error) {
      if (
        !isMountedRef.current ||
        currentRouteNodeIdRef.current !== actionNodeId ||
        currentRequestedNodeIdRef.current !== actionNodeId
      ) {
        return
      }
      setMetadataError('标签或备注更新失败')
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
      <section className="hero-panel">
        <div className="hero-panel__content">
          <p className="hero-panel__eyebrow">Node Detail</p>
          <h2 className="hero-panel__title">{node.display_name}</h2>
          <p className="hero-panel__description">
            {node.region} · {node.city} · {node.provider}
          </p>
          <div className="badge-row">
            <StatusBadge label={node.lifecycle_status} />
            <StatusBadge label={node.monitoring_status} />
            <StatusBadge label={node.binding_status} />
            <StatusBadge label={node.current_health_status} />
          </div>
        </div>
        <div className="hero-panel__meta">
          <div className="hero-meta-card">
            <span>标签</span>
            <strong>{formatLabelList(node.labels)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>最近心跳</span>
            <strong>{formatDateTime(node.last_heartbeat_at)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>最近同步</span>
            <strong>{formatDateTime(node.last_sync_at)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>当前主问题</span>
            <strong>{node.current_primary_issue_summary || '暂无明显异常'}</strong>
          </div>
        </div>
      </section>

      {showBindingConflict ? (
        <DetailSection eyebrow="Binding Conflict" title="绑定冲突处置" aside="高优先级">
          <article className="metric-card" aria-label="高优先级：绑定冲突待处理">
            <h3>高优先级：绑定冲突待处理</h3>
            <p>同一台机器重装或合法替换 agent 后，通常会出现新的指纹接入请求。请先核对这次变更。</p>
            <dl>
              <div>
                <dt>当前已绑定指纹</dt>
                <dd>{currentFingerprintSummary(bindingConflict)}</dd>
              </div>
              <div>
                <dt>待确认指纹</dt>
                <dd>{maskFingerprint(pendingBindingMetadata(bindingConflict)?.fingerprint)}</dd>
              </div>
              <div>
                <dt>首次出现</dt>
                <dd>{formatDateTime(pendingBindingMetadata(bindingConflict)?.first_seen_at)}</dd>
              </div>
              <div>
                <dt>最近出现</dt>
                <dd>{formatDateTime(pendingBindingMetadata(bindingConflict)?.last_seen_at)}</dd>
              </div>
              <div>
                <dt>尝试次数</dt>
                <dd>{pendingBindingMetadata(bindingConflict)?.attempt_count ?? 0}</dd>
              </div>
            </dl>
            {bindingConflictLoading ? <p>正在加载绑定冲突详情…</p> : null}
            {bindingConflictError ? <p role="alert">{bindingConflictError}</p> : null}
            <div className="badge-row badge-row--wrap">
              <button
                type="button"
                disabled={bindingAction !== null || bindingConflictLoading}
                onClick={() => void handleBindingAction('confirm', confirmNodeRebind)}
              >
                {bindingAction === 'confirm' ? '正在确认…' : NODE_BINDING_CONFIRM_REBIND_LABEL}
              </button>
              <button
                type="button"
                disabled={bindingAction !== null || bindingConflictLoading}
                onClick={() => void handleBindingAction('reject', rejectPendingNodeBinding)}
              >
                {bindingAction === 'reject' ? '正在拒绝…' : NODE_BINDING_REJECT_PENDING_LABEL}
              </button>
              <button
                type="button"
                disabled={bindingAction !== null || bindingConflictLoading}
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

      <div className="summary-grid">
        <article className="summary-card">
          <p className="summary-card__label">健康状态</p>
          <p className="summary-card__value">{node.current_health_status}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">活跃异常数</p>
          <p className="summary-card__value">{node.current_active_incident_count}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">当前主问题</p>
          <p className="summary-card__value summary-card__value--text">
            {node.current_primary_issue_summary || '暂无明显异常'}
          </p>
        </article>
      </div>

      <DetailSection eyebrow="Metadata" title="标签与备注">
        <div className="page-stack">
          {metadataEditing ? (
            <>
              <p>
                <label>
                  标签
                  <input
                    name="metadata-labels"
                    value={metadataLabelDraft}
                    onChange={(event) => setMetadataLabelDraft(event.target.value)}
                  />
                </label>
              </p>
              <p>
                <label>
                  备注
                  <textarea
                    name="metadata-note"
                    value={metadataNoteDraft}
                    onChange={(event) => setMetadataNoteDraft(event.target.value)}
                    rows={3}
                  />
                </label>
              </p>
              <div className="badge-row badge-row--wrap">
                <button
                  type="button"
                  disabled={metadataSubmitting}
                  onClick={() => void handleMetadataSave()}
                >
                  {metadataSubmitting ? '正在保存…' : '保存标签与备注'}
                </button>
                <button
                  type="button"
                  disabled={metadataSubmitting}
                  onClick={() => {
                    setMetadataEditing(false)
                    setMetadataLabelDraft('')
                    setMetadataNoteDraft('')
                    setMetadataError(null)
                  }}
                >
                  取消
                </button>
              </div>
            </>
          ) : (
            <>
              <p>标签：{formatLabelList(node.labels)}</p>
              <p>备注：{node.note.trim() || '—'}</p>
              <div>
                <button
                  type="button"
                  onClick={() => {
                    setMetadataEditing(true)
                    setMetadataLabelDraft(node.labels.join(', '))
                    setMetadataNoteDraft(node.note)
                    setMetadataError(null)
                  }}
                >
                  编辑标签与备注
                </button>
              </div>
            </>
          )}
          {metadataError ? <p role="alert">{metadataError}</p> : null}
        </div>
      </DetailSection>

      <DetailSection eyebrow="Runtime Control" title="运行控制">
        <div className="page-stack">
          <p>维护会继续采集，但不解释结果。暂停会停止采集并产生数据空档。</p>
          <div className="badge-row badge-row--wrap">
            {nodeRuntimeActions(node).map(({ action, label }) => (
              <button
                key={action}
                ref={(element) => {
                  actionButtonRefs.current[action] = element
                }}
                type="button"
                disabled={runtimeSubmitting}
                onClick={() => void handleRuntimeAction(action)}
              >
                {label}
              </button>
            ))}
          </div>
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
          {runtimeError ? <p>{runtimeError}</p> : null}
        </div>
      </DetailSection>

      <DetailSection eyebrow="Lifecycle" title="生命周期">
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
              <p>
                退役会让节点退出当前工作集，但会保留历史记录。这不是删除，也不会清空事件、
                observation 或 agent 绑定历史。
              </p>
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
      </DetailSection>

      <DetailSection
        eyebrow="Current Runtime Facts"
        title="当前主机指标"
        aside={sample ? `样本时间：${formatDateTime(sample.observed_at)}` : '等待首批样本'}
      >
        {sample ? (
          <div className="metric-grid">
            <article className="metric-card">
              <h3>CPU / Load</h3>
              <dl>
                <div>
                  <dt>CPU 使用率</dt>
                  <dd>{formatPercent(sample.cpu_usage_pct)}</dd>
                </div>
                <div>
                  <dt>Load 1 / 5 / 15</dt>
                  <dd>
                    {formatNumber(sample.load_1)} / {formatNumber(sample.load_5)} /{' '}
                    {formatNumber(sample.load_15)}
                  </dd>
                </div>
                <div>
                  <dt>iowait / steal</dt>
                  <dd>
                    {formatPercent(sample.cpu_iowait_pct)} /{' '}
                    {formatPercent(sample.cpu_steal_pct)}
                  </dd>
                </div>
              </dl>
            </article>

            <article className="metric-card">
              <h3>内存 / Swap</h3>
              <dl>
                <div>
                  <dt>内存使用率</dt>
                  <dd>{formatPercent(sample.mem_used_pct)}</dd>
                </div>
                <div>
                  <dt>可用内存</dt>
                  <dd>{formatBytes(sample.mem_available_bytes)}</dd>
                </div>
                <div>
                  <dt>Swap 使用率</dt>
                  <dd>{formatPercent(sample.swap_used_pct)}</dd>
                </div>
              </dl>
            </article>

            <article className="metric-card">
              <h3>磁盘 / Inode</h3>
              <dl>
                <div>
                  <dt>磁盘使用率</dt>
                  <dd>{formatPercent(sample.disk_used_pct)}</dd>
                </div>
                <div>
                  <dt>Inode 使用率</dt>
                  <dd>{formatPercent(sample.inode_used_pct)}</dd>
                </div>
                <div>
                  <dt>磁盘繁忙度</dt>
                  <dd>{formatPercent(sample.disk_busy_pct)}</dd>
                </div>
              </dl>
            </article>

            <article className="metric-card">
              <h3>网络 / 吞吐</h3>
              <dl>
                <div>
                  <dt>流入 / 流出</dt>
                  <dd>
                    {formatBytesPerSecond(sample.net_in_bytes_per_sec)} /{' '}
                    {formatBytesPerSecond(sample.net_out_bytes_per_sec)}
                  </dd>
                </div>
                <div>
                  <dt>磁盘读 / 写</dt>
                  <dd>
                    {formatBytesPerSecond(sample.disk_read_bytes_per_sec)} /{' '}
                    {formatBytesPerSecond(sample.disk_write_bytes_per_sec)}
                  </dd>
                </div>
                <div>
                  <dt>运行时长</dt>
                  <dd>{formatUptime(sample.uptime_seconds)}</dd>
                </div>
              </dl>
            </article>
          </div>
        ) : (
          <div className="empty-state">
            <h3>尚未收到主机样本</h3>
            <p>该节点已存在，但首批 HostSample 还未到达。请等待下一次 agent 同步。</p>
          </div>
        )}
      </DetailSection>

      <DetailSection eyebrow="Incidents" title="当前活跃异常">
        {!hasCurrentActivity ? (
          <div className="empty-state">
            <h3>正在加载活跃异常…</h3>
            <p>等待节点相关的 incident 读模型返回最新结果。</p>
          </div>
        ) : incidentsError ? (
          <div className="empty-state">
            <h3>活跃异常暂不可用</h3>
            <p>{incidentsError}</p>
          </div>
        ) : (
          <IncidentList incidents={incidents} />
        )}
      </DetailSection>

      <DetailSection eyebrow="Events" title="最近相关事件">
        {!hasCurrentActivity ? (
          <div className="empty-state">
            <h3>正在加载相关事件…</h3>
            <p>等待节点相关的事件流返回最新记录。</p>
          </div>
        ) : eventsError ? (
          <div className="empty-state">
            <h3>相关事件暂不可用</h3>
            <p>{eventsError}</p>
          </div>
        ) : (
          <EventList events={events} />
        )}
      </DetailSection>
    </div>
  )
}

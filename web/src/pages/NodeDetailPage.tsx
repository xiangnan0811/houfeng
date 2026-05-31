import { useEffect, useRef, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'

import type { NodeRuntimeAction } from '../components/node-detail'
import {
  ApiError,
  confirmNodeRebind,
  enterNodeMaintenance,
  exitNodeMaintenance,
  getNode,
  getNodeOnboarding,
  getNodeRuntimeFacts,
  listVPSForNode,
  listEvents,
  listHistoricalIncidents,
  listIncidents,
  pauseNodeMonitoring,
  postNodeAction,
  rejectPendingNodeBinding,
  resetNodeBinding,
  restoreRetiredNodeToObserving,
  retireNode,
  resumeNodeMonitoring,
} from '../lib/api'
import type { ActiveIncidentRecord, NodeOnboardingState } from '../lib/types'
import { NodeDetailPageBody } from './node-detail/NodeDetailPageBody'
import { NodeDetailLoading } from './node-detail/NodeDetailLoading'
import { NodeDetailUnavailable } from './node-detail/NodeDetailUnavailable'
import {
  NODE_BINDING_ACTION_ERROR,
  NODE_BINDING_CONFLICT_LOAD_ERROR,
  NODE_BINDING_CONFLICT_STATUS,
  NODE_LIFECYCLE_ACTION_ERROR,
} from './node-detail/nodeDetailConstants'
import {
  INITIAL_NODE_DETAIL_STATE,
  applyOnboardingRecordToNode,
  describeError,
  mergeNonMetadataNodeRecord,
} from './node-detail/nodeDetailHelpers'
import type {
  BindingConflictAction,
  BindingConflictState,
  HistoryTab,
  LinkedVPSState,
  NodeDetailPageState,
  NodeLifecycleAction,
  PendingRuntimeConfirmation,
  TimeWindow,
} from './node-detail/types'

export function NodeDetailPage() {
  const { nodeId } = useParams()
  return <NodeDetailPageContent key={nodeId ?? 'missing-node'} nodeId={nodeId} />
}

function NodeDetailPageContent({ nodeId }: { nodeId?: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const [state, setState] = useState<NodeDetailPageState>(INITIAL_NODE_DETAIL_STATE)
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
  const [historyOpen, setHistoryOpen] = useState(false)
  const [historyTab, setHistoryTab] = useState<HistoryTab>('events')
  const [historyIncidents, setHistoryIncidents] = useState<ActiveIncidentRecord[] | null>(null)
  const [historyIncidentsLoading, setHistoryIncidentsLoading] = useState(false)
  const [historyIncidentsError, setHistoryIncidentsError] = useState<string | null>(null)
  const [commandOpen, setCommandOpen] = useState(false)
  const [commandSubmitting, setCommandSubmitting] = useState(false)
  const [commandError, setCommandError] = useState<string | null>(null)
  const [onboardingOpen, setOnboardingOpen] = useState(false)
  const [timeWindow, setTimeWindow] = useState<TimeWindow>('24h')
  const [linkedVPSState, setLinkedVPSState] = useState<LinkedVPSState>({
    requestedNodeId: null,
    records: [],
    loading: false,
    loaded: false,
    error: null,
  })
  const [linkedVPSVisible, setLinkedVPSVisible] = useState(false)
  const linkedVPSSectionRef = useRef<HTMLDivElement | null>(null)
  const linkedVPSFetchRef = useRef({
    nodeId: null as string | null,
    inFlight: false,
    fetched: false,
    requestId: 0,
  })
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

  // Deep-link: create/list redirects land here with ?onboarding=1 to open the
  // onboarding drawer directly. Consume the param so a refresh/back doesn't reopen it.
  useEffect(() => {
    if (searchParams.get('onboarding') !== '1') return
    setOnboardingOpen(true)
    const next = new URLSearchParams(searchParams)
    next.delete('onboarding')
    setSearchParams(next, { replace: true })
  }, [searchParams, setSearchParams])

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

  // Refetch runtime facts when time window changes (keep old data visible).
  // The mounted ref skips the initial invocation so the main load effect handles
  // the first fetch. It resets on nodeId change for the same reason.
  const timeWindowMountedRef = useRef(false)
  useEffect(() => {
    timeWindowMountedRef.current = false
  }, [nodeId])

  useEffect(() => {
    if (!timeWindowMountedRef.current) {
      timeWindowMountedRef.current = true
      return
    }

    let cancelled = false
    if (!nodeId) return

    getNodeRuntimeFacts(nodeId, timeWindow)
      .then((runtimeFacts) => {
        if (cancelled) return
        setState((current) => {
          if (current.requestedNodeId !== nodeId) return current
          return { ...current, runtimeFacts }
        })
      })
      .catch(() => {
        // On window-switch fetch error, keep old data visible — no-op.
      })

    return () => { cancelled = true }
  }, [nodeId, timeWindow])

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
    linkedVPSFetchRef.current = {
      nodeId: nodeId ?? null,
      inFlight: false,
      fetched: false,
      requestId: linkedVPSFetchRef.current.requestId + 1,
    }
    setLinkedVPSVisible(false)
    setLinkedVPSState({
      requestedNodeId: null,
      records: [],
      loading: false,
      loaded: false,
      error: null,
    })
  }, [nodeId])

  useEffect(() => {
    if (linkedVPSVisible) return
    const element = linkedVPSSectionRef.current
    if (!element) return
    if (typeof IntersectionObserver === 'undefined') return

    const observer = new IntersectionObserver((entries) => {
      if (!entries.some((entry) => entry.isIntersecting)) return
      setLinkedVPSVisible(true)
      observer.disconnect()
    }, { rootMargin: '160px' })

    observer.observe(element)
    return () => {
      observer.disconnect()
    }
  }, [linkedVPSVisible, state.node, state.requestedNodeId])

  useEffect(() => {
    if (!nodeId) {
      setLinkedVPSState({
        requestedNodeId: null,
        records: [],
        loading: false,
        loaded: false,
        error: null,
      })
      return
    }
    if (!linkedVPSVisible) return
    if (state.requestedNodeId !== nodeId || !state.node) return

    if (linkedVPSFetchRef.current.nodeId !== nodeId) {
      linkedVPSFetchRef.current = {
        nodeId,
        inFlight: false,
        fetched: false,
        requestId: linkedVPSFetchRef.current.requestId + 1,
      }
    }
    if (linkedVPSFetchRef.current.inFlight || linkedVPSFetchRef.current.fetched) return

    const requestId = linkedVPSFetchRef.current.requestId + 1
    linkedVPSFetchRef.current = {
      nodeId,
      inFlight: true,
      fetched: false,
      requestId,
    }

    setLinkedVPSState((current) => ({
      requestedNodeId: nodeId,
      records: current.requestedNodeId === nodeId ? current.records : [],
      loading: true,
      loaded: false,
      error: null,
    }))

    listVPSForNode(nodeId)
      .then((records) => {
        const fetchState = linkedVPSFetchRef.current
        if (
          !isMountedRef.current ||
          currentRouteNodeIdRef.current !== nodeId ||
          currentRequestedNodeIdRef.current !== nodeId ||
          fetchState.nodeId !== nodeId ||
          fetchState.requestId !== requestId
        ) return
        linkedVPSFetchRef.current = { nodeId, inFlight: false, fetched: true, requestId }
        setLinkedVPSState({
          requestedNodeId: nodeId,
          records: Array.isArray(records) ? records : [],
          loading: false,
          loaded: true,
          error: null,
        })
      })
      .catch((error: unknown) => {
        const fetchState = linkedVPSFetchRef.current
        if (
          !isMountedRef.current ||
          currentRouteNodeIdRef.current !== nodeId ||
          currentRequestedNodeIdRef.current !== nodeId ||
          fetchState.nodeId !== nodeId ||
          fetchState.requestId !== requestId
        ) return
        linkedVPSFetchRef.current = { nodeId, inFlight: false, fetched: true, requestId }
        setLinkedVPSState({
          requestedNodeId: nodeId,
          records: [],
          loading: false,
          loaded: true,
          error: describeError(error, '加载关联 VPS 失败'),
        })
      })
  }, [linkedVPSVisible, nodeId, state.node, state.requestedNodeId])

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

  // Poll node data while command drawer is open and a pending action is
  // in-flight, so the user sees the result without a manual page refresh.
  useEffect(() => {
    if (!commandOpen || !nodeId) return
    if (state.node?.last_action?.status !== 'pending') return

    let cancelled = false
    const interval = setInterval(() => {
      if (cancelled) return
      getNode(nodeId)
        .then((updated) => {
          if (cancelled) return
          setState((prev) =>
            prev.requestedNodeId === nodeId && prev.node
              ? { ...prev, node: mergeNonMetadataNodeRecord(prev.node, updated) }
              : prev,
          )
        })
        .catch(() => {
          // Silent — polling failures should not surface to the user.
        })
    }, 3000)

    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [commandOpen, state.node?.last_action?.status, nodeId])

  const missingNodeId = !nodeId
  const isCurrentNode = state.requestedNodeId === nodeId
  const hasCurrentActivity = state.requestedActivityNodeId === nodeId
  const error = isCurrentNode ? state.error : null
  const node = isCurrentNode ? state.node : null
  const runtimeFacts = isCurrentNode ? state.runtimeFacts : null
  const incidents = hasCurrentActivity ? state.incidents : []
  const events = hasCurrentActivity ? state.events : []
  const eventsError = hasCurrentActivity ? state.eventsError : null
  const linkedVPS =
    linkedVPSState.requestedNodeId === nodeId ? linkedVPSState.records : []
  const linkedVPSLoading =
    linkedVPSState.requestedNodeId === nodeId ? linkedVPSState.loading : false
  const linkedVPSError =
    linkedVPSState.requestedNodeId === nodeId ? linkedVPSState.error : null
  const linkedVPSLoaded =
    linkedVPSState.requestedNodeId === nodeId ? linkedVPSState.loaded : false

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
        node: applyOnboardingRecordToNode(current.node, onboarding),
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
    return <NodeDetailLoading />
  }

  if (missingNodeId || error || !node) {
    return <NodeDetailUnavailable message={error ?? '未找到节点'} />
  }

  const hasCurrentBindingConflictState = bindingConflictState.requestedNodeId === nodeId
  const bindingConflict = hasCurrentBindingConflictState ? bindingConflictState.onboarding : null
  const bindingConflictError = hasCurrentBindingConflictState ? bindingConflictState.error : null
  const bindingConflictLoading =
    hasCurrentBindingConflictState && bindingConflictState.loading && !bindingConflict

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

  function handleHistoryTabChange(tab: HistoryTab) {
    setHistoryTab(tab)
  }

  function handleTimeWindowChange(nextTimeWindow: TimeWindow) {
    setTimeWindow(nextTimeWindow)
  }

  function startRetireConfirmation() {
    setShowRetireConfirmation(true)
    setLifecycleError(null)
  }

  function cancelRetireConfirmation() {
    setShowRetireConfirmation(false)
    setLifecycleError(null)
  }

  function openCommandDrawer() {
    setCommandOpen(true)
    setCommandError(null)
  }

  function closeCommandDrawer() {
    setCommandOpen(false)
    setCommandError(null)
  }

  function openOnboardingDrawer() {
    setOnboardingOpen(true)
  }

  function closeOnboardingDrawer() {
    setOnboardingOpen(false)
  }

  function closeHistoryDrawer() {
    setHistoryOpen(false)
  }

  async function handleCommandExecute(cmdId: string) {
    if (!node) return
    const actionNodeId = node.node_id
    setCommandSubmitting(true)
    setCommandError(null)

    try {
      const action = await postNodeAction(actionNodeId, cmdId)
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
                last_action: {
                  action_id: action.action_id,
                  command_id: action.command_id,
                  status: action.status,
                },
              }
            : current.node,
      }))
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteNodeIdRef.current !== actionNodeId ||
        currentRequestedNodeIdRef.current !== actionNodeId
      ) {
        return
      }
      setCommandError(describeError(error, '下发命令失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteNodeIdRef.current === actionNodeId &&
        currentRequestedNodeIdRef.current === actionNodeId
      ) {
        setCommandSubmitting(false)
      }
    }
  }

  return (
    <NodeDetailPageBody
      node={node}
      runtimeFacts={runtimeFacts}
      runtimeSubmitting={runtimeSubmitting}
      runtimeError={runtimeError}
      pendingRuntimeConfirmation={pendingRuntimeConfirmation}
      onRuntimeAction={(action, confirmed) => void handleRuntimeAction(action, confirmed)}
      onCancelRuntimeConfirmation={() => {
        pendingFocusRestoreRef.current = 'pause'
        setPendingRuntimeConfirmation(null)
      }}
      registerActionRef={registerActionRef}
      incidents={incidents}
      events={events}
      eventsError={eventsError}
      linkedVPSSectionRef={linkedVPSSectionRef}
        linkedVPS={linkedVPS}
        linkedVPSLoading={linkedVPSLoading}
        linkedVPSLoaded={linkedVPSLoaded}
        linkedVPSError={linkedVPSError}
        bindingConflict={bindingConflict}
      bindingConflictLoading={bindingConflictLoading}
      bindingConflictError={bindingConflictError}
      bindingAction={bindingAction}
      onBindingConfirm={() => void handleBindingAction('confirm', confirmNodeRebind)}
      onBindingReject={() => void handleBindingAction('reject', rejectPendingNodeBinding)}
      onBindingReset={() => void handleBindingAction('reset', resetNodeBinding)}
      timeWindow={timeWindow}
      onTimeWindowChange={handleTimeWindowChange}
      showRetireConfirmation={showRetireConfirmation}
      lifecycleSubmitting={lifecycleSubmitting}
      lifecycleError={lifecycleError}
      onLifecycleRestore={() => void handleLifecycleAction('restore-to-observing')}
      onStartRetire={startRetireConfirmation}
      onConfirmRetire={() => void handleLifecycleAction('retire')}
      onCancelRetire={cancelRetireConfirmation}
      historyOpen={historyOpen}
      historyTab={historyTab}
      historyIncidents={historyIncidents}
      historyIncidentsLoading={historyIncidentsLoading}
      historyIncidentsError={historyIncidentsError}
      onOpenHistory={openHistory}
      onCloseHistory={closeHistoryDrawer}
      onHistoryTabChange={handleHistoryTabChange}
      onRetryHistoryIncidents={retryHistoryIncidents}
      commandOpen={commandOpen}
      commandSubmitting={commandSubmitting}
      commandError={commandError}
      onOpenCommands={openCommandDrawer}
      onCloseCommand={closeCommandDrawer}
      onExecuteCommand={(commandId) => void handleCommandExecute(commandId)}
      onboardingOpen={onboardingOpen}
      onOpenOnboarding={openOnboardingDrawer}
      onCloseOnboarding={closeOnboardingDrawer}
    />
  )
}

import { useEffect, useRef, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'

import type { MonitoringInstanceRuntimeAction } from '../components/monitoring-detail'
import {
  ApiError,
  confirmMonitoringInstanceRebind,
  enterMonitoringInstanceMaintenance,
  exitMonitoringInstanceMaintenance,
  getMonitoringInstance,
  getMonitoringInstanceOnboarding,
  getMonitoringInstanceRuntimeFacts,
  listVPSForMonitoringInstance,
  listEvents,
  listHistoricalIncidents,
  listIncidents,
  pauseMonitoringInstanceMonitoring,
  postMonitoringInstanceAction,
  rejectPendingMonitoringInstanceBinding,
  resetMonitoringInstanceBinding,
  restoreRetiredMonitoringInstanceToObserving,
  retireMonitoringInstance,
  resumeMonitoringInstanceMonitoring,
} from '../lib/api'
import type { ActiveIncidentRecord, MonitoringInstanceOnboardingState } from '../lib/types'
import { MonitoringDetailPageBody } from './monitoring-detail/MonitoringDetailPageBody'
import { MonitoringDetailLoading } from './monitoring-detail/MonitoringDetailLoading'
import { MonitoringDetailUnavailable } from './monitoring-detail/MonitoringDetailUnavailable'
import {
  MONITORING_INSTANCE_BINDING_ACTION_ERROR,
  MONITORING_INSTANCE_BINDING_CONFLICT_LOAD_ERROR,
  MONITORING_INSTANCE_BINDING_CONFLICT_STATUS,
  MONITORING_INSTANCE_LIFECYCLE_ACTION_ERROR,
} from './monitoring-detail/monitoringDetailConstants'
import {
  INITIAL_MONITORING_DETAIL_STATE,
  applyOnboardingRecordToMonitoringInstance,
  describeError,
  mergeNonMetadataMonitoringInstanceRecord,
} from './monitoring-detail/monitoringDetailHelpers'
import type {
  BindingConflictAction,
  BindingConflictState,
  HistoryTab,
  LinkedVPSState,
  MonitoringDetailPageState,
  MonitoringInstanceLifecycleAction,
  PendingRuntimeConfirmation,
  TimeWindow,
} from './monitoring-detail/types'

export function MonitoringDetailPage() {
  const { monitoringInstanceId } = useParams()
  return (
    <MonitoringDetailPageContent
      key={monitoringInstanceId ?? 'missing-monitoring-instance'}
      monitoringInstanceId={monitoringInstanceId}
    />
  )
}

function MonitoringDetailPageContent({ monitoringInstanceId }: { monitoringInstanceId?: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const [state, setState] = useState<MonitoringDetailPageState>(INITIAL_MONITORING_DETAIL_STATE)
  const [runtimeSubmitting, setRuntimeSubmitting] = useState(false)
  const [runtimeError, setRuntimeError] = useState<string | null>(null)
  const [pendingRuntimeConfirmation, setPendingRuntimeConfirmation] =
    useState<PendingRuntimeConfirmation | null>(null)
  const [lifecycleSubmitting, setLifecycleSubmitting] = useState<MonitoringInstanceLifecycleAction | null>(null)
  const [lifecycleError, setLifecycleError] = useState<string | null>(null)
  const [showRetireConfirmation, setShowRetireConfirmation] = useState(false)
  const [bindingConflictState, setBindingConflictState] = useState<BindingConflictState>({
    requestedMonitoringInstanceId: null,
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
    requestedMonitoringInstanceId: null,
    records: [],
    loading: false,
    loaded: false,
    error: null,
  })
  const [linkedVPSVisible, setLinkedVPSVisible] = useState(false)
  const linkedVPSSectionRef = useRef<HTMLDivElement | null>(null)
  const linkedVPSFetchRef = useRef({
    monitoringInstanceId: null as string | null,
    inFlight: false,
    fetched: false,
    requestId: 0,
  })
  const currentRouteMonitoringInstanceIdRef = useRef<string | null>(monitoringInstanceId ?? null)
  const currentRequestedMonitoringInstanceIdRef = useRef<string | null>(null)
  const isMountedRef = useRef(true)
  const actionButtonRefs = useRef<Record<MonitoringInstanceRuntimeAction, HTMLButtonElement | null>>({
    'enter-maintenance': null,
    'exit-maintenance': null,
    pause: null,
    resume: null,
  })
  const pendingFocusRestoreRef = useRef<MonitoringInstanceRuntimeAction | null>(null)

  useEffect(() => {
    currentRouteMonitoringInstanceIdRef.current = monitoringInstanceId ?? null
  }, [monitoringInstanceId])

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
    currentRequestedMonitoringInstanceIdRef.current = state.requestedMonitoringInstanceId
  }, [state.requestedMonitoringInstanceId])

  useEffect(() => {
    if (pendingRuntimeConfirmation) return

    const action = pendingFocusRestoreRef.current
    if (!action) return

    const preferred = actionButtonRefs.current[action]
    const fallback = action === 'pause' ? actionButtonRefs.current.resume : null
    const target = [preferred, fallback].find((element) => element?.isConnected)

    target?.focus()
    pendingFocusRestoreRef.current = null
  }, [pendingRuntimeConfirmation, state.monitoringInstance])

  useEffect(
    () => () => {
      isMountedRef.current = false
    },
    [],
  )

  // Refetch runtime facts when time window changes (keep old data visible).
  // The mounted ref skips the initial invocation so the main load effect handles
  // the first fetch. It resets on monitoringInstanceId change for the same reason.
  const timeWindowMountedRef = useRef(false)
  useEffect(() => {
    timeWindowMountedRef.current = false
  }, [monitoringInstanceId])

  useEffect(() => {
    if (!timeWindowMountedRef.current) {
      timeWindowMountedRef.current = true
      return
    }

    let cancelled = false
    if (!monitoringInstanceId) return

    getMonitoringInstanceRuntimeFacts(monitoringInstanceId, timeWindow)
      .then((runtimeFacts) => {
        if (cancelled) return
        setState((current) => {
          if (current.requestedMonitoringInstanceId !== monitoringInstanceId) return current
          return { ...current, runtimeFacts }
        })
      })
      .catch(() => {
        // On window-switch fetch error, keep old data visible — no-op.
      })

    return () => { cancelled = true }
  }, [monitoringInstanceId, timeWindow])

  useEffect(() => {
    let cancelled = false
    if (!monitoringInstanceId) return

    Promise.all([getMonitoringInstance(monitoringInstanceId), getMonitoringInstanceRuntimeFacts(monitoringInstanceId)])
      .then(([monitoringInstance, runtimeFacts]) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          requestedMonitoringInstanceId: monitoringInstanceId,
          error: null,
          monitoringInstance,
          runtimeFacts,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message =
          error instanceof ApiError && error.status === 404
            ? '监控实例不存在'
            : describeError(error, '加载监控实例详情失败')
        setState((current) => ({
          ...current,
          requestedMonitoringInstanceId: monitoringInstanceId,
          error: message,
          monitoringInstance: null,
          runtimeFacts: null,
        }))
      })

    return () => {
      cancelled = true
    }
  }, [monitoringInstanceId])

  useEffect(() => {
    linkedVPSFetchRef.current = {
      monitoringInstanceId: monitoringInstanceId ?? null,
      inFlight: false,
      fetched: false,
      requestId: linkedVPSFetchRef.current.requestId + 1,
    }
    setLinkedVPSVisible(false)
    setLinkedVPSState({
      requestedMonitoringInstanceId: null,
      records: [],
      loading: false,
      loaded: false,
      error: null,
    })
  }, [monitoringInstanceId])

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
  }, [linkedVPSVisible, state.monitoringInstance, state.requestedMonitoringInstanceId])

  useEffect(() => {
    if (!monitoringInstanceId) {
      setLinkedVPSState({
        requestedMonitoringInstanceId: null,
        records: [],
        loading: false,
        loaded: false,
        error: null,
      })
      return
    }
    if (!linkedVPSVisible) return
    if (state.requestedMonitoringInstanceId !== monitoringInstanceId || !state.monitoringInstance) return

    if (linkedVPSFetchRef.current.monitoringInstanceId !== monitoringInstanceId) {
      linkedVPSFetchRef.current = {
        monitoringInstanceId,
        inFlight: false,
        fetched: false,
        requestId: linkedVPSFetchRef.current.requestId + 1,
      }
    }
    if (linkedVPSFetchRef.current.inFlight || linkedVPSFetchRef.current.fetched) return

    const requestId = linkedVPSFetchRef.current.requestId + 1
    linkedVPSFetchRef.current = {
      monitoringInstanceId,
      inFlight: true,
      fetched: false,
      requestId,
    }

    setLinkedVPSState((current) => ({
      requestedMonitoringInstanceId: monitoringInstanceId,
      records: current.requestedMonitoringInstanceId === monitoringInstanceId ? current.records : [],
      loading: true,
      loaded: false,
      error: null,
    }))

    listVPSForMonitoringInstance(monitoringInstanceId)
      .then((records) => {
        const fetchState = linkedVPSFetchRef.current
        if (
          !isMountedRef.current ||
          currentRouteMonitoringInstanceIdRef.current !== monitoringInstanceId ||
          currentRequestedMonitoringInstanceIdRef.current !== monitoringInstanceId ||
          fetchState.monitoringInstanceId !== monitoringInstanceId ||
          fetchState.requestId !== requestId
        ) return
        linkedVPSFetchRef.current = { monitoringInstanceId, inFlight: false, fetched: true, requestId }
        setLinkedVPSState({
          requestedMonitoringInstanceId: monitoringInstanceId,
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
          currentRouteMonitoringInstanceIdRef.current !== monitoringInstanceId ||
          currentRequestedMonitoringInstanceIdRef.current !== monitoringInstanceId ||
          fetchState.monitoringInstanceId !== monitoringInstanceId ||
          fetchState.requestId !== requestId
        ) return
        linkedVPSFetchRef.current = { monitoringInstanceId, inFlight: false, fetched: true, requestId }
        setLinkedVPSState({
          requestedMonitoringInstanceId: monitoringInstanceId,
          records: [],
          loading: false,
          loaded: true,
          error: describeError(error, '加载关联 VPS 失败'),
        })
      })
  }, [linkedVPSVisible, monitoringInstanceId, state.monitoringInstance, state.requestedMonitoringInstanceId])

  useEffect(() => {
    let cancelled = false
    if (!monitoringInstanceId) {
      setBindingConflictState({
        requestedMonitoringInstanceId: null,
        onboarding: null,
        loading: false,
        error: null,
      })
      return
    }

    if (state.requestedMonitoringInstanceId !== monitoringInstanceId || !state.monitoringInstance) {
      return
    }

    if (state.monitoringInstance.binding_status !== MONITORING_INSTANCE_BINDING_CONFLICT_STATUS) {
      setBindingConflictState({
        requestedMonitoringInstanceId: monitoringInstanceId,
        onboarding: null,
        loading: false,
        error: null,
      })
      return
    }

    setBindingConflictState((current) => ({
      requestedMonitoringInstanceId: monitoringInstanceId,
      onboarding: current.requestedMonitoringInstanceId === monitoringInstanceId ? current.onboarding : null,
      loading: true,
      error: null,
    }))

    getMonitoringInstanceOnboarding(monitoringInstanceId)
      .then((onboarding) => {
        if (cancelled) return
        setBindingConflictState({
          requestedMonitoringInstanceId: monitoringInstanceId,
          onboarding,
          loading: false,
          error: null,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setBindingConflictState({
          requestedMonitoringInstanceId: monitoringInstanceId,
          onboarding: null,
          loading: false,
          error: describeError(error, MONITORING_INSTANCE_BINDING_CONFLICT_LOAD_ERROR),
        })
      })

    return () => {
      cancelled = true
    }
  }, [monitoringInstanceId, state.monitoringInstance, state.requestedMonitoringInstanceId])

  useEffect(() => {
    let cancelled = false
    if (!monitoringInstanceId) return

    Promise.allSettled([
      listIncidents({ object_type: 'monitoring_instance', object_id: monitoringInstanceId }),
      listEvents({ object_type: 'monitoring_instance', object_id: monitoringInstanceId }),
    ]).then(([incidentsResult, eventsResult]) => {
      if (cancelled) return
      setState((current) => ({
        ...current,
        requestedActivityMonitoringInstanceId: monitoringInstanceId,
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
  }, [monitoringInstanceId])

  // Reset historical incidents when navigating between monitoring so the drawer never
  // shows stale data from the previous monitoringInstance when reopened.
  useEffect(() => {
    setHistoryIncidents(null)
    setHistoryIncidentsError(null)
    setHistoryIncidentsLoading(false)
  }, [monitoringInstanceId])

  // Lazy-load historical incidents the first time the user opens the drawer
  // and switches to the "历史异常" tab. Subsequent opens reuse the cached set
  // (cleared on monitoringInstance id change via the reset effect above). We use refs so
  // setState calls inside the effect do not re-trigger it (which would cancel
  // the in-flight promise).
  const historyFetchRef = useRef<{
    monitoringInstanceId: string | null
    inFlight: boolean
    fetched: boolean
  }>({ monitoringInstanceId: null, inFlight: false, fetched: false })

  useEffect(() => {
    if (historyFetchRef.current.monitoringInstanceId !== monitoringInstanceId) {
      historyFetchRef.current = { monitoringInstanceId: monitoringInstanceId ?? null, inFlight: false, fetched: false }
    }
  }, [monitoringInstanceId])

  const wantsHistoryIncidents = historyOpen && historyTab === 'incidents'

  useEffect(() => {
    if (!monitoringInstanceId) return
    if (!wantsHistoryIncidents) return
    if (historyFetchRef.current.inFlight || historyFetchRef.current.fetched) return

    let cancelled = false
    const targetMonitoringInstanceId = monitoringInstanceId
    historyFetchRef.current = { monitoringInstanceId: targetMonitoringInstanceId, inFlight: true, fetched: false }
    setHistoryIncidentsLoading(true)
    setHistoryIncidentsError(null)

    listHistoricalIncidents('monitoring_instance', targetMonitoringInstanceId)
      .then((records) => {
        if (cancelled) return
        setHistoryIncidents(records)
        historyFetchRef.current = { monitoringInstanceId: targetMonitoringInstanceId, inFlight: false, fetched: true }
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setHistoryIncidentsError(describeError(error, '加载历史异常失败'))
        historyFetchRef.current = { monitoringInstanceId: targetMonitoringInstanceId, inFlight: false, fetched: false }
      })
      .finally(() => {
        if (cancelled) return
        setHistoryIncidentsLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [monitoringInstanceId, wantsHistoryIncidents])

  // Poll monitoringInstance data while command drawer is open and a pending action is
  // in-flight, so the user sees the result without a manual page refresh.
  useEffect(() => {
    if (!commandOpen || !monitoringInstanceId) return
    if (state.monitoringInstance?.last_action?.status !== 'pending') return

    let cancelled = false
    const interval = setInterval(() => {
      if (cancelled) return
      getMonitoringInstance(monitoringInstanceId)
        .then((updated) => {
          if (cancelled) return
          setState((prev) =>
            prev.requestedMonitoringInstanceId === monitoringInstanceId && prev.monitoringInstance
              ? { ...prev, monitoringInstance: mergeNonMetadataMonitoringInstanceRecord(prev.monitoringInstance, updated) }
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
  }, [commandOpen, state.monitoringInstance?.last_action?.status, monitoringInstanceId])

  const missingMonitoringInstanceId = !monitoringInstanceId
  const isCurrentMonitoringInstance = state.requestedMonitoringInstanceId === monitoringInstanceId
  const hasCurrentMonitoringActivity = state.requestedActivityMonitoringInstanceId === monitoringInstanceId
  const error = isCurrentMonitoringInstance ? state.error : null
  const monitoringInstance = isCurrentMonitoringInstance ? state.monitoringInstance : null
  const runtimeFacts = isCurrentMonitoringInstance ? state.runtimeFacts : null
  const incidents = hasCurrentMonitoringActivity ? state.incidents : []
  const events = hasCurrentMonitoringActivity ? state.events : []
  const eventsError = hasCurrentMonitoringActivity ? state.eventsError : null
  const linkedVPS =
    linkedVPSState.requestedMonitoringInstanceId === monitoringInstanceId ? linkedVPSState.records : []
  const linkedVPSLoading =
    linkedVPSState.requestedMonitoringInstanceId === monitoringInstanceId ? linkedVPSState.loading : false
  const linkedVPSError =
    linkedVPSState.requestedMonitoringInstanceId === monitoringInstanceId ? linkedVPSState.error : null
  const linkedVPSLoaded =
    linkedVPSState.requestedMonitoringInstanceId === monitoringInstanceId ? linkedVPSState.loaded : false

  async function handleRuntimeAction(action: MonitoringInstanceRuntimeAction, confirmed = false) {
    if (!monitoringInstance) return
    if (action === 'pause' && !confirmed) {
      setPendingRuntimeConfirmation({ action })
      return
    }

    const actionMonitoringInstanceId = monitoringInstance.monitoring_instance_id
    setRuntimeSubmitting(true)
    setRuntimeError(null)

    try {
      const updated =
        action === 'enter-maintenance'
          ? await enterMonitoringInstanceMaintenance(actionMonitoringInstanceId)
          : action === 'exit-maintenance'
            ? await exitMonitoringInstanceMaintenance(actionMonitoringInstanceId)
            : action === 'pause'
              ? await pauseMonitoringInstanceMonitoring(actionMonitoringInstanceId)
              : await resumeMonitoringInstanceMonitoring(actionMonitoringInstanceId)
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      setState((current) => ({
        ...current,
        monitoringInstance:
          current.requestedMonitoringInstanceId === actionMonitoringInstanceId && current.monitoringInstance
            ? mergeNonMetadataMonitoringInstanceRecord(current.monitoringInstance, updated)
            : current.monitoringInstance,
      }))
      pendingFocusRestoreRef.current = action
      setPendingRuntimeConfirmation((current) => (current?.action === action ? null : current))
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      setRuntimeError(describeError(error, '监控实例运行控制操作失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteMonitoringInstanceIdRef.current === actionMonitoringInstanceId &&
        currentRequestedMonitoringInstanceIdRef.current === actionMonitoringInstanceId
      ) {
        setRuntimeSubmitting(false)
      }
    }
  }

  async function handleLifecycleAction(action: MonitoringInstanceLifecycleAction) {
    if (!monitoringInstance) return
    const actionMonitoringInstanceId = monitoringInstance.monitoring_instance_id
    setLifecycleSubmitting(action)
    setLifecycleError(null)

    try {
      const updated =
        action === 'retire'
          ? await retireMonitoringInstance(actionMonitoringInstanceId)
          : await restoreRetiredMonitoringInstanceToObserving(actionMonitoringInstanceId)
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      setState((current) => ({
        ...current,
        monitoringInstance:
          current.requestedMonitoringInstanceId === actionMonitoringInstanceId && current.monitoringInstance
            ? mergeNonMetadataMonitoringInstanceRecord(current.monitoringInstance, updated)
            : current.monitoringInstance,
      }))
      setShowRetireConfirmation(false)
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      setLifecycleError(describeError(error, MONITORING_INSTANCE_LIFECYCLE_ACTION_ERROR))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteMonitoringInstanceIdRef.current === actionMonitoringInstanceId &&
        currentRequestedMonitoringInstanceIdRef.current === actionMonitoringInstanceId
      ) {
        setLifecycleSubmitting(null)
      }
    }
  }

  function applyOnboardingToMonitoringInstance(actionMonitoringInstanceId: string, onboarding: MonitoringInstanceOnboardingState) {
    setState((current) => {
      if (current.requestedMonitoringInstanceId !== actionMonitoringInstanceId) return current
      return {
        ...current,
        monitoringInstance: applyOnboardingRecordToMonitoringInstance(current.monitoringInstance, onboarding),
      }
    })
    setBindingConflictState({
      requestedMonitoringInstanceId: actionMonitoringInstanceId,
      onboarding: onboarding.binding_status === MONITORING_INSTANCE_BINDING_CONFLICT_STATUS ? onboarding : null,
      loading: false,
      error: null,
    })
  }

  async function handleBindingAction(
    action: BindingConflictAction,
    request: (targetMonitoringInstanceId: string) => Promise<MonitoringInstanceOnboardingState>,
  ) {
    if (!monitoringInstance || !bindingConflict || bindingConflictLoading) return
    const actionMonitoringInstanceId = monitoringInstance.monitoring_instance_id
    setBindingAction(action)
    setBindingConflictState((current) => ({
      ...current,
      requestedMonitoringInstanceId: actionMonitoringInstanceId,
      error: null,
    }))

    try {
      const nextOnboarding = await request(actionMonitoringInstanceId)
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      applyOnboardingToMonitoringInstance(actionMonitoringInstanceId, nextOnboarding)
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      setBindingConflictState((current) => ({
        ...current,
        requestedMonitoringInstanceId: actionMonitoringInstanceId,
        error: describeError(error, MONITORING_INSTANCE_BINDING_ACTION_ERROR),
      }))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteMonitoringInstanceIdRef.current === actionMonitoringInstanceId &&
        currentRequestedMonitoringInstanceIdRef.current === actionMonitoringInstanceId
      ) {
        setBindingAction(null)
      }
    }
  }

  if (!missingMonitoringInstanceId && !isCurrentMonitoringInstance) {
    return <MonitoringDetailLoading />
  }

  if (missingMonitoringInstanceId || error || !monitoringInstance) {
    return <MonitoringDetailUnavailable message={error ?? '未找到监控实例'} />
  }

  const hasCurrentBindingConflictState = bindingConflictState.requestedMonitoringInstanceId === monitoringInstanceId
  const bindingConflict = hasCurrentBindingConflictState ? bindingConflictState.onboarding : null
  const bindingConflictError = hasCurrentBindingConflictState ? bindingConflictState.error : null
  const bindingConflictLoading =
    hasCurrentBindingConflictState && bindingConflictState.loading && !bindingConflict

  function registerActionRef(action: MonitoringInstanceRuntimeAction, element: HTMLButtonElement | null) {
    actionButtonRefs.current[action] = element
  }

  function retryHistoryIncidents() {
    historyFetchRef.current = { monitoringInstanceId: monitoringInstanceId ?? null, inFlight: false, fetched: false }
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
    if (!monitoringInstance) return
    const actionMonitoringInstanceId = monitoringInstance.monitoring_instance_id
    setCommandSubmitting(true)
    setCommandError(null)

    try {
      const action = await postMonitoringInstanceAction(actionMonitoringInstanceId, cmdId)
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      setState((current) => ({
        ...current,
        monitoringInstance:
          current.requestedMonitoringInstanceId === actionMonitoringInstanceId && current.monitoringInstance
            ? {
                ...current.monitoringInstance,
                last_action: {
                  action_id: action.action_id,
                  command_id: action.command_id,
                  status: action.status,
                },
              }
            : current.monitoringInstance,
      }))
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      setCommandError(describeError(error, '下发命令失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteMonitoringInstanceIdRef.current === actionMonitoringInstanceId &&
        currentRequestedMonitoringInstanceIdRef.current === actionMonitoringInstanceId
      ) {
        setCommandSubmitting(false)
      }
    }
  }

  return (
    <MonitoringDetailPageBody
      monitoringInstance={monitoringInstance}
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
      onBindingConfirm={() => void handleBindingAction('confirm', confirmMonitoringInstanceRebind)}
      onBindingReject={() => void handleBindingAction('reject', rejectPendingMonitoringInstanceBinding)}
      onBindingReset={() => void handleBindingAction('reset', resetMonitoringInstanceBinding)}
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

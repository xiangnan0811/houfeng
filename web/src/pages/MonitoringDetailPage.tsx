import { useEffect, useRef, useState, type FormEvent } from 'react'

import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import type { MonitoringInstanceRuntimeAction } from '../components/monitoring-detail'
import {
  archiveMonitoringInstance,
  confirmMonitoringInstanceRebind,
  enterMonitoringInstanceMaintenance,
  exitMonitoringInstanceMaintenance,
  getMonitoringInstance,
  getMonitoringInstanceManagementReview,
  getMonitoringInstanceOnboarding,
  listVPSForMonitoringInstance,
  pauseMonitoringInstanceMonitoring,
  permanentCleanupMonitoringInstance,
  postMonitoringInstanceAction,
  rejectPendingMonitoringInstanceBinding,
  resetMonitoringInstanceBinding,
  restoreMonitoringInstanceFromArchive,
  restoreMonitoringInstanceLifecycle,
  resumeMonitoringInstanceMonitoring,
  retireMonitoringInstance,
  updateMonitoringInstanceMetadata,
} from '../lib/api'
import { listHistoricalIncidents } from '../lib/observabilityApi'
import type {
  ActiveIncidentRecord,
  MonitoringInstanceManagementReview,
  MonitoringInstanceOnboardingState,
  MonitoringInstanceRecord,
} from '../lib/types'
import { classifyHeartbeatFreshness } from './monitoring/heartbeatFreshness'
import { useMonitoringDetailSources } from './monitoring-detail/useMonitoringDetailSources'
import { MonitoringDetailPageBody } from './monitoring-detail/MonitoringDetailPageBody'
import { MonitoringDetailLoading } from './monitoring-detail/MonitoringDetailLoading'
import { MonitoringDetailUnavailable } from './monitoring-detail/MonitoringDetailUnavailable'
import {
  MONITORING_INSTANCE_BINDING_ACTION_ERROR,
  MONITORING_INSTANCE_BINDING_CONFLICT_LOAD_ERROR,
  MONITORING_INSTANCE_BINDING_CONFLICT_STATUS,
} from './monitoring-detail/monitoringDetailConstants'
import { READ_ONLY_PREVIEW } from '../lib/readOnlyPreview'
import { resolveMonitoringListHref } from './monitoring/monitoringListUrl'
import {
  applyOnboardingRecordToMonitoringInstance,
  describeError,
  mergeNonMetadataMonitoringInstanceRecord,
  parseLabels,
  validateReturnVPSId,
} from './monitoring-detail/monitoringDetailHelpers'
import './monitoring/MonitoringCommands.css'
import './monitoring-detail/MonitoringDetailWorkspace.css'
import type {
  BindingConflictAction,
  BindingConflictState,
  HistoryTab,
  LinkedVPSState,
  MetadataFormState,
  PendingBindingConfirmation,
  PendingRuntimeConfirmation,
  TimeWindow,
} from './monitoring-detail/types'

const LINKED_VPS_SUMMARY_FETCH_DELAY_MS = 300
type MonitoringManagementAction = 'retire' | 'restore-lifecycle' | 'archive' | 'restore-archive' | 'permanent-cleanup'

export function MonitoringDetailPage() {
  const { monitoringInstanceId } = useParams()
  return (
    <MonitoringDetailPageContent
      key={monitoringInstanceId ?? 'missing-monitoring-instance'}
      {...(monitoringInstanceId === undefined ? {} : { monitoringInstanceId })}
    />
  )
}

function MonitoringDetailPageContent({ monitoringInstanceId }: { monitoringInstanceId?: string }) {
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const returnVPSId = validateReturnVPSId(searchParams.get('return_vps'))
  const {
    timeWindow,
    setTimeWindow,
    state,
    setState,
    latestSample,
    snapshotReadAt,
    loadedTimeWindow,
    runtimeFactsError,
    runtimeFactsLoading,
    realtimeSamples,
    runtimeStreamStatus,
    runtimeStreamError,
    thresholds,
    heartbeatPolicy,
    settingsResolved,
    incidentsRetrying,
    eventsRetrying,
    retryRecord,
    retryRuntime,
    retrySettings,
    retryIncidents,
    retryEvents,
    resetObservationEpoch,
    isMountedRef,
    currentRouteMonitoringInstanceIdRef,
  } = useMonitoringDetailSources({
    ...(monitoringInstanceId === undefined ? {} : { monitoringInstanceId }),
    searchParams,
    setSearchParams,
    locationState: location.state,
  })
  const [runtimeSubmitting, setRuntimeSubmitting] = useState(false)
  const [runtimeError, setRuntimeError] = useState<string | null>(null)
  const [pendingRuntimeConfirmation, setPendingRuntimeConfirmation] =
    useState<PendingRuntimeConfirmation | null>(null)
  const [metadataEditing, setMetadataEditing] = useState(false)
  const [metadataForm, setMetadataForm] = useState<MetadataFormState>({ group: '', labels: '', note: '' })
  const [metadataSubmitting, setMetadataSubmitting] = useState(false)
  const [metadataError, setMetadataError] = useState<string | null>(null)
  const [managementReview, setManagementReview] = useState<MonitoringInstanceManagementReview | null>(null)
  const [managementRequestedMonitoringInstanceId, setManagementRequestedMonitoringInstanceId] = useState<string | null>(null)
  const [managementLoading, setManagementLoading] = useState(false)
  const [managementError, setManagementError] = useState<string | null>(null)
  const [managementSubmittingAction, setManagementSubmittingAction] =
    useState<MonitoringManagementAction | null>(null)
  const [managementActionError, setManagementActionError] = useState<string | null>(null)
  const [bindingConflictState, setBindingConflictState] = useState<BindingConflictState>({
    requestedMonitoringInstanceId: null,
    onboarding: null,
    loading: false,
    error: null,
  })
  const [bindingAction, setBindingAction] = useState<BindingConflictAction | null>(null)
  const [pendingBindingConfirmation, setPendingBindingConfirmation] =
    useState<PendingBindingConfirmation | null>(null)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [historyTab, setHistoryTab] = useState<HistoryTab>('events')
  const [historyIncidents, setHistoryIncidents] = useState<ActiveIncidentRecord[] | null>(null)
  const [historyIncidentsLoading, setHistoryIncidentsLoading] = useState(false)
  const [historyIncidentsError, setHistoryIncidentsError] = useState<string | null>(null)
  const [commandOpen, setCommandOpen] = useState(false)
  const [commandSubmitting, setCommandSubmitting] = useState(false)
  const [commandError, setCommandError] = useState<string | null>(null)
  const [onboardingOpen, setOnboardingOpen] = useState(false)
  const [commandPollError, setCommandPollError] = useState<string | null>(null)
  const [linkedVPSRetryKey, setLinkedVPSRetryKey] = useState(0)
  const [bindingReloadKey, setBindingReloadKey] = useState(0)
  const [linkedVPSState, setLinkedVPSState] = useState<LinkedVPSState>({
    requestedMonitoringInstanceId: null,
    records: [],
    loading: false,
    loaded: false,
    error: null,
  })
  const linkedVPSFetchRef = useRef({
    monitoringInstanceId: null as string | null,
    inFlight: false,
    fetched: false,
    scheduled: false,
    timerId: null as number | null,
    requestId: 0,
  })
  const linkedVPSInteractionBusyRef = useRef(false)
  const currentRequestedMonitoringInstanceIdRef = useRef<string | null>(null)
  const metadataRequestRef = useRef(0)
  const managementReviewRequestRef = useRef(0)
  const actionButtonRefs = useRef<Record<MonitoringInstanceRuntimeAction, HTMLButtonElement | null>>({
    'enter-maintenance': null,
    'exit-maintenance': null,
    pause: null,
    resume: null,
  })
  const pendingFocusRestoreRef = useRef<MonitoringInstanceRuntimeAction | null>(null)

  useEffect(() => {
    metadataRequestRef.current += 1
  }, [monitoringInstanceId])

  useEffect(() => {
    if (!state.monitoringInstance) return
    setMetadataEditing(false)
    setMetadataSubmitting(false)
    setMetadataError(null)
    setMetadataForm({
      group: state.monitoringInstance.group || '',
      labels: state.monitoringInstance.labels.join(', '),
      note: state.monitoringInstance.note,
    })
  }, [state.monitoringInstance?.monitoring_instance_id])

  useEffect(() => {
    managementReviewRequestRef.current += 1
    setManagementReview(null)
    setManagementRequestedMonitoringInstanceId(null)
    setManagementLoading(false)
    setManagementError(null)
    setManagementSubmittingAction(null)
    setManagementActionError(null)
  }, [monitoringInstanceId])

  // Deep-link: create/list redirects land here with ?onboarding=1 to open the
  // onboarding drawer directly. Consume only the onboarding param so a refresh/back
  // doesn't reopen it, while retaining navigation origin and state.
  useEffect(() => {
    if (searchParams.get('onboarding') !== '1') return
    if (!READ_ONLY_PREVIEW) setOnboardingOpen(true)
    const next = new URLSearchParams(searchParams)
    next.delete('onboarding')
    setSearchParams(next, { replace: true, state: location.state })
  }, [searchParams, setSearchParams, location.state])

  useEffect(() => {
    currentRequestedMonitoringInstanceIdRef.current = state.requestedMonitoringInstanceId
  }, [state.requestedMonitoringInstanceId])

  const linkedVPSInteractionBusy = Boolean(
    pendingRuntimeConfirmation ||
    runtimeSubmitting ||
    bindingAction ||
    historyOpen ||
    commandOpen ||
    onboardingOpen ||
    commandSubmitting ||
    historyIncidentsLoading,
  )

  useEffect(() => {
    linkedVPSInteractionBusyRef.current = linkedVPSInteractionBusy
  }, [linkedVPSInteractionBusy])

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

  useEffect(() => {
    const pendingTimerId = linkedVPSFetchRef.current.timerId
    if (pendingTimerId !== null) window.clearTimeout(pendingTimerId)
    linkedVPSFetchRef.current = {
      monitoringInstanceId: monitoringInstanceId ?? null,
      inFlight: false,
      fetched: false,
      scheduled: false,
      timerId: null,
      requestId: linkedVPSFetchRef.current.requestId + 1,
    }
    setLinkedVPSState({
      requestedMonitoringInstanceId: null,
      records: [],
      loading: false,
      loaded: false,
      error: null,
    })
  }, [monitoringInstanceId])

  useEffect(() => {
    if (!monitoringInstanceId) {
      const pendingTimerId = linkedVPSFetchRef.current.timerId
      if (pendingTimerId !== null) window.clearTimeout(pendingTimerId)
      setLinkedVPSState({
        requestedMonitoringInstanceId: null,
        records: [],
        loading: false,
        loaded: false,
        error: null,
      })
      return
    }
    if (state.requestedMonitoringInstanceId !== monitoringInstanceId || !state.monitoringInstance) return
    if (
      state.monitoringInstance.binding_status === MONITORING_INSTANCE_BINDING_CONFLICT_STATUS &&
      (
        bindingConflictState.requestedMonitoringInstanceId !== monitoringInstanceId ||
        bindingConflictState.loading
      )
    ) return

    if (linkedVPSFetchRef.current.monitoringInstanceId !== monitoringInstanceId) {
      linkedVPSFetchRef.current = {
        monitoringInstanceId,
        inFlight: false,
        fetched: false,
        scheduled: false,
        timerId: null,
        requestId: linkedVPSFetchRef.current.requestId + 1,
      }
    }
    if (
      linkedVPSInteractionBusy ||
      linkedVPSFetchRef.current.inFlight ||
      linkedVPSFetchRef.current.fetched ||
      linkedVPSFetchRef.current.scheduled
    ) return

    const requestId = linkedVPSFetchRef.current.requestId + 1
    const timerId = window.setTimeout(() => {
      const fetchState = linkedVPSFetchRef.current
      if (
        linkedVPSInteractionBusyRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== monitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== monitoringInstanceId ||
        fetchState.monitoringInstanceId !== monitoringInstanceId ||
        fetchState.requestId !== requestId ||
        !fetchState.scheduled
      ) {
        if (
          fetchState.monitoringInstanceId === monitoringInstanceId &&
          fetchState.requestId === requestId &&
          fetchState.scheduled
        ) {
          linkedVPSFetchRef.current = {
            monitoringInstanceId,
            inFlight: false,
            fetched: false,
            scheduled: false,
            timerId: null,
            requestId,
          }
        }
        return
      }

      linkedVPSFetchRef.current = {
        monitoringInstanceId,
        inFlight: true,
        fetched: false,
        scheduled: false,
        timerId: null,
        requestId,
      }

      listVPSForMonitoringInstance(monitoringInstanceId)
        .then((records) => {
          const currentFetchState = linkedVPSFetchRef.current
          if (
            !isMountedRef.current ||
            currentRouteMonitoringInstanceIdRef.current !== monitoringInstanceId ||
            currentRequestedMonitoringInstanceIdRef.current !== monitoringInstanceId ||
            currentFetchState.monitoringInstanceId !== monitoringInstanceId ||
            currentFetchState.requestId !== requestId
          ) return
          linkedVPSFetchRef.current = {
            monitoringInstanceId,
            inFlight: false,
            fetched: true,
            scheduled: false,
            timerId: null,
            requestId,
          }
          setLinkedVPSState({
            requestedMonitoringInstanceId: monitoringInstanceId,
            records: Array.isArray(records) ? records : [],
            loading: false,
            loaded: true,
            error: null,
          })
        })
        .catch((error: unknown) => {
          const currentFetchState = linkedVPSFetchRef.current
          if (
            !isMountedRef.current ||
            currentRouteMonitoringInstanceIdRef.current !== monitoringInstanceId ||
            currentRequestedMonitoringInstanceIdRef.current !== monitoringInstanceId ||
            currentFetchState.monitoringInstanceId !== monitoringInstanceId ||
            currentFetchState.requestId !== requestId
          ) return
          linkedVPSFetchRef.current = {
            monitoringInstanceId,
            inFlight: false,
            fetched: true,
            scheduled: false,
            timerId: null,
            requestId,
          }
          setLinkedVPSState({
            requestedMonitoringInstanceId: monitoringInstanceId,
            records: [],
            loading: false,
            loaded: true,
            error: describeError(error, '加载关联 VPS 失败'),
          })
        })
    }, LINKED_VPS_SUMMARY_FETCH_DELAY_MS)

    linkedVPSFetchRef.current = {
      monitoringInstanceId,
      inFlight: false,
      fetched: false,
      scheduled: true,
      timerId,
      requestId,
    }

    setLinkedVPSState((current) => ({
      requestedMonitoringInstanceId: monitoringInstanceId,
      records: current.requestedMonitoringInstanceId === monitoringInstanceId ? current.records : [],
      loading: true,
      loaded: false,
      error: null,
    }))

    return () => {
      const fetchState = linkedVPSFetchRef.current
      if (
        fetchState.monitoringInstanceId === monitoringInstanceId &&
        fetchState.requestId === requestId &&
        fetchState.scheduled &&
        fetchState.timerId !== null
      ) {
        window.clearTimeout(fetchState.timerId)
        linkedVPSFetchRef.current = {
          monitoringInstanceId,
          inFlight: false,
          fetched: false,
          scheduled: false,
          timerId: null,
          requestId,
        }
      }
    }
  }, [
    bindingConflictState.loading,
    bindingConflictState.requestedMonitoringInstanceId,
    linkedVPSInteractionBusy,
    monitoringInstanceId,
    state.monitoringInstance,
    linkedVPSRetryKey,
    bindingReloadKey,
    state.requestedMonitoringInstanceId,
  ])

  useEffect(() => {
    let cancelled = false
    if (!monitoringInstanceId) {
      setBindingConflictState({
        requestedMonitoringInstanceId: null,
        onboarding: null,
        loading: false,
        error: null,
      })
      setPendingBindingConfirmation(null)
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
      setPendingBindingConfirmation(null)
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
  }, [monitoringInstanceId, state.monitoringInstance, state.requestedMonitoringInstanceId, bindingReloadKey])

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
        .catch((error: unknown) => {
          if (cancelled) return
          setCommandPollError(describeError(error, '刷新命令执行状态失败'))
        })
    }, 3000)

    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [commandOpen, state.monitoringInstance?.last_action?.status, monitoringInstanceId])

  const missingMonitoringInstanceId = !monitoringInstanceId
  const isCurrentMonitoringInstance = state.requestedMonitoringInstanceId === monitoringInstanceId
  const error = isCurrentMonitoringInstance ? state.error : null
  const monitoringInstance = isCurrentMonitoringInstance ? state.monitoringInstance : null
  const windowReady = loadedTimeWindow === timeWindow
  const runtimeFacts = windowReady ? state.runtimeFacts : null
  const incidents = state.requestedIncidentsMonitoringInstanceId === monitoringInstanceId ? state.incidents : []
  const incidentsError = state.requestedIncidentsMonitoringInstanceId === monitoringInstanceId ? state.incidentsError : null
  const incidentsLoaded = state.requestedIncidentsMonitoringInstanceId === monitoringInstanceId
  const events = state.requestedEventsMonitoringInstanceId === monitoringInstanceId ? state.events : []
  const eventsError = state.requestedEventsMonitoringInstanceId === monitoringInstanceId ? state.eventsError : null
  const eventsLoaded = state.requestedEventsMonitoringInstanceId === monitoringInstanceId
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
      setPendingRuntimeConfirmation({
        action,
        monitoringInstanceId: monitoringInstance.monitoring_instance_id,
        displayName: monitoringInstance.display_name,
        updatedAt: monitoringInstance.updated_at,
        monitoringStatus: monitoringInstance.monitoring_status,
      })
      return
    }
    if (
      action === 'pause' &&
      confirmed &&
      pendingRuntimeConfirmation &&
      (
        pendingRuntimeConfirmation.monitoringInstanceId !== monitoringInstance.monitoring_instance_id ||
        pendingRuntimeConfirmation.updatedAt !== monitoringInstance.updated_at
      )
    ) {
      setRuntimeError('实例已更新，请重新确认')
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

  function updateMetadataField<K extends keyof MetadataFormState>(
    field: K,
    value: MetadataFormState[K],
  ) {
    setMetadataForm((current) => ({ ...current, [field]: value }))
  }

  function startMetadataEdit() {
    if (!monitoringInstance) return
    setMetadataForm({
      group: monitoringInstance.group || '',
      labels: monitoringInstance.labels.join(', '),
      note: monitoringInstance.note,
    })
    setMetadataError(null)
    setMetadataEditing(true)
  }

  function cancelMetadataEdit() {
    if (!monitoringInstance) return
    setMetadataForm({
      group: monitoringInstance.group || '',
      labels: monitoringInstance.labels.join(', '),
      note: monitoringInstance.note,
    })
    setMetadataError(null)
    setMetadataEditing(false)
  }

  async function handleMetadataSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!monitoringInstance || !monitoringInstanceId) return

    const actionMonitoringInstanceId = monitoringInstance.monitoring_instance_id
    const requestId = metadataRequestRef.current + 1
    metadataRequestRef.current = requestId
    setMetadataSubmitting(true)
    setMetadataError(null)

    try {
      const updated = await updateMonitoringInstanceMetadata(
        actionMonitoringInstanceId,
        {
          ...(metadataForm.group.trim() ? { group: metadataForm.group.trim() } : {}),
          labels: parseLabels(metadataForm.labels),
          note: metadataForm.note.trim(),
        },
        {
          expectedUpdatedAt: monitoringInstance.updated_at,
        },
      )
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        metadataRequestRef.current !== requestId
      ) {
        return
      }
      setState((current) => ({
        ...current,
        monitoringInstance:
          current.requestedMonitoringInstanceId === actionMonitoringInstanceId && current.monitoringInstance
            ? {
                ...current.monitoringInstance,
                group: updated.group,
                labels: updated.labels,
                note: updated.note,
                updated_at: updated.updated_at,
              }
            : current.monitoringInstance,
      }))
      setMetadataForm({
        group: updated.group || '',
        labels: updated.labels.join(', '),
        note: updated.note,
      })
      setMetadataEditing(false)
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        metadataRequestRef.current !== requestId
      ) {
        return
      }
      setMetadataError(describeError(error, '标签或备注更新失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteMonitoringInstanceIdRef.current === actionMonitoringInstanceId &&
        currentRequestedMonitoringInstanceIdRef.current === actionMonitoringInstanceId &&
        metadataRequestRef.current === requestId
      ) {
        setMetadataSubmitting(false)
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
    if (onboarding.binding_status !== MONITORING_INSTANCE_BINDING_CONFLICT_STATUS) {
      setPendingBindingConfirmation(null)
    }
  }

  async function handleBindingAction(
    action: BindingConflictAction,
    request: (targetMonitoringInstanceId: string) => Promise<MonitoringInstanceOnboardingState>,
  ) {
    if (!monitoringInstance || !bindingConflict || bindingConflictLoading) return
    if (
      pendingBindingConfirmation &&
      (
        pendingBindingConfirmation.monitoringInstanceId !== monitoringInstance.monitoring_instance_id ||
        pendingBindingConfirmation.updatedAt !== monitoringInstance.updated_at
      )
    ) {
      setBindingConflictState((current) => ({
        ...current,
        error: '实例已更新，请重新确认',
      }))
      return
    }
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
      resetObservationEpoch()
      setPendingBindingConfirmation((current) => (current?.action === action ? null : current))
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

  function requestBindingAction(action: BindingConflictAction) {
    if (!monitoringInstance || !bindingConflict || bindingConflictLoading || bindingAction !== null) return
    setBindingConflictState((current) => ({
      ...current,
      requestedMonitoringInstanceId: monitoringInstance.monitoring_instance_id,
      error: null,
    }))
    setPendingBindingConfirmation({
      action,
      monitoringInstanceId: monitoringInstance.monitoring_instance_id,
      displayName: monitoringInstance.display_name,
      updatedAt: monitoringInstance.updated_at,
    })
  }

  function cancelBindingConfirmation() {
    setPendingBindingConfirmation(null)
  }

  if (!missingMonitoringInstanceId && !isCurrentMonitoringInstance) {
    return <MonitoringDetailLoading />
  }

  if (missingMonitoringInstanceId || error || !monitoringInstance) {
    return (
      <MonitoringDetailUnavailable
        message={error ?? '未找到监控实例'}
        returnVPSId={returnVPSId}
        onRetry={retryRecord}
      />
    )
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

  function openCommandDrawer() {
    setCommandOpen(true)
    setCommandError(null)
  }

  function closeCommandDrawer() {
    setCommandOpen(false)
    setCommandError(null)
  }

  function openOnboardingDrawer() {
    if (READ_ONLY_PREVIEW) return
    setOnboardingOpen(true)
  }

  function closeOnboardingDrawer() {
    setOnboardingOpen(false)
  }

  function closeHistoryDrawer() {
    setHistoryOpen(false)
  }

  async function handleCommandExecute(cmdId: string, options: { confirmedSensitive?: boolean } = {}) {
    if (!monitoringInstance) return
    const actionMonitoringInstanceId = monitoringInstance.monitoring_instance_id
    setCommandSubmitting(true)
    setCommandError(null)

    try {
      const action = await postMonitoringInstanceAction(actionMonitoringInstanceId, cmdId, options)
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
                  sensitivity: options.confirmedSensitive ? 'sensitive' : 'standard',
                  queued_at: new Date().toISOString(),
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

  function applyManagementRecord(actionMonitoringInstanceId: string, updated: MonitoringInstanceRecord) {
    // A review payload is authoritative only for the route it was requested for;
    // anything else must never overwrite the identity rendered on this page.
    if (updated?.monitoring_instance_id !== actionMonitoringInstanceId) return
    setState((current) => ({
      ...current,
      monitoringInstance:
        current.requestedMonitoringInstanceId === actionMonitoringInstanceId && current.monitoringInstance
          ? mergeNonMetadataMonitoringInstanceRecord(current.monitoringInstance, updated)
          : current.monitoringInstance,
    }))
  }

  async function loadManagementReview(force = false) {
    if (!monitoringInstanceId) return
    const actionMonitoringInstanceId = monitoringInstanceId
    if (
      !force &&
      managementRequestedMonitoringInstanceId === actionMonitoringInstanceId &&
      (managementReview || managementLoading)
    ) {
      return
    }

    const requestId = ++managementReviewRequestRef.current
    setManagementRequestedMonitoringInstanceId(actionMonitoringInstanceId)
    setManagementLoading(true)
    setManagementError(null)
    if (force) setManagementReview(null)

    try {
      const review = await getMonitoringInstanceManagementReview(actionMonitoringInstanceId)
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        managementReviewRequestRef.current !== requestId
      ) {
        return
      }
      setManagementReview(review)
      applyManagementRecord(actionMonitoringInstanceId, review.record)
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        managementReviewRequestRef.current !== requestId
      ) {
        return
      }
      setManagementError(describeError(error, '加载监控实例管理审查失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteMonitoringInstanceIdRef.current === actionMonitoringInstanceId &&
        currentRequestedMonitoringInstanceIdRef.current === actionMonitoringInstanceId &&
        managementReviewRequestRef.current === requestId
      ) {
        setManagementLoading(false)
      }
    }
  }

  async function runManagementRecordAction(
    action: MonitoringManagementAction,
    request: (actionMonitoringInstanceId: string) => Promise<MonitoringInstanceRecord>,
  ) {
    if (!monitoringInstance) return
    const actionMonitoringInstanceId = monitoringInstance.monitoring_instance_id
    setManagementSubmittingAction(action)
    setManagementActionError(null)

    try {
      const updated = await request(actionMonitoringInstanceId)
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      applyManagementRecord(actionMonitoringInstanceId, updated)
      await loadManagementReview(true)
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      setManagementActionError(describeError(error, '监控实例管理操作失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteMonitoringInstanceIdRef.current === actionMonitoringInstanceId &&
        currentRequestedMonitoringInstanceIdRef.current === actionMonitoringInstanceId
      ) {
        setManagementSubmittingAction(null)
      }
    }
  }

  function handleManagementRetire(reason: string) {
    void runManagementRecordAction('retire', (actionMonitoringInstanceId) =>
      retireMonitoringInstance(actionMonitoringInstanceId, { reason }),
    )
  }

  function handleManagementRestoreLifecycle(reason: string) {
    void runManagementRecordAction('restore-lifecycle', (actionMonitoringInstanceId) =>
      restoreMonitoringInstanceLifecycle(actionMonitoringInstanceId, { reason }),
    )
  }

  function handleManagementArchive(reason: string, confirmationName: string) {
    void runManagementRecordAction('archive', (actionMonitoringInstanceId) =>
      archiveMonitoringInstance(actionMonitoringInstanceId, {
        reason,
        confirmation_name: confirmationName,
      }),
    )
  }

  function handleManagementRestoreArchive() {
    void runManagementRecordAction('restore-archive', (actionMonitoringInstanceId) =>
      restoreMonitoringInstanceFromArchive(actionMonitoringInstanceId),
    )
  }

  async function handleManagementPermanentCleanup(reason: string, confirmationName: string) {
    if (!monitoringInstance) return
    const actionMonitoringInstanceId = monitoringInstance.monitoring_instance_id
    setManagementSubmittingAction('permanent-cleanup')
    setManagementActionError(null)

    try {
      await permanentCleanupMonitoringInstance(actionMonitoringInstanceId, {
        reason,
        confirmation_name: confirmationName,
      })
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      navigate(resolveMonitoringListHref(location.state), { state: location.state })
    } catch (error: unknown) {
      if (
        !isMountedRef.current ||
        currentRouteMonitoringInstanceIdRef.current !== actionMonitoringInstanceId ||
        currentRequestedMonitoringInstanceIdRef.current !== actionMonitoringInstanceId
      ) {
        return
      }
      setManagementActionError(describeError(error, '永久清理监控实例失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteMonitoringInstanceIdRef.current === actionMonitoringInstanceId &&
        currentRequestedMonitoringInstanceIdRef.current === actionMonitoringInstanceId
      ) {
        setManagementSubmittingAction(null)
      }
    }
  }

  return (
    <MonitoringDetailPageBody
      monitoringInstance={monitoringInstance}
      runtimeFacts={runtimeFacts}
      latestSample={latestSample}
      snapshotReadAt={snapshotReadAt}
      heartbeatFreshness={classifyHeartbeatFreshness(
        monitoringInstance.last_heartbeat_at,
        heartbeatPolicy,
        snapshotReadAt ?? new Date(0),
        Boolean(settingsResolved && snapshotReadAt),
      )}
      runtimeFactsError={runtimeFactsError}
      runtimeFactsLoading={runtimeFactsLoading}
      onRetryRuntimeFacts={retryRuntime}
      onRetrySettings={retrySettings}
      runtimeSubmitting={runtimeSubmitting}
      runtimeError={runtimeError}
      pendingRuntimeConfirmation={pendingRuntimeConfirmation}
      metadataEditing={metadataEditing}
      metadataGroupDraft={metadataForm.group}
      metadataLabelDraft={metadataForm.labels}
      metadataNoteDraft={metadataForm.note}
      metadataSubmitting={metadataSubmitting}
      metadataError={metadataError}
      managementReview={managementRequestedMonitoringInstanceId === monitoringInstanceId ? managementReview : null}
      managementLoading={managementRequestedMonitoringInstanceId === monitoringInstanceId && managementLoading}
      managementError={managementRequestedMonitoringInstanceId === monitoringInstanceId ? managementError : null}
      managementSubmittingAction={managementSubmittingAction}
      managementActionError={managementActionError}
      onRuntimeAction={(action, confirmed) => void handleRuntimeAction(action, confirmed)}
      onCancelRuntimeConfirmation={() => {
        pendingFocusRestoreRef.current = 'pause'
        setPendingRuntimeConfirmation(null)
      }}
      registerActionRef={registerActionRef}
      onMetadataGroupDraftChange={(value) => updateMetadataField('group', value)}
      onMetadataLabelDraftChange={(value) => updateMetadataField('labels', value)}
      onMetadataNoteDraftChange={(value) => updateMetadataField('note', value)}
      onMetadataStartEdit={startMetadataEdit}
      onMetadataCancelEdit={cancelMetadataEdit}
      onMetadataSubmit={(event) => void handleMetadataSave(event)}
      onManagementLoadReview={(force) => void loadManagementReview(force)}
      onManagementRetire={handleManagementRetire}
      onManagementRestoreLifecycle={handleManagementRestoreLifecycle}
      onManagementArchive={handleManagementArchive}
      onManagementRestoreArchive={handleManagementRestoreArchive}
      onManagementPermanentCleanup={(reason, confirmationName) => void handleManagementPermanentCleanup(reason, confirmationName)}
      incidents={incidents}
      incidentsError={incidentsError}
      events={events}
      eventsError={eventsError}
      incidentsLoaded={incidentsLoaded}
      eventsLoaded={eventsLoaded}
      incidentsRetrying={incidentsRetrying}
      eventsRetrying={eventsRetrying}
      onRetryIncidents={retryIncidents}
      onRetryEvents={retryEvents}
      onRetryBindingConflict={() => setBindingReloadKey((key) => key + 1)}
      onRetryLinkedVPS={() => {
        linkedVPSFetchRef.current = {
          ...linkedVPSFetchRef.current,
          fetched: false,
          inFlight: false,
          scheduled: false,
        }
        setLinkedVPSRetryKey((key) => key + 1)
      }}
      commandPollError={commandPollError}
      onRetryCommandPoll={() => {
        setCommandPollError(null)
        if (!monitoringInstanceId) return
        getMonitoringInstance(monitoringInstanceId)
          .then((updated) => {
            setState((prev) =>
              prev.requestedMonitoringInstanceId === monitoringInstanceId && prev.monitoringInstance
                ? { ...prev, monitoringInstance: mergeNonMetadataMonitoringInstanceRecord(prev.monitoringInstance, updated) }
                : prev,
            )
          })
          .catch((error: unknown) => setCommandPollError(describeError(error, '刷新命令执行状态失败')))
      }}
      linkedVPS={linkedVPS}
      linkedVPSLoading={linkedVPSLoading}
      linkedVPSLoaded={linkedVPSLoaded}
      linkedVPSError={linkedVPSError}
      bindingConflict={bindingConflict}
      bindingConflictLoading={bindingConflictLoading}
      bindingConflictError={bindingConflictError}
      bindingAction={bindingAction}
      pendingBindingConfirmation={pendingBindingConfirmation}
      onBindingConfirm={() => void handleBindingAction('confirm', confirmMonitoringInstanceRebind)}
      onBindingReject={() => void handleBindingAction('reject', rejectPendingMonitoringInstanceBinding)}
      onBindingReset={() => void handleBindingAction('reset', resetMonitoringInstanceBinding)}
      onRequestBindingAction={requestBindingAction}
      onCancelBindingConfirmation={cancelBindingConfirmation}
      timeWindow={timeWindow}
      onTimeWindowChange={handleTimeWindowChange}
      realtimeSamples={realtimeSamples}
      runtimeStreamStatus={runtimeStreamStatus}
      runtimeStreamError={runtimeStreamError}
      thresholds={thresholds}
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
      onExecuteCommand={(commandId, options) => void handleCommandExecute(commandId, options)}
      onboardingOpen={onboardingOpen}
      onboardingReturnVPSId={returnVPSId}
      onOpenOnboarding={openOnboardingDrawer}
      onCloseOnboarding={closeOnboardingDrawer}
      onRefresh={() => {
        retryRecord()
        retryRuntime()
        retryIncidents()
        retryEvents()
      }}
    />
  )
}

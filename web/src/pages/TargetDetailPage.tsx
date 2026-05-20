import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'

import {
  type ProbeCreateFormState,
  type ProbeFormMode,
  type PendingProbeConfirmation,
  type TargetRuntimeAction,
} from '../components/target-detail'
import {
  ApiError,
  archiveTarget,
  createProbeItem,
  deleteProbeItem,
  enterTargetMaintenance,
  exitTargetMaintenance,
  getTarget,
  getTargetRuntimeFacts,
  listEvents,
  listHistoricalIncidents,
  listIncidents,
  listTargetProbeItems,
  pauseTarget,
  restoreTargetToPaused,
  resumeTarget,
  updateTargetMetadata,
  updateProbeItem,
} from '../lib/api'
import { formatConfigSummary } from '../lib/format'
import type {
  ActiveIncidentRecord,
  CreateProbeItemInput,
  ProbeItemRecord,
  ProbeObservation,
  TargetRecord,
  UpdateProbeItemInput,
} from '../lib/types'
import { TargetDetailPageBody } from './target-detail/TargetDetailPageBody'
import { TargetDetailLoading } from './target-detail/TargetDetailLoading'
import { TargetDetailUnavailable } from './target-detail/TargetDetailUnavailable'
import { INITIAL_PROBE_CREATE_FORM } from './target-detail/targetDetailConstants'
import {
  buildProbeCreateInput,
  buildProbeUpdateInput,
  dedupeLabels,
  describeError,
  focusRestoreActionAfterSuccess,
  formStateForProbeItem,
  hasUnsupportedProbeConfigFields,
  INITIAL_TARGET_DETAIL_STATE,
  initialProbeCreateFormForTarget,
  mergeRuntimeTargetRecord,
  parseLabels,
  probeCreateFormForKind,
} from './target-detail/targetDetailHelpers'
import type {
  HistoryTab,
  MetadataFormState,
  PendingRuntimeConfirmation,
  ProbeFocusRestoreRequest,
  TargetDetailPageState,
  TimeWindow,
} from './target-detail/types'

export function TargetDetailPage() {
  const { targetId } = useParams()
  return <TargetDetailPageContent key={targetId ?? 'missing-target'} targetId={targetId} />
}

function TargetDetailPageContent({ targetId }: { targetId?: string }) {
  const [state, setState] = useState<TargetDetailPageState>(INITIAL_TARGET_DETAIL_STATE)
  const [runtimeSubmitting, setRuntimeSubmitting] = useState(false)
  const [runtimeError, setRuntimeError] = useState<string | null>(null)
  const [pendingRuntimeConfirmation, setPendingRuntimeConfirmation] =
    useState<PendingRuntimeConfirmation | null>(null)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [historyTab, setHistoryTab] = useState<HistoryTab>('events')
  const [historyIncidents, setHistoryIncidents] = useState<ActiveIncidentRecord[] | null>(
    null,
  )
  const [historyIncidentsLoading, setHistoryIncidentsLoading] = useState(false)
  const [historyIncidentsError, setHistoryIncidentsError] = useState<string | null>(null)
  const [timeWindow, setTimeWindow] = useState<TimeWindow>('24h')
  const [probeCreateOpen, setProbeCreateOpen] = useState(false)
  const [probeFormMode, setProbeFormMode] = useState<ProbeFormMode>({ kind: 'create' })
  const [probeCreateSubmitting, setProbeCreateSubmitting] = useState(false)
  const [probeCreateError, setProbeCreateError] = useState<string | null>(null)
  const [probeMutationError, setProbeMutationError] = useState<string | null>(null)
  const [probeMutationBusyId, setProbeMutationBusyId] = useState<string | null>(null)
  const [metadataEditing, setMetadataEditing] = useState(false)
  const [metadataSubmitting, setMetadataSubmitting] = useState(false)
  const [metadataError, setMetadataError] = useState<string | null>(null)
  const [metadataForm, setMetadataForm] = useState<MetadataFormState>({ group: '', labels: '', note: '' })
  const [pendingProbeConfirmation, setPendingProbeConfirmation] =
    useState<PendingProbeConfirmation | null>(null)
  const [probeCreateForm, setProbeCreateForm] = useState<ProbeCreateFormState>(
    INITIAL_PROBE_CREATE_FORM,
  )
  const currentRouteTargetIdRef = useRef<string | null>(targetId ?? null)
  const currentRequestedTargetIdRef = useRef<string | null>(null)
  const isMountedRef = useRef(true)
  const probeFormRequestRef = useRef(0)
  const probeRowMutationRequestRef = useRef(0)
  const probeRowMutationInFlightRef = useRef(false)
  const runtimeActionButtonRefs = useRef<Record<TargetRuntimeAction, HTMLButtonElement | null>>({
    'enter-maintenance': null,
    'exit-maintenance': null,
    pause: null,
    resume: null,
    archive: null,
    'restore-to-paused': null,
  })
  const pendingRuntimeFocusRestoreRef = useRef<TargetRuntimeAction | null>(null)
  const probeDeleteButtonRefs = useRef<Record<string, HTMLButtonElement | null>>({})
  const addProbeButtonRef = useRef<HTMLButtonElement | null>(null)
  const pendingProbeFocusRestoreRef = useRef<ProbeFocusRestoreRequest | null>(null)
  const pendingProbeConfirmationCardRef = useRef<HTMLDivElement | null>(null)
  const metadataRequestRef = useRef(0)

  useEffect(() => {
    currentRouteTargetIdRef.current = targetId ?? null
    metadataRequestRef.current += 1
  }, [targetId])

  useEffect(() => {
    currentRequestedTargetIdRef.current = state.requestedTargetId
  }, [state.requestedTargetId])

  useEffect(
    () => () => {
      isMountedRef.current = false
    },
    [],
  )

  // Refetch runtime facts when time window changes (keep old data visible).
  // The mounted ref skips the initial invocation so the main load effect handles
  // the first fetch. It resets on targetId change for the same reason.
  const timeWindowMountedRef = useRef(false)
  useEffect(() => {
    timeWindowMountedRef.current = false
  }, [targetId])

  useEffect(() => {
    if (!timeWindowMountedRef.current) {
      timeWindowMountedRef.current = true
      return
    }

    let cancelled = false
    if (!targetId) return

    getTargetRuntimeFacts(targetId, timeWindow)
      .then((runtimeFacts) => {
        if (cancelled) return
        setState((current) => {
          if (current.requestedTargetId !== targetId) return current
          return { ...current, runtimeFacts }
        })
      })
      .catch(() => {
        // On window-switch fetch error, keep old data visible — no-op.
      })

    return () => { cancelled = true }
  }, [targetId, timeWindow])

  useEffect(() => {
    if (pendingRuntimeConfirmation) return

    const action = pendingRuntimeFocusRestoreRef.current
    if (!action) return

    const preferred = runtimeActionButtonRefs.current[action]
    const fallback =
      action === 'pause'
        ? runtimeActionButtonRefs.current.resume
        : action === 'archive'
          ? runtimeActionButtonRefs.current['restore-to-paused']
          : null
    const target = [preferred, fallback].find((element) => element?.isConnected)

    target?.focus()
    pendingRuntimeFocusRestoreRef.current = null
  }, [pendingRuntimeConfirmation, state.target])

  useEffect(() => {
    if (pendingProbeConfirmation) return

    const request = pendingProbeFocusRestoreRef.current
    if (!request) return

    const preferred = request.probeItemId
      ? probeDeleteButtonRefs.current[request.probeItemId]
      : null
    const fallback = addProbeButtonRef.current
    const target = [preferred, fallback].find((element) => element?.isConnected)

    target?.focus()
    pendingProbeFocusRestoreRef.current = null
  }, [pendingProbeConfirmation, state.probeItems])

  useEffect(() => {
    const stack = pendingProbeConfirmationCardRef.current?.querySelector('.page-stack')
    const existingSummary = stack?.querySelector('[data-probe-confirmation-summary="true"]')
    existingSummary?.remove()

    if (!pendingProbeConfirmation || !stack) return

    const probeItem = state.probeItems.find(
      (item) => item.probe_item_id === pendingProbeConfirmation.probeItemId,
    )
    if (!probeItem) return

    const summary = document.createElement('p')
    summary.dataset.probeConfirmationSummary = 'true'
    summary.textContent = formatConfigSummary(probeItem.config)
    stack.appendChild(summary)

    return () => {
      summary.remove()
    }
  }, [pendingProbeConfirmation, state.probeItems])

  useEffect(() => {
    let cancelled = false
    if (!targetId) return

    Promise.all([
      getTarget(targetId),
      listTargetProbeItems(targetId),
      getTargetRuntimeFacts(targetId),
    ])
      .then(([target, probeItems, runtimeFacts]) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          requestedTargetId: targetId,
          error: null,
          target,
          probeItems,
          runtimeFacts,
        }))
        setMetadataEditing(false)
        setMetadataSubmitting(false)
        setMetadataError(null)
        setMetadataForm({
          group: target.group || '',
          labels: target.labels.join(', '),
          note: target.note,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        const message =
          error instanceof ApiError && error.status === 404
            ? '目标不存在'
            : describeError(error, '加载目标详情失败')
        setState((current) => ({
          ...current,
          requestedTargetId: targetId,
          error: message,
          target: null,
          probeItems: [],
          runtimeFacts: null,
        }))
      })

    return () => {
      cancelled = true
    }
  }, [targetId])

  useEffect(() => {
    let cancelled = false
    if (!targetId) return

    Promise.allSettled([
      listIncidents({ object_type: 'target', object_id: targetId }),
      listEvents({ object_type: 'target', object_id: targetId }),
    ]).then(([incidentsResult, eventsResult]) => {
      if (cancelled) return
      setState((current) => ({
        ...current,
        requestedActivityTargetId: targetId,
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
  }, [targetId])

  const historyFetchRef = useRef<{
    targetId: string | null
    inFlight: boolean
    fetched: boolean
  }>({ targetId: null, inFlight: false, fetched: false })

  useEffect(() => {
    if (historyFetchRef.current.targetId !== targetId) {
      historyFetchRef.current = {
        targetId: targetId ?? null,
        inFlight: false,
        fetched: false,
      }
    }
  }, [targetId])

  const wantsHistoryIncidents = historyOpen && historyTab === 'incidents'

  useEffect(() => {
    if (!targetId) return
    if (!wantsHistoryIncidents) return
    if (historyFetchRef.current.inFlight || historyFetchRef.current.fetched) return

    let cancelled = false
    const actionTargetId = targetId
    historyFetchRef.current = {
      targetId: actionTargetId,
      inFlight: true,
      fetched: false,
    }
    setHistoryIncidentsLoading(true)
    setHistoryIncidentsError(null)

    listHistoricalIncidents('target', actionTargetId)
      .then((records) => {
        if (cancelled) return
        setHistoryIncidents(records)
        historyFetchRef.current = {
          targetId: actionTargetId,
          inFlight: false,
          fetched: true,
        }
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setHistoryIncidentsError(describeError(error, '加载历史异常失败'))
        historyFetchRef.current = {
          targetId: actionTargetId,
          inFlight: false,
          fetched: false,
        }
      })
      .finally(() => {
        if (cancelled) return
        setHistoryIncidentsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [targetId, wantsHistoryIncidents])

  const observationsByProbe = useMemo(() => {
    const map = new Map<string, ProbeObservation[]>()
    for (const observation of state.runtimeFacts?.latest_probe_observations ?? []) {
      const existing = map.get(observation.probe_item_id) ?? []
      existing.push(observation)
      map.set(observation.probe_item_id, existing)
    }
    return map
  }, [state.runtimeFacts])
  const recentObservations = state.runtimeFacts?.recent_probe_observations ?? []

  const missingTargetId = !targetId
  const isCurrentTarget = state.requestedTargetId === targetId
  const hasCurrentActivity = state.requestedActivityTargetId === targetId
  const error = isCurrentTarget ? state.error : null
  const target = isCurrentTarget ? state.target : null
  const probeItems = isCurrentTarget ? state.probeItems : []
  const incidents = hasCurrentActivity ? state.incidents : []
  const incidentsError = hasCurrentActivity ? state.incidentsError : null
  const events = hasCurrentActivity ? state.events : []
  const eventsError = hasCurrentActivity ? state.eventsError : null
  const runtimeConfirmationActive = pendingRuntimeConfirmation !== null
  const probeConfirmationActive = pendingProbeConfirmation !== null

  function openHistory(tab: 'events' | 'incidents' = 'events') {
    setHistoryTab(tab)
    setHistoryOpen(true)
  }

  function retryHistoryIncidents() {
    if (!targetId) return
    historyFetchRef.current = {
      targetId,
      inFlight: false,
      fetched: false,
    }
    setHistoryIncidents(null)
    setHistoryIncidentsError(null)
  }

  function registerActionRef(
    action: TargetRuntimeAction,
    element: HTMLButtonElement | null,
  ) {
    runtimeActionButtonRefs.current[action] = element
  }

  function updateMetadataField<K extends keyof MetadataFormState>(
    field: K,
    value: MetadataFormState[K],
  ) {
    setMetadataForm((current) => ({ ...current, [field]: value }))
  }

  async function handleMetadataSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!target || !targetId) return

    const actionTargetId = targetId
    const requestId = metadataRequestRef.current + 1
    metadataRequestRef.current = requestId
    setMetadataSubmitting(true)
    setMetadataError(null)

    try {
      const updated = await updateTargetMetadata(
        actionTargetId,
        {
          group: metadataForm.group.trim() || undefined,
          labels: dedupeLabels(parseLabels(metadataForm.labels)),
          note: metadataForm.note.trim(),
        },
        {
          expectedUpdatedAt: target.updated_at,
        },
      )
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId ||
        metadataRequestRef.current !== requestId
      ) {
        return
      }
      setState((current) => ({
        ...current,
        target:
          current.target?.target_id === actionTargetId
            ? {
                ...current.target,
                group: updated.group,
                labels: updated.labels,
                note: updated.note,
                updated_at: updated.updated_at,
              }
            : current.target,
      }))
      setMetadataForm({
        group: updated.group || '',
        labels: updated.labels.join(', '),
        note: updated.note,
      })
      setMetadataEditing(false)
    } catch (metadataError) {
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId ||
        metadataRequestRef.current !== requestId
      ) {
        return
      }
      setMetadataError(describeError(metadataError, '标签或备注更新失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteTargetIdRef.current === actionTargetId &&
        currentRequestedTargetIdRef.current === actionTargetId &&
        metadataRequestRef.current === requestId
      ) {
        setMetadataSubmitting(false)
      }
    }
  }

  async function handleRuntimeAction(action: TargetRuntimeAction, confirmed = false) {
    if (!target) return
    if (probeConfirmationActive) return
    if (runtimeConfirmationActive && !confirmed) return
    if ((action === 'pause' || action === 'archive') && !confirmed) {
      setPendingRuntimeConfirmation({ action })
      return
    }

    const actionTargetId = target.target_id
    setRuntimeSubmitting(true)
    setRuntimeError(null)

    try {
      const updated =
        action === 'enter-maintenance'
          ? await enterTargetMaintenance(actionTargetId)
          : action === 'exit-maintenance'
            ? await exitTargetMaintenance(actionTargetId)
            : action === 'pause'
              ? await pauseTarget(actionTargetId)
              : action === 'resume'
                ? await resumeTarget(actionTargetId)
                : action === 'archive'
                ? await archiveTarget(actionTargetId)
                  : await restoreTargetToPaused(actionTargetId)
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId
      ) {
        return
      }
      setState((current) => ({
        ...current,
        target:
          current.target?.target_id === actionTargetId
            ? mergeRuntimeTargetRecord(current.target, updated)
            : current.target,
      }))
      pendingRuntimeFocusRestoreRef.current = focusRestoreActionAfterSuccess(action)
      setPendingRuntimeConfirmation((current) => (current?.action === action ? null : current))
    } catch (error) {
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId
      ) {
        return
      }
      setRuntimeError(describeError(error, '目标运行控制操作失败'))
    } finally {
      if (
        isMountedRef.current &&
        currentRouteTargetIdRef.current === actionTargetId &&
        currentRequestedTargetIdRef.current === actionTargetId
      ) {
        setRuntimeSubmitting(false)
      }
    }
  }

  function updateProbeCreateField<K extends keyof ProbeCreateFormState>(
    field: K,
    value: ProbeCreateFormState[K],
  ) {
    setProbeCreateForm((current) => ({ ...current, [field]: value }))
  }

  function openProbeCreateForm(target: TargetRecord) {
    if (probeCreateSubmitting || runtimeConfirmationActive || probeConfirmationActive) return
    probeFormRequestRef.current += 1
    setProbeFormMode({ kind: 'create' })
    setProbeCreateForm(initialProbeCreateFormForTarget(target))
    setProbeCreateError(null)
    setProbeMutationError(null)
    setProbeCreateOpen(true)
  }

  function openProbeEditForm(probeItem: ProbeItemRecord) {
    if (probeCreateSubmitting || runtimeConfirmationActive || probeConfirmationActive) return
    if (hasUnsupportedProbeConfigFields(probeItem)) {
      probeFormRequestRef.current += 1
      setProbeCreateOpen(false)
      setProbeCreateError(null)
      setProbeMutationError('ProbeItem 包含当前 V1 表单不支持的配置字段，不能安全编辑。')
      return
    }

    probeFormRequestRef.current += 1
    setProbeFormMode({ kind: 'edit', probeItemId: probeItem.probe_item_id })
    setProbeCreateForm(formStateForProbeItem(probeItem))
    setProbeCreateError(null)
    setProbeMutationError(null)
    setProbeCreateOpen(true)
  }

  function closeProbeForm(target: TargetRecord) {
    if (probeCreateSubmitting) return
    probeFormRequestRef.current += 1
    setProbeCreateOpen(false)
    setProbeFormMode({ kind: 'create' })
    setProbeCreateForm(initialProbeCreateFormForTarget(target))
    setProbeCreateError(null)
  }

  function replaceProbeItem(updated: ProbeItemRecord) {
    setState((current) => ({
      ...current,
      probeItems: current.probeItems.map((item) =>
        item.probe_item_id === updated.probe_item_id ? updated : item,
      ),
    }))
  }

  function removeProbeItem(probeItemId: string) {
    setState((current) => ({
      ...current,
      probeItems: current.probeItems.filter((item) => item.probe_item_id !== probeItemId),
    }))
  }

  async function handleProbeCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!target) return

    const actionTargetId = target.target_id
    const requestId = probeFormRequestRef.current + 1
    probeFormRequestRef.current = requestId
    setProbeCreateError(null)
    setProbeMutationError(null)

    let payload: CreateProbeItemInput | UpdateProbeItemInput
    try {
      payload =
        probeFormMode.kind === 'edit'
          ? buildProbeUpdateInput(probeCreateForm)
          : buildProbeCreateInput(probeCreateForm)
    } catch (validationError) {
      setProbeCreateError(
        describeError(
          validationError,
          probeFormMode.kind === 'edit' ? '保存 ProbeItem 失败' : '创建 ProbeItem 失败',
        ),
      )
      return
    }

    setProbeCreateSubmitting(true)
    try {
      const createdOrUpdated =
        probeFormMode.kind === 'edit'
          ? await updateProbeItem(actionTargetId, probeFormMode.probeItemId, payload)
          : await createProbeItem(actionTargetId, payload)
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId ||
        probeFormRequestRef.current !== requestId
      ) {
        return
      }
      if (probeFormMode.kind === 'edit') {
        replaceProbeItem(createdOrUpdated)
      } else {
        setState((current) => ({
          ...current,
          probeItems: [...current.probeItems, createdOrUpdated],
        }))
      }
      setProbeCreateOpen(false)
      setProbeFormMode({ kind: 'create' })
      setProbeCreateForm(initialProbeCreateFormForTarget(target))
    } catch (submitError) {
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId ||
        probeFormRequestRef.current !== requestId
      ) {
        return
      }
      setProbeCreateError(
        describeError(
          submitError,
          probeFormMode.kind === 'edit' ? '保存 ProbeItem 失败' : '创建 ProbeItem 失败',
        ),
      )
    } finally {
      if (
        isMountedRef.current &&
        currentRouteTargetIdRef.current === actionTargetId &&
        currentRequestedTargetIdRef.current === actionTargetId &&
        probeFormRequestRef.current === requestId
      ) {
        setProbeCreateSubmitting(false)
      }
    }
  }

  async function handleToggleProbeItem(probeItem: ProbeItemRecord) {
    if (
      !target ||
      probeCreateSubmitting ||
      probeRowMutationInFlightRef.current ||
      runtimeConfirmationActive ||
      probeConfirmationActive
    ) {
      return
    }

    const actionTargetId = target.target_id
    const requestId = probeRowMutationRequestRef.current + 1
    probeRowMutationRequestRef.current = requestId
    probeRowMutationInFlightRef.current = true
    setProbeMutationBusyId(probeItem.probe_item_id)
    setProbeMutationError(null)

    try {
      const updated = await updateProbeItem(actionTargetId, probeItem.probe_item_id, {
        probe_kind: probeItem.probe_kind,
        enabled: !probeItem.enabled,
        frequency_tier: probeItem.frequency_tier,
        timeout_seconds: probeItem.timeout_seconds,
        config: probeItem.config,
      })
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId ||
        probeRowMutationRequestRef.current !== requestId
      ) {
        return
      }
      replaceProbeItem(updated)
    } catch (error) {
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId ||
        probeRowMutationRequestRef.current !== requestId
      ) {
        return
      }
      setProbeMutationError(describeError(error, 'ProbeItem 操作失败'))
    } finally {
      const isCurrentRowMutation = probeRowMutationRequestRef.current === requestId
      if (isCurrentRowMutation) {
        probeRowMutationInFlightRef.current = false
      }
      if (
        isMountedRef.current &&
        currentRouteTargetIdRef.current === actionTargetId &&
        currentRequestedTargetIdRef.current === actionTargetId &&
        isCurrentRowMutation
      ) {
        setProbeMutationBusyId(null)
      }
    }
  }

  async function handleDeleteProbeItem(probeItem: ProbeItemRecord, confirmed = false) {
    if (!target || probeCreateSubmitting || probeRowMutationInFlightRef.current) {
      return
    }
    if (runtimeConfirmationActive) return
    if (probeConfirmationActive && (!confirmed || pendingProbeConfirmation?.probeItemId !== probeItem.probe_item_id)) {
      return
    }
    if (!confirmed) {
      setPendingProbeConfirmation({ probeItemId: probeItem.probe_item_id, action: 'delete' })
      return
    }

    const actionTargetId = target.target_id
    const requestId = probeRowMutationRequestRef.current + 1
    probeRowMutationRequestRef.current = requestId
    probeRowMutationInFlightRef.current = true
    setProbeMutationBusyId(probeItem.probe_item_id)
    setProbeMutationError(null)

    try {
      await deleteProbeItem(actionTargetId, probeItem.probe_item_id)
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId ||
        probeRowMutationRequestRef.current !== requestId
      ) {
        return
      }
      removeProbeItem(probeItem.probe_item_id)
      pendingProbeFocusRestoreRef.current = {}
      setPendingProbeConfirmation((current) =>
        current?.probeItemId === probeItem.probe_item_id ? null : current,
      )
      if (probeFormMode.kind === 'edit' && probeFormMode.probeItemId === probeItem.probe_item_id) {
        setProbeCreateOpen(false)
        setProbeFormMode({ kind: 'create' })
        setProbeCreateForm(initialProbeCreateFormForTarget(target))
        setProbeCreateError(null)
      }
    } catch (error) {
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId ||
        probeRowMutationRequestRef.current !== requestId
      ) {
        return
      }
      setProbeMutationError(describeError(error, 'ProbeItem 操作失败'))
    } finally {
      const isCurrentRowMutation = probeRowMutationRequestRef.current === requestId
      if (isCurrentRowMutation) {
        probeRowMutationInFlightRef.current = false
      }
      if (
        isMountedRef.current &&
        currentRouteTargetIdRef.current === actionTargetId &&
        currentRequestedTargetIdRef.current === actionTargetId &&
        isCurrentRowMutation
      ) {
        setProbeMutationBusyId(null)
      }
    }
  }

  if (!missingTargetId && !isCurrentTarget) {
    return <TargetDetailLoading />
  }

  if (missingTargetId || error || !target) {
    return <TargetDetailUnavailable error={error} />
  }

  return (
    <TargetDetailPageBody
      target={target}
      probeItems={probeItems}
      activityLoaded={hasCurrentActivity}
      incidents={incidents}
      incidentsError={incidentsError}
      events={events}
      eventsError={eventsError}
      recentObservations={recentObservations}
      observationsByProbe={observationsByProbe}
      runtimeSubmitting={runtimeSubmitting}
      runtimeError={runtimeError}
      pendingRuntimeConfirmation={pendingRuntimeConfirmation}
      runtimeConfirmationActive={runtimeConfirmationActive}
      probeConfirmationActive={probeConfirmationActive}
      onRuntimeAction={(action, confirmed) => void handleRuntimeAction(action, confirmed)}
      onCancelPauseConfirmation={() => {
        pendingRuntimeFocusRestoreRef.current = 'pause'
        setPendingRuntimeConfirmation(null)
      }}
      onCancelArchiveConfirmation={() => {
        pendingRuntimeFocusRestoreRef.current = 'archive'
        setPendingRuntimeConfirmation(null)
      }}
      registerActionRef={registerActionRef}
      timeWindow={timeWindow}
      onTimeWindowChange={setTimeWindow}
      addProbeButtonRef={addProbeButtonRef}
      probeCreateOpen={probeCreateOpen}
      probeFormMode={probeFormMode}
      probeCreateForm={probeCreateForm}
      probeCreateSubmitting={probeCreateSubmitting}
      probeCreateError={probeCreateError}
      probeMutationError={probeMutationError}
      onOpenProbeCreate={() => openProbeCreateForm(target)}
      onCloseProbeForm={() => closeProbeForm(target)}
      onProbeSubmit={handleProbeCreate}
      onProbeKindChange={(probeKind) => {
        if (probeFormMode.kind === 'edit') {
          updateProbeCreateField('probeKind', probeKind)
          return
        }
        setProbeCreateForm((current) => probeCreateFormForKind(current, probeKind))
      }}
      onProbeFieldChange={updateProbeCreateField}
      metadataEditing={metadataEditing}
      metadataSubmitting={metadataSubmitting}
      metadataError={metadataError}
      metadataForm={metadataForm}
      onMetadataGroupChange={(value) => updateMetadataField('group', value)}
      onMetadataLabelChange={(value) => updateMetadataField('labels', value)}
      onMetadataNoteChange={(value) => updateMetadataField('note', value)}
      onStartMetadataEdit={() => {
        setMetadataEditing(true)
        setMetadataError(null)
        setMetadataForm({
          group: target.group || '',
          labels: target.labels.join(', '),
          note: target.note,
        })
      }}
      onCancelMetadataEdit={() => {
        setMetadataEditing(false)
        setMetadataError(null)
        setMetadataForm({
          group: target.group || '',
          labels: target.labels.join(', '),
          note: target.note,
        })
      }}
      onMetadataSubmit={handleMetadataSave}
      probeMutationBusyId={probeMutationBusyId}
      pendingProbeConfirmation={pendingProbeConfirmation}
      pendingProbeConfirmationCardRef={pendingProbeConfirmationCardRef}
      registerDeleteButtonRef={(probeItemId, element) => {
        probeDeleteButtonRefs.current[probeItemId] = element
      }}
      onAddProbe={() => openProbeCreateForm(target)}
      onEditProbe={(probeItem) => openProbeEditForm(probeItem)}
      onToggleProbe={(probeItem) => void handleToggleProbeItem(probeItem)}
      onDeleteProbe={(probeItem) => void handleDeleteProbeItem(probeItem)}
      onConfirmDeleteProbe={(probeItem) => void handleDeleteProbeItem(probeItem, true)}
      onCancelDeleteConfirmation={(probeItem) => {
        pendingProbeFocusRestoreRef.current = {
          probeItemId: probeItem.probe_item_id,
        }
        setPendingProbeConfirmation((current) =>
          current?.probeItemId === probeItem.probe_item_id ? null : current,
        )
      }}
      historyOpen={historyOpen}
      historyTab={historyTab}
      historyIncidents={historyIncidents}
      historyIncidentsLoading={historyIncidentsLoading}
      historyIncidentsError={historyIncidentsError}
      onOpenHistory={openHistory}
      onCloseHistory={() => setHistoryOpen(false)}
      onHistoryTabChange={setHistoryTab}
      onRetryHistoryIncidents={retryHistoryIncidents}
    />
  )
}

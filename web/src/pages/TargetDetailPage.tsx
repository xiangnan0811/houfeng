import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { ActionConfirmationCard } from '../components/ActionConfirmationCard'
import {
  TargetLabelsAndNote,
  TargetLatencyTrends,
  TargetProbeForm,
  type ProbeCreateFormState,
  type ProbeFormMode,
  TargetProbeList,
  type PendingProbeConfirmation,
  TargetWatchtowerHeader,
  type TargetRuntimeAction,
} from '../components/target-detail'
import { EventList } from '../components/EventList'
import { IncidentList } from '../components/IncidentList'
import { Card } from '../components/atoms/Card'
import {
  Drawer,
  MonoDigits,
  Tabs,
  Timestamp,
} from '../components/atoms'
import { Button } from '../components/atoms/Button'
import { StatusBadge } from '../components/StatusBadge'
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
  FrequencyTier,
  ProbeItemRecord,
  ProbeKind,
  ProbeObservation,
  StateChangeEventRecord,
  TargetRecord,
  TargetRuntimeFacts,
  UpdateProbeItemInput,
} from '../lib/types'

type State = {
  requestedTargetId: string | null
  error: string | null
  target: TargetRecord | null
  probeItems: ProbeItemRecord[]
  runtimeFacts: TargetRuntimeFacts | null
  requestedActivityTargetId: string | null
  incidents: ActiveIncidentRecord[]
  incidentsError: string | null
  events: StateChangeEventRecord[]
  eventsError: string | null
}

const DEFAULT_FREQUENCY_BY_PROBE_KIND: Record<ProbeKind, FrequencyTier> = {
  tcp: '5m',
  http: '5m',
  tls: '6h',
}

const PROBE_CONFIG_KEYS: Record<ProbeKind, Set<string>> = {
  tcp: new Set(['port']),
  http: new Set(['scheme', 'path', 'method', 'expected_status_range']),
  tls: new Set(['port', 'expiry_warning_days']),
}

const initialProbeCreateForm: ProbeCreateFormState = {
  probeKind: 'tcp',
  enabled: true,
  frequencyTier: '5m',
  timeoutSeconds: '5',
  port: '',
  httpScheme: 'https',
  httpPath: '/',
  httpMethod: 'GET',
  expectedStatusStart: '200',
  expectedStatusEnd: '299',
  tlsExpiryWarningDays: '14',
}

type PendingRuntimeConfirmation = {
  action: 'pause' | 'archive'
}

type ProbeFocusRestoreRequest = {
  probeItemId?: string
}

type MetadataFormState = {
  group: string
  labels: string
  note: string
}

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function parseLabels(value: string) {
  return value
    .split(/[,，]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function dedupeLabels(values: string[]) {
  return values.filter((value, index) => values.indexOf(value) === index)
}

function parseOptionalPositiveInteger(value: string, label: string): number | undefined {
  const normalized = value.trim()
  if (normalized === '') return undefined
  if (!/^[1-9]\d*$/.test(normalized)) {
    throw new Error(`${label}必须为正整数。`)
  }
  return Number.parseInt(normalized, 10)
}

function parseRequiredPositiveInteger(value: string, label: string): number {
  const parsed = parseOptionalPositiveInteger(value, label)
  if (parsed == null) {
    throw new Error(`${label}必须为正整数。`)
  }
  return parsed
}

function buildProbeCreateInput(form: ProbeCreateFormState): CreateProbeItemInput {
  const timeoutSeconds = parseRequiredPositiveInteger(form.timeoutSeconds, '超时秒数')
  if (form.probeKind === 'tcp') {
    return {
      probe_kind: 'tcp',
      enabled: form.enabled,
      frequency_tier: form.frequencyTier,
      timeout_seconds: timeoutSeconds,
      config: { port: parseRequiredPositiveInteger(form.port, '端口') },
    }
  }
  if (form.probeKind === 'http') {
    const start = parseRequiredPositiveInteger(form.expectedStatusStart, '期望状态码起点')
    const end = parseRequiredPositiveInteger(form.expectedStatusEnd, '期望状态码终点')
    if (start > end) {
      throw new Error('期望状态码起点不能大于终点。')
    }
    return {
      probe_kind: 'http',
      enabled: form.enabled,
      frequency_tier: form.frequencyTier,
      timeout_seconds: timeoutSeconds,
      config: {
        scheme: form.httpScheme.trim(),
        path: form.httpPath.trim() || '/',
        method: form.httpMethod,
        expected_status_range: [start, end],
      },
    }
  }
  return {
    probe_kind: 'tls',
    enabled: form.enabled,
    frequency_tier: form.frequencyTier,
    timeout_seconds: timeoutSeconds,
    config: {
      port: parseRequiredPositiveInteger(form.port, '端口'),
      expiry_warning_days: parseRequiredPositiveInteger(form.tlsExpiryWarningDays, '证书预警天数'),
    },
  }
}

function buildProbeUpdateInput(form: ProbeCreateFormState): UpdateProbeItemInput {
  return buildProbeCreateInput(form)
}

function initialProbeCreateFormForTarget(target: TargetRecord): ProbeCreateFormState {
  return {
    ...initialProbeCreateForm,
    port: target.base_port ? String(target.base_port) : '',
  }
}

function probeCreateFormForKind(
  current: ProbeCreateFormState,
  probeKind: ProbeKind,
): ProbeCreateFormState {
  return {
    ...current,
    probeKind,
    frequencyTier: DEFAULT_FREQUENCY_BY_PROBE_KIND[probeKind],
  }
}

function formStateForProbeItem(probeItem: ProbeItemRecord): ProbeCreateFormState {
  const config = probeItem.config as Record<string, unknown>
  if (probeItem.probe_kind === 'http') {
    const range = Array.isArray(config.expected_status_range)
      ? config.expected_status_range
      : []
    return {
      ...initialProbeCreateForm,
      probeKind: 'http',
      enabled: probeItem.enabled,
      frequencyTier: probeItem.frequency_tier,
      timeoutSeconds: String(probeItem.timeout_seconds),
      httpScheme: typeof config.scheme === 'string' ? config.scheme : 'https',
      httpPath: typeof config.path === 'string' ? config.path : '/',
      httpMethod: config.method === 'HEAD' ? 'HEAD' : 'GET',
      expectedStatusStart: String(typeof range[0] === 'number' ? range[0] : 200),
      expectedStatusEnd: String(typeof range[1] === 'number' ? range[1] : 299),
    }
  }
  if (probeItem.probe_kind === 'tls') {
    return {
      ...initialProbeCreateForm,
      probeKind: 'tls',
      enabled: probeItem.enabled,
      frequencyTier: probeItem.frequency_tier,
      timeoutSeconds: String(probeItem.timeout_seconds),
      port: String(typeof config.port === 'number' ? config.port : ''),
      tlsExpiryWarningDays: String(
        typeof config.expiry_warning_days === 'number' ? config.expiry_warning_days : 14,
      ),
    }
  }
  return {
    ...initialProbeCreateForm,
    probeKind: 'tcp',
    enabled: probeItem.enabled,
    frequencyTier: probeItem.frequency_tier,
    timeoutSeconds: String(probeItem.timeout_seconds),
    port: String(typeof config.port === 'number' ? config.port : ''),
  }
}

function hasUnsupportedProbeConfigFields(probeItem: ProbeItemRecord): boolean {
  const config = probeItem.config
  if (config == null || typeof config !== 'object' || Array.isArray(config)) {
    return true
  }
  const allowedKeys = PROBE_CONFIG_KEYS[probeItem.probe_kind]
  return Object.keys(config).some((key) => !allowedKeys.has(key))
}

function focusRestoreActionAfterSuccess(action: TargetRuntimeAction): TargetRuntimeAction {
  switch (action) {
    case 'enter-maintenance':
      return 'exit-maintenance'
    case 'exit-maintenance':
      return 'enter-maintenance'
    case 'pause':
      return 'resume'
    case 'resume':
      return 'pause'
    case 'archive':
      return 'restore-to-paused'
    case 'restore-to-paused':
      return 'resume'
  }
}

function mergeRuntimeTargetRecord(current: TargetRecord, updated: TargetRecord): TargetRecord {
  return {
    ...updated,
    labels: current.labels,
    note: current.note,
  }
}

export function TargetDetailPage() {
  const { targetId } = useParams()
  return <TargetDetailPageContent key={targetId ?? 'missing-target'} targetId={targetId} />
}

function TargetDetailPageContent({ targetId }: { targetId?: string }) {
  const [state, setState] = useState<State>({
    requestedTargetId: null,
    error: null,
    target: null,
    probeItems: [],
    runtimeFacts: null,
    requestedActivityTargetId: null,
    incidents: [],
    incidentsError: null,
    events: [],
    eventsError: null,
  })
  const [runtimeSubmitting, setRuntimeSubmitting] = useState(false)
  const [runtimeError, setRuntimeError] = useState<string | null>(null)
  const [pendingRuntimeConfirmation, setPendingRuntimeConfirmation] =
    useState<PendingRuntimeConfirmation | null>(null)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [historyTab, setHistoryTab] = useState<'events' | 'incidents'>('events')
  const [historyIncidents, setHistoryIncidents] = useState<ActiveIncidentRecord[] | null>(
    null,
  )
  const [historyIncidentsLoading, setHistoryIncidentsLoading] = useState(false)
  const [historyIncidentsError, setHistoryIncidentsError] = useState<string | null>(null)
  const [timeWindow, setTimeWindow] = useState<'24h' | '7d' | '30d'>('24h')
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
    initialProbeCreateForm,
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
  const events = hasCurrentActivity ? state.events : []
  const eventsError = hasCurrentActivity ? state.eventsError : null
  const runtimeConfirmationActive = pendingRuntimeConfirmation !== null
  const probeConfirmationActive = pendingProbeConfirmation !== null

  const showDangerZone = target ? target.current_active_incident_count > 0 : false
  const firstIncident =
    incidents.length > 0
      ? [...incidents].sort(
          (a, b) =>
            new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
        )[0]
      : null

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
    return <section className="page-panel">正在加载目标详情…</section>
  }

  if (missingTargetId || error || !target) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">目标详情</p>
        <h2 className="page-panel__title">目标详情不可用</h2>
        <p className="page-panel__description">{error ?? '未找到目标'}</p>
        <Link className="text-link" to="/targets">
          返回目标列表
        </Link>
      </section>
    )
  }

  const probeRowMutationBusy = probeMutationBusyId !== null
  const probeActionsDisabled =
    probeCreateSubmitting || probeRowMutationBusy || runtimeConfirmationActive || probeConfirmationActive
  const isArchived = target.run_status === '已归档'

  return (
    <div className="page-stack">
      <TargetWatchtowerHeader
        target={target}
        runtimeSubmitting={runtimeSubmitting}
        disabled={probeConfirmationActive}
        onRuntimeAction={(action) => void handleRuntimeAction(action)}
        registerActionRef={registerActionRef}
        onOpenHistory={() => openHistory('events')}
      />

      {showDangerZone ? (
        <Card cardRole="warning" className="watchtower-danger" aria-label="当前主问题">
          <p className="watchtower-danger__eyebrow">当前主问题</p>
          <h2 className="watchtower-danger__summary">
            {target.current_primary_issue_summary || '存在活跃异常'}
          </h2>
          <p className="watchtower-danger__meta">
            共{' '}
            <MonoDigits>{target.current_active_incident_count}</MonoDigits>{' '}
            个活跃异常 · 健康状态{' '}
            <StatusBadge label={target.current_health_status} />
            {firstIncident?.started_at ? (
              <>
                {' '}
                · 持续 <Timestamp value={firstIncident.started_at} mode="relative" />
              </>
            ) : null}
          </p>
          <div className="watchtower-danger__actions">
            <Button variant="ghost" size="sm" onClick={() => openHistory('events')}>
              查看完整时间线 →
            </Button>
          </div>
        </Card>
      ) : null}

      {pendingRuntimeConfirmation?.action === 'pause' ? (
        <ActionConfirmationCard
          title="确认暂停目标监控"
          current={
            target.run_status === '维护中'
              ? '当前：目标运行状态为维护中。'
              : '当前：目标运行状态为启用。'
          }
          result="操作后：目标运行状态变为暂停。"
          impact="会停止该目标下所有 ProbeItem 的执行，不再产生新的目标观测记录。"
          unchanged="不会删除历史事件、观测记录或 ProbeItem 配置。"
          confirmLabel="确认暂停目标"
          disabled={runtimeSubmitting}
          onConfirm={() => void handleRuntimeAction('pause', true)}
          onCancel={() => {
            pendingRuntimeFocusRestoreRef.current = 'pause'
            setPendingRuntimeConfirmation(null)
          }}
        />
      ) : null}
      {runtimeError ? (
        <p className="watchtower-runtime-error" role="alert">
          {runtimeError}
        </p>
      ) : null}

      <div className="watchtower-window-tabs">
        <Tabs<'24h' | '7d' | '30d'>
          variant="pill"
          value={timeWindow}
          onChange={setTimeWindow}
          items={[
            { value: '24h', label: '24h' },
            { value: '7d', label: '7d' },
            { value: '30d', label: '30d' },
          ]}
        />
      </div>

      <TargetLatencyTrends
        probeItems={probeItems}
        recentObservations={recentObservations}
        isMaintenance={target.run_status === '维护中'}
        watchtower
      />

      <div>
        <button
          ref={addProbeButtonRef}
          type="button"
          disabled={probeCreateSubmitting || runtimeConfirmationActive || probeConfirmationActive}
          onClick={() => {
            if (probeCreateSubmitting) return
            if (probeCreateOpen && probeFormMode.kind === 'create') {
              probeFormRequestRef.current += 1
              setProbeCreateOpen(false)
              return
            }
            openProbeCreateForm(target)
          }}
        >
          添加 ProbeItem
        </button>
        {probeMutationError ? <p>{probeMutationError}</p> : null}
        {probeCreateOpen ? (
          <TargetProbeForm
            mode={probeFormMode}
            form={probeCreateForm}
            submitting={probeCreateSubmitting}
            error={probeCreateError}
            onSubmit={handleProbeCreate}
            onProbeKindChange={(probeKind) => {
              if (probeFormMode.kind === 'edit') {
                updateProbeCreateField('probeKind', probeKind)
                return
              }
              setProbeCreateForm((current) => probeCreateFormForKind(current, probeKind))
            }}
            onFieldChange={updateProbeCreateField}
          />
        ) : null}
      </div>

      <details className="watchtower-secondary">
        <summary>标签与备注</summary>
        <div className="watchtower-secondary__body">
          <TargetLabelsAndNote
            target={target}
            editing={metadataEditing}
            groupDraft={metadataForm.group}
            labelDraft={metadataForm.labels}
            noteDraft={metadataForm.note}
            submitting={metadataSubmitting}
            error={metadataError}
            onGroupDraftChange={(value) => updateMetadataField('group', value)}
            onLabelDraftChange={(value) => updateMetadataField('labels', value)}
            onNoteDraftChange={(value) => updateMetadataField('note', value)}
            onStartEdit={() => {
              setMetadataEditing(true)
              setMetadataError(null)
              setMetadataForm({
                group: target.group || '',
                labels: target.labels.join(', '),
                note: target.note,
              })
            }}
            onCancelEdit={() => {
              setMetadataEditing(false)
              setMetadataError(null)
              setMetadataForm({
                group: target.group || '',
                labels: target.labels.join(', '),
                note: target.note,
              })
            }}
            onSubmit={handleMetadataSave}
          />
        </div>
      </details>

      <details className="watchtower-secondary">
        <summary>ProbeItem 列表</summary>
        <div className="watchtower-secondary__body">
          <TargetProbeList
            probeItems={probeItems}
            observationsByProbe={observationsByProbe}
            actionsDisabled={probeActionsDisabled}
            pendingProbeConfirmation={pendingProbeConfirmation}
            confirmationCardDisabled={probeCreateSubmitting || probeRowMutationBusy}
            pendingProbeConfirmationCardRef={pendingProbeConfirmationCardRef}
            registerDeleteButtonRef={(probeItemId, element) => {
              probeDeleteButtonRefs.current[probeItemId] = element
            }}
            onAddProbe={() => openProbeCreateForm(target)}
            onEdit={(probeItem) => openProbeEditForm(probeItem)}
            onToggle={(probeItem) => void handleToggleProbeItem(probeItem)}
            onDelete={(probeItem) => void handleDeleteProbeItem(probeItem)}
            onConfirmDelete={(probeItem) => void handleDeleteProbeItem(probeItem, true)}
            onCancelDeleteConfirmation={(probeItem) => {
              pendingProbeFocusRestoreRef.current = {
                probeItemId: probeItem.probe_item_id,
              }
              setPendingProbeConfirmation((current) =>
                current?.probeItemId === probeItem.probe_item_id ? null : current,
              )
            }}
          />
        </div>
      </details>

      <details className="watchtower-secondary">
        <summary>生命周期</summary>
        <div className="watchtower-secondary__body">
          <div className="page-stack">
            {isArchived ? (
              <div>
                <p>目标处于已归档状态，可恢复至暂停以重新纳入工作集。</p>
                <button
                  type="button"
                  ref={(element) => {
                    runtimeActionButtonRefs.current['restore-to-paused'] = element
                  }}
                  disabled={runtimeSubmitting}
                  onClick={() => void handleRuntimeAction('restore-to-paused')}
                >
                  {runtimeSubmitting ? '正在恢复…' : '恢复到暂停'}
                </button>
              </div>
            ) : (
              <>
                <p>归档会退出当前工作集并保留历史。这不是删除，也不会清空事件、观测记录或 ProbeItem 配置。</p>
                {pendingRuntimeConfirmation?.action === 'archive' ? (
                  <ActionConfirmationCard
                    title="确认归档目标"
                    current="当前：目标仍在当前工作集中。"
                    result="操作后：目标退出当前工作集，运行状态变为已归档。"
                    impact="归档后不会继续作为活跃目标参与观测、异常判定或通知。"
                    unchanged="不会删除历史事件、观测记录或 ProbeItem 配置。后续可恢复到暂停。"
                    confirmLabel="确认归档"
                    disabled={runtimeSubmitting}
                    onConfirm={() => void handleRuntimeAction('archive', true)}
                    onCancel={() => {
                      pendingRuntimeFocusRestoreRef.current = 'archive'
                      setPendingRuntimeConfirmation(null)
                    }}
                  />
                ) : (
                  <div className="badge-row badge-row--wrap">
                    <button
                      type="button"
                      ref={(element) => {
                        runtimeActionButtonRefs.current.archive = element
                      }}
                      disabled={runtimeSubmitting || probeConfirmationActive}
                      onClick={() => void handleRuntimeAction('archive')}
                    >
                      归档
                    </button>
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      </details>

      <p className="watchtower-snapshot-meta">
        数据快照时间：<Timestamp value={new Date().toISOString()} mode="absolute" />
        ，刷新页面获取最新。
      </p>

      <Drawer
        open={historyOpen}
        onClose={() => setHistoryOpen(false)}
        title={`${target.name} · 历史`}
        ariaLabel="目标历史抽屉"
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
              <p>该目标近期没有发生过被记录的状态变更事件。</p>
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
            emptyDescription="该目标近期没有触发过被记录的异常。"
          />
        ) : (
          <div className="empty-state">
            <h3>近期无异常发生</h3>
            <p>该目标近期没有触发过被记录的异常。</p>
          </div>
        )}
      </Drawer>
    </div>
  )
}

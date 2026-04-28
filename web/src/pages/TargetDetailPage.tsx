import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { ActionConfirmationCard } from '../components/ActionConfirmationCard'
import { DetailSection } from '../components/DetailSection'
import { EventList } from '../components/EventList'
import { IncidentList } from '../components/IncidentList'
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
  listIncidents,
  listTargetProbeItems,
  pauseTarget,
  restoreTargetToPaused,
  resumeTarget,
  updateTargetMetadata,
  updateProbeItem,
} from '../lib/api'
import {
  formatConfigSummary,
  formatDateTime,
  formatLabelList,
  formatLatency,
} from '../lib/format'
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

const PROBE_KIND_OPTIONS = [
  { value: 'tcp', label: 'TCP' },
  { value: 'http', label: 'HTTP' },
  { value: 'tls', label: 'TLS' },
] as const

const FREQUENCY_TIER_OPTIONS = [
  { value: '1m', label: '1 分钟' },
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
  { value: '6h', label: '6 小时' },
] as const

const PROBE_CONFIG_KEYS: Record<ProbeKind, Set<string>> = {
  tcp: new Set(['port']),
  http: new Set(['scheme', 'path', 'method', 'expected_status_range']),
  tls: new Set(['port', 'expiry_warning_days']),
}

type ProbeCreateFormState = {
  probeKind: ProbeKind
  enabled: boolean
  frequencyTier: FrequencyTier
  timeoutSeconds: string
  port: string
  httpScheme: string
  httpPath: string
  httpMethod: 'GET' | 'HEAD'
  expectedStatusStart: string
  expectedStatusEnd: string
  tlsExpiryWarningDays: string
}

type ProbeFormMode = { kind: 'create' } | { kind: 'edit'; probeItemId: string }

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

type TargetRuntimeAction =
  | 'enter-maintenance'
  | 'exit-maintenance'
  | 'pause'
  | 'resume'
  | 'archive'
  | 'restore-to-paused'

type PendingRuntimeConfirmation = {
  action: 'pause' | 'archive'
}

type PendingProbeConfirmation = {
  probeItemId: string
  action: 'delete'
}

type ProbeFocusRestoreRequest = {
  probeItemId?: string
}

type MetadataFormState = {
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

function probeActionAccessibleName(action: string, probeItem: ProbeItemRecord): string {
  return `${action} ProbeItem ${probeItem.probe_item_id} ${probeItem.probe_kind.toUpperCase()} ${formatConfigSummary(probeItem.config)}`
}

function pauseConfirmationCurrent() {
  return '当前：目标运行状态为启用或维护中。'
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

function targetRuntimeActions(
  target: TargetRecord,
): Array<{ action: TargetRuntimeAction; label: string }> {
  if (target.run_status === '启用') {
    return [
      { action: 'enter-maintenance', label: '进入维护' },
      { action: 'pause', label: '暂停' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '维护中') {
    return [
      { action: 'exit-maintenance', label: '退出维护' },
      { action: 'pause', label: '暂停' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '暂停') {
    return [
      { action: 'resume', label: '恢复' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '已归档') {
    return [{ action: 'restore-to-paused', label: '恢复到暂停' }]
  }

  return []
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
  const [probeCreateOpen, setProbeCreateOpen] = useState(false)
  const [probeFormMode, setProbeFormMode] = useState<ProbeFormMode>({ kind: 'create' })
  const [probeCreateSubmitting, setProbeCreateSubmitting] = useState(false)
  const [probeCreateError, setProbeCreateError] = useState<string | null>(null)
  const [probeMutationError, setProbeMutationError] = useState<string | null>(null)
  const [probeMutationBusyId, setProbeMutationBusyId] = useState<string | null>(null)
  const [metadataEditing, setMetadataEditing] = useState(false)
  const [metadataSubmitting, setMetadataSubmitting] = useState(false)
  const [metadataError, setMetadataError] = useState<string | null>(null)
  const [metadataForm, setMetadataForm] = useState<MetadataFormState>({ labels: '', note: '' })
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
    currentRequestedTargetIdRef.current = state.requestedTargetId
  }, [state.requestedTargetId])

  useEffect(
    () => () => {
      isMountedRef.current = false
    },
    [],
  )

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

  const observationsByProbe = useMemo(() => {
    const map = new Map<string, ProbeObservation[]>()
    for (const observation of state.runtimeFacts?.latest_probe_observations ?? []) {
      const existing = map.get(observation.probe_item_id) ?? []
      existing.push(observation)
      map.set(observation.probe_item_id, existing)
    }
    return map
  }, [state.runtimeFacts])

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

  useEffect(() => {
    if (!target) return
    setMetadataEditing(false)
    setMetadataSubmitting(false)
    setMetadataError(null)
    setMetadataForm({
      labels: target.labels.join(', '),
      note: target.note,
    })
    metadataRequestRef.current += 1
  }, [target])

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
      const updated = await updateTargetMetadata(actionTargetId, {
        labels: dedupeLabels(parseLabels(metadataForm.labels)),
        note: metadataForm.note.trim(),
      })
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
        target: updated,
      }))
      setMetadataEditing(false)
    } catch {
      if (
        !isMountedRef.current ||
        currentRouteTargetIdRef.current !== actionTargetId ||
        currentRequestedTargetIdRef.current !== actionTargetId ||
        metadataRequestRef.current !== requestId
      ) {
        return
      }
      setMetadataError('标签或备注更新失败')
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
        target: updated,
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
        <p className="page-panel__eyebrow">Target Detail</p>
        <h2 className="page-panel__title">目标详情不可用</h2>
        <p className="page-panel__description">{error ?? '未找到目标'}</p>
        <Link className="text-link" to="/targets">
          返回目标列表
        </Link>
      </section>
    )
  }

  const probeRowMutationBusy = probeMutationBusyId !== null
  const runtimeActionsDisabled = runtimeSubmitting || probeConfirmationActive || runtimeConfirmationActive
  const probeActionsDisabled =
    probeCreateSubmitting || probeRowMutationBusy || runtimeConfirmationActive || probeConfirmationActive

  return (
    <div className="page-stack">
      <section className="hero-panel">
        <div className="hero-panel__content">
          <p className="hero-panel__eyebrow">Target Detail</p>
          <h2 className="hero-panel__title">{target.name}</h2>
          <p className="hero-panel__description">
            {target.target_type} · {target.host}
            {target.base_port ? `:${target.base_port}` : ''}
          </p>
          <div className="badge-row">
            <StatusBadge label={target.run_status} />
            <StatusBadge label={target.current_health_status} />
            <StatusBadge label={target.target_type} />
          </div>
        </div>
        <div className="hero-panel__meta">
          <div className="hero-meta-card">
            <span>标签</span>
            <strong>{formatLabelList(target.labels)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>执行节点标签</span>
            <strong>{formatLabelList(target.execution_node_labels)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>最近成功</span>
            <strong>{formatDateTime(target.last_success_at)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>最近失败</span>
            <strong>{formatDateTime(target.last_failure_at)}</strong>
          </div>
        </div>
      </section>

      <div className="summary-grid">
        <article className="summary-card">
          <p className="summary-card__label">健康状态</p>
          <p className="summary-card__value">{target.current_health_status}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">ProbeItem 数量</p>
          <p className="summary-card__value">{probeItems.length}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">当前主问题</p>
          <p className="summary-card__value summary-card__value--text">
            {target.current_primary_issue_summary || '暂无明显异常'}
          </p>
        </article>
      </div>

      <DetailSection eyebrow="Metadata" title="标签与备注">
        <div className="page-stack">
          {metadataEditing ? (
            <form onSubmit={handleMetadataSave} className="page-stack">
              <label>
                标签
                <input
                  name="metadata-labels"
                  value={metadataForm.labels}
                  onChange={(event) => updateMetadataField('labels', event.target.value)}
                />
              </label>
              <label>
                备注
                <textarea
                  name="metadata-note"
                  value={metadataForm.note}
                  onChange={(event) => updateMetadataField('note', event.target.value)}
                  rows={3}
                />
              </label>
              <div className="badge-row badge-row--wrap">
                <button type="submit" disabled={metadataSubmitting}>
                  {metadataSubmitting ? '正在保存…' : '保存标签与备注'}
                </button>
                <button
                  type="button"
                  disabled={metadataSubmitting}
                  onClick={() => {
                    setMetadataEditing(false)
                    setMetadataError(null)
                    setMetadataForm({
                      labels: target.labels.join(', '),
                      note: target.note,
                    })
                  }}
                >
                  取消
                </button>
              </div>
            </form>
          ) : (
            <>
              <p>标签：{formatLabelList(target.labels)}</p>
              <p>{target.note || '暂无备注'}</p>
              <div>
                <button
                  type="button"
                  onClick={() => {
                    setMetadataEditing(true)
                    setMetadataError(null)
                    setMetadataForm({
                      labels: target.labels.join(', '),
                      note: target.note,
                    })
                  }}
                >
                  编辑标签与备注
                </button>
              </div>
            </>
          )}
          {metadataError ? <p>{metadataError}</p> : null}
        </div>
      </DetailSection>

      <DetailSection eyebrow="Runtime Control" title="运行控制">
        <div className="page-stack">
          <p>维护会继续采集，但不解释结果。暂停会停止采集并产生数据空档。归档会退出当前工作集并保留历史。</p>
          <div className="badge-row badge-row--wrap">
            {targetRuntimeActions(target).map(({ action, label }) => (
              <button
                key={action}
                ref={(element) => {
                  runtimeActionButtonRefs.current[action] = element
                }}
                type="button"
                disabled={runtimeActionsDisabled}
                onClick={() => void handleRuntimeAction(action)}
              >
                {label}
              </button>
            ))}
          </div>
          {pendingRuntimeConfirmation ? (
            <ActionConfirmationCard
              title={
                pendingRuntimeConfirmation.action === 'pause'
                  ? '确认暂停目标监控'
                  : '确认归档目标'
              }
              current={
                pendingRuntimeConfirmation.action === 'pause'
                  ? pauseConfirmationCurrent()
                  : '当前：目标仍在当前工作集中。'
              }
              result={
                pendingRuntimeConfirmation.action === 'pause'
                  ? '操作后：目标运行状态变为暂停。'
                  : '操作后：目标退出当前工作集，运行状态变为已归档。'
              }
              impact={
                pendingRuntimeConfirmation.action === 'pause'
                  ? '会停止该 Target 下所有 ProbeItem 的执行，不再产生新的目标 observation。'
                  : '归档后不会继续作为活跃目标参与观测、异常判定或通知。'
              }
              unchanged={
                pendingRuntimeConfirmation.action === 'pause'
                  ? '不会删除历史事件、观测记录或 ProbeItem 配置。'
                  : '不会删除历史事件、观测记录或 ProbeItem 配置。后续可恢复到暂停。'
              }
              confirmLabel={
                pendingRuntimeConfirmation.action === 'pause' ? '确认暂停目标' : '确认归档'
              }
              disabled={runtimeSubmitting}
              onConfirm={() => void handleRuntimeAction(pendingRuntimeConfirmation.action, true)}
              onCancel={() => {
                pendingRuntimeFocusRestoreRef.current = pendingRuntimeConfirmation.action
                setPendingRuntimeConfirmation(null)
              }}
            />
          ) : null}
          {runtimeError ? <p>{runtimeError}</p> : null}
        </div>
      </DetailSection>

      <DetailSection eyebrow="Probe Items" title="ProbeItem 列表">
        <div className="page-stack">
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
          </div>
          {probeMutationError ? <p>{probeMutationError}</p> : null}
          {probeCreateOpen ? (
            <section className="page-panel">
              <p className="page-panel__eyebrow">
                {probeFormMode.kind === 'edit' ? 'ProbeItem Edit' : 'ProbeItem Create'}
              </p>
              <h3 className="page-panel__title">
                {probeFormMode.kind === 'edit' ? '编辑 ProbeItem' : '创建 ProbeItem'}
              </h3>
              <form onSubmit={handleProbeCreate}>
                <p>
                  <label>
                    Probe 类型
                    <select
                      name="probeKind"
                      value={probeCreateForm.probeKind}
                      onChange={(event) =>
                        updateProbeCreateField(
                          'probeKind',
                          event.target.value as ProbeCreateFormState['probeKind'],
                        )
                      }
                    >
                      {PROBE_KIND_OPTIONS.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  </label>
                </p>
                <p>
                  <label>
                    <input
                      name="enabled"
                      type="checkbox"
                      checked={probeCreateForm.enabled}
                      onChange={(event) =>
                        updateProbeCreateField('enabled', event.target.checked)
                      }
                    />
                    启用 ProbeItem
                  </label>
                </p>
                <p>
                  <label>
                    频率档位
                    <select
                      name="frequencyTier"
                      value={probeCreateForm.frequencyTier}
                      onChange={(event) =>
                        updateProbeCreateField(
                          'frequencyTier',
                          event.target.value as ProbeCreateFormState['frequencyTier'],
                        )
                      }
                    >
                      {FREQUENCY_TIER_OPTIONS.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  </label>
                </p>
                <p>
                  <label>
                    超时秒数
                    <input
                      name="timeoutSeconds"
                      inputMode="numeric"
                      value={probeCreateForm.timeoutSeconds}
                      onChange={(event) =>
                        updateProbeCreateField('timeoutSeconds', event.target.value)
                      }
                    />
                  </label>
                </p>
                {probeCreateForm.probeKind !== 'http' ? (
                  <p>
                    <label>
                      端口
                      <input
                        name="port"
                        inputMode="numeric"
                        value={probeCreateForm.port}
                        onChange={(event) =>
                          updateProbeCreateField('port', event.target.value)
                        }
                      />
                    </label>
                  </p>
                ) : null}
                {probeCreateForm.probeKind === 'http' ? (
                  <>
                    <p>
                      <label>
                        HTTP Scheme
                        <select
                          name="httpScheme"
                          value={probeCreateForm.httpScheme}
                          onChange={(event) =>
                            updateProbeCreateField('httpScheme', event.target.value)
                          }
                        >
                          <option value="http">http</option>
                          <option value="https">https</option>
                        </select>
                      </label>
                    </p>
                    <p>
                      <label>
                        HTTP Path
                        <input
                          name="httpPath"
                          value={probeCreateForm.httpPath}
                          onChange={(event) =>
                            updateProbeCreateField('httpPath', event.target.value)
                          }
                        />
                      </label>
                    </p>
                    <p>
                      <label>
                        HTTP Method
                        <select
                          name="httpMethod"
                          value={probeCreateForm.httpMethod}
                          onChange={(event) =>
                            updateProbeCreateField(
                              'httpMethod',
                              event.target.value as ProbeCreateFormState['httpMethod'],
                            )
                          }
                        >
                          <option value="GET">GET</option>
                          <option value="HEAD">HEAD</option>
                        </select>
                      </label>
                    </p>
                    <p>
                      <label>
                        期望状态码起点
                        <input
                          name="expectedStatusStart"
                          inputMode="numeric"
                          value={probeCreateForm.expectedStatusStart}
                          onChange={(event) =>
                            updateProbeCreateField(
                              'expectedStatusStart',
                              event.target.value,
                            )
                          }
                        />
                      </label>
                    </p>
                    <p>
                      <label>
                        期望状态码终点
                        <input
                          name="expectedStatusEnd"
                          inputMode="numeric"
                          value={probeCreateForm.expectedStatusEnd}
                          onChange={(event) =>
                            updateProbeCreateField(
                              'expectedStatusEnd',
                              event.target.value,
                            )
                          }
                        />
                      </label>
                    </p>
                  </>
                ) : null}
                {probeCreateForm.probeKind === 'tls' ? (
                  <p>
                    <label>
                      证书预警天数
                      <input
                        name="tlsExpiryWarningDays"
                        inputMode="numeric"
                        value={probeCreateForm.tlsExpiryWarningDays}
                        onChange={(event) =>
                          updateProbeCreateField(
                            'tlsExpiryWarningDays',
                            event.target.value,
                          )
                        }
                      />
                    </label>
                  </p>
                ) : null}
                {probeCreateError ? <p>{probeCreateError}</p> : null}
                <div>
                  <button type="submit" disabled={probeCreateSubmitting}>
                    {probeCreateSubmitting
                      ? probeFormMode.kind === 'edit'
                        ? '正在保存…'
                        : '正在创建…'
                      : probeFormMode.kind === 'edit'
                        ? '保存 ProbeItem'
                        : '创建 ProbeItem'}
                  </button>
                </div>
              </form>
            </section>
          ) : null}
          {probeItems.length === 0 ? (
            <div className="empty-state">
              <h3>当前还没有 ProbeItem</h3>
              <p>当前还没有 ProbeItem，请为该入口添加至少一种观测方式。</p>
            </div>
          ) : (
            <div className="probe-list">
              {probeItems.map((probeItem) => {
                const observations = observationsByProbe.get(probeItem.probe_item_id) ?? []
                return (
                  <article key={probeItem.probe_item_id} className="probe-card">
                    <header className="probe-card__header">
                      <div>
                        <h3>{probeItem.probe_kind.toUpperCase()}</h3>
                        <p>{formatConfigSummary(probeItem.config)}</p>
                      </div>
                      <div className="badge-row">
                        <StatusBadge label={probeItem.enabled ? '启用' : '停用'} />
                        <StatusBadge label={probeItem.frequency_tier} tone="cyan" />
                      </div>
                    </header>

                    <div className="badge-row badge-row--wrap">
                      <button
                        type="button"
                        aria-label={probeActionAccessibleName('编辑', probeItem)}
                        disabled={probeActionsDisabled}
                        onClick={() => openProbeEditForm(probeItem)}
                      >
                        编辑
                      </button>
                      <button
                        type="button"
                        aria-label={probeActionAccessibleName(
                          probeItem.enabled ? '停用' : '启用',
                          probeItem,
                        )}
                        disabled={probeActionsDisabled}
                        onClick={() => void handleToggleProbeItem(probeItem)}
                      >
                        {probeItem.enabled ? '停用' : '启用'}
                      </button>
                      <button
                        ref={(element) => {
                          probeDeleteButtonRefs.current[probeItem.probe_item_id] = element
                        }}
                        type="button"
                        aria-label={probeActionAccessibleName('删除', probeItem)}
                        disabled={probeActionsDisabled}
                        onClick={() => void handleDeleteProbeItem(probeItem)}
                      >
                        删除
                      </button>
                    </div>
                    {pendingProbeConfirmation?.probeItemId === probeItem.probe_item_id ? (
                      <div ref={pendingProbeConfirmationCardRef}>
                        <ActionConfirmationCard
                          title="确认删除 ProbeItem"
                          current="当前：这条 ProbeItem 仍属于当前 Target。"
                          result="操作后：这条观测方式会被移除。"
                          impact="仅用于误建场景。删除后该 ProbeItem 不再产生新的 observation。"
                          unchanged="不会删除 Target，也不会删除既有事件或历史观测记录。"
                          confirmLabel="确认删除 ProbeItem"
                          disabled={probeCreateSubmitting || probeRowMutationBusy}
                          onConfirm={() => void handleDeleteProbeItem(probeItem, true)}
                          onCancel={() => {
                            pendingProbeFocusRestoreRef.current = {
                              probeItemId: probeItem.probe_item_id,
                            }
                            setPendingProbeConfirmation((current) =>
                              current?.probeItemId === probeItem.probe_item_id ? null : current,
                            )
                          }}
                        />
                      </div>
                    ) : null}

                    <dl className="probe-card__meta">
                      <div>
                        <dt>超时</dt>
                        <dd>{probeItem.timeout_seconds}s</dd>
                      </div>
                      <div>
                        <dt>最近观测</dt>
                        <dd>
                          {observations.length > 0
                            ? formatDateTime(observations[0].observed_at)
                            : '尚无观测结果'}
                        </dd>
                      </div>
                    </dl>

                    {observations.length > 0 ? (
                      <div className="observation-list">
                        {observations.map((observation) => (
                          <div
                            key={`${observation.probe_item_id}-${observation.node_id}`}
                            className="observation-row"
                          >
                            <div>
                              <strong>{observation.node_id}</strong>
                              <p>{formatDateTime(observation.observed_at)}</p>
                            </div>
                            <div>
                              <StatusBadge
                                label={
                                  observation.result_kind === 'success' ? '成功' : '失败'
                                }
                                tone={
                                  observation.result_kind === 'success' ? 'green' : 'red'
                                }
                              />
                            </div>
                            <div>
                              <span>Latency</span>
                              <strong>{formatLatency(observation.latency_ms)}</strong>
                            </div>
                            <div>
                              <span>HTTP / TLS</span>
                              <strong>
                                {observation.http_status ?? observation.tls_expiry_days ?? '—'}
                              </strong>
                            </div>
                            <div>
                              <span>错误摘要</span>
                              <strong>
                                {observation.error_summary || observation.error_code || '—'}
                              </strong>
                            </div>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <div className="empty-inline">尚无观测结果</div>
                    )}
                  </article>
                )
              })}
            </div>
          )}
        </div>
      </DetailSection>

      <DetailSection eyebrow="Incidents" title="当前活跃异常">
        {!hasCurrentActivity ? (
          <div className="empty-state">
            <h3>正在加载活跃异常…</h3>
            <p>等待目标相关的 incident 读模型返回最新结果。</p>
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
            <p>等待目标相关的事件流返回最新记录。</p>
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

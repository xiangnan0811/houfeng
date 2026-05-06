import { type FormEvent, type ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { ActionConfirmationCard } from '../components/ActionConfirmationCard'
import { StatusBadge } from '../components/StatusBadge'
import {
  Button,
  DataTable,
  type DataTableColumn,
  type HealthState,
  Hostname,
  MonoDigits,
  Sparkline,
  StatusGlyph,
  Timestamp,
} from '../components/atoms'
import {
  FilterBar,
  FilterChip,
  FilterMultiSelect,
  FilterSelect,
  FilterToggle,
} from '../components/filters'
import {
  ApiError,
  archiveTarget,
  createTarget,
  enterTargetMaintenance,
  exitTargetMaintenance,
  listTargetSparklines,
  listTargets,
  pauseTarget,
  restoreTargetToPaused,
  resumeTarget,
  updateTargetMetadata,
} from '../lib/api'
import { formatLabelList } from '../lib/format'
import type { CreateTargetInput, TargetRecord, TargetSparklinesResponse } from '../lib/types'

const TARGET_TYPE_OPTIONS = [
  { value: 'service', label: 'service' },
  { value: 'china_reference', label: 'china_reference' },
] as const

const TARGET_RUN_STATUS_OPTIONS = [
  { value: '启用', label: '启用' },
  { value: '维护中', label: '维护中' },
  { value: '暂停', label: '暂停' },
] as const

const TARGET_RUN_STATUS_FILTER_OPTIONS = [
  { value: '启用', label: '启用' },
  { value: '维护中', label: '维护中' },
  { value: '暂停', label: '暂停' },
  { value: '已归档', label: '已归档' },
] as const

const TARGET_HEALTH_STATUS_FILTER_OPTIONS = [
  { value: '正常', label: '正常' },
  { value: '关注', label: '关注' },
  { value: '告警', label: '告警' },
  { value: '严重', label: '严重' },
] as const

type TargetFilterState = {
  group: string | null
  type: string | null
  runStatus: string | null
  health: string | null
  labels: string[]
  executionLabels: string[]
  abnormal: boolean
}

function parseMultiValue(value: string | null): string[] {
  if (!value) return []
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function distinctSorted(values: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const value of values) {
    if (!seen.has(value)) {
      seen.add(value)
      out.push(value)
    }
  }
  return out.sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
}

type CreateTargetFormState = {
  name: string
  targetType: CreateTargetInput['target_type']
  host: string
  basePort: string
  executionNodeLabels: string
  runStatus: CreateTargetInput['run_status']
  group: string
  labels: string
  note: string
}

const initialCreateForm: CreateTargetFormState = {
  name: '',
  targetType: 'service',
  host: '',
  basePort: '',
  executionNodeLabels: '',
  runStatus: '启用',
  group: '',
  labels: '',
  note: '',
}

type TargetRuntimeAction =
  | 'enter-maintenance'
  | 'exit-maintenance'
  | 'pause'
  | 'resume'
  | 'archive'
  | 'restore-to-paused'

type PendingTargetConfirmation = {
  targetId: string
  action: 'pause' | 'archive'
}

type FocusRestoreRequest = {
  targetId: string
  preferredAction: TargetRuntimeAction
}

/** Map target run_status + health into the StatusGlyph state vocabulary.
 *  v1 baseline: maintenance / 暂停 / 已归档 outrank health for at-a-glance scanning. */
function targetGlyphState(target: TargetRecord): HealthState {
  if (target.run_status === '已归档') return 'offline'
  if (target.run_status === '维护中') return 'maintenance'
  if (target.run_status === '暂停') return 'offline'
  switch (target.current_health_status) {
    case '正常':
      return 'normal'
    case '关注':
      return 'notice'
    case '告警':
      return 'alert'
    case '严重':
      return 'critical'
    default:
      return 'offline'
  }
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

function buildCreateTargetInput(form: CreateTargetFormState): CreateTargetInput {
  const executionNodeLabels = parseLabels(form.executionNodeLabels)
  if (executionNodeLabels.length === 0) {
    throw new Error('执行节点标签至少需要填写一个。')
  }

  const basePort = parseOptionalPositiveInteger(form.basePort, '基础端口')
  return {
    name: form.name.trim(),
    target_type: form.targetType,
    host: form.host.trim(),
    ...(basePort == null ? {} : { base_port: basePort }),
    execution_node_labels: executionNodeLabels,
    run_status: form.runStatus,
    group: form.group.trim(),
    labels: parseLabels(form.labels),
    note: form.note.trim(),
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

function pauseConfirmationCurrent() {
  return '当前：目标运行状态为启用或维护中。'
}

function actionButtonKey(targetId: string, action: TargetRuntimeAction) {
  return `${targetId}:${action}`
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

function mergeMetadataTargetRecord(current: TargetRecord, updated: TargetRecord): TargetRecord {
  return {
    ...current,
    group: updated.group,
    labels: updated.labels,
    note: updated.note,
    updated_at: updated.updated_at,
  }
}

function renderLabelsContent(target: TargetRecord): ReactNode {
  if (target.labels.length === 0) return '—'
  const visible = target.labels.slice(0, 3)
  const overflow = target.labels.length - visible.length
  if (overflow === 0) return visible.join(' · ')
  return (
    <>
      {visible.join(' · ')}
      <span className="targets-table__labels-more"> +{overflow}</span>
    </>
  )
}

export function TargetsPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const mountedRef = useRef(false)
  const createRequestRef = useRef(0)
  const [targets, setTargets] = useState<TargetRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [createForm, setCreateForm] = useState<CreateTargetFormState>(initialCreateForm)
  const [runtimeBusyTargetId, setRuntimeBusyTargetId] = useState<string | null>(null)
  const [runtimeErrors, setRuntimeErrors] = useState<Record<string, string>>({})
  const [metadataEditingTargetId, setMetadataEditingTargetId] = useState<string | null>(null)
  const [metadataLabelInput, setMetadataLabelInput] = useState('')
  const [metadataGroupInput, setMetadataGroupInput] = useState('')
  const [metadataSavingTargetId, setMetadataSavingTargetId] = useState<string | null>(null)
  const [metadataErrors, setMetadataErrors] = useState<Record<string, string>>({})
  const [pendingConfirmation, setPendingConfirmation] = useState<PendingTargetConfirmation | null>(null)
  const actionButtonRefs = useRef<Record<string, HTMLButtonElement | null>>({})
  const pendingFocusRestoreRef = useRef<FocusRestoreRequest | null>(null)
  const [sparklines, setSparklines] = useState<TargetSparklinesResponse | null>(null)
  const [selectAll, setSelectAll] = useState(false)
  const [batchSubmitting, setBatchSubmitting] = useState(false)
  const [pendingBatchAction, setPendingBatchAction] = useState<string | null>(null)
  const [batchError, setBatchError] = useState<string | null>(null)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    listTargets()
      .then((result) => {
        if (cancelled) return
        setTargets(result)
        setLoading(false)
      })
      .catch((value: unknown) => {
        if (cancelled) return
        setError(value instanceof ApiError ? value.message : '加载目标列表失败')
        setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    listTargetSparklines()
      .then(data => { if (!cancelled) setSparklines(data) })
      .catch(() => {}) // silent fail
    return () => { cancelled = true }
  }, [])

  function resetCreateFlow() {
    createRequestRef.current += 1
    setCreateSubmitting(false)
    setCreateError(null)
    setCreateForm(initialCreateForm)
  }

  function updateCreateField<K extends keyof CreateTargetFormState>(
    field: K,
    value: CreateTargetFormState[K],
  ) {
    setCreateForm((current) => ({ ...current, [field]: value }))
  }

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreateError(null)

    let payload: CreateTargetInput
    try {
      payload = buildCreateTargetInput(createForm)
    } catch (validationError) {
      setCreateError(describeError(validationError, '创建目标失败'))
      return
    }

    const requestId = createRequestRef.current + 1
    createRequestRef.current = requestId
    setCreateSubmitting(true)
    try {
      const created = await createTarget(payload)
      if (!mountedRef.current || createRequestRef.current !== requestId) return
      setTargets((current) => [
        created,
        ...current.filter((item) => item.target_id !== created.target_id),
      ])
      navigate(`/targets/${created.target_id}`)
    } catch (submitError) {
      if (!mountedRef.current || createRequestRef.current !== requestId) return
      setCreateError(describeError(submitError, '创建目标失败'))
    } finally {
      if (mountedRef.current && createRequestRef.current === requestId) {
        setCreateSubmitting(false)
      }
    }
  }

  function queueFocusRestore(targetId: string, preferredAction: TargetRuntimeAction) {
    pendingFocusRestoreRef.current = { targetId, preferredAction }
  }

  useEffect(() => {
    const request = pendingFocusRestoreRef.current
    if (!request) return

    const preferred = actionButtonRefs.current[actionButtonKey(request.targetId, request.preferredAction)]
    const fallback =
      request.preferredAction === 'pause'
        ? actionButtonRefs.current[actionButtonKey(request.targetId, 'resume')]
        : request.preferredAction === 'archive'
          ? actionButtonRefs.current[actionButtonKey(request.targetId, 'restore-to-paused')]
          : null
    const target = [preferred, fallback].find((element) => element?.isConnected)

    target?.focus()
    pendingFocusRestoreRef.current = null
  }, [targets, pendingConfirmation])

  async function handleRuntimeAction(
    target: TargetRecord,
    action: TargetRuntimeAction,
    confirmed = false,
  ) {
    if ((action === 'pause' || action === 'archive') && !confirmed) {
      setPendingConfirmation({ targetId: target.target_id, action })
      return
    }

    setRuntimeBusyTargetId(target.target_id)
    setRuntimeErrors((current) => {
      if (!current[target.target_id]) return current
      const next = { ...current }
      delete next[target.target_id]
      return next
    })

    try {
      const updated =
        action === 'enter-maintenance'
          ? await enterTargetMaintenance(target.target_id)
          : action === 'exit-maintenance'
            ? await exitTargetMaintenance(target.target_id)
            : action === 'pause'
              ? await pauseTarget(target.target_id)
              : action === 'resume'
                ? await resumeTarget(target.target_id)
                : action === 'archive'
                  ? await archiveTarget(target.target_id)
                  : await restoreTargetToPaused(target.target_id)
      setTargets((current) =>
        current.map((item) =>
          item.target_id === updated.target_id ? mergeRuntimeTargetRecord(item, updated) : item,
        ),
      )
      queueFocusRestore(updated.target_id, focusRestoreActionAfterSuccess(action))
      setPendingConfirmation((current) =>
        current?.targetId === updated.target_id && current.action === action ? null : current,
      )
    } catch (runtimeError) {
      setRuntimeErrors((current) => ({
        ...current,
        [target.target_id]: describeError(runtimeError, '目标运行控制操作失败'),
      }))
    } finally {
      setRuntimeBusyTargetId((current) => (current === target.target_id ? null : current))
    }
  }

  function beginMetadataEdit(target: TargetRecord) {
    if (metadataSavingTargetId) return
    setMetadataEditingTargetId(target.target_id)
    setMetadataLabelInput(target.labels.join(', '))
    setMetadataGroupInput(target.group || '')
    setMetadataErrors((current) => {
      if (!current[target.target_id]) return current
      const next = { ...current }
      delete next[target.target_id]
      return next
    })
  }

  function cancelMetadataEdit(targetId: string) {
    setMetadataEditingTargetId((current) => (current === targetId ? null : current))
    setMetadataLabelInput('')
    setMetadataGroupInput('')
    setMetadataSavingTargetId((current) => (current === targetId ? null : current))
    setMetadataErrors((current) => {
      if (!current[targetId]) return current
      const next = { ...current }
      delete next[targetId]
      return next
    })
  }

  async function saveMetadataLabels(target: TargetRecord) {
    setMetadataSavingTargetId(target.target_id)
    setMetadataErrors((current) => {
      if (!current[target.target_id]) return current
      const next = { ...current }
      delete next[target.target_id]
      return next
    })

    try {
      const updated = await updateTargetMetadata(
        target.target_id,
        {
          group: metadataGroupInput.trim() || undefined,
          labels: dedupeLabels(parseLabels(metadataLabelInput)),
          note: target.note,
        },
        {
          expectedUpdatedAt: target.updated_at,
        },
      )
      setTargets((current) =>
        current.map((item) =>
          item.target_id === updated.target_id ? mergeMetadataTargetRecord(item, updated) : item,
        ),
      )
      setMetadataEditingTargetId((current) => (current === target.target_id ? null : current))
      setMetadataLabelInput('')
      setMetadataGroupInput('')
    } catch (metadataError) {
      setMetadataErrors((current) => ({
        ...current,
        [target.target_id]: describeError(metadataError, '标签更新失败'),
      }))
    } finally {
      setMetadataSavingTargetId((current) => (current === target.target_id ? null : current))
    }
  }

  const filterState: TargetFilterState = useMemo(
    () => ({
      group: searchParams.get('group'),
      type: searchParams.get('type'),
      runStatus: searchParams.get('run_status'),
      health: searchParams.get('health'),
      labels: parseMultiValue(searchParams.get('labels')),
      executionLabels: parseMultiValue(searchParams.get('execution_labels')),
      abnormal: searchParams.get('abnormal') === '1',
    }),
    [searchParams],
  )

  const groupOptions = useMemo(
    () =>
      distinctSorted(targets.map((target) => target.group).filter(Boolean)).map((value) => ({
        value,
        label: value,
      })),
    [targets],
  )

  const labelOptions = useMemo(
    () =>
      distinctSorted(targets.flatMap((target) => target.labels)).map((value) => ({
        value,
        label: value,
      })),
    [targets],
  )

  const executionLabelOptions = useMemo(
    () =>
      distinctSorted(targets.flatMap((target) => target.execution_node_labels)).map(
        (value) => ({ value, label: value }),
      ),
    [targets],
  )

  const filteredTargets = useMemo(() => {
    return targets.filter((target) => {
      if (filterState.group && target.group !== filterState.group) return false
      if (filterState.type && target.target_type !== filterState.type) return false
      if (filterState.runStatus && target.run_status !== filterState.runStatus) return false
      if (filterState.health && target.current_health_status !== filterState.health) return false
      if (filterState.labels.length > 0) {
        const hasAll = filterState.labels.every((label) => target.labels.includes(label))
        if (!hasAll) return false
      }
      if (filterState.executionLabels.length > 0) {
        const hasAll = filterState.executionLabels.every((label) =>
          target.execution_node_labels.includes(label),
        )
        if (!hasAll) return false
      }
      if (filterState.abnormal && target.current_health_status === '正常') return false
      return true
    })
  }, [targets, filterState])

  const hasActiveFilters =
    filterState.group !== null ||
    filterState.type !== null ||
    filterState.runStatus !== null ||
    filterState.health !== null ||
    filterState.labels.length > 0 ||
    filterState.executionLabels.length > 0 ||
    filterState.abnormal

  const groupFilterActive = filterState.group !== null

  async function executeBatchTargetAction(action: TargetRuntimeAction) {
    if (action === 'pause' || action === 'archive') {
      setPendingBatchAction(action)
      return
    }
    setBatchSubmitting(true)
    setBatchError(null)
    const targetIDs = filteredTargets.map((t) => t.target_id)
    let failCount = 0
    for (const targetID of targetIDs) {
      try {
        switch (action) {
          case 'enter-maintenance':
            await enterTargetMaintenance(targetID)
            break
          case 'exit-maintenance':
            await exitTargetMaintenance(targetID)
            break
          case 'resume':
            await resumeTarget(targetID)
            break
        }
      } catch {
        failCount++
      }
    }
    if (failCount > 0) {
      setBatchError(`${failCount}/${targetIDs.length} 个目标失败`)
    }
    setBatchSubmitting(false)
    setSelectAll(false)
    // Refresh the targets list
    try {
      const updated = await listTargets()
      setTargets(updated)
    } catch {
      // silent
    }
  }

  async function executeBatchTargetPauseConfirmed() {
    setPendingBatchAction(null)
    setBatchSubmitting(true)
    setBatchError(null)
    const targetIDs = filteredTargets.map((t) => t.target_id)
    let failCount = 0
    for (const targetID of targetIDs) {
      try {
        await pauseTarget(targetID)
      } catch {
        failCount++
      }
    }
    if (failCount > 0) {
      setBatchError(`${failCount}/${targetIDs.length} 个目标失败`)
    }
    setBatchSubmitting(false)
    setSelectAll(false)
    try {
      const updated = await listTargets()
      setTargets(updated)
    } catch {
      // silent
    }
  }

  function updateSearchParam(key: string, value: string | null) {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (value === null || value === '') {
          next.delete(key)
        } else {
          next.set(key, value)
        }
        return next
      },
      { replace: true },
    )
  }

  function setSingleFilter(key: 'group' | 'type' | 'run_status' | 'health', value: string | null) {
    updateSearchParam(key, value)
  }

  function setMultiFilter(key: 'labels' | 'execution_labels', values: string[]) {
    updateSearchParam(key, values.length === 0 ? null : values.join(','))
  }

  function setAbnormalFilter(checked: boolean) {
    updateSearchParam('abnormal', checked ? '1' : null)
  }

  function clearAllFilters() {
    setSearchParams(new URLSearchParams(), { replace: true })
  }

  if (loading) {
    return <section className="page-panel">正在加载目标列表…</section>
  }

  if (error) {
    return (
      <section className="page-panel">
        <h2 className="page-panel__title">目标</h2>
        <p className="page-panel__description">{error}</p>
      </section>
    )
  }

  const columns: DataTableColumn<TargetRecord>[] = [
    {
      key: 'glyph',
      label: '',
      width: 32,
      align: 'center',
      render: (target) => (
        <StatusGlyph
          state={targetGlyphState(target)}
          size="md"
          ariaLabel={`${target.name} 健康 ${target.current_health_status}`}
        />
      ),
    },
    {
      key: 'identity',
      label: '目标',
      render: (target) => (
        <div className="targets-table__identity">
          <span className="targets-table__name">{target.name}</span>
          <Hostname truncate maxChars={14} className="targets-table__id">
            {target.target_id}
          </Hostname>
          <span className="targets-table__freshness">
            成功{' '}
            <Timestamp value={target.last_success_at ?? null} mode="relative" />
            {' '}· 失败{' '}
            <Timestamp value={target.last_failure_at ?? null} mode="relative" />
          </span>
        </div>
      ),
    },
    {
      key: 'type',
      label: '类型',
      render: (target) => <span className="targets-table__type">{target.target_type}</span>,
    },
    {
      key: 'host',
      label: 'Host',
      render: (target) => {
        const hostDisplay = target.base_port ? `${target.host}:${target.base_port}` : target.host

        return (
          <span className="targets-table__host">
            {target.group ? <span className="targets-table__group">{target.group} · </span> : null}
            <Hostname>{hostDisplay}</Hostname>
          </span>
        )
      },
    },
    {
      key: 'labels',
      label: '标签',
      render: (target) => {
        if (metadataEditingTargetId === target.target_id) {
          return (
            <div
              className="targets-table__label-editor"
              onClick={(event) => event.stopPropagation()}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.stopPropagation()
                }
              }}
            >
              <label className="targets-table__label-editor-field">
                <span className="visually-hidden">Group</span>
                <input
                  name={`target-group-${target.target_id}`}
                  value={metadataGroupInput}
                  onChange={(event) => setMetadataGroupInput(event.target.value)}
                  aria-label="Group"
                  placeholder="Group"
                />
              </label>
              <label className="targets-table__label-editor-field">
                <span className="visually-hidden">标签</span>
                <input
                  name={`target-labels-${target.target_id}`}
                  value={metadataLabelInput}
                  onChange={(event) => setMetadataLabelInput(event.target.value)}
                  aria-label="标签"
                />
              </label>
              <div className="targets-table__label-editor-actions">
                <Button
                  size="sm"
                  variant="primary"
                  disabled={metadataSavingTargetId === target.target_id}
                  onClick={() => void saveMetadataLabels(target)}
                >
                  {metadataSavingTargetId === target.target_id ? '正在保存…' : '保存标签'}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={metadataSavingTargetId === target.target_id}
                  onClick={() => cancelMetadataEdit(target.target_id)}
                >
                  取消
                </Button>
              </div>
              {metadataErrors[target.target_id] ? (
                <p className="targets-table__inline-error" role="alert">
                  {metadataErrors[target.target_id]}
                </p>
              ) : null}
            </div>
          )
        }
        // Test fixture compatibility: existing tests assert on
        // "标签：alpha · beta" formatted text ("标签：" prefix + dotted list).
        // Keep the visible labels contiguous in a single text node so RTL's
        // exact-text matcher resolves it; the overflow "+N" suffix is in a
        // sibling span and never appears together with that exact assertion.
        return (
          <span className="targets-table__labels-cell">
            标签：{renderLabelsContent(target)}
          </span>
        )
      },
    },
    {
      key: 'status',
      label: '状态',
      render: (target) => (
        <span className="targets-table__status">
          <StatusBadge label={target.run_status} />
          <StatusBadge label={target.current_health_status} />
          {target.execution_node_labels.length > 0 ? (
            <span className="targets-table__exec-labels">
              {formatLabelList(target.execution_node_labels)}
            </span>
          ) : null}
        </span>
      ),
    },
    {
      key: 'trends',
      label: '近 24h',
      cellClassName: 'targets-table__trends',
      render: (target) => {
        const series = sparklines?.targets?.[target.target_id]
        if (!series || !series.latency) {
          return <span className="targets-table__trends-empty">—</span>
        }
        const vals = series.latency.filter((v): v is number => v != null)
        const latest = vals.length > 0 ? vals[vals.length - 1] : null
        const tone = !latest
          ? 'default'
          : latest > 1000
            ? 'critical'
            : latest > 200
              ? 'alert'
              : latest > 10
                ? 'notice'
                : 'accent'
        return (
          <span className="targets-table__trend-strip">
            <span className="targets-table__trend-item">
              <span className="targets-table__trend-value">
                {latest != null ? <MonoDigits>{latest.toFixed(1)} ms</MonoDigits> : '—'}
              </span>
              {vals.length > 0 ? (
                <Sparkline values={vals} tone={tone} width={64} height={14} />
              ) : (
                <span className="targets-table__trends-empty">—</span>
              )}
            </span>
          </span>
        )
      },
    },
    {
      key: 'issue',
      label: '当前主问题',
      render: (target) => (
        <div className="targets-table__issue">
          <MonoDigits className="targets-table__issue-count">
            {target.current_active_incident_count}
          </MonoDigits>
          <span className="targets-table__issue-summary">
            {target.current_primary_issue_summary || '暂无明显异常'}
          </span>
        </div>
      ),
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      cellClassName: 'targets-table__actions-cell',
      render: (target) => {
        const actions = targetRuntimeActions(target)
        return (
          <div
            className="targets-table__actions"
            onClick={(event) => event.stopPropagation()}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.stopPropagation()
              }
            }}
          >
            {metadataEditingTargetId === target.target_id ? null : (
              <Button
                size="sm"
                variant="ghost"
                disabled={metadataSavingTargetId !== null}
                onClick={() => beginMetadataEdit(target)}
              >
                快速编辑标签
              </Button>
            )}
            {actions.map(({ action, label }) => (
              <button
                key={action}
                ref={(element) => {
                  actionButtonRefs.current[actionButtonKey(target.target_id, action)] = element
                }}
                type="button"
                className="btn btn--ghost btn--sm"
                disabled={runtimeBusyTargetId === target.target_id}
                onClick={() => void handleRuntimeAction(target, action)}
              >
                {label}
              </button>
            ))}
          </div>
        )
      },
    },
  ]

  function shouldNavigateOnRowClick(target: TargetRecord): boolean {
    if (metadataEditingTargetId === target.target_id) return false
    if (pendingConfirmation?.targetId === target.target_id) return false
    return true
  }

  return (
    <section className="page-stack">
      <header className="section-heading section-heading--inline">
        <div>
          <p className="section-heading__eyebrow">目标</p>
          <h2 className="section-heading__title">目标列表</h2>
          <p className="section-heading__description">
            以 ProbeItem 视角组织目标状态，并保留执行节点标签与最近成功/失败摘要。
          </p>
        </div>
        <Button
          variant="primary"
          size="md"
          onClick={() =>
            setCreateOpen((current) => {
              if (current) {
                resetCreateFlow()
              }
              return !current
            })
          }
        >
          新建目标
        </Button>
      </header>

      {createOpen ? (
        <section className="page-panel">
          <p className="page-panel__eyebrow">目标创建</p>
          <h3 className="page-panel__title">创建目标</h3>
          <p className="page-panel__description">
            填写入口、执行节点标签与运行状态，创建后进入目标详情页继续配置 ProbeItem。
          </p>
          <form onSubmit={handleCreate}>
            <p>
              <label>
                目标名称
                <input
                  name="name"
                  value={createForm.name}
                  onChange={(event) => updateCreateField('name', event.target.value)}
                  required
                />
              </label>
            </p>
            <p>
              <label>
                目标类型
                <select
                  name="targetType"
                  value={createForm.targetType}
                  onChange={(event) =>
                    updateCreateField(
                      'targetType',
                      event.target.value as CreateTargetFormState['targetType'],
                    )
                  }
                >
                  {TARGET_TYPE_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </label>
            </p>
            <p>
              <label>
                主机地址
                <input
                  name="host"
                  value={createForm.host}
                  onChange={(event) => updateCreateField('host', event.target.value)}
                  required
                />
              </label>
            </p>
            <p>
              <label>
                基础端口
                <input
                  name="basePort"
                  inputMode="numeric"
                  value={createForm.basePort}
                  onChange={(event) => updateCreateField('basePort', event.target.value)}
                />
              </label>
            </p>
            <p>
              <label>
                执行节点标签
                <input
                  name="executionNodeLabels"
                  value={createForm.executionNodeLabels}
                  onChange={(event) =>
                    updateCreateField('executionNodeLabels', event.target.value)
                  }
                />
              </label>
            </p>
            <p>
              <label>
                运行状态
                <select
                  name="runStatus"
                  value={createForm.runStatus}
                  onChange={(event) =>
                    updateCreateField(
                      'runStatus',
                      event.target.value as CreateTargetFormState['runStatus'],
                    )
                  }
                >
                  {TARGET_RUN_STATUS_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </label>
            </p>
            <p>
              <label>
                Group
                <input
                  name="group"
                  value={createForm.group}
                  onChange={(event) => updateCreateField('group', event.target.value)}
                />
              </label>
            </p>
            <p>
              <label>
                目标标签
                <input
                  name="labels"
                  value={createForm.labels}
                  onChange={(event) => updateCreateField('labels', event.target.value)}
                />
              </label>
            </p>
            <p>
              <label>
                备注
                <textarea
                  name="note"
                  value={createForm.note}
                  onChange={(event) => updateCreateField('note', event.target.value)}
                  rows={3}
                />
              </label>
            </p>
            {createError ? <p>{createError}</p> : null}
            <div>
              <button type="submit" disabled={createSubmitting}>
                {createSubmitting ? '正在创建…' : '创建目标'}
              </button>
            </div>
          </form>
        </section>
      ) : null}

      {targets.length === 0 ? (
        <div className="empty-state">
          <h3>当前还没有目标</h3>
          <p>创建第一个目标后，可以继续为它配置 ProbeItem。</p>
          <p>
            <button type="button" onClick={() => setCreateOpen(true)}>
              创建第一个目标
            </button>
          </p>
        </div>
      ) : (
        <>
          <FilterBar
            hasActiveFilters={hasActiveFilters}
            onClearAll={clearAllFilters}
            activeChips={
              <>
                {filterState.group ? (
                  <FilterChip
                    label={`Group: ${filterState.group}`}
                    onRemove={() => setSingleFilter('group', null)}
                  />
                ) : null}
                {filterState.type ? (
                  <FilterChip
                    label={`类型: ${filterState.type}`}
                    onRemove={() => setSingleFilter('type', null)}
                  />
                ) : null}
                {filterState.runStatus ? (
                  <FilterChip
                    label={`运行状态: ${filterState.runStatus}`}
                    onRemove={() => setSingleFilter('run_status', null)}
                  />
                ) : null}
                {filterState.health ? (
                  <FilterChip
                    label={`健康状态: ${filterState.health}`}
                    onRemove={() => setSingleFilter('health', null)}
                  />
                ) : null}
                {filterState.labels.map((label) => (
                  <FilterChip
                    key={`label-${label}`}
                    label={`标签: ${label}`}
                    onRemove={() =>
                      setMultiFilter(
                        'labels',
                        filterState.labels.filter((item) => item !== label),
                      )
                    }
                  />
                ))}
                {filterState.executionLabels.map((label) => (
                  <FilterChip
                    key={`execution-${label}`}
                    label={`执行节点标签: ${label}`}
                    onRemove={() =>
                      setMultiFilter(
                        'execution_labels',
                        filterState.executionLabels.filter((item) => item !== label),
                      )
                    }
                  />
                ))}
                {filterState.abnormal ? (
                  <FilterChip
                    label="仅看异常"
                    onRemove={() => setAbnormalFilter(false)}
                  />
                ) : null}
              </>
            }
          >
            <FilterSelect
              label="Group"
              value={filterState.group}
              options={groupOptions}
              onChange={(value) => setSingleFilter('group', value)}
            />
            <FilterSelect
              label="类型"
              value={filterState.type}
              options={TARGET_TYPE_OPTIONS}
              onChange={(value) => setSingleFilter('type', value)}
            />
            <FilterSelect
              label="运行状态"
              value={filterState.runStatus}
              options={TARGET_RUN_STATUS_FILTER_OPTIONS}
              onChange={(value) => setSingleFilter('run_status', value)}
            />
            <FilterSelect
              label="健康状态"
              value={filterState.health}
              options={TARGET_HEALTH_STATUS_FILTER_OPTIONS}
              onChange={(value) => setSingleFilter('health', value)}
            />
            <FilterMultiSelect
              label="标签"
              values={filterState.labels}
              options={labelOptions}
              onChange={(values) => setMultiFilter('labels', values)}
            />
            <FilterMultiSelect
              label="执行节点标签"
              values={filterState.executionLabels}
              options={executionLabelOptions}
              onChange={(values) => setMultiFilter('execution_labels', values)}
            />
            <FilterToggle
              label="仅看异常"
              checked={filterState.abnormal}
              onChange={setAbnormalFilter}
            />
          </FilterBar>
          {groupFilterActive && filteredTargets.length > 0 ? (
            <div className="batch-bar">
              <label className="batch-bar__toggle">
                <input
                  type="checkbox"
                  checked={selectAll}
                  onChange={(e) => setSelectAll(e.target.checked)}
                />
                全选 ({filteredTargets.length})
              </label>
              {selectAll ? (
                <div className="batch-bar__actions">
                  <button
                    className="btn btn--secondary btn--sm"
                    disabled={batchSubmitting}
                    onClick={() => executeBatchTargetAction('enter-maintenance')}
                  >
                    进入维护
                  </button>
                  <button
                    className="btn btn--secondary btn--sm"
                    disabled={batchSubmitting}
                    onClick={() => executeBatchTargetAction('exit-maintenance')}
                  >
                    退出维护
                  </button>
                  <button
                    className="btn btn--secondary btn--sm"
                    disabled={batchSubmitting}
                    onClick={() => executeBatchTargetAction('pause')}
                  >
                    暂停
                  </button>
                  <button
                    className="btn btn--secondary btn--sm"
                    disabled={batchSubmitting}
                    onClick={() => executeBatchTargetAction('resume')}
                  >
                    恢复
                  </button>
                </div>
              ) : null}
              {batchError ? (
                <span className="batch-bar__error">{batchError}</span>
              ) : null}
              {batchSubmitting ? <span>批量操作中…</span> : null}
            </div>
          ) : null}
          {pendingBatchAction === 'pause' ? (
            <ActionConfirmationCard
              title="确认批量暂停目标"
              current={`将对 ${filteredTargets.length} 个目标执行暂停操作。`}
              result="操作后：所有已选目标运行状态变为暂停。"
              impact="会停止这些目标下所有 ProbeItem 的执行，不再产生新的目标观测记录。"
              unchanged="不会删除历史事件、观测记录或 ProbeItem 配置。"
              confirmLabel="确认批量暂停"
              disabled={batchSubmitting}
              onConfirm={() => void executeBatchTargetPauseConfirmed()}
              onCancel={() => setPendingBatchAction(null)}
            />
          ) : null}
          {filteredTargets.length === 0 ? (
            <div className="empty-state">
              <h3>没有匹配当前筛选的目标</h3>
              <p>请尝试调整筛选条件，或清空筛选恢复完整列表。</p>
              <p>
                <button type="button" onClick={clearAllFilters}>
                  清空筛选
                </button>
              </p>
            </div>
          ) : (
            <DataTable<TargetRecord>
              columns={columns}
              rows={filteredTargets}
              rowKey={(target) => target.target_id}
              density="compact"
              className="targets-table"
              onRowClick={(target) => {
                if (!shouldNavigateOnRowClick(target)) return
                navigate(`/targets/${target.target_id}`)
              }}
            />
          )}

          {filteredTargets.map((target) => {
            const runtimeError = runtimeErrors[target.target_id]
            const showConfirmation =
              pendingConfirmation?.targetId === target.target_id
            if (!runtimeError && !showConfirmation) return null
            return (
              <div key={`runtime-${target.target_id}`} className="targets-table__row-overlay">
                {showConfirmation ? (
                  <ActionConfirmationCard
                    title={
                      pendingConfirmation.action === 'pause' ? '确认暂停目标监控' : '确认归档目标'
                    }
                    current={
                      pendingConfirmation.action === 'pause'
                        ? pauseConfirmationCurrent()
                        : '当前：目标仍在当前工作集中。'
                    }
                    result={
                      pendingConfirmation.action === 'pause'
                        ? '操作后：目标运行状态变为暂停。'
                        : '操作后：目标退出当前工作集，运行状态变为已归档。'
                    }
                    impact={
                      pendingConfirmation.action === 'pause'
                        ? '会停止该目标下所有 ProbeItem 的执行，不再产生新的目标观测记录。'
                        : '归档后不会继续作为活跃目标参与观测、异常判定或通知。'
                    }
                    unchanged={
                      pendingConfirmation.action === 'pause'
                        ? '不会删除历史事件、观测记录或 ProbeItem 配置。'
                        : '不会删除历史事件、观测记录或 ProbeItem 配置。后续可恢复到暂停。'
                    }
                    confirmLabel={
                      pendingConfirmation.action === 'pause' ? '确认暂停目标' : '确认归档'
                    }
                    disabled={runtimeBusyTargetId === target.target_id}
                    onConfirm={() =>
                      void handleRuntimeAction(target, pendingConfirmation.action, true)
                    }
                    onCancel={() => {
                      const { action } = pendingConfirmation
                      queueFocusRestore(target.target_id, action)
                      setPendingConfirmation((current) =>
                        current?.targetId === target.target_id && current.action === action
                          ? null
                          : current,
                      )
                    }}
                  />
                ) : null}
                {runtimeError ? (
                  <p className="targets-table__inline-error" role="alert" aria-live="assertive">
                    {runtimeError}
                  </p>
                ) : null}
              </div>
            )
          })}
        </>
      )}
    </section>
  )
}

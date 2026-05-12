import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { Button, DataTable } from '../components/atoms'
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
import type { CreateTargetInput, TargetRecord, TargetSparklinesResponse } from '../lib/types'
import { CreateTargetPanel } from './targets/CreateTargetPanel'
import { TargetsBatchPanel } from './targets/TargetsBatchPanel'
import { TargetsFilterPanel } from './targets/TargetsFilterPanel'
import { TargetsRuntimeOverlays } from './targets/TargetsRuntimeOverlays'
import { TargetsSupportSurface } from './targets/TargetsSupportSurface'
import { buildTargetsTableColumns } from './targets/TargetsTableColumns'
import {
  actionButtonKey,
  buildCreateTargetInput,
  dedupeLabels,
  describeError,
  distinctSorted,
  focusRestoreActionAfterSuccess,
  initialCreateForm,
  mergeMetadataTargetRecord,
  mergeRuntimeTargetRecord,
  parseLabels,
  parseMultiValue,
} from './targets/targetHelpers'
import type {
  CreateTargetFormState,
  FocusRestoreRequest,
  PendingTargetConfirmation,
  TargetFilterState,
  TargetRuntimeAction,
} from './targets/types'

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
      .then((data) => {
        if (!cancelled) setSparklines(data)
      })
      .catch(() => {}) // silent fail
    return () => {
      cancelled = true
    }
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
  const abnormalTargetCount = targets.filter(
    (target) => target.current_health_status !== '正常',
  ).length
  const pausedTargetCount = targets.filter((target) => target.run_status === '暂停').length
  const archivedTargetCount = targets.filter((target) => target.run_status === '已归档').length
  const serviceTargetCount = targets.filter((target) => target.target_type === 'service').length

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

  const columns = buildTargetsTableColumns({
    sparklines,
    metadataEditingTargetId,
    metadataLabelInput,
    metadataGroupInput,
    metadataSavingTargetId,
    metadataErrors,
    runtimeBusyTargetId,
    actionButtonRefs,
    onMetadataGroupInputChange: setMetadataGroupInput,
    onMetadataLabelInputChange: setMetadataLabelInput,
    onSaveMetadata: (target) => void saveMetadataLabels(target),
    onCancelMetadata: cancelMetadataEdit,
    onStartMetadataEdit: beginMetadataEdit,
    onRuntimeAction: (target, action) => void handleRuntimeAction(target, action),
  })

  function shouldNavigateOnRowClick(target: TargetRecord): boolean {
    if (metadataEditingTargetId === target.target_id) return false
    if (pendingConfirmation?.targetId === target.target_id) return false
    return true
  }

  return (
    <section className="page-stack targets-page">
      <header className="page-panel page-panel--inline">
        <div>
          <p className="page-panel__eyebrow">目标</p>
          <h2 className="page-panel__title">入口观测</h2>
          <p className="page-panel__description">
            以 ProbeItem 视角组织服务入口状态，并保留执行节点标签与最近成功/失败摘要。
          </p>
        </div>
        <div className="page-panel__actions">
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
        </div>
      </header>

      {createOpen ? (
        <CreateTargetPanel
          form={createForm}
          submitting={createSubmitting}
          error={createError}
          onSubmit={handleCreate}
          onFieldChange={updateCreateField}
        />
      ) : null}

      <TargetsSupportSurface
        totalTargetCount={targets.length}
        displayedTargetCount={filteredTargets.length}
        abnormalTargetCount={abnormalTargetCount}
        pausedTargetCount={pausedTargetCount}
        archivedTargetCount={archivedTargetCount}
        executionLabelCount={executionLabelOptions.length}
        serviceTargetCount={serviceTargetCount}
        hasActiveFilters={hasActiveFilters}
        onAbnormalClick={() => setAbnormalFilter(abnormalTargetCount > 0)}
        onPausedClick={() => setSingleFilter('run_status', pausedTargetCount > 0 ? '暂停' : null)}
        onArchivedClick={() =>
          setSingleFilter('run_status', archivedTargetCount > 0 ? '已归档' : null)
        }
      />

      {targets.length === 0 ? (
        <div className="empty-state">
          <h3>当前还没有目标</h3>
          <p>创建第一个目标后，可以继续为它配置 ProbeItem。</p>
          <p>
            <button type="button" className="btn btn--primary btn--md" onClick={() => setCreateOpen(true)}>
              创建第一个目标
            </button>
          </p>
        </div>
      ) : (
        <>
          <TargetsFilterPanel
            hasActiveFilters={hasActiveFilters}
            filterState={filterState}
            groupOptions={groupOptions}
            labelOptions={labelOptions}
            executionLabelOptions={executionLabelOptions}
            onClearAll={clearAllFilters}
            onSingleFilterChange={setSingleFilter}
            onMultiFilterChange={setMultiFilter}
            onAbnormalFilterChange={setAbnormalFilter}
          />
          <TargetsBatchPanel
            show={groupFilterActive && filteredTargets.length > 0}
            filteredTargetCount={filteredTargets.length}
            selectAll={selectAll}
            batchSubmitting={batchSubmitting}
            batchError={batchError}
            pendingBatchAction={pendingBatchAction}
            onSelectAllChange={setSelectAll}
            onBatchAction={(action) => void executeBatchTargetAction(action as TargetRuntimeAction)}
            onConfirmBatchPause={() => void executeBatchTargetPauseConfirmed()}
            onCancelBatchPause={() => setPendingBatchAction(null)}
          />
          {filteredTargets.length === 0 ? (
            <div className="empty-state">
              <h3>没有匹配当前筛选的目标</h3>
              <p>请尝试调整筛选条件，或清空筛选恢复完整列表。</p>
              <p>
                <button type="button" className="btn btn--ghost btn--md" onClick={clearAllFilters}>
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

          <TargetsRuntimeOverlays
            targets={filteredTargets}
            pendingConfirmation={pendingConfirmation}
            runtimeErrors={runtimeErrors}
            runtimeBusyTargetId={runtimeBusyTargetId}
            onConfirmRuntimeAction={(target, action) => {
              void handleRuntimeAction(target, action, true)
            }}
            onCancelConfirmation={(targetId, action) => {
              queueFocusRestore(targetId, action)
              setPendingConfirmation((current) =>
                current?.targetId === targetId && current.action === action
                  ? null
                  : current,
              )
            }}
          />
        </>
      )}
    </section>
  )
}

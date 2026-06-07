import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { Button, Input, Modal, Hostname, MonoDigits, StatusGlyph, Timestamp } from '../components/atoms'
import { PageState } from '../components/PageState'
import { StatusBadge } from '../components/StatusBadge'
import {
  ApiError,
  archiveTarget,
  createTarget,
  enterTargetMaintenance,
  exitTargetMaintenance,
  listTargetAssetContexts,
  listTargetSparklines,
  listTargets,
  pauseTarget,
  restoreTargetToPaused,
  resumeTarget,
  updateTargetMetadata,
} from '../lib/api'
import type { AssetContextForTarget, CreateTargetInput, TargetRecord, TargetSparklinesResponse } from '../lib/types'
import {
  assetContextHasAttention,
  assetContextMessage,
  assetContextPrimarySummary,
  subscriptionStateLabel,
  vpsLifecycleLabel,
} from './assetContextSummary'
import { CreateTargetPanel } from './targets/CreateTargetPanel'
import { TargetsBatchPanel } from './targets/TargetsBatchPanel'
import { TargetsFilterPanel } from './targets/TargetsFilterPanel'
import { TargetsSupportSurface } from './targets/TargetsSupportSurface'
import { TargetsRuntimeOverlays } from './targets/TargetsRuntimeOverlays'
import { TargetsActionsCell } from './targets/TargetsActionsCell'
import { TargetsTrendCell } from './targets/TargetsTrendCell'
import {
  actionButtonKey,
  buildTargetEvidenceLead,
  buildCreateTargetInput,
  countAbnormalTargets,
  countArchivedTargets,
  countCoverageGapTargets,
  countPausedTargets,
  dedupeLabels,
  describeError,
  describeTargetFilterContext,
  distinctSorted,
  focusRestoreActionAfterSuccess,
  initialCreateForm,
  pickTopTargetEvidence,
  mergeMetadataTargetRecord,
  mergeRuntimeTargetRecord,
  parseLabels,
  parseMultiValue,
  isCoverageGapTarget,
  targetGlyphState,
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
  const [targetAssetContexts, setTargetAssetContexts] = useState<Map<string, AssetContextForTarget>>(new Map())
  const [targetAssetContextError, setTargetAssetContextError] = useState<string | null>(null)

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

  useEffect(() => {
    let cancelled = false
    if (typeof IntersectionObserver === 'undefined') return
    listTargetAssetContexts()
      .then((contexts) => {
        if (cancelled) return
        setTargetAssetContexts(new Map(contexts.map((context) => [context.target_id, context])))
        setTargetAssetContextError(null)
      })
      .catch((value: unknown) => {
        if (cancelled) return
        setTargetAssetContexts(new Map())
        setTargetAssetContextError(describeError(value, '加载 Target 资产上下文失败'))
      })
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

  function openCreateDrawer() {
    setCreateOpen(true)
  }

  function closeCreateDrawer() {
    resetCreateFlow()
    setCreateOpen(false)
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
      coverageGap: searchParams.get('coverage_gap') === '1',
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
          target.execution_monitoring_instance_labels.includes(label),
        )
        if (!hasAll) return false
      }
      if (filterState.abnormal && target.current_health_status === '正常') return false
      if (filterState.coverageGap && !isCoverageGapTarget(target)) return false
      return true
    })
  }, [targets, filterState])

  const groupFilterActive = filterState.group !== null
  const abnormalTargetCount = useMemo(() => countAbnormalTargets(targets), [targets])
  const pausedTargetCount = useMemo(() => countPausedTargets(targets), [targets])
  const archivedTargetCount = useMemo(() => countArchivedTargets(targets), [targets])
  const coverageGapTargetCount = useMemo(() => countCoverageGapTargets(targets), [targets])
  const serviceTargetCount = useMemo(() => targets.filter((target) => target.target_type === 'service').length, [targets])
  const executionLabelCount = useMemo(
    () => distinctSorted(targets.flatMap((target) => target.execution_monitoring_instance_labels)).length,
    [targets],
  )
  const hasActiveFilters =
    filterState.group !== null ||
    filterState.type !== null ||
    filterState.runStatus !== null ||
    filterState.health !== null ||
    filterState.labels.length > 0 ||
    filterState.executionLabels.length > 0 ||
    filterState.abnormal ||
    filterState.coverageGap
  const targetFilterContext = describeTargetFilterContext(filterState)
  const targetEvidenceLead = buildTargetEvidenceLead({
    totalTargetCount: targets.length,
    displayedTargetCount: filteredTargets.length,
    abnormalTargetCount,
    pausedTargetCount,
    archivedTargetCount,
    coverageGapTargetCount,
    hasActiveFilters,
  })
  const topTargetEvidence = pickTopTargetEvidence(filteredTargets)

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

  function setBooleanFilter(key: 'abnormal' | 'coverage_gap', enabled: boolean) {
    updateSearchParam(key, enabled ? '1' : null)
  }

  function clearAllFilters() {
    setSearchParams(new URLSearchParams(), { replace: true })
  }

  if (loading) {
    return <PageState kind="loading" title="正在加载目标列表…" />
  }

  if (error) {
    return (
      <PageState
        kind="error"
        eyebrow="目标"
        title="目标列表不可用"
        description={error}
        technicalSummary={error}
      />
    )
  }

  function shouldNavigateOnRowClick(target: TargetRecord): boolean {
    if (metadataEditingTargetId === target.target_id) return false
    if (pendingConfirmation?.targetId === target.target_id) return false
    return true
  }

  const editingTarget = metadataEditingTargetId
    ? targets.find((target) => target.target_id === metadataEditingTargetId) ?? null
    : null

  return (
    <div className="page-stack animate-in">
      <div className="page-header animate-in">
        <div>
          <h1 className="page-title">入口探测</h1>
          <p className="page-sub">监控入口健康与延迟</p>
        </div>
        <div className="header-actions">
          <button
            type="button"
            className="btn md primary"
            onClick={createOpen ? closeCreateDrawer : openCreateDrawer}
          >
            新建目标
          </button>
        </div>
      </div>

      <div className="hero-stats animate-in">
        <div className="hero-stat">
          <div className="hs-label">全部目标</div>
          <div className="hs-value"><MonoDigits>{targets.length}</MonoDigits></div>
        </div>
        <div className="hero-stat">
          <div className="hs-label">异常</div>
          <div className={`hs-value${abnormalTargetCount > 0 ? ' danger' : ''}`}>
            <MonoDigits>{abnormalTargetCount}</MonoDigits>
          </div>
        </div>
        <div className="hero-stat">
          <div className="hs-label">启用</div>
          <div className="hs-value">
            <MonoDigits>{targets.filter((t) => t.run_status === '启用').length}</MonoDigits>
          </div>
        </div>
        <div className="hero-stat">
          <div className="hs-label">暂停/归档</div>
          <div className={`hs-value${(pausedTargetCount + archivedTargetCount) > 0 ? ' muted' : ''}`}>
            <MonoDigits>{pausedTargetCount + archivedTargetCount}</MonoDigits>
          </div>
        </div>
      </div>

      <TargetsSupportSurface
        totalTargetCount={targets.length}
        displayedTargetCount={filteredTargets.length}
        abnormalTargetCount={abnormalTargetCount}
        pausedTargetCount={pausedTargetCount}
        archivedTargetCount={archivedTargetCount}
        coverageGapTargetCount={coverageGapTargetCount}
        executionLabelCount={executionLabelCount}
        serviceTargetCount={serviceTargetCount}
        evidenceLead={targetEvidenceLead}
        topEvidence={topTargetEvidence}
        filterContext={targetFilterContext}
        hasActiveFilters={hasActiveFilters}
        onAbnormalClick={() => setSingleFilter('health', abnormalTargetCount > 0 ? '严重' : null)}
        onPausedClick={() => setSingleFilter('run_status', pausedTargetCount > 0 ? '暂停' : null)}
        onArchivedClick={() => setSingleFilter('run_status', archivedTargetCount > 0 ? '已归档' : null)}
        onCoverageClick={() => setBooleanFilter('coverage_gap', coverageGapTargetCount > 0)}
        onClearFilters={clearAllFilters}
        onCreateClick={() => openCreateDrawer()}
      />

      <Modal
        open={createOpen}
        onClose={closeCreateDrawer}
        title="创建目标"
        ariaLabel="创建目标"
        persistent
      >
        <CreateTargetPanel
          form={createForm}
          submitting={createSubmitting}
          error={createError}
          onCancel={closeCreateDrawer}
          onSubmit={handleCreate}
          onFieldChange={updateCreateField}
        />
      </Modal>

      <Modal
        open={editingTarget !== null}
        onClose={() => {
          if (editingTarget) cancelMetadataEdit(editingTarget.target_id)
        }}
        title={editingTarget ? `${editingTarget.name} · 快速编辑标签` : '快速编辑标签'}
        size="md"
      >
        {editingTarget ? (
          <div className="page-stack">
            <p className="page-panel__description">
              更新列表扫描使用的 group 与标签，不会修改备注或运行状态。
            </p>
            <Input
              label="Group"
              name={`target-group-${editingTarget.target_id}`}
              value={metadataGroupInput}
              onChange={(event) => setMetadataGroupInput(event.target.value)}
              placeholder="Group"
            />
            <Input
              label="标签"
              name={`target-labels-${editingTarget.target_id}`}
              value={metadataLabelInput}
              onChange={(event) => setMetadataLabelInput(event.target.value)}
              hint="用逗号分隔多个标签。"
            />
            {metadataErrors[editingTarget.target_id] ? (
              <p className="targets-table__inline-error" role="alert">
                {metadataErrors[editingTarget.target_id]}
              </p>
            ) : null}
            <div className="action-confirm__actions">
              <Button
                variant="secondary"
                disabled={metadataSavingTargetId === editingTarget.target_id}
                onClick={() => cancelMetadataEdit(editingTarget.target_id)}
              >
                取消
              </Button>
              <Button
                variant="primary"
                disabled={metadataSavingTargetId === editingTarget.target_id}
                onClick={() => void saveMetadataLabels(editingTarget)}
              >
                {metadataSavingTargetId === editingTarget.target_id ? '正在保存…' : '保存标签'}
              </Button>
            </div>
          </div>
        ) : null}
      </Modal>

      <div className="animate-in d2">
        {targets.length === 0 ? (
          <PageState
            kind="empty"
            surface="empty"
            title="候风尚未配置任何观测目标"
            description="创建第一个目标后，可以继续为它配置 ProbeItem。"
            action={
              <button type="button" className="btn md primary" onClick={() => openCreateDrawer()}>
                新建第一个目标
              </button>
            }
          />
        ) : (
          <>
            <div className="filter-panel animate-in d1">
              <TargetsFilterPanel
                filterState={filterState}
                groupOptions={groupOptions}
                onSingleFilterChange={setSingleFilter}
              />
            </div>
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
            {targetAssetContextError ? (
              <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
                {targetAssetContextError}
              </p>
            ) : null}
            {filteredTargets.length === 0 ? (
              <PageState
                kind="empty"
                surface="empty"
                title="没有匹配当前筛选的目标"
                description="请尝试调整筛选条件，或清空筛选恢复完整列表。"
                action={
                  <button type="button" className="btn sm secondary" onClick={clearAllFilters}>
                    清空筛选
                  </button>
                }
              />
            ) : (
              <table className="table targets-table animate-in d2">
                <thead>
                  <tr>
                    <th></th>
                    <th>目标</th>
                    <th>类型</th>
                    <th>Host</th>
                    <th>状态</th>
                    <th>资产上下文</th>
                    <th>近 24h 延迟</th>
                    <th>当前主问题</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredTargets.map((target) => {
                    const hostDisplay = target.base_port
                      ? `${target.host}:${target.base_port}`
                      : target.host
                    const assetContext = targetAssetContexts.get(target.target_id)
                    const primaryContext = assetContextPrimarySummary(assetContext)
                    return (
                      <tr
                        key={target.target_id}
                        tabIndex={0}
                        onClick={(e) => {
                          if (
                            e.target instanceof Element &&
                            e.target.closest('a[href],button,input,select,textarea,[role="button"],[role="link"]')
                          ) return
                          if (!shouldNavigateOnRowClick(target)) return
                          navigate(`/targets/${target.target_id}`)
                        }}
                        onKeyDown={(e) => {
                          if (
                            e.target instanceof Element &&
                            e.target.closest('a[href],button,input,select,textarea,[role="button"],[role="link"]')
                          ) return
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault()
                            if (shouldNavigateOnRowClick(target)) {
                              navigate(`/targets/${target.target_id}`)
                            }
                          }
                        }}
                      >
                        <td>
                          <StatusGlyph
                            state={targetGlyphState(target)}
                            size="md"
                            ariaLabel={`${target.name} 健康 ${target.current_health_status}`}
                          />
                        </td>
                        <td>
                          <div className="name">{target.name}</div>
                          <div className="sub">
                            成功 <Timestamp value={target.last_success_at ?? null} mode="relative" />
                            {' '}· 失败 <Timestamp value={target.last_failure_at ?? null} mode="relative" />
                          </div>
                        </td>
                        <td><span className="probe-kind">{target.target_type}</span></td>
                        <td className="mono">
                          {target.group ? <span className="targets-table__group">{target.group} · </span> : null}
                          <Hostname>{hostDisplay}</Hostname>
                        </td>
                        <td>
                          <span className="targets-table__status">
                            <StatusBadge label={target.run_status} />
                            <StatusBadge label={target.current_health_status} />
                          </span>
                          {target.execution_monitoring_instance_labels.length > 0 && (
                            <div className="sub">执行: {target.execution_monitoring_instance_labels.join(', ')}</div>
                          )}
                        </td>
                        <td>
                          {primaryContext ? (
                            <div className="asset-context-cell">
                              <span className={assetContextHasAttention(assetContext) ? 'asset-context-pill asset-context-pill--attention' : 'asset-context-pill'}>
                                {assetContextMessage(assetContext)}
                              </span>
                              <small>
                                {vpsLifecycleLabel(primaryContext.lifecycle_status)} · {subscriptionStateLabel(primaryContext.subscription_state)}
                              </small>
                            </div>
                          ) : (
                            <span className="asset-context-pill">未关联 VPS</span>
                          )}
                        </td>
                        <td className="targets-table__trends">
                          <TargetsTrendCell target={target} sparklines={sparklines} />
                        </td>
                        <td>
                          <div className="targets-table__issue">
                            <MonoDigits className="targets-table__issue-count">
                              {target.current_active_incident_count}
                            </MonoDigits>
                            <span className="targets-table__issue-summary">
                              {target.current_primary_issue_summary || '暂无明显异常'}
                            </span>
                          </div>
                        </td>
                        <td className="targets-table__actions-cell">
                          <TargetsActionsCell
                            target={target}
                            metadataEditingTargetId={metadataEditingTargetId}
                            metadataSavingTargetId={metadataSavingTargetId}
                            runtimeBusyTargetId={runtimeBusyTargetId}
                            actionButtonRefs={actionButtonRefs}
                            onStartMetadataEdit={beginMetadataEdit}
                            onRuntimeAction={(t, action) => void handleRuntimeAction(t, action)}
                          />
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
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
      </div>
    </div>
  )
}

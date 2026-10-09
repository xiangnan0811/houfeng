import { Fragment, type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { Badge, ColumnResizeHandle, Modal, Hostname, MonoDigits, Tabs, Timestamp, isInteractiveRowTarget } from '../components/atoms'
import { PageState } from '../components/PageState'
import { useColumnWidths } from '../lib/useColumnWidths'
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
  getTargetLifecycleReview,
  restoreTargetToPaused,
  resumeTarget,
} from '../lib/api'
import { requiresSharedImpactConfirmation } from '../lib/assetLifecycle'
import { useVisibleRefresh, type VisibleRefreshContext } from '../lib/useVisibleRefresh'
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
import { TargetsTrendCell } from './targets/TargetsTrendCell'
import {
  buildCreateTargetInput,
  countAbnormalTargets,
  countArchivedTargets,
  countCoverageGapTargets,
  countPausedTargets,
  countStaleTargets,
  countUnobservedTargets,
  combineCurrentAndRetiredTargets,
  describeError,
  distinctSorted,
  initialCreateForm,
  parseMultiValue,
  isAbnormalTarget,
  isCoverageGapTarget,
  isStaleTarget,
  isTargetListInvalidatingError,
  isUnobservedTarget,
  targetMatchesGroup,
  targetAttentionBadges,
  targetCoverageNotices,
  targetKnownHealthNote,
  targetCoverageSummary,
  targetTypePresentation,
} from './targets/targetHelpers'
import type {
  CreateTargetFormState,
  TargetFilterState,
  TargetRuntimeAction,
} from './targets/types'

const TARGET_LIST_COLUMN_WIDTHS = [40, 180, 72, 168, 210, 120, 140]
const TARGET_LIST_HEADERS = ['', '目标', '类型', 'Host', '健康', '资产上下文', '近 24h 延迟'] as const
const TAB_OWNED_RUN_STATUS = new Set(['暂停'])

type TargetQuickView = 'all' | 'abnormal' | 'unobserved' | 'stale' | 'paused' | 'archived' | 'coverage'

function TargetTypeCell({ value }: { value: string }) {
  const presented = targetTypePresentation(value)
  return (
    <span className="probe-kind">
      {presented.label}
      {presented.raw ? (
        <details>
          <summary>诊断信息</summary>
          <span>{presented.raw}</span>
        </details>
      ) : null}
    </span>
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
  const [sparklines, setSparklines] = useState<TargetSparklinesResponse | null>(null)
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [frozenBatchIds, setFrozenBatchIds] = useState<string[] | null>(null)
  const [batchSubmitting, setBatchSubmitting] = useState(false)
  const [pendingBatchAction, setPendingBatchAction] = useState<string | null>(null)
  const [batchError, setBatchError] = useState<string | null>(null)
  const [targetAssetContexts, setTargetAssetContexts] = useState<Map<string, AssetContextForTarget>>(new Map())
  const [targetAssetContextError, setTargetAssetContextError] = useState<string | null>(null)
  const retiredRef = useRef<TargetRecord[] | null>(null)
  const pendingCurrentRef = useRef<TargetRecord[] | null>(null)
  const initialDoneRef = useRef(false)
  const mutationRef = useRef(false)
  const pairedReadRef = useRef(false)
  const retiredSnapshotStaleRef = useRef(false)
  const snapshotAtRef = useRef<string | null>(null)
  const [refreshFailureAt, setRefreshFailureAt] = useState<string | null>(null)
  const [retiredSnapshotStale, setRetiredSnapshotStale] = useState(false)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const publishList = useCallback((currentTargets: TargetRecord[], retiredTargets: TargetRecord[]) => {
    const at = new Date().toISOString()
    snapshotAtRef.current = at
    pendingCurrentRef.current = currentTargets
    retiredRef.current = retiredTargets
    retiredSnapshotStaleRef.current = false
    initialDoneRef.current = true
    setRetiredSnapshotStale(false)
    setRefreshFailureAt(null)
    setError(null)
    setTargets(combineCurrentAndRetiredTargets(currentTargets, retiredTargets))
    setLoading(false)
  }, [])

  const failInitialLoad = useCallback((value: unknown) => {
    if (initialDoneRef.current) return
    initialDoneRef.current = true
    setTargets([])
    setRefreshFailureAt(null)
    setError(value instanceof ApiError ? value.message : '加载目标列表失败')
    setLoading(false)
  }, [])

  const invalidateDisplayedList = useCallback((value: unknown) => {
    retiredSnapshotStaleRef.current = false
    setRetiredSnapshotStale(false)
    pendingCurrentRef.current = []
    retiredRef.current = []
    snapshotAtRef.current = null
    setRefreshFailureAt(null)
    setTargets([])
    setError(value instanceof ApiError ? value.message : '加载目标列表失败')
    setLoading(false)
  }, [])

  const refreshCurrent = useCallback(async (context: VisibleRefreshContext) => {
    if (pairedReadRef.current) {
      const [currentResult, retiredResult] = await Promise.allSettled([
        listTargets('current'),
        listTargets('retired'),
      ])
      if (!context.isCurrent() || mutationRef.current || !mountedRef.current) return
      pairedReadRef.current = false
      if (currentResult.status === 'fulfilled' && retiredResult.status === 'fulfilled') {
        publishList(currentResult.value, retiredResult.value)
        return
      }
      const failures = [currentResult, retiredResult].flatMap((result) =>
        result.status === 'rejected' ? [result.reason] : [],
      )
      const invalidating = failures.find((failure) => isTargetListInvalidatingError(failure))
      if (invalidating) {
        invalidateDisplayedList(invalidating)
        return
      }
      retiredSnapshotStaleRef.current = true
      setRetiredSnapshotStale(true)
      setRefreshFailureAt(snapshotAtRef.current)
      return
    }

    try {
      const currentTargets = await listTargets('current')
      if (!context.isCurrent() || mutationRef.current || !mountedRef.current) return
      if (retiredSnapshotStaleRef.current) {
        setRefreshFailureAt(snapshotAtRef.current)
        return
      }
      pendingCurrentRef.current = currentTargets
      if (retiredRef.current == null) return
      publishList(currentTargets, retiredRef.current)
    } catch (value: unknown) {
      if (!context.isCurrent() || mutationRef.current || !mountedRef.current) return
      if (!initialDoneRef.current) {
        failInitialLoad(value)
        return
      }
      if (isTargetListInvalidatingError(value)) {
        invalidateDisplayedList(value)
        return
      }
      setRefreshFailureAt(snapshotAtRef.current)
    }
  }, [failInitialLoad, invalidateDisplayedList, publishList])

  const { refresh, invalidate } = useVisibleRefresh(refreshCurrent, {
    enabled: !batchSubmitting,
  })

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    let cancelled = false
    listTargets('retired')
      .then((retiredTargets) => {
        if (cancelled || !mountedRef.current) return
        retiredRef.current = retiredTargets
        if (mutationRef.current || initialDoneRef.current || pendingCurrentRef.current == null) return
        publishList(pendingCurrentRef.current, retiredTargets)
      })
      .catch((value: unknown) => {
        if (cancelled || !mountedRef.current || mutationRef.current) return
        failInitialLoad(value)
      })
    return () => {
      cancelled = true
    }
  }, [failInitialLoad, publishList])

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

  const { widths: targetColumnWidths, startResize: startTargetColumnResize } = useColumnWidths(
    'targets-list',
    TARGET_LIST_COLUMN_WIDTHS,
  )

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
      invalidate()
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

  const filterState: TargetFilterState = useMemo(
    () => ({
      group: searchParams.get('group'),
      type: searchParams.get('type'),
      runStatus: searchParams.get('run_status'),
      lifecycle: searchParams.get('lifecycle_status'),
      health: searchParams.get('health'),
      labels: parseMultiValue(searchParams.get('labels')),
      executionLabels: parseMultiValue(searchParams.get('execution_labels')),
      abnormal: searchParams.get('abnormal') === '1',
      unobserved: searchParams.get('view') === 'unobserved',
      stale: searchParams.get('view') === 'stale',
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
      if (filterState.lifecycle && target.lifecycle_status !== filterState.lifecycle) return false
      if (target.lifecycle_status === 'retired' && (filterState.runStatus || filterState.abnormal || filterState.unobserved || filterState.stale || filterState.coverageGap)) return false
      if (filterState.group && !targetMatchesGroup(target, filterState.group)) return false
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
      if (filterState.abnormal && !isAbnormalTarget(target)) return false
      if (filterState.unobserved && !isUnobservedTarget(target)) return false
      if (filterState.stale && !isStaleTarget(target)) return false
      if (filterState.coverageGap && !isCoverageGapTarget(target)) return false
      return true
    })
  }, [targets, filterState])

  const abnormalTargetCount = useMemo(() => countAbnormalTargets(targets), [targets])
  const unobservedTargetCount = useMemo(() => countUnobservedTargets(targets), [targets])
  const staleTargetCount = useMemo(() => countStaleTargets(targets), [targets])
  const pausedTargetCount = useMemo(() => countPausedTargets(targets), [targets])
  const archivedTargetCount = useMemo(() => countArchivedTargets(targets), [targets])
  const coverageGapTargetCount = useMemo(() => countCoverageGapTargets(targets), [targets])
  const visibleIds = useMemo(() => filteredTargets.filter((target) => target.lifecycle_status !== 'retired').map((target) => target.target_id), [filteredTargets])
  const [prevVisibleIds, setPrevVisibleIds] = useState(visibleIds)
  if (visibleIds !== prevVisibleIds) {
    setPrevVisibleIds(visibleIds)
    if (selectedIds.some((id) => !visibleIds.includes(id))) {
      setSelectedIds(selectedIds.filter((id) => visibleIds.includes(id)))
    }
  }
  const batchTargetIds = frozenBatchIds ?? selectedIds.filter((id) => visibleIds.includes(id))
  const selectedVisibleCount = visibleIds.filter((id) => selectedIds.includes(id)).length
  const allVisibleSelected = visibleIds.length > 0 && selectedVisibleCount === visibleIds.length
  const someVisibleSelected = selectedVisibleCount > 0 && !allVisibleSelected
  const hasActiveFilters = Boolean(
    filterState.type || filterState.health || filterState.runStatus || filterState.lifecycle || filterState.group,
  )
  const navigationLocked = pendingBatchAction !== null || batchSubmitting
  const quickView: TargetQuickView = filterState.unobserved
    ? 'unobserved'
    : filterState.stale
      ? 'stale'
      : filterState.coverageGap
        ? 'coverage'
        : filterState.abnormal
          ? 'abnormal'
          : filterState.runStatus === '暂停'
            ? 'paused'
            : filterState.lifecycle === 'retired'
              ? 'archived'
              : 'all'

  async function runBatchOnIds(action: TargetRuntimeAction, ids: string[]) {
    invalidate()
    mutationRef.current = true
    setBatchSubmitting(true)
    setBatchError(null)
    setPendingBatchAction(null)
    let failCount = 0
    for (const targetID of ids) {
      try {
        const selectedTarget = targets.find((target) => target.target_id === targetID)
        if (selectedTarget?.lifecycle_status === 'retired' && action !== 'restore-to-paused') {
          throw new Error('已退役目标不能执行运行控制')
        }
        const review = await getTargetLifecycleReview(targetID)
        if (review && requiresSharedImpactConfirmation(review.dependency_impacts ?? [], 'target', targetID)) {
          throw new Error('目标影响多台 VPS，请在目标详情确认共享影响')
        }
        const confirmation = undefined
        if (action === 'enter-maintenance') await enterTargetMaintenance(targetID)
        else if (action === 'exit-maintenance') await exitTargetMaintenance(targetID)
        else if (action === 'pause') await pauseTarget(targetID, confirmation)
        else if (action === 'resume') await resumeTarget(targetID)
        else if (action === 'archive') await archiveTarget(targetID, confirmation)
        else await restoreTargetToPaused(targetID, confirmation)
      } catch {
        failCount++
      }
    }
    if (failCount > 0) setBatchError(`${failCount}/${ids.length} 个目标失败`)
    setFrozenBatchIds(null)
    setSelectedIds([])
    pairedReadRef.current = true
    invalidate()
    mutationRef.current = false
    setBatchSubmitting(false)
    try {
      await refresh()
    } catch {
      /* refreshCurrent keeps the previous rows when either paired read fails */
    }
  }

  async function retryPairedListSnapshot() {
    if (mutationRef.current || !retiredSnapshotStaleRef.current) return
    pairedReadRef.current = true
    invalidate()
    try {
      await refresh()
    } catch {
      /* refreshCurrent keeps the previous rows when either paired read fails */
    }
  }

  async function executeBatchTargetAction(action: TargetRuntimeAction) {
    const ids = batchTargetIds
    if (ids.length === 0) return
    if (action === 'pause' || action === 'archive') {
      setFrozenBatchIds(ids)
      setPendingBatchAction(action)
      return
    }
    await runBatchOnIds(action, ids)
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

  function setQuickView(view: TargetQuickView) {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        const tabOwnedRunStatus = TAB_OWNED_RUN_STATUS.has(next.get('run_status') ?? '')
        next.delete('lifecycle_status')
        next.delete('view')
        if (view === 'all') {
          next.delete('abnormal')
          next.delete('coverage_gap')
          if (tabOwnedRunStatus) next.delete('run_status')
        } else if (view === 'abnormal') {
          next.set('abnormal', '1')
          next.delete('coverage_gap')
          if (tabOwnedRunStatus) next.delete('run_status')
        } else if (view === 'unobserved') {
          next.set('view', 'unobserved')
          next.delete('abnormal')
          next.delete('coverage_gap')
          if (tabOwnedRunStatus) next.delete('run_status')
        } else if (view === 'stale') {
          next.set('view', 'stale')
          next.delete('abnormal')
          next.delete('coverage_gap')
          if (tabOwnedRunStatus) next.delete('run_status')
        } else if (view === 'paused') {
          next.set('run_status', '暂停')
          next.delete('abnormal')
          next.delete('coverage_gap')
        } else if (view === 'archived') {
          next.delete('run_status')
          next.set('lifecycle_status', 'retired')
          next.delete('abnormal')
          next.delete('coverage_gap')
        } else {
          next.set('coverage_gap', '1')
          next.delete('abnormal')
          if (tabOwnedRunStatus) next.delete('run_status')
        }
        return next
      },
      { replace: true },
    )
  }

  function clearAllFilters() {
    setSearchParams(new URLSearchParams(), { replace: true })
  }

  function toggleSelected(targetId: string) {
    setSelectedIds((current) =>
      current.includes(targetId) ? current.filter((id) => id !== targetId) : [...current, targetId],
    )
  }

  function toggleSelectAll(checked: boolean) {
    setSelectedIds(checked ? [...visibleIds] : [])
  }

  function shouldNavigateOnRowClick() {
    return !navigationLocked
  }

  if (loading) {
    return <PageState kind="loading" title="正在加载目标列表…" />
  }

  if (error) {
    return (
      <PageState
        kind="error"
        title="目标列表不可用"
        description={error}
        technicalSummary={error}
      />
    )
  }

  return (
    <div className="page targets-page">
      <header className="page__head">
        <h1 className="page__title">入口探测</h1>
        <div className="page__actions">
          <button
            type="button"
            className="btn md primary"
            onClick={createOpen ? closeCreateDrawer : openCreateDrawer}
          >
            新建目标
          </button>
        </div>
      </header>

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

      {targets.length === 0 ? (
        <PageState
          kind="empty"
          surface="empty"
          title="候风尚未配置任何观测目标"
          description="创建第一个目标后，可以继续为它配置探测项。"
          action={
            <button type="button" className="btn md primary" onClick={() => openCreateDrawer()}>
              新建第一个目标
            </button>
          }
        />
      ) : (
        <>
          {refreshFailureAt ? (
            <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
              更新失败，显示上次结果 <Timestamp value={refreshFailureAt} mode="both" />
              {retiredSnapshotStale ? (
                <button type="button" className="btn sm ghost" onClick={() => void retryPairedListSnapshot()}>
                  重试
                </button>
              ) : null}
            </p>
          ) : null}
          <div className="monitoring-page__tools">
            <div className="monitoring-page__views">
              <Tabs
                label="关注视图"
                idBase="target-quick-view"
                activation="manual"
                value={quickView}
                onChange={(view) => {
                  if (!navigationLocked) setQuickView(view)
                }}
                items={[
                  { value: 'all', label: '全部', count: targets.length },
                  { value: 'abnormal', label: '异常', count: abnormalTargetCount },
                  { value: 'unobserved', label: '尚无观测', count: unobservedTargetCount },
                  { value: 'stale', label: '观测过期', count: staleTargetCount },
                  { value: 'paused', label: '暂停', count: pausedTargetCount },
                  { value: 'archived', label: '退役', count: archivedTargetCount },
                  { value: 'coverage', label: '覆盖缺口', count: coverageGapTargetCount },
                ]}
              />
            </div>
            <div className="monitoring-page__controls">
              <TargetsFilterPanel
                filterState={filterState}
                groupOptions={groupOptions}
                hasActiveFilters={hasActiveFilters}
                onClearAll={clearAllFilters}
                onSingleFilterChange={setSingleFilter}
                batch={(
                  <TargetsBatchPanel
                    selectedCount={batchTargetIds.length}
                    batchSubmitting={batchSubmitting}
                    batchError={batchError}
                    pendingBatchAction={pendingBatchAction}
                    onBatchAction={(action) => void executeBatchTargetAction(action as TargetRuntimeAction)}
                    onConfirmBatchPause={() => void runBatchOnIds('pause', batchTargetIds)}
                    onConfirmBatchArchive={() => void runBatchOnIds('archive', batchTargetIds)}
                    onCancelBatchConfirm={() => {
                      setPendingBatchAction(null)
                      setFrozenBatchIds(null)
                    }}
                  />
                )}
              />
            </div>
          </div>
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
            <div className="page__work" role="region" aria-label="入口清单" tabIndex={0}>
              <table className="table table--resizable targets-table">
                <colgroup>
                  {targetColumnWidths.map((width, index) => (
                    <col key={index} width={width} />
                  ))}
                </colgroup>
                <thead>
                  <tr>
                    {TARGET_LIST_HEADERS.map((label, index) => (
                      <th key={`${label}-${index}`} scope="col">
                        {index === 0 ? (
                          <input
                            type="checkbox"
                            className="monitoring-table__select-check"
                            checked={allVisibleSelected}
                            disabled={navigationLocked}
                            ref={(node) => {
                              if (node) node.indeterminate = someVisibleSelected
                            }}
                            onChange={(event) => toggleSelectAll(event.target.checked)}
                            aria-label="全选可见目标"
                          />
                        ) : (
                          label
                        )}
                        {index > 0 ? (
                          <ColumnResizeHandle onDragStart={(clientX) => startTargetColumnResize(index, clientX)} />
                        ) : null}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {filteredTargets.map((target) => {
                    const hostDisplay = target.base_port
                      ? `${target.host}:${target.base_port}`
                      : target.host
                    const assetContext = targetAssetContexts.get(target.target_id)
                    const primaryContext = assetContextPrimarySummary(assetContext)
                    const badges = targetAttentionBadges(target)
                    const coverageNotes = targetCoverageNotices(target)
                    const issue = target.lifecycle_status === 'retired' ? '' : target.current_primary_issue_summary.trim()
                    const healthNote = targetKnownHealthNote(target)
                    return (
                      <Fragment key={target.target_id}>
                        {/* a11y-allow-nonsemantic-click: keyboard-complete-row */}
                        <tr
                          tabIndex={0}
                          onClick={(e) => {
                            if (isInteractiveRowTarget(e.target)) return
                            if (!shouldNavigateOnRowClick()) return
                            navigate(`/targets/${target.target_id}`)
                          }}
                          onKeyDown={(e) => {
                            if (isInteractiveRowTarget(e.target)) return
                            if (e.key === 'Enter' || e.key === ' ') {
                              e.preventDefault()
                              if (shouldNavigateOnRowClick()) {
                                navigate(`/targets/${target.target_id}`)
                              }
                            }
                          }}
                        >
                        <td className="monitoring-table__select">
                          <input
                            type="checkbox"
                            className="monitoring-table__select-check"
                            checked={selectedIds.includes(target.target_id)}
                            disabled={navigationLocked || target.lifecycle_status === 'retired'}
                            onChange={() => toggleSelected(target.target_id)}
                            onClick={(event) => event.stopPropagation()}
                            aria-label={`选择 ${target.name}`}
                          />
                        </td>
                        <td>
                          <div className="targets-table__identity">
                            <div className="targets-table__identity-head">
                              <div className="name">{target.name}</div>
                            </div>
                            <div className="sub">
                              成功 <Timestamp value={target.last_success_at ?? null} mode="relative" />
                              {' '}· 失败 <Timestamp value={target.last_failure_at ?? null} mode="relative" />
                            </div>
                          </div>
                        </td>
                        <td><TargetTypeCell value={target.target_type} /></td>
                        <td className="mono">
                          {target.group ? <span className="targets-table__group">{target.group} · </span> : null}
                          <Hostname>{hostDisplay}</Hostname>
                        </td>
                        <td>
                          <div className="targets-table__health">
                            {badges.length > 0 ? (
                              <span className="targets-table__health-head">
                                {badges.map((badge) => (
                                  <Badge key={badge.label} variant="state" tone={badge.tone}>
                                    {badge.label}
                                  </Badge>
                                ))}
                                {target.current_active_incident_count > 0 ? (
                                  <MonoDigits className="targets-table__issue-count">
                                    {target.current_active_incident_count}
                                  </MonoDigits>
                                ) : null}
                              </span>
                            ) : (
                              <span className="targets-table__health-quiet">—</span>
                            )}
                            <span className="targets-table__issue-summary">{targetCoverageSummary(target)}</span>
                            {coverageNotes.map((notice) => (
                              <span key={notice.key} className="targets-table__issue-summary">{notice.title}</span>
                            ))}
                            {issue ? (
                              <span className="targets-table__issue-summary" title={issue}>{issue}</span>
                            ) : null}
                            {healthNote ? (
                              <span className="targets-table__issue-summary">{healthNote}</span>
                            ) : null}
                          </div>
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
                        </tr>
                      </Fragment>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  )
}

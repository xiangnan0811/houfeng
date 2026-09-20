import { Fragment, type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { Badge, ColumnResizeHandle, Modal, Hostname, MonoDigits, StatusGlyph, Tabs, Timestamp, isInteractiveRowTarget } from '../components/atoms'
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
  restoreTargetToPaused,
  resumeTarget,
} from '../lib/api'
import type { AssetContextForTarget, CreateTargetInput, TargetRecord, TargetSparklinesResponse } from '../lib/types'
import {
  assetContextHasAttention,
  assetContextMessage,
  assetContextPrimarySummary,
  subscriptionStateLabel,
  vpsLifecycleLabel,
} from './assetContextSummary'
import './target-detail/TargetDetailWorkspace.css'
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
  describeError,
  distinctSorted,
  initialCreateForm,
  parseMultiValue,
  isCoverageGapTarget,
  targetAttentionBadges,
  targetGlyphState,
  targetIssueSummary,
} from './targets/targetHelpers'
import type {
  CreateTargetFormState,
  TargetFilterState,
  TargetRuntimeAction,
} from './targets/types'

const TARGET_LIST_COLUMN_WIDTHS = [40, 180, 72, 168, 150, 120, 140]
const TARGET_LIST_HEADERS = ['', '目标', '类型', 'Host', '健康', '资产上下文', '近 24h 延迟'] as const
const TAB_OWNED_RUN_STATUS = new Set(['暂停', '已归档'])

type TargetQuickView = 'all' | 'abnormal' | 'paused' | 'archived' | 'coverage'

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

  const abnormalTargetCount = useMemo(() => countAbnormalTargets(targets), [targets])
  const pausedTargetCount = useMemo(() => countPausedTargets(targets), [targets])
  const archivedTargetCount = useMemo(() => countArchivedTargets(targets), [targets])
  const coverageGapTargetCount = useMemo(() => countCoverageGapTargets(targets), [targets])
  const visibleIds = useMemo(() => filteredTargets.map((target) => target.target_id), [filteredTargets])
  const batchTargetIds = frozenBatchIds ?? selectedIds.filter((id) => visibleIds.includes(id))
  const selectedVisibleCount = visibleIds.filter((id) => selectedIds.includes(id)).length
  const allVisibleSelected = visibleIds.length > 0 && selectedVisibleCount === visibleIds.length
  const someVisibleSelected = selectedVisibleCount > 0 && !allVisibleSelected
  const hasActiveFilters = Boolean(
    filterState.type || filterState.health || filterState.runStatus || filterState.group,
  )
  const navigationLocked = pendingBatchAction !== null || batchSubmitting
  const quickView: TargetQuickView = filterState.coverageGap
    ? 'coverage'
    : filterState.abnormal
      ? 'abnormal'
      : filterState.runStatus === '暂停'
        ? 'paused'
        : filterState.runStatus === '已归档'
          ? 'archived'
          : 'all'

  useEffect(() => {
    setSelectedIds((current) => current.filter((id) => visibleIds.includes(id)))
  }, [visibleIds])

  async function runBatchOnIds(action: TargetRuntimeAction, ids: string[]) {
    setBatchSubmitting(true)
    setBatchError(null)
    setPendingBatchAction(null)
    let failCount = 0
    for (const targetID of ids) {
      try {
        if (action === 'enter-maintenance') await enterTargetMaintenance(targetID)
        else if (action === 'exit-maintenance') await exitTargetMaintenance(targetID)
        else if (action === 'pause') await pauseTarget(targetID)
        else if (action === 'resume') await resumeTarget(targetID)
        else if (action === 'archive') await archiveTarget(targetID)
        else await restoreTargetToPaused(targetID)
      } catch {
        failCount++
      }
    }
    if (failCount > 0) setBatchError(`${failCount}/${ids.length} 个目标失败`)
    setBatchSubmitting(false)
    setFrozenBatchIds(null)
    setSelectedIds([])
    try {
      const updated = await listTargets()
      setTargets(updated)
    } catch {
      /* keep current rows */
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
        if (view === 'all') {
          next.delete('abnormal')
          next.delete('coverage_gap')
          if (tabOwnedRunStatus) next.delete('run_status')
        } else if (view === 'abnormal') {
          next.set('abnormal', '1')
          next.delete('coverage_gap')
          if (tabOwnedRunStatus) next.delete('run_status')
        } else if (view === 'paused') {
          next.set('run_status', '暂停')
          next.delete('abnormal')
          next.delete('coverage_gap')
        } else if (view === 'archived') {
          next.set('run_status', '已归档')
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
            className="btn sm primary"
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
          description="创建第一个目标后，可以继续为它配置 ProbeItem。"
          action={
            <button type="button" className="btn md primary" onClick={() => openCreateDrawer()}>
              新建第一个目标
            </button>
          }
        />
      ) : (
        <>
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
                  { value: 'paused', label: '暂停', count: pausedTargetCount },
                  { value: 'archived', label: '归档', count: archivedTargetCount },
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
                    const summary = targetIssueSummary(target)
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
                            disabled={navigationLocked}
                            onChange={() => toggleSelected(target.target_id)}
                            onClick={(event) => event.stopPropagation()}
                            aria-label={`选择 ${target.name}`}
                          />
                        </td>
                        <td>
                          <div className="targets-table__identity">
                            <div className="targets-table__identity-head">
                              <StatusGlyph
                                state={targetGlyphState(target)}
                                size="md"
                                ariaLabel={`${target.name} ${badges[0]?.label ?? '运行正常'}`}
                              />
                              <div className="name">{target.name}</div>
                            </div>
                            <div className="sub">
                              成功 <Timestamp value={target.last_success_at ?? null} mode="relative" />
                              {' '}· 失败 <Timestamp value={target.last_failure_at ?? null} mode="relative" />
                            </div>
                          </div>
                        </td>
                        <td><span className="probe-kind">{target.target_type}</span></td>
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
                            {summary && !badges.some((badge) => badge.label === summary) ? (
                              <span className="targets-table__issue-summary" title={summary}>{summary}</span>
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

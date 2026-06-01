import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { type DataTableSortState } from '../components/atoms'
import { PageState } from '../components/PageState'
import {
  ApiError,
  createMonitoringInstance,
  enterMonitoringInstanceMaintenance,
  exitMonitoringInstanceMaintenance,
  listMonitoringInstanceAssetContexts,
  listMonitoringInstances,
  listMonitoringInstanceSparklines,
  pauseMonitoringInstanceMonitoring,
  postMonitoringInstanceAction,
  postMonitoringInstanceBatch,
  resumeMonitoringInstanceMonitoring,
  retireMonitoringInstance,
  restoreRetiredMonitoringInstanceToObserving,
  updateMonitoringInstanceMetadata,
} from '../lib/api'
import type { AssetContextForMonitoringInstance, CreateMonitoringInstanceInput, MonitoringInstanceRecord, MonitoringInstanceSparklinesResponse } from '../lib/types'
import { CreateMonitoringInstanceDrawer } from './monitoring/CreateMonitoringInstanceDrawer'
import { MonitoringHero } from './monitoring/MonitoringHero'
import { MonitoringInstancesListSection } from './monitoring/MonitoringInstancesListSection'
import { buildMonitoringInstancesTableColumns } from './monitoring/MonitoringInstancesTableColumns'
import { MonitoringToolbar } from './monitoring/MonitoringToolbar'
import {
  actionButtonKey,
  countAbnormalMonitoringInstances,
  countMaintenanceOrPausedMonitoringInstances,
  countPendingOnboardingMonitoringInstances,
  distinctSorted,
  initialCreateForm,
  isBindingConflictMonitoringInstance,
  isPendingOnboardingMonitoringInstance,
  isRuntimeAttentionMonitoringInstance,
  mergeNonMetadataMonitoringInstanceRecord,
  parseLabels,
  parseMultiValue,
} from './monitoring/monitoringHelpers'
import type {
  FocusRestoreRequest,
  MonitoringInstanceFilterState,
  MonitoringInstanceListView,
  MonitoringInstanceRuntimeAction,
  PendingMonitoringInstanceConfirmation,
} from './monitoring/types'

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

export function MonitoringPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [monitoring, setMonitoringInstances] = useState<MonitoringInstanceRecord[]>([])
  const [monitoringInstanceListView, setMonitoringInstanceListView] = useState<MonitoringInstanceListView>('all')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [labelInput, setLabelInput] = useState('')
  const [createForm, setCreateForm] = useState<CreateMonitoringInstanceInput>(initialCreateForm)
  const [runtimeBusyMonitoringInstanceId, setRuntimeBusyMonitoringInstanceId] = useState<string | null>(null)
  const [runtimeErrors, setRuntimeErrors] = useState<Record<string, string>>({})
  const [editingLabelMonitoringInstanceId, setEditingLabelMonitoringInstanceId] = useState<string | null>(null)
  const [labelDraft, setLabelDraft] = useState('')
  const [groupDraft, setGroupDraft] = useState('')
  const [metadataBusyMonitoringInstanceId, setMetadataBusyMonitoringInstanceId] = useState<string | null>(null)
  const [metadataErrors, setMetadataErrors] = useState<Record<string, string>>({})
  const [pendingConfirmation, setPendingConfirmation] = useState<PendingMonitoringInstanceConfirmation | null>(null)
  const actionButtonRefs = useRef<Record<string, HTMLButtonElement | null>>({})
  const pendingFocusRestoreRef = useRef<FocusRestoreRequest | null>(null)
  const [sparklines, setSparklines] = useState<MonitoringInstanceSparklinesResponse | null>(null)
  const [selectAll, setSelectAll] = useState(false)
  const [batchSubmitting, setBatchSubmitting] = useState(false)
  const [pendingBatchAction, setPendingBatchAction] = useState<string | null>(null)
  const [batchError, setBatchError] = useState<string | null>(null)
  const [commandOpen, setCommandOpen] = useState(false)
  const [commandID, setCommandID] = useState('')
  const [sortState, setSortState] = useState<DataTableSortState | null>(null)
  const [compareSet, setCompareSet] = useState<Set<string>>(new Set())
  const [monitoringInstanceAssetContexts, setMonitoringInstanceAssetContexts] = useState<Map<string, AssetContextForMonitoringInstance>>(new Map())
  const [monitoringInstanceAssetContextError, setMonitoringInstanceAssetContextError] = useState<string | null>(null)

  function resetCreateFlow() {
    setCreateError(null)
    setLabelInput('')
    setCreateForm(initialCreateForm)
  }

  useEffect(() => {
    let cancelled = false
    listMonitoringInstances()
      .then((result) => {
        if (cancelled) return
        setMonitoringInstances(result)
        setLoading(false)
      })
      .catch((value: unknown) => {
        if (cancelled) return
        setError(value instanceof ApiError ? value.message : '加载监控实例列表失败')
        setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    listMonitoringInstanceSparklines(['cpu_usage_pct', 'mem_used_pct', 'disk_used_pct'])
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
    listMonitoringInstanceAssetContexts()
      .then((contexts) => {
        if (cancelled) return
        setMonitoringInstanceAssetContexts(new Map(contexts.map((context) => [context.monitoring_instance_id, context])))
        setMonitoringInstanceAssetContextError(null)
      })
      .catch((value: unknown) => {
        if (cancelled) return
        setMonitoringInstanceAssetContexts(new Map())
        setMonitoringInstanceAssetContextError(describeError(value, '加载监控实例资产上下文失败'))
      })
    return () => {
      cancelled = true
    }
  }, [])

  function updateField<K extends keyof CreateMonitoringInstanceInput>(field: K, value: CreateMonitoringInstanceInput[K]) {
    setCreateForm((current) => ({ ...current, [field]: value }))
  }

  function queueFocusRestore(monitoringInstanceId: string, preferredAction: MonitoringInstanceRuntimeAction) {
    pendingFocusRestoreRef.current = { monitoringInstanceId, preferredAction }
  }

  useEffect(() => {
    if (pendingConfirmation) return

    const request = pendingFocusRestoreRef.current
    if (!request) return

    const preferred = actionButtonRefs.current[actionButtonKey(request.monitoringInstanceId, request.preferredAction)]
    const fallback =
      request.preferredAction === 'pause'
        ? actionButtonRefs.current[actionButtonKey(request.monitoringInstanceId, 'resume')]
        : null
    const target = [preferred, fallback].find((element) => element?.isConnected)

    target?.focus()
    pendingFocusRestoreRef.current = null
  }, [monitoring, pendingConfirmation])

  async function handleRuntimeAction(
    monitoringInstance: MonitoringInstanceRecord,
    action: MonitoringInstanceRuntimeAction,
    confirmed = false,
  ) {
    if (action === 'pause' && !confirmed) {
      setPendingConfirmation({ monitoringInstanceId: monitoringInstance.monitoring_instance_id, action })
      return
    }

    setRuntimeBusyMonitoringInstanceId(monitoringInstance.monitoring_instance_id)
    setRuntimeErrors((current) => {
      if (!current[monitoringInstance.monitoring_instance_id]) return current
      const next = { ...current }
      delete next[monitoringInstance.monitoring_instance_id]
      return next
    })

    try {
      const updated =
        action === 'enter-maintenance'
          ? await enterMonitoringInstanceMaintenance(monitoringInstance.monitoring_instance_id)
          : action === 'exit-maintenance'
            ? await exitMonitoringInstanceMaintenance(monitoringInstance.monitoring_instance_id)
            : action === 'pause'
              ? await pauseMonitoringInstanceMonitoring(monitoringInstance.monitoring_instance_id)
              : action === 'resume'
                ? await resumeMonitoringInstanceMonitoring(monitoringInstance.monitoring_instance_id)
                : action === 'retire'
                  ? await retireMonitoringInstance(monitoringInstance.monitoring_instance_id)
                  : await restoreRetiredMonitoringInstanceToObserving(monitoringInstance.monitoring_instance_id)
      setMonitoringInstances((current) =>
        current.map((item) =>
          item.monitoring_instance_id === updated.monitoring_instance_id ? mergeNonMetadataMonitoringInstanceRecord(item, updated) : item,
        ),
      )
      queueFocusRestore(updated.monitoring_instance_id, action)
      setPendingConfirmation((current) =>
        current?.monitoringInstanceId === updated.monitoring_instance_id ? null : current,
      )
    } catch (runtimeError) {
      setRuntimeErrors((current) => ({
        ...current,
        [monitoringInstance.monitoring_instance_id]: describeError(runtimeError, '监控实例运行控制操作失败'),
      }))
    } finally {
      setRuntimeBusyMonitoringInstanceId((current) => (current === monitoringInstance.monitoring_instance_id ? null : current))
    }
  }

  async function handleSaveLabels(monitoringInstance: MonitoringInstanceRecord) {
    setMetadataBusyMonitoringInstanceId(monitoringInstance.monitoring_instance_id)
    setMetadataErrors((current) => {
      if (!current[monitoringInstance.monitoring_instance_id]) return current
      const next = { ...current }
      delete next[monitoringInstance.monitoring_instance_id]
      return next
    })

    try {
      const updated = await updateMonitoringInstanceMetadata(
        monitoringInstance.monitoring_instance_id,
        {
          group: groupDraft.trim() || undefined,
          labels: parseLabels(labelDraft),
          note: monitoringInstance.note,
        },
        {
          expectedUpdatedAt: monitoringInstance.updated_at,
        },
      )
      setMonitoringInstances((current) =>
        current.map((item) =>
          item.monitoring_instance_id === updated.monitoring_instance_id
            ? {
                ...item,
                group: updated.group,
                labels: updated.labels,
                note: updated.note,
                updated_at: updated.updated_at,
              }
            : item,
        ),
      )
      setEditingLabelMonitoringInstanceId((current) => (current === monitoringInstance.monitoring_instance_id ? null : current))
      setLabelDraft('')
      setGroupDraft('')
    } catch (metadataError) {
      setMetadataErrors((current) => ({
        ...current,
        [monitoringInstance.monitoring_instance_id]: describeError(metadataError, '标签更新失败'),
      }))
    } finally {
      setMetadataBusyMonitoringInstanceId((current) => (current === monitoringInstance.monitoring_instance_id ? null : current))
    }
  }

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreateSubmitting(true)
    setCreateError(null)

    const payload: CreateMonitoringInstanceInput = {
      ...createForm,
      display_name: createForm.display_name.trim(),
      group: createForm.group.trim(),
      region: createForm.region.trim(),
      city: createForm.city.trim(),
      provider: createForm.provider.trim(),
      note: createForm.note.trim(),
      labels: parseLabels(labelInput),
    }

    try {
      const monitoringInstance = await createMonitoringInstance(payload)
      setMonitoringInstances((current) => {
        const withoutCreated = current.filter((item) => item.monitoring_instance_id !== monitoringInstance.monitoring_instance_id)
        return [monitoringInstance, ...withoutCreated]
      })
      setCreateOpen(false)
      resetCreateFlow()
      navigate(`/monitoring/${monitoringInstance.monitoring_instance_id}?onboarding=1`)
    } catch (submitError) {
      setCreateError(describeError(submitError, '接入监控实例失败'))
    } finally {
      setCreateSubmitting(false)
    }
  }

  const bindingConflictMonitoringInstances = useMemo(
    () => monitoring.filter(isBindingConflictMonitoringInstance),
    [monitoring],
  )
  const runtimeAttentionMonitoringInstances = useMemo(
    () => monitoring.filter(isRuntimeAttentionMonitoringInstance),
    [monitoring],
  )
  const abnormalMonitoringInstanceCount = useMemo(() => countAbnormalMonitoringInstances(monitoring), [monitoring])
  const pendingOnboardingMonitoringInstanceCount = useMemo(() => countPendingOnboardingMonitoringInstances(monitoring), [monitoring])
  const maintenanceOrPausedMonitoringInstanceCount = useMemo(() => countMaintenanceOrPausedMonitoringInstances(monitoring), [monitoring])
  const baseMonitoringInstances = monitoringInstanceListView === 'binding-conflict'
    ? bindingConflictMonitoringInstances
    : monitoringInstanceListView === 'runtime-attention'
      ? runtimeAttentionMonitoringInstances
      : monitoring

  const filterState: MonitoringInstanceFilterState = useMemo(
    () => ({
      group: searchParams.get('group'),
      region: searchParams.get('region'),
      city: searchParams.get('city'),
      provider: searchParams.get('provider'),
      lifecycle: searchParams.get('lifecycle'),
      runStatus: searchParams.get('run_status'),
      health: searchParams.get('health'),
      labels: parseMultiValue(searchParams.get('labels')),
      abnormal: searchParams.get('abnormal') === '1',
      onboardingPending: searchParams.get('onboarding') === 'pending',
    }),
    [searchParams],
  )

  const regionOptions = useMemo(
    () =>
      distinctSorted(monitoring.map((monitoringInstance) => monitoringInstance.region)).map((value) => ({
        value,
        label: value,
      })),
    [monitoring],
  )

  const providerOptions = useMemo(
    () =>
      distinctSorted(monitoring.map((monitoringInstance) => monitoringInstance.provider)).map((value) => ({
        value,
        label: value,
      })),
    [monitoring],
  )

  const filteredMonitoringInstances = useMemo(() => {
    return baseMonitoringInstances.filter((monitoringInstance) => {
      if (filterState.group && monitoringInstance.group !== filterState.group) return false
      if (filterState.region && monitoringInstance.region !== filterState.region) return false
      if (filterState.city && monitoringInstance.city !== filterState.city) return false
      if (filterState.provider && monitoringInstance.provider !== filterState.provider) return false
      if (filterState.lifecycle && monitoringInstance.lifecycle_status !== filterState.lifecycle) return false
      if (filterState.runStatus && monitoringInstance.monitoring_status !== filterState.runStatus) return false
      if (monitoringInstanceListView === 'runtime-attention' && !isRuntimeAttentionMonitoringInstance(monitoringInstance)) return false
      if (filterState.health && monitoringInstance.current_health_status !== filterState.health) return false
      if (filterState.labels.length > 0) {
        const hasAll = filterState.labels.every((label) => monitoringInstance.labels.includes(label))
        if (!hasAll) return false
      }
      if (filterState.abnormal && monitoringInstance.current_health_status === '正常') return false
      if (filterState.onboardingPending && !isPendingOnboardingMonitoringInstance(monitoringInstance)) return false
      return true
    })
  }, [baseMonitoringInstances, filterState, monitoringInstanceListView])

  function handleSortChange(key: string) {
    setSortState((current) => {
      if (current?.key === key) {
        const nextDir = current.direction === 'asc' ? 'desc' : 'asc'
        return { key, direction: nextDir }
      }
      return { key, direction: 'asc' }
    })
  }

  const sortedFilteredMonitoringInstances = useMemo(() => {
    if (!sortState) return filteredMonitoringInstances
    const sorted = [...filteredMonitoringInstances]
    sorted.sort((a, b) => {
      let cmp = 0
      if (sortState.key === 'identity') {
        cmp = (a.display_name || '').localeCompare(b.display_name || '', 'zh-Hans-CN')
      } else if (sortState.key === 'issue') {
        cmp = (a.current_active_incident_count ?? 0) - (b.current_active_incident_count ?? 0)
      } else if (sortState.key === 'location') {
        const la = [a.group, a.region, a.city, a.provider].filter(Boolean).join(' · ')
        const lb = [b.group, b.region, b.city, b.provider].filter(Boolean).join(' · ')
        cmp = la.localeCompare(lb, 'zh-Hans-CN')
      }
      return sortState.direction === 'desc' ? -cmp : cmp
    })
    return sorted
  }, [filteredMonitoringInstances, sortState])

  const hasActiveFilters =
    filterState.group !== null ||
    filterState.region !== null ||
    filterState.city !== null ||
    filterState.provider !== null ||
    filterState.lifecycle !== null ||
    filterState.runStatus !== null ||
    filterState.health !== null ||
    filterState.labels.length > 0 ||
    filterState.abnormal ||
    filterState.onboardingPending

  const healthOptions = ['正常', '关注', '告警', '严重']
  const lifecycleOptions = ['待接入', '在用', '观察中', '不续费', '已退役']
  const runStatusOptions = ['monitoring', 'paused', 'maintenance']

  async function executeBatchAction(action: string) {
    if (action === 'pause') {
      setPendingBatchAction('pause')
      return
    }
    setBatchSubmitting(true)
    setBatchError(null)
    const monitoringInstanceIDs = filteredMonitoringInstances.map((monitoringInstance) => monitoringInstance.monitoring_instance_id)
    try {
      const res = await postMonitoringInstanceBatch(monitoringInstanceIDs, action)
      const failed = res.results.filter((result) => !result.ok)
      if (failed.length > 0) {
        setBatchError(`${failed.length}/${monitoringInstanceIDs.length} 个监控实例失败`)
      }
    } catch (e) {
      setBatchError(describeError(e, '批量操作失败'))
    } finally {
      setBatchSubmitting(false)
      setSelectAll(false)
    }
  }

  async function executeBatchPauseConfirmed() {
    setPendingBatchAction(null)
    setBatchSubmitting(true)
    setBatchError(null)
    const monitoringInstanceIDs = filteredMonitoringInstances.map((monitoringInstance) => monitoringInstance.monitoring_instance_id)
    try {
      const res = await postMonitoringInstanceBatch(monitoringInstanceIDs, 'pause')
      const failed = res.results.filter((result) => !result.ok)
      if (failed.length > 0) {
        setBatchError(`${failed.length}/${monitoringInstanceIDs.length} 个监控实例失败`)
      }
    } catch (e) {
      setBatchError(describeError(e, '批量暂停失败'))
    } finally {
      setBatchSubmitting(false)
      setSelectAll(false)
    }
  }

  async function executeBatchCommand() {
    if (!commandID.trim()) return
    setCommandOpen(false)
    setBatchSubmitting(true)
    setBatchError(null)
    const monitoringInstanceIDs = filteredMonitoringInstances.map((monitoringInstance) => monitoringInstance.monitoring_instance_id)
    let failCount = 0
    for (const monitoringInstanceID of monitoringInstanceIDs) {
      try {
        await postMonitoringInstanceAction(monitoringInstanceID, commandID.trim())
      } catch {
        failCount++
      }
    }
    setBatchSubmitting(false)
    setSelectAll(false)
    if (failCount > 0) {
      setBatchError(`${monitoringInstanceIDs.length - failCount} 个监控实例已下发，${failCount} 个失败，等待 agent 执行`)
    }
    setCommandID('')
  }

  if (loading) {
    return <PageState kind="loading" title="正在加载监控实例列表…" />
  }

  if (error) {
    return (
      <PageState
        kind="error"
        eyebrow="监控实例"
        title="监控实例列表不可用"
        description={error}
        technicalSummary={error}
      />
    )
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

  function setAbnormalFilter(checked: boolean) {
    updateSearchParam('abnormal', checked ? '1' : null)
  }

  function setOnboardingFilter(checked: boolean) {
    updateSearchParam('onboarding', checked ? 'pending' : null)
  }

  function clearAllFilters() {
    setSearchParams(new URLSearchParams(), { replace: true })
  }

  function applyQuickView(view: string) {
    setMonitoringInstanceListView(
      view === 'binding-conflict'
        ? 'binding-conflict'
        : view === 'runtime-attention'
          ? 'runtime-attention'
          : 'all',
    )
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('abnormal')
      next.delete('onboarding')
      if (view === 'abnormal') next.set('abnormal', '1')
      if (view === 'onboarding') next.set('onboarding', 'pending')
      return next
    }, { replace: true })
  }

  const batchPanelVisible = selectAll || commandOpen || pendingBatchAction !== null || batchError !== null || batchSubmitting

  function toggleCompare(monitoringInstanceId: string) {
    setCompareSet((current) => {
      const next = new Set(current)
      if (next.has(monitoringInstanceId)) {
        next.delete(monitoringInstanceId)
      } else if (next.size < 2) {
        next.add(monitoringInstanceId)
      } else {
        return current // ignore if already 2 selected and trying a 3rd
      }
      return next
    })
  }

  function cancelLabelEdit(monitoringInstance: MonitoringInstanceRecord) {
    setEditingLabelMonitoringInstanceId((current) =>
      current === monitoringInstance.monitoring_instance_id ? null : current,
    )
    setLabelDraft('')
    setGroupDraft('')
    setMetadataErrors((current) => {
      if (!current[monitoringInstance.monitoring_instance_id]) return current
      const next = { ...current }
      delete next[monitoringInstance.monitoring_instance_id]
      return next
    })
  }

  function startLabelEdit(monitoringInstance: MonitoringInstanceRecord) {
    if (metadataBusyMonitoringInstanceId !== null) return
    setEditingLabelMonitoringInstanceId(monitoringInstance.monitoring_instance_id)
    setLabelDraft(monitoringInstance.labels.join(', '))
    setGroupDraft(monitoringInstance.group || '')
    setMetadataErrors((current) => {
      if (!current[monitoringInstance.monitoring_instance_id]) return current
      const next = { ...current }
      delete next[monitoringInstance.monitoring_instance_id]
      return next
    })
  }

  const columns = buildMonitoringInstancesTableColumns({
    compareSet,
    sparklines,
    assetContexts: monitoringInstanceAssetContexts,
    editingLabelMonitoringInstanceId,
    labelDraft,
    groupDraft,
    metadataBusyMonitoringInstanceId,
    metadataErrors,
    runtimeBusyMonitoringInstanceId,
    actionButtonRefs,
    onToggleCompare: toggleCompare,
    onLabelDraftChange: setLabelDraft,
    onGroupDraftChange: setGroupDraft,
    onSaveLabels: (monitoringInstance) => void handleSaveLabels(monitoringInstance),
    onCancelLabels: cancelLabelEdit,
    onStartLabelEdit: startLabelEdit,
    onRuntimeAction: (monitoringInstance, action) => void handleRuntimeAction(monitoringInstance, action),
    onQueueFocusRestore: queueFocusRestore,
  })

  function shouldNavigateOnRowClick(monitoringInstance: MonitoringInstanceRecord): boolean {
    // Block navigation while the row is in edit mode or has a pending pause confirmation.
    if (editingLabelMonitoringInstanceId === monitoringInstance.monitoring_instance_id) return false
    if (pendingConfirmation?.monitoringInstanceId === monitoringInstance.monitoring_instance_id) return false
    return true
  }

  function toggleCreateDrawer() {
    setCreateOpen((current) => {
      if (current) {
        resetCreateFlow()
      }
      return !current
    })
  }

  return (
    <div className="page-stack animate-in">
      <MonitoringHero
        totalMonitoringInstanceCount={monitoring.length}
        abnormalMonitoringInstanceCount={abnormalMonitoringInstanceCount}
        pendingOnboardingMonitoringInstanceCount={pendingOnboardingMonitoringInstanceCount}
        maintenanceOrPausedMonitoringInstanceCount={maintenanceOrPausedMonitoringInstanceCount}
        onAbnormalClick={() => setAbnormalFilter(abnormalMonitoringInstanceCount > 0)}
        onOnboardingClick={() => setOnboardingFilter(pendingOnboardingMonitoringInstanceCount > 0)}
        onRuntimeAttentionClick={() => applyQuickView('runtime-attention')}
        onCreateClick={toggleCreateDrawer}
      />

      <CreateMonitoringInstanceDrawer
        open={createOpen}
        form={createForm}
        labelInput={labelInput}
        submitting={createSubmitting}
        error={createError}
        onClose={() => {
          setCreateOpen(false)
          resetCreateFlow()
        }}
        onSubmit={handleCreate}
        onFieldChange={updateField}
        onLabelInputChange={setLabelInput}
      />

      <div className="animate-in d2">
        <MonitoringToolbar
          filterState={filterState}
          healthOptions={healthOptions}
          lifecycleOptions={lifecycleOptions}
          runStatusOptions={runStatusOptions}
          regionOptions={regionOptions}
          providerOptions={providerOptions}
          compareSet={compareSet}
          onFilterChange={updateSearchParam}
          onAbnormalChange={(checked) => setAbnormalFilter(checked)}
        />
        {monitoringInstanceAssetContextError ? (
          <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
            {monitoringInstanceAssetContextError}
          </p>
        ) : null}

        <MonitoringInstancesListSection
          monitoringInstanceListView={monitoringInstanceListView}
          baseMonitoringInstances={baseMonitoringInstances}
          monitoring={sortedFilteredMonitoringInstances}
          columns={columns}
          showTrends={true}
          sortState={sortState}
          hasActiveFilters={hasActiveFilters}
          batchPanelVisible={batchPanelVisible}
          selectAll={selectAll}
          batchSubmitting={batchSubmitting}
          batchError={batchError}
          commandOpen={commandOpen}
          commandID={commandID}
          pendingBatchAction={pendingBatchAction}
          runtimeErrors={runtimeErrors}
          pendingConfirmation={pendingConfirmation}
          runtimeBusyMonitoringInstanceId={runtimeBusyMonitoringInstanceId}
          onClearAllFilters={clearAllFilters}
          onSelectAllChange={setSelectAll}
          onBatchAction={(action) => void executeBatchAction(action)}
          onCommandOpenChange={setCommandOpen}
          onCommandIDChange={setCommandID}
          onExecuteBatchCommand={() => void executeBatchCommand()}
          onConfirmBatchPause={() => void executeBatchPauseConfirmed()}
          onCancelBatchPause={() => setPendingBatchAction(null)}
          onSortChange={handleSortChange}
          onRowClick={(monitoringInstance) => {
            if (!shouldNavigateOnRowClick(monitoringInstance)) return
            navigate(`/monitoring/${monitoringInstance.monitoring_instance_id}`)
          }}
          onConfirmPause={(monitoringInstance) => void handleRuntimeAction(monitoringInstance, 'pause', true)}
          onCancelPause={(monitoringInstance) => {
            queueFocusRestore(monitoringInstance.monitoring_instance_id, 'pause')
            setPendingConfirmation((current) =>
              current?.monitoringInstanceId === monitoringInstance.monitoring_instance_id ? null : current,
            )
          }}
          onCreateMonitoringInstance={toggleCreateDrawer}
        />
      </div>
    </div>
  )
}

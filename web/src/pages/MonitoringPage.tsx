import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { type DataTableSortState } from '../components/atoms'
import { PageState } from '../components/PageState'
import {
  ApiError,
  listMonitoringInstanceAssetContexts,
  listMonitoringInstances,
  listMonitoringInstanceSparklines,
  postMonitoringInstanceAction,
  postMonitoringInstanceBatch,
} from '../lib/api'
import type { AssetContextForMonitoringInstance, MonitoringInstanceRecord, MonitoringInstanceSparklinesResponse } from '../lib/types'
import { MonitoringHero } from './monitoring/MonitoringHero'
import { MonitoringInstancesListSection } from './monitoring/MonitoringInstancesListSection'
import { buildMonitoringInstancesTableColumns } from './monitoring/MonitoringInstancesTableColumns'
import { MonitoringSupportSurface } from './monitoring/MonitoringSupportSurface'
import { MonitoringToolbar } from './monitoring/MonitoringToolbar'
import {
  buildMonitoringInstanceEvidenceLead,
  countAbnormalMonitoringInstances,
  countMaintenanceOrPausedMonitoringInstances,
  countPendingOnboardingMonitoringInstances,
  describeMonitoringInstanceFilterContext,
  distinctSorted,
  isBindingConflictMonitoringInstance,
  isPendingOnboardingMonitoringInstance,
  isRuntimeAttentionMonitoringInstance,
  MONITORING_INSTANCE_HEALTH_STATUS_FILTER_OPTIONS,
  MONITORING_INSTANCE_LIFECYCLE_FILTER_OPTIONS,
  MONITORING_INSTANCE_RUN_STATUS_FILTER_OPTIONS,
  parseMultiValue,
  pickTopMonitoringInstanceEvidence,
} from './monitoring/monitoringHelpers'
import type {
  MonitoringInstanceFilterState,
  MonitoringInstanceListView,
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
  const [sparklines, setSparklines] = useState<MonitoringInstanceSparklinesResponse | null>(null)
  const [selectAll, setSelectAll] = useState(false)
  const [batchPanelOpen, setBatchPanelOpen] = useState(false)
  const [batchSubmitting, setBatchSubmitting] = useState(false)
  const [pendingBatchAction, setPendingBatchAction] = useState<string | null>(null)
  const [batchError, setBatchError] = useState<string | null>(null)
  const [commandOpen, setCommandOpen] = useState(false)
  const [commandID, setCommandID] = useState('')
  const [sortState, setSortState] = useState<DataTableSortState | null>(null)
  const [compareSet, setCompareSet] = useState<Set<string>>(new Set())
  const [monitoringInstanceAssetContexts, setMonitoringInstanceAssetContexts] = useState<Map<string, AssetContextForMonitoringInstance>>(new Map())
  const [monitoringInstanceAssetContextError, setMonitoringInstanceAssetContextError] = useState<string | null>(null)

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
  const monitoringFilterContext = describeMonitoringInstanceFilterContext(filterState)
  const monitoringEvidenceLead = buildMonitoringInstanceEvidenceLead({
    totalMonitoringInstanceCount: monitoring.length,
    displayedMonitoringInstanceCount: sortedFilteredMonitoringInstances.length,
    abnormalMonitoringInstanceCount,
    pendingOnboardingMonitoringInstanceCount,
    maintenanceOrPausedMonitoringInstanceCount,
    hasActiveFilters,
  })
  const topMonitoringEvidence = pickTopMonitoringInstanceEvidence(sortedFilteredMonitoringInstances)

  const healthOptions = MONITORING_INSTANCE_HEALTH_STATUS_FILTER_OPTIONS.map((option) => option.value)
  const lifecycleOptions = MONITORING_INSTANCE_LIFECYCLE_FILTER_OPTIONS.map((option) => option.value)
  const runStatusOptions = MONITORING_INSTANCE_RUN_STATUS_FILTER_OPTIONS.map((option) => option.value)

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
      setBatchPanelOpen(false)
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
      setBatchPanelOpen(false)
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
    setBatchPanelOpen(false)
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

  const batchPanelVisible = batchPanelOpen || selectAll || commandOpen || pendingBatchAction !== null || batchError !== null || batchSubmitting

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

  const columns = buildMonitoringInstancesTableColumns({
    compareSet,
    sparklines,
    assetContexts: monitoringInstanceAssetContexts,
    onToggleCompare: toggleCompare,
  })

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
      />

      <MonitoringSupportSurface
        totalMonitoringInstanceCount={monitoring.length}
        displayedMonitoringInstanceCount={sortedFilteredMonitoringInstances.length}
        abnormalMonitoringInstanceCount={abnormalMonitoringInstanceCount}
        pendingOnboardingMonitoringInstanceCount={pendingOnboardingMonitoringInstanceCount}
        evidenceLead={monitoringEvidenceLead}
        topEvidence={topMonitoringEvidence}
        filterContext={monitoringFilterContext}
        hasActiveFilters={hasActiveFilters}
        onAbnormalClick={() => setAbnormalFilter(abnormalMonitoringInstanceCount > 0)}
        onOnboardingClick={() => setOnboardingFilter(pendingOnboardingMonitoringInstanceCount > 0)}
        onRuntimeAttentionClick={() => applyQuickView('runtime-attention')}
        onClearFilters={clearAllFilters}
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
          onOpenBatchPanel={() => setBatchPanelOpen(true)}
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
            navigate(`/monitoring/${monitoringInstance.monitoring_instance_id}`)
          }}
          onOpenVPSInventory={() => navigate('/vps')}
        />
      </div>
    </div>
  )
}

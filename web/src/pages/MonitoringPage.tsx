import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { TabPanel } from '../components/atoms'
import { PageState } from '../components/PageState'
import {
  ApiError,
  getSettings,
  listMonitoringInstances,
  listMonitoringInstanceRuntimeSummaries,
  listMonitoringInstanceSparklines,
  postMonitoringInstanceAction,
  postMonitoringInstanceBatch,
} from '../lib/api'
import { resolveThresholds, type MetricThresholds } from '../config/thresholds'
import type {
  MonitoringInstanceRecord,
  MonitoringInstanceRuntimeSummariesResponse,
  MonitoringInstanceSparklinesResponse,
} from '../lib/types'
import { MonitoringHero } from './monitoring/MonitoringHero'
import { MonitoringInstancesBatchPanel } from './monitoring/MonitoringInstancesBatchPanel'
import { MonitoringInstancesFilterPanel } from './monitoring/MonitoringInstancesFilterPanel'
import { MonitoringInstancesListSection } from './monitoring/MonitoringInstancesListSection'
import { buildMonitoringInstancesTableColumns } from './monitoring/MonitoringInstancesTableColumns'
import { MonitoringToolbar } from './monitoring/MonitoringToolbar'
import { classifyHeartbeatFreshness, readHeartbeatFreshnessPolicy } from './monitoring/heartbeatFreshness'
import {
  compareMonitoringHealth,
  compareMonitoringHeartbeat,
  countAbnormalMonitoringInstances,
  countBindingConflictMonitoringInstances,
  countMaintenanceOrPausedMonitoringInstances,
  countPendingOnboardingMonitoringInstances,
  distinctSorted,
  matchesMonitoringFilters,
  matchesMonitoringQuickView,
  matchesMonitoringSearch,
  monitoringLocationLine,
} from './monitoring/monitoringHelpers'
import {
  clearMonitoringFilters,
  currentMonitoringListHref,
  hasActiveMonitoringFilters,
  monitoringListNavigationState,
  parseMonitoringFilters,
  parseMonitoringQuickView,
  parseMonitoringSearchQuery,
  parseMonitoringSelectedIds,
  parseMonitoringSortState,
  writeMonitoringFilters,
  writeMonitoringQuickView,
  writeMonitoringSearchQuery,
  writeMonitoringSelectedIds,
  writeMonitoringSort,
} from './monitoring/monitoringListUrl'
import type {
  HeartbeatFreshness,
  HeartbeatFreshnessPolicy,
  MonitoringInstanceFilterState,
  MonitoringInstanceQuickView,
  MonitoringInstanceSortKey,
  MonitoringInstanceSortState,
} from './monitoring/types'

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

export function MonitoringPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const [monitoring, setMonitoringInstances] = useState<MonitoringInstanceRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [snapshotReadAt, setSnapshotReadAt] = useState<Date | null>(null)
  const [sparklines, setSparklines] = useState<MonitoringInstanceSparklinesResponse | null>(null)
  const [sparklinesError, setSparklinesError] = useState<string | null>(null)
  const [runtimeSummaries, setRuntimeSummaries] = useState<MonitoringInstanceRuntimeSummariesResponse | null>(null)
  const [runtimeSummariesError, setRuntimeSummariesError] = useState<string | null>(null)
  const [thresholds, setThresholds] = useState<MetricThresholds | null>(null)
  const [heartbeatPolicy, setHeartbeatPolicy] = useState<HeartbeatFreshnessPolicy | null>(null)
  const [settingsError, setSettingsError] = useState<string | null>(null)
  const [settingsResolved, setSettingsResolved] = useState(false)
  const [batchSubmitting, setBatchSubmitting] = useState(false)
  const [pendingBatchAction, setPendingBatchAction] = useState<string | null>(null)
  const [frozenBatchIds, setFrozenBatchIds] = useState<string[] | null>(null)
  const [batchError, setBatchError] = useState<string | null>(null)
  const [commandOpen, setCommandOpen] = useState(false)
  const [commandID, setCommandID] = useState('')
  const [listReloadKey, setListReloadKey] = useState(0)
  const [sparkReloadKey, setSparkReloadKey] = useState(0)
  const [settingsReloadKey, setSettingsReloadKey] = useState(0)
  const [runtimeSummariesReloadKey, setRuntimeSummariesReloadKey] = useState(0)
  const loadedRef = useRef(false)
  const listGeneration = useRef(0)
  const sparkGeneration = useRef(0)
  const settingsGeneration = useRef(0)
  const runtimeSummariesGeneration = useRef(0)

  const searchQuery = parseMonitoringSearchQuery(searchParams)
  const quickView = parseMonitoringQuickView(searchParams)
  const sortState = parseMonitoringSortState(searchParams)
  const filterState = useMemo(() => parseMonitoringFilters(searchParams), [searchParams])
  const selectedIds = useMemo(() => parseMonitoringSelectedIds(searchParams), [searchParams])
  const listHref = currentMonitoringListHref(location.search)
  const detailState = monitoringListNavigationState(location.state, listHref)
  const navigationLocked = pendingBatchAction !== null || commandOpen || batchSubmitting
  const selectionSnapshotReady = snapshotReadAt !== null

  useEffect(() => {
    const generation = ++listGeneration.current
    if (loadedRef.current) {
      setRefreshing(true)
    } else {
      setLoading(true)
      setError(null)
    }

    listMonitoringInstances()
      .then((result) => {
        if (generation !== listGeneration.current) return
        loadedRef.current = true
        setMonitoringInstances(result)
        setError(null)
        setSnapshotReadAt(new Date())
        setLoading(false)
        setRefreshing(false)
      })
      .catch((value: unknown) => {
        if (generation !== listGeneration.current) return
        setError(describeError(value, '加载监控实例列表失败'))
        setLoading(false)
        setRefreshing(false)
      })

    return () => {
      listGeneration.current += 1
    }
  }, [listReloadKey])

  useEffect(() => {
    const generation = ++sparkGeneration.current
    listMonitoringInstanceSparklines(['cpu_usage_pct', 'mem_used_pct', 'disk_used_pct'])
      .then((data) => {
        if (generation !== sparkGeneration.current) return
        setSparklines(data)
        setSparklinesError(null)
      })
      .catch((value: unknown) => {
        if (generation !== sparkGeneration.current) return
        setSparklinesError(describeError(value, '24小时趋势不可用'))
      })
    return () => {
      sparkGeneration.current += 1
    }
  }, [sparkReloadKey])
  useEffect(() => {
    const generation = ++runtimeSummariesGeneration.current
    listMonitoringInstanceRuntimeSummaries()
      .then((data) => {
        if (generation !== runtimeSummariesGeneration.current) return
        setRuntimeSummaries(data)
        setRuntimeSummariesError(null)
      })
      .catch((value: unknown) => {
        if (generation !== runtimeSummariesGeneration.current) return
        setRuntimeSummariesError(describeError(value, '运行时长与网络速率不可用'))
      })
    return () => {
      runtimeSummariesGeneration.current += 1
    }
  }, [runtimeSummariesReloadKey])


  useEffect(() => {
    const generation = ++settingsGeneration.current
    getSettings()
      .then((settings) => {
        if (generation !== settingsGeneration.current) return
        const policy = readHeartbeatFreshnessPolicy(settings.incident_defaults)
        setHeartbeatPolicy(policy)
        setThresholds(resolveThresholds(settings.incident_defaults))
        setSettingsError(policy ? null : '新鲜度策略不可用')
        setSettingsResolved(true)
      })
      .catch((value: unknown) => {
        if (generation !== settingsGeneration.current) return
        setHeartbeatPolicy(null)
        setThresholds(null)
        setSettingsError(describeError(value, '新鲜度策略不可用'))
        setSettingsResolved(true)
      })
    return () => {
      settingsGeneration.current += 1
    }
  }, [settingsReloadKey])

  const freshnessById = useMemo(() => {
    const now = snapshotReadAt ?? new Date()
    const map = new Map<string, HeartbeatFreshness>()
    for (const monitoringInstance of monitoring) {
      map.set(
        monitoringInstance.monitoring_instance_id,
        classifyHeartbeatFreshness(
          monitoringInstance.last_heartbeat_at,
          heartbeatPolicy,
          now,
          settingsResolved,
        ),
      )
    }
    return map
  }, [monitoring, heartbeatPolicy, snapshotReadAt, settingsResolved])

  const abnormalMonitoringInstanceCount = useMemo(() => countAbnormalMonitoringInstances(monitoring), [monitoring])
  const pendingOnboardingMonitoringInstanceCount = useMemo(
    () => countPendingOnboardingMonitoringInstances(monitoring),
    [monitoring],
  )
  const maintenanceOrPausedMonitoringInstanceCount = useMemo(
    () => countMaintenanceOrPausedMonitoringInstances(monitoring),
    [monitoring],
  )
  const bindingConflictMonitoringInstanceCount = useMemo(
    () => countBindingConflictMonitoringInstances(monitoring),
    [monitoring],
  )

  const baseMonitoringInstances = useMemo(
    () => monitoring.filter((monitoringInstance) => matchesMonitoringQuickView(monitoringInstance, quickView)),
    [monitoring, quickView],
  )

  const filteredMonitoringInstances = useMemo(() => {
    return baseMonitoringInstances.filter((monitoringInstance) => {
      if (!matchesMonitoringFilters(monitoringInstance, filterState)) return false
      return matchesMonitoringSearch(monitoringInstance, searchQuery)
    })
  }, [baseMonitoringInstances, filterState, searchQuery])

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
        cmp = monitoringLocationLine(a).localeCompare(monitoringLocationLine(b), 'zh-Hans-CN')
      } else if (sortState.key === 'health') {
        cmp = compareMonitoringHealth(a, b)
      } else if (sortState.key === 'heartbeat') {
        cmp = compareMonitoringHeartbeat(
          freshnessById.get(a.monitoring_instance_id) ?? { kind: 'missing' },
          freshnessById.get(b.monitoring_instance_id) ?? { kind: 'missing' },
        )
      }
      return sortState.direction === 'desc' ? -cmp : cmp
    })
    return sorted
  }, [filteredMonitoringInstances, sortState, freshnessById])

  const visibleEligibleIds = useMemo(
    () => sortedFilteredMonitoringInstances
      .filter((monitoringInstance) => !monitoringInstance.archived_at)
      .map((monitoringInstance) => monitoringInstance.monitoring_instance_id),
    [sortedFilteredMonitoringInstances],
  )
  const visibleEligibleSet = useMemo(() => new Set(visibleEligibleIds), [visibleEligibleIds])
  const selectedIdsForView = (
    selectionSnapshotReady && frozenBatchIds === null
      ? selectedIds.filter((id) => visibleEligibleSet.has(id))
      : selectedIds
  )

  useEffect(() => {
    if (!selectionSnapshotReady || frozenBatchIds !== null) return
    const nextSelectedIds = selectedIds.filter((id) => visibleEligibleSet.has(id))
    const next = new URLSearchParams(searchParams)
    writeMonitoringSelectedIds(next, nextSelectedIds)
    if (next.toString() === searchParams.toString()) return
    setSearchParams(next, { replace: true, state: location.state })
  }, [
    frozenBatchIds,
    location.state,
    searchParams,
    selectedIds,
    selectionSnapshotReady,
    setSearchParams,
    visibleEligibleSet,
  ])

  const selectedSet = new Set(selectedIdsForView)
  const selectedVisibleCount = visibleEligibleIds.filter((id) => selectedSet.has(id)).length
  const allVisibleSelected = visibleEligibleIds.length > 0 && selectedVisibleCount === visibleEligibleIds.length
  const someVisibleSelected = selectedVisibleCount > 0 && !allVisibleSelected
  const batchTargetIds = frozenBatchIds ?? selectedIdsForView

  const hasActiveFilters = hasActiveMonitoringFilters(filterState)
  const groupOptions = useMemo(
    () => distinctSorted(monitoring.map((monitoringInstance) => monitoringInstance.group)).map((value) => ({ value, label: value })),
    [monitoring],
  )
  const regionOptions = useMemo(
    () => distinctSorted(monitoring.map((monitoringInstance) => monitoringInstance.region)).map((value) => ({ value, label: value })),
    [monitoring],
  )
  const cityOptions = useMemo(
    () => distinctSorted(monitoring.map((monitoringInstance) => monitoringInstance.city)).map((value) => ({ value, label: value })),
    [monitoring],
  )
  const providerOptions = useMemo(
    () => distinctSorted(monitoring.map((monitoringInstance) => monitoringInstance.provider)).map((value) => ({ value, label: value })),
    [monitoring],
  )
  const labelOptions = useMemo(
    () => distinctSorted(monitoring.flatMap((monitoringInstance) => monitoringInstance.labels)).map((value) => ({ value, label: value })),
    [monitoring],
  )

  function navigateQuery(next: URLSearchParams, replace: boolean, flushSync = false) {
    setSearchParams(next, { replace, state: location.state, flushSync })
  }

  function refreshAll() {
    setListReloadKey((key) => key + 1)
    setSparkReloadKey((key) => key + 1)
    setSettingsReloadKey((key) => key + 1)
    setRuntimeSummariesReloadKey((key) => key + 1)
  }

  function setQuickView(view: MonitoringInstanceQuickView) {
    if (navigationLocked) return
    const next = new URLSearchParams(searchParams)
    writeMonitoringQuickView(next, view)
    navigateQuery(next, false)
  }

  function setSearchQuery(value: string) {
    if (navigationLocked) return
    const next = new URLSearchParams(searchParams)
    writeMonitoringSearchQuery(next, value)
    navigateQuery(next, true, true)
  }

  function handleSortChange(key: string) {
    if (navigationLocked) return
    const sortKey = key as MonitoringInstanceSortKey
    let nextSort: MonitoringInstanceSortState | null
    if (sortState?.key === sortKey) {
      nextSort = { key: sortKey, direction: sortState.direction === 'asc' ? 'desc' : 'asc' }
    } else {
      nextSort = { key: sortKey, direction: 'asc' }
    }
    const next = new URLSearchParams(searchParams)
    writeMonitoringSort(next, nextSort)
    navigateQuery(next, true)
  }

  function applyFilters(nextFilters: MonitoringInstanceFilterState) {
    if (navigationLocked) return
    const next = new URLSearchParams(searchParams)
    writeMonitoringFilters(next, nextFilters)
    navigateQuery(next, true)
  }

  function replaceSelectedIds(nextSelectedIds: string[]) {
    const next = new URLSearchParams(searchParams)
    writeMonitoringSelectedIds(next, nextSelectedIds)
    navigateQuery(next, true, true)
  }

  function toggleSelected(monitoringInstanceId: string) {
    if (navigationLocked) return
    if (!visibleEligibleIds.includes(monitoringInstanceId)) return
    replaceSelectedIds(
      selectedIdsForView.includes(monitoringInstanceId)
        ? selectedIdsForView.filter((id) => id !== monitoringInstanceId)
        : [...selectedIdsForView, monitoringInstanceId],
    )
  }

  function toggleSelectAll(checked: boolean) {
    if (navigationLocked) return
    replaceSelectedIds(checked ? [...visibleEligibleIds] : [])
  }

  function clearFilterFields() {
    if (navigationLocked) return
    const next = new URLSearchParams(searchParams)
    clearMonitoringFilters(next)
    navigateQuery(next, true)
  }

  function clearAllFilters() {
    if (navigationLocked) return
    const next = new URLSearchParams(searchParams)
    clearMonitoringFilters(next)
    writeMonitoringSearchQuery(next, '')
    writeMonitoringQuickView(next, 'all')
    navigateQuery(next, true)
  }

  async function executeBatchAction(action: string) {
    const monitoringInstanceIDs = batchTargetIds
    if (action === 'pause') {
      setFrozenBatchIds([...monitoringInstanceIDs])
      setPendingBatchAction('pause')
      return
    }
    if (monitoringInstanceIDs.length === 0) {
      replaceSelectedIds([])
      return
    }
    setFrozenBatchIds([...monitoringInstanceIDs])
    setBatchSubmitting(true)
    setBatchError(null)
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
      replaceSelectedIds([])
      setFrozenBatchIds(null)
      refreshAll()
    }
  }

  async function executeBatchPauseConfirmed() {
    const monitoringInstanceIDs = frozenBatchIds ?? batchTargetIds
    setPendingBatchAction(null)
    if (monitoringInstanceIDs.length === 0) {
      replaceSelectedIds([])
      setFrozenBatchIds(null)
      return
    }
    setFrozenBatchIds([...monitoringInstanceIDs])
    setBatchSubmitting(true)
    setBatchError(null)
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
      replaceSelectedIds([])
      setFrozenBatchIds(null)
      refreshAll()
    }
  }

  async function executeBatchCommand(commandId: string, options: { confirmedSensitive?: boolean } = {}) {
    const selectedCommandID = commandId.trim()
    if (!selectedCommandID) return
    const monitoringInstanceIDs = frozenBatchIds ?? batchTargetIds
    if (monitoringInstanceIDs.length === 0) {
      setCommandOpen(false)
      replaceSelectedIds([])
      setFrozenBatchIds(null)
      return
    }
    setFrozenBatchIds([...monitoringInstanceIDs])
    setBatchSubmitting(true)
    setBatchError(null)
    let failCount = 0
    for (const monitoringInstanceID of monitoringInstanceIDs) {
      try {
        await postMonitoringInstanceAction(monitoringInstanceID, selectedCommandID, options)
      } catch {
        failCount++
      }
    }
    setBatchSubmitting(false)
    setCommandOpen(false)
    replaceSelectedIds([])
    setFrozenBatchIds(null)
    setCommandID('')
    if (failCount > 0) {
      setBatchError(`${monitoringInstanceIDs.length - failCount} 个监控实例已下发，${failCount} 个失败，等待 agent 执行`)
    }
    refreshAll()
  }

  const columns = buildMonitoringInstancesTableColumns({
    selectedIds: selectedIdsForView,
    allVisibleSelected,
    someVisibleSelected,
    sparklines,
    thresholds,
    detailState,
    freshnessById,
    snapshotReadAt,
    navigationLocked,
    onToggleSelected: toggleSelected,
    onToggleSelectAll: toggleSelectAll,
    runtimeSummaries,
  })
  const compareHref = selectedIdsForView.length === 2
    ? `/monitoring/compare?${selectedIdsForView.map((id) => `id=${encodeURIComponent(id)}`).join('&')}`
    : ''
  const settingsFreshnessUnavailable = settingsResolved && heartbeatPolicy === null
  const summaryCountsReady = snapshotReadAt !== null

  return (
    <div className="page monitoring-page">
      <MonitoringHero
        snapshotReadAt={snapshotReadAt}
        refreshing={refreshing || loading}
        refreshLocked={navigationLocked}
        onRefresh={refreshAll}
      />

      <MonitoringToolbar
        quickView={quickView}
        quickViews={[
          { value: 'all', label: '全部', ...(summaryCountsReady ? { count: monitoring.length } : {}) },
          { value: 'abnormal', label: '异常', ...(summaryCountsReady ? { count: abnormalMonitoringInstanceCount } : {}) },
          { value: 'onboarding', label: '待接入', ...(summaryCountsReady ? { count: pendingOnboardingMonitoringInstanceCount } : {}) },
          { value: 'runtime-attention', label: '维护/暂停', ...(summaryCountsReady ? { count: maintenanceOrPausedMonitoringInstanceCount } : {}) },
          { value: 'binding-conflict', label: '绑定异常', ...(summaryCountsReady ? { count: bindingConflictMonitoringInstanceCount } : {}) },
        ]}
        navigationLocked={navigationLocked}
        onQuickViewChange={setQuickView}
        filters={(
          <MonitoringInstancesFilterPanel
            searchQuery={searchQuery}
            hasActiveFilters={hasActiveFilters}
            filterState={filterState}
            groupOptions={groupOptions}
            regionOptions={regionOptions}
            cityOptions={cityOptions}
            providerOptions={providerOptions}
            labelOptions={labelOptions}
            disabled={navigationLocked}
            batch={(
              <MonitoringInstancesBatchPanel
                selectedCount={selectedIdsForView.length}
                confirmedBatchCount={batchTargetIds.length}
                batchSubmitting={batchSubmitting}
                batchError={batchError}
                commandOpen={commandOpen}
                commandID={commandID}
                pendingBatchAction={pendingBatchAction}
                navigationLocked={navigationLocked}
                compareHref={compareHref}
                compareState={detailState}
                onBatchAction={(action) => void executeBatchAction(action)}
                onCommandOpenChange={(open) => {
                  setCommandOpen(open)
                  if (open) setFrozenBatchIds([...selectedIdsForView])
                  else if (pendingBatchAction === null) setFrozenBatchIds(null)
                }}
                onCommandIDChange={setCommandID}
                onExecuteBatchCommand={(commandId, options) => void executeBatchCommand(commandId, options)}
                onConfirmBatchPause={() => void executeBatchPauseConfirmed()}
                onCancelBatchPause={() => {
                  setPendingBatchAction(null)
                  setFrozenBatchIds(null)
                }}
              />
            )}
            onSearchChange={setSearchQuery}
            onClearAll={clearFilterFields}
            onSingleFilterChange={(key, value) => {
              if (key === 'run_status') applyFilters({ ...filterState, runStatus: value })
              else applyFilters({ ...filterState, [key]: value })
            }}
            onMultiFilterChange={(_key, values) => applyFilters({ ...filterState, labels: values })}
          />
        )}
      />

      {error && monitoring.length > 0 ? (
        <p className="monitoring-page__source-error" role="alert">
          列表刷新失败，仍显示上次读取结果。{error}
          <button type="button" className="btn sm ghost" onClick={refreshAll}>重试</button>
        </p>
      ) : null}
      {sparklinesError ? (
        <p className="monitoring-page__source-error" role="status">
          24小时历史趋势不可用。{sparklinesError}
          <button type="button" className="btn sm ghost" onClick={() => setSparkReloadKey((key) => key + 1)}>重试趋势</button>
        </p>
      ) : null}
      {runtimeSummariesError ? (
        <p className="monitoring-page__source-error" role="status">
          {runtimeSummaries
            ? `运行时长与网络速率刷新失败，仍显示上次读取结果。${runtimeSummariesError}`
            : `运行时长与网络速率不可用。${runtimeSummariesError}`}
          <button
            type="button"
            className="btn sm ghost"
            onClick={() => setRuntimeSummariesReloadKey((key) => key + 1)}
          >
            重试运行指标
          </button>
        </p>
      ) : null}
      {settingsFreshnessUnavailable ? (
        <p className="monitoring-page__source-error" role="status">
          新鲜度策略不可用，心跳时间仍保留。
          {thresholds ? null : '指标颜色不按配置阈值着色。'}
          {settingsError && settingsError !== '新鲜度策略不可用' ? settingsError : null}
          <button type="button" className="btn sm ghost" onClick={() => setSettingsReloadKey((key) => key + 1)}>重试策略</button>
        </p>
      ) : null}

      <TabPanel idBase="monitoring-quick-view" value={quickView} className="page__work monitoring-page__work">
        {loading && monitoring.length === 0 ? (
          <PageState kind="loading" title="正在加载监控实例列表…" />
        ) : error && monitoring.length === 0 ? (
          <PageState
            kind="error"
            title="监控实例列表不可用"
            description={error}
            technicalSummary={error}
            action={<button type="button" className="btn md ghost" onClick={refreshAll}>重试</button>}
          />
        ) : (
          <MonitoringInstancesListSection
            key="monitoring-table-v4"
            quickView={quickView}
            baseMonitoringInstances={baseMonitoringInstances}
            monitoring={sortedFilteredMonitoringInstances}
            columns={columns}
            sortState={sortState}
            hasActiveFilters={hasActiveFilters}
            hasSearchQuery={searchQuery.trim().length > 0}
            navigationLocked={navigationLocked}
            refreshing={refreshing}
            onClearAllFilters={clearAllFilters}
            onSortChange={handleSortChange}
            onRowClick={(monitoringInstance) => {
              navigate(`/monitoring/${monitoringInstance.monitoring_instance_id}`, { state: detailState })
            }}
            onOpenVPSInventory={() => navigate('/vps?view=unlinked')}
          />
        )}
      </TabPanel>
    </div>
  )
}

import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { type DataTableSortState } from '../components/atoms'
import { PageState } from '../components/PageState'
import {
  ApiError,
  createNode,
  enterNodeMaintenance,
  exitNodeMaintenance,
  issueNodeEnrollmentToken,
  listNodes,
  listNodeSparklines,
  pauseNodeMonitoring,
  postNodeAction,
  postNodeBatch,
  resumeNodeMonitoring,
  updateNodeMetadata,
} from '../lib/api'
import { setOnboardingTokenCache } from '../lib/onboardingTokenCache'
import type { CreateNodeInput, NodeRecord, NodeSparklinesResponse } from '../lib/types'
import { type AutoRefreshOption, useAutoRefresh } from '../lib/useAutoRefresh'
import { CreateNodeDrawer } from './nodes/CreateNodeDrawer'
import { NodesHero } from './nodes/NodesHero'
import { NodesListSection } from './nodes/NodesListSection'
import { NodesSupportSurface } from './nodes/NodesSupportSurface'
import { buildNodesTableColumns } from './nodes/NodesTableColumns'
import { NodesToolbar } from './nodes/NodesToolbar'
import {
  actionButtonKey,
  buildNodeEvidenceLead,
  countAbnormalNodes,
  countMaintenanceOrPausedNodes,
  countPendingOnboardingNodes,
  describeNodeFilterContext,
  distinctSorted,
  initialCreateForm,
  isBindingConflictNode,
  isPendingOnboardingNode,
  mergeNonMetadataNodeRecord,
  parseLabels,
  parseMultiValue,
  pickTopNodeEvidence,
  runtimeAttentionFilter,
} from './nodes/nodeHelpers'
import type {
  FocusRestoreRequest,
  NodeFilterState,
  NodeListView,
  NodeRuntimeAction,
  PendingNodeConfirmation,
} from './nodes/types'

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

export function NodesPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [nodes, setNodes] = useState<NodeRecord[]>([])
  const [nodeListView, setNodeListView] = useState<NodeListView>('all')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [labelInput, setLabelInput] = useState('')
  const [createForm, setCreateForm] = useState<CreateNodeInput>(initialCreateForm)
  const [runtimeBusyNodeId, setRuntimeBusyNodeId] = useState<string | null>(null)
  const [runtimeErrors, setRuntimeErrors] = useState<Record<string, string>>({})
  const [editingLabelNodeId, setEditingLabelNodeId] = useState<string | null>(null)
  const [labelDraft, setLabelDraft] = useState('')
  const [groupDraft, setGroupDraft] = useState('')
  const [metadataBusyNodeId, setMetadataBusyNodeId] = useState<string | null>(null)
  const [metadataErrors, setMetadataErrors] = useState<Record<string, string>>({})
  const [pendingConfirmation, setPendingConfirmation] = useState<PendingNodeConfirmation | null>(null)
  const actionButtonRefs = useRef<Record<string, HTMLButtonElement | null>>({})
  const pendingFocusRestoreRef = useRef<FocusRestoreRequest | null>(null)
  const [sparklines, setSparklines] = useState<NodeSparklinesResponse | null>(null)
  const [selectAll, setSelectAll] = useState(false)
  const [batchSubmitting, setBatchSubmitting] = useState(false)
  const [pendingBatchAction, setPendingBatchAction] = useState<string | null>(null)
  const [batchError, setBatchError] = useState<string | null>(null)
  const [commandOpen, setCommandOpen] = useState(false)
  const [commandID, setCommandID] = useState('')
  const [sortState, setSortState] = useState<DataTableSortState | null>(null)
  const [showTrends, setShowTrends] = useState(true)
  const [autoRefresh, setAutoRefresh] = useState<AutoRefreshOption>(null)
  const [compareSet, setCompareSet] = useState<Set<string>>(new Set())

  function resetCreateFlow() {
    setCreateError(null)
    setLabelInput('')
    setCreateForm(initialCreateForm)
  }

  function refreshNodes() {
    listNodes()
      .then((result) => {
        setNodes(result)
      })
      .catch(() => {
        // silent refresh — keep old data on error
      })
  }

  useEffect(() => {
    let cancelled = false
    listNodes()
      .then((result) => {
        if (cancelled) return
        setNodes(result)
        setLoading(false)
      })
      .catch((value: unknown) => {
        if (cancelled) return
        setError(value instanceof ApiError ? value.message : '加载节点列表失败')
        setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [])

  useAutoRefresh(autoRefresh, refreshNodes)

  useEffect(() => {
    let cancelled = false
    listNodeSparklines(['cpu_usage_pct', 'mem_used_pct', 'disk_used_pct'])
      .then((data) => {
        if (!cancelled) setSparklines(data)
      })
      .catch(() => {}) // silent fail
    return () => {
      cancelled = true
    }
  }, [])

  function updateField<K extends keyof CreateNodeInput>(field: K, value: CreateNodeInput[K]) {
    setCreateForm((current) => ({ ...current, [field]: value }))
  }

  function queueFocusRestore(nodeId: string, preferredAction: NodeRuntimeAction) {
    pendingFocusRestoreRef.current = { nodeId, preferredAction }
  }

  useEffect(() => {
    if (pendingConfirmation) return

    const request = pendingFocusRestoreRef.current
    if (!request) return

    const preferred = actionButtonRefs.current[actionButtonKey(request.nodeId, request.preferredAction)]
    const fallback =
      request.preferredAction === 'pause'
        ? actionButtonRefs.current[actionButtonKey(request.nodeId, 'resume')]
        : null
    const target = [preferred, fallback].find((element) => element?.isConnected)

    target?.focus()
    pendingFocusRestoreRef.current = null
  }, [nodes, pendingConfirmation])

  async function handleRuntimeAction(
    node: NodeRecord,
    action: NodeRuntimeAction,
    confirmed = false,
  ) {
    if (action === 'pause' && !confirmed) {
      setPendingConfirmation({ nodeId: node.node_id, action })
      return
    }

    setRuntimeBusyNodeId(node.node_id)
    setRuntimeErrors((current) => {
      if (!current[node.node_id]) return current
      const next = { ...current }
      delete next[node.node_id]
      return next
    })

    try {
      const updated =
        action === 'enter-maintenance'
          ? await enterNodeMaintenance(node.node_id)
          : action === 'exit-maintenance'
            ? await exitNodeMaintenance(node.node_id)
            : action === 'pause'
              ? await pauseNodeMonitoring(node.node_id)
              : await resumeNodeMonitoring(node.node_id)
      setNodes((current) =>
        current.map((item) =>
          item.node_id === updated.node_id ? mergeNonMetadataNodeRecord(item, updated) : item,
        ),
      )
      queueFocusRestore(updated.node_id, action)
      setPendingConfirmation((current) =>
        current?.nodeId === updated.node_id ? null : current,
      )
    } catch (runtimeError) {
      setRuntimeErrors((current) => ({
        ...current,
        [node.node_id]: describeError(runtimeError, '节点运行控制操作失败'),
      }))
    } finally {
      setRuntimeBusyNodeId((current) => (current === node.node_id ? null : current))
    }
  }

  async function issueTokenAndEnterOnboarding(node: NodeRecord) {
    try {
      const issue = await issueNodeEnrollmentToken(node.node_id)
      setOnboardingTokenCache(node.node_id, issue)
      setCreateOpen(false)
      resetCreateFlow()
      navigate(`/nodes/${node.node_id}/onboarding`)
    } catch (issueError) {
      setCreateOpen(false)
      resetCreateFlow()
      navigate(`/nodes/${node.node_id}/onboarding`, {
        state: {
          tokenIssueError: `接入 Token 生成失败：${describeError(issueError, '请稍后重试')}`,
        },
      })
    }
  }

  async function handleSaveLabels(node: NodeRecord) {
    setMetadataBusyNodeId(node.node_id)
    setMetadataErrors((current) => {
      if (!current[node.node_id]) return current
      const next = { ...current }
      delete next[node.node_id]
      return next
    })

    try {
      const updated = await updateNodeMetadata(
        node.node_id,
        {
          group: groupDraft.trim() || undefined,
          labels: parseLabels(labelDraft),
          note: node.note,
        },
        {
          expectedUpdatedAt: node.updated_at,
        },
      )
      setNodes((current) =>
        current.map((item) =>
          item.node_id === updated.node_id
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
      setEditingLabelNodeId((current) => (current === node.node_id ? null : current))
      setLabelDraft('')
      setGroupDraft('')
    } catch (metadataError) {
      setMetadataErrors((current) => ({
        ...current,
        [node.node_id]: describeError(metadataError, '标签更新失败'),
      }))
    } finally {
      setMetadataBusyNodeId((current) => (current === node.node_id ? null : current))
    }
  }

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreateSubmitting(true)
    setCreateError(null)

    const payload: CreateNodeInput = {
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
      const node = await createNode(payload)
      setNodes((current) => {
        const withoutCreated = current.filter((item) => item.node_id !== node.node_id)
        return [node, ...withoutCreated]
      })
      await issueTokenAndEnterOnboarding(node)
    } catch (submitError) {
      setCreateError(describeError(submitError, '创建节点失败'))
    } finally {
      setCreateSubmitting(false)
    }
  }

  const bindingConflictNodes = useMemo(
    () => nodes.filter(isBindingConflictNode),
    [nodes],
  )
  const abnormalNodeCount = useMemo(() => countAbnormalNodes(nodes), [nodes])
  const pendingOnboardingNodeCount = useMemo(() => countPendingOnboardingNodes(nodes), [nodes])
  const maintenanceOrPausedNodeCount = useMemo(() => countMaintenanceOrPausedNodes(nodes), [nodes])
  const baseNodes = nodeListView === 'binding-conflict' ? bindingConflictNodes : nodes

  const filterState: NodeFilterState = useMemo(
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
      distinctSorted(nodes.map((node) => node.region)).map((value) => ({
        value,
        label: value,
      })),
    [nodes],
  )

  const cityOptions = useMemo(
    () =>
      distinctSorted(nodes.map((node) => node.city)).map((value) => ({
        value,
        label: value,
      })),
    [nodes],
  )

  const providerOptions = useMemo(
    () =>
      distinctSorted(nodes.map((node) => node.provider)).map((value) => ({
        value,
        label: value,
      })),
    [nodes],
  )

  const groupOptions = useMemo(
    () =>
      distinctSorted(nodes.map((node) => node.group).filter(Boolean)).map((value) => ({
        value,
        label: value,
      })),
    [nodes],
  )

  const labelOptions = useMemo(
    () =>
      distinctSorted(nodes.flatMap((node) => node.labels)).map((value) => ({
        value,
        label: value,
      })),
    [nodes],
  )

  const filteredNodes = useMemo(() => {
    return baseNodes.filter((node) => {
      if (filterState.group && node.group !== filterState.group) return false
      if (filterState.region && node.region !== filterState.region) return false
      if (filterState.city && node.city !== filterState.city) return false
      if (filterState.provider && node.provider !== filterState.provider) return false
      if (filterState.lifecycle && node.lifecycle_status !== filterState.lifecycle) return false
      if (filterState.runStatus && node.monitoring_status !== filterState.runStatus) return false
      if (filterState.health && node.current_health_status !== filterState.health) return false
      if (filterState.labels.length > 0) {
        const hasAll = filterState.labels.every((label) => node.labels.includes(label))
        if (!hasAll) return false
      }
      if (filterState.abnormal && node.current_health_status === '正常') return false
      if (filterState.onboardingPending && !isPendingOnboardingNode(node)) return false
      return true
    })
  }, [baseNodes, filterState])

  function handleSortChange(key: string) {
    setSortState((current) => {
      if (current?.key === key) {
        const nextDir = current.direction === 'asc' ? 'desc' : 'asc'
        return { key, direction: nextDir }
      }
      return { key, direction: 'asc' }
    })
  }

  const sortedFilteredNodes = useMemo(() => {
    if (!sortState) return filteredNodes
    const sorted = [...filteredNodes]
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
  }, [filteredNodes, sortState])

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

  const filterContext = useMemo(() => describeNodeFilterContext(filterState), [filterState])
  const topEvidence = useMemo(
    () => pickTopNodeEvidence(sortedFilteredNodes),
    [sortedFilteredNodes],
  )
  const displayedAbnormalNodeCount = useMemo(
    () => countAbnormalNodes(sortedFilteredNodes),
    [sortedFilteredNodes],
  )
  const displayedPendingOnboardingNodeCount = useMemo(
    () => countPendingOnboardingNodes(sortedFilteredNodes),
    [sortedFilteredNodes],
  )
  const displayedMaintenanceOrPausedNodeCount = useMemo(
    () => countMaintenanceOrPausedNodes(sortedFilteredNodes),
    [sortedFilteredNodes],
  )
  const evidenceLead = useMemo(
    () =>
      buildNodeEvidenceLead({
        totalNodeCount: nodes.length,
        displayedNodeCount: sortedFilteredNodes.length,
        abnormalNodeCount: displayedAbnormalNodeCount,
        pendingOnboardingNodeCount: displayedPendingOnboardingNodeCount,
        maintenanceOrPausedNodeCount: displayedMaintenanceOrPausedNodeCount,
        hasActiveFilters,
      }),
    [
      nodes.length,
      sortedFilteredNodes.length,
      displayedAbnormalNodeCount,
      displayedPendingOnboardingNodeCount,
      displayedMaintenanceOrPausedNodeCount,
      hasActiveFilters,
    ],
  )

  async function executeBatchAction(action: string) {
    if (action === 'pause') {
      setPendingBatchAction('pause')
      return
    }
    setBatchSubmitting(true)
    setBatchError(null)
    const nodeIDs = filteredNodes.map((node) => node.node_id)
    try {
      const res = await postNodeBatch(nodeIDs, action)
      const failed = res.results.filter((result) => !result.ok)
      if (failed.length > 0) {
        setBatchError(`${failed.length}/${nodeIDs.length} 个节点失败`)
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
    const nodeIDs = filteredNodes.map((node) => node.node_id)
    try {
      const res = await postNodeBatch(nodeIDs, 'pause')
      const failed = res.results.filter((result) => !result.ok)
      if (failed.length > 0) {
        setBatchError(`${failed.length}/${nodeIDs.length} 个节点失败`)
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
    const nodeIDs = filteredNodes.map((node) => node.node_id)
    let failCount = 0
    for (const nodeID of nodeIDs) {
      try {
        await postNodeAction(nodeID, commandID.trim())
      } catch {
        failCount++
      }
    }
    setBatchSubmitting(false)
    setSelectAll(false)
    if (failCount > 0) {
      setBatchError(`${nodeIDs.length - failCount} 个节点已下发，${failCount} 个失败，等待 agent 执行`)
    }
    setCommandID('')
  }

  if (loading) {
    return <PageState kind="loading" title="正在加载节点列表…" />
  }

  if (error) {
    return (
      <PageState
        kind="error"
        eyebrow="节点"
        title="节点列表不可用"
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

  function setSingleFilter(
    key: 'group' | 'region' | 'city' | 'provider' | 'lifecycle' | 'run_status' | 'health',
    value: string | null,
  ) {
    updateSearchParam(key, value)
  }

  function setMultiFilter(key: 'labels', values: string[]) {
    updateSearchParam(key, values.length === 0 ? null : values.join(','))
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

  const viewTabs = [
    { value: 'all' as const, label: '全部节点', count: nodes.length },
    {
      value: 'binding-conflict' as const,
      label: '绑定异常',
      count: bindingConflictNodes.length,
    },
  ]

  function toggleCompare(nodeId: string) {
    setCompareSet((current) => {
      const next = new Set(current)
      if (next.has(nodeId)) {
        next.delete(nodeId)
      } else if (next.size < 2) {
        next.add(nodeId)
      } else {
        return current // ignore if already 2 selected and trying a 3rd
      }
      return next
    })
  }

  function cancelLabelEdit(node: NodeRecord) {
    setEditingLabelNodeId((current) =>
      current === node.node_id ? null : current,
    )
    setLabelDraft('')
    setGroupDraft('')
    setMetadataErrors((current) => {
      if (!current[node.node_id]) return current
      const next = { ...current }
      delete next[node.node_id]
      return next
    })
  }

  function startLabelEdit(node: NodeRecord) {
    if (metadataBusyNodeId !== null) return
    setEditingLabelNodeId(node.node_id)
    setLabelDraft(node.labels.join(', '))
    setGroupDraft(node.group || '')
    setMetadataErrors((current) => {
      if (!current[node.node_id]) return current
      const next = { ...current }
      delete next[node.node_id]
      return next
    })
  }

  const columns = buildNodesTableColumns({
    compareSet,
    sparklines,
    editingLabelNodeId,
    labelDraft,
    groupDraft,
    metadataBusyNodeId,
    metadataErrors,
    runtimeBusyNodeId,
    actionButtonRefs,
    onToggleCompare: toggleCompare,
    onLabelDraftChange: setLabelDraft,
    onGroupDraftChange: setGroupDraft,
    onSaveLabels: (node) => void handleSaveLabels(node),
    onCancelLabels: cancelLabelEdit,
    onStartLabelEdit: startLabelEdit,
    onRuntimeAction: (node, action) => void handleRuntimeAction(node, action),
    onQueueFocusRestore: queueFocusRestore,
  })

  function shouldNavigateOnRowClick(node: NodeRecord): boolean {
    // Block navigation while the row is in edit mode or has a pending pause confirmation.
    if (editingLabelNodeId === node.node_id) return false
    if (pendingConfirmation?.nodeId === node.node_id) return false
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
    <section className="page-stack nodes-page">
      <NodesHero
        totalNodeCount={nodes.length}
        abnormalNodeCount={abnormalNodeCount}
        pendingOnboardingNodeCount={pendingOnboardingNodeCount}
        maintenanceOrPausedNodeCount={maintenanceOrPausedNodeCount}
        onAbnormalClick={() => setAbnormalFilter(abnormalNodeCount > 0)}
        onOnboardingClick={() => setOnboardingFilter(pendingOnboardingNodeCount > 0)}
        onRuntimeAttentionClick={() => setSingleFilter('run_status', runtimeAttentionFilter(nodes))}
        onCreateClick={toggleCreateDrawer}
      />

      <NodesSupportSurface
        totalNodeCount={nodes.length}
        displayedNodeCount={sortedFilteredNodes.length}
        abnormalNodeCount={abnormalNodeCount}
        pendingOnboardingNodeCount={pendingOnboardingNodeCount}
        maintenanceOrPausedNodeCount={maintenanceOrPausedNodeCount}
        evidenceLead={evidenceLead}
        topEvidence={topEvidence}
        filterContext={filterContext}
        hasActiveFilters={hasActiveFilters}
        onAbnormalClick={() => setAbnormalFilter(abnormalNodeCount > 0)}
        onOnboardingClick={() => setOnboardingFilter(pendingOnboardingNodeCount > 0)}
        onRuntimeAttentionClick={() =>
          setSingleFilter('run_status', runtimeAttentionFilter(nodes))
        }
        onClearFilters={clearAllFilters}
        onCreateClick={toggleCreateDrawer}
      />

      <CreateNodeDrawer
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

      <NodesToolbar
        viewTabs={viewTabs}
        nodeListView={nodeListView}
        displayedCount={sortedFilteredNodes.length}
        baseCount={baseNodes.length}
        showTrends={showTrends}
        compareSet={compareSet}
        autoRefresh={autoRefresh}
        onNodeListViewChange={setNodeListView}
        onShowTrendsChange={setShowTrends}
        onAutoRefreshChange={setAutoRefresh}
      />

      <NodesListSection
        nodeListView={nodeListView}
        baseNodes={baseNodes}
        nodes={sortedFilteredNodes}
        columns={columns}
        showTrends={showTrends}
        sortState={sortState}
        hasActiveFilters={hasActiveFilters}
        filterState={filterState}
        groupOptions={groupOptions}
        regionOptions={regionOptions}
        cityOptions={cityOptions}
        providerOptions={providerOptions}
        labelOptions={labelOptions}
        selectAll={selectAll}
        batchSubmitting={batchSubmitting}
        batchError={batchError}
        commandOpen={commandOpen}
        commandID={commandID}
        pendingBatchAction={pendingBatchAction}
        runtimeErrors={runtimeErrors}
        pendingConfirmation={pendingConfirmation}
        runtimeBusyNodeId={runtimeBusyNodeId}
        onClearAllFilters={clearAllFilters}
        onSingleFilterChange={setSingleFilter}
        onMultiFilterChange={setMultiFilter}
        onAbnormalFilterChange={setAbnormalFilter}
        onOnboardingFilterChange={setOnboardingFilter}
        onSelectAllChange={setSelectAll}
        onBatchAction={(action) => void executeBatchAction(action)}
        onCommandOpenChange={setCommandOpen}
        onCommandIDChange={setCommandID}
        onExecuteBatchCommand={() => void executeBatchCommand()}
        onConfirmBatchPause={() => void executeBatchPauseConfirmed()}
        onCancelBatchPause={() => setPendingBatchAction(null)}
        onSortChange={handleSortChange}
        onRowClick={(node) => {
          if (!shouldNavigateOnRowClick(node)) return
          navigate(`/nodes/${node.node_id}`)
        }}
        onConfirmPause={(node) => void handleRuntimeAction(node, 'pause', true)}
        onCancelPause={(node) => {
          queueFocusRestore(node.node_id, 'pause')
          setPendingConfirmation((current) =>
            current?.nodeId === node.node_id ? null : current,
          )
        }}
        onCreateNode={toggleCreateDrawer}
      />
    </section>
  )
}

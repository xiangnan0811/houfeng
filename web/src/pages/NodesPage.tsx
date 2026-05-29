import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { type DataTableSortState } from '../components/atoms'
import { PageState } from '../components/PageState'
import {
  ApiError,
  createNode,
  enterNodeMaintenance,
  exitNodeMaintenance,
  listNodes,
  listNodeSparklines,
  pauseNodeMonitoring,
  postNodeAction,
  postNodeBatch,
  resumeNodeMonitoring,
  retireNode,
  restoreRetiredNodeToObserving,
  updateNodeMetadata,
} from '../lib/api'
import type { CreateNodeInput, NodeRecord, NodeSparklinesResponse } from '../lib/types'
import { CreateNodeDrawer } from './nodes/CreateNodeDrawer'
import { NodesHero } from './nodes/NodesHero'
import { NodesListSection } from './nodes/NodesListSection'
import { buildNodesTableColumns } from './nodes/NodesTableColumns'
import { NodesToolbar } from './nodes/NodesToolbar'
import {
  actionButtonKey,
  countAbnormalNodes,
  countMaintenanceOrPausedNodes,
  countPendingOnboardingNodes,
  distinctSorted,
  initialCreateForm,
  isBindingConflictNode,
  isPendingOnboardingNode,
  isRuntimeAttentionNode,
  mergeNonMetadataNodeRecord,
  parseLabels,
  parseMultiValue,
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
  const [compareSet, setCompareSet] = useState<Set<string>>(new Set())

  function resetCreateFlow() {
    setCreateError(null)
    setLabelInput('')
    setCreateForm(initialCreateForm)
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
              : action === 'resume'
                ? await resumeNodeMonitoring(node.node_id)
                : action === 'retire'
                  ? await retireNode(node.node_id)
                  : await restoreRetiredNodeToObserving(node.node_id)
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
      setCreateOpen(false)
      resetCreateFlow()
      navigate(`/nodes/${node.node_id}/onboarding`)
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
  const runtimeAttentionNodes = useMemo(
    () => nodes.filter(isRuntimeAttentionNode),
    [nodes],
  )
  const abnormalNodeCount = useMemo(() => countAbnormalNodes(nodes), [nodes])
  const pendingOnboardingNodeCount = useMemo(() => countPendingOnboardingNodes(nodes), [nodes])
  const maintenanceOrPausedNodeCount = useMemo(() => countMaintenanceOrPausedNodes(nodes), [nodes])
  const baseNodes = nodeListView === 'binding-conflict'
    ? bindingConflictNodes
    : nodeListView === 'runtime-attention'
      ? runtimeAttentionNodes
      : nodes

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

  const providerOptions = useMemo(
    () =>
      distinctSorted(nodes.map((node) => node.provider)).map((value) => ({
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
      if (nodeListView === 'runtime-attention' && !isRuntimeAttentionNode(node)) return false
      if (filterState.health && node.current_health_status !== filterState.health) return false
      if (filterState.labels.length > 0) {
        const hasAll = filterState.labels.every((label) => node.labels.includes(label))
        if (!hasAll) return false
      }
      if (filterState.abnormal && node.current_health_status === '正常') return false
      if (filterState.onboardingPending && !isPendingOnboardingNode(node)) return false
      return true
    })
  }, [baseNodes, filterState, nodeListView])

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
    setNodeListView(
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
    <div className="page-stack animate-in">
      <NodesHero
        totalNodeCount={nodes.length}
        abnormalNodeCount={abnormalNodeCount}
        pendingOnboardingNodeCount={pendingOnboardingNodeCount}
        maintenanceOrPausedNodeCount={maintenanceOrPausedNodeCount}
        onAbnormalClick={() => setAbnormalFilter(abnormalNodeCount > 0)}
        onOnboardingClick={() => setOnboardingFilter(pendingOnboardingNodeCount > 0)}
        onRuntimeAttentionClick={() => applyQuickView('runtime-attention')}
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

      <div className="animate-in d2">
        <NodesToolbar
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

        <NodesListSection
          nodeListView={nodeListView}
          baseNodes={baseNodes}
          nodes={sortedFilteredNodes}
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
          runtimeBusyNodeId={runtimeBusyNodeId}
          onClearAllFilters={clearAllFilters}
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
      </div>
    </div>
  )
}

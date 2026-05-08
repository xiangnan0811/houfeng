import { type FormEvent, type ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { ActionConfirmationCard } from '../components/ActionConfirmationCard'
import { StatusBadge } from '../components/StatusBadge'
import {
  Button,
  DataTable,
  type DataTableColumn,
  type DataTableSortState,
  Drawer,
  Hostname,
  type HealthState,
  MonoDigits,
  Sparkline,
  StatusGlyph,
  Tabs,
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
import { formatPercent } from '../lib/format'
import { DEFAULT_THRESHOLDS } from '../config/thresholds'
import {
  AUTO_REFRESH_OPTIONS,
  type AutoRefreshOption,
  useAutoRefresh,
} from '../lib/useAutoRefresh'

const NODE_LIFECYCLE_FILTER_OPTIONS = [
  { value: '待接入', label: '待接入' },
  { value: '在用', label: '在用' },
  { value: '观察中', label: '观察中' },
  { value: '不续费', label: '不续费' },
  { value: '已退役', label: '已退役' },
] as const

const NODE_RUN_STATUS_FILTER_OPTIONS = [
  { value: '启用', label: '启用' },
  { value: '暂停', label: '暂停' },
  { value: '维护中', label: '维护中' },
] as const

const NODE_HEALTH_STATUS_FILTER_OPTIONS = [
  { value: '正常', label: '正常' },
  { value: '关注', label: '关注' },
  { value: '告警', label: '告警' },
  { value: '严重', label: '严重' },
] as const

type NodeFilterState = {
  group: string | null
  region: string | null
  city: string | null
  provider: string | null
  lifecycle: string | null
  runStatus: string | null
  health: string | null
  labels: string[]
  abnormal: boolean
  onboardingPending: boolean
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
    if (!value) continue
    if (!seen.has(value)) {
      seen.add(value)
      out.push(value)
    }
  }
  return out.sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
}

const initialCreateForm: CreateNodeInput = {
  display_name: '',
  group: '',
  region: '',
  city: '',
  provider: '',
  labels: [],
  note: '',
}

const NODE_BINDING_CONFLICT_STATUS = '指纹变更待确认'
const NODE_BINDING_UNBOUND_STATUS = '未绑定'
const NODE_BINDING_CONFLICT_SUMMARY = '等待绑定确认'

type NodeRuntimeAction = 'enter-maintenance' | 'exit-maintenance' | 'pause' | 'resume'
type NodeListView = 'all' | 'binding-conflict'
type PendingNodeConfirmation = {
  nodeId: string
  action: 'pause'
}

type FocusRestoreRequest = {
  nodeId: string
  preferredAction: NodeRuntimeAction
}

/** Map backend Chinese health-status into the StatusGlyph state vocabulary.
 *  Health is derived (正常/关注/告警/严重) per architecture-data-model §node;
 *  monitoring 维护中/暂停 visually outranks health for at-a-glance scanning. */
function nodeGlyphState(node: NodeRecord): HealthState {
  if (node.monitoring_status === '维护中') return 'maintenance'
  if (node.monitoring_status === '暂停') return 'offline'
  switch (node.current_health_status) {
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
  const result: string[] = []
  const seen = new Set<string>()

  for (const label of value.split(/[,，]/).map((item) => item.trim()).filter(Boolean)) {
    if (seen.has(label)) continue
    seen.add(label)
    result.push(label)
  }

  return result
}

function nodeRuntimeActions(node: NodeRecord): Array<{ action: NodeRuntimeAction; label: string }> {
  if (node.monitoring_status === '启用') {
    return [
      { action: 'enter-maintenance', label: '进入维护' },
      { action: 'pause', label: '暂停监控' },
    ]
  }

  if (node.monitoring_status === '维护中') {
    return [
      { action: 'exit-maintenance', label: '退出维护' },
      { action: 'pause', label: '暂停监控' },
    ]
  }

  if (node.monitoring_status === '暂停') {
    return [{ action: 'resume', label: '恢复监控' }]
  }

  return []
}

function isBindingConflictNode(node: NodeRecord) {
  return node.binding_status === NODE_BINDING_CONFLICT_STATUS
}

function isPendingOnboardingNode(node: NodeRecord) {
  return (
    node.lifecycle_status === '待接入' ||
    node.binding_status === NODE_BINDING_UNBOUND_STATUS ||
    node.binding_status === NODE_BINDING_CONFLICT_STATUS
  )
}

function countAbnormalNodes(nodes: NodeRecord[]) {
  return nodes.filter((node) => node.current_health_status !== '正常').length
}

function countPendingOnboardingNodes(nodes: NodeRecord[]) {
  return nodes.filter(isPendingOnboardingNode).length
}

function countMaintenanceOrPausedNodes(nodes: NodeRecord[]) {
  return nodes.filter(
    (node) => node.monitoring_status === '维护中' || node.monitoring_status === '暂停',
  ).length
}

function runtimeAttentionFilter(nodes: NodeRecord[]): string | null {
  if (nodes.some((node) => node.monitoring_status === '维护中')) return '维护中'
  if (nodes.some((node) => node.monitoring_status === '暂停')) return '暂停'
  return null
}

function pauseConfirmationCurrent(node: NodeRecord) {
  return node.monitoring_status === '维护中'
    ? '当前：监控运行状态为维护中。'
    : '当前：监控运行状态为启用。'
}

function mergeNonMetadataNodeRecord(current: NodeRecord, updated: NodeRecord): NodeRecord {
  return {
    ...updated,
    labels: current.labels,
    note: current.note,
  }
}

function renderLabelsCell(node: NodeRecord): ReactNode {
  if (node.labels.length === 0) return <span className="empty-inline">—</span>
  const visible = node.labels.slice(0, 3)
  const overflow = node.labels.length - visible.length
  return (
    <span className="nodes-table__labels">
      {visible.join(' · ')}
      {overflow > 0 ? (
        <span className="nodes-table__labels-more"> +{overflow}</span>
      ) : null}
    </span>
  )
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
      .then(data => { if (!cancelled) setSparklines(data) })
      .catch(() => {}) // silent fail
    return () => { cancelled = true }
  }, [])

  function updateField<K extends keyof CreateNodeInput>(field: K, value: CreateNodeInput[K]) {
    setCreateForm((current) => ({ ...current, [field]: value }))
  }

  function actionButtonKey(nodeId: string, action: NodeRuntimeAction) {
    return `${nodeId}:${action}`
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

  async function executeBatchAction(action: string) {
    if (action === 'pause') {
      setPendingBatchAction('pause')
      return
    }
    setBatchSubmitting(true)
    setBatchError(null)
    const nodeIDs = filteredNodes.map((n) => n.node_id)
    try {
      const res = await postNodeBatch(nodeIDs, action)
      const failed = res.results.filter((r) => !r.ok)
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
    const nodeIDs = filteredNodes.map((n) => n.node_id)
    try {
      const res = await postNodeBatch(nodeIDs, 'pause')
      const failed = res.results.filter((r) => !r.ok)
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
    const nodeIDs = filteredNodes.map((n) => n.node_id)
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
    return <section className="page-panel">正在加载节点列表…</section>
  }

  if (error) {
    return (
      <section className="page-panel">
        <h2 className="page-panel__title">节点</h2>
        <p className="page-panel__description">{error}</p>
      </section>
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

  const columns: DataTableColumn<NodeRecord>[] = [
    {
      key: 'compare',
      label: '',
      width: 28,
      align: 'center',
      render: (node) => {
        const checked = compareSet.has(node.node_id)
        const disabled = !checked && compareSet.size >= 2
        return (
          <input
            type="checkbox"
            className="nodes-table__compare-check"
            checked={checked}
            disabled={disabled}
            onChange={() => toggleCompare(node.node_id)}
            onClick={(event) => event.stopPropagation()}
            aria-label={`选择 ${node.display_name} 进行对比`}
          />
        )
      },
    },
    {
      key: 'glyph',
      label: '',
      width: 32,
      align: 'center',
      render: (node) => (
        <StatusGlyph
          state={nodeGlyphState(node)}
          size="md"
          ariaLabel={`${node.display_name} 健康 ${node.current_health_status}`}
        />
      ),
    },
    {
      key: 'identity',
      label: '节点',
      sortable: true,
      render: (node) => (
        <div className="nodes-table__identity">
          <Hostname truncate maxChars={14} className="nodes-table__id">
            {node.node_id}
          </Hostname>
          <Link
            className="text-link nodes-table__name"
            to={`/nodes/${node.node_id}`}
            onClick={(event) => event.stopPropagation()}
          >
            {node.display_name}
          </Link>
          <span className="nodes-table__freshness">
            心跳 <Timestamp value={node.last_heartbeat_at} mode="relative" />
            {node.last_sync_at ? <> · 同步 <Timestamp value={node.last_sync_at} mode="relative" /></> : null}
          </span>
        </div>
      ),
    },
    {
      key: 'location',
      label: '位置',
      sortable: true,
      render: (node) => (
        <span className="nodes-table__location">
          {[node.group, node.region, node.city, node.provider].filter(Boolean).join(' · ') || '—'}
        </span>
      ),
    },
    {
      key: 'labels',
      label: '标签',
      render: (node) => {
        if (editingLabelNodeId === node.node_id) {
          return (
            <div
              className="nodes-table__label-editor"
              onClick={(event) => event.stopPropagation()}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.stopPropagation()
                }
              }}
            >
              <label className="nodes-table__label-editor-field">
                <span className="visually-hidden">Group</span>
                <input
                  name={`group-${node.node_id}`}
                  value={groupDraft}
                  onChange={(event) => setGroupDraft(event.target.value)}
                  aria-label="Group"
                  placeholder="Group"
                />
              </label>
              <label className="nodes-table__label-editor-field">
                <span className="visually-hidden">标签</span>
                <input
                  name={`labels-${node.node_id}`}
                  value={labelDraft}
                  onChange={(event) => setLabelDraft(event.target.value)}
                  aria-label="标签"
                />
              </label>
              <div className="nodes-table__label-editor-actions">
                <Button
                  size="sm"
                  variant="primary"
                  disabled={metadataBusyNodeId === node.node_id}
                  onClick={() => void handleSaveLabels(node)}
                >
                  {metadataBusyNodeId === node.node_id ? '正在保存…' : '保存标签'}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={metadataBusyNodeId === node.node_id}
                  onClick={() => {
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
                  }}
                >
                  取消
                </Button>
              </div>
              {metadataErrors[node.node_id] ? (
                <p className="nodes-table__inline-error" role="alert">
                  {metadataErrors[node.node_id]}
                </p>
              ) : null}
            </div>
          )
        }
        return renderLabelsCell(node)
      },
    },
    {
      key: 'issue',
      label: '当前主问题',
      sortable: true,
      render: (node) => {
        const summary = isBindingConflictNode(node)
          ? NODE_BINDING_CONFLICT_SUMMARY
          : node.current_primary_issue_summary || '暂无明显异常'
        return (
          <div className="nodes-table__issue">
            <MonoDigits className="nodes-table__issue-count">
              {node.current_active_incident_count}
            </MonoDigits>
            <span className="nodes-table__issue-summary">{summary}</span>
            {isBindingConflictNode(node) ? (
              <StatusBadge label={NODE_BINDING_CONFLICT_STATUS} />
            ) : null}
          </div>
        )
      },
    },
    {
      key: 'trends',
      label: '近 24h',
      cellClassName: 'nodes-table__trends',
      render: (node) => {
        const series = sparklines?.nodes?.[node.node_id]
        if (!series) {
          return <span className="nodes-table__trends-empty">—</span>
        }
        const cpu = series.cpu_usage_pct
        const mem = series.mem_used_pct
        const disk = series.disk_used_pct
        const latestCpu = cpu?.[cpu.length - 1] ?? null
        const latestMem = mem?.[mem.length - 1] ?? null
        const latestDisk = disk?.[disk.length - 1] ?? null

        const t = DEFAULT_THRESHOLDS
        const cpuTone = !latestCpu ? 'default' : latestCpu >= t.cpu.critical ? 'critical' : latestCpu >= t.cpu.notice ? 'alert' : 'accent'
        const memTone = !latestMem ? 'default' : latestMem >= t.mem.critical ? 'critical' : latestMem >= t.mem.notice ? 'alert' : 'accent'
        const diskTone = !latestDisk ? 'default' : latestDisk >= t.disk.critical ? 'critical' : latestDisk >= t.disk.notice ? 'alert' : 'accent'

        return (
          <span className="nodes-table__trend-strip">
            <span className="nodes-table__trend-item">
              <span className="nodes-table__trend-value">
                {latestCpu != null ? <MonoDigits>{formatPercent(latestCpu)}</MonoDigits> : '—'}
              </span>
              {cpu && cpu.length > 0 ? (
                <Sparkline values={cpu.filter((v): v is number => v != null)} tone={cpuTone} width={64} height={14} />
              ) : (
                <span className="nodes-table__trends-empty">—</span>
              )}
            </span>
            <span className="nodes-table__trend-item">
              <span className="nodes-table__trend-value">
                {latestMem != null ? <MonoDigits>{formatPercent(latestMem)}</MonoDigits> : '—'}
              </span>
              {mem && mem.length > 0 ? (
                <Sparkline values={mem.filter((v): v is number => v != null)} tone={memTone} width={64} height={14} />
              ) : (
                <span className="nodes-table__trends-empty">—</span>
              )}
            </span>
            <span className="nodes-table__trend-item">
              <span className="nodes-table__trend-value">
                {latestDisk != null ? <MonoDigits>{formatPercent(latestDisk)}</MonoDigits> : '—'}
              </span>
              {disk && disk.length > 0 ? (
                <Sparkline values={disk.filter((v): v is number => v != null)} tone={diskTone} width={64} height={14} />
              ) : (
                <span className="nodes-table__trends-empty">—</span>
              )}
            </span>
          </span>
        )
      },
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      render: (node) => {
        const actions = nodeRuntimeActions(node)
        return (
          <div
            className="nodes-table__actions"
            onClick={(event) => event.stopPropagation()}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.stopPropagation()
              }
            }}
          >
            {editingLabelNodeId === node.node_id ? null : (
              <Button
                size="sm"
                variant="ghost"
                disabled={metadataBusyNodeId !== null}
                onClick={() => {
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
                }}
              >
                快速编辑标签
              </Button>
            )}
            <Link
              className="btn btn--ghost btn--sm"
              to={`/nodes/${node.node_id}/onboarding`}
              onClick={(event) => event.stopPropagation()}
            >
              接入工作台
            </Link>
            {actions.map(({ action, label }) => (
              <button
                key={action}
                type="button"
                className="btn btn--ghost btn--sm"
                ref={(element) => {
                  actionButtonRefs.current[actionButtonKey(node.node_id, action)] = element
                }}
                disabled={runtimeBusyNodeId === node.node_id}
                onClick={() => {
                  if (action === 'pause') {
                    queueFocusRestore(node.node_id, action)
                  }
                  void handleRuntimeAction(node, action)
                }}
              >
                {label}
              </button>
            ))}
          </div>
        )
      },
    },
  ]

  function shouldNavigateOnRowClick(node: NodeRecord): boolean {
    // Block navigation while the row is in edit mode or has a pending pause confirmation.
    if (editingLabelNodeId === node.node_id) return false
    if (pendingConfirmation?.nodeId === node.node_id) return false
    return true
  }

  return (
    <section className="page-stack nodes-page">
      <header className="section-heading section-heading--inline nodes-hero">
        <div>
          <p className="section-heading__eyebrow">节点</p>
          <h2 className="section-heading__title">节点列表</h2>
          <p className="section-heading__description">
            按健康风险、接入状态和最近运行事实管理服务器节点。
          </p>
        </div>
        <div className="nodes-hero__aside" aria-label="节点库存摘要">
          <div className="nodes-hero__stats">
            <Link className="nodes-hero-stat nodes-hero-stat--normal" to="/nodes" aria-label={`全部节点：${nodes.length}`}>
              <span>全部</span>
              <strong>
                <MonoDigits>{nodes.length}</MonoDigits>
              </strong>
            </Link>
            <button
              type="button"
              className="nodes-hero-stat nodes-hero-stat--alert"
              aria-label={`异常节点：${abnormalNodeCount}`}
              onClick={() => setAbnormalFilter(abnormalNodeCount > 0)}
            >
              <span>异常</span>
              <strong>
                <MonoDigits>{abnormalNodeCount}</MonoDigits>
              </strong>
            </button>
            <button
              type="button"
              className="nodes-hero-stat nodes-hero-stat--notice"
              aria-label={`待接入节点：${pendingOnboardingNodeCount}`}
              onClick={() => setOnboardingFilter(pendingOnboardingNodeCount > 0)}
            >
              <span>待接入</span>
              <strong>
                <MonoDigits>{pendingOnboardingNodeCount}</MonoDigits>
              </strong>
            </button>
            <button
              type="button"
              className="nodes-hero-stat nodes-hero-stat--maintenance"
              aria-label={`维护或暂停节点：${maintenanceOrPausedNodeCount}`}
              onClick={() => {
                setSingleFilter('run_status', runtimeAttentionFilter(nodes))
              }}
            >
              <span>维护/暂停</span>
              <strong>
                <MonoDigits>{maintenanceOrPausedNodeCount}</MonoDigits>
              </strong>
            </button>
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
            新建节点
          </Button>
        </div>
      </header>

      <Drawer
        open={createOpen}
        onClose={() => {
          setCreateOpen(false)
          resetCreateFlow()
        }}
        title="节点创建"
        ariaLabel="创建节点表单"
      >
        <p className="page-panel__description">创建完成后将立即生成接入 Token，并跳转到节点接入准备页。</p>
        <form onSubmit={handleCreate}>
          <p>
            <label>
              显示名称
              <input
                name="display_name"
                value={createForm.display_name}
                onChange={(event) => updateField('display_name', event.target.value)}
                required
              />
            </label>
          </p>
          <p>
            <label>
              Group
              <input
                name="group"
                value={createForm.group}
                onChange={(event) => updateField('group', event.target.value)}
              />
            </label>
          </p>
          <p>
            <label>
              地区
              <input
                name="region"
                value={createForm.region}
                onChange={(event) => updateField('region', event.target.value)}
                required
              />
            </label>
          </p>
          <p>
            <label>
              城市
              <input
                name="city"
                value={createForm.city}
                onChange={(event) => updateField('city', event.target.value)}
                required
              />
            </label>
          </p>
          <p>
            <label>
              供应商
              <input
                name="provider"
                value={createForm.provider}
                onChange={(event) => updateField('provider', event.target.value)}
                required
              />
            </label>
          </p>
          <p>
            <label>
              生命周期状态固定为待接入
            </label>
          </p>
          <p>
            <label>
              标签
              <input
                name="labels"
                value={labelInput}
                onChange={(event) => setLabelInput(event.target.value)}
              />
            </label>
          </p>
          <p>
            <label>
              备注
              <textarea
                name="note"
                value={createForm.note}
                onChange={(event) => updateField('note', event.target.value)}
                rows={3}
              />
            </label>
          </p>
          {createError ? <p>{createError}</p> : null}
          <div>
            <button type="submit" disabled={createSubmitting}>
              {createSubmitting ? '正在创建…' : '创建并生成 Token'}
            </button>
          </div>
        </form>
      </Drawer>

      <div className="nodes-toolbar" aria-label="节点列表工具栏">
        <div className="nodes-toolbar__primary">
          <Tabs<NodeListView>
            variant="pill"
            items={viewTabs}
            value={nodeListView}
            onChange={setNodeListView}
          />
          <span className="nodes-toolbar__result">
            当前显示 <MonoDigits>{sortedFilteredNodes.length}</MonoDigits> / <MonoDigits>{baseNodes.length}</MonoDigits>
          </span>
        </div>
        <div className="nodes-toolbar__actions">
          <button
            type="button"
            className={`btn btn--ghost btn--sm ${!showTrends ? 'btn--active' : ''}`}
            onClick={() => setShowTrends((v) => !v)}
          >
            {showTrends ? '隐藏趋势' : '显示趋势'}
          </button>
          {compareSet.size === 2 ? (
            <Link
              className="btn btn--secondary btn--sm"
              to={`/nodes/compare?id=${[...compareSet].join('&id=')}`}
            >
              对比选中节点
            </Link>
          ) : (
            <span className="nodes-toolbar__hint">
              选择 2 个节点可对比
            </span>
          )}
          <label className="nodes-toolbar__refresh">
            <span>自动刷新</span>
            <select
              className="auto-refresh-select"
              value={autoRefresh == null ? '' : String(autoRefresh)}
              onChange={(e) => {
                const v = e.target.value
                setAutoRefresh(v === '' ? null : Number(v))
              }}
              aria-label="自动刷新间隔"
            >
              {AUTO_REFRESH_OPTIONS.map((opt) => (
                <option key={opt.label} value={opt.value == null ? '' : String(opt.value)}>
                  {opt.label}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>

      {baseNodes.length === 0 ? (
        <div className="empty-state">
          <h3>{nodeListView === 'binding-conflict' ? '没有绑定异常节点' : '暂无节点'}</h3>
          <p>
            {nodeListView === 'binding-conflict'
              ? '当前没有等待绑定确认的节点。'
              : '请先创建第一个节点。'}
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
                {filterState.region ? (
                  <FilterChip
                    label={`地区: ${filterState.region}`}
                    onRemove={() => setSingleFilter('region', null)}
                  />
                ) : null}
                {filterState.city ? (
                  <FilterChip
                    label={`城市: ${filterState.city}`}
                    onRemove={() => setSingleFilter('city', null)}
                  />
                ) : null}
                {filterState.provider ? (
                  <FilterChip
                    label={`供应商: ${filterState.provider}`}
                    onRemove={() => setSingleFilter('provider', null)}
                  />
                ) : null}
                {filterState.lifecycle ? (
                  <FilterChip
                    label={`生命周期: ${filterState.lifecycle}`}
                    onRemove={() => setSingleFilter('lifecycle', null)}
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
                {filterState.abnormal ? (
                  <FilterChip
                    label="仅看异常"
                    onRemove={() => setAbnormalFilter(false)}
                  />
                ) : null}
                {filterState.onboardingPending ? (
                  <FilterChip
                    label="待接入/绑定待处理"
                    onRemove={() => setOnboardingFilter(false)}
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
              label="地区"
              value={filterState.region}
              options={regionOptions}
              onChange={(value) => setSingleFilter('region', value)}
            />
            <FilterSelect
              label="城市"
              value={filterState.city}
              options={cityOptions}
              onChange={(value) => setSingleFilter('city', value)}
            />
            <FilterSelect
              label="供应商"
              value={filterState.provider}
              options={providerOptions}
              onChange={(value) => setSingleFilter('provider', value)}
            />
            <FilterSelect
              label="生命周期"
              value={filterState.lifecycle}
              options={NODE_LIFECYCLE_FILTER_OPTIONS}
              onChange={(value) => setSingleFilter('lifecycle', value)}
            />
            <FilterSelect
              label="运行状态"
              value={filterState.runStatus}
              options={NODE_RUN_STATUS_FILTER_OPTIONS}
              onChange={(value) => setSingleFilter('run_status', value)}
            />
            <FilterSelect
              label="健康状态"
              value={filterState.health}
              options={NODE_HEALTH_STATUS_FILTER_OPTIONS}
              onChange={(value) => setSingleFilter('health', value)}
            />
            <FilterMultiSelect
              label="标签"
              values={filterState.labels}
              options={labelOptions}
              onChange={(values) => setMultiFilter('labels', values)}
            />
            <FilterToggle
              label="仅看异常"
              checked={filterState.abnormal}
              onChange={setAbnormalFilter}
            />
            <FilterToggle
              label="待接入/绑定待处理"
              checked={filterState.onboardingPending}
              onChange={setOnboardingFilter}
            />
          </FilterBar>
          {hasActiveFilters && filteredNodes.length > 0 ? (
            <div className={`batch-bar${selectAll ? ' batch-bar--active' : ''}`}>
              <label className="batch-bar__toggle">
                <input
                  type="checkbox"
                  checked={selectAll}
                  onChange={(e) => setSelectAll(e.target.checked)}
                />
                全选 ({filteredNodes.length})
              </label>
              {selectAll ? (
                <div className="batch-bar__actions">
                  <button
                    className="btn btn--secondary btn--sm"
                    disabled={batchSubmitting}
                    onClick={() => executeBatchAction('enter-maintenance')}
                  >
                    进入维护
                  </button>
                  <button
                    className="btn btn--secondary btn--sm"
                    disabled={batchSubmitting}
                    onClick={() => executeBatchAction('exit-maintenance')}
                  >
                    退出维护
                  </button>
                  <button
                    className="btn btn--secondary btn--sm"
                    disabled={batchSubmitting}
                    onClick={() => executeBatchAction('pause')}
                  >
                    暂停监控
                  </button>
                  <button
                    className="btn btn--secondary btn--sm"
                    disabled={batchSubmitting}
                    onClick={() => executeBatchAction('resume')}
                  >
                    恢复监控
                  </button>
                  <button
                    className="btn btn--secondary btn--sm"
                    disabled={batchSubmitting}
                    onClick={() => setCommandOpen(true)}
                  >
                    执行命令…
                  </button>
                </div>
              ) : null}
              {batchError ? (
                <span className="batch-bar__error">{batchError}</span>
              ) : null}
              {batchSubmitting ? <span>批量操作中…</span> : null}
            </div>
          ) : null}
          {commandOpen ? (
            <div className="page-panel">
              <p className="page-panel__eyebrow">批量命令执行</p>
              <h3 className="page-panel__title">下发命令到已选节点</h3>
              <p className="page-panel__description">
                将对 {filteredNodes.length} 个节点下发命令。请输入命令 ID。
              </p>
              <p>
                <label>
                  命令 ID
                  <input
                    value={commandID}
                    onChange={(e) => setCommandID(e.target.value)}
                    placeholder="例如：whoami"
                  />
                </label>
              </p>
              <div style={{ display: 'flex', gap: 'var(--space-2)' }}>
                <button
                  className="btn btn--primary btn--md"
                  disabled={!commandID.trim() || batchSubmitting}
                  onClick={() => void executeBatchCommand()}
                >
                  下发命令
                </button>
                <button
                  className="btn btn--ghost btn--md"
                  onClick={() => {
                    setCommandOpen(false)
                    setCommandID('')
                  }}
                >
                  取消
                </button>
              </div>
            </div>
          ) : null}
          {pendingBatchAction === 'pause' ? (
            <ActionConfirmationCard
              title="确认批量暂停节点监控"
              current={`将对 ${filteredNodes.length} 个节点执行暂停操作。`}
              result="操作后：所有已选节点的监控运行状态变为暂停。"
              impact="会停止主机指标采集，并停止这些节点承担的探针执行。趋势图会从此开始出现数据空档。"
              unchanged="不会删除历史事件、观测记录或 agent 绑定关系。"
              confirmLabel="确认批量暂停监控"
              disabled={batchSubmitting}
              onConfirm={() => void executeBatchPauseConfirmed()}
              onCancel={() => setPendingBatchAction(null)}
            />
          ) : null}
          {sortedFilteredNodes.length === 0 ? (
            <div className="empty-state">
              <h3>没有匹配当前筛选的节点</h3>
              <p>请尝试调整筛选条件，或清空筛选恢复完整列表。</p>
              <p>
                <button type="button" onClick={clearAllFilters}>
                  清空筛选
                </button>
              </p>
            </div>
          ) : (
            <DataTable<NodeRecord>
              columns={showTrends ? columns : columns.filter((col) => col.key !== 'trends')}
              rows={sortedFilteredNodes}
              rowKey={(node) => node.node_id}
              density="compact"
              className="nodes-table"
              sortState={sortState}
              onSortChange={handleSortChange}
              onRowClick={(node) => {
                if (!shouldNavigateOnRowClick(node)) return
                navigate(`/nodes/${node.node_id}`)
              }}
            />
          )}

          {sortedFilteredNodes.map((node) => {
            const runtimeError = runtimeErrors[node.node_id]
            const showPauseConfirmation =
              pendingConfirmation?.nodeId === node.node_id &&
              pendingConfirmation.action === 'pause'
            if (!runtimeError && !showPauseConfirmation) return null
            return (
              <div key={`runtime-${node.node_id}`} className="nodes-table__row-overlay">
                {showPauseConfirmation ? (
                  <ActionConfirmationCard
                    title="确认暂停节点监控"
                    current={pauseConfirmationCurrent(node)}
                    result="操作后：监控运行状态变为暂停。"
                    impact="会停止主机指标采集，并停止该节点承担的探针执行。趋势图会从此开始出现数据空档。"
                    unchanged="不会删除历史事件、观测记录或 agent 绑定关系。"
                    confirmLabel="确认暂停监控"
                    disabled={runtimeBusyNodeId === node.node_id}
                    onConfirm={() => void handleRuntimeAction(node, 'pause', true)}
                    onCancel={() => {
                      queueFocusRestore(node.node_id, 'pause')
                      setPendingConfirmation((current) =>
                        current?.nodeId === node.node_id ? null : current,
                      )
                    }}
                  />
                ) : null}
                {runtimeError ? (
                  <p className="nodes-table__inline-error" role="alert">
                    {node.display_name}：{runtimeError}
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

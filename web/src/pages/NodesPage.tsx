import { type FormEvent, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { ActionConfirmationCard } from '../components/ActionConfirmationCard'
import { StatusBadge } from '../components/StatusBadge'
import {
  ApiError,
  enterNodeMaintenance,
  exitNodeMaintenance,
  issueNodeEnrollmentToken,
  listNodes,
  pauseNodeMonitoring,
  resumeNodeMonitoring,
  updateNodeMetadata,
} from '../lib/api'
import { formatDateTime, formatLabelList } from '../lib/format'
import { setOnboardingTokenCache } from '../lib/onboardingTokenCache'
import type { NodeRecord } from '../lib/types'

type CreateNodeInput = {
  display_name: string
  region: string
  city: string
  provider: string
  labels: string[]
  note: string
}

const initialCreateForm: CreateNodeInput = {
  display_name: '',
  region: '',
  city: '',
  provider: '',
  labels: [],
  note: '',
}

const NODE_BINDING_CONFLICT_STATUS = '指纹变更待确认'
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

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

async function createNode(input: CreateNodeInput) {
  const response = await fetch('/api/nodes', {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
    body: JSON.stringify({
      ...input,
      lifecycle_status: '待接入',
    }),
  })

  if (!response.ok) {
    let message = `Request failed: ${response.status}`
    const rawBody = await response.text()
    if (rawBody.trim()) {
      try {
        const errorBody = JSON.parse(rawBody) as { error?: string; message?: string }
        message = errorBody.error ?? errorBody.message ?? rawBody
      } catch {
        message = rawBody
      }
    }
    throw new ApiError(response.status, message)
  }

  return (await response.json()) as NodeRecord
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

export function NodesPage() {
  const navigate = useNavigate()
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
  const [metadataBusyNodeId, setMetadataBusyNodeId] = useState<string | null>(null)
  const [metadataErrors, setMetadataErrors] = useState<Record<string, string>>({})
  const [pendingConfirmation, setPendingConfirmation] = useState<PendingNodeConfirmation | null>(null)
  const actionButtonRefs = useRef<Record<string, HTMLButtonElement | null>>({})
  const pendingFocusRestoreRef = useRef<FocusRestoreRequest | null>(null)

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
                labels: updated.labels,
                note: updated.note,
                updated_at: updated.updated_at,
              }
            : item,
        ),
      )
      setEditingLabelNodeId((current) => (current === node.node_id ? null : current))
      setLabelDraft('')
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

  const bindingConflictNodes = nodes.filter(isBindingConflictNode)
  const visibleNodes = nodeListView === 'binding-conflict' ? bindingConflictNodes : nodes

  return (
    <section className="page-stack">
      <header className="section-heading">
        <div>
          <p className="section-heading__eyebrow">Nodes</p>
          <h2 className="section-heading__title">节点列表</h2>
          <p className="section-heading__description">
            当前以“当前问题优先、最近运行事实次之”的冻结 V1 层级展示节点状态。
          </p>
        </div>
        <button
          type="button"
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
        </button>
      </header>

      {createOpen ? (
        <section className="page-panel">
          <p className="page-panel__eyebrow">Node Create</p>
          <h3 className="page-panel__title">创建节点并进入接入工作台</h3>
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
        </section>
      ) : null}

      <section className="page-panel">
        <p className="page-panel__eyebrow">List View</p>
        <h3 className="page-panel__title">列表视图</h3>
        <div className="badge-row badge-row--wrap">
          <button
            type="button"
            aria-pressed={nodeListView === 'all'}
            onClick={() => setNodeListView('all')}
          >
            全部节点 {nodes.length}
          </button>
          <button
            type="button"
            aria-pressed={nodeListView === 'binding-conflict'}
            onClick={() => setNodeListView('binding-conflict')}
          >
            绑定异常 {bindingConflictNodes.length}
          </button>
        </div>
      </section>

      {visibleNodes.length === 0 ? (
        <div className="empty-state">
          <h3>{nodeListView === 'binding-conflict' ? '没有绑定异常节点' : '暂无节点'}</h3>
          <p>
            {nodeListView === 'binding-conflict'
              ? '当前没有等待绑定确认的节点。'
              : '请先创建第一个节点。'}
          </p>
        </div>
      ) : null}

      <div className="resource-table">
        <div className="resource-table__head">
          <span>节点</span>
          <span>状态</span>
          <span>最近心跳 / 同步</span>
          <span>当前主问题</span>
        </div>
        {visibleNodes.map((node) => (
          <article key={node.node_id} className="resource-table__row">
            <div>
              <strong>
                <Link className="text-link" to={`/nodes/${node.node_id}`}>
                  {node.display_name}
                </Link>
              </strong>
              <p>
                {node.region} · {node.city} · {node.provider}
              </p>
              <p>
                <Link className="text-link" to={`/nodes/${node.node_id}/onboarding`}>
                  接入工作台
                </Link>
              </p>
              <p>
                <Link className="text-link" to={`/nodes/${node.node_id}`}>
                  查看详情
                </Link>
              </p>
              <p>标签：{formatLabelList(node.labels)}</p>
              {editingLabelNodeId === node.node_id ? (
                <div className="page-stack">
                  <p>
                    <label>
                      标签
                      <input
                        name={`labels-${node.node_id}`}
                        value={labelDraft}
                        onChange={(event) => setLabelDraft(event.target.value)}
                      />
                    </label>
                  </p>
                  <div className="badge-row badge-row--wrap">
                    <button
                      type="button"
                      disabled={metadataBusyNodeId === node.node_id}
                      onClick={() => void handleSaveLabels(node)}
                    >
                      {metadataBusyNodeId === node.node_id ? '正在保存…' : '保存标签'}
                    </button>
                    <button
                      type="button"
                      disabled={metadataBusyNodeId === node.node_id}
                      onClick={() => {
                        setEditingLabelNodeId((current) =>
                          current === node.node_id ? null : current,
                        )
                        setLabelDraft('')
                        setMetadataErrors((current) => {
                          if (!current[node.node_id]) return current
                          const next = { ...current }
                          delete next[node.node_id]
                          return next
                        })
                      }}
                    >
                      取消
                    </button>
                  </div>
                </div>
              ) : (
                <button
                  type="button"
                  disabled={metadataBusyNodeId !== null}
                  onClick={() => {
                    if (metadataBusyNodeId !== null) return
                    setEditingLabelNodeId(node.node_id)
                    setLabelDraft(node.labels.join(', '))
                    setMetadataErrors((current) => {
                      if (!current[node.node_id]) return current
                      const next = { ...current }
                      delete next[node.node_id]
                      return next
                    })
                  }}
                >
                  快速编辑标签
                </button>
              )}
              <div className="badge-row badge-row--wrap">
                {nodeRuntimeActions(node).map(({ action, label }) => (
                  <button
                    key={action}
                    ref={(element) => {
                      actionButtonRefs.current[actionButtonKey(node.node_id, action)] = element
                    }}
                    type="button"
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
              {pendingConfirmation?.nodeId === node.node_id && pendingConfirmation.action === 'pause' ? (
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
              {runtimeErrors[node.node_id] ? <p>{runtimeErrors[node.node_id]}</p> : null}
              {metadataErrors[node.node_id] ? (
                <p role="alert">{metadataErrors[node.node_id]}</p>
              ) : null}
            </div>
            <div className="badge-row badge-row--wrap">
              <StatusBadge label={node.lifecycle_status} />
              <StatusBadge label={node.monitoring_status} />
              <StatusBadge label={node.current_health_status} />
              {isBindingConflictNode(node) ? (
                <StatusBadge label={NODE_BINDING_CONFLICT_STATUS} />
              ) : null}
            </div>
            <div>
              <strong>{formatDateTime(node.last_heartbeat_at)}</strong>
              <p>同步：{formatDateTime(node.last_sync_at)}</p>
            </div>
            <div>
              <strong>{node.current_active_incident_count}</strong>
              <p>
                {isBindingConflictNode(node)
                  ? NODE_BINDING_CONFLICT_SUMMARY
                  : node.current_primary_issue_summary || '暂无明显异常'}
              </p>
            </div>
          </article>
        ))}
      </div>
    </section>
  )
}

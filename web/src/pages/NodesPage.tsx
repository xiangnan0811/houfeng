import { type FormEvent, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { StatusBadge } from '../components/StatusBadge'
import { ApiError, issueNodeEnrollmentToken, listNodes } from '../lib/api'
import { formatDateTime } from '../lib/format'
import { setOnboardingTokenCache } from '../lib/onboardingTokenCache'
import type { NodeRecord } from '../lib/types'

type CreateNodeInput = {
  display_name: string
  region: string
  city: string
  provider: string
  lifecycle_status: string
  labels: string[]
  note: string
}

const initialCreateForm: CreateNodeInput = {
  display_name: '',
  region: '',
  city: '',
  provider: '',
  lifecycle_status: '待接入',
  labels: [],
  note: '',
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
    body: JSON.stringify(input),
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
  return value
    .split(/[,，]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

export function NodesPage() {
  const navigate = useNavigate()
  const [nodes, setNodes] = useState<NodeRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [labelInput, setLabelInput] = useState('')
  const [createForm, setCreateForm] = useState<CreateNodeInput>(initialCreateForm)

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

      try {
        const issue = await issueNodeEnrollmentToken(node.node_id)
        setOnboardingTokenCache(node.node_id, issue)
        setCreateOpen(false)
        setLabelInput('')
        setCreateForm(initialCreateForm)
        navigate(`/nodes/${node.node_id}/onboarding`)
      } catch (issueError) {
        setCreateError(
          `节点已创建，但生成接入 Token 失败：${describeError(issueError, '请稍后重试')}`,
        )
      }
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
        <button type="button" onClick={() => setCreateOpen((current) => !current)}>
          新建节点
        </button>
      </header>

      {createOpen ? (
        <section className="page-panel">
          <p className="page-panel__eyebrow">Node Create</p>
          <h3 className="page-panel__title">创建节点并进入接入工作台</h3>
          <p className="page-panel__description">
            创建完成后将立即生成接入 Token，并跳转到节点接入准备页。
          </p>
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
                生命周期状态
                <select
                  name="lifecycle_status"
                  value={createForm.lifecycle_status}
                  onChange={(event) => updateField('lifecycle_status', event.target.value)}
                >
                  <option value="待接入">待接入</option>
                  <option value="在用">在用</option>
                  <option value="观察中">观察中</option>
                  <option value="不续费">不续费</option>
                  <option value="已退役">已退役</option>
                </select>
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

      <div className="resource-table">
        <div className="resource-table__head">
          <span>节点</span>
          <span>状态</span>
          <span>最近心跳 / 同步</span>
          <span>当前主问题</span>
        </div>
        {nodes.map((node) => (
          <Link key={node.node_id} className="resource-table__row" to={`/nodes/${node.node_id}`}>
            <div>
              <strong>{node.display_name}</strong>
              <p>
                {node.region} · {node.city} · {node.provider}
              </p>
            </div>
            <div className="badge-row badge-row--wrap">
              <StatusBadge label={node.lifecycle_status} />
              <StatusBadge label={node.monitoring_status} />
              <StatusBadge label={node.current_health_status} />
            </div>
            <div>
              <strong>{formatDateTime(node.last_heartbeat_at)}</strong>
              <p>同步：{formatDateTime(node.last_sync_at)}</p>
            </div>
            <div>
              <strong>{node.current_active_incident_count}</strong>
              <p>{node.current_primary_issue_summary || '暂无明显异常'}</p>
            </div>
          </Link>
        ))}
      </div>
    </section>
  )
}

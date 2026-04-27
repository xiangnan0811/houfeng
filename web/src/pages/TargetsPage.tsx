import { type FormEvent, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { StatusBadge } from '../components/StatusBadge'
import {
  ApiError,
  archiveTarget,
  createTarget,
  enterTargetMaintenance,
  exitTargetMaintenance,
  listTargets,
  pauseTarget,
  restoreTargetToPaused,
  resumeTarget,
} from '../lib/api'
import { formatDateTime, formatLabelList } from '../lib/format'
import type { CreateTargetInput, TargetRecord } from '../lib/types'

const TARGET_PAUSE_CONFIRM_MESSAGE = '暂停会停止采集并产生数据空档，确定继续吗？'
const TARGET_ARCHIVE_CONFIRM_MESSAGE = '归档会让目标退出当前工作集，但会保留历史记录，确定继续吗？'

const TARGET_TYPE_OPTIONS = [
  { value: 'service', label: 'service' },
  { value: 'china_reference', label: 'china_reference' },
] as const

const TARGET_RUN_STATUS_OPTIONS = [
  { value: '启用', label: '启用' },
  { value: '维护中', label: '维护中' },
  { value: '暂停', label: '暂停' },
] as const

type CreateTargetFormState = {
  name: string
  targetType: CreateTargetInput['target_type']
  host: string
  basePort: string
  executionNodeLabels: string
  runStatus: CreateTargetInput['run_status']
  labels: string
  note: string
}

const initialCreateForm: CreateTargetFormState = {
  name: '',
  targetType: 'service',
  host: '',
  basePort: '',
  executionNodeLabels: '',
  runStatus: '启用',
  labels: '',
  note: '',
}

type TargetRuntimeAction =
  | 'enter-maintenance'
  | 'exit-maintenance'
  | 'pause'
  | 'resume'
  | 'archive'
  | 'restore-to-paused'

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function parseLabels(value: string) {
  return value
    .split(/[,，]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function parseOptionalPositiveInteger(value: string, label: string): number | undefined {
  const normalized = value.trim()
  if (normalized === '') return undefined
  if (!/^[1-9]\d*$/.test(normalized)) {
    throw new Error(`${label}必须为正整数。`)
  }
  return Number.parseInt(normalized, 10)
}

function buildCreateTargetInput(form: CreateTargetFormState): CreateTargetInput {
  const executionNodeLabels = parseLabels(form.executionNodeLabels)
  if (executionNodeLabels.length === 0) {
    throw new Error('执行节点标签至少需要填写一个。')
  }

  const basePort = parseOptionalPositiveInteger(form.basePort, 'Base Port')
  return {
    name: form.name.trim(),
    target_type: form.targetType,
    host: form.host.trim(),
    ...(basePort == null ? {} : { base_port: basePort }),
    execution_node_labels: executionNodeLabels,
    run_status: form.runStatus,
    labels: parseLabels(form.labels),
    note: form.note.trim(),
  }
}

function targetRuntimeActions(
  target: TargetRecord,
): Array<{ action: TargetRuntimeAction; label: string }> {
  if (target.run_status === '启用') {
    return [
      { action: 'enter-maintenance', label: '进入维护' },
      { action: 'pause', label: '暂停' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '维护中') {
    return [
      { action: 'exit-maintenance', label: '退出维护' },
      { action: 'pause', label: '暂停' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '暂停') {
    return [
      { action: 'resume', label: '恢复' },
      { action: 'archive', label: '归档' },
    ]
  }

  if (target.run_status === '已归档') {
    return [{ action: 'restore-to-paused', label: '恢复到暂停' }]
  }

  return []
}

export function TargetsPage() {
  const navigate = useNavigate()
  const mountedRef = useRef(false)
  const [targets, setTargets] = useState<TargetRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [createForm, setCreateForm] = useState<CreateTargetFormState>(initialCreateForm)
  const [runtimeBusyTargetId, setRuntimeBusyTargetId] = useState<string | null>(null)
  const [runtimeErrors, setRuntimeErrors] = useState<Record<string, string>>({})

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

  function resetCreateFlow() {
    setCreateError(null)
    setCreateForm(initialCreateForm)
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

    setCreateSubmitting(true)
    try {
      const created = await createTarget(payload)
      if (!mountedRef.current) return
      setTargets((current) => [
        created,
        ...current.filter((item) => item.target_id !== created.target_id),
      ])
      navigate(`/targets/${created.target_id}`)
    } catch (submitError) {
      if (!mountedRef.current) return
      setCreateError(describeError(submitError, '创建目标失败'))
    } finally {
      if (mountedRef.current) {
        setCreateSubmitting(false)
      }
    }
  }

  async function handleRuntimeAction(target: TargetRecord, action: TargetRuntimeAction) {
    if (action === 'pause' && !window.confirm(TARGET_PAUSE_CONFIRM_MESSAGE)) {
      return
    }
    if (action === 'archive' && !window.confirm(TARGET_ARCHIVE_CONFIRM_MESSAGE)) {
      return
    }

    setRuntimeBusyTargetId(target.target_id)
    setRuntimeErrors((current) => {
      if (!current[target.target_id]) return current
      const next = { ...current }
      delete next[target.target_id]
      return next
    })

    try {
      const updated =
        action === 'enter-maintenance'
          ? await enterTargetMaintenance(target.target_id)
          : action === 'exit-maintenance'
            ? await exitTargetMaintenance(target.target_id)
            : action === 'pause'
              ? await pauseTarget(target.target_id)
              : action === 'resume'
                ? await resumeTarget(target.target_id)
                : action === 'archive'
                  ? await archiveTarget(target.target_id)
                  : await restoreTargetToPaused(target.target_id)
      setTargets((current) =>
        current.map((item) => (item.target_id === updated.target_id ? updated : item)),
      )
    } catch (runtimeError) {
      setRuntimeErrors((current) => ({
        ...current,
        [target.target_id]: describeError(runtimeError, '目标运行控制操作失败'),
      }))
    } finally {
      setRuntimeBusyTargetId((current) => (current === target.target_id ? null : current))
    }
  }

  if (loading) {
    return <section className="page-panel">正在加载目标列表…</section>
  }

  if (error) {
    return (
      <section className="page-panel">
        <h2 className="page-panel__title">目标</h2>
        <p className="page-panel__description">{error}</p>
      </section>
    )
  }

  return (
    <section className="page-stack">
      <header className="section-heading">
        <div>
          <p className="section-heading__eyebrow">Targets</p>
          <h2 className="section-heading__title">目标列表</h2>
          <p className="section-heading__description">
            以 ProbeItem 视角组织目标状态，并保留执行节点标签与最近成功/失败摘要。
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
          新建目标
        </button>
      </header>

      {createOpen ? (
        <section className="page-panel">
          <p className="page-panel__eyebrow">Target Create</p>
          <h3 className="page-panel__title">创建目标</h3>
          <p className="page-panel__description">
            填写入口、执行节点标签与运行状态，创建后进入目标详情页继续配置 ProbeItem。
          </p>
          <form onSubmit={handleCreate}>
            <p>
              <label>
                目标名称
                <input
                  name="name"
                  value={createForm.name}
                  onChange={(event) => updateCreateField('name', event.target.value)}
                  required
                />
              </label>
            </p>
            <p>
              <label>
                目标类型
                <select
                  name="targetType"
                  value={createForm.targetType}
                  onChange={(event) =>
                    updateCreateField(
                      'targetType',
                      event.target.value as CreateTargetFormState['targetType'],
                    )
                  }
                >
                  {TARGET_TYPE_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </label>
            </p>
            <p>
              <label>
                Host
                <input
                  name="host"
                  value={createForm.host}
                  onChange={(event) => updateCreateField('host', event.target.value)}
                  required
                />
              </label>
            </p>
            <p>
              <label>
                Base Port
                <input
                  name="basePort"
                  inputMode="numeric"
                  value={createForm.basePort}
                  onChange={(event) => updateCreateField('basePort', event.target.value)}
                />
              </label>
            </p>
            <p>
              <label>
                执行节点标签
                <input
                  name="executionNodeLabels"
                  value={createForm.executionNodeLabels}
                  onChange={(event) =>
                    updateCreateField('executionNodeLabels', event.target.value)
                  }
                />
              </label>
            </p>
            <p>
              <label>
                运行状态
                <select
                  name="runStatus"
                  value={createForm.runStatus}
                  onChange={(event) =>
                    updateCreateField(
                      'runStatus',
                      event.target.value as CreateTargetFormState['runStatus'],
                    )
                  }
                >
                  {TARGET_RUN_STATUS_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </label>
            </p>
            <p>
              <label>
                目标标签
                <input
                  name="labels"
                  value={createForm.labels}
                  onChange={(event) => updateCreateField('labels', event.target.value)}
                />
              </label>
            </p>
            <p>
              <label>
                备注
                <textarea
                  name="note"
                  value={createForm.note}
                  onChange={(event) => updateCreateField('note', event.target.value)}
                  rows={3}
                />
              </label>
            </p>
            {createError ? <p>{createError}</p> : null}
            <div>
              <button type="submit" disabled={createSubmitting}>
                {createSubmitting ? '正在创建…' : '创建目标'}
              </button>
            </div>
          </form>
        </section>
      ) : null}

      {targets.length === 0 ? (
        <div className="empty-state">
          <h3>当前还没有目标</h3>
          <p>创建第一个目标后，可以继续为它配置 ProbeItem。</p>
          <p>
            <button type="button" onClick={() => setCreateOpen(true)}>
              创建第一个目标
            </button>
          </p>
        </div>
      ) : (
        <div className="resource-table">
          <div className="resource-table__head">
            <span>目标</span>
            <span>执行与状态</span>
            <span>最近成功 / 失败</span>
            <span>当前主问题</span>
          </div>
          {targets.map((target) => (
            <article key={target.target_id} className="resource-table__row">
              <div>
                <strong>{target.name}</strong>
                <p>
                  {target.target_type} · {target.host}
                  {target.base_port ? `:${target.base_port}` : ''}
                </p>
                <p>
                  <Link className="text-link" to={`/targets/${target.target_id}`}>
                    查看详情
                  </Link>
                </p>
                <div className="badge-row badge-row--wrap">
                  {targetRuntimeActions(target).map(({ action, label }) => (
                    <button
                      key={action}
                      type="button"
                      disabled={runtimeBusyTargetId === target.target_id}
                      onClick={() => void handleRuntimeAction(target, action)}
                    >
                      {label}
                    </button>
                  ))}
                </div>
                {runtimeErrors[target.target_id] ? <p>{runtimeErrors[target.target_id]}</p> : null}
              </div>
              <div>
                <div className="badge-row badge-row--wrap">
                  <StatusBadge label={target.run_status} />
                  <StatusBadge label={target.current_health_status} />
                </div>
                <p>{formatLabelList(target.execution_node_labels)}</p>
              </div>
              <div>
                <strong>{formatDateTime(target.last_success_at)}</strong>
                <p>失败：{formatDateTime(target.last_failure_at)}</p>
              </div>
              <div>
                <strong>{target.current_active_incident_count}</strong>
                <p>{target.current_primary_issue_summary || '暂无明显异常'}</p>
              </div>
            </article>
          ))}
        </div>
      )}
    </section>
  )
}

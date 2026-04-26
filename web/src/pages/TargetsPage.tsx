import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { StatusBadge } from '../components/StatusBadge'
import {
  ApiError,
  archiveTarget,
  enterTargetMaintenance,
  exitTargetMaintenance,
  listTargets,
  pauseTarget,
  restoreTargetToPaused,
  resumeTarget,
} from '../lib/api'
import { formatDateTime, formatLabelList } from '../lib/format'
import type { TargetRecord } from '../lib/types'

const TARGET_PAUSE_CONFIRM_MESSAGE = '暂停会停止采集并产生数据空档，确定继续吗？'
const TARGET_ARCHIVE_CONFIRM_MESSAGE = '归档会让目标退出当前工作集，但会保留历史记录，确定继续吗？'

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
  const [targets, setTargets] = useState<TargetRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [runtimeBusyTargetId, setRuntimeBusyTargetId] = useState<string | null>(null)
  const [runtimeErrors, setRuntimeErrors] = useState<Record<string, string>>({})

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
      </header>

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
    </section>
  )
}

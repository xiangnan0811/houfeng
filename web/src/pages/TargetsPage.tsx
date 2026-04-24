import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { StatusBadge } from '../components/StatusBadge'
import { ApiError, listTargets } from '../lib/api'
import { formatDateTime, formatLabelList } from '../lib/format'
import type { TargetRecord } from '../lib/types'

export function TargetsPage() {
  const [targets, setTargets] = useState<TargetRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

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
          <Link
            key={target.target_id}
            className="resource-table__row"
            to={`/targets/${target.target_id}`}
          >
            <div>
              <strong>{target.name}</strong>
              <p>
                {target.target_type} · {target.host}
                {target.base_port ? `:${target.base_port}` : ''}
              </p>
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
          </Link>
        ))}
      </div>
    </section>
  )
}

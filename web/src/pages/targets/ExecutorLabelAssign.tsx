import { useState } from 'react'
import { Link } from 'react-router-dom'

import { updateMonitoringInstanceMetadata } from '../../lib/api'
import type { MonitoringInstanceRecord } from '../../lib/types'
import { describeError } from './targetHelpers'

const ASSIGN_CANDIDATE_LIMIT = 3

// 没有实例带执行标签时，让用户显式给某台实例加上该标签；不做静默的默认标签，
// 否则所有写了同一标签的目标都会从这台实例发起探测。
export function ExecutorLabelAssign({
  label,
  candidates,
  onAssigned,
}: {
  label: string
  candidates: MonitoringInstanceRecord[]
  onAssigned: (updated: MonitoringInstanceRecord) => void
}) {
  const [pending, setPending] = useState<MonitoringInstanceRecord | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function confirm(instance: MonitoringInstanceRecord) {
    setSubmitting(true)
    setError(null)
    try {
      const updated = await updateMonitoringInstanceMetadata(
        instance.monitoring_instance_id,
        {
          ...(instance.group.trim() ? { group: instance.group.trim() } : {}),
          labels: [...instance.labels, label],
          note: instance.note,
        },
        { expectedUpdatedAt: instance.updated_at },
      )
      setPending(null)
      onAssigned(updated)
    } catch (caught: unknown) {
      setError(describeError(caught, '添加标签失败'))
    } finally {
      setSubmitting(false)
    }
  }

  const shown = candidates.slice(0, ASSIGN_CANDIDATE_LIMIT)
  const instanceName = (instance: MonitoringInstanceRecord) => instance.display_name || instance.monitoring_instance_id

  return (
    <span className="target-create-drawer__label-assign" role="group" aria-label="给监控实例加标签">
      {pending ? (
        <>
          <span className="target-create-drawer__executors">
            确认给 {instanceName(pending)} 加上「{label}」？加上后，它也会执行其他带这个标签的目标。
          </span>
          <button type="button" className="btn sm primary" disabled={submitting} onClick={() => void confirm(pending)}>
            {submitting ? '正在添加…' : '确认添加'}
          </button>
          <button type="button" className="btn sm secondary" disabled={submitting} onClick={() => { setPending(null); setError(null) }}>
            取消
          </button>
        </>
      ) : (
        <>
          {shown.map((instance) => (
            <button
              key={instance.monitoring_instance_id}
              type="button"
              className="btn sm secondary"
              onClick={() => { setPending(instance); setError(null) }}
            >
              给 {instanceName(instance)} 加上「{label}」
            </button>
          ))}
          {candidates.length > shown.length ? (
            <Link to="/monitoring">到监控实例中选择其余 {candidates.length - shown.length} 台</Link>
          ) : null}
        </>
      )}
      {error ? (
        <span className="target-create-drawer__executors target-create-drawer__executors--warn" role="alert">{error}</span>
      ) : null}
    </span>
  )
}

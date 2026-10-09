import { useEffect, useId, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { getMonitoringInstance, updateMonitoringInstanceMetadata } from '../../lib/api'
import { ApiError } from '../../lib/apiRequest'
import type { MonitoringInstanceRecord } from '../../lib/types'
import { describeError } from './targetHelpers'

const ASSIGN_CANDIDATE_LIMIT = 3
// 同步与心跳会不断刷新实例的 updated_at；写入前重读一次，冲突时再重读重试一次。
const ASSIGN_ATTEMPTS = 2

type PendingAssign = { instanceId: string; label: string }

async function appendLabel(instanceId: string, label: string): Promise<MonitoringInstanceRecord> {
  for (let attempt = 1; ; attempt += 1) {
    const current = await getMonitoringInstance(instanceId)
    const labels = current.labels.includes(label) ? current.labels : [...current.labels, label]
    try {
      return await updateMonitoringInstanceMetadata(
        instanceId,
        {
          ...(current.group.trim() ? { group: current.group.trim() } : {}),
          labels,
          note: current.note,
        },
        { expectedUpdatedAt: current.updated_at },
      )
    } catch (error: unknown) {
      if (!(error instanceof ApiError && error.status === 409) || attempt >= ASSIGN_ATTEMPTS) throw error
    }
  }
}

function describeAssignError(error: unknown): string {
  if (error instanceof ApiError && error.status === 409) return '实例资料正在更新，没有加上标签，请再确认一次。'
  return describeError(error, '添加标签失败')
}

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
  const [pending, setPending] = useState<PendingAssign | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const requestRef = useRef(0)
  const mountedRef = useRef(true)
  const groupRef = useRef<HTMLSpanElement>(null)
  const confirmRef = useRef<HTMLButtonElement>(null)
  const focusTriggerRef = useRef<string | null>(null)
  const descriptionId = useId()

  // 确认只对发起时的标签有效；标签变了就回到候选列表，不沿用旧确认。
  const confirming = pending && pending.label === label
    ? candidates.find((instance) => instance.monitoring_instance_id === pending.instanceId) ?? null
    : null

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  useEffect(() => {
    if (confirming) {
      confirmRef.current?.focus()
      return
    }
    const triggerId = focusTriggerRef.current
    if (!triggerId) return
    focusTriggerRef.current = null
    Array.from(groupRef.current?.querySelectorAll<HTMLButtonElement>('button[data-instance-id]') ?? [])
      .find((button) => button.dataset.instanceId === triggerId)
      ?.focus()
  }, [confirming])

  async function confirm(instance: MonitoringInstanceRecord) {
    const requestId = requestRef.current + 1
    requestRef.current = requestId
    const requestedLabel = label
    setSubmitting(true)
    setError(null)
    try {
      const updated = await appendLabel(instance.monitoring_instance_id, requestedLabel)
      if (!mountedRef.current || requestRef.current !== requestId) return
      setPending(null)
      onAssigned(updated)
    } catch (caught: unknown) {
      if (!mountedRef.current || requestRef.current !== requestId) return
      setError(`给 ${instanceName(instance)} 加上「${requestedLabel}」失败：${describeAssignError(caught)}`)
    } finally {
      if (mountedRef.current && requestRef.current === requestId) setSubmitting(false)
    }
  }

  function cancel() {
    if (pending) focusTriggerRef.current = pending.instanceId
    setPending(null)
    setError(null)
  }

  const shown = candidates.slice(0, ASSIGN_CANDIDATE_LIMIT)

  return (
    <span ref={groupRef} className="target-create-drawer__label-assign" role="group" aria-label="给监控实例加标签">
      {confirming ? (
        <>
          <span id={descriptionId} className="target-create-drawer__executors" role="status">
            确认给 {instanceName(confirming)} 加上「{label}」？加上后，它也会执行其他带这个标签的目标。
          </span>
          <button
            ref={confirmRef}
            type="button"
            className="btn sm primary"
            aria-describedby={descriptionId}
            disabled={submitting}
            onClick={() => void confirm(confirming)}
          >
            {submitting ? '正在添加…' : '确认添加'}
          </button>
          <button type="button" className="btn sm secondary" disabled={submitting} onClick={cancel}>
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
              data-instance-id={instance.monitoring_instance_id}
              disabled={submitting}
              onClick={() => { setPending({ instanceId: instance.monitoring_instance_id, label }); setError(null) }}
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

function instanceName(instance: MonitoringInstanceRecord): string {
  return instance.display_name || instance.monitoring_instance_id
}

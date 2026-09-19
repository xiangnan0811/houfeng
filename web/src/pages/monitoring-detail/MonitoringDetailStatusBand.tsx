import type { ReactNode } from 'react'

import { StatusBadge } from '../../components/StatusBadge'
import { Timestamp } from '../../components/atoms'
import type { MonitoringInstanceRecord } from '../../lib/types'
import { monitoringInstanceEffectiveHealth } from '../monitoring/monitoringHelpers'
import type { HeartbeatFreshness } from '../monitoring/types'

type Props = {
  monitoringInstance: MonitoringInstanceRecord
  heartbeatFreshness?: HeartbeatFreshness
  snapshotReadAt: Date | null
}

const HEALTH_TONE: Record<string, string> = {
  正常: 'normal',
  关注: 'notice',
  告警: 'alert',
  严重: 'critical',
}

function heartbeatEvidence(
  freshness: HeartbeatFreshness | undefined,
  snapshotReadAt: Date | null,
  fallbackAt: string | undefined,
): ReactNode {
  if (freshness?.kind === 'missing') return '未收到心跳'
  if (freshness?.kind === 'invalid') return '时间无效'
  const at = freshness && 'at' in freshness ? freshness.at : fallbackAt
  if (!at) return '—'
  return (
    <>
      {snapshotReadAt ? (
        <Timestamp value={at} mode="relative" now={snapshotReadAt} />
      ) : (
        <Timestamp value={at} mode="absolute" />
      )}
      {freshness?.kind === 'stale' ? ' · 数据陈旧' : null}
      {freshness?.kind === 'policy-unavailable' ? ' · 新鲜度策略不可用' : null}
    </>
  )
}

export function MonitoringDetailStatusBand({
  monitoringInstance,
  heartbeatFreshness,
  snapshotReadAt,
}: Props) {
  const healthLabel = monitoringInstanceEffectiveHealth(monitoringInstance)
  const healthTone = HEALTH_TONE[healthLabel] ?? 'unknown'
  const archived = Boolean(monitoringInstance.archived_at)
  const showMonitoringBadge =
    monitoringInstance.monitoring_status === '维护中' || monitoringInstance.monitoring_status === '暂停'
  const showBindingBadge =
    monitoringInstance.binding_status === '未绑定' ||
    monitoringInstance.binding_status === '指纹变更待确认'

  return (
    <div className="monitoring-detail-status" aria-label="监控实例当前状态">
      <span
        className={`monitoring-detail-status__health monitoring-detail-status__health--${healthTone}`}
      >
        {healthLabel}
      </span>
      <span className="monitoring-detail-status__heartbeat">
        心跳 {heartbeatEvidence(heartbeatFreshness, snapshotReadAt, monitoringInstance.last_heartbeat_at)}
      </span>
      {showMonitoringBadge ? <StatusBadge label={monitoringInstance.monitoring_status} /> : null}
      {showBindingBadge ? <StatusBadge label={monitoringInstance.binding_status} /> : null}
      {archived ? <StatusBadge label="已归档" /> : null}
    </div>
  )
}

import type { ReactNode } from 'react'

import { StatusBadge } from '../../components/StatusBadge'
import { Timestamp } from '../../components/atoms'
import type { HostSample, MonitoringInstanceRecord } from '../../lib/types'
import { formatSampledUptime } from '../monitoring/monitoringHelpers'
import type { HeartbeatFreshness } from '../monitoring/types'

type Props = {
  monitoringInstance: MonitoringInstanceRecord
  heartbeatFreshness?: HeartbeatFreshness
  snapshotReadAt: Date | null
  sample: HostSample | null
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
  sample,
}: Props) {
  const archived = Boolean(monitoringInstance.archived_at)
  const showMonitoringBadge =
    monitoringInstance.monitoring_status === '维护中' || monitoringInstance.monitoring_status === '暂停'
  const showBindingBadge =
    monitoringInstance.binding_status === '未绑定' ||
    monitoringInstance.binding_status === '指纹变更待确认'

  return (
    <div className="monitoring-detail-status" aria-label="心跳与采样">
      <span className="monitoring-detail-status__heartbeat">
        心跳 {heartbeatEvidence(heartbeatFreshness, snapshotReadAt, monitoringInstance.last_heartbeat_at)}
      </span>
      {sample ? (
        <span className="monitoring-detail-status__sample">
          采样{' '}
          {snapshotReadAt ? (
            <Timestamp value={sample.observed_at} mode="relative" now={snapshotReadAt} />
          ) : (
            <Timestamp value={sample.observed_at} mode="absolute" />
          )}
          {' · 已运行 '}
          {formatSampledUptime(sample.uptime_seconds)}
        </span>
      ) : (
        <span className="monitoring-detail-status__sample">当前样本 尚无</span>
      )}
      {showMonitoringBadge ? <StatusBadge label={monitoringInstance.monitoring_status} /> : null}
      {showBindingBadge ? <StatusBadge label={monitoringInstance.binding_status} /> : null}
      {archived ? <StatusBadge label="已归档" /> : null}
    </div>
  )
}

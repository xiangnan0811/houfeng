import type { ReactNode } from 'react'

import { Button } from '../../components/atoms/Button'
import { MonoDigits, Timestamp } from '../../components/atoms/Mono'
import { formatElapsedSince } from '../../lib/format'
import type { ActiveIncidentRecord, HostSample, MonitoringInstanceRecord } from '../../lib/types'
import type { HeartbeatFreshness } from '../monitoring/types'
import { MONITORING_INSTANCE_BINDING_CONFLICT_STATUS } from './monitoringDetailConstants'

type NoticeTone = 'critical' | 'alert' | 'notice' | 'maintenance' | 'offline'

type Props = {
  monitoringInstance: MonitoringInstanceRecord
  incidents: ActiveIncidentRecord[]
  incidentsError: string | null
  incidentsLoaded: boolean
  incidentsRetrying: boolean
  onRetryIncidents: () => void
  runtimeError: string | null
  runtimeFactsError: string | null
  runtimeFactsLoading: boolean
  hasRetainedRuntimeFacts: boolean
  onRetryRuntimeFacts: () => void
  bindingConflictLoading: boolean
  bindingConflictError: string | null
  onRetryBindingConflict: () => void
  onOpenBindingConflict: () => void
  onOpenIncidentHistory: () => void
  heartbeatFreshness?: HeartbeatFreshness
  snapshotReadAt: Date | null
  sample: HostSample | null
}

function earliestIncident(incidents: ActiveIncidentRecord[]): ActiveIncidentRecord | null {
  return (
    [...incidents].sort(
      (a, b) => new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
    )[0] ?? null
  )
}

function incidentTone(status: string, fallback?: string): { tone: NoticeTone; mark: string } {
  const value = status === '严重' || status === '告警' || status === '关注' ? status : fallback
  if (value === '严重') return { tone: 'critical', mark: '严重' }
  if (value === '关注') return { tone: 'notice', mark: '关注' }
  return { tone: 'alert', mark: '告警' }
}

function coversHeartbeat(summary: string, incidents: ActiveIncidentRecord[]): boolean {
  if (/心跳/.test(summary)) return true
  return incidents.some(
    (incident) => incident.incident_class === 'heartbeat_stale' || /心跳/.test(incident.source_summary || ''),
  )
}

function relativeStamp(value: string | undefined, now: Date | null): ReactNode {
  if (!value) return '尚无'
  return now ? <Timestamp value={value} mode="relative" now={now} /> : <Timestamp value={value} mode="absolute" />
}

function NoticeRow({
  tone,
  mark,
  title,
  detail,
  action,
  role = 'status',
}: {
  tone: NoticeTone
  mark: string
  title: ReactNode
  detail?: ReactNode
  action?: ReactNode
  role?: 'status' | 'alert'
}) {
  return (
    <div className={`monitoring-detail-notice monitoring-detail-notice--${tone}`} role={role}>
      <div className="monitoring-detail-notice__copy">
        <span className="monitoring-detail-notice__mark">{mark}</span>
        <span className="monitoring-detail-notice__text">{title}</span>
        {detail ? <span className="monitoring-detail-notice__detail">{detail}</span> : null}
      </div>
      {action ? <div className="monitoring-detail-notice__actions">{action}</div> : null}
    </div>
  )
}

export function MonitoringDetailNotices({
  monitoringInstance,
  incidents,
  incidentsError,
  incidentsLoaded,
  incidentsRetrying,
  onRetryIncidents,
  runtimeError,
  runtimeFactsError,
  runtimeFactsLoading,
  hasRetainedRuntimeFacts,
  onRetryRuntimeFacts,
  bindingConflictLoading,
  bindingConflictError,
  onRetryBindingConflict,
  onOpenBindingConflict,
  onOpenIncidentHistory,
  heartbeatFreshness,
  snapshotReadAt,
  sample,
}: Props) {
  const showBindingConflict =
    monitoringInstance.binding_status === MONITORING_INSTANCE_BINDING_CONFLICT_STATUS
  const unbound = monitoringInstance.binding_status === '未绑定'
  const activeIncidentCount = monitoringInstance.current_active_incident_count
  const showActiveIncidents = activeIncidentCount > 0 || incidents.length > 0
  const firstIncident = earliestIncident(incidents)
  const incidentSummary =
    monitoringInstance.current_primary_issue_summary ||
    firstIncident?.source_summary ||
    '存在活跃异常'
  const incidentCount = activeIncidentCount > 0 ? activeIncidentCount : incidents.length
  const { tone: incidentNoticeTone, mark: incidentMark } = incidentTone(
    monitoringInstance.current_health_status,
    firstIncident?.severity,
  )
  const heartbeatCovered = showActiveIncidents && coversHeartbeat(incidentSummary, incidents)
  const showStale = !unbound && heartbeatFreshness?.kind === 'stale' && !heartbeatCovered
  const showMissingHeartbeat = !unbound && heartbeatFreshness?.kind === 'missing' && !heartbeatCovered
  const showMaintenance = monitoringInstance.monitoring_status === '维护中'
  const showPause = monitoringInstance.monitoring_status === '暂停'
  const hasAnyNotice =
    showBindingConflict ||
    showActiveIncidents ||
    showStale ||
    showMissingHeartbeat ||
    showMaintenance ||
    showPause ||
    Boolean(incidentsError && incidentsLoaded) ||
    Boolean(runtimeError) ||
    Boolean(runtimeFactsError)

  if (!hasAnyNotice) return null

  const heartbeatAt = heartbeatFreshness && 'at' in heartbeatFreshness ? heartbeatFreshness.at : monitoringInstance.last_heartbeat_at

  return (
    <div className="monitoring-detail-notices">
      {showBindingConflict ? (
        <NoticeRow
          tone="alert"
          mark="待确认"
          title="绑定冲突待确认"
          role="alert"
          detail={bindingConflictError || (bindingConflictLoading ? '正在加载…' : undefined)}
          action={
            bindingConflictError ? (
              <Button variant="ghost" size="sm" aria-label="重试加载绑定冲突" onClick={onRetryBindingConflict}>
                重试
              </Button>
            ) : (
              <Button variant="primary" size="sm" disabled={bindingConflictLoading} onClick={onOpenBindingConflict}>
                处置绑定冲突
              </Button>
            )
          }
        />
      ) : null}

      {showActiveIncidents ? (
        <NoticeRow
          tone={incidentNoticeTone}
          mark={incidentMark}
          title={incidentSummary}
          role={incidentNoticeTone === 'notice' ? 'status' : 'alert'}
          detail={
            <>
              活跃 <MonoDigits>{incidentCount}</MonoDigits>
              {firstIncident?.started_at ? (
                <>
                  {' · 已持续 '}
                  <MonoDigits>{formatElapsedSince(firstIncident.started_at)}</MonoDigits>
                </>
              ) : null}
            </>
          }
          action={
            <Button variant="secondary" size="sm" onClick={onOpenIncidentHistory}>
              查看事件
            </Button>
          }
        />
      ) : null}

      {showStale ? (
        <NoticeRow
          tone="notice"
          mark="数据陈旧"
          title="心跳与采样已落后"
          detail={
            <>
              心跳 {relativeStamp(heartbeatAt, snapshotReadAt)}
              {' · 采样 '}
              {sample ? relativeStamp(sample.observed_at, snapshotReadAt) : '尚无'}
            </>
          }
        />
      ) : null}

      {showMissingHeartbeat ? (
        <NoticeRow
          tone="alert"
          mark="未收到心跳"
          title="还没有来自这台主机的心跳"
          role="alert"
          detail={sample ? <>当前样本 {relativeStamp(sample.observed_at, snapshotReadAt)}</> : '当前样本 尚无'}
        />
      ) : null}

      {showMaintenance ? (
        <NoticeRow
          tone="maintenance"
          mark="维护中"
          title="观测继续，异常通知已抑制"
        />
      ) : null}

      {showPause ? (
        <NoticeRow
          tone="offline"
          mark="暂停"
          title="监控已暂停"
          detail="不会按在线主机判定或发出通知"
        />
      ) : null}

      {incidentsError && incidentsLoaded ? (
        <NoticeRow
          tone="alert"
          mark="加载失败"
          title={incidentsError}
          role="alert"
          action={
            <Button variant="ghost" size="sm" aria-label="重试加载活跃异常" disabled={incidentsRetrying} onClick={onRetryIncidents}>
              {incidentsRetrying ? '重试中…' : '重试'}
            </Button>
          }
        />
      ) : null}

      {runtimeError ? (
        <NoticeRow tone="alert" mark="操作失败" title={runtimeError} role="alert" />
      ) : null}

      {runtimeFactsError ? (
        <NoticeRow
          tone="alert"
          mark="指标失败"
          title={
            hasRetainedRuntimeFacts
              ? `运行指标刷新失败，仍显示上次已标记窗口。${runtimeFactsError}`
              : `运行指标不可用。${runtimeFactsError}`
          }
          role="status"
          action={
            <Button variant="ghost" size="sm" disabled={runtimeFactsLoading} onClick={onRetryRuntimeFacts}>
              {runtimeFactsLoading ? '重试中…' : '重试运行指标'}
            </Button>
          }
        />
      ) : null}
    </div>
  )
}

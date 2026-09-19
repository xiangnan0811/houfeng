import { Button } from '../../components/atoms/Button'
import { MonoDigits, Timestamp } from '../../components/atoms/Mono'
import type { ActiveIncidentRecord, MonitoringInstanceRecord } from '../../lib/types'
import { MONITORING_INSTANCE_BINDING_CONFLICT_STATUS } from './monitoringDetailConstants'

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
}

function earliestIncident(incidents: ActiveIncidentRecord[]): ActiveIncidentRecord | null {
  return (
    [...incidents].sort(
      (a, b) => new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
    )[0] ?? null
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
}: Props) {
  const showBindingConflict =
    monitoringInstance.binding_status === MONITORING_INSTANCE_BINDING_CONFLICT_STATUS
  const activeIncidentCount = monitoringInstance.current_active_incident_count
  const showActiveIncidents = activeIncidentCount > 0 || incidents.length > 0
  const firstIncident = earliestIncident(incidents)
  const incidentSummary =
    monitoringInstance.current_primary_issue_summary ||
    firstIncident?.source_summary ||
    '存在活跃异常'
  const incidentCount = activeIncidentCount > 0 ? activeIncidentCount : incidents.length
  const hasAnyNotice =
    showBindingConflict ||
    showActiveIncidents ||
    Boolean(incidentsError && incidentsLoaded) ||
    Boolean(runtimeError) ||
    Boolean(runtimeFactsError)

  if (!hasAnyNotice) return null

  return (
    <div className="monitoring-detail-notices">
      {showBindingConflict ? (
        <div className="monitoring-detail-notice" role="status">
          <span className="monitoring-detail-notice__text">绑定冲突待确认</span>
          {bindingConflictError ? (
            <>
              <span className="monitoring-detail-notice__detail">{bindingConflictError}</span>
              <Button variant="ghost" size="sm" aria-label="重试加载绑定冲突" onClick={onRetryBindingConflict}>重试</Button>
            </>
          ) : (
            <>
              {bindingConflictLoading ? (
                <span className="monitoring-detail-notice__detail">正在加载…</span>
              ) : null}
              <Button
                variant="primary"
                size="sm"
                disabled={bindingConflictLoading}
                onClick={onOpenBindingConflict}
              >
                处置绑定冲突
              </Button>
            </>
          )}
        </div>
      ) : null}

      {showActiveIncidents ? (
        <div className="monitoring-detail-notice" role="status">
          <span className="monitoring-detail-notice__text">{incidentSummary}</span>
          <span className="monitoring-detail-notice__detail">
            活跃 <MonoDigits>{incidentCount}</MonoDigits>
            {firstIncident?.started_at ? (
              <>
                {' · 持续 '}
                <Timestamp value={firstIncident.started_at} mode="relative" />
              </>
            ) : null}
          </span>
          <button type="button" className="text-link" onClick={onOpenIncidentHistory}>
            事件
          </button>
        </div>
      ) : null}

      {incidentsError && incidentsLoaded ? (
        <div className="monitoring-detail-notice" role="alert">
          <span className="monitoring-detail-notice__text">{incidentsError}</span>
          <Button variant="ghost" size="sm" aria-label="重试加载活跃异常" disabled={incidentsRetrying} onClick={onRetryIncidents}>
            {incidentsRetrying ? '重试中…' : '重试'}
          </Button>
        </div>
      ) : null}

      {runtimeError ? (
        <div className="monitoring-detail-notice" role="alert">
          <span className="monitoring-detail-notice__text">{runtimeError}</span>
        </div>
      ) : null}

      {runtimeFactsError ? (
        <div className="monitoring-detail-notice" role="status">
          <span className="monitoring-detail-notice__text">
            {hasRetainedRuntimeFacts
              ? `运行指标刷新失败，仍显示上次已标记窗口。${runtimeFactsError}`
              : `运行指标不可用。${runtimeFactsError}`}
          </span>
          <Button variant="ghost" size="sm" disabled={runtimeFactsLoading} onClick={onRetryRuntimeFacts}>
            {runtimeFactsLoading ? '重试中…' : '重试运行指标'}
          </Button>
        </div>
      ) : null}
    </div>
  )
}
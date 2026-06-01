import { Badge, Button, Hostname, MonoDigits, Timestamp } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import type { VPSMonitoringInstanceSummary } from '../../lib/types'
import { HealthBadge } from '../assetPageBadges'

type VPSMonitoringInstanceLinksSectionProps = {
  monitoring: VPSMonitoringInstanceSummary[]
  unlinkingMonitoringInstanceId: string | null
  linkFeedback: string | null
  linkFeedbackIsError: boolean
  onOpenLink: () => void
  onUnlinkMonitoringInstance: (monitoringInstance: VPSMonitoringInstanceSummary) => void
}

export function VPSMonitoringInstanceLinksSection({
  monitoring,
  unlinkingMonitoringInstanceId,
  linkFeedback,
  linkFeedbackIsError,
  onOpenLink,
  onUnlinkMonitoringInstance,
}: VPSMonitoringInstanceLinksSectionProps) {
  return (
    <section className="page-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">MONITORING EVIDENCE</p>
          <h2>监控实例证据</h2>
          <p className="section-heading__description">
            关联监控实例用于解释续费决策，只使用 VPS detail contract 返回的 linked monitoring instance health、心跳、异常数量和主问题摘要。
          </p>
        </div>
        <span className="section-heading__meta">
          <MonoDigits>{monitoring.length}</MonoDigits> 个 active link
        </span>
        <Button variant="secondary" size="sm" onClick={onOpenLink}>关联监控实例</Button>
      </div>
      {linkFeedback ? (
        <p
          className={[
            'asset-operation-feedback',
            linkFeedbackIsError && 'asset-operation-feedback--error',
          ].filter(Boolean).join(' ')}
          role={linkFeedbackIsError ? 'alert' : 'status'}
        >
          {linkFeedback}
        </p>
      ) : null}
      {monitoring.length > 0 ? (
        <div className="vps-monitoring-instance-evidence-strip" aria-label="监控实例证据摘要">
          {monitoring.map((monitoringInstance) => (
            <article key={monitoringInstance.monitoring_instance_id} className="vps-monitoring-instance-evidence-strip__item">
              <div>
                <strong>{monitoringInstance.display_name}</strong>
                <Hostname truncate>{monitoringInstance.monitoring_instance_id}</Hostname>
              </div>
              <HealthBadge value={monitoringInstance.current_health_status} />
              <span className="asset-status-stack">
                <Badge variant="info" tone="neutral">{monitoringInstance.monitoring_status || '未知'}</Badge>
              </span>
              <span><MonoDigits>{monitoringInstance.current_active_incident_count}</MonoDigits> 个活跃异常</span>
              <small>{formatOptional(monitoringInstance.current_primary_issue_summary)}</small>
              <div className="vps-monitoring-instance-evidence-strip__location">
                <span>{[monitoringInstance.region, monitoringInstance.city].filter(Boolean).join(' · ') || '—'}</span>
                <span>{formatOptional(monitoringInstance.provider)}</span>
              </div>
              <div className="vps-monitoring-instance-evidence-strip__heartbeat">
                <span>最近心跳</span>
                <Timestamp value={monitoringInstance.last_heartbeat_at} />
              </div>
              <div className="vps-monitoring-instance-evidence-strip__actions">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={unlinkingMonitoringInstanceId !== null}
                  onClick={() => onUnlinkMonitoringInstance(monitoringInstance)}
                >
                  {unlinkingMonitoringInstanceId === monitoringInstance.monitoring_instance_id ? '解除中…' : '解除关联'}
                </Button>
              </div>
            </article>
          ))}
        </div>
      ) : (
        <p className="empty-inline">尚未关联监控实例</p>
      )}
    </section>
  )
}

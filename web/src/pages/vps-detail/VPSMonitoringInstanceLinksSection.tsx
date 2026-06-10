import { Badge, Button, Hostname, MonoDigits, Timestamp } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import type { VPSMonitoringInstanceSummary } from '../../lib/types'
import { HealthBadge } from '../assetPageBadges'

type VPSMonitoringInstanceLinksSectionProps = {
  monitoring: VPSMonitoringInstanceSummary[]
  unlinkingMonitoringInstanceId: string | null
  pendingUnlinkMonitoringInstance: VPSMonitoringInstanceSummary | null
  linkFeedback: string | null
  linkFeedbackIsError: boolean
  onCreateMonitoringInstance: () => void
  onOpenLink: () => void
  onUpgradeMonitoringInstance: (monitoringInstance: VPSMonitoringInstanceSummary) => void
  onRequestUnlinkMonitoringInstance: (monitoringInstance: VPSMonitoringInstanceSummary) => void
  onCancelUnlinkMonitoringInstance: () => void
  onConfirmUnlinkMonitoringInstance: (monitoringInstance: VPSMonitoringInstanceSummary) => void
}

export function VPSMonitoringInstanceLinksSection({
  monitoring,
  unlinkingMonitoringInstanceId,
  pendingUnlinkMonitoringInstance,
  linkFeedback,
  linkFeedbackIsError,
  onCreateMonitoringInstance,
  onOpenLink,
  onUpgradeMonitoringInstance,
  onRequestUnlinkMonitoringInstance,
  onCancelUnlinkMonitoringInstance,
  onConfirmUnlinkMonitoringInstance,
}: VPSMonitoringInstanceLinksSectionProps) {
  const pendingUnlinkName = pendingUnlinkMonitoringInstance?.display_name ?? pendingUnlinkMonitoringInstance?.monitoring_instance_id ?? ''
  const hasNoActiveLinks = monitoring.length === 0
  const hasSingleActiveLink = monitoring.length === 1
  const hasDuplicateActiveLinks = monitoring.length > 1

  return (
    <section className="page-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">MONITORING EVIDENCE</p>
          <h2>监控实例证据</h2>
        <p className="section-heading__description">
            监控实例作为 VPS 的运行观测事实，用于解释续费和迁移判断。
        </p>
        </div>
        <span className="section-heading__meta">
          <MonoDigits>{monitoring.length}</MonoDigits> 个 active link
        </span>
        {hasNoActiveLinks ? (
          <div className="section-heading__actions">
            <Button variant="primary" size="sm" onClick={onCreateMonitoringInstance}>创建并接入 agent</Button>
            <Button variant="secondary" size="sm" onClick={onOpenLink}>关联已有监控实例</Button>
          </div>
        ) : hasSingleActiveLink ? (
          <div className="section-heading__actions">
            <Button variant="primary" size="sm" onClick={() => onUpgradeMonitoringInstance(monitoring[0])}>升级/重新接入 agent</Button>
          </div>
        ) : null}
      </div>
      {hasDuplicateActiveLinks ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          检测到 <MonoDigits>{monitoring.length}</MonoDigits> 个 active 监控实例关联。请人工核对要保留的实例，逐个升级/重新接入或解除多余关联。
        </p>
      ) : null}
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
      {pendingUnlinkMonitoringInstance ? (
        <section className="asset-lifecycle-confirm" role="alertdialog" aria-label="确认解除监控实例关联">
          <p className="asset-lifecycle-confirm__eyebrow">操作确认</p>
          <h4>确认解除监控实例关联</h4>
          <div className="asset-lifecycle-confirm__flow">
            <span>当前：{pendingUnlinkName} 正作为该 VPS 的监控证据。</span>
            <span>操作后：该监控实例不再关联到这个 VPS。</span>
          </div>
          <div className="asset-lifecycle-confirm__callouts">
            <p>会移除 VPS 台账中的监控实例证据关联，后续资产判断不再把它计入该 VPS。</p>
            <p>不会删除监控实例、历史事件、agent 绑定或观测数据。</p>
          </div>
          <div className="asset-operation-actions">
            <Button
              type="button"
              variant="secondary"
              disabled={unlinkingMonitoringInstanceId === pendingUnlinkMonitoringInstance.monitoring_instance_id}
              onClick={onCancelUnlinkMonitoringInstance}
            >
              取消
            </Button>
            <Button
              type="button"
              variant="danger"
              disabled={unlinkingMonitoringInstanceId === pendingUnlinkMonitoringInstance.monitoring_instance_id}
              onClick={() => onConfirmUnlinkMonitoringInstance(pendingUnlinkMonitoringInstance)}
            >
              {unlinkingMonitoringInstanceId === pendingUnlinkMonitoringInstance.monitoring_instance_id ? '解除中…' : '确认解除关联'}
            </Button>
          </div>
        </section>
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
                  onClick={() => onUpgradeMonitoringInstance(monitoringInstance)}
                >
                  升级/重新接入 agent
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={unlinkingMonitoringInstanceId !== null}
                  onClick={() => onRequestUnlinkMonitoringInstance(monitoringInstance)}
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

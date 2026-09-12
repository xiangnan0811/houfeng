import { Link } from 'react-router-dom'

import { Button, MonoDigits, Timestamp } from '../../components/atoms'
import type { VPSMonitoringInstanceSummary } from '../../lib/types'
import { HealthBadge } from '../assetPageBadges'
import { VPSObject } from './VPSDetailDialog'
import {
  monitoringConfigurationLabel,
  monitoringInstanceDetailHref,
  monitoringInstanceName,
  monitoringObservedHealthLabel,
  monitoringProviderLabel,
  monitoringRegionLabel,
} from './vpsDetailResourcePresentation'

type VPSMonitoringInstanceLinksSectionProps = {
  vpsId: string
  monitoring: VPSMonitoringInstanceSummary[]
  readOnly?: boolean
  writeBlocked?: boolean
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
  vpsId,
  monitoring,
  readOnly = false,
  writeBlocked = false,
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
  const hasDuplicateActiveLinks = monitoring.length > 1

  return (
    <div className="vps-objects">
      <p className="vps-context">
        共<MonoDigits>{monitoring.length}</MonoDigits>项
      </p>
      {!readOnly && hasNoActiveLinks ? (
        <div>
          <Button variant="primary" size="sm" disabled={writeBlocked} onClick={onCreateMonitoringInstance}>接入/升级 agent</Button>
          <Button variant="secondary" size="sm" disabled={writeBlocked} onClick={onOpenLink}>关联已有监控实例</Button>
        </div>
      ) : null}
      {hasDuplicateActiveLinks ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          检测到 <MonoDigits>{monitoring.length}</MonoDigits> 个 active 监控实例关联。请人工核对要保留的实例，逐个接入/升级或解除多余关联。
        </p>
      ) : null}
      {!readOnly && linkFeedback ? (
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
      {!readOnly && pendingUnlinkMonitoringInstance ? (
        <section className="asset-lifecycle-confirm" role="alertdialog" aria-label="确认解除监控实例关联">
          <p className="asset-lifecycle-confirm__eyebrow">操作确认</p>
          <h4>确认解除监控实例关联</h4>
          <div className="asset-lifecycle-confirm__flow">
            <span>当前：{pendingUnlinkName} 正作为该 VPS 的监控证据。</span>
            <span>操作后：该监控实例不再关联到这个 VPS。</span>
          </div>
          <div className="asset-lifecycle-confirm__callouts">
            <p>会移除 VPS 台账中的监控关联，后续不再把它计入该 VPS。</p>
            <p>不会删除监控实例、历史事件、agent 绑定或观测数据。</p>
          </div>
          <div className="asset-operation-actions">
            <Button
              type="button"
              variant="secondary"
              disabled={writeBlocked || unlinkingMonitoringInstanceId === pendingUnlinkMonitoringInstance.monitoring_instance_id}
              onClick={onCancelUnlinkMonitoringInstance}
            >
              取消
            </Button>
            <Button
              type="button"
              variant="danger"
              disabled={writeBlocked || unlinkingMonitoringInstanceId === pendingUnlinkMonitoringInstance.monitoring_instance_id}
              onClick={() => onConfirmUnlinkMonitoringInstance(pendingUnlinkMonitoringInstance)}
            >
              {unlinkingMonitoringInstanceId === pendingUnlinkMonitoringInstance.monitoring_instance_id ? '解除中…' : '确认解除关联'}
            </Button>
          </div>
        </section>
      ) : null}
      {monitoring.length > 0 ? (
        <ul className="vps-object-list">
          {monitoring.map((monitoringInstance) => {
            const name = monitoringInstanceName(monitoringInstance)
            const health = monitoringObservedHealthLabel(monitoringInstance.current_health_status)
            const healthRecorded = health !== '观测健康未记录'
            const heartbeat = monitoringInstance.last_heartbeat_at?.trim() ?? ''
            return (
              <VPSObject
                key={monitoringInstance.monitoring_instance_id}
                name={name}
                id={monitoringInstance.monitoring_instance_id}
                status={
                  <span>
                    {healthRecorded ? <>观测健康 <HealthBadge value={health} /></> : health}
                  </span>
                }
                actions={
                  <div className="vps-object__actions">
                    <Link
                      className="btn sm ghost"
                      to={monitoringInstanceDetailHref(vpsId, monitoringInstance.monitoring_instance_id)}
                    >
                      查看监控实例
                    </Link>
                    {!readOnly ? (
                      <>
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={writeBlocked || unlinkingMonitoringInstanceId !== null}
                          onClick={() => onUpgradeMonitoringInstance(monitoringInstance)}
                        >
                          接入/升级 agent
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={writeBlocked || unlinkingMonitoringInstanceId !== null}
                          onClick={() => onRequestUnlinkMonitoringInstance(monitoringInstance)}
                        >
                          {unlinkingMonitoringInstanceId === monitoringInstance.monitoring_instance_id ? '解除中…' : '解除关联'}
                        </Button>
                      </>
                    ) : null}
                  </div>
                }
              >
                <dl className="vps-object__facts">
                  <div>
                    <dt>监控配置</dt>
                    <dd>{monitoringConfigurationLabel(monitoringInstance.monitoring_status)}</dd>
                  </div>
                  <div>
                    <dt>服务商</dt>
                    <dd>{monitoringProviderLabel(monitoringInstance.provider)}</dd>
                  </div>
                  <div>
                    <dt>区域</dt>
                    <dd>{monitoringRegionLabel(monitoringInstance.region, monitoringInstance.city)}</dd>
                  </div>
                  <div>
                    <dt>最近心跳</dt>
                    <dd>{heartbeat ? <Timestamp value={heartbeat} /> : '尚未收到心跳'}</dd>
                  </div>
                </dl>
              </VPSObject>
            )
          })}
        </ul>
      ) : (
        <p className="empty-inline">尚未关联监控实例</p>
      )}
    </div>
  )
}

import { Link, useLocation } from 'react-router-dom'

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
  linkFeedback,
  linkFeedbackIsError,
  onCreateMonitoringInstance,
  onUpgradeMonitoringInstance,
}: VPSMonitoringInstanceLinksSectionProps) {
  const location = useLocation()
  const hasNoActiveLinks = !monitoring.some((item) => item.lifecycle_status !== '已退役')

  return (
    <div className="vps-objects">
      <p className="vps-context">
        共<MonoDigits>{monitoring.length}</MonoDigits>项
      </p>
      {!readOnly && hasNoActiveLinks ? (
        <div>
          <Button variant="primary" size="sm" disabled={writeBlocked} onClick={onCreateMonitoringInstance}>接入/升级 agent</Button>
        </div>
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
      {monitoring.length > 0 ? (
        <ul className="vps-object-list">
          {monitoring.map((monitoringInstance) => {
            const name = monitoringInstanceName(monitoringInstance)
            const health = monitoringObservedHealthLabel(monitoringInstance.current_health_status, monitoringInstance)
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
                      state={location.state}
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

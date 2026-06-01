import { StatusBadge } from '../StatusBadge'
import { Hostname, MonoDigits, Timestamp } from '../atoms'
import { Button } from '../atoms/Button'
import { formatLabelList, formatUptime } from '../../lib/format'
import type { HostSample, MonitoringInstanceRecord } from '../../lib/types'

export type MonitoringInstanceRuntimeAction = 'enter-maintenance' | 'exit-maintenance' | 'pause' | 'resume'

type Props = {
  monitoringInstance: MonitoringInstanceRecord
  latestSample: HostSample | null
  runtimeActions: Array<{ action: MonitoringInstanceRuntimeAction; label: string }>
  runtimeSubmitting: boolean
  onRuntimeAction: (action: MonitoringInstanceRuntimeAction) => void
  registerActionRef: (action: MonitoringInstanceRuntimeAction, element: HTMLButtonElement | null) => void
  onOpenHistory: () => void
  onOpenCommands: () => void
  onOpenOnboarding: () => void
}

function locationLine(monitoringInstance: MonitoringInstanceRecord): string {
  const parts = [monitoringInstance.region, monitoringInstance.city, monitoringInstance.provider].map((p) => (p ?? '').trim()).filter(Boolean)
  return parts.join(' · ')
}

export function MonitoringInstanceWatchtowerHeader({
  monitoringInstance,
  latestSample,
  runtimeActions,
  runtimeSubmitting,
  onRuntimeAction,
  registerActionRef,
  onOpenHistory,
  onOpenCommands,
  onOpenOnboarding,
}: Props) {
  const labelText = formatLabelList(monitoringInstance.labels)
  const agentVersion = latestSample?.agent_version || '—'
  const uptime = latestSample ? formatUptime(latestSample.uptime_seconds) : '—'
  const heartbeat = monitoringInstance.last_heartbeat_at

  return (
    <header className="watchtower-header" role="banner" aria-label="监控实例身份与操作">
      <div className="watchtower-header__row1">
        <div className="watchtower-header__title-block">
          <h1>{monitoringInstance.display_name}</h1>
          <div className="badge-row">
            <StatusBadge label={monitoringInstance.lifecycle_status} />
            <StatusBadge label={monitoringInstance.monitoring_status} />
            <StatusBadge label={monitoringInstance.binding_status} />
            <StatusBadge label={monitoringInstance.current_health_status} />
          </div>
        </div>
        <div className="watchtower-header__actions-block">
          <span className="watchtower-header__freshness" aria-label="数据新鲜度">
            心跳 <Timestamp value={heartbeat} mode="relative" /> · 运行{' '}
            <MonoDigits>{uptime}</MonoDigits>
          </span>
          <div className="watchtower-header__actions">
            <Button variant="ghost" size="sm" onClick={onOpenHistory}>
              查看历史
            </Button>
            <details className="watchtower-actions-menu">
              <summary aria-label="运行控制操作">…</summary>
              <div className="watchtower-actions-menu__panel">
                {runtimeActions.map(({ action, label }) => (
                  <button
                    key={action}
                    ref={(element) => registerActionRef(action, element)}
                    type="button"
                    disabled={runtimeSubmitting}
                    onClick={() => onRuntimeAction(action)}
                  >
                    {label}
                  </button>
                ))}
                <button
                  type="button"
                  className="watchtower-actions-menu__item"
                  onClick={(e) => {
                    e.stopPropagation()
                    onOpenOnboarding()
                  }}
                >
                  接入 agent…
                </button>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation()
                    onOpenCommands()
                  }}
                >
                  执行命令…
                </button>
              </div>
            </details>
          </div>
        </div>
      </div>
      <div className="watchtower-header__row2">
        {monitoringInstance.group ? (
          <>
            <span className="watchtower-header__meta-item">{monitoringInstance.group}</span>
            <span className="watchtower-header__meta-sep" aria-hidden>
              ·
            </span>
          </>
        ) : null}
        <span className="watchtower-header__meta-item">
          <Hostname>{monitoringInstance.monitoring_instance_id}</Hostname>
        </span>
        {locationLine(monitoringInstance) ? (
          <>
            <span className="watchtower-header__meta-sep" aria-hidden>
              ·
            </span>
            <span className="watchtower-header__meta-item">{locationLine(monitoringInstance)}</span>
          </>
        ) : null}
        {monitoringInstance.labels.length > 0 ? (
          <>
            <span className="watchtower-header__meta-sep" aria-hidden>
              ·
            </span>
            <span className="watchtower-header__meta-item watchtower-header__labels">
              {labelText}
            </span>
          </>
        ) : null}
        <span className="watchtower-header__meta-sep" aria-hidden>
          ·
        </span>
        <span className="watchtower-header__meta-item">
          agent <MonoDigits>{agentVersion}</MonoDigits>
        </span>
      </div>
    </header>
  )
}

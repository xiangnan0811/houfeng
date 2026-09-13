import { useEffect, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { StatusBadge } from '../StatusBadge'
import { Hostname, MonoDigits, Timestamp } from '../atoms'
import { Button } from '../atoms/Button'
import { formatLabelList, formatUptime } from '../../lib/format'
import type { HostSample, MonitoringInstanceRecord, VPSSummary } from '../../lib/types'

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
  onboardingActionLabel: string
  managementOnly?: boolean
  linkedVPS: VPSSummary[]
  linkedVPSLoading: boolean
  linkedVPSLoaded: boolean
  linkedVPSError: string | null
}

type HeaderStatusBadge = {
  dimension: string
  value: string
}

function confirmedMetadata(value: string | null | undefined): string {
  const trimmed = (value ?? '').trim()
  return trimmed === '未确认' ? '' : trimmed
}

function locationLine(monitoringInstance: MonitoringInstanceRecord): string {
  const location = [monitoringInstance.region, monitoringInstance.city]
    .map(confirmedMetadata)
    .filter(Boolean)
    .join(' · ')
  const provider = confirmedMetadata(monitoringInstance.provider)
  return [location || '位置未确认', provider || 'Provider 未确认'].join(' · ')
}

function HeaderStatusBadge({ dimension, value }: HeaderStatusBadge) {
  return (
    <span
      className="watchtower-status-badge"
      title={`${dimension}: ${value}`}
      aria-label={`${dimension}: ${value}`}
    >
      <span className="watchtower-status-badge__dimension">{dimension}</span>
      <StatusBadge label={value} />
    </span>
  )
}

function linkedVPSSummary(linkedVPS: VPSSummary[], loading: boolean, loaded: boolean, error: string | null, navigationState: unknown) {
  if (loading && !loaded) return <span className="watchtower-header__meta-item">VPS 关联加载中</span>
  if (error) return <span className="watchtower-header__meta-item">VPS 关联未同步</span>
  if (!loaded) return <span className="watchtower-header__meta-item">VPS 关联待同步</span>
  if (linkedVPS.length === 0) {
    return (
      <span className="watchtower-header__meta-item">
        VPS <Link className="text-link" to="/vps?view=unlinked">未关联</Link>
      </span>
    )
  }
  if (linkedVPS.length === 1) {
    const vps = linkedVPS[0]
    if (!vps?.vps_id || !vps.display_name) return <span className="watchtower-header__meta-item">VPS 关联未同步</span>
    return (
      <span className="watchtower-header__meta-item">
        VPS <Link className="text-link" to={`/vps/${vps.vps_id}`} state={navigationState}>{vps.display_name}</Link>
      </span>
    )
  }
  const primary = linkedVPS[0]
  if (!primary?.vps_id) return <span className="watchtower-header__meta-item">VPS {linkedVPS.length} 台</span>
  return (
    <span className="watchtower-header__meta-item">
      VPS <Link className="text-link" to={`/vps/${primary.vps_id}`} state={navigationState}>{linkedVPS.length} 台</Link>
    </span>
  )
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
  onboardingActionLabel,
  managementOnly = false,
  linkedVPS,
  linkedVPSLoading,
  linkedVPSLoaded,
  linkedVPSError,
}: Props) {
  const location = useLocation()
  const [now, setNow] = useState(() => new Date())
  const labels = Array.isArray(monitoringInstance.labels) ? monitoringInstance.labels : []
  const labelText = formatLabelList(labels)
  const agentVersion = latestSample?.agent_version || '—'
  const uptime = latestSample ? formatUptime(latestSample.uptime_seconds) : '—'
  const heartbeat = latestSample?.observed_at ?? monitoringInstance.last_heartbeat_at

  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 10000)
    return () => window.clearInterval(timer)
  }, [])

  return (
    <header className="page__head watchtower-identity" role="banner" aria-label="监控实例身份与操作">
      <div className="watchtower-identity__copy">
        <h1 className="page__title">{monitoringInstance.display_name}</h1>
        <div className="watchtower-identity__statuses" role="group" aria-label="监控实例当前状态">
          <HeaderStatusBadge dimension="生命周期" value={monitoringInstance.lifecycle_status} />
          <HeaderStatusBadge dimension="监控" value={monitoringInstance.monitoring_status} />
          <HeaderStatusBadge dimension="绑定" value={monitoringInstance.binding_status} />
          <HeaderStatusBadge dimension="健康" value={monitoringInstance.current_health_status} />
        </div>
        <dl className="watchtower-identity__meta">
          {monitoringInstance.group ? (
            <div className="watchtower-identity__meta-item">
              <dt>分组</dt>
              <dd>{monitoringInstance.group}</dd>
            </div>
          ) : null}
          <div className="watchtower-identity__meta-item">
            <dt>实例</dt>
            <dd><Hostname>{monitoringInstance.monitoring_instance_id}</Hostname></dd>
          </div>
          <div className="watchtower-identity__meta-item">
            <dt>位置</dt>
            <dd>{locationLine(monitoringInstance)}</dd>
          </div>
          <div className="watchtower-identity__meta-item">
            <dt>关联</dt>
            <dd>{linkedVPSSummary(linkedVPS, linkedVPSLoading, linkedVPSLoaded, linkedVPSError, location.state)}</dd>
          </div>
          {labels.length > 0 ? (
            <div className="watchtower-identity__meta-item">
              <dt>标签</dt>
              <dd className="watchtower-header__labels">{labelText}</dd>
            </div>
          ) : null}
          <div className="watchtower-identity__meta-item">
            <dt>agent</dt>
            <dd><MonoDigits>{agentVersion}</MonoDigits></dd>
          </div>
        </dl>
      </div>
      <div className="page__actions">
        <span className="watchtower-header__freshness" aria-label="数据新鲜度">
          心跳 <Timestamp value={heartbeat} mode="relative" now={now} /> · 运行{' '}
          <MonoDigits>{uptime}</MonoDigits>
        </span>
        <Button variant="ghost" size="sm" onClick={onOpenHistory}>
          查看历史
        </Button>
        <details className="watchtower-actions-menu">
          <summary aria-label="运行控制操作">…</summary>
          <div className="watchtower-actions-menu__panel">
            {!managementOnly && runtimeActions.map(({ action, label }) => (
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
            {!managementOnly ? (
              <>
                <button
                  type="button"
                  className="watchtower-actions-menu__item"
                  onClick={(e) => {
                    e.stopPropagation()
                    onOpenOnboarding()
                  }}
                >
                  {onboardingActionLabel}
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
              </>
            ) : (
              <span className="watchtower-actions-menu__hint">归档实例只允许在管理实例中操作</span>
            )}
            <Link
              className="watchtower-actions-menu__item"
              to={`/command-audit?monitoring_instance=${encodeURIComponent(monitoringInstance.monitoring_instance_id)}`}
            >
              查看命令审计
            </Link>
          </div>
        </details>
      </div>
    </header>
  )
}

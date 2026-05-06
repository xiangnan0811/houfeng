import { StatusBadge } from '../StatusBadge'
import { Hostname, MonoDigits, Timestamp } from '../atoms'
import { Button } from '../atoms/Button'
import { formatLabelList, formatUptime } from '../../lib/format'
import type { HostSample, NodeRecord } from '../../lib/types'

export type NodeRuntimeAction = 'enter-maintenance' | 'exit-maintenance' | 'pause' | 'resume'

type Props = {
  node: NodeRecord
  latestSample: HostSample | null
  runtimeActions: Array<{ action: NodeRuntimeAction; label: string }>
  runtimeSubmitting: boolean
  onRuntimeAction: (action: NodeRuntimeAction) => void
  registerActionRef: (action: NodeRuntimeAction, element: HTMLButtonElement | null) => void
  onOpenHistory: () => void
  onOpenCommands: () => void
}

function locationLine(node: NodeRecord): string {
  const parts = [node.region, node.city, node.provider].map((p) => p.trim()).filter(Boolean)
  return parts.join(' · ')
}

export function NodeWatchtowerHeader({
  node,
  latestSample,
  runtimeActions,
  runtimeSubmitting,
  onRuntimeAction,
  registerActionRef,
  onOpenHistory,
  onOpenCommands,
}: Props) {
  const labelText = formatLabelList(node.labels)
  const agentVersion = latestSample?.agent_version || '—'
  const uptime = latestSample ? formatUptime(latestSample.uptime_seconds) : '—'
  const heartbeat = node.last_heartbeat_at

  return (
    <header className="watchtower-header" role="banner" aria-label="节点身份与操作">
      <div className="watchtower-header__row1">
        <div className="watchtower-header__title-block">
          <h1>{node.display_name}</h1>
          <div className="badge-row">
            <StatusBadge label={node.lifecycle_status} />
            <StatusBadge label={node.monitoring_status} />
            <StatusBadge label={node.binding_status} />
            <StatusBadge label={node.current_health_status} />
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
            {runtimeActions.length > 0 ? (
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
                    className="btn btn--ghost btn--sm"
                    onClick={(e) => {
                      e.stopPropagation()
                      onOpenCommands()
                    }}
                  >
                    执行命令…
                  </button>
                </div>
              </details>
            ) : null}
          </div>
        </div>
      </div>
      <div className="watchtower-header__row2">
        <span className="watchtower-header__meta-item">
          <Hostname>{node.node_id}</Hostname>
        </span>
        {locationLine(node) ? (
          <>
            <span className="watchtower-header__meta-sep" aria-hidden>
              ·
            </span>
            <span className="watchtower-header__meta-item">{locationLine(node)}</span>
          </>
        ) : null}
        {node.labels.length > 0 ? (
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

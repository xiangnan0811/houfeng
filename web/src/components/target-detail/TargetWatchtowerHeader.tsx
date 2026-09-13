import { StatusBadge } from '../StatusBadge'
import { Hostname, Timestamp } from '../atoms'
import { Button } from '../atoms/Button'
import { formatLabelList } from '../../lib/format'
import type { TargetRecord } from '../../lib/types'
import type { TargetRuntimeAction } from './TargetRuntimeControls'

const RUNTIME_ACTION_BUTTONS_BY_RUN_STATUS: Record<
  string,
  Array<{ action: TargetRuntimeAction; label: string }>
> = {
  启用: [
    { action: 'enter-maintenance', label: '进入维护' },
    { action: 'pause', label: '暂停' },
  ],
  维护中: [
    { action: 'exit-maintenance', label: '退出维护' },
    { action: 'pause', label: '暂停' },
  ],
  暂停: [{ action: 'resume', label: '恢复' }],
  已归档: [],
}

function targetRuntimeActions(
  target: TargetRecord,
): Array<{ action: TargetRuntimeAction; label: string }> {
  return RUNTIME_ACTION_BUTTONS_BY_RUN_STATUS[target.run_status] ?? []
}

type Props = {
  target: TargetRecord
  runtimeSubmitting: boolean
  disabled?: boolean
  onRuntimeAction: (action: TargetRuntimeAction) => void
  registerActionRef: (
    action: TargetRuntimeAction,
    element: HTMLButtonElement | null,
  ) => void
  onOpenHistory: () => void
  onOpenMaintenance: () => void
}

export function TargetWatchtowerHeader({
  target,
  runtimeSubmitting,
  disabled = false,
  onRuntimeAction,
  registerActionRef,
  onOpenHistory,
  onOpenMaintenance,
}: Props) {
  const hostDisplay = target.base_port
    ? `${target.host}:${target.base_port}`
    : target.host
  const labelText = formatLabelList(target.labels)
  const execLabelText = formatLabelList(target.execution_monitoring_instance_labels)
  const runtimeActions = targetRuntimeActions(target)

  return (
    <header className="page__head watchtower-identity" role="banner" aria-label="目标身份与操作">
      <div className="watchtower-identity__copy">
        <h1 className="page__title">{target.name}</h1>
        <div className="watchtower-identity__statuses" role="group" aria-label="入口探测当前状态">
          <StatusBadge label={target.run_status} />
          <StatusBadge label={target.current_health_status} />
          <StatusBadge label={target.target_type} />
        </div>
        <dl className="watchtower-identity__meta">
          {target.group ? (
            <div className="watchtower-identity__meta-item">
              <dt>分组</dt>
              <dd>{target.group}</dd>
            </div>
          ) : null}
          <div className="watchtower-identity__meta-item">
            <dt>入口</dt>
            <dd><Hostname truncate maxChars={14}>{target.target_id}</Hostname></dd>
          </div>
          <div className="watchtower-identity__meta-item">
            <dt>主机</dt>
            <dd><Hostname>{hostDisplay}</Hostname></dd>
          </div>
          {target.labels.length > 0 ? (
            <div className="watchtower-identity__meta-item">
              <dt>标签</dt>
              <dd className="watchtower-header__labels">{labelText}</dd>
            </div>
          ) : null}
          {target.execution_monitoring_instance_labels.length > 0 ? (
            <div className="watchtower-identity__meta-item">
              <dt>执行</dt>
              <dd className="watchtower-header__labels">{execLabelText}</dd>
            </div>
          ) : null}
        </dl>
      </div>
      <div className="page__actions">
        <span className="watchtower-header__freshness" aria-label="数据新鲜度">
          最近成功{' '}
          <Timestamp value={target.last_success_at ?? null} mode="relative" />
          {' · '}最近失败{' '}
          <Timestamp value={target.last_failure_at ?? null} mode="relative" />
        </span>
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
                disabled={runtimeSubmitting || disabled}
                onClick={() => onRuntimeAction(action)}
              >
                {label}
              </button>
            ))}
            <button type="button" onClick={onOpenMaintenance}>
              资料维护
            </button>
          </div>
        </details>
      </div>
    </header>
  )
}

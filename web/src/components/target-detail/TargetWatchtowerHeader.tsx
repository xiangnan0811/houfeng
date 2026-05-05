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
}

export function TargetWatchtowerHeader({
  target,
  runtimeSubmitting,
  disabled = false,
  onRuntimeAction,
  registerActionRef,
  onOpenHistory,
}: Props) {
  const hostDisplay = target.base_port
    ? `${target.host}:${target.base_port}`
    : target.host
  const labelText = formatLabelList(target.labels)
  const execLabelText = formatLabelList(target.execution_node_labels)
  const runtimeActions = targetRuntimeActions(target)

  return (
    <header className="watchtower-header" role="banner" aria-label="目标身份与操作">
      <div className="watchtower-header__row1">
        <div className="watchtower-header__title-block">
          <h1>{target.name}</h1>
          <div className="badge-row">
            <StatusBadge label={target.run_status} />
            <StatusBadge label={target.current_health_status} />
            <StatusBadge label={target.target_type} />
          </div>
        </div>
        <div className="watchtower-header__actions-block">
          <span className="watchtower-header__freshness" aria-label="数据新鲜度">
            最近成功{' '}
            <Timestamp value={target.last_success_at ?? null} mode="relative" />
            {' · '}最近失败{' '}
            <Timestamp value={target.last_failure_at ?? null} mode="relative" />
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
                      disabled={runtimeSubmitting || disabled}
                      onClick={() => onRuntimeAction(action)}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </details>
            ) : null}
          </div>
        </div>
      </div>
      <div className="watchtower-header__row2">
        <span className="watchtower-header__meta-item">
          <Hostname truncate maxChars={14}>{target.target_id}</Hostname>
        </span>
        <span className="watchtower-header__meta-sep" aria-hidden>
          ·
        </span>
        <span className="watchtower-header__meta-item">
          <Hostname>{hostDisplay}</Hostname>
        </span>
        {target.labels.length > 0 ? (
          <>
            <span className="watchtower-header__meta-sep" aria-hidden>
              ·
            </span>
            <span className="watchtower-header__meta-item watchtower-header__labels">
              {labelText}
            </span>
          </>
        ) : null}
        {target.execution_node_labels.length > 0 ? (
          <>
            <span className="watchtower-header__meta-sep" aria-hidden>
              ·
            </span>
            <span className="watchtower-header__meta-item watchtower-header__labels">
              {execLabelText}
            </span>
          </>
        ) : null}
      </div>
    </header>
  )
}

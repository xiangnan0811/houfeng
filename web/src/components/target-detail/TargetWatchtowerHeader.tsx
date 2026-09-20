import { Badge, Hostname } from '../atoms'
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
  const controlBadge =
    target.run_status === '维护中'
      ? { label: '维护中', tone: 'maintenance' as const }
      : target.run_status === '暂停'
        ? { label: '暂停', tone: 'offline' as const }
        : target.run_status === '已归档'
          ? { label: '已归档', tone: 'offline' as const }
          : null

  return (
    <header className="target-detail-header" role="banner" aria-label="目标身份与操作">
      <div className="target-detail-header__identity">
        <div className="target-detail-header__title-row">
          <h1 className="target-detail-header__title">{target.name}</h1>
          {controlBadge ? (
            <Badge variant="state" tone={controlBadge.tone}>{controlBadge.label}</Badge>
          ) : null}
        </div>
        <dl className="target-detail-identity">
          <div className="target-detail-identity__item">
            <dt>类型</dt>
            <dd>{target.target_type}</dd>
          </div>
          <div className="target-detail-identity__item">
            <dt>主机</dt>
            <dd><Hostname>{hostDisplay}</Hostname></dd>
          </div>
          {target.group ? (
            <div className="target-detail-identity__item">
              <dt>分组</dt>
              <dd>{target.group}</dd>
            </div>
          ) : null}
          {target.labels.length > 0 ? (
            <div className="target-detail-identity__item">
              <dt>标签</dt>
              <dd>{labelText}</dd>
            </div>
          ) : null}
          {target.execution_monitoring_instance_labels.length > 0 ? (
            <div className="target-detail-identity__item">
              <dt>执行</dt>
              <dd>{execLabelText}</dd>
            </div>
          ) : null}
          <div className="target-detail-identity__item">
            <dt>ID</dt>
            <dd><Hostname truncate maxChars={14}>{target.target_id}</Hostname></dd>
          </div>
        </dl>
      </div>
      <div className="target-detail-header__end">
        <div className="target-detail-header__actions">
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
          <Button variant="ghost" size="sm" onClick={onOpenMaintenance}>
            资料维护
          </Button>
        </div>
      </div>
    </header>
  )
}

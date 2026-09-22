import { Hostname, MonoDigits, Modal, Timestamp } from '../../components/atoms'
import { Button } from '../../components/atoms/Button'
import type { MonitoringInstanceOnboardingState } from '../../lib/types'
import {
  MONITORING_INSTANCE_BINDING_CONFIRM_REBIND_LABEL,
  MONITORING_INSTANCE_BINDING_REJECT_PENDING_LABEL,
  MONITORING_INSTANCE_BINDING_RESET_LABEL,
} from './monitoringDetailConstants'
import {
  currentFingerprintSummary,
  maskFingerprint,
  pendingBindingMetadata,
} from './monitoringDetailHelpers'
import type { BindingConflictAction } from './types'

type Props = {
  open: boolean
  readOnly: boolean
  bindingConflict: MonitoringInstanceOnboardingState | null
  loading: boolean
  error: string | null
  bindingAction: BindingConflictAction | null
  actionsDisabled: boolean
  onConfirm: () => void
  onReject: () => void
  onReset: () => void
  onRetry: () => void
  onClose: () => void
}

export function MonitoringInstanceBindingConflictDialog({
  open,
  readOnly,
  bindingConflict,
  loading,
  error,
  bindingAction,
  actionsDisabled,
  onConfirm,
  onReject,
  onReset,
  onRetry,
  onClose,
}: Props) {
  const pendingMetadata = pendingBindingMetadata(bindingConflict)
  const attemptCount = pendingMetadata?.attempt_count
  const attemptDisplay = typeof attemptCount === 'number' ? attemptCount : '—'

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="处置绑定冲突"
      size="md"
      footer={readOnly ? undefined : (
        <div className="monitoring-detail-binding-dialog__actions">
          <Button variant="secondary" disabled={actionsDisabled} onClick={onConfirm}>
            {bindingAction === 'confirm' ? '正在确认…' : MONITORING_INSTANCE_BINDING_CONFIRM_REBIND_LABEL}
          </Button>
          <Button variant="secondary" disabled={actionsDisabled} onClick={onReject}>
            {bindingAction === 'reject' ? '正在拒绝…' : MONITORING_INSTANCE_BINDING_REJECT_PENDING_LABEL}
          </Button>
          <Button variant="secondary" disabled={actionsDisabled} onClick={onReset}>
            {bindingAction === 'reset' ? '正在重置…' : MONITORING_INSTANCE_BINDING_RESET_LABEL}
          </Button>
        </div>
      )}
    >
      <div className="monitoring-detail-binding-dialog page-stack">
        <p>
          同一台机器重装或合法替换 agent 后，通常会出现新的指纹接入请求。请先核对这次变更。
        </p>
        <dl className="monitoring-detail-binding-dialog__facts">
          <div>
            <dt>当前已绑定指纹</dt>
            <dd><Hostname>{currentFingerprintSummary(bindingConflict)}</Hostname></dd>
          </div>
          <div>
            <dt>待确认指纹</dt>
            <dd><Hostname>{maskFingerprint(pendingMetadata?.fingerprint)}</Hostname></dd>
          </div>
          <div>
            <dt>首次出现</dt>
            <dd><Timestamp value={pendingMetadata?.first_seen_at} mode="absolute" /></dd>
          </div>
          <div>
            <dt>最近出现</dt>
            <dd><Timestamp value={pendingMetadata?.last_seen_at} mode="absolute" /></dd>
          </div>
          <div>
            <dt>尝试次数</dt>
            <dd><MonoDigits>{attemptDisplay}</MonoDigits></dd>
          </div>
        </dl>
        {loading ? <p role="status">正在加载绑定冲突详情…</p> : null}
        {error ? (
          <p role="alert">
            {error}
            <Button variant="ghost" size="sm" onClick={onRetry}>重试</Button>
          </p>
        ) : null}
        <p className="monitoring-detail-binding-dialog__hint">
          如需重新生成一次性接入命令，请从页头「管理」菜单选择「升级/重新接入 agent…」。
        </p>
      </div>
      {readOnly ? (
        <p className="monitoring-detail-binding-dialog__readonly">只读预览不能处置绑定冲突</p>
      ) : null}
    </Modal>
  )
}

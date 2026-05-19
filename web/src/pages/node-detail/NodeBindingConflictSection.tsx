import { DetailSection } from '../../components/DetailSection'
import { Hostname, MonoDigits, Timestamp } from '../../components/atoms'
import type { NodeOnboardingState } from '../../lib/types'
import {
  NODE_BINDING_CONFIRM_REBIND_LABEL,
  NODE_BINDING_REJECT_PENDING_LABEL,
  NODE_BINDING_RESET_LABEL,
} from './nodeDetailConstants'
import {
  currentFingerprintSummary,
  maskFingerprint,
  pendingBindingMetadata,
} from './nodeDetailHelpers'
import type { BindingConflictAction } from './types'

type NodeBindingConflictSectionProps = {
  bindingConflict: NodeOnboardingState | null
  loading: boolean
  error: string | null
  bindingAction: BindingConflictAction | null
  actionsDisabled: boolean
  onConfirm: () => void
  onReject: () => void
  onReset: () => void
}

export function NodeBindingConflictSection({
  bindingConflict,
  loading,
  error,
  bindingAction,
  actionsDisabled,
  onConfirm,
  onReject,
  onReset,
}: NodeBindingConflictSectionProps) {
  const pendingMetadata = pendingBindingMetadata(bindingConflict)

  return (
    <DetailSection eyebrow="绑定冲突" title="绑定冲突处置" aside="高优先级">
      <article className="metric-card" aria-label="高优先级：绑定冲突待处理">
        <h3>高优先级：绑定冲突待处理</h3>
        <p>同一台机器重装或合法替换 agent 后，通常会出现新的指纹接入请求。请先核对这次变更。</p>
        <dl>
          <div>
            <dt>当前已绑定指纹</dt>
            <dd>
              <Hostname>{currentFingerprintSummary(bindingConflict)}</Hostname>
            </dd>
          </div>
          <div>
            <dt>待确认指纹</dt>
            <dd>
              <Hostname>{maskFingerprint(pendingMetadata?.fingerprint)}</Hostname>
            </dd>
          </div>
          <div>
            <dt>首次出现</dt>
            <dd>
              <Timestamp value={pendingMetadata?.first_seen_at} mode="absolute" />
            </dd>
          </div>
          <div>
            <dt>最近出现</dt>
            <dd>
              <Timestamp value={pendingMetadata?.last_seen_at} mode="absolute" />
            </dd>
          </div>
          <div>
            <dt>尝试次数</dt>
            <dd>
              <MonoDigits>{pendingMetadata?.attempt_count ?? 0}</MonoDigits>
            </dd>
          </div>
        </dl>
        {loading ? <p>正在加载绑定冲突详情…</p> : null}
        {error ? <p role="alert">{error}</p> : null}
        <div className="badge-row badge-row--wrap">
          <button
            type="button"
            disabled={actionsDisabled}
            onClick={onConfirm}
          >
            {bindingAction === 'confirm' ? '正在确认…' : NODE_BINDING_CONFIRM_REBIND_LABEL}
          </button>
          <button
            type="button"
            disabled={actionsDisabled}
            onClick={onReject}
          >
            {bindingAction === 'reject' ? '正在拒绝…' : NODE_BINDING_REJECT_PENDING_LABEL}
          </button>
          <button
            type="button"
            disabled={actionsDisabled}
            onClick={onReset}
          >
            {bindingAction === 'reset' ? '正在重置…' : NODE_BINDING_RESET_LABEL}
          </button>
        </div>
        <p>
          如需重新生成一次性接入命令，请从右上角运行控制菜单打开接入工作台。
        </p>
      </article>
    </DetailSection>
  )
}

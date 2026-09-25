import type { FormEvent } from 'react'

import { Button, Select } from '../../components/atoms'
import { replacedDecisionBlocked } from '../../lib/assetLifecycle'
import type { ApiFieldError } from '../../lib/apiRequest'
import { VPS_RENEWAL_DECISION_LABELS, type VPSAssetDetail, type VPSRenewalDecision } from '../../lib/types'
import type { DecisionDraftState } from './types'
import { RENEWAL_DECISION_OPTIONS } from './vpsDetailOptions'

type VPSRenewalDecisionFormProps = {
  formId: string
  detail: VPSAssetDetail
  draft: DecisionDraftState
  submitting: boolean
  fieldErrors?: ApiFieldError[]
  onDraftChange: (draft: DecisionDraftState) => void
  onFeedbackClear: () => void
  onEditFacts?: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSRenewalDecisionForm({
  formId,
  detail,
  draft,
  submitting,
  fieldErrors = [],
  onDraftChange,
  onFeedbackClear,
  onEditFacts,
  onSubmit,
}: VPSRenewalDecisionFormProps) {
  const savedLabel = VPS_RENEWAL_DECISION_LABELS[detail.renewal_decision]
  const pendingLabel = VPS_RENEWAL_DECISION_LABELS[draft.renewalDecision]
  const decisionChanged = draft.renewalDecision !== detail.renewal_decision
  const replacedBlocked = replacedDecisionBlocked(detail.lifecycle_status, detail.usage_status)
  const renewalError = fieldErrors.find((item) => item.field === 'renewal_decision')?.message
  const lifecycleError = fieldErrors.find((item) => item.field === 'lifecycle_status')?.message
  const usageError = fieldErrors.find((item) => item.field === 'usage_status')?.message

  return (
    <form id={formId} className="vps-form" onSubmit={onSubmit}>
      <p className="vps-context">{detail.display_name}</p>
      {decisionChanged ? (
        <p className="vps-context">当前「{savedLabel}」，将改为「{pendingLabel}」</p>
      ) : null}
      <Select
        label="续费决策"
        aria-label="续费决策"
        value={draft.renewalDecision}
        disabled={submitting}
        {...(renewalError ? { error: renewalError } : {})}
        onChange={(event) => {
          onDraftChange({
            ...draft,
            renewalDecision: event.target.value as VPSRenewalDecision,
          })
          onFeedbackClear()
        }}
      >
        {RENEWAL_DECISION_OPTIONS.map(([value, label]) => (
          <option key={value} value={value} disabled={value === 'replaced' && replacedBlocked}>
            {label}{value === 'replaced' && replacedBlocked ? '（先调整为非 active、用途非 in_use）' : ''}
          </option>
        ))}
      </Select>
      {replacedBlocked ? (
        <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
          已替换要求生命周期不是 active，且用途不是 in_use。先调整这两项，再选择已替换。
          {onEditFacts ? <Button type="button" variant="ghost" size="sm" onClick={onEditFacts}>编辑事实</Button> : null}
        </p>
      ) : null}
      {lifecycleError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">生命周期：{lifecycleError}</p> : null}
      {usageError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">用途：{usageError}</p> : null}
      <label className="input-field vps-wide">
        <span className="input-field__label">决策理由</span>
        <textarea
          className="input"
          aria-label="决策理由"
          value={draft.reason}
          disabled={submitting}
          rows={3}
          onChange={(event) => {
            onDraftChange({ ...draft, reason: event.target.value })
            onFeedbackClear()
          }}
          placeholder="例如：价格上涨，迁移到首尔监控实例"
        />
      </label>
    </form>
  )
}

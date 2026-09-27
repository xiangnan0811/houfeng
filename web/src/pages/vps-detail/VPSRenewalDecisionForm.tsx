import type { FormEvent } from 'react'

import { Button, Select } from '../../components/atoms'
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
  const renewalError = fieldErrors.find((item) => item.field === 'renewal_decision')?.message
  const autoRenewVerified = ['disabled', 'never_enabled', 'unsupported'].includes(detail.auto_renew_check ?? 'unchecked')

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
          <option key={value} value={value}>
            {label}
          </option>
        ))}
      </Select>
      {draft.renewalDecision === 'cancel' ? (
        <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
          {autoRenewVerified ? '已核对服务商不会自动续费。' : '请核对服务商自动续费是否已关闭；保存不续费意向不会代替服务商操作，未完成核对时将保留提醒。'}
          {onEditFacts ? <Button type="button" variant="ghost" size="sm" disabled={submitting} onClick={onEditFacts}>核对自动续费</Button> : null}
        </p>
      ) : null}
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
      <label className="input-field">
        <span className="input-field__label">复核日期</span>
        <input className="input" type="date" value={draft.reviewAt ?? ''} disabled={submitting} onChange={(event) => {
          onDraftChange({ ...draft, reviewAt: event.target.value })
          onFeedbackClear()
        }} />
      </label>
    </form>
  )
}

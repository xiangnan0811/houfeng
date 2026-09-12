import type { FormEvent } from 'react'

import { Select } from '../../components/atoms'
import { VPS_RENEWAL_DECISION_LABELS, type VPSAssetDetail, type VPSRenewalDecision } from '../../lib/types'
import type { DecisionDraftState } from './types'
import { RENEWAL_DECISION_OPTIONS } from './vpsDetailOptions'

type VPSRenewalDecisionFormProps = {
  formId: string
  detail: VPSAssetDetail
  draft: DecisionDraftState
  submitting: boolean
  onDraftChange: (draft: DecisionDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSRenewalDecisionForm({
  formId,
  detail,
  draft,
  submitting,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSRenewalDecisionFormProps) {
  const savedLabel = VPS_RENEWAL_DECISION_LABELS[detail.renewal_decision]
  const pendingLabel = VPS_RENEWAL_DECISION_LABELS[draft.renewalDecision]
  const decisionChanged = draft.renewalDecision !== detail.renewal_decision

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
        onChange={(event) => {
          onDraftChange({
            ...draft,
            renewalDecision: event.target.value as VPSRenewalDecision,
          })
          onFeedbackClear()
        }}
      >
        {RENEWAL_DECISION_OPTIONS.map(([value, label]) => (
          <option key={value} value={value}>{label}</option>
        ))}
      </Select>
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

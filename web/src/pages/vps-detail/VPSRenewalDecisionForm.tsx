import type { FormEvent } from 'react'

import { Button } from '../../components/atoms'
import type { VPSAssetDetail, VPSRenewalDecision } from '../../lib/types'
import { RenewalBadge } from '../assetPageBadges'
import type { DecisionDraftState } from './types'
import { RENEWAL_DECISION_OPTIONS } from './vpsDetailOptions'

type VPSRenewalDecisionFormProps = {
  detail: VPSAssetDetail
  draft: DecisionDraftState
  submitting: boolean
  error: string | null
  notice: string | null
  decisionChanged: boolean
  onCancel: () => void
  onDraftChange: (draft: DecisionDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSRenewalDecisionForm({
  detail,
  draft,
  submitting,
  error,
  notice,
  decisionChanged,
  onCancel,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSRenewalDecisionFormProps) {
  return (
    <form className="asset-operation-form" onSubmit={onSubmit}>
      <div className="asset-operation-form__header">
        <div>
          <h3>续费决策</h3>
          <p>记录这台 VPS 下一次续费前的处理判断。</p>
        </div>
        <RenewalBadge value={detail.renewal_decision} />
      </div>
      <label className="asset-operation-field">
        <span>续费决策</span>
        <select
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
        </select>
      </label>
      <label className="asset-operation-field asset-operation-field--wide">
        <span>决策理由</span>
        <textarea
          aria-label="决策理由"
          value={draft.reason}
          disabled={submitting}
          onChange={(event) => {
            onDraftChange({ ...draft, reason: event.target.value })
            onFeedbackClear()
          }}
          placeholder="例如：价格上涨，迁移到首尔监控实例"
        />
      </label>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      <div className="asset-operation-actions">
        <Button type="button" variant="secondary" disabled={submitting} onClick={onCancel}>
          取消
        </Button>
        <Button type="submit" disabled={submitting || !decisionChanged}>
          {submitting ? '保存中…' : '保存续费决策'}
        </Button>
      </div>
    </form>
  )
}

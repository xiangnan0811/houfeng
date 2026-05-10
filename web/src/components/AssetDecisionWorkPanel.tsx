import type { FormEvent } from 'react'

import { Button } from './atoms'
import {
  VPS_RENEWAL_DECISION_LABELS,
  type VPSAssetRecord,
  type VPSRenewalDecision,
} from '../lib/types'
import { RenewalBadge } from '../pages/assetPageBadges'
import { renewalLabel } from '../pages/assetPageUtils'

export type AssetDecisionDraft = {
  renewalDecision: VPSRenewalDecision
  reason: string
}

type AssetDecisionWorkPanelProps = {
  selectedVPS: VPSAssetRecord | null
  decisionDraft: AssetDecisionDraft
  submitting: boolean
  error: string | null
  notice: string | null
  onDraftChange: (draft: AssetDecisionDraft) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onCancel: () => void
}

const RENEWAL_OPTIONS = Object.entries(VPS_RENEWAL_DECISION_LABELS).map(([value, label]) => ({
  value,
  label,
}))

export function AssetDecisionWorkPanel({
  selectedVPS,
  decisionDraft,
  submitting,
  error,
  notice,
  onDraftChange,
  onSubmit,
  onCancel,
}: AssetDecisionWorkPanelProps) {
  return (
    <section className="page-panel asset-decision-panel" aria-label="续费决策处理">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">DECISION</p>
          <h2>处理面板</h2>
        </div>
      </div>
      {selectedVPS ? (
        <form className="asset-operation-form" onSubmit={onSubmit}>
          <div className="asset-operation-form__header">
            <div>
              <h3>{selectedVPS.display_name}</h3>
              <p>{selectedVPS.vps_id} · 当前 {renewalLabel(selectedVPS.renewal_decision)}</p>
            </div>
            <RenewalBadge value={selectedVPS.renewal_decision} />
          </div>
          <label className="asset-operation-field">
            <span>续费决策</span>
            <select
              className="input"
              value={decisionDraft.renewalDecision}
              onChange={(event) =>
                onDraftChange({
                  ...decisionDraft,
                  renewalDecision: event.target.value as VPSRenewalDecision,
                })
              }
            >
              {RENEWAL_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
          </label>
          <label className="asset-operation-field">
            <span>决策理由</span>
            <textarea
              className="input"
              value={decisionDraft.reason}
              onChange={(event) => onDraftChange({ ...decisionDraft, reason: event.target.value })}
            />
          </label>
          {error && <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{error}</p>}
          <div className="asset-operation-actions">
            <Button type="button" variant="secondary" disabled={submitting} onClick={onCancel}>
              取消
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? '保存中…' : '保存续费决策'}
            </Button>
          </div>
        </form>
      ) : (
        <div className="asset-operation-form">
          <div className="asset-operation-form__header">
            <div>
              <h3>选择一台 VPS</h3>
              <p>从左侧队列进入处理；已确认保留、观察或已替换的资产会离开当前工作台队列。</p>
            </div>
          </div>
          {notice && <p className="asset-operation-feedback" role="status">{notice}</p>}
        </div>
      )}
    </section>
  )
}

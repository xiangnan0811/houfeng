import { type FormEvent } from 'react'

import { Button, Input } from '../../components/atoms'
import type { VPSAssetDetail } from '../../lib/types'
import type { SubscriptionDraftState } from './types'

type VPSSubscriptionFormProps = {
  detail: VPSAssetDetail
  draft: SubscriptionDraftState
  submitting: boolean
  error: string | null
  notice: string | null
  onCancel: () => void
  onDraftChange: (draft: SubscriptionDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSSubscriptionForm({
  detail,
  draft,
  submitting,
  error,
  notice,
  onCancel,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSSubscriptionFormProps) {
  function update<K extends keyof SubscriptionDraftState>(key: K, value: SubscriptionDraftState[K]) {
    onDraftChange({ ...draft, [key]: value })
    onFeedbackClear()
  }

  return (
    <form className="asset-operation-form" onSubmit={onSubmit}>
      <div className="asset-operation-form__header">
        <h3>{detail.display_name}</h3>
        <p>只补录账单事实；生命周期、用途和续费决策继续归 VPS 管理。</p>
      </div>

      <div className="asset-operation-form__grid">
        <Input
          label="价格"
          type="number"
          min="0"
          step="0.01"
          value={draft.price}
          onChange={(event) => update('price', event.target.value)}
          required
        />
        <Input
          label="币种"
          value={draft.currency}
          onChange={(event) => update('currency', event.target.value)}
          required
        />
        <Input
          label="计费周期"
          value={draft.billingCycle}
          onChange={(event) => update('billingCycle', event.target.value)}
          placeholder="monthly / annual"
        />
        <Input
          label="计费月数"
          type="number"
          min="1"
          value={draft.billingMonths}
          onChange={(event) => update('billingMonths', event.target.value)}
          required
        />
        <Input
          label="开始日期"
          type="date"
          value={draft.startedAt}
          onChange={(event) => update('startedAt', event.target.value)}
        />
        <Input
          label="续费日期"
          type="date"
          value={draft.renewAt}
          onChange={(event) => update('renewAt', event.target.value)}
        />
      </div>

      <div className="asset-operation-form__checks">
        <label className="ck">
          <input
            type="checkbox"
            checked={draft.autoRenew}
            onChange={(event) => update('autoRenew', event.target.checked)}
          />
          <span className="ck-box" /> 自动续费
        </label>
        <label className="ck">
          <input
            type="checkbox"
            checked={draft.autoRenewCancelled}
            onChange={(event) => update('autoRenewCancelled', event.target.checked)}
          />
          <span className="ck-box" /> 已取消自动续费
        </label>
      </div>

      <Input
        label="支付方式"
        value={draft.paymentMethod}
        onChange={(event) => update('paymentMethod', event.target.value)}
      />
      <Input
        label="备注"
        value={draft.note}
        onChange={(event) => update('note', event.target.value)}
      />

      {error ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{error}</p> : null}
      {notice ? <p className="asset-operation-feedback" role="status">{notice}</p> : null}

      <div className="page-form-actions">
        <Button variant="secondary" disabled={submitting} onClick={onCancel}>取消</Button>
        <Button type="submit" disabled={submitting}>{submitting ? '创建中…' : '创建订阅'}</Button>
      </div>
    </form>
  )
}

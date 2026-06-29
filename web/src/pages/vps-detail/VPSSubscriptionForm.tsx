import { type FormEvent } from 'react'

import { Button, Input, Select } from '../../components/atoms'
import {
  BILLING_PERIOD_UNIT_OPTIONS,
  COMMON_CURRENCY_OPTIONS,
  COMMON_PAYMENT_METHOD_OPTIONS,
  CUSTOM_OPTION_VALUE,
  RENEWAL_MODE_OPTIONS,
  displayOption,
} from '../../lib/assetOptions'
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
        <Select label="币种" value={draft.currency} onChange={(event) => update('currency', event.target.value)} required>
          {COMMON_CURRENCY_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{displayOption(option)}</option>
          ))}
          <option value={CUSTOM_OPTION_VALUE}>自定义币种</option>
        </Select>
        {draft.currency === CUSTOM_OPTION_VALUE ? (
          <Input
            label="自定义币种"
            value={draft.customCurrency}
            onChange={(event) => update('customCurrency', event.target.value)}
            placeholder="例如：JPY"
            required
          />
        ) : null}
        <Select
          label="计费周期单位"
          value={draft.billingPeriodUnit}
          onChange={(event) => update('billingPeriodUnit', event.target.value as SubscriptionDraftState['billingPeriodUnit'])}
        >
          {BILLING_PERIOD_UNIT_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{displayOption(option)}</option>
          ))}
        </Select>
        <Input
          label="计费周期长度"
          type="number"
          min="1"
          value={draft.billingPeriodLength}
          onChange={(event) => update('billingPeriodLength', event.target.value)}
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
        <Select label="支付方式" value={draft.paymentMethod} onChange={(event) => update('paymentMethod', event.target.value)}>
          <option value="">未记录</option>
          {COMMON_PAYMENT_METHOD_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{displayOption(option)}</option>
          ))}
          <option value={CUSTOM_OPTION_VALUE}>自定义支付方式</option>
        </Select>
        {draft.paymentMethod === CUSTOM_OPTION_VALUE ? (
          <Input
            label="自定义支付方式"
            value={draft.customPaymentMethod}
            onChange={(event) => update('customPaymentMethod', event.target.value)}
          />
        ) : null}
      </div>

      <div className="asset-operation-form__section">
        <div className="input-field__label">续费方式</div>
        <div className="asset-option-grid" role="radiogroup" aria-label="续费方式">
          {RENEWAL_MODE_OPTIONS.map((option) => (
            <label key={option.value} className="asset-option-radio">
              <input
                type="radio"
                name="vps-subscription-renewal-mode"
                value={option.value}
                aria-label={option.label}
                checked={draft.renewalMode === option.value}
                onChange={() => update('renewalMode', option.value)}
              />
              <span className="asset-option-radio__icon" aria-hidden="true">{option.icon}</span>
              <span className="asset-option-radio__label">{option.label}</span>
            </label>
          ))}
        </div>
      </div>

      <Input
        label="备注"
        value={draft.note}
        onChange={(event) => update('note', event.target.value)}
      />

      {error ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{error}</p> : null}
      {notice ? <p className="asset-operation-feedback" role="status">{notice}</p> : null}

      <div className="page-form-actions">
        <Button variant="secondary" disabled={submitting} onClick={onCancel}>取消</Button>
        <Button type="submit" disabled={submitting}>{submitting ? '保存中…' : '创建/更新订阅'}</Button>
      </div>
    </form>
  )
}

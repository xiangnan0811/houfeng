import { type FormEvent } from 'react'

import { Input, Select } from '../../components/atoms'
import {
  BILLING_PERIOD_UNIT_OPTIONS,
  COMMON_CURRENCY_OPTIONS,
  COMMON_PAYMENT_METHOD_OPTIONS,
  CUSTOM_OPTION_VALUE,
  RENEWAL_MODE_OPTIONS,
} from '../../lib/assetOptions'
import type { VPSAssetDetail } from '../../lib/types'
import type { SubscriptionDraftState } from './types'
import { VPSFormSection } from './VPSDetailDialog'

type VPSSubscriptionFormProps = {
  formId: string
  detail: VPSAssetDetail
  draft: SubscriptionDraftState
  submitting: boolean
  onDraftChange: (draft: SubscriptionDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

function localDateInputValue(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

export function VPSSubscriptionForm({
  formId,
  detail,
  draft,
  submitting,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSSubscriptionFormProps) {
  function update<K extends keyof SubscriptionDraftState>(key: K, value: SubscriptionDraftState[K]) {
    onDraftChange({ ...draft, [key]: value })
    onFeedbackClear()
  }
  const vpsExpiry = detail.validity_mode === 'fixed' && detail.expires_at ? detail.expires_at.slice(0, 10) : ''

  return (
    <form id={formId} className="vps-form" onSubmit={onSubmit} aria-busy={submitting}>
      <p className="vps-context">{detail.display_name}</p>

      <VPSFormSection title="名称">
        <div className="vps-wide">
          {/* 未改过时沿用 VPS 名称；清空表示不命名，不会被再次填回。 */}
          <Input
            label="订阅名称"
            value={draft.displayName ?? detail.display_name}
            onChange={(event) => update('displayName', event.target.value)}
          />
        </div>
        <p className="vps-form-hint">默认使用 VPS 名称，可改为套餐或账单名，例如“东京 2C4G 年付”。</p>
      </VPSFormSection>

      <VPSFormSection title="金额">
        <div className="vps-inline">
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
              <option key={option.value} value={option.value}>{option.label}</option>
            ))}
            <option value={CUSTOM_OPTION_VALUE}>自定义币种</option>
          </Select>
        </div>
        {draft.currency === CUSTOM_OPTION_VALUE ? (
          <div className="vps-wide">
            <Input
              label="自定义币种"
              value={draft.customCurrency}
              onChange={(event) => update('customCurrency', event.target.value)}
              placeholder="例如：JPY"
              required
            />
          </div>
        ) : null}
      </VPSFormSection>

      <VPSFormSection title="计费周期">
        <div className="vps-inline">
          <span>每</span>
          <div className="vps-short">
            <Input
              aria-label="计费周期长度"
              type="number"
              min="1"
              value={draft.billingPeriodLength}
              onChange={(event) => update('billingPeriodLength', event.target.value)}
              required
            />
          </div>
          <div className="vps-short">
            <Select
              aria-label="计费周期单位"
              value={draft.billingPeriodUnit}
              onChange={(event) => update('billingPeriodUnit', event.target.value as SubscriptionDraftState['billingPeriodUnit'])}
            >
              {BILLING_PERIOD_UNIT_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </Select>
          </div>
        </div>
      </VPSFormSection>

      <VPSFormSection title="日期">
        <div className="vps-form-grid">
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
        {/* 未知日期保持为空，不替用户写入猜测值；VPS 已登记到期日时只提供一键填入。 */}
        {/* 开始日期决定成本趋势入哪个月，补录时默认今天会把成本挤进当月，所以只在用户点击时填入。 */}
        {!draft.startedAt ? (
          <p className="vps-form-hint">
            <button type="button" className="text-link" disabled={submitting} onClick={() => update('startedAt', localDateInputValue(new Date()))}>
              开始日期填今天
            </button>
          </p>
        ) : null}
        {vpsExpiry && draft.renewAt !== vpsExpiry ? (
          <p className="vps-form-hint">
            <button type="button" className="text-link" disabled={submitting} onClick={() => update('renewAt', vpsExpiry)}>
              使用 VPS 到期日 {vpsExpiry}
            </button>
          </p>
        ) : null}
      </VPSFormSection>

      <VPSFormSection title="支付">
        <div className="vps-form-grid">
          <Select label="支付方式" value={draft.paymentMethod} onChange={(event) => update('paymentMethod', event.target.value)}>
            <option value="">未记录</option>
            {COMMON_PAYMENT_METHOD_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>{option.label}</option>
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
        <fieldset className="vps-fieldset">
          <legend className="input-field__label">续费方式</legend>
          <div className="vps-inline" role="radiogroup" aria-label="续费方式">
            {RENEWAL_MODE_OPTIONS.map((option) => (
              <label key={option.value}>
                <input
                  type="radio"
                  name="vps-subscription-renewal-mode"
                  value={option.value}
                  aria-label={option.label}
                  checked={draft.renewalMode === option.value}
                  onChange={() => update('renewalMode', option.value)}
                />
                {option.label}
              </label>
            ))}
          </div>
        </fieldset>
      </VPSFormSection>

      <Input
        label="备注"
        value={draft.note}
        onChange={(event) => update('note', event.target.value)}
      />
    </form>
  )
}

import { type FormEvent } from 'react'

import { Input, Select } from '../../components/atoms'
import {
  COMMON_CURRENCY_OPTIONS,
  CUSTOM_OPTION_VALUE,
  VALIDITY_EXTENSION_SOURCE_OPTIONS,
  displayOption,
} from '../../lib/assetOptions'
import { formatDate, formatMoney } from '../../lib/format'
import type { SubscriptionRecord, VPSAssetDetail } from '../../lib/types'
import type { ValidityExtensionDraftState } from './types'

type VPSValidityExtensionFormProps = {
  formId: string
  detail: VPSAssetDetail
  activeSubscription: SubscriptionRecord | null
  draft: ValidityExtensionDraftState
  submitting: boolean
  onDraftChange: (draft: ValidityExtensionDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSValidityExtensionForm({
  formId,
  detail,
  activeSubscription,
  draft,
  submitting,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSValidityExtensionFormProps) {
  function update<K extends keyof ValidityExtensionDraftState>(key: K, value: ValidityExtensionDraftState[K]) {
    onDraftChange({ ...draft, [key]: value })
    onFeedbackClear()
  }

  return (
    <form id={formId} className="vps-form" onSubmit={onSubmit} aria-busy={submitting}>
      <p className="vps-context">{detail.display_name}</p>
      <p className="vps-context">当前 VPS 有效期：{detail.validity_mode === 'unlimited' ? '无固定期限' : detail.validity_mode === 'fixed' ? formatDate(detail.expires_at) : '未知'}。确认新的有效期后保存，不改变续费意向。</p>
      <p className="vps-context">
        当前生效中订阅：
        {' '}
        {activeSubscription
          ? `${formatMoney(activeSubscription.price, activeSubscription.currency)} · 续费日 ${formatDate(activeSubscription.renew_at)}`
          : '未记录订阅，仍可独立更新 VPS 有效期。'}
      </p>
      {activeSubscription?.renew_at && draft.extendTo && activeSubscription.renew_at.slice(0, 10) !== draft.extendTo ? <p className="vps-context">VPS 到期日与账单续费日不同，请分别核对。</p> : null}

      <div className="vps-form-grid">
        <Input
          label="延长至日期"
          type="date"
          value={draft.extendTo}
          disabled={submitting}
          onChange={(event) => update('extendTo', event.target.value)}
          required
        />
        <Select label="来源类型" value={draft.sourceType} disabled={submitting} onChange={(event) => update('sourceType', event.target.value)}>
          {VALIDITY_EXTENSION_SOURCE_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{displayOption(option)}</option>
          ))}
          <option value={CUSTOM_OPTION_VALUE}>自定义来源</option>
        </Select>
        {draft.sourceType === CUSTOM_OPTION_VALUE ? (
          <Input
            label="自定义来源"
            disabled={submitting}
            value={draft.customSourceType}
            onChange={(event) => update('customSourceType', event.target.value)}
          />
        ) : null}
        <Input
          label="延长费用"
          disabled={submitting}
          type="number"
          min="0"
          step="0.01"
          value={draft.fee}
          onChange={(event) => update('fee', event.target.value)}
        />
        <Select label="费用币种" value={draft.currency} disabled={submitting} onChange={(event) => update('currency', event.target.value)}>
          {COMMON_CURRENCY_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{displayOption(option)}</option>
          ))}
          <option value={CUSTOM_OPTION_VALUE}>自定义币种</option>
        </Select>
        {draft.currency === CUSTOM_OPTION_VALUE ? (
          <Input
            label="自定义币种"
            disabled={submitting}
            value={draft.customCurrency}
            onChange={(event) => update('customCurrency', event.target.value)}
            placeholder="例如：JPY"
          />
        ) : null}
      </div>

      <Input
        label="延长原因"
        disabled={submitting}
        value={draft.reason}
        onChange={(event) => update('reason', event.target.value)}
        placeholder="例如：机房故障补偿 7 天"
        required
      />
    </form>
  )
}

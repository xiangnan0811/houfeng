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
      <p className="vps-context">
        当前生效中订阅：
        {' '}
        {activeSubscription
          ? `${formatMoney(activeSubscription.price, activeSubscription.currency)} · 续费日 ${formatDate(activeSubscription.renew_at)}`
          : '未找到。需要先补录或恢复一个生效中订阅。'}
      </p>

      <div className="vps-form-grid">
        <Input
          label="延长至日期"
          type="date"
          value={draft.extendTo}
          onChange={(event) => update('extendTo', event.target.value)}
          required
        />
        <Select label="来源类型" value={draft.sourceType} onChange={(event) => update('sourceType', event.target.value)}>
          {VALIDITY_EXTENSION_SOURCE_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{displayOption(option)}</option>
          ))}
          <option value={CUSTOM_OPTION_VALUE}>自定义来源</option>
        </Select>
        {draft.sourceType === CUSTOM_OPTION_VALUE ? (
          <Input
            label="自定义来源"
            value={draft.customSourceType}
            onChange={(event) => update('customSourceType', event.target.value)}
          />
        ) : null}
        <Input
          label="延长费用"
          type="number"
          min="0"
          step="0.01"
          value={draft.fee}
          onChange={(event) => update('fee', event.target.value)}
        />
        <Select label="费用币种" value={draft.currency} onChange={(event) => update('currency', event.target.value)}>
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
          />
        ) : null}
      </div>

      <Input
        label="延长原因"
        value={draft.reason}
        onChange={(event) => update('reason', event.target.value)}
        placeholder="例如：机房故障补偿 7 天"
        required
      />
    </form>
  )
}

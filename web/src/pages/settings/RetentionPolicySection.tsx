import { DetailSection } from '../../components/DetailSection'
import { Input } from '../../components/atoms'
import type { SettingsRetentionPolicyForm } from './types'
import { SectionIntro } from './SectionIntro'

type RetentionPolicySectionProps = {
  value: SettingsRetentionPolicyForm
  onChange: (patch: Partial<SettingsRetentionPolicyForm>) => void
}

type RetentionInputProps = {
  ariaLabel: string
  value: string
  onChange: (value: string) => void
}

function RetentionInput({ ariaLabel, value, onChange }: RetentionInputProps) {
  return (
    <Input
      label={ariaLabel}
      inputMode="numeric"
      value={value}
      trailingIcon="天"
      onChange={(event) => onChange(event.target.value)}
    />
  )
}

export function RetentionPolicySection({ value, onChange }: RetentionPolicySectionProps) {
  return (
    <DetailSection eyebrow="保留策略" title="数据保留策略" ribbon="notice">
      <div className="settings-form-grid settings-form-grid--tight">
        <RetentionInput
          ariaLabel="原始层保留天数"
          value={value.rawLayerDays}
          onChange={(nextValue) => onChange({ rawLayerDays: nextValue })}
        />
        <RetentionInput
          ariaLabel="聚合层保留天数"
          value={value.aggregateLayerDays}
          onChange={(nextValue) => onChange({ aggregateLayerDays: nextValue })}
        />
        <RetentionInput
          ariaLabel="事件层保留天数"
          value={value.eventLayerDays}
          onChange={(nextValue) => onChange({ eventLayerDays: nextValue })}
        />
        <RetentionInput
          ariaLabel="通知层保留天数"
          value={value.notificationLayerDays}
          onChange={(nextValue) => onChange({ notificationLayerDays: nextValue })}
        />
      </div>
      <SectionIntro>中心后台会按这些窗口自动清理原始观测、事件和通知记录，并维护日级聚合数据作为后续趋势与摘要基础；窗口变更只在保存后进入持久化策略。</SectionIntro>
    </DetailSection>
  )
}

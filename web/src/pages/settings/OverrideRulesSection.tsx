import { DetailSection } from '../../components/DetailSection'
import type { SettingsFormState } from './types'
import { SectionIntro } from './SectionIntro'

const TARGET_TYPE_OPTIONS = [
  { value: 'service', label: 'service' },
  { value: 'china_reference', label: 'china_reference' },
] as const

type OverrideRulesForm = Pick<
  SettingsFormState,
  'nodeLabelOverridesText' | 'targetTypeOverridesText' | 'targetLabelOverridesText'
>

type OverrideRulesSectionProps = {
  form: OverrideRulesForm
  onChange: (patch: Partial<OverrideRulesForm>) => void
}

type OverrideTextareaProps = {
  ariaLabel: string
  value: string
  onChange: (value: string) => void
}

function OverrideTextarea({ ariaLabel, value, onChange }: OverrideTextareaProps) {
  let previewContent: string | null = null
  if (value.trim()) {
    try {
      previewContent = JSON.stringify(JSON.parse(value), null, 2)
    } catch {
      previewContent = null
    }
  }

  return (
    <div className="input-field override-rule-field">
      <label className="input-field__label">{ariaLabel}</label>
      <div className="input-field__shell">
        <textarea
          aria-label={ariaLabel}
          className="input mono override-rule-field__textarea"
          rows={10}
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
      </div>
      {previewContent ? (
        <details className="override-rule-preview">
          <summary>预览</summary>
          <pre><code>{previewContent}</code></pre>
        </details>
      ) : null}
    </div>
  )
}

function TargetTypeSummary() {
  return (
    <p className="empty-inline">
      允许的目标类型选择器：{TARGET_TYPE_OPTIONS.map((option) => option.value).join('、')}。
    </p>
  )
}

export function OverrideRulesSection({ form, onChange }: OverrideRulesSectionProps) {
  return (
    <DetailSection eyebrow="覆盖规则" title="少量覆盖规则" ribbon="notice" aside={<TargetTypeSummary />}>
      <SectionIntro>
        仅保留节点标签、目标类型、目标标签三类结构化覆盖，不扩展为通用规则引擎。当前频率相关覆盖已接入实时规划链；异常默认覆盖仍仅作为持久化策略保存，并在页尾统一提交前校验 JSON 数组。
      </SectionIntro>
      <div className="settings-cluster">
        <OverrideTextarea
          ariaLabel="节点标签覆盖规则 JSON"
          value={form.nodeLabelOverridesText}
          onChange={(value) => onChange({ nodeLabelOverridesText: value })}
        />
        <OverrideTextarea
          ariaLabel="目标类型覆盖规则 JSON"
          value={form.targetTypeOverridesText}
          onChange={(value) => onChange({ targetTypeOverridesText: value })}
        />
        <OverrideTextarea
          ariaLabel="目标标签覆盖规则 JSON"
          value={form.targetLabelOverridesText}
          onChange={(value) => onChange({ targetLabelOverridesText: value })}
        />
      </div>
    </DetailSection>
  )
}

import { useState } from 'react'

import type { SettingsFormState } from './types'

type OverrideRulesForm = Pick<
  SettingsFormState,
  'monitoringInstanceLabelOverridesText' | 'targetTypeOverridesText' | 'targetLabelOverridesText'
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
  const [error, setError] = useState<string | null>(null)

  function handleChange(newValue: string) {
    onChange(newValue)
    if (newValue.trim() === '' || newValue.trim() === '[]') {
      setError(null)
      return
    }
    try {
      const parsed = JSON.parse(newValue)
      if (!Array.isArray(parsed)) {
        setError('必须是 JSON 数组')
      } else {
        setError(null)
      }
    } catch {
      setError('JSON 格式无效')
    }
  }

  function handleFormat() {
    try {
      const parsed = JSON.parse(value)
      onChange(JSON.stringify(parsed, null, 2))
      setError(null)
    } catch {
      setError('无法格式化：JSON 格式无效')
    }
  }

  return (
    <div className="settings-row settings-row--block">
      <div className="sr-label-row">
        <span className="sr-label">{ariaLabel}</span>
        <button type="button" className="btn sm ghost" onClick={handleFormat}>格式化</button>
      </div>
      <textarea
        aria-label={ariaLabel}
        className={`input mono input--compact override-textarea${error ? ' input--error' : ''}`}
        rows={6}
        value={value}
        onChange={(event) => handleChange(event.target.value)}
      />
      {error && <p className="override-error">{error}</p>}
    </div>
  )
}

export function OverrideRulesSection({ form, onChange }: OverrideRulesSectionProps) {
  return (
    <>
      <div className="ss-title">覆盖规则</div>
      <div className="ss-desc">监控实例标签、目标类型、目标标签结构化覆盖</div>
      <OverrideTextarea
        ariaLabel="监控实例标签覆盖规则 JSON"
        value={form.monitoringInstanceLabelOverridesText}
        onChange={(value) => onChange({ monitoringInstanceLabelOverridesText: value })}
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
    </>
  )
}

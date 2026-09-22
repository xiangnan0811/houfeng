import type { FormEvent } from 'react'

import { Input, Select } from '../../components/atoms'
import type { VPSExperienceCategory, VPSExperienceSeverity, VPSTimeline } from '../../lib/types'
import type { ExperienceDraftState } from './types'
import { EXPERIENCE_CATEGORY_OPTIONS, EXPERIENCE_SEVERITY_OPTIONS } from './vpsDetailOptions'

type VPSExperienceLogFormProps = {
  formId: string
  timeline: VPSTimeline
  draft: ExperienceDraftState
  submitting: boolean
  onDraftChange: (draft: ExperienceDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSExperienceLogForm({
  formId,
  timeline,
  draft,
  submitting,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSExperienceLogFormProps) {
  return (
    <form id={formId} className="vps-form" onSubmit={onSubmit} aria-busy={submitting}>
      <p className="vps-context">{timeline.experience_logs.length} 条</p>
      <div className="vps-form-grid">
        <Select
          label="分类"
          value={draft.category}
          onChange={(event) => {
            onDraftChange({
              ...draft,
              category: event.target.value as VPSExperienceCategory,
            })
            onFeedbackClear()
          }}
        >
          {EXPERIENCE_CATEGORY_OPTIONS.map(([value, label]) => (
            <option key={value} value={value}>{label}</option>
          ))}
        </Select>
        <Select
          label="级别"
          value={draft.severity}
          onChange={(event) => {
            onDraftChange({
              ...draft,
              severity: event.target.value as VPSExperienceSeverity,
            })
            onFeedbackClear()
          }}
        >
          {EXPERIENCE_SEVERITY_OPTIONS.map(([value, label]) => (
            <option key={value} value={value}>{label}</option>
          ))}
        </Select>
        <Input
          label="摘要"
          value={draft.summary}
          onChange={(event) => {
            onDraftChange({ ...draft, summary: event.target.value })
            onFeedbackClear()
          }}
          placeholder="例如：晚高峰丢包明显"
        />
        <Input
          label="发生时间"
          type="datetime-local"
          value={draft.occurredAt}
          onChange={(event) => {
            onDraftChange({ ...draft, occurredAt: event.target.value })
            onFeedbackClear()
          }}
        />
      </div>
      <label className="input-field">
        <span className="input-field__label">详情</span>
        <textarea
          className="input"
          aria-label="详情"
          value={draft.details}
          onChange={(event) => {
            onDraftChange({ ...draft, details: event.target.value })
            onFeedbackClear()
          }}
          placeholder="例如：连续三天晚高峰 tcp probe 抖动，已向服务商提交工单"
        />
      </label>
    </form>
  )
}

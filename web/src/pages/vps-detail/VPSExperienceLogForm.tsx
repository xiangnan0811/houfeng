import type { FormEvent } from 'react'

import { Badge, Button, Input } from '../../components/atoms'
import type { VPSExperienceCategory, VPSExperienceSeverity, VPSTimeline } from '../../lib/types'
import type { ExperienceDraftState } from './types'
import { EXPERIENCE_CATEGORY_OPTIONS, EXPERIENCE_SEVERITY_OPTIONS } from './vpsDetailOptions'

type VPSExperienceLogFormProps = {
  timeline: VPSTimeline
  draft: ExperienceDraftState
  submitting: boolean
  error: string | null
  notice: string | null
  onDraftChange: (draft: ExperienceDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSExperienceLogForm({
  timeline,
  draft,
  submitting,
  error,
  notice,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSExperienceLogFormProps) {
  return (
    <form className="asset-operation-form" onSubmit={onSubmit}>
      <div className="asset-operation-form__header">
        <div>
          <h3>经验记录</h3>
          <p>补充这台 VPS 的稳定性、网络、账单或迁移原因。</p>
        </div>
        <Badge variant="count" tone="neutral">{timeline.experience_logs.length} 条</Badge>
      </div>
      <label className="asset-operation-field">
        <span>分类</span>
        <select
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
        </select>
      </label>
      <label className="asset-operation-field">
        <span>级别</span>
        <select
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
        </select>
      </label>
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
      <label className="asset-operation-field asset-operation-field--wide">
        <span>详情</span>
        <textarea
          value={draft.details}
          onChange={(event) => {
            onDraftChange({ ...draft, details: event.target.value })
            onFeedbackClear()
          }}
          placeholder="例如：连续三天晚高峰 tcp probe 抖动，已向服务商提交工单"
        />
      </label>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      <div className="asset-operation-actions">
        <Button type="submit" disabled={submitting}>
          {submitting ? '记录中…' : '写入经验记录'}
        </Button>
      </div>
    </form>
  )
}

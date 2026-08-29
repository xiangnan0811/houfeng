import { type FormEvent } from 'react'

import { Button, Input } from '../../components/atoms'
import type { VPSAssetDetail } from '../../lib/types'
import type { MonitoringInstanceCreateDraftState } from './types'

type VPSMonitoringInstanceCreateFormProps = {
  detail: VPSAssetDetail
  draft: MonitoringInstanceCreateDraftState
  submitting: boolean
  submitDisabled?: boolean
  error: string | null
  notice: string | null
  onCancel: () => void
  onDraftChange: (draft: MonitoringInstanceCreateDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSMonitoringInstanceCreateForm({
  detail,
  draft,
  submitting,
  submitDisabled = false,
  error,
  notice,
  onCancel,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSMonitoringInstanceCreateFormProps) {
  function update<K extends keyof MonitoringInstanceCreateDraftState>(key: K, value: MonitoringInstanceCreateDraftState[K]) {
    onDraftChange({ ...draft, [key]: value })
    onFeedbackClear()
  }

  return (
    <form className="asset-operation-form" onSubmit={onSubmit}>
      <div className="asset-operation-form__header">
        <h3>为 {detail.display_name} 创建监控实例</h3>
        <p>已按 VPS 资料预填，必要时微调后直接创建并进入 agent 接入。</p>
      </div>
      <div className="asset-operation-form__grid">
        <Input
          label="监控实例名称"
          value={draft.displayName}
          onChange={(event) => update('displayName', event.target.value)}
          required
        />
        <Input
          label="分组"
          value={draft.group}
          onChange={(event) => update('group', event.target.value)}
          placeholder="可选"
        />
        <Input
          label="服务商"
          value={draft.provider}
          onChange={(event) => update('provider', event.target.value)}
        />
        <Input
          label="区域"
          value={draft.region}
          onChange={(event) => update('region', event.target.value)}
        />
        <Input
          label="城市"
          value={draft.city}
          onChange={(event) => update('city', event.target.value)}
        />
        <Input
          label="标签"
          value={draft.labels}
          onChange={(event) => update('labels', event.target.value)}
          placeholder="用逗号分隔"
        />
      </div>
      <Input
        label="关联备注"
        value={draft.linkNote}
        onChange={(event) => update('linkNote', event.target.value)}
      />
      <Input
        label="监控备注"
        value={draft.note}
        onChange={(event) => update('note', event.target.value)}
      />
      {error ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{error}</p> : null}
      {notice ? <p className="asset-operation-feedback" role="status">{notice}</p> : null}
      <div className="page-form-actions">
        <Button variant="secondary" disabled={submitting} onClick={onCancel}>取消</Button>
        <Button type="submit" disabled={submitting || submitDisabled}>
          {submitting ? '创建中…' : '接入/升级 agent'}
        </Button>
      </div>
    </form>
  )
}

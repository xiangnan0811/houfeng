import { type FormEvent } from 'react'

import { Input } from '../../components/atoms'
import type { VPSAssetDetail } from '../../lib/types'
import type { MonitoringInstanceCreateDraftState } from './types'

type VPSMonitoringInstanceCreateFormProps = {
  formId: string
  detail: VPSAssetDetail
  draft: MonitoringInstanceCreateDraftState
  submitting: boolean
  onDraftChange: (draft: MonitoringInstanceCreateDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSMonitoringInstanceCreateForm({
  formId,
  detail,
  draft,
  submitting,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSMonitoringInstanceCreateFormProps) {
  function update<K extends keyof MonitoringInstanceCreateDraftState>(key: K, value: MonitoringInstanceCreateDraftState[K]) {
    onDraftChange({ ...draft, [key]: value })
    onFeedbackClear()
  }

  return (
    <form id={formId} className="vps-form" onSubmit={onSubmit} aria-busy={submitting}>
      <p className="vps-context">{detail.display_name}</p>
      <div className="vps-form-grid">
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
    </form>
  )
}

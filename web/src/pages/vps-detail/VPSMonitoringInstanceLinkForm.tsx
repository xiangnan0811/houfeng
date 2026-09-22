import { type FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { Select } from '../../components/atoms'
import type { MonitoringInstanceRecord, VPSAssetDetail } from '../../lib/types'
import type { LinkDraftState } from './types'

type VPSMonitoringInstanceLinkFormProps = {
  formId: string
  detail: VPSAssetDetail
  draft: LinkDraftState
  monitoring: MonitoringInstanceRecord[]
  monitoringInstancesLoading: boolean
  monitoringInstancesError: string | null
  controlsDisabled: boolean
  submitting: boolean
  onDraftChange: (draft: LinkDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSMonitoringInstanceLinkForm({
  formId,
  detail,
  draft,
  monitoring,
  monitoringInstancesLoading,
  monitoringInstancesError,
  controlsDisabled,
  submitting,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSMonitoringInstanceLinkFormProps) {
  const linkedMonitoringInstanceIDs = new Set(detail.monitoring_instance_links.map((monitoringInstance) => monitoringInstance.monitoring_instance_id))
  const selectableMonitoringInstances = monitoring.filter((monitoringInstance) => !linkedMonitoringInstanceIDs.has(monitoringInstance.monitoring_instance_id))

  const listHint = monitoringInstancesLoading
    ? '正在读取监控实例列表…'
    : monitoringInstancesError
      ? `监控实例列表不可用：${monitoringInstancesError}`
      : selectableMonitoringInstances.length === 0
        ? '没有可关联的既有监控实例；普通 agent 接入请使用 VPS 详情页的“接入/升级 agent”。'
        : '高级关联只补建 VPS 与既有监控实例之间的关系，不重复采集服务商、位置或业务状态。'

  return (
    <form id={formId} className="vps-form" onSubmit={onSubmit} aria-busy={submitting}>
      <p className="vps-context">{detail.monitoring_instance_links.length} 个监控实例</p>
      <Select
        label="选择监控实例"
        value={draft.monitoringInstanceId}
        disabled={controlsDisabled || monitoringInstancesLoading || selectableMonitoringInstances.length === 0}
        onChange={(event) => {
          onDraftChange({ ...draft, monitoringInstanceId: event.target.value })
          onFeedbackClear()
        }}
        hint={(
          <>
            {listHint}
            {' '}
            <Link className="text-link" to="/monitoring">监控实例列表</Link>
          </>
        )}
      >
        <option value="">选择现有监控实例</option>
        {selectableMonitoringInstances.map((monitoringInstance) => (
          <option key={monitoringInstance.monitoring_instance_id} value={monitoringInstance.monitoring_instance_id}>
            {monitoringInstance.display_name} · {monitoringInstance.monitoring_instance_id} · {monitoringInstance.provider || 'provider 未填'} · {monitoringInstance.lifecycle_status} / {monitoringInstance.current_health_status}
          </option>
        ))}
      </Select>
      <label className="input-field">
        <span className="input-field__label">关联备注</span>
        <textarea
          className="input"
          aria-label="关联备注"
          value={draft.note}
          onChange={(event) => {
            onDraftChange({ ...draft, note: event.target.value })
            onFeedbackClear()
          }}
          placeholder="例如：主业务监控实例"
          disabled={controlsDisabled}
        />
      </label>
    </form>
  )
}

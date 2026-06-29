import { useId, type FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { Badge, Button } from '../../components/atoms'
import type { MonitoringInstanceRecord, VPSAssetDetail } from '../../lib/types'
import type { LinkDraftState } from './types'

type VPSMonitoringInstanceLinkFormProps = {
  detail: VPSAssetDetail
  draft: LinkDraftState
  monitoring: MonitoringInstanceRecord[]
  monitoringInstancesLoading: boolean
  monitoringInstancesError: string | null
  controlsDisabled: boolean
  submitting: boolean
  error: string | null
  notice: string | null
  onCancel: () => void
  onDraftChange: (draft: LinkDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSMonitoringInstanceLinkForm({
  detail,
  draft,
  monitoring,
  monitoringInstancesLoading,
  monitoringInstancesError,
  controlsDisabled,
  submitting,
  error,
  notice,
  onCancel,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSMonitoringInstanceLinkFormProps) {
  const monitoringInstanceSelectId = useId()
  const noteId = useId()
  const linkedMonitoringInstanceIDs = new Set(detail.monitoring_instance_links.map((monitoringInstance) => monitoringInstance.monitoring_instance_id))
  const selectableMonitoringInstances = monitoring.filter((monitoringInstance) => !linkedMonitoringInstanceIDs.has(monitoringInstance.monitoring_instance_id))

  return (
    <form className="asset-operation-form" onSubmit={onSubmit}>
      <div className="asset-operation-form__header">
        <div>
          <h3>关联监控实例</h3>
          <p>把资产台账中的 VPS 与观测系统中的监控实例对齐。</p>
        </div>
        <Badge variant="count" tone="neutral">{detail.monitoring_instance_links.length} 个监控实例</Badge>
      </div>
      <label className="asset-operation-field" htmlFor={monitoringInstanceSelectId}>
        <span>选择监控实例</span>
        <select
          id={monitoringInstanceSelectId}
          aria-label="选择监控实例"
          value={draft.monitoringInstanceId}
          disabled={controlsDisabled || monitoringInstancesLoading || selectableMonitoringInstances.length === 0}
          onChange={(event) => {
            onDraftChange({ ...draft, monitoringInstanceId: event.target.value })
            onFeedbackClear()
          }}
        >
          <option value="">选择现有监控实例</option>
          {selectableMonitoringInstances.map((monitoringInstance) => (
            <option key={monitoringInstance.monitoring_instance_id} value={monitoringInstance.monitoring_instance_id}>
              {monitoringInstance.display_name} · {monitoringInstance.monitoring_instance_id} · {monitoringInstance.provider || 'provider 未填'} · {monitoringInstance.lifecycle_status} / {monitoringInstance.current_health_status}
            </option>
          ))}
        </select>
        <small>
          {monitoringInstancesLoading
            ? '正在读取监控实例列表…'
            : monitoringInstancesError
              ? `监控实例列表不可用：${monitoringInstancesError}`
              : selectableMonitoringInstances.length === 0
                ? '没有可关联的既有监控实例；普通 agent 接入请使用 VPS 详情页的“接入/升级 agent”。'
                : '高级关联只补建 VPS 与既有监控实例之间的关系，不重复采集服务商、位置或业务状态。'}
          {' '}
          <Link className="text-link" to="/monitoring">监控实例列表</Link>
        </small>
      </label>
      <label className="asset-operation-field asset-operation-field--wide" htmlFor={noteId}>
        <span>关联备注</span>
        <textarea
          id={noteId}
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
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      <div className="asset-operation-actions">
        <Button type="button" variant="secondary" disabled={submitting} onClick={onCancel}>
          取消
        </Button>
        <Button type="submit" disabled={controlsDisabled}>
          {submitting ? '关联中…' : '关联监控实例'}
        </Button>
      </div>
    </form>
  )
}

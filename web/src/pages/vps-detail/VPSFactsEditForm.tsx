import type { FormEvent } from 'react'

import { Button, Input } from '../../components/atoms'
import type { VPSLifecycleStatus, VPSUsageStatus } from '../../lib/types'
import type { FactEditFormState } from './types'
import { LIFECYCLE_OPTIONS, USAGE_OPTIONS } from './vpsDetailOptions'

type VPSFactsEditFormProps = {
  draft: FactEditFormState
  submitting: boolean
  error: string | null
  notice: string | null
  onCancel: () => void
  onDraftChange: (draft: FactEditFormState) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSFactsEditForm({
  draft,
  submitting,
  error,
  notice,
  onCancel,
  onDraftChange,
  onSubmit,
}: VPSFactsEditFormProps) {
  return (
    <form className="asset-facts-edit-form" onSubmit={onSubmit}>
      <Input label="VPS 名称" value={draft.displayName} onChange={(event) => onDraftChange({ ...draft, displayName: event.target.value })} />
      <Input label="Provider ID" value={draft.providerID} onChange={(event) => onDraftChange({ ...draft, providerID: event.target.value })} />
      <Input label="服务商名称快照" value={draft.providerName} onChange={(event) => onDraftChange({ ...draft, providerName: event.target.value })} />
      <Input label="产品名" value={draft.productName} onChange={(event) => onDraftChange({ ...draft, productName: event.target.value })} />
      <Input label="订单号" value={draft.orderRef} onChange={(event) => onDraftChange({ ...draft, orderRef: event.target.value })} />
      <Input label="国家 / 地区" value={draft.country} onChange={(event) => onDraftChange({ ...draft, country: event.target.value })} />
      <Input label="区域" value={draft.region} onChange={(event) => onDraftChange({ ...draft, region: event.target.value })} />
      <Input label="城市" value={draft.city} onChange={(event) => onDraftChange({ ...draft, city: event.target.value })} />
      <Input label="数据中心" value={draft.datacenter} onChange={(event) => onDraftChange({ ...draft, datacenter: event.target.value })} />
      <Input label="IPv4" value={draft.ipv4} onChange={(event) => onDraftChange({ ...draft, ipv4: event.target.value })} />
      <Input label="IPv6" value={draft.ipv6} onChange={(event) => onDraftChange({ ...draft, ipv6: event.target.value })} />
      <Input label="SSH Host" value={draft.sshHost} onChange={(event) => onDraftChange({ ...draft, sshHost: event.target.value })} />
      <Input label="SSH 端口" type="number" min="1" max="65535" value={draft.sshPort} onChange={(event) => onDraftChange({ ...draft, sshPort: event.target.value })} />
      <Input label="SSH 用户" value={draft.sshUser} onChange={(event) => onDraftChange({ ...draft, sshUser: event.target.value })} />
      <Input label="操作系统" value={draft.osName} onChange={(event) => onDraftChange({ ...draft, osName: event.target.value })} />
      <Input label="虚拟化" value={draft.virtualization} onChange={(event) => onDraftChange({ ...draft, virtualization: event.target.value })} />
      <label className="input-field">
        <span className="input-field__label">生命周期</span>
        <select
          className="input"
          value={draft.lifecycleStatus}
          onChange={(event) => onDraftChange({
            ...draft,
            lifecycleStatus: event.target.value as VPSLifecycleStatus,
          })}
        >
          {LIFECYCLE_OPTIONS.map(([value, label]) => (
            <option key={value} value={value}>{label}</option>
          ))}
        </select>
      </label>
      <label className="input-field">
        <span className="input-field__label">用途状态</span>
        <select
          className="input"
          value={draft.usageStatus}
          onChange={(event) => onDraftChange({
            ...draft,
            usageStatus: event.target.value as VPSUsageStatus,
          })}
        >
          {USAGE_OPTIONS.map(([value, label]) => (
            <option key={value} value={value}>{label}</option>
          ))}
        </select>
      </label>
      <Input label="重要性" value={draft.importance} onChange={(event) => onDraftChange({ ...draft, importance: event.target.value })} />
      <Input label="标签" hint="用逗号分隔" value={draft.labels} onChange={(event) => onDraftChange({ ...draft, labels: event.target.value })} />
      <Input label="备注" value={draft.note} onChange={(event) => onDraftChange({ ...draft, note: event.target.value })} />
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      <div className="page-form-actions">
        <Button type="button" variant="secondary" disabled={submitting} onClick={onCancel}>
          取消编辑
        </Button>
        <Button type="submit" disabled={submitting}>
          {submitting ? '保存中…' : '保存基础信息'}
        </Button>
      </div>
    </form>
  )
}

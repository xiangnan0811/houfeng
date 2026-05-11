import type { FormEvent } from 'react'

import { Button, Input } from '../../components/atoms'
import { formatDateTime } from '../../lib/format'
import type { VPSAssetDetail, VPSLifecycleStatus, VPSUsageStatus } from '../../lib/types'
import { AssetLabels } from '../assetPageBadges'
import type { FactEditFormState } from './types'
import { VPSDetailItem } from './VPSDetailItem'
import { LIFECYCLE_OPTIONS, USAGE_OPTIONS } from './vpsDetailOptions'

type VPSFactsSectionProps = {
  detail: VPSAssetDetail
  editOpen: boolean
  draft: FactEditFormState | null
  submitting: boolean
  error: string | null
  notice: string | null
  onToggleEdit: () => void
  onDraftChange: (draft: FactEditFormState) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSFactsSection({
  detail,
  editOpen,
  draft,
  submitting,
  error,
  notice,
  onToggleEdit,
  onDraftChange,
  onSubmit,
}: VPSFactsSectionProps) {
  return (
    <section className="page-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">FACTS</p>
          <h2>基础信息</h2>
        </div>
        <div className="section-heading__actions">
          <AssetLabels labels={detail.labels} />
          <Button variant={editOpen ? 'secondary' : 'primary'} size="sm" onClick={onToggleEdit}>
            {editOpen ? '收起编辑' : '编辑基础信息'}
          </Button>
        </div>
      </div>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      {editOpen && draft ? (
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
          <div className="page-form-actions">
            <Button type="button" variant="secondary" disabled={submitting} onClick={onToggleEdit}>
              取消编辑
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? '保存中…' : '保存基础信息'}
            </Button>
          </div>
        </form>
      ) : null}
      <dl className="asset-detail-grid">
        <VPSDetailItem label="VPS ID" value={detail.vps_id} />
        <VPSDetailItem label="Provider ID" value={detail.provider_id} />
        <VPSDetailItem label="产品名" value={detail.product_name} />
        <VPSDetailItem label="订单号" value={detail.order_ref} />
        <VPSDetailItem label="数据中心" value={detail.datacenter} />
        <VPSDetailItem label="重要性" value={detail.importance} />
        <VPSDetailItem label="IPv4" value={detail.ipv4} />
        <VPSDetailItem label="IPv6" value={detail.ipv6} />
        <VPSDetailItem label="SSH Host" value={detail.ssh_host} />
        <VPSDetailItem label="SSH 端口" value={detail.ssh_port} />
        <VPSDetailItem label="SSH 用户" value={detail.ssh_user} />
        <VPSDetailItem label="操作系统" value={detail.os_name} />
        <VPSDetailItem label="虚拟化" value={detail.virtualization} />
        <VPSDetailItem label="归档时间" value={detail.archived_at ? formatDateTime(detail.archived_at) : null} />
        <VPSDetailItem label="备注" value={detail.note} />
      </dl>
    </section>
  )
}

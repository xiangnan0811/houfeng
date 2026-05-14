import { Button } from '../../components/atoms'
import { formatDateTime } from '../../lib/format'
import type { VPSAssetDetail } from '../../lib/types'
import { AssetLabels } from '../assetPageBadges'
import { VPSDetailItem } from './VPSDetailItem'

type VPSFactsSectionProps = {
  detail: VPSAssetDetail
  error: string | null
  notice: string | null
  onEdit: () => void
}

export function VPSFactsSection({
  detail,
  error,
  notice,
  onEdit,
}: VPSFactsSectionProps) {
  return (
    <section className="page-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">BASELINE FACTS</p>
          <h2>基础信息</h2>
          <p className="section-heading__description">服务商、位置、规格与访问字段用于支撑上方资产判断，不承载复杂编辑表单。</p>
        </div>
        <div className="section-heading__actions">
          <AssetLabels labels={detail.labels} />
          <Button variant="secondary" size="sm" onClick={onEdit}>编辑基础信息</Button>
        </div>
      </div>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
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

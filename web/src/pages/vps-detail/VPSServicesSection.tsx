import { Link } from 'react-router-dom'

import { Badge, Button, DataTable, MonoDigits, type DataTableColumn } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import {
  ASSET_SERVICE_STATUS_LABELS,
  ASSET_SERVICE_TYPE_LABELS,
  type AssetServiceRecord,
} from '../../lib/types'
import { AssetLabels } from '../assetPageBadges'

type VPSServicesSectionProps = {
  services: AssetServiceRecord[]
  error: string | null
  notice: string | null
  readOnly?: boolean
  onCreate: () => void
}

export function VPSServicesSection({
  services,
  error,
  notice,
  readOnly = false,
  onCreate,
}: VPSServicesSectionProps) {
  const serviceColumns: DataTableColumn<AssetServiceRecord>[] = [
    {
      key: 'service',
      label: '服务',
      render: (service) => (
        <div className="asset-table__identity">
          <strong>{service.name}</strong>
          <span>{service.service_id}</span>
        </div>
      ),
    },
    {
      key: 'type',
      label: '类型 / 状态',
      render: (service) => (
        <span className="asset-status-stack">
          <Badge variant="info" tone="neutral">
            {ASSET_SERVICE_TYPE_LABELS[service.service_type]}
          </Badge>
          <Badge variant="count" tone={service.status === 'active' ? 'normal' : 'neutral'}>
            {ASSET_SERVICE_STATUS_LABELS[service.status]}
          </Badge>
        </span>
      ),
    },
    {
      key: 'endpoint',
      label: '入口',
      render: (service) => (
        <div className="asset-table__stack">
          <strong>{formatOptional(service.url)}</strong>
          <span>{service.port ? `端口 ${service.port}` : '端口未记录'}</span>
        </div>
      ),
    },
    {
      key: 'target',
      label: 'Target',
      render: (service) =>
        service.target_id ? (
          <Link className="text-link" to={`/targets/${service.target_id}`}>
            {service.target_id}
          </Link>
        ) : (
          <span className="asset-table__muted">未关联</span>
        ),
    },
    {
      key: 'labels',
      label: '标签',
      render: (service) => <AssetLabels labels={service.labels} />,
    },
    {
      key: 'note',
      label: '备注',
      render: (service) => formatOptional(service.note),
    },
  ]

  return (
    <section className="page-panel page-panel--scroll-x">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">SERVICE CONTEXT</p>
          <h2>服务资产</h2>
          <p className="section-heading__description">当前 VPS 的手工服务记录，只提供上下文和 Target 跳转。</p>
        </div>
        <span className="section-heading__meta">
          <MonoDigits>{services.length}</MonoDigits> 个手工记录服务
        </span>
        {!readOnly ? <Button variant="secondary" size="sm" onClick={onCreate}>新增服务</Button> : null}
      </div>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      <DataTable
        className="asset-table vps-service-table"
        columns={serviceColumns}
        rows={services}
        rowKey={(service) => service.service_id}
        emptyContent={<span className="empty-inline">尚未记录服务</span>}
      />
    </section>
  )
}

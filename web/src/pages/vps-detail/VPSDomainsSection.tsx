import { Link } from 'react-router-dom'

import { Badge, Button, DataTable, MonoDigits, type DataTableColumn } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import {
  ASSET_DOMAIN_STATUS_LABELS,
  type AssetDomainRecord,
} from '../../lib/types'
import { AssetLabels } from '../assetPageBadges'

type VPSDomainsSectionProps = {
  domains: AssetDomainRecord[]
  error: string | null
  notice: string | null
  readOnly?: boolean
  onCreate: () => void
}

export function VPSDomainsSection({
  domains,
  error,
  notice,
  readOnly = false,
  onCreate,
}: VPSDomainsSectionProps) {
  const domainColumns: DataTableColumn<AssetDomainRecord>[] = [
    {
      key: 'domain',
      label: '域名',
      render: (domain) => (
        <div className="asset-table__identity">
          <strong>{domain.domain_name}</strong>
          <span>{domain.domain_id}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '状态 / HTTPS',
      render: (domain) => (
        <span className="asset-status-stack">
          <Badge variant="count" tone={domain.status === 'active' ? 'normal' : 'neutral'}>
            {ASSET_DOMAIN_STATUS_LABELS[domain.status]}
          </Badge>
          <Badge variant="info" tone={domain.https_enabled ? 'normal' : 'neutral'}>
            {domain.https_enabled ? 'HTTPS' : '未记录 HTTPS'}
          </Badge>
        </span>
      ),
    },
    {
      key: 'purpose',
      label: '用途 / 注册商',
      render: (domain) => (
        <div className="asset-table__stack">
          <strong>{formatOptional(domain.purpose)}</strong>
          <span>{formatOptional(domain.registrar)}</span>
        </div>
      ),
    },
    {
      key: 'expires',
      label: '过期 / 续费',
      render: (domain) => (
        <div className="asset-table__stack">
          <strong>{formatOptional(domain.expires_at)}</strong>
          <span>{domain.auto_renew ? '自动续费' : '手工续费'}</span>
        </div>
      ),
    },
    {
      key: 'links',
      label: '关联',
      render: (domain) => (
        <div className="asset-table__stack">
          <span>{domain.service_id ? `服务 ${domain.service_id}` : '未关联服务'}</span>
          {domain.target_id ? (
            <Link className="text-link" to={`/targets/${domain.target_id}`}>
              Target {domain.target_id}
            </Link>
          ) : (
            <span className="asset-table__muted">未关联 Target</span>
          )}
        </div>
      ),
    },
    {
      key: 'labels',
      label: '标签',
      render: (domain) => <AssetLabels labels={domain.labels} />,
    },
    {
      key: 'note',
      label: '备注',
      render: (domain) => formatOptional(domain.note),
    },
  ]

  return (
    <section className="page-panel page-panel--scroll-x">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">DOMAIN CONTEXT</p>
          <h2>域名资产</h2>
          <p className="section-heading__description">当前 VPS 的手工域名记录，不扩展为完整 DNS 或 Registrar 管理。</p>
        </div>
        <span className="section-heading__meta">
          <MonoDigits>{domains.length}</MonoDigits> 个手工记录域名
        </span>
        {!readOnly ? <Button variant="secondary" size="sm" onClick={onCreate}>新增域名</Button> : null}
      </div>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      <DataTable
        className="asset-table vps-domain-table"
        columns={domainColumns}
        rows={domains}
        rowKey={(domain) => domain.domain_id}
        emptyContent={<span className="empty-inline">尚未记录域名</span>}
      />
    </section>
  )
}

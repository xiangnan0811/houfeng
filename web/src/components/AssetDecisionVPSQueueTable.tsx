import type { ReactNode } from 'react'

import { DataTable, MonoDigits, type DataTableColumn } from './atoms'
import { formatOptional } from '../lib/format'
import type { VPSAssetRecord } from '../lib/types'
import { LifecycleBadge, RenewalBadge, UsageBadge } from '../pages/assetPageBadges'

type AssetDecisionVPSQueueTableProps = {
  title: string
  eyebrow: string
  ariaLabel: string
  loading: boolean
  error: string | null
  rows: VPSAssetRecord[]
  renderActions: (vps: VPSAssetRecord) => ReactNode
}

export function AssetDecisionVPSQueueTable({
  title,
  eyebrow,
  ariaLabel,
  loading,
  error,
  rows,
  renderActions,
}: AssetDecisionVPSQueueTableProps) {
  const columns: DataTableColumn<VPSAssetRecord>[] = [
    {
      key: 'identity',
      label: 'VPS',
      render: (vps) => (
        <div className="asset-table__identity">
          <strong>{vps.display_name}</strong>
          <span>{vps.vps_id}</span>
        </div>
      ),
    },
    {
      key: 'provider',
      label: '服务商 / 区域',
      render: (vps) => (
        <div className="asset-table__stack">
          <strong>{formatOptional(vps.provider_name)}</strong>
          <span>{[vps.country, vps.region, vps.city].filter(Boolean).join(' · ') || '—'}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '状态',
      render: (vps) => (
        <span className="asset-status-stack">
          <LifecycleBadge value={vps.lifecycle_status} />
          <UsageBadge value={vps.usage_status} />
          <RenewalBadge value={vps.renewal_decision} />
        </span>
      ),
    },
    {
      key: 'nodes',
      label: 'Node',
      align: 'center',
      render: (vps) => <MonoDigits>{vps.active_node_link_count}</MonoDigits>,
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      render: renderActions,
    },
  ]

  return (
    <section className="page-panel" aria-label={ariaLabel}>
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">{eyebrow}</p>
          <h2>{title}</h2>
        </div>
        <span className="section-heading__meta">
          <MonoDigits>{rows.length}</MonoDigits> 台 VPS
        </span>
      </div>
      {loading ? (
        <div className="empty-state">正在加载 {title}…</div>
      ) : error ? (
        <div className="empty-state">{error}</div>
      ) : (
        <DataTable
          className="asset-table asset-decision-vps-table"
          columns={columns}
          rows={rows}
          rowKey={(vps) => vps.vps_id}
          emptyContent={<span className="empty-inline">暂无{title}</span>}
        />
      )}
    </section>
  )
}

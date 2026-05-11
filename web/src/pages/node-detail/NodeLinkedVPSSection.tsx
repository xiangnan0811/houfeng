import type { RefObject } from 'react'
import { Link } from 'react-router-dom'

import { DetailSection } from '../../components/DetailSection'
import { DataTable, Timestamp, type DataTableColumn } from '../../components/atoms'
import type { VPSSummary } from '../../lib/types'
import { AssetLabels, LifecycleBadge, RenewalBadge, UsageBadge } from '../assetPageBadges'
import { formatAssetLocation } from './nodeDetailHelpers'

type NodeLinkedVPSSectionProps = {
  sectionRef: RefObject<HTMLDivElement | null>
  records: VPSSummary[]
  loading: boolean
  loaded: boolean
  error: string | null
}

export function NodeLinkedVPSSection({
  sectionRef,
  records,
  loading,
  loaded,
  error,
}: NodeLinkedVPSSectionProps) {
  const linkedVPSColumns: DataTableColumn<VPSSummary>[] = [
    {
      key: 'vps',
      label: 'VPS',
      render: (vps) => (
        <div className="asset-table__identity">
          <strong>
            <Link className="text-link" to={`/vps/${vps.vps_id}`}>{vps.display_name}</Link>
          </strong>
          <span>{vps.vps_id}</span>
        </div>
      ),
    },
    {
      key: 'provider',
      label: 'Provider / 位置',
      render: (vps) => (
        <div className="asset-table__stack">
          <strong>{vps.provider_name || 'Provider 未确认'}</strong>
          <span>{formatAssetLocation(vps)}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '资产状态',
      render: (vps) => (
        <span className="asset-status-stack">
          <LifecycleBadge value={vps.lifecycle_status} />
          <UsageBadge value={vps.usage_status} />
          <RenewalBadge value={vps.renewal_decision} />
        </span>
      ),
    },
    {
      key: 'link',
      label: '关联',
      render: (vps) => (
        <div className="asset-table__stack">
          <strong><Timestamp value={vps.linked_at} /></strong>
          <span>{vps.note || '无关联备注'}</span>
        </div>
      ),
    },
    {
      key: 'labels',
      label: '标签',
      render: (vps) => <AssetLabels labels={vps.labels} />,
    },
  ]

  return (
    <div ref={sectionRef}>
      <DetailSection
        eyebrow="ASSET LEDGER"
        title="关联 VPS"
        aside={loading ? '加载中' : loaded ? `${records.length} 台` : '待同步'}
      >
        {error ? <p className="empty-inline" role="alert">{error}</p> : null}
        <DataTable
          className="asset-table node-vps-table"
          columns={linkedVPSColumns}
          rows={records}
          rowKey={(vps) => vps.vps_id}
          emptyContent={
            <span className="empty-inline">
              {loading
                ? '正在加载关联 VPS…'
                : loaded
                  ? '尚未关联 VPS'
                  : '关联 VPS 待同步'}
            </span>
          }
        />
      </DetailSection>
    </div>
  )
}

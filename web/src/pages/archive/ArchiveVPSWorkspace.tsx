import { Link } from 'react-router-dom'

import { Badge, DataTable, MonoDigits } from '../../components/atoms'
import type { SubscriptionRecord, VPSAssetRecord } from '../../lib/types'
import { formatDateTime } from '../../lib/format'
import { lifecycleLabel, renewalLabel, usageLabel, vpsLocationLabel } from '../assetPageUtils'
import { lifecycleTone, subscriptionMonthlySummary } from './archivePageHelpers'

type ArchiveVPSWorkspaceProps = {
  vpsRows: VPSAssetRecord[]
  subscriptions: SubscriptionRecord[]
}

export function ArchiveVPSWorkspace({
  vpsRows,
  subscriptions,
}: ArchiveVPSWorkspaceProps) {
  return (
    <section className="page-panel archive-page__workspace">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">READ ONLY</p>
          <h2 className="section-heading__title">归档服务器</h2>
          <p className="section-heading__description">只保留已取消、已归档 VPS 的清单入口；单台历史在详情页只读查看。</p>
        </div>
      </div>
      <div className="page-panel page-panel--scroll-x archive-page__table-panel">
        <DataTable
          className="archive-page__vps-table"
          rows={vpsRows}
          rowKey={(vps) => vps.vps_id}
          columns={[
            {
              key: 'identity',
              label: 'VPS',
              width: '240px',
              render: (vps) => (
                <div className="asset-table__identity">
                  <strong>{vps.display_name}</strong>
                  <small>{vps.provider_name || '服务商缺失'} · {vps.product_name || '产品缺失'}</small>
                  <small>{vps.vps_id}</small>
                </div>
              ),
            },
            {
              key: 'status',
              label: '状态',
              width: '136px',
              render: (vps) => (
                <div className="badge-row badge-row--wrap">
                  <Badge variant="state" tone={lifecycleTone(vps.lifecycle_status)}>
                    {lifecycleLabel(vps.lifecycle_status)}
                  </Badge>
                  <Badge variant="info" tone="neutral">{usageLabel(vps.usage_status)}</Badge>
                </div>
              ),
            },
            {
              key: 'decision',
              label: '续费判断',
              width: '132px',
              render: (vps) => renewalLabel(vps.renewal_decision),
            },
            {
              key: 'monthly',
              label: '历史月成本',
              width: '180px',
              render: (vps) => subscriptionMonthlySummary(subscriptions.filter((subscription) => subscription.vps_id === vps.vps_id)),
            },
            {
              key: 'location',
              label: '位置',
              width: '180px',
              render: (vps) => (
                <div className="asset-table__stack">
                  <strong>{vpsLocationLabel(vps)}</strong>
                  <small>{vps.datacenter || '机房缺失'}</small>
                </div>
              ),
            },
            {
              key: 'archived_at',
              label: '归档时间',
              width: '156px',
              render: (vps) => <MonoDigits>{formatDateTime(vps.archived_at ?? vps.updated_at)}</MonoDigits>,
            },
            {
              key: 'action',
              label: '详情',
              width: '128px',
              render: (vps) => (
                <Link className="btn sm secondary" to={`/archive/${encodeURIComponent(vps.vps_id)}`}>
                  查看归档详情
                </Link>
              ),
            },
          ]}
        />
      </div>
    </section>
  )
}

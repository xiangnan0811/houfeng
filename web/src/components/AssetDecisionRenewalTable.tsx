import type { ReactNode } from 'react'

import { PageState } from './PageState'
import { DataTable, type DataTableColumn } from './atoms'
import { formatDate, formatMoney } from '../lib/format'
import type { SubscriptionRecord, VPSAssetRecord } from '../lib/types'
import { SubscriptionStatusBadge } from '../pages/assetPageBadges'

type AssetDecisionRenewalTableProps = {
  loading: boolean
  error: string | null
  renewals: SubscriptionRecord[]
  vpsByID?: Map<string, VPSAssetRecord>
  renderVPSReference?: (subscription: SubscriptionRecord, vps: VPSAssetRecord | undefined) => ReactNode
  renderActions: (subscription: SubscriptionRecord) => ReactNode
}

export function AssetDecisionRenewalTable({
  loading,
  error,
  renewals,
  vpsByID,
  renderVPSReference,
  renderActions,
}: AssetDecisionRenewalTableProps) {
  const columns: DataTableColumn<SubscriptionRecord>[] = [
    {
      key: 'subscription',
      label: '订阅 / VPS',
      render: (subscription) => {
        const vps = vpsByID?.get(subscription.vps_id)
        return (
          <div className="asset-table__identity">
            <strong>{renderVPSReference ? renderVPSReference(subscription, vps) : (vps?.display_name ?? subscription.vps_id)}</strong>
            <span>{vps ? subscription.vps_id : 'VPS 名称未加载'} · {subscription.subscription_id}</span>
          </div>
        )
      },
    },
    {
      key: 'renew',
      label: '续费日期',
      render: (subscription) => (
        <div className="asset-table__stack">
          <strong>{formatDate(subscription.renew_at)}</strong>
          <span>{subscription.auto_renew ? '自动续费' : '手动续费'} · {subscription.auto_renew_cancelled ? '已取消自动续费' : '自动续费未取消'}</span>
        </div>
      ),
    },
    {
      key: 'price',
      label: '金额',
      render: (subscription) => (
        <div className="asset-table__stack">
          <strong>{formatMoney(subscription.price, subscription.currency)}</strong>
          <span>月付 {formatMoney(subscription.monthly_price, subscription.currency)}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '状态',
      render: (subscription) => <SubscriptionStatusBadge value={subscription.status} />,
    },
    {
      key: 'actions',
      label: '入口',
      align: 'right',
      render: renderActions,
    },
  ]

  if (loading) {
    return (
      <PageState
        kind="loading"
        title="正在加载续费候选…"
        surface="empty"
        compact
      />
    )
  }
  if (error) {
    return (
      <PageState
        kind="error"
        title="续费候选不可用"
        description={error}
        technicalSummary={error}
        surface="empty"
        compact
      />
    )
  }

  return (
    <DataTable
      className="asset-table asset-decision-renewals-table"
      columns={columns}
      rows={renewals}
      rowKey={(subscription) => subscription.subscription_id}
      emptyContent={<span className="empty-inline">当前窗口暂无续费候选</span>}
    />
  )
}

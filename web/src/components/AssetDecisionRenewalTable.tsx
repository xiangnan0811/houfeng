import type { ReactNode } from 'react'

import { DataTable, type DataTableColumn } from './atoms'
import { formatDate, formatMoney } from '../lib/format'
import type { SubscriptionRecord } from '../lib/types'
import { SubscriptionStatusBadge } from '../pages/assetPageBadges'

type AssetDecisionRenewalTableProps = {
  loading: boolean
  error: string | null
  renewals: SubscriptionRecord[]
  renderActions: (subscription: SubscriptionRecord) => ReactNode
}

export function AssetDecisionRenewalTable({
  loading,
  error,
  renewals,
  renderActions,
}: AssetDecisionRenewalTableProps) {
  const columns: DataTableColumn<SubscriptionRecord>[] = [
    {
      key: 'subscription',
      label: '订阅 / VPS',
      render: (subscription) => (
        <div className="asset-table__identity">
          <strong>{subscription.vps_id}</strong>
          <span>{subscription.subscription_id}</span>
        </div>
      ),
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

  if (loading) return <div className="empty-state">正在加载续费候选…</div>
  if (error) return <div className="empty-state">{error}</div>

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

import { Link } from 'react-router-dom'

import { MonoDigits } from '../../components/atoms'
import type { DashboardAssetSummary } from '../../lib/types'
import { DASHBOARD_LINKS } from './dashboardLinks'
import { formatAssetCost } from './dashboardHelpers'

type AssetDecisionSummaryProps = {
  summary: DashboardAssetSummary
}

export function AssetDecisionSummary({ summary }: AssetDecisionSummaryProps) {
  const pressureCount =
    summary.renewal_due_30d_vps_count +
    summary.unreviewed_vps_count +
    summary.to_cancel_vps_count +
    summary.to_migrate_vps_count +
    summary.unlinked_vps_count +
    summary.abnormal_linked_vps_count
  const lifecycleReviewCount = summary.to_cancel_vps_count + summary.to_migrate_vps_count
  const lifecycleReviewDetail =
    `待取消 ${summary.to_cancel_vps_count} · 待迁移 ${summary.to_migrate_vps_count}`
  const items = [
    {
      label: '30 天续费',
      value: summary.renewal_due_30d_vps_count,
      detail: `订阅 ${summary.renewal_due_30d_subscription_count}`,
      to: DASHBOARD_LINKS.assetDecisions,
      tone: summary.renewal_due_30d_vps_count > 0 ? 'notice' : 'normal',
    },
    {
      label: '待决策',
      value: summary.unreviewed_vps_count,
      detail: '续费状态未评估',
      to: DASHBOARD_LINKS.assetDecisions,
      tone: summary.unreviewed_vps_count > 0 ? 'notice' : 'normal',
    },
    {
      label: '取消/迁移',
      value: lifecycleReviewCount,
      detail: lifecycleReviewDetail,
      to: DASHBOARD_LINKS.assetDecisions,
      tone: lifecycleReviewCount > 0 ? 'alert' : 'normal',
    },
    {
      label: '未关联 Node',
      value: summary.unlinked_vps_count,
      detail: '需人工核对',
      to: DASHBOARD_LINKS.vps,
      tone: summary.unlinked_vps_count > 0 ? 'notice' : 'normal',
    },
    {
      label: '关联异常',
      value: summary.abnormal_linked_vps_count,
      detail: 'VPS 关联异常 Node',
      to: DASHBOARD_LINKS.nodesAbnormal,
      tone: summary.abnormal_linked_vps_count > 0 ? 'alert' : 'normal',
    },
    {
      label: '成本',
      value: summary.cost_by_currency.length,
      detail: formatAssetCost(summary),
      to: DASHBOARD_LINKS.subscriptionsRenew30d,
      tone: summary.cost_by_currency.length > 0 ? 'neutral' : 'normal',
    },
  ] as const

  return (
    <div className="dashboard-asset-summary" aria-label="资产决策摘要">
      <div className="dashboard-asset-summary__header">
        <div>
          <h3>资产决策</h3>
          <p>
            {pressureCount > 0
              ? `${pressureCount} 项资产信号需要复核`
              : '资产层暂无续费、关联或决策压力'}
          </p>
        </div>
        <Link className="text-link" to={DASHBOARD_LINKS.assetDecisions}>
          进入决策
        </Link>
      </div>
      <div className="dashboard-asset-summary__grid">
        {items.map((item) => (
          <Link
            className={`dashboard-asset-item dashboard-asset-item--${item.tone}`}
            to={item.to}
            key={item.label}
            aria-label={`${item.label}：${item.detail}`}
          >
            <span className="dashboard-asset-item__label">{item.label}</span>
            <strong>
              <MonoDigits>{item.value}</MonoDigits>
            </strong>
            <span className="dashboard-asset-item__detail">{item.detail}</span>
          </Link>
        ))}
      </div>
    </div>
  )
}

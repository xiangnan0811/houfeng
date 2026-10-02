import { Link } from 'react-router-dom'

import { PageState as PageStateView } from '../../../../components/PageState'
import { renewalModeFromLegacy, renewalModeLabel } from '../../../../lib/assetOptions'
import { formatDate, formatMoney } from '../../../../lib/format'
import type { VPSAssetRecord } from '../../../../lib/types'
import { SubscriptionStatusBadge } from '../../../assetPageBadges'
import { daysUntilDate, renewalTimingLabel, renewalUrgency } from '../../../assetPageUtils'
import { vpsDetailPath } from '../../paths'
import type { QueueState } from '../../types'
import { ScanName, ScanRow, WorkbenchPanel } from './WorkbenchPanel'

type RenewalsWorkbenchProps = {
  queueState: QueueState
  vpsByID: Map<string, VPSAssetRecord>
  renewalWindow: number
}

export function RenewalsWorkbench({ queueState, vpsByID, renewalWindow }: RenewalsWorkbenchProps) {
  const ready = !queueState.renewalsLoading && !queueState.renewalsError && queueState.renewals.length > 0
  return (
    <WorkbenchPanel
      title="续费窗口"
      className="asset-workbench--renewals"
      actions={ready ? (
        <Link className="btn sm secondary" to={`/asset-decisions?view=renewal&renew_within_days=${renewalWindow}`}>查看续费取舍组</Link>
      ) : null}
    >
      {queueState.renewalsLoading ? (
        <PageStateView kind="loading" title="正在加载续费候选…" surface="empty" compact />
      ) : queueState.renewalsError ? (
        <PageStateView
          kind="error"
          title="续费候选不可用"
          description={queueState.renewalsError}
          technicalSummary={queueState.renewalsError}
          surface="empty"
          compact
        />
      ) : queueState.renewals.length === 0 ? (
        <PageStateView kind="empty" title="当前窗口暂无续费候选" surface="empty" compact />
      ) : (
        <ul className="asset-scan-list asset-scan-list--renewals" aria-label="续费窗口">
          {queueState.renewals.map((subscription) => {
            const vps = vpsByID.get(subscription.vps_id)
            const days = daysUntilDate(subscription.renew_at)
            const monthlyDiffers = subscription.monthly_price !== subscription.price
            return (
              <ScanRow key={subscription.subscription_id} clickable>
                <span className="asset-scan-row__when" data-urgency={renewalUrgency(days)}>
                  <time className="mono tnum" dateTime={subscription.renew_at ?? undefined}>{formatDate(subscription.renew_at)}</time>
                  <small>{renewalTimingLabel(days)}</small>
                </span>
                <ScanName
                  name={<Link className="text-link" data-row-primary to={vpsDetailPath(subscription.vps_id)}>{vps?.display_name ?? 'VPS 名称未加载'}</Link>}
                  meta={subscription.display_name?.trim() || undefined}
                />
                <span className="asset-scan-row__muted">{renewalModeLabel(subscription.renewal_mode ?? renewalModeFromLegacy(subscription))}</span>
                <span className="asset-scan-row__amount mono tnum">
                  {formatMoney(subscription.price, subscription.currency)}
                  {monthlyDiffers ? <small>月付 {formatMoney(subscription.monthly_price, subscription.currency)}</small> : null}
                </span>
                {/* 续费窗口里绝大多数是生效中，只有其它状态才需要提醒。 */}
                {subscription.status !== 'active' ? <SubscriptionStatusBadge value={subscription.status} /> : <span aria-hidden="true" />}
              </ScanRow>
            )
          })}
        </ul>
      )}
    </WorkbenchPanel>
  )
}

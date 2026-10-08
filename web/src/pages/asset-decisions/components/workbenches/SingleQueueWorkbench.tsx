import { Link } from 'react-router-dom'

import { TabPanel, Tabs } from '../../../../components/atoms'
import { PageState as PageStateView } from '../../../../components/PageState'
import { formatDate } from '../../../../lib/format'
import type { VPSAssetRecord } from '../../../../lib/types'
import { RenewalBadge } from '../../../assetPageBadges'
import { daysUntilDate, renewalTimingLabel, renewalUrgency } from '../../../assetPageUtils'
import { exchangeRateStatusLabel } from '../../../subscriptions/exchangeRatePresentation'
import { hasCancellationAttention, subscriptionCostAttention } from '../../businessLogic'
import { baseMoney } from '../../formatters'
import { vpsWorkbenchPath } from '../../paths'
import type { DecisionQueueItem, DecisionQueueView, QueueState } from '../../types'
import { compactLocation } from './workbenchFormat'
import { ScanName, ScanRow, WorkbenchPanel } from './WorkbenchPanel'

export type SingleQueueWorkbenchProps = {
  queueView: DecisionQueueView
  renewalWindow: number
  queueState: QueueState
  visibleDecisionQueue: DecisionQueueItem[]
  totalDecisionQueue: number
  renewalDueQueueCount: number
  missingSubscriptionCount: number
  unlinkedCount: number
  cancellationAttentionCount: number
  onSetQueueView: (view: DecisionQueueView) => void
  onSelectVPS: (vps: VPSAssetRecord) => void
  onNavigateToVPS: (vps: VPSAssetRecord) => void
  onNavigateToVPSSubscription: (vpsID: string) => void
}

function QueueRow({ item, renewalWindow, onSelectVPS, onNavigateToVPS, onNavigateToVPSSubscription }: {
  item: DecisionQueueItem
} & Pick<SingleQueueWorkbenchProps, 'renewalWindow' | 'onSelectVPS' | 'onNavigateToVPS' | 'onNavigateToVPSSubscription'>) {
  const { vps, subscription } = item
  const days = subscription ? daysUntilDate(subscription.renew_at) : null
  const showArchive = vps.renewal_decision === 'cancel' || hasCancellationAttention(item)
  return (
    <ScanRow clickable>
      <ScanName
        name={<button type="button" className="asset-scan-row__link" data-row-primary onClick={() => onNavigateToVPS(vps)}>{vps.display_name}</button>}
        meta={[vps.provider_name?.trim(), compactLocation(vps)].filter(Boolean).join(' · ')}
      />
      <RenewalBadge value={vps.renewal_decision} />
      {subscription ? (
        // 续费窗口内高亮剩余天数，与原队列的紧急口径一致。
        <span className="asset-scan-row__when" data-urgency={renewalUrgency(days, renewalWindow)}>
          <time className="mono tnum" dateTime={subscription.renew_at ?? undefined}>{formatDate(subscription.renew_at)}</time>
          <small>{renewalTimingLabel(days)}</small>
        </span>
      ) : (
        <span className="asset-scan-row__when">
          <button type="button" className="text-link" onClick={() => onNavigateToVPSSubscription(vps.vps_id)}>缺订阅</button>
        </span>
      )}
      {subscription ? (
        <span className="asset-scan-row__amount mono tnum">
          {subscriptionCostAttention(subscription) ? <small className="asset-scan-row__warn">{exchangeRateStatusLabel(subscription.exchange_rate_status)}</small> : null}
          {subscription.monthly_price_base == null ? '金额待核对' : `${baseMoney(subscription.monthly_price_base, subscription.base_currency ?? 'CNY')}/月`}
        </span>
      ) : <span aria-hidden="true" />}
      {vps.active_monitoring_instance_link_count > 0
        ? <span aria-hidden="true" />
        : <span className="asset-scan-row__muted">未关联监控</span>}
      <span className="asset-scan-row__actions">
        <button className="btn sm primary" type="button" onClick={() => onSelectVPS(vps)}>处理</button>
        {showArchive ? (
          <Link className="btn sm secondary" to={vpsWorkbenchPath(vps.vps_id, 'archive')}>结束使用并归档</Link>
        ) : null}
      </span>
    </ScanRow>
  )
}

export function SingleQueueWorkbench(props: SingleQueueWorkbenchProps) {
  const { queueView, queueState, visibleDecisionQueue, onSetQueueView } = props
  const queueTabs = [
    { value: 'all', label: '全部', count: props.totalDecisionQueue },
    { value: 'unreviewed', label: '待评估', count: queueState.unreviewed.length },
    { value: 'renewal', label: `${props.renewalWindow}天续费`, count: props.renewalDueQueueCount },
    { value: 'cancel', label: '决定不续费', count: queueState.cancel.length },
    { value: 'cancellation_attention', label: '自动续费待核对', count: props.cancellationAttentionCount },
    { value: 'unlinked', label: '未关联', count: props.unlinkedCount },
    { value: 'missing_subscription', label: '缺订阅', count: props.missingSubscriptionCount },
  ] satisfies Array<{ value: DecisionQueueView; label: string; count: number }>

  return (
    <WorkbenchPanel title="单台队列" id="single-vps-queue" className="asset-workbench--queue">
      <div className="asset-workbench__tabs">
        <Tabs
          label="单台辅助队列视图"
          idBase="asset-decision-queue"
          items={queueTabs}
          value={queueView}
          onChange={onSetQueueView}
          variant="pill"
        />
      </div>
      <TabPanel idBase="asset-decision-queue" value={queueView} className="asset-decision-tab-panel">
        {queueState.queueLoading ? (
          <PageStateView kind="loading" title="正在加载单台队列…" surface="empty" compact />
        ) : queueState.queueError ? (
          <PageStateView kind="error" title="单台队列不可用" surface="empty" compact />
        ) : visibleDecisionQueue.length === 0 ? (
          <PageStateView
            kind="empty"
            title="当前视图暂无待处理 VPS"
            action={
              <div className="asset-empty-actions">
                {queueView !== 'all' && (
                  <button className="btn sm secondary" type="button" onClick={() => onSetQueueView('all')}>查看全部</button>
                )}
                <Link className="btn sm ghost" to="/vps">VPS 库存</Link>
                <Link className="btn sm ghost" to="/vps?view=missing_subscription">缺订阅 VPS</Link>
              </div>
            }
            surface="empty"
            compact
          />
        ) : (
          <ul className="asset-scan-list asset-scan-list--queue" aria-label="单台辅助队列">
            {visibleDecisionQueue.map((item) => (
              <QueueRow
                key={item.vps.vps_id}
                item={item}
                renewalWindow={props.renewalWindow}
                onSelectVPS={props.onSelectVPS}
                onNavigateToVPS={props.onNavigateToVPS}
                onNavigateToVPSSubscription={props.onNavigateToVPSSubscription}
              />
            ))}
          </ul>
        )}
      </TabPanel>
    </WorkbenchPanel>
  )
}

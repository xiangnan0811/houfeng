import type { ReactNode } from 'react'

import { Button, Timestamp } from '../../components/atoms'
import { formatMoney } from '../../lib/format'
import type { SubscriptionRecord } from '../../lib/types'
import { VPSDetailResourceList } from './VPSDetailResourceList'
import {
  readyResourceState,
  subscriptionCadenceLabel,
  subscriptionDueLabel,
  subscriptionResourceName,
} from './vpsDetailResourcePresentation'


type Props = {
  decisionLabel: string
  primary?: SubscriptionRecord
  extras?: SubscriptionRecord[]
  loading?: boolean
  error?: string | null
  onRetry?: () => void
  empty?: ReactNode
  leading?: ReactNode
  trailing?: ReactNode
  plannedCancellation?: boolean
  cancellationPlanLabel?: string
}

function SubscriptionIdentity({ item }: { item: SubscriptionRecord }) {
  const name = subscriptionResourceName(item)
  const id = item.subscription_id.trim()
  return (
    <div className="vps-detail-workspace__ops-identity">
      <p className="vps-detail-workspace__renewal-name">{name}</p>
      {id ? <p className="vps-detail-workspace__renewal-id mono">{id}</p> : null}
    </div>
  )
}

export function VPSSubscriptionOpsBody({
  decisionLabel,
  primary,
  extras = [],
  loading = false,
  error = null,
  onRetry,
  empty = null,
  leading = null,
  trailing = null,
  plannedCancellation = false,
  cancellationPlanLabel,
}: Props) {
  const due = primary ? subscriptionDueLabel(primary, { plannedCancellation }) : null


  return (
    <div className="vps-detail-workspace__anchor">
      {loading ? <p className="vps-detail-resource-group__status">正在加载订阅…</p> : null}
      {error ? (
        <div className="vps-detail-resource-group__error" role="status">
          <p>{error}</p>
          {onRetry ? (
            <Button type="button" size="sm" variant="ghost" aria-label="重试 订阅" onClick={onRetry}>
              重试
            </Button>
          ) : null}
        </div>
      ) : null}

      <div className="vps-detail-workspace__ops">
        {primary ? (
          <>
            <SubscriptionIdentity item={primary} />
            <p className="vps-detail-workspace__renewal-money">
              {primary.currency.trim() ? (
                <span className="vps-detail-workspace__renewal-price">
                  {formatMoney(primary.price, primary.currency)}
                </span>
              ) : null}
              <span className="vps-detail-workspace__renewal-cadence">{subscriptionCadenceLabel(primary)}</span>
            </p>
            <div className="vps-detail-workspace__renewal-follow">
              {due ? <p className="vps-detail-workspace__renewal-date">{due}</p> : null}
              <div className="vps-detail-workspace__renewal-decision">
                <span>决策</span>
                <strong>{decisionLabel || '—'}</strong>
              </div>
            </div>
            {plannedCancellation && cancellationPlanLabel ? (
              <p className="vps-detail-workspace__cancellation-plan">
                取消计划 {cancellationPlanLabel}
              </p>
            ) : null}
          </>
        ) : !loading ? empty : null}
      </div>

      {extras.length > 0 ? (
        <VPSDetailResourceList
          state={readyResourceState(extras, null)}
          groupLabel=""
          loadingLabel=""
          emptyLabel=""
          retryLabel="订阅"
          getKey={(item) => item.subscription_id}
          getName={subscriptionResourceName}
          getId={(item) => item.subscription_id}
          getAddress={(item) => item.currency.trim() ? formatMoney(item.price, item.currency) : ''}
          getType={subscriptionCadenceLabel}
          getSummary={(item) => subscriptionDueLabel(item, { plannedCancellation }) ?? ''}

        />
      ) : null}

      {primary?.updated_at ? (
        <p className="vps-detail-workspace__renewal-updated">
          订阅更新 <Timestamp value={primary.updated_at} mode="absolute" />
        </p>
      ) : null}

      {trailing}
      {leading}
    </div>
  )
}

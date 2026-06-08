import { formatMoney } from '../../lib/format'
import type { SubscriptionRecord, VPSAssetRecord } from '../../lib/types'

export function subscriptionsForVPS(
  subscriptions: SubscriptionRecord[],
  vpsID: string | null,
): SubscriptionRecord[] {
  if (!vpsID) return []
  return subscriptions.filter((subscription) => subscription.vps_id === vpsID)
}

export function selectedVPS(rows: VPSAssetRecord[], selectedVPSID: string | null): VPSAssetRecord | null {
  if (!selectedVPSID) return null
  return rows.find((row) => row.vps_id === selectedVPSID) ?? null
}

export function lifecycleTone(status: VPSAssetRecord['lifecycle_status']): 'neutral' | 'offline' {
  return status === 'cancelled' || status === 'archived' ? 'offline' : 'neutral'
}

export function subscriptionMonthlySummary(subscriptions: SubscriptionRecord[]): string {
  if (subscriptions.length === 0) return '暂无月成本'

  const totals = new Map<string, number>()
  for (const subscription of subscriptions) {
    const currency = subscription.currency || '---'
    totals.set(currency, (totals.get(currency) ?? 0) + subscription.monthly_price)
  }

  return Array.from(totals.entries())
    .map(([currency, total]) => `${formatMoney(total, currency)}/月`)
    .join(' + ')
}

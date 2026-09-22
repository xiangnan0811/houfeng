import { listMonitoringInstances, listProviders, listSubscriptions, listTargets, listVPSAssets } from '../../lib/api'
import { formatDate, formatMoney } from '../../lib/format'
import type { MonitoringInstanceRecord, ProviderRecord, SubscriptionRecord, TargetRecord, VPSAssetRecord } from '../../lib/types'

export interface SearchResult {
  kind: 'vps' | 'monitoring_instance' | 'target' | 'provider' | 'subscription' | 'record'
  id: string
  label: string
  hint: string
  to: string
}

/** Cap on the client-side asset matches, which are unranked. */
const MAX_RESULTS = 10

export type AssetSearchOutcome = {
  matches: SearchResult[]
  error: string | null
}

export async function searchAssets(q: string): Promise<AssetSearchOutcome> {
  try {
    const [vpsAssets, monitoring, targets, providers, subscriptions] = await Promise.all([
      listVPSAssets(),
      listMonitoringInstances(),
      listTargets(),
      listProviders(),
      listSubscriptions({ sort: 'renew_at', order: 'asc' }),
    ])
    return {
      matches: combine(vpsAssets, monitoring, targets, providers, subscriptions, q)
        .slice(0, MAX_RESULTS),
      error: null,
    }
  } catch (err) {
    return { matches: [], error: err instanceof Error ? err.message : '搜索失败' }
  }
}

function combine(
  vpsAssets: VPSAssetRecord[],
  monitoring: MonitoringInstanceRecord[],
  targets: TargetRecord[],
  providers: ProviderRecord[],
  subscriptions: SubscriptionRecord[],
  q: string,
): SearchResult[] {
  const out: SearchResult[] = []
  for (const vps of vpsAssets) {
    if (matchesVPS(vps, q)) {
      out.push({
        kind: 'vps',
        id: vps.vps_id,
        label: vps.display_name || vps.vps_id,
        hint: compactHint([vps.provider_name, vps.region || vps.city, vps.ssh_host || vps.ipv4]),
        to: `/vps/${vps.vps_id}`,
      })
    }
  }
  for (const monitoringInstance of monitoring) {
    if (matchesMonitoringInstance(monitoringInstance, q)) {
      out.push({
        kind: 'monitoring_instance',
        id: monitoringInstance.monitoring_instance_id,
        label: monitoringInstance.display_name || monitoringInstance.monitoring_instance_id,
        hint: compactHint([monitoringInstance.region, monitoringInstance.city, monitoringInstance.provider]) || monitoringInstance.monitoring_instance_id,
        to: `/monitoring/${monitoringInstance.monitoring_instance_id}`,
      })
    }
  }
  for (const target of targets) {
    if (matchesTarget(target, q)) {
      out.push({
        kind: 'target',
        id: target.target_id,
        label: target.name || target.target_id,
        hint: compactHint([target.host, target.base_port ? String(target.base_port) : null]),
        to: `/targets/${target.target_id}`,
      })
    }
  }
  for (const provider of providers) {
    if (matchesProvider(provider, q)) {
      out.push({
        kind: 'provider',
        id: provider.provider_id,
        label: provider.name || provider.provider_id,
        hint: compactHint([provider.country, provider.account_hint]),
        to: '/providers',
      })
    }
  }
  for (const subscription of subscriptions) {
    if (matchesSubscription(subscription, q)) {
      out.push({
        kind: 'subscription',
        id: subscription.subscription_id,
        label: subscription.subscription_id,
        hint: compactHint([
          subscription.vps_id,
          formatMoney(subscription.monthly_price, subscription.currency),
          subscription.renew_at ? `续费 ${formatDate(subscription.renew_at)}` : null,
        ]),
        to: `/subscriptions?vps_id=${encodeURIComponent(subscription.vps_id)}&view=details`,
      })
    }
  }
  return out
}

function matchesVPS(vps: VPSAssetRecord, q: string): boolean {
  return (
    includesLower(vps.display_name, q) ||
    includesLower(vps.vps_id, q) ||
    includesLower(vps.provider_name, q) ||
    includesLower(vps.product_name, q) ||
    includesLower(vps.order_ref, q) ||
    includesLower(vps.country, q) ||
    includesLower(vps.region, q) ||
    includesLower(vps.city, q) ||
    includesLower(vps.datacenter, q) ||
    includesLower(vps.ipv4, q) ||
    includesLower(vps.ipv6, q) ||
    includesLower(vps.ssh_host, q) ||
    (vps.labels ?? []).some((label) => includesLower(label, q))
  )
}

function matchesMonitoringInstance(monitoringInstance: MonitoringInstanceRecord, q: string): boolean {
  return (
    includesLower(monitoringInstance.display_name, q) ||
    includesLower(monitoringInstance.monitoring_instance_id, q) ||
    includesLower(monitoringInstance.region, q) ||
    includesLower(monitoringInstance.city, q) ||
    includesLower(monitoringInstance.provider, q) ||
    (monitoringInstance.labels ?? []).some((label) => includesLower(label, q))
  )
}

function matchesTarget(target: TargetRecord, q: string): boolean {
  return (
    includesLower(target.name, q) ||
    includesLower(target.target_id, q) ||
    includesLower(target.host, q) ||
    (target.labels ?? []).some((label) => includesLower(label, q))
  )
}

function matchesProvider(provider: ProviderRecord, q: string): boolean {
  return (
    includesLower(provider.name, q) ||
    includesLower(provider.provider_id, q) ||
    includesLower(provider.country, q) ||
    includesLower(provider.account_hint, q) ||
    (provider.labels ?? []).some((label) => includesLower(label, q))
  )
}

function matchesSubscription(subscription: SubscriptionRecord, q: string): boolean {
  return (
    includesLower(subscription.subscription_id, q) ||
    includesLower(subscription.vps_id, q) ||
    includesLower(subscription.currency, q) ||
    includesLower(subscription.payment_method, q) ||
    includesLower(subscription.note, q) ||
    includesLower(subscription.renew_at, q)
  )
}

function compactHint(parts: Array<string | null | undefined>): string {
  return parts.filter((part) => part && part.trim()).join(' · ')
}

function includesLower(value: string | undefined | null, q: string): boolean {
  if (!value) return false
  return value.toLowerCase().includes(q)
}

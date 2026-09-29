import { countryLabel } from '../../lib/assetOptions'
import { formatDate } from '../../lib/format'
import type { SubscriptionRecord, VPSAssetRecord } from '../../lib/types'
import { daysUntilDate, renewalLabel, usageLabel } from '../assetPageUtils'

// 未选中 VPS 时，目录检查器汇总当前可见列表；只用列表页已加载的资产与订阅证据。
export const OVERVIEW_RENEWAL_LIMIT = 5
export const OVERVIEW_DISTRIBUTION_LIMIT = 4

export type OverviewRow = {
  vps: VPSAssetRecord
  subscription: SubscriptionRecord | null
}

export type OverviewCount = {
  key: string
  label: string
  count: number
}

export type OverviewRenewal = {
  vpsId: string
  name: string
  date: string
  days: number
}

export type OverviewRenewals =
  | { status: 'loading' | 'error' }
  | { status: 'ready'; items: OverviewRenewal[]; hidden: number; undated: number }

export type InventoryOverview = {
  total: number
  renewals: OverviewRenewals
  renewalDecisions: OverviewCount[]
  // 用途是多选标签，一台可计入多个用途；未标注的单独计数。
  usages: OverviewCount[]
  providers: OverviewCount[]
  regions: OverviewCount[]
}

type Evidence = 'loading' | 'error' | 'ready'

export function buildInventoryOverview(rows: OverviewRow[], subscriptionEvidence: Evidence): InventoryOverview {
  return {
    total: rows.length,
    renewals: buildRenewals(rows, subscriptionEvidence),
    renewalDecisions: countBy(rows, (row) => [row.vps.renewal_decision], renewalLabel),
    usages: collapseTail(countBy(rows, usageKeys, usageLabel)),
    providers: collapseTail(countBy(rows, (row) => [row.vps.provider_name.trim()], (value) => value || '未填写')),
    regions: collapseTail(countBy(rows, (row) => [row.vps.country.trim().toUpperCase()], countryLabel)),
  }
}

function buildRenewals(rows: OverviewRow[], evidence: Evidence): OverviewRenewals {
  if (evidence !== 'ready') return { status: evidence }
  const dated: OverviewRenewal[] = []
  for (const row of rows) {
    const renewAt = row.subscription?.renew_at
    const days = daysUntilDate(renewAt)
    if (!renewAt || days == null) continue
    dated.push({ vpsId: row.vps.vps_id, name: row.vps.display_name, date: formatDate(renewAt), days })
  }
  // 已过续费日的排在最前，其余按剩余天数升序；同日按名称稳定排序。
  dated.sort((left, right) => left.days - right.days || left.name.localeCompare(right.name, 'zh-CN'))
  return {
    status: 'ready',
    items: dated.slice(0, OVERVIEW_RENEWAL_LIMIT),
    hidden: Math.max(0, dated.length - OVERVIEW_RENEWAL_LIMIT),
    undated: rows.length - dated.length,
  }
}

function usageKeys(row: OverviewRow): string[] {
  const tags = [...new Set((row.vps.usage_tags ?? []).map((tag) => tag.trim()).filter(Boolean))]
  return tags.length > 0 ? tags : ['']
}

function countBy(
  rows: OverviewRow[],
  keysOf: (row: OverviewRow) => string[],
  labelOf: (key: string) => string,
): OverviewCount[] {
  const counts = new Map<string, number>()
  for (const row of rows) {
    for (const key of keysOf(row)) counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  // 原始值与合并项分属不同 key 命名空间，自由输入的用途不会与合并项冲突。
  return [...counts.entries()]
    .map(([key, count]) => ({ key: `value:${key}`, label: labelOf(key), count }))
    .sort((left, right) => right.count - left.count || left.label.localeCompare(right.label, 'zh-CN'))
}

// 分布只列前几项，其余合并为“其余 N 类”，避免长尾撑高检查器；
// 不用“其他”，以免与用户自己录入的“其他”用途或服务商混淆。
function collapseTail(items: OverviewCount[]): OverviewCount[] {
  if (items.length <= OVERVIEW_DISTRIBUTION_LIMIT + 1) return items
  const head = items.slice(0, OVERVIEW_DISTRIBUTION_LIMIT)
  const tail = items.slice(OVERVIEW_DISTRIBUTION_LIMIT)
  const rest = tail.reduce((sum, item) => sum + item.count, 0)
  return [...head, { key: 'aggregate:rest', label: `其余 ${tail.length} 类`, count: rest }]
}

import { ApiError } from '../../lib/api'
import type {
  AssetDecisionComparisonInsight,
  AssetDecisionComparisonLane,
  AssetDecisionComparisonPrimaryAxis,
  AssetDecisionComparisonSignal,
  AssetDecisionEvidenceAssessment,
  AssetDecisionEvidenceDecisionBias,
  AssetDecisionEvidenceQualityTier,
  AssetDecisionEvidenceSnapshot,
  AssetDecisionGroupListFilter,
  AssetDecisionManualGroupScenario,
  AssetDecisionOverview,
  AssetDecisionGroupMember,
  AssetDecisionView,
  SubscriptionRecord,
  VPSAssetRecord,
} from '../../lib/types'
import { MANUAL_GROUP_SCENARIO_LABELS } from './constants'
import type {
  AssetQualityIssue,
  ContextFilterChip,
  MainWorkbenchView,
  WorkbenchView,
} from './types'

type RenewalWindow = 30 | 60 | 90

const RENEWAL_WINDOWS: readonly RenewalWindow[] = [30, 60, 90]

const MANUAL_GROUP_SCENARIO_OPTIONS: ReadonlyArray<{ value: AssetDecisionManualGroupScenario; label: string }> = [
  { value: 'general', label: '通用组合' },
  { value: 'primary_standby', label: '主备取舍' },
  { value: 'budget_reduction', label: '预算压缩' },
  { value: 'provider_review', label: '服务商评估' },
  { value: 'region_review', label: '同区比较' },
  { value: 'migration_retirement', label: '迁移退役' },
  { value: 'evidence_cleanup', label: '资料清理' },
]

const COMPARISON_AXIS_LABELS: Record<AssetDecisionComparisonPrimaryAxis, string> = {
  renewal: '续费压力',
  cost: '成本取舍',
  service_context: '承载上下文',
  monitoring: '监控证据',
  evidence: '资料完整度',
  lifecycle: '生命周期',
  review: '人工复核',
}

const COMPARISON_LANE_LABELS: Record<AssetDecisionComparisonLane, string> = {
  primary: '主力',
  standby: '备用',
  observe: '观察',
  retire: '退役',
  evidence: '补证据',
  review: '复核',
}

export function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

export function parseRenewalWindow(value?: string | null): RenewalWindow {
  const parsed = Number.parseInt(value ?? '', 10)
  return RENEWAL_WINDOWS.includes(parsed as RenewalWindow) ? (parsed as RenewalWindow) : 30
}

export function parseWorkbenchView(value?: string | null): WorkbenchView {
  switch (value) {
    case 'renewal_attention':
    case 'renewal':
      return 'renewal'
    case 'region_portfolio':
    case 'region':
      return 'region'
    case 'provider_portfolio':
    case 'provider':
      return 'provider'
    case 'cost_pressure':
    case 'cost':
      return 'cost'
    case 'evidence_gap':
    case 'evidence':
      return 'evidence'
    case 'single_queue':
      return 'single_queue'
    case 'needs_decision':
    default:
      return 'needs_decision'
  }
}

export function trimParam(value: string | null): string | undefined {
  const trimmed = value?.trim()
  return trimmed || undefined
}

function parseScenario(value: string | null): AssetDecisionManualGroupScenario | undefined {
  const normalized = trimParam(value)
  if (!normalized) return undefined
  return MANUAL_GROUP_SCENARIO_OPTIONS.some((option) => option.value === normalized)
    ? normalized as AssetDecisionManualGroupScenario
    : undefined
}

export function buildAssetDecisionFilter(searchParams: URLSearchParams, view: AssetDecisionView, renewalWindow: RenewalWindow): AssetDecisionGroupListFilter {
  const scenario = parseScenario(searchParams.get('scenario'))
  return {
    view,
    renew_within_days: renewalWindow,
    provider_id: trimParam(searchParams.get('provider_id')),
    vps_id: trimParam(searchParams.get('vps_id')),
    country: trimParam(searchParams.get('country')),
    region: trimParam(searchParams.get('region')),
    city: trimParam(searchParams.get('city')),
    scenario,
  }
}

export function assetDecisionFilterKey(filter: AssetDecisionGroupListFilter): string {
  return [
    filter.view ?? '',
    filter.renew_within_days ?? '',
    filter.provider_id ?? '',
    filter.vps_id ?? '',
    filter.country ?? '',
    filter.region ?? '',
    filter.city ?? '',
    filter.scenario ?? '',
  ].join('|')
}

export function portfolioViewForWorkbench(view: WorkbenchView): MainWorkbenchView {
  return view === 'single_queue' ? 'needs_decision' : view
}

export function buildContextFilterChips(filter: AssetDecisionGroupListFilter): ContextFilterChip[] {
  const chips: ContextFilterChip[] = []
  if (filter.provider_id) chips.push({ key: 'provider_id', label: '服务商', value: filter.provider_id })
  if (filter.vps_id) chips.push({ key: 'vps_id', label: 'VPS', value: filter.vps_id })
  if (filter.country) chips.push({ key: 'country', label: '国家', value: filter.country })
  if (filter.region) chips.push({ key: 'region', label: '区域', value: filter.region })
  if (filter.city) chips.push({ key: 'city', label: '城市', value: filter.city })
  if (filter.scenario) chips.push({ key: 'scenario', label: '场景', value: MANUAL_GROUP_SCENARIO_LABELS[filter.scenario] })
  return chips
}

function isObjectRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function parseComparisonAxis(value: unknown): AssetDecisionComparisonPrimaryAxis {
  return typeof value === 'string' && value in COMPARISON_AXIS_LABELS
    ? value as AssetDecisionComparisonPrimaryAxis
    : 'review'
}

function parseComparisonLane(value: unknown): AssetDecisionComparisonLane {
  return typeof value === 'string' && value in COMPARISON_LANE_LABELS
    ? value as AssetDecisionComparisonLane
    : 'review'
}

function parseComparisonSignal(value: unknown): AssetDecisionComparisonSignal | null {
  if (!isObjectRecord(value) || typeof value.kind !== 'string' || typeof value.label !== 'string') return null
  return {
    kind: value.kind,
    label: value.label,
    tone: typeof value.tone === 'string' ? value.tone : 'neutral',
    details: typeof value.details === 'string' ? value.details : undefined,
  }
}

function parseComparisonSignals(value: unknown): AssetDecisionComparisonSignal[] {
  if (!Array.isArray(value)) return []
  return value.map(parseComparisonSignal).filter((signal): signal is AssetDecisionComparisonSignal => signal != null)
}

export function parseComparisonInsight(snapshot?: AssetDecisionEvidenceSnapshot | null): AssetDecisionComparisonInsight | null {
  const raw = snapshot?.comparison_insight
  if (!isObjectRecord(raw)) return null
  const laneCounts = Array.isArray(raw.lane_counts)
    ? raw.lane_counts
      .map((item) => {
        if (!isObjectRecord(item) || typeof item.count !== 'number') return null
        return {
          lane: parseComparisonLane(item.lane),
          count: item.count,
        }
      })
      .filter((item): item is AssetDecisionComparisonInsight['lane_counts'][number] => item != null)
    : []
  return {
    summary: typeof raw.summary === 'string' && raw.summary.trim() ? raw.summary : '保存时对比洞察',
    primary_axis: parseComparisonAxis(raw.primary_axis),
    lane_counts: laneCounts,
    priority_vps_ids: Array.isArray(raw.priority_vps_ids)
      ? raw.priority_vps_ids.filter((item): item is string => typeof item === 'string')
      : [],
    tradeoffs: parseComparisonSignals(raw.tradeoffs),
  }
}

export function parseEvidenceAssessment(snapshot?: AssetDecisionEvidenceSnapshot | null): AssetDecisionEvidenceAssessment | null {
  const raw = snapshot?.evidence_assessment
  if (!raw || typeof raw !== 'object') return null
  const candidate = raw as Partial<AssetDecisionEvidenceAssessment>
  if (
    typeof candidate.confidence_score !== 'number' ||
    typeof candidate.pressure_score !== 'number' ||
    typeof candidate.readiness_score !== 'number' ||
    typeof candidate.quality_tier !== 'string' ||
    typeof candidate.decision_bias !== 'string'
  ) {
    return null
  }
  return {
    confidence_score: candidate.confidence_score,
    pressure_score: candidate.pressure_score,
    readiness_score: candidate.readiness_score,
    quality_tier: candidate.quality_tier as AssetDecisionEvidenceQualityTier,
    decision_bias: candidate.decision_bias as AssetDecisionEvidenceDecisionBias,
    support_signal_count: typeof candidate.support_signal_count === 'number' ? candidate.support_signal_count : 0,
    risk_signal_count: typeof candidate.risk_signal_count === 'number' ? candidate.risk_signal_count : 0,
    gap_signal_count: typeof candidate.gap_signal_count === 'number' ? candidate.gap_signal_count : 0,
    summary: typeof candidate.summary === 'string' ? candidate.summary : '证据评估快照',
  }
}

// VPS 和订阅相关工具函数

const MS_PER_DAY = 86400000

export function daysUntilDate(value?: string | null): number | null {
  if (!value) return null
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null

  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const targetDay = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()
  return Math.ceil((targetDay - today) / MS_PER_DAY)
}

export function isSubscriptionInRenewalWindow(
  subscription: SubscriptionRecord | null,
  windowDays: number,
): boolean {
  const days = daysUntilDate(subscription?.renew_at)
  return days != null && days <= windowDays
}

function subscriptionRenewalSortValue(subscription: SubscriptionRecord): number {
  if (!subscription.renew_at) return Number.POSITIVE_INFINITY
  const parsed = new Date(subscription.renew_at).getTime()
  return Number.isNaN(parsed) ? Number.POSITIVE_INFINITY : parsed
}

export function groupSubscriptionsByVPS(
  subscriptions: SubscriptionRecord[],
): Map<string, SubscriptionRecord[]> {
  const grouped = new Map<string, SubscriptionRecord[]>()
  for (const subscription of subscriptions) {
    const group = grouped.get(subscription.vps_id) ?? []
    group.push(subscription)
    grouped.set(subscription.vps_id, group)
  }

  for (const group of grouped.values()) {
    group.sort((left, right) => {
      const leftActive = left.status === 'active' ? 0 : 1
      const rightActive = right.status === 'active' ? 0 : 1
      if (leftActive !== rightActive) return leftActive - rightActive
      return subscriptionRenewalSortValue(left) - subscriptionRenewalSortValue(right)
    })
  }

  return grouped
}

export function selectPrimarySubscription(
  grouped: Map<string, SubscriptionRecord[]>,
  vpsID: string,
): SubscriptionRecord | null {
  return grouped.get(vpsID)?.[0] ?? null
}

function vpsLocationHasValue(vps: VPSAssetRecord): boolean {
  return Boolean(vps.country || vps.region || vps.city || vps.datacenter)
}

export function buildVPSQualityIssues(
  vps: VPSAssetRecord,
  subscription: SubscriptionRecord | null,
  options: { includeMissingSubscription?: boolean } = {},
): AssetQualityIssue[] {
  const issues: AssetQualityIssue[] = []
  const includeMissingSubscription = options.includeMissingSubscription ?? true

  if (includeMissingSubscription && !subscription) {
    issues.push({ key: 'missing-subscription', label: '缺订阅', tone: 'critical' })
  }
  if (vps.active_monitoring_instance_link_count <= 0) {
    issues.push({ key: 'unlinked-monitoring-instance', label: '未关联监控实例', tone: 'alert' })
  }
  if (!vps.provider_id && !(vps.provider_name ?? '').trim()) {
    issues.push({ key: 'missing-provider', label: '缺服务商', tone: 'notice' })
  }
  if (!vpsLocationHasValue(vps)) {
    issues.push({ key: 'missing-location', label: '缺位置', tone: 'notice' })
  }
  if (!vps.ssh_host && !vps.ipv4 && !vps.ipv6) {
    issues.push({ key: 'missing-access', label: '缺访问入口', tone: 'notice' })
  }

  return issues
}

export function sourceAvailabilityLabel(source: AssetDecisionOverview['source_availability'] | AssetDecisionGroupMember['source_availability']): string {
  const missing = [
    !source.subscriptions && '订阅',
    !source.services && '服务',
    !source.domains && '域名',
    !source.monitoring && '监控',
    !source.targets && 'Target',
  ].filter(Boolean)
  return missing.length > 0 ? `${missing.join('、')}证据不可用` : '证据源正常'
}

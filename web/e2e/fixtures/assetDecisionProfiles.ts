import type {
  AssetDecisionManualGroupSummary,
  AssetDecisionRecordSummary,
  AssetDecisionScenarioTemplateSummary,
  SubscriptionRecord,
  VPSAssetRecord,
} from '../../src/lib/types'
import { vpsAssetFixture } from '../../src/pages/dashboard/dashboardTestFixtures'
import { apiRouteKey, type ApiFixtureProfile } from './contracts'
import { coreRouteProfile } from './profiles'

// 浏览器时钟固定在上海 2026-10-02 12:00（playwright.config 的 timezoneId），续费日按同一日历生成。
// 用例须先 page.clock.setFixedTime(ASSET_DECISION_NOW)，避免 Node 与浏览器时区或跨午夜导致天数漂移。
export const ASSET_DECISION_NOW = new Date('2026-10-02T04:00:00Z')

function calendarDate(days: number): string {
  return new Date(ASSET_DECISION_NOW.getTime() + days * 86_400_000).toISOString().slice(0, 10)
}

const STAMP = '2026-09-20T08:00:00Z'
const SOURCES = { subscriptions: true, services: true, domains: true, monitoring: true, targets: true }

type QueueVPS = readonly [id: string, name: string, provider: string, country: string, city: string, decision: 'unreviewed' | 'cancel', links: number]

const QUEUE_VPS: readonly QueueVPS[] = [
  ['vps_tyo', 'Tokyo Edge', 'Example Cloud', 'JP', 'Tokyo', 'unreviewed', 1],
  ['vps_fra', 'Frankfurt Mirror', 'Hetzner Online GmbH Falkenstein', 'DE', 'Frankfurt', 'unreviewed', 2],
  ['vps_sgp', 'Singapore Probe', 'Lightnode', 'SG', 'Singapore', 'unreviewed', 0],
  ['vps_lax', 'LA Backup', 'RackNerd', 'US', 'Los Angeles', 'unreviewed', 1],
  ['vps_sel', 'Seoul Relay', 'Vultr', 'KR', 'Seoul', 'cancel', 0],
  ['vps_ams', 'Amsterdam Build', 'DigitalOcean', 'NL', 'Amsterdam', 'cancel', 1],
]

function queueVPS([vpsId, name, provider, country, city, decision, links]: QueueVPS): VPSAssetRecord {
  return vpsAssetFixture({
    vps_id: vpsId,
    display_name: name,
    provider_id: `pv_${vpsId.slice(4)}`,
    provider_name: provider,
    country,
    region: city,
    city,
    renewal_decision: decision,
    active_monitoring_instance_link_count: links,
  })
}

const VPS_ROWS = QUEUE_VPS.map(queueVPS)

// [vps_id, 续费在几天后, 原价, 币种, 月成本基准, 汇率过期, 续费方式]
const SUBSCRIPTION_ROWS = [
  ['vps_fra', 4, 18.29, 'USD', 128, false, 'auto'],
  ['vps_tyo', 9, 12, 'USD', 84, true, 'auto'],
  ['vps_lax', 21, 2.57, 'USD', 18, false, 'manual'],
  ['vps_sel', 26, 30, 'EUR', 236, false, 'auto'],
  ['vps_ams', 64, 6, 'USD', 42, false, 'auto_cancelled'],
] as const

function subscriptionRow([vpsId, days, price, currency, base, stale, mode]: (typeof SUBSCRIPTION_ROWS)[number]): SubscriptionRecord {
  return {
    subscription_id: `sub_${vpsId.slice(4)}`,
    vps_id: vpsId,
    price,
    currency,
    billing_cycle: 'monthly',
    billing_months: 1,
    billing_period_unit: 'month',
    billing_period_length: 1,
    monthly_price: price,
    monthly_price_base: base,
    yearly_price_base: base * 12,
    base_currency: 'CNY',
    exchange_rate: base / price,
    exchange_rate_date: '2026-09-30',
    exchange_rate_stale: stale,
    budget_status: 'ok',
    started_at: '2026-01-01',
    renew_at: calendarDate(days),
    auto_renew: mode !== 'manual',
    auto_renew_cancelled: mode === 'auto_cancelled',
    renewal_mode: mode,
    status: 'active',
    payment_method: 'card',
    display_name: `${vpsId.slice(4).toUpperCase()} 月付`,
    cost_category: 'compute',
    labels: [],
    note: '',
    created_at: STAMP,
    updated_at: STAMP,
  }
}

const SUBSCRIPTIONS = SUBSCRIPTION_ROWS.map(subscriptionRow)

// 与后端 BuiltinScenarioTemplates 一致的标题与目标。
const BUILTIN_TEMPLATES = [
  ['primary_standby', '主力与容灾取舍', '比较主力、备用和容灾 VPS 的保留优先级'],
  ['budget_reduction', '预算压缩', '找出高成本、弱承载或闲置付费资产'],
  ['provider_review', '服务商组合复核', '比较同服务商多台 VPS 的成本、承载和异常信号'],
  ['region_review', '同区取舍', '比较同国家/地区/城市内多台 VPS 的保留角色'],
  ['migration_retirement', '迁移与退役收尾', '核对迁移、取消、过期状态割裂和仍在运行的关联对象'],
  ['evidence_cleanup', '资料补齐', '补齐订阅、监控、服务上下文和基础资料缺口'],
  ['general', '通用组合判断', '从任意 VPS 篮子开始记录组合目标和成员意图'],
] as const

function template(id: string, title: string, scenario: string, patch: Partial<AssetDecisionScenarioTemplateSummary> = {}): AssetDecisionScenarioTemplateSummary {
  return {
    template_id: id,
    builtin: true,
    status: 'active',
    scenario: scenario as AssetDecisionScenarioTemplateSummary['scenario'],
    title,
    goal: `${title}：比较候选 VPS 的保留与退出路径`,
    note: '',
    member_count: 0,
    created_at: STAMP,
    updated_at: STAMP,
    archived_at: null,
    ...patch,
  }
}

// 场景模板 API 固定先返回 7 个内置模板，再返回自定义模板；集合不得被截断。
const TEMPLATES = [
  ...BUILTIN_TEMPLATES.map(([scenario, title, goal]) => template(`adt_builtin_${scenario}`, title, scenario, { goal })),
  template('adt_custom_001', '日本节点季度复核', 'primary_standby', { builtin: false, member_count: 3, goal: '季度复核东京与大阪节点' }),
  template('adt_custom_002', '欧洲旧机退役清单', 'migration_retirement', { builtin: false, status: 'archived', member_count: 2, goal: '' }),
]

function manualGroup(id: string, title: string, scenario: string, status: string, members: number, patch: Partial<AssetDecisionManualGroupSummary> = {}) {
  return {
    manual_group_id: id,
    status,
    scenario,
    title,
    goal: '',
    note: '',
    source_type: 'manual',
    renew_within_days: 30,
    member_count: members,
    lifecycle_counts: { active: members },
    usage_tag_counts: { in_use: members },
    renewal_decision_counts: { keep: members },
    renewal_window_count: 1,
    unreviewed_count: 0,
    migrate_count: 0,
    cancel_count: 0,
    cancellation_attention_count: 0,
    idle_count: 0,
    standby_count: 0,
    in_use_count: members,
    service_count: members,
    domain_count: 1,
    target_count: 1,
    running_target_count: 1,
    monitoring_link_count: members,
    abnormal_monitoring_count: 0,
    active_incident_count: 0,
    primary_issue_summary: '',
    monthly_cost_by_currency: [{ currency: 'CNY', monthly_total: members * 60, yearly_total: members * 720 }],
    evidence_chips: [],
    source_availability: SOURCES,
    created_at: STAMP,
    updated_at: STAMP,
    archived_at: null,
    ...patch,
  } as unknown as AssetDecisionManualGroupSummary
}

const MANUAL_GROUPS = [
  manualGroup('admg_001', '东京主备取舍', 'primary_standby', 'active', 2),
  manualGroup('admg_002', '欧洲节点收敛（法兰克福 / 阿姆斯特丹 / 赫尔辛基三地合并评估）', 'region_review', 'active', 3),
  manualGroup('admg_003', '低价备份机清理', 'budget_reduction', 'archived', 4),
]

type RecordSeed = {
  id: string
  title: string
  status: string
  view: string
  members: number
  done: number
  blocked: number
  readback: string
  drift: number
  needsEvidence: number
  updated: string
}

function record(seed: RecordSeed): AssetDecisionRecordSummary {
  const open = seed.members - seed.done - seed.blocked
  return {
    record_id: seed.id,
    title: seed.title,
    goal: '',
    status: seed.status,
    source_type: 'auto_group',
    source_group_id: `adg_${seed.id.slice(4)}`,
    source_group_type: 'renewal_attention',
    source_view: seed.view,
    scope_key: 'renewal-window',
    scope_label: '未来 30 天',
    renew_within_days: 30,
    member_count: seed.members,
    followup_todo_count: Math.max(0, open),
    followup_in_progress_count: 0,
    followup_blocked_count: seed.blocked,
    followup_done_count: seed.done,
    followup_skipped_count: 0,
    evidence_snapshot: { group_id: `adg_${seed.id.slice(4)}`, monthly_cost_base: 140, base_currency: 'CNY' },
    execution_readback: {
      status: seed.readback,
      summary: '',
      open_count: Math.max(0, open),
      aligned_count: seed.done,
      drift_count: seed.drift,
      blocked_count: seed.blocked,
      needs_evidence_count: seed.needsEvidence,
    },
    execution_plan: { summary: '', lane_counts: [], actionable_count: Math.max(0, open), blocked_count: seed.blocked },
    created_at: seed.updated,
    updated_at: seed.updated,
    decided_at: null,
    completed_at: seed.status === 'completed' ? seed.updated : null,
  } as unknown as AssetDecisionRecordSummary
}

const RECORDS = [
  record({ id: 'adr_001', title: '十月续费窗口取舍', status: 'in_progress', view: 'renewal', members: 4, done: 1, blocked: 1, readback: 'blocked', drift: 0, needsEvidence: 0, updated: '2026-09-30T10:20:00Z' }),
  record({ id: 'adr_002', title: '东京主备取舍记录', status: 'draft', view: 'needs_decision', members: 2, done: 0, blocked: 0, readback: 'needs_evidence', drift: 0, needsEvidence: 1, updated: '2026-09-28T03:00:00Z' }),
  record({ id: 'adr_003', title: '欧洲服务商整体评估：Hetzner 与 DigitalOcean 主机成本、带宽与迁移代价对比', status: 'in_progress', view: 'provider', members: 5, done: 3, blocked: 0, readback: 'drift', drift: 2, needsEvidence: 0, updated: '2026-09-21T09:40:00Z' }),
  record({ id: 'adr_004', title: '低价备份机退役', status: 'completed', view: 'cost', members: 3, done: 3, blocked: 0, readback: 'aligned', drift: 0, needsEvidence: 0, updated: '2026-09-02T12:00:00Z' }),
]

/** 资产决策：四个次级工作区（记录、场景与组合、续费窗口、单台队列）均有数据。 */
export function assetDecisionWorkbenchesProfile(): ApiFixtureProfile {
  const base = coreRouteProfile('/asset-decisions')
  const window30 = SUBSCRIPTIONS.filter((row) => row.renew_at! <= calendarDate(30))
  return {
    ...base,
    [apiRouteKey('GET', '/api/asset-decisions/manual-groups?view=needs_decision&renew_within_days=30')]: { status: 200, body: MANUAL_GROUPS },
    [apiRouteKey('GET', '/api/asset-decisions/scenario-templates')]: { status: 200, body: TEMPLATES },
    [apiRouteKey('GET', '/api/asset-decisions/records?view=needs_decision&renew_within_days=30')]: { status: 200, body: RECORDS },
    [apiRouteKey('GET', '/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc')]: { status: 200, body: window30 },
    [apiRouteKey('GET', '/api/subscriptions?sort=renew_at&order=asc')]: { status: 200, body: SUBSCRIPTIONS },
    [apiRouteKey('GET', '/api/vps')]: { status: 200, body: VPS_ROWS },
    [apiRouteKey('GET', '/api/vps?renewal_decision=unreviewed')]: { status: 200, body: VPS_ROWS.filter((row) => row.renewal_decision === 'unreviewed') },
    [apiRouteKey('GET', '/api/vps?renewal_decision=cancel')]: { status: 200, body: VPS_ROWS.filter((row) => row.renewal_decision === 'cancel') },
  }
}

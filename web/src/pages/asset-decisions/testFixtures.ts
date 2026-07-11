import { fireEvent, screen, within } from '@testing-library/react'
import { createElement } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { expect, vi } from 'vitest'


export function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

export function LocationProbe() {
  const location = useLocation()
  return createElement('output', { 'aria-label': 'current-url' }, location.pathname, location.search)
}

export function HistoryControls() {
  const navigate = useNavigate()
  return [
    createElement('button', { key: 'back', type: 'button', onClick: () => navigate(-1) }, '返回上一条历史'),
    createElement('button', { key: 'forward', type: 'button', onClick: () => navigate(1) }, '前往下一条历史'),
  ]
}

export const sourceAvailability = {
  subscriptions: true,
  services: true,
  domains: true,
  monitoring: true,
  targets: true,
}

export const subscription = {
  subscription_id: 'sub_001',
  vps_id: 'vps_review',
  price: 12,
  currency: 'USD',
  billing_cycle: 'monthly',
  billing_months: 1,
  monthly_price: 12,
  monthly_price_base: 84,
  yearly_price_base: 1008,
  base_currency: 'CNY',
  started_at: '2026-05-01',
  renew_at: '2026-06-20',
  auto_renew: true,
  auto_renew_cancelled: false,
  status: 'active',
  payment_method: 'card',
  note: '',
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
}

export const vps = {
  vps_id: 'vps_review',
  display_name: 'Tokyo Review',
  provider_id: 'pv_001',
  provider_name: 'Hetzner',
  product_name: 'cx22',
  order_ref: 'ord-1',
  country: 'JP',
  region: 'Kanto',
  city: 'Tokyo',
  datacenter: 'nrt',
  ipv4: '192.0.2.1',
  ipv6: '',
  ssh_host: '192.0.2.1',
  ssh_port: 22,
  ssh_user: 'root',
  os_name: 'Debian',
  virtualization: 'kvm',
  lifecycle_status: 'active',
  usage_status: 'in_use',
  renewal_decision: 'unreviewed',
  importance: 'normal',
  labels: ['edge'],
  note: '',
  active_monitoring_instance_link_count: 1,
  running_monitoring_instance_count: 1,
  running_target_count: 1,
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
  archived_at: null,
}

export const migrateVPS = {
  ...vps,
  vps_id: 'vps_migrate',
  display_name: 'Frankfurt Migration',
  country: 'DE',
  region: 'Hesse',
  city: 'Frankfurt',
  renewal_decision: 'migrate',
}

export const cancelVPS = {
  ...vps,
  vps_id: 'vps_cancel',
  display_name: 'Seoul Cancel',
  country: 'KR',
  region: 'Seoul',
  city: 'Seoul',
  renewal_decision: 'cancel',
  active_monitoring_instance_link_count: 0,
  running_monitoring_instance_count: 0,
  running_target_count: 0,
}

export function evidenceAssessment(overrides: Record<string, unknown> = {}) {
  return {
    confidence_score: 82,
    pressure_score: 38,
    readiness_score: 76,
    quality_tier: 'strong',
    decision_bias: 'keep',
    support_signal_count: 5,
    risk_signal_count: 1,
    gap_signal_count: 0,
    summary: '证据完整：可保存组合判断',
    ...overrides,
  }
}

export function comparisonInsight(overrides: Record<string, unknown> = {}) {
  return {
    summary: '主力承载明确，备用仍需补齐订阅和监控证据',
    primary_axis: 'service_context',
    lane_counts: [
      { lane: 'primary', count: 1 },
      { lane: 'evidence', count: 1 },
    ],
    priority_vps_ids: ['vps_primary', 'vps_standby'],
    tradeoffs: [
      { kind: 'service_context', label: '承载差异', tone: 'notice', details: '主力承载服务，备用资料不足' },
    ],
    ...overrides,
  }
}

export function memberComparisonInsight(overrides: Record<string, unknown> = {}) {
  return {
    rank: 1,
    lane: 'primary',
    summary: '主力候选：承载服务且监控证据可用',
    strengths: [
      { kind: 'service_context', label: '承载服务', tone: 'normal' },
      { kind: 'monitoring', label: '监控在线', tone: 'normal' },
    ],
    risks: [],
    gaps: [],
    tradeoffs: [],
    ...overrides,
  }
}

export function groupSummary(overrides: Record<string, unknown> = {}) {
  return {
    group_id: 'adg_auto_001',
    group_type: 'renewal_attention',
    view: 'renewal',
    title: '德国主力组合',
    scope_key: 'renewal-window',
    scope_label: '未来 30 天',
    priority: 90,
    member_count: 2,
    lifecycle_counts: { active: 2 },
    usage_counts: { in_use: 1, standby: 1 },
    renewal_decision_counts: { unreviewed: 2 },
    renewal_window_count: 2,
    unreviewed_count: 1,
    migrate_count: 0,
    cancel_count: 0,
    cancellation_attention_count: 0,
    idle_count: 0,
    standby_count: 1,
    in_use_count: 1,
    service_count: 2,
    domain_count: 1,
    target_count: 1,
    running_target_count: 1,
    monitoring_link_count: 2,
    abnormal_monitoring_count: 0,
    active_incident_count: 0,
    primary_issue_summary: '',
    monthly_cost_by_currency: [{ currency: 'USD', monthly_total: 20, yearly_total: 240 }],
    evidence_chips: [{ kind: 'renewal_due', label: '续费临近', tone: 'alert' }],
    evidence_assessment: evidenceAssessment(),
    comparison_insight: comparisonInsight(),
    ...overrides,
  }
}

export function overview(overrides: Record<string, unknown> = {}) {
  return {
    snapshot_generated_at: '2026-06-04T09:00:00Z',
    renew_within_days: 30,
    group_count: 3,
    member_vps_count: 4,
    needs_decision_count: 1,
    renewal_group_count: 1,
    region_group_count: 1,
    provider_group_count: 1,
    cost_group_count: 1,
    evidence_group_count: 1,
    top_groups: [groupSummary()],
    type_counts: { renewal_attention: 1 },
    view_counts: { renewal: 1 },
    source_availability: sourceAvailability,
    ...overrides,
  }
}

export function groupDetail() {
  return {
    ...groupSummary(),
    members: [
      {
        vps: {
          ...vps,
          vps_id: 'vps_primary',
          display_name: 'Germany Primary',
          country: 'DE',
          region: 'Hesse',
          city: 'Frankfurt',
          renewal_decision: 'keep',
        },
        primary_subscription: {
          ...subscription,
          subscription_id: 'sub_primary',
          vps_id: 'vps_primary',
          monthly_price: 20,
        },
        subscription_count: 1,
        active_subscription_count: 1,
        inactive_subscription_count: 0,
        service_count: 2,
        domain_count: 1,
        target_count: 1,
        running_target_count: 1,
        monitoring_link_count: 1,
        running_monitoring_count: 1,
        abnormal_monitoring_count: 0,
        active_incident_count: 0,
        primary_issue_summary: '',
        suggested_role: 'primary_candidate',
        suggested_action: 'keep',
        evidence_chips: [{ kind: 'carries_service', label: '承载服务', tone: 'normal' }],
        evidence_assessment: evidenceAssessment(),
        comparison_insight: memberComparisonInsight(),
        renewal_within_window: true,
        source_availability: sourceAvailability,
      },
      {
        vps: {
          ...vps,
          vps_id: 'vps_standby',
          display_name: 'Germany Standby',
          country: 'DE',
          region: 'Hesse',
          city: 'Frankfurt',
          usage_status: 'standby',
          renewal_decision: 'observe',
        },
        primary_subscription: null,
        subscription_count: 0,
        active_subscription_count: 0,
        inactive_subscription_count: 0,
        service_count: 0,
        domain_count: 0,
        target_count: 0,
        running_target_count: 0,
        monitoring_link_count: 0,
        running_monitoring_count: 0,
        abnormal_monitoring_count: 0,
        active_incident_count: 0,
        primary_issue_summary: '',
        suggested_role: 'evidence_needed',
        suggested_action: 'complete_evidence',
        evidence_chips: [{ kind: 'missing_subscription', label: '缺订阅', tone: 'alert' }],
        evidence_assessment: evidenceAssessment({
          confidence_score: 34,
          pressure_score: 18,
          readiness_score: 22,
          quality_tier: 'blocked',
          decision_bias: 'complete_evidence',
          support_signal_count: 1,
          risk_signal_count: 0,
          gap_signal_count: 3,
          summary: '证据阻塞：3 项缺口，先补齐资料',
        }),
        comparison_insight: memberComparisonInsight({
          rank: 2,
          lane: 'evidence',
          summary: '补证据候选：缺订阅和监控关联后再判断是否备用',
          strengths: [],
          risks: [{ kind: 'missing_subscription', label: '缺订阅', tone: 'alert' }],
          gaps: [
            { kind: 'missing_subscription', label: '缺订阅', tone: 'alert' },
            { kind: 'missing_monitoring', label: '未关联监控', tone: 'alert' },
          ],
        }),
        renewal_within_window: false,
        source_availability: sourceAvailability,
      },
    ],
  }
}

export function recordReadback(overrides: Record<string, unknown> = {}) {
  return {
    status: 'needs_evidence',
    summary: '1 台 VPS 仍需补证据',
    open_count: 0,
    aligned_count: 1,
    drift_count: 0,
    blocked_count: 0,
    needs_evidence_count: 1,
    ...overrides,
  }
}

export function memberReadback(overrides: Record<string, unknown> = {}) {
  return {
    status: 'aligned',
    summary: '当前事实与判断一致',
    issues: [],
    current_facts: {
      found: true,
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'keep',
      active_subscription_count: 1,
      service_count: 2,
      domain_count: 1,
      target_count: 1,
      running_target_count: 1,
      monitoring_link_count: 1,
      running_monitoring_count: 1,
      abnormal_monitoring_count: 0,
      active_incident_count: 0,
      source_availability: sourceAvailability,
    },
    ...overrides,
  }
}

export function recordExecutionPlan(overrides: Record<string, unknown> = {}) {
  return {
    summary: '1 台 VPS 需要补齐证据',
    lane_counts: [
      { lane: 'evidence', count: 1 },
      { lane: 'keep_observe', count: 1 },
    ],
    actionable_count: 2,
    blocked_count: 0,
    ...overrides,
  }
}

export function memberExecutionPlan(overrides: Record<string, unknown> = {}) {
  return {
    lane: 'keep_observe',
    step_kind: 'open_vps_detail',
    tone: 'normal',
    summary: '当前事实已对齐，待确认跟进状态',
    step_label: '打开 VPS 详情核对判断',
    issue_count: 0,
    blocked: false,
    actionable: true,
    ...overrides,
  }
}

export function decisionRecord(overrides: Record<string, unknown> = {}) {
  return {
    record_id: 'adr_001',
    title: '德国主备取舍记录',
    goal: '保留主力并补齐备用证据',
    status: 'draft',
    source_type: 'auto_group',
    source_group_id: 'adg_auto_001',
    source_group_type: 'renewal_attention',
    source_view: 'renewal',
    scope_key: 'renewal-window',
    scope_label: '未来 30 天',
    renew_within_days: 30,
    member_count: 2,
    followup_todo_count: 2,
    followup_in_progress_count: 0,
    followup_blocked_count: 0,
    followup_done_count: 0,
    followup_skipped_count: 0,
    execution_readback: recordReadback(),
    execution_plan: recordExecutionPlan(),
    evidence_snapshot: {
      group_id: 'adg_auto_001',
      monthly_cost_base: 140,
      base_currency: 'CNY',
      comparison_insight: comparisonInsight({
        summary: '保存时判断：主力保留，备用补证据后再观察',
        primary_axis: 'service_context',
      }),
      evidence_assessment: evidenceAssessment({
        confidence_score: 68,
        pressure_score: 42,
        readiness_score: 61,
        quality_tier: 'usable',
        decision_bias: 'review',
        summary: '证据可用：复核后决策',
      }),
    },
    created_at: '2026-06-05T08:00:00Z',
    updated_at: '2026-06-05T08:00:00Z',
    decided_at: null,
    completed_at: null,
    members: [
      {
        record_id: 'adr_001',
        vps_id: 'vps_primary',
        display_name: 'Germany Primary',
        suggested_role: 'primary_candidate',
        decided_role: 'primary_candidate',
        suggested_action: 'keep',
        decided_action: 'keep',
        reason: '主力保留',
        followup_status: 'todo',
        followup_note: '',
        followup_updated_at: null,
        execution_readback: memberReadback(),
        execution_plan: memberExecutionPlan(),
        evidence_snapshot: {
          service_count: 2,
          domain_count: 1,
          running_monitoring_count: 1,
          monitoring_link_count: 1,
          primary_issue_summary: '',
          comparison_insight: memberComparisonInsight({
            summary: '保存时成员判断：主力保留',
          }),
          evidence_assessment: evidenceAssessment(),
        },
        created_at: '2026-06-05T08:00:00Z',
        updated_at: '2026-06-05T08:00:00Z',
      },
    ],
    ...overrides,
  }
}

export function manualGroupDetail(overrides: Record<string, unknown> = {}) {
  return {
    manual_group_id: 'admg_001',
    status: 'active',
    scenario: 'primary_standby',
    title: '德国主备自定义组合',
    goal: '保留主力，观察备用',
    note: '从自动组创建',
    source_type: 'auto_group',
    source_group_id: 'adg_auto_001',
    source_group_type: 'renewal_attention',
    source_view: 'renewal',
    scope_key: 'renewal-window',
    scope_label: '未来 30 天',
    renew_within_days: 30,
    member_count: 1,
    lifecycle_counts: { active: 1 },
    usage_counts: { in_use: 1 },
    renewal_decision_counts: { keep: 1 },
    renewal_window_count: 1,
    unreviewed_count: 0,
    migrate_count: 0,
    cancel_count: 0,
    cancellation_attention_count: 0,
    idle_count: 0,
    standby_count: 0,
    in_use_count: 1,
    service_count: 2,
    domain_count: 1,
    target_count: 1,
    running_target_count: 1,
    monitoring_link_count: 1,
    abnormal_monitoring_count: 0,
    active_incident_count: 0,
    primary_issue_summary: '',
    monthly_cost_by_currency: [{ currency: 'USD', monthly_total: 20, yearly_total: 240 }],
    evidence_chips: [{ kind: 'carries_service', label: '承载服务', tone: 'normal' }],
    evidence_assessment: evidenceAssessment(),
    comparison_insight: comparisonInsight({
      summary: '自定义组合中主力证据清晰，可保存记录',
      lane_counts: [{ lane: 'primary', count: 1 }],
      priority_vps_ids: ['vps_primary'],
    }),
    source_availability: sourceAvailability,
    created_at: '2026-06-06T08:00:00Z',
    updated_at: '2026-06-06T08:00:00Z',
    archived_at: null,
    members: [
      {
        ...groupDetail().members[0],
        manual_group_id: 'admg_001',
        vps_id: 'vps_primary',
        intended_role: 'primary_candidate',
        intended_action: 'keep',
        reason: '主力稳定',
        note: '先保留',
        sort_order: 10,
        evidence_snapshot: { vps_id: 'vps_primary', service_count: 2 },
        current_fact_found: true,
        created_at: '2026-06-06T08:00:00Z',
        updated_at: '2026-06-06T08:00:00Z',
      },
    ],
    ...overrides,
  }
}

export function cloneGroupMember(index: number) {
  const [primary, standby] = groupDetail().members
  const baseMember = index % 2 === 0 ? primary : standby
  const lane = index % 3 === 0 ? 'primary' : index % 3 === 1 ? 'evidence' : 'review'
  const suggestedRole = lane === 'primary' ? 'primary_candidate' : lane === 'evidence' ? 'evidence_needed' : 'observe_candidate'
  const suggestedAction = lane === 'primary' ? 'keep' : lane === 'evidence' ? 'complete_evidence' : 'review'
  return {
    ...baseMember,
    vps: {
      ...baseMember.vps,
      vps_id: `vps_bulk_${index}`,
      display_name: `Bulk Member ${index}`,
      renewal_decision: suggestedAction === 'keep' ? 'keep' : 'unreviewed',
    },
    primary_subscription: baseMember.primary_subscription
      ? {
        ...baseMember.primary_subscription,
        subscription_id: `sub_bulk_${index}`,
        vps_id: `vps_bulk_${index}`,
      }
      : null,
    suggested_role: suggestedRole,
    suggested_action: suggestedAction,
    evidence_assessment: evidenceAssessment({
      quality_tier: lane === 'evidence' ? 'blocked' : 'usable',
      decision_bias: suggestedAction,
    }),
    comparison_insight: memberComparisonInsight({
      rank: index,
      lane,
      summary: `Bulk Member ${index} 的紧凑取舍判断`,
    }),
  }
}

export function groupDetailWithManyMembers(count = 8) {
  const members = Array.from({ length: count }, (_, index) => cloneGroupMember(index + 1))
  return {
    ...groupDetail(),
    member_count: members.length,
    members,
  }
}

export function manualGroupDetailWithManyMembers(count = 8) {
  const members = groupDetailWithManyMembers(count).members.map((member, index) => ({
    ...member,
    manual_group_id: 'admg_001',
    vps_id: member.vps.vps_id,
    intended_role: member.suggested_role,
    intended_action: member.suggested_action,
    reason: `成员 ${index + 1} 取舍理由`,
    note: '',
    sort_order: (index + 1) * 10,
    evidence_snapshot: { vps_id: member.vps.vps_id, service_count: member.service_count },
    current_fact_found: true,
    created_at: '2026-06-06T08:00:00Z',
    updated_at: '2026-06-06T08:00:00Z',
  }))
  return {
    ...manualGroupDetail(),
    member_count: members.length,
    members,
  }
}

export function cloneRecordMember(index: number) {
  const baseMember = decisionRecord().members[0]
  const lane = index % 3 === 0 ? 'cancel_retire' : index % 3 === 1 ? 'evidence' : 'keep_observe'
  const stepKind = lane === 'cancel_retire'
    ? 'open_cancellation_workbench'
    : lane === 'evidence'
      ? 'open_subscription_context'
      : 'open_vps_detail'
  return {
    ...baseMember,
    vps_id: `vps_record_bulk_${index}`,
    display_name: `Record Bulk ${index}`,
    decided_action: lane === 'cancel_retire' ? 'open_cancellation_workbench' : lane === 'evidence' ? 'complete_evidence' : 'keep',
    followup_status: 'todo',
    execution_readback: memberReadback({
      status: lane === 'evidence' ? 'needs_evidence' : 'aligned',
      summary: `Record Bulk ${index} 当前回读`,
      issues: lane === 'evidence' ? [{ kind: 'missing_subscription', label: '缺订阅', tone: 'alert', details: '' }] : [],
    }),
    execution_plan: memberExecutionPlan({
      lane,
      step_kind: stepKind,
      tone: lane === 'cancel_retire' ? 'critical' : lane === 'evidence' ? 'alert' : 'normal',
      summary: `Record Bulk ${index} 的执行下一步`,
      step_label: lane === 'cancel_retire' ? '打开取消/退役工作台' : lane === 'evidence' ? '核对订阅上下文' : '打开 VPS 详情核对判断',
      issue_count: lane === 'evidence' ? 1 : 0,
      blocked: false,
      actionable: true,
    }),
  }
}

export function decisionRecordWithManyMembers(count = 8) {
  const members = Array.from({ length: count }, (_, index) => cloneRecordMember(index + 1))
  return decisionRecord({
    member_count: members.length,
    followup_todo_count: members.length,
    execution_plan: recordExecutionPlan({
      lane_counts: [
        { lane: 'evidence', count: Math.ceil(count / 3) },
        { lane: 'keep_observe', count: Math.floor(count / 3) },
        { lane: 'cancel_retire', count: Math.floor(count / 3) },
      ],
      actionable_count: members.length,
    }),
    members,
  })
}

export function manualGroupSummary(overrides: Record<string, unknown> = {}) {
  const detail = manualGroupDetail()
  const summary = {
    ...detail,
    members: undefined,
  }
  delete summary.members
  return {
    ...summary,
    ...overrides,
  }
}

export function scenarioTemplate(overrides: Record<string, unknown> = {}) {
  return {
    template_id: 'adt_builtin_primary_standby',
    builtin: true,
    status: 'active',
    scenario: 'primary_standby',
    title: '主备取舍模板',
    goal: '比较主力与备用 VPS 的保留、观察和补证据路径',
    note: '创建组合时重新读取当前事实',
    source_manual_group_id: null,
    member_count: 0,
    created_at: '2026-06-06T08:00:00Z',
    updated_at: '2026-06-06T08:00:00Z',
    archived_at: null,
    members: [],
    ...overrides,
  }
}

export type InitialWorkbenchOptions = {
  overviewBody?: unknown
  groupsBody?: unknown
  manualGroupsBody?: unknown
  recordsBody?: unknown
  templatesBody?: unknown
  renewalEvidenceBody?: unknown
  subscriptionsBody?: unknown
  unreviewedBody?: unknown
  migrateBody?: unknown
  cancelBody?: unknown
  vpsCatalogBody?: unknown
  routes?: MockFetchRoute[]
}

export type MockFetchRoute = {
  url: string
  method?: string
  body?: unknown
  status?: number
  responses?: Array<{ body: unknown; status?: number }>
}

export function initialWorkbenchResponse(url: string, options: InitialWorkbenchOptions = {}) {
  if (url.startsWith('/api/asset-decisions/overview')) {
    return options.overviewBody ?? overview()
  }
  if (url.startsWith('/api/asset-decisions/groups?')) {
    return options.groupsBody ?? [groupSummary()]
  }
  if (url.startsWith('/api/asset-decisions/records?') || url === '/api/asset-decisions/records') {
    return options.recordsBody ?? [decisionRecord()]
  }
  if (url.startsWith('/api/asset-decisions/manual-groups?') || url === '/api/asset-decisions/manual-groups') {
    return options.manualGroupsBody ?? [manualGroupSummary()]
  }
  if (url === '/api/asset-decisions/scenario-templates') {
    return options.templatesBody ?? [scenarioTemplate()]
  }
  if (url.startsWith('/api/subscriptions?renew_within_days=')) {
    return options.renewalEvidenceBody ?? [subscription]
  }
  if (url === '/api/subscriptions?sort=renew_at&order=asc') {
    return options.subscriptionsBody ?? [subscription]
  }
  if (url === '/api/vps?renewal_decision=unreviewed') {
    return options.unreviewedBody ?? [vps]
  }
  if (url === '/api/vps?renewal_decision=migrate') {
    return options.migrateBody ?? [migrateVPS]
  }
  if (url === '/api/vps?renewal_decision=cancel') {
    return options.cancelBody ?? [cancelVPS]
  }
  if (url === '/api/vps') {
    return options.vpsCatalogBody ?? [
      vps,
      migrateVPS,
      cancelVPS,
      ...groupDetail().members.map((member) => member.vps),
    ]
  }
  return undefined
}

export function mockInitialWorkbench(fetchMock: ReturnType<typeof vi.fn>, options: InitialWorkbenchOptions = {}) {
  const routes = (options.routes ?? []).map((route) => ({
    ...route,
    responses: route.responses ? [...route.responses] : [{ body: route.body, status: route.status }],
  }))
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const route = routes.find((candidate) => candidate.url === url && candidate.method === method)
      ?? routes.find((candidate) => candidate.url === url && !candidate.method && method === 'GET')
    if (route) {
      const response = route.responses.length > 1 ? route.responses.shift()! : route.responses[0]
      return Promise.resolve(mockJSONResponse(response.body, response.status ?? 200))
    }
    if (method !== 'GET') {
      return Promise.reject(new Error(`unhandled ${method} request: ${url}`))
    }
    const body = initialWorkbenchResponse(url, options)
    if (body !== undefined) return Promise.resolve(mockJSONResponse(body))
    return Promise.reject(new Error(`unhandled request: ${url}`))
  })
}

export function expectFetchCalledWith(fetchMock: ReturnType<typeof vi.fn>, url: string, init?: RequestInit) {
  if (init) {
    expect(fetchMock).toHaveBeenCalledWith(url, init)
    return
  }
  expect(fetchMock.mock.calls.some((call) => call[0] === url)).toBe(true)
}

export function findFetchCall(fetchMock: ReturnType<typeof vi.fn>, url: string, method?: string) {
  return fetchMock.mock.calls.find((call) => {
    if (call[0] !== url) return false
    if (!method) return true
    return call[1]?.method === method
  })
}

export function fetchRequestInventory(fetchMock: ReturnType<typeof vi.fn>, startIndex = 0): string[] {
  return fetchMock.mock.calls
    .slice(startIndex)
    .map((call) => `${call[1]?.method ?? 'GET'} ${call[0]}`)
    .sort()
}

export type SecondaryWorkbenchLabel = '保存记录' | '场景与组合' | '续费窗口' | '单台队列'

export function getSecondaryWorkbenchButton(supportStrip: HTMLElement, label: SecondaryWorkbenchLabel) {
  return within(supportStrip).getByRole('button', { name: label })
}

export function expectTabPanelRelationship(container: HTMLElement, tablistName: string) {
  const tablist = within(container).getByRole('tablist', { name: tablistName })
  const activeTab = within(tablist).getByRole('tab', { selected: true })
  const panel = within(container).getByRole('tabpanel')
  expect(activeTab.id).not.toBe('')
  expect(panel.id).not.toBe('')
  expect(activeTab).toHaveAttribute('aria-controls', panel.id)
  expect(panel).toHaveAttribute('aria-labelledby', activeTab.id)
}

export async function findSecondaryWorkbenchButton(label: SecondaryWorkbenchLabel) {
  const supportStrip = await screen.findByRole('navigation', { name: '资产决策辅助入口' })
  return getSecondaryWorkbenchButton(supportStrip, label)
}

export async function openSecondaryWorkbench(label: SecondaryWorkbenchLabel) {
  const button = await findSecondaryWorkbenchButton(label)
  fireEvent.click(button)
  return button
}

export function expectAutomaticGroupDefaultCover(dialog: HTMLElement) {
  expectTabPanelRelationship(dialog, '决策组详情分区')
  expect(within(dialog).getByLabelText('决策组当前判断')).toBeInTheDocument()
  expectDecisionCoverDensity(within(dialog).getByLabelText('决策组当前判断'))
  expect(within(dialog).getByRole('button', { name: '创建组合' })).toBeInTheDocument()
  expect(within(dialog).getByRole('tab', { name: '概览' })).toBeInTheDocument()
  expect(within(dialog).getByRole('tab', { name: /成员/ })).toBeInTheDocument()
  expect(within(dialog).getByRole('tab', { name: '保存' })).toBeInTheDocument()
  expect(within(dialog).queryByRole('heading', { name: '当前判断' })).not.toBeInTheDocument()
  expect(within(dialog).queryByText(/预算压力集中在弱承载成员/)).not.toBeInTheDocument()
  expect(within(dialog).queryByText(/先创建自定义组合/)).not.toBeInTheDocument()
  expect(within(dialog).queryByText(/成员 \d+/)).not.toBeInTheDocument()
  expect(within(dialog).queryByText(/CNY/)).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '保存为决策记录' })).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('详情二级面板')).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '成员明细' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '保存记录面板' })).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('决策组详情目录')).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '成员取舍' })).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('关键成员摘要')).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('heading', { name: '关键成员摘要' })).not.toBeInTheDocument()
  expect(within(dialog).queryByText('Germany Primary')).not.toBeInTheDocument()
  expect(within(dialog).queryByText('Germany Standby')).not.toBeInTheDocument()
  expect(within(dialog).queryByText('主力候选：承载服务且监控证据可用')).not.toBeInTheDocument()
  expect(within(dialog).queryByText('补证据候选：缺订阅和监控关联后再判断是否备用')).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '处理' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '原始明细' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '数据底稿' })).not.toBeInTheDocument()
}


export function openAutomaticGroupMembers(dialog: HTMLElement) {
  fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
  return within(dialog).getByLabelText('成员取舍列表')
}

export function expectAutomaticGroupMembersPanelIsCompact(dialog: HTMLElement, memberDecisions: HTMLElement) {
  expect(within(dialog).getByRole('heading', { name: '成员取舍' })).toBeInTheDocument()
  expect(within(dialog).queryByText('MEMBER DECISIONS')).not.toBeInTheDocument()
  expect(within(dialog).queryByText('按当前判断排序，优先看角色、动作、成本承载和证据缺口。')).not.toBeInTheDocument()
  expect(within(dialog).queryByText('服务 2 · 域名 1 · Target 1 · 监控 1')).not.toBeInTheDocument()
  expect(within(dialog).queryByText('服务 0 · 域名 0 · Target 0 · 监控 0')).not.toBeInTheDocument()
  expect(within(memberDecisions).queryByText(/Hetzner ·/)).not.toBeInTheDocument()
  expect(within(memberDecisions).queryByText(/USD .*\/月/)).not.toBeInTheDocument()
  expect(within(memberDecisions).queryByText(/cx22|CPX31/i)).not.toBeInTheDocument()
  expect(within(dialog).queryByText('成本')).not.toBeInTheDocument()
  expect(within(dialog).queryByText('证据')).not.toBeInTheDocument()
  expect(within(dialog).queryByText('风险')).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('决策组成员对比')).not.toBeInTheDocument()
  expect(within(memberDecisions).getAllByText('Germany Primary').length).toBeGreaterThan(0)
  expect(within(memberDecisions).getAllByText('Germany Standby').length).toBeGreaterThan(0)
  expect(within(memberDecisions).getAllByText('主力候选').length).toBeGreaterThan(0)
  expect(within(memberDecisions).getAllByText('保留').length).toBeGreaterThan(0)
  expect(within(memberDecisions).getAllByText('补证据').length).toBeGreaterThan(0)
  expectMemberRowsUseSingleAction(memberDecisions)
}

export function expectAutomaticSavePanelIsBrief(dialog: HTMLElement) {
  expect(within(dialog).getByRole('heading', { name: '保存组合决策记录' })).toBeInTheDocument()
  expect(within(dialog).queryByText('SAVE DECISION')).not.toBeInTheDocument()
  expect(within(dialog).getByLabelText('标题')).toBeInTheDocument()
  expect(within(dialog).getByLabelText('状态')).toBeInTheDocument()
  expect(within(dialog).getByLabelText('组合目标')).toBeInTheDocument()
  expect(within(dialog).queryAllByLabelText('角色')).toHaveLength(0)
  expect(within(dialog).queryAllByLabelText('动作')).toHaveLength(0)
  expect(within(dialog).queryAllByLabelText('理由')).toHaveLength(0)
  expect(within(dialog).getByRole('button', { name: '编辑 Germany Primary 成员理由' })).toBeInTheDocument()
}


export function openManualGroupMembers(dialog: HTMLElement) {
  fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
  return within(dialog).getByLabelText('自定义组合成员取舍')
}

export function expectTemplateDefaultCover(dialog: HTMLElement) {
  expectTabPanelRelationship(dialog, '场景模板详情分区')
  const cover = within(dialog).getByLabelText('场景模板当前判断')
  expect(cover).toBeInTheDocument()
  expectDecisionCoverDensity(cover)
  expect(within(dialog).getByRole('tab', { name: '概览' })).toBeInTheDocument()
  expect(within(dialog).queryByLabelText('场景模板详情目录')).not.toBeInTheDocument()
  expect(within(dialog).getByRole('button', { name: '创建组合' })).toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '成员蓝图' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '状态维护' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('heading', { name: '从模板创建自定义组合' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('heading', { name: '成员蓝图' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '归档模板' })).not.toBeInTheDocument()
}


export function expectSavedRecordDefaultCover(dialog: HTMLElement) {
  expectTabPanelRelationship(dialog, '决策记录详情分区')
  const cover = within(dialog).getByLabelText('保存记录当前判断')
  expect(cover).toBeInTheDocument()
  expectDecisionCoverDensity(cover)
  expect(within(dialog).getByRole('tab', { name: '概览' })).toBeInTheDocument()
  expect(within(dialog).queryByLabelText('详情二级面板')).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('heading', { name: '保存时判断依据' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '执行跟进' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '成员跟进' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '来源复核' })).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('保存记录成员摘要')).not.toBeInTheDocument()
  expect(within(dialog).queryByText('快照成员 1')).not.toBeInTheDocument()
  expect(within(dialog).queryByText('主力保留')).not.toBeInTheDocument()
  expect(within(dialog).queryByText(/Germany Primary|Germany Standby|fra-legacy-cancel|ams-core-01|sjc-edge-02/)).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('Germany Primary 跟进状态')).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('决策记录成员')).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '原始成员' })).not.toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: '成员底稿' })).not.toBeInTheDocument()
}


export function expectMemberRowsUseSingleAction(container: HTMLElement) {
  container.querySelectorAll('.asset-decision-member-row__actions').forEach((actions) => {
    const interactiveCount = actions.querySelectorAll('button, a[href]').length
    expect(interactiveCount).toBeLessThanOrEqual(1)
  })
}

export function expectTaskPanelDensity(
  container: HTMLElement,
  options: {
    textMax: number
    interactiveMax: number
    inputsMax?: number
    memberRowsMax?: number
  },
) {
  const text = normalizedText(container)
  const interactiveCount = container.querySelectorAll('button, a[href]').length
  const inputCount = container.querySelectorAll('input, textarea, select').length
  const memberRows = container.querySelectorAll('.asset-decision-member-row, .asset-decision-record-followup-row, .asset-decision-execution-card, .asset-decision-save-member, .asset-decision-template-member').length
  expect(text.length).toBeLessThanOrEqual(options.textMax)
  expect(interactiveCount).toBeLessThanOrEqual(options.interactiveMax)
  expect(inputCount).toBeLessThanOrEqual(options.inputsMax ?? 0)
  if (options.memberRowsMax != null) {
    expect(memberRows).toBeLessThanOrEqual(options.memberRowsMax)
  }
}

export function expectNoDetailCoverWhileInTaskPanel(dialog: HTMLElement) {
  expect(within(dialog).queryByLabelText('决策组当前判断')).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('自定义组合当前判断')).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('场景模板当前判断')).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('保存记录当前判断')).not.toBeInTheDocument()
}

export function expectSavedRecordMembersPanelIsCompact(dialog: HTMLElement) {
  const panel = within(dialog).getByLabelText('成员跟进列表')
  expect(panel.querySelector('.asset-table-scroll')).toBeNull()
  expect(panel.querySelector('table')).toBeNull()
  expect(panel.querySelectorAll('.asset-decision-record-followup-row').length).toBeGreaterThan(0)
  expect(panel.querySelectorAll('input, textarea, select')).toHaveLength(0)
  expect(within(panel).queryByRole('columnheader', { name: 'VPS' })).not.toBeInTheDocument()
  expect(within(panel).queryByRole('columnheader', { name: '当前事实' })).not.toBeInTheDocument()
  expect(within(panel).queryByRole('columnheader', { name: '跟进' })).not.toBeInTheDocument()
  expect(panel).not.toHaveTextContent(/仍有 active 订阅|IP 203\.0\.113\.9|风险 provider 2|受阻 ChatGPT、Netflix|服务 2 · 域名 1/)
}

export function openSavedRecordRawMembersPanel(dialog: HTMLElement): boolean {
  const rawButton = within(dialog).queryByRole('button', { name: '查看成员底稿' })
  if (!rawButton) return false
  fireEvent.click(rawButton)
  expect(within(dialog).getByLabelText('决策记录成员')).toBeInTheDocument()
  return true
}

export function normalizedText(element: HTMLElement): string {
  return element.textContent?.replace(/\s+/g, ' ').trim() ?? ''
}

export function expectDecisionCoverDensity(cover: HTMLElement) {
  const text = normalizedText(cover)
  expect(text.length).toBeLessThanOrEqual(96)
  expect(cover).not.toHaveTextContent(/。.*。/)
  expect(cover.querySelector('.asset-table-scroll')).toBeNull()
  expect(cover.querySelector('.asset-decision-detail-directory')).toBeNull()
  expect(cover.querySelector('.asset-decision-detail-panel')).toBeNull()
  expect(cover.querySelector('.asset-decision-member-decisions')).toBeNull()
  expect(cover.querySelector('.asset-decision-member-row')).toBeNull()
  expect(cover.querySelector('.asset-decision-record-form')).toBeNull()
  expect(cover.querySelector('.asset-decision-execution-board')).toBeNull()
  expect(cover).not.toHaveTextContent(/GROUP DECISION|SCENARIO DECISION|SAVED EVIDENCE|EXECUTION PLAN|SOURCE CONTINUITY|BLUEPRINT|CREATE SCENARIO/)
  expect(cover).not.toHaveTextContent(/Germany Primary|Germany Standby|保存时判断依据|执行编排|来源复核|成员跟进|底稿/)
}

export function expectNoAssetDecisionPageEnglishNoise(container: HTMLElement = document.body) {
  expect(container).not.toHaveTextContent(/\b(?:PORTFOLIO|RENEWAL|CLOSED LOOP|EVIDENCE|WORKBENCH|SCENARIO|SCENARIOS|DECISION MEMORY|SINGLE VPS QUEUE|AUTO GROUP|AUX QUEUE)\b/)
}

export function countSourceLines(content: string): number {
  const normalized = content.replace(/\r?\n$/, '')
  return normalized ? normalized.split(/\r?\n/).length : 0
}

import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AssetDecisionsPage } from './AssetDecisionsPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

function LocationProbe() {
  const location = useLocation()
  return <output aria-label="current-url">{location.pathname}{location.search}</output>
}

function HistoryControls() {
  const navigate = useNavigate()
  return <>
    <button type="button" onClick={() => navigate(-1)}>返回上一条历史</button>
    <button type="button" onClick={() => navigate(1)}>前往下一条历史</button>
  </>
}

const sourceAvailability = {
  subscriptions: true,
  services: true,
  domains: true,
  monitoring: true,
  targets: true,
}

const subscription = {
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

const vps = {
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

const migrateVPS = {
  ...vps,
  vps_id: 'vps_migrate',
  display_name: 'Frankfurt Migration',
  country: 'DE',
  region: 'Hesse',
  city: 'Frankfurt',
  renewal_decision: 'migrate',
}

const cancelVPS = {
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

function evidenceAssessment(overrides: Record<string, unknown> = {}) {
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

function comparisonInsight(overrides: Record<string, unknown> = {}) {
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

function memberComparisonInsight(overrides: Record<string, unknown> = {}) {
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

function groupSummary(overrides: Record<string, unknown> = {}) {
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

function overview(overrides: Record<string, unknown> = {}) {
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

function groupDetail() {
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

function recordReadback(overrides: Record<string, unknown> = {}) {
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

function memberReadback(overrides: Record<string, unknown> = {}) {
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

function recordExecutionPlan(overrides: Record<string, unknown> = {}) {
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

function memberExecutionPlan(overrides: Record<string, unknown> = {}) {
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

function decisionRecord(overrides: Record<string, unknown> = {}) {
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

function manualGroupDetail(overrides: Record<string, unknown> = {}) {
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

function cloneGroupMember(index: number) {
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

function groupDetailWithManyMembers(count = 8) {
  const members = Array.from({ length: count }, (_, index) => cloneGroupMember(index + 1))
  return {
    ...groupDetail(),
    member_count: members.length,
    members,
  }
}

function manualGroupDetailWithManyMembers(count = 8) {
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

function cloneRecordMember(index: number) {
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

function decisionRecordWithManyMembers(count = 8) {
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

function manualGroupSummary(overrides: Record<string, unknown> = {}) {
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

function scenarioTemplate(overrides: Record<string, unknown> = {}) {
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

type InitialWorkbenchOptions = {
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

type MockFetchRoute = {
  url: string
  method?: string
  body?: unknown
  status?: number
  responses?: Array<{ body: unknown; status?: number }>
}

function initialWorkbenchResponse(url: string, options: InitialWorkbenchOptions = {}) {
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

function mockInitialWorkbench(fetchMock: ReturnType<typeof vi.fn>, options: InitialWorkbenchOptions = {}) {
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

function expectFetchCalledWith(fetchMock: ReturnType<typeof vi.fn>, url: string, init?: RequestInit) {
  if (init) {
    expect(fetchMock).toHaveBeenCalledWith(url, init)
    return
  }
  expect(fetchMock.mock.calls.some((call) => call[0] === url)).toBe(true)
}

function findFetchCall(fetchMock: ReturnType<typeof vi.fn>, url: string, method?: string) {
  return fetchMock.mock.calls.find((call) => {
    if (call[0] !== url) return false
    if (!method) return true
    return call[1]?.method === method
  })
}

function fetchRequestInventory(fetchMock: ReturnType<typeof vi.fn>, startIndex = 0): string[] {
  return fetchMock.mock.calls
    .slice(startIndex)
    .map((call) => `${call[1]?.method ?? 'GET'} ${call[0]}`)
    .sort()
}

type SecondaryWorkbenchLabel = '保存记录' | '场景与组合' | '续费窗口' | '单台队列'

function getSecondaryWorkbenchButton(supportStrip: HTMLElement, label: SecondaryWorkbenchLabel) {
  return within(supportStrip).getByRole('button', { name: label })
}

function expectTabPanelRelationship(container: HTMLElement, tablistName: string) {
  const tablist = within(container).getByRole('tablist', { name: tablistName })
  const activeTab = within(tablist).getByRole('tab', { selected: true })
  const panel = within(container).getByRole('tabpanel')
  expect(activeTab.id).not.toBe('')
  expect(panel.id).not.toBe('')
  expect(activeTab).toHaveAttribute('aria-controls', panel.id)
  expect(panel).toHaveAttribute('aria-labelledby', activeTab.id)
}

async function findSecondaryWorkbenchButton(label: SecondaryWorkbenchLabel) {
  const supportStrip = await screen.findByRole('navigation', { name: '资产决策辅助入口' })
  return getSecondaryWorkbenchButton(supportStrip, label)
}

async function openSecondaryWorkbench(label: SecondaryWorkbenchLabel) {
  const button = await findSecondaryWorkbenchButton(label)
  fireEvent.click(button)
  return button
}

function expectAutomaticGroupDefaultCover(dialog: HTMLElement) {
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


function openAutomaticGroupMembers(dialog: HTMLElement) {
  fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
  return within(dialog).getByLabelText('成员取舍列表')
}

function expectAutomaticGroupMembersPanelIsCompact(dialog: HTMLElement, memberDecisions: HTMLElement) {
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

function expectAutomaticSavePanelIsBrief(dialog: HTMLElement) {
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


function openManualGroupMembers(dialog: HTMLElement) {
  fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
  return within(dialog).getByLabelText('自定义组合成员取舍')
}

function expectTemplateDefaultCover(dialog: HTMLElement) {
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


function expectSavedRecordDefaultCover(dialog: HTMLElement) {
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


function expectMemberRowsUseSingleAction(container: HTMLElement) {
  container.querySelectorAll('.asset-decision-member-row__actions').forEach((actions) => {
    const interactiveCount = actions.querySelectorAll('button, a[href]').length
    expect(interactiveCount).toBeLessThanOrEqual(1)
  })
}

function expectTaskPanelDensity(
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

function expectNoDetailCoverWhileInTaskPanel(dialog: HTMLElement) {
  expect(within(dialog).queryByLabelText('决策组当前判断')).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('自定义组合当前判断')).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('场景模板当前判断')).not.toBeInTheDocument()
  expect(within(dialog).queryByLabelText('保存记录当前判断')).not.toBeInTheDocument()
}

function expectSavedRecordMembersPanelIsCompact(dialog: HTMLElement) {
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

function openSavedRecordRawMembersPanel(dialog: HTMLElement): boolean {
  const rawButton = within(dialog).queryByRole('button', { name: '查看成员底稿' })
  if (!rawButton) return false
  fireEvent.click(rawButton)
  expect(within(dialog).getByLabelText('决策记录成员')).toBeInTheDocument()
  return true
}

function normalizedText(element: HTMLElement): string {
  return element.textContent?.replace(/\s+/g, ' ').trim() ?? ''
}

function expectDecisionCoverDensity(cover: HTMLElement) {
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

function expectNoAssetDecisionPageEnglishNoise(container: HTMLElement = document.body) {
  expect(container).not.toHaveTextContent(/\b(?:PORTFOLIO|RENEWAL|CLOSED LOOP|EVIDENCE|WORKBENCH|SCENARIO|SCENARIOS|DECISION MEMORY|SINGLE VPS QUEUE|AUTO GROUP|AUX QUEUE)\b/)
}

function countSourceLines(content: string): number {
  const normalized = content.replace(/\r?\n$/, '')
  return normalized ? normalized.split(/\r?\n/).length : 0
}

describe('AssetDecisionsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the portfolio-first workbench without flattening secondary areas', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: '资产组合决策' })).toBeInTheDocument())
    const commandSummary = screen.getByLabelText('资产组合决策当前判断')
    expect(within(commandSummary).getByRole('heading', { name: '回读缺证据 · 德国主备取舍记录' })).toBeInTheDocument()
    expect(within(commandSummary).queryByText('全局资产组合 · 需要决策 · 30 天续费窗口')).not.toBeInTheDocument()
    expect(within(commandSummary).queryByText('30 天窗口 · 续费组 1 · 待决策 1')).not.toBeInTheDocument()
    expect(within(commandSummary).queryByText('证据源正常')).not.toBeInTheDocument()
    expect(within(commandSummary).queryByText(/VPS 跟进阻塞|当前记录仍有证据缺口|先补齐资料/)).not.toBeInTheDocument()
    expect(within(commandSummary).getByRole('button', { name: '补证据' })).toBeInTheDocument()
    expect(within(commandSummary).getByRole('link', { name: '资料缺口' })).toHaveAttribute('href', '/asset-decisions?view=evidence&renew_within_days=30&scenario=evidence_cleanup')
    expect(screen.queryByRole('heading', { name: '决策路径' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('资产组合决策推进路径')).not.toBeInTheDocument()
    expectNoAssetDecisionPageEnglishNoise()
    expect(screen.queryByText('PORTFOLIO')).not.toBeInTheDocument()
    const supportStrip = screen.getByRole('navigation', { name: '资产决策辅助入口' })
    expect(supportStrip).toHaveClass('asset-decision-support-strip')
    const secondaryButtons = within(supportStrip).getAllByRole('button')
    expect(secondaryButtons).toHaveLength(4)
    for (const label of ['保存记录', '场景与组合', '续费窗口', '单台队列'] as const) {
      const button = getSecondaryWorkbenchButton(supportStrip, label)
      expect(button).toBeInTheDocument()
      expect(button).toHaveAttribute('aria-pressed', 'false')
    }
    expect(normalizedText(supportStrip).length).toBeLessThanOrEqual(160)
    expect(within(supportStrip).queryByText(/回看判断与执行回读|管理比较篮子和启动模板|只读订阅窗口事实|保留单台续费处理/)).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '决策组扫描' })).toBeInTheDocument()
    expectTabPanelRelationship(document.body, '资产决策组合视图')
    const groupQueue = screen.getByLabelText('决策组扫描列表')
    expect(groupQueue).toBeInTheDocument()
    expect(screen.queryByText(/当前视图：/)).not.toBeInTheDocument()
    expect(screen.queryByText(/自动组只读派生/)).not.toBeInTheDocument()
    expect(screen.queryByText(/不会创建持久化决策记录/)).not.toBeInTheDocument()
    expect(screen.queryByText(/快照/)).not.toBeInTheDocument()
    expect(within(groupQueue).queryByLabelText('德国主力组合 关键证据')).not.toBeInTheDocument()
    expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0)
    expect(screen.queryByText('主力承载明确，备用仍需补齐订阅和监控证据')).not.toBeInTheDocument()
    expect(within(groupQueue).queryByText('NEXT STEP')).not.toBeInTheDocument()
    expect(within(groupQueue).queryByText('COMPARISON')).not.toBeInTheDocument()
    expect(within(groupQueue).queryByLabelText('证据评估刻度')).not.toBeInTheDocument()
    expect(within(groupQueue).queryByText('证据强')).not.toBeInTheDocument()
    expect(within(groupQueue).queryByText(/服务 2 · 域名 1 · Target 1\/1/)).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '自定义组合' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '已保存组合决策' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '场景模板' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '续费证据区' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '单台待处理队列' })).not.toBeInTheDocument()

    expectFetchCalledWith(fetchMock, '/api/asset-decisions/overview?view=needs_decision&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups?view=needs_decision&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/records?view=needs_decision&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/manual-groups?view=needs_decision&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/scenario-templates')
    expectFetchCalledWith(fetchMock, '/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc')
  })

  it('issues the exact eleven-request initial inventory once', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(11))
    expect(fetchRequestInventory(fetchMock)).toEqual([
      'GET /api/asset-decisions/groups?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/manual-groups?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/overview?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/records?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/scenario-templates',
      'GET /api/subscriptions?renew_within_days=30&sort=renew_at&order=asc',
      'GET /api/subscriptions?sort=renew_at&order=asc',
      'GET /api/vps',
      'GET /api/vps?renewal_decision=cancel',
      'GET /api/vps?renewal_decision=migrate',
      'GET /api/vps?renewal_decision=unreviewed',
    ])
  })

  it('shows a quiet stable state without promoting templates or manual groups', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      overviewBody: overview({
        group_count: 0,
        member_vps_count: 0,
        needs_decision_count: 0,
        renewal_group_count: 0,
        region_group_count: 0,
        provider_group_count: 0,
        cost_group_count: 0,
        evidence_group_count: 0,
        top_groups: [],
        type_counts: {},
        view_counts: {},
      }),
      groupsBody: [],
      recordsBody: [],
      manualGroupsBody: [manualGroupSummary({ title: '欧洲主备手工组合', status: 'active' })],
      templatesBody: [scenarioTemplate({ title: '主备取舍模板', status: 'active' })],
      renewalEvidenceBody: [],
      subscriptionsBody: [],
      unreviewedBody: [],
      migrateBody: [],
      cancelBody: [],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    const commandSummary = await screen.findByLabelText('资产组合决策当前判断')
    expect(within(commandSummary).getByRole('heading', { name: '当前没有需要处理的组合决策' })).toBeInTheDocument()
    expect(within(commandSummary).queryByRole('button', { name: /处理|使用模板|继续组合|打开决策组/ })).not.toBeInTheDocument()
    expect(within(commandSummary).queryByText(/主备取舍模板|欧洲主备手工组合/)).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '当前视图暂无决策组' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '场景工作区' })).not.toBeInTheDocument()
    expectNoAssetDecisionPageEnglishNoise()
  })

  it('keeps legacy single_queue URLs on the portfolio workbench and points to the support queue', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/asset-decisions?view=single_queue&renew_within_days=30']}>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: '决策组扫描' })).toBeInTheDocument())
    expect(screen.queryByRole('tab', { name: /单台队列/ })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '单台辅助队列' })).toBeInTheDocument()
    const singleQueueButton = await findSecondaryWorkbenchButton('单台队列')
    expect(singleQueueButton).toHaveAttribute('aria-pressed', 'true')
    const singleQueue = screen.getByRole('heading', { name: '单台辅助队列' }).closest('section') as HTMLElement
    fireEvent.click(within(singleQueue).getAllByRole('button', { name: '处理' })[0])
    expect(await screen.findByRole('dialog', { name: '续费决策处理' })).toBeInTheDocument()
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/overview?view=needs_decision&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups?view=needs_decision&renew_within_days=30')
  })

  it('auto-expands the matching secondary workbench for supported deep links', async () => {
    const cases: Array<{
      entry: string
      activeButton: '保存记录' | '场景与组合' | '续费窗口'
      visibleHeading: string
      expectedDialog?: string
      route?: MockFetchRoute
    }> = [
      {
        entry: '/asset-decisions?record_id=adr_001',
        activeButton: '保存记录',
        visibleHeading: '已保存组合决策',
        expectedDialog: '德国主备取舍记录',
        route: { url: '/api/asset-decisions/records/adr_001', body: decisionRecord() },
      },
      {
        entry: '/asset-decisions?view=renewal&renew_within_days=30&record_id=adr_001',
        activeButton: '保存记录',
        visibleHeading: '已保存组合决策',
        expectedDialog: '德国主备取舍记录',
        route: { url: '/api/asset-decisions/records/adr_001', body: decisionRecord() },
      },
      {
        entry: '/asset-decisions?manual_group_id=admg_001',
        activeButton: '场景与组合',
        visibleHeading: '场景工作区',
        expectedDialog: '自定义资产组合详情',
        route: { url: '/api/asset-decisions/manual-groups/admg_001', body: manualGroupDetail() },
      },
      {
        entry: '/asset-decisions?template_id=adt_builtin_primary_standby',
        activeButton: '场景与组合',
        visibleHeading: '场景工作区',
        expectedDialog: '资产决策场景模板详情',
        route: { url: '/api/asset-decisions/scenario-templates/adt_builtin_primary_standby', body: scenarioTemplate() },
      },
      {
        entry: '/asset-decisions?view=renewal&renew_within_days=30',
        activeButton: '续费窗口',
        visibleHeading: '续费事实',
      },
    ] as const

    for (const deepLink of cases) {
      const fetchMock = vi.fn()
      mockInitialWorkbench(fetchMock, deepLink.route ? { routes: [deepLink.route] } : undefined)
      vi.stubGlobal('fetch', fetchMock)

      const { unmount } = render(
        <MemoryRouter initialEntries={[deepLink.entry]}>
          <AssetDecisionsPage />
        </MemoryRouter>,
      )

      const supportStrip = await screen.findByRole('navigation', { name: '资产决策辅助入口' })
      await waitFor(() => expect(screen.getByRole('heading', { name: deepLink.visibleHeading })).toBeInTheDocument())
      const activeButton = getSecondaryWorkbenchButton(supportStrip, deepLink.activeButton)
      expect(activeButton).toHaveClass('asset-decision-support-strip__item--active')
      expect(activeButton).toHaveAttribute('aria-pressed', 'true')
      if (deepLink.expectedDialog) {
        expect(await screen.findByRole('dialog', { name: deepLink.expectedDialog })).toBeInTheDocument()
      }

      unmount()
      vi.unstubAllGlobals()
    }
  })

  it('lets support entries override renewal URL deep links after the first auto-open', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/asset-decisions?view=renewal&renew_within_days=30']}>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: '续费事实' })).toBeInTheDocument())
    fireEvent.click(await findSecondaryWorkbenchButton('保存记录'))
    await waitFor(() => expect(screen.getByRole('heading', { name: '已保存组合决策' })).toBeInTheDocument())
    expect(await findSecondaryWorkbenchButton('保存记录')).toHaveAttribute('aria-pressed', 'true')

    fireEvent.click(await findSecondaryWorkbenchButton('场景与组合'))
    await waitFor(() => expect(screen.getByRole('heading', { name: '场景工作区' })).toBeInTheDocument())
    expect(await findSecondaryWorkbenchButton('场景与组合')).toHaveAttribute('aria-pressed', 'true')
  })

  it('carries cross-page context filters into visible chips and asset-decision queries', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/asset-decisions?view=evidence&renew_within_days=30&provider_id=pv_001&vps_id=vps_review&scenario=evidence_cleanup']}>
        <AssetDecisionsPage />
        <LocationProbe />
      </MemoryRouter>,
    )

    const chips = await screen.findByLabelText('资产决策上下文筛选')
    expect(within(chips).getByText('服务商: pv_001')).toBeInTheDocument()
    expect(within(chips).getByText('VPS: vps_review')).toBeInTheDocument()
    expect(within(chips).getByText('场景: 资料清理')).toBeInTheDocument()
    expect(screen.getByLabelText('current-url')).toHaveTextContent('/asset-decisions?view=evidence&renew_within_days=30&provider_id=pv_001&vps_id=vps_review&scenario=evidence_cleanup')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/overview?view=evidence&renew_within_days=30&provider_id=pv_001&vps_id=vps_review&scenario=evidence_cleanup')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups?view=evidence&renew_within_days=30&provider_id=pv_001&vps_id=vps_review&scenario=evidence_cleanup')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/records?view=evidence&renew_within_days=30&provider_id=pv_001&vps_id=vps_review&scenario=evidence_cleanup')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/manual-groups?view=evidence&renew_within_days=30&provider_id=pv_001&vps_id=vps_review&scenario=evidence_cleanup')

    fireEvent.click(within(chips).getByRole('button', { name: '清除上下文' }))

    await waitFor(() => expect(screen.getByLabelText('current-url')).toHaveTextContent('/asset-decisions?view=evidence&renew_within_days=30'))
    expect(screen.queryByLabelText('资产决策上下文筛选')).not.toBeInTheDocument()
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/overview?view=evidence&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups?view=evidence&renew_within_days=30')
  })

  it('switches workbench tabs through asset-decision group queries', async () => {
    const regionGroup = groupSummary({
      group_id: 'adg_auto_region',
      group_type: 'region_portfolio',
      view: 'region',
      title: '日本同区取舍',
      scope_label: 'JP / Kanto / Tokyo',
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/overview?view=region&renew_within_days=30', body: overview({ region_group_count: 1 }) },
        { url: '/api/asset-decisions/groups?view=region&renew_within_days=30', body: [regionGroup] },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('tab', { name: /同区比较/ }))

    await waitFor(() => expect(screen.getAllByText('日本同区取舍').length).toBeGreaterThan(0))
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/overview?view=region&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups?view=region&renew_within_days=30')
  })

  it('opens a readback next-work record and preserves URL context filters', async () => {
    const driftRecord = decisionRecord({
      execution_readback: recordReadback({
        status: 'drift',
        summary: '1 台 VPS 跟进完成但当前事实仍未闭环',
        drift_count: 1,
        needs_evidence_count: 0,
        aligned_count: 0,
      }),
      execution_plan: recordExecutionPlan({
        summary: '1 台 VPS 事实漂移，优先复核闭环',
        lane_counts: [{ lane: 'cancel_retire', count: 1 }],
        actionable_count: 1,
      }),
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      recordsBody: [driftRecord],
      routes: [
        { url: '/api/asset-decisions/records/adr_001', body: driftRecord },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/asset-decisions?provider_id=pv_001']}>
        <AssetDecisionsPage />
        <LocationProbe />
      </MemoryRouter>,
    )

    const commandSummary = await screen.findByLabelText('资产组合决策当前判断')
    expect(within(commandSummary).getByRole('heading', { name: /事实漂移/ })).toBeInTheDocument()
    // 闭环异常存在时，风险标签必须随异常数字一起显示，说明具体异常类型
    const anomalyItem = within(commandSummary).getByText('闭环异常').closest('.asset-decision-focus__item')
    expect(anomalyItem).not.toBeNull()
    expect(within(anomalyItem as HTMLElement).getByText(/事实漂移/)).toBeInTheDocument()
    fireEvent.click(within(commandSummary).getByRole('button', { name: '复核记录' }))

    expect(await screen.findByRole('dialog', { name: '德国主备取舍记录' })).toBeInTheDocument()
    expect(screen.getByLabelText('current-url')).toHaveTextContent('/asset-decisions?provider_id=pv_001&record_id=adr_001')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/records/adr_001')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/records?view=needs_decision&renew_within_days=30&provider_id=pv_001')
  })

  it('opens an automatic group from next-work without writing business assets', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      recordsBody: [],
      routes: [
        { url: '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30', body: groupDetail() },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/asset-decisions']}>
        <AssetDecisionsPage />
        <LocationProbe />
      </MemoryRouter>,
    )

    const commandSummary = await screen.findByLabelText('资产组合决策当前判断')
    await waitFor(() => expect(within(commandSummary).getByText('自动组合')).toBeInTheDocument())
    fireEvent.click(within(commandSummary).getByRole('button', { name: '打开决策组' }))

    const dialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    expectAutomaticGroupDefaultCover(dialog)
    expect(screen.getByLabelText('current-url')).toHaveTextContent('/asset-decisions?group_id=adg_auto_001')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30')
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/vps/') && call[1]?.method === 'PATCH')).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/subscriptions/') && call[1]?.method)).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/monitoring-instances/') && call[1]?.method)).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/targets/') && call[1]?.method)).toBe(false)
  })

  it('closes a nested renewal draft when browser history leaves its group', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30', body: groupDetail() },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter
        initialEntries={['/asset-decisions', '/asset-decisions?group_id=adg_auto_001']}
        initialIndex={1}
      >
        <AssetDecisionsPage />
        <LocationProbe />
        <HistoryControls />
      </MemoryRouter>,
    )

    const dialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    const members = openAutomaticGroupMembers(dialog)
    fireEvent.click(within(members).getAllByRole('button', { name: '处理' })[0])
    expect(within(dialog).getByLabelText('续费决策')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '返回上一条历史' }))

    await waitFor(() => expect(screen.getByLabelText('current-url')).toHaveTextContent('/asset-decisions'))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '资产决策组详情' })).not.toBeInTheDocument())
    expect(screen.queryByRole('dialog', { name: '续费决策处理' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '前往下一条历史' }))
    const reopenedDialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    expect(within(reopenedDialog).getByRole('tab', { name: '概览' })).toHaveAttribute('aria-selected', 'true')
    expect(within(reopenedDialog).queryByLabelText('续费决策')).not.toBeInTheDocument()
  })

  it('keeps loaded auto groups available when overview fails', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      recordsBody: [],
      manualGroupsBody: [],
      templatesBody: [],
      routes: [
        {
          url: '/api/asset-decisions/overview?view=needs_decision&renew_within_days=30',
          body: { error: 'overview unavailable' },
          status: 500,
        },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0))
    const commandSummary = screen.getByLabelText('资产组合决策当前判断')
    expect(within(commandSummary).getByRole('button', { name: '打开决策组' })).toBeInTheDocument()
    expect(screen.getByText('组合概览暂不可用，当前只展示已成功加载的事实。')).toBeInTheDocument()
    expect(screen.getByText('组合概览不可用')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '决策组不可用' })).not.toBeInTheDocument()
  })

  it('keeps the loaded overview available when decision groups fail', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        {
          url: '/api/asset-decisions/groups?view=needs_decision&renew_within_days=30',
          body: { error: 'groups unavailable' },
          status: 500,
        },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { name: '决策组不可用' })).toBeInTheDocument()
    const currentFacts = screen.getByLabelText('资产组合决策当前事实')
    expect(within(currentFacts).getByText('组合组数')).toBeInTheDocument()
    expect(within(currentFacts).getByText('3')).toBeInTheDocument()
    expect(screen.getByText('自动组暂不可用，当前只展示已成功加载的事实。')).toBeInTheDocument()
  })

  it('does not invent readback next-work items when saved records fail to load', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      overviewBody: overview({
        group_count: 0,
        needs_decision_count: 0,
        renewal_group_count: 0,
        evidence_group_count: 0,
        cost_group_count: 0,
      }),
      groupsBody: [],
      manualGroupsBody: [],
      templatesBody: [],
      routes: [
        {
          url: '/api/asset-decisions/records?view=needs_decision&renew_within_days=30',
          body: { error: 'records unavailable' },
          status: 500,
        },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    const commandSummary = await screen.findByLabelText('资产组合决策当前判断')
    expect(within(commandSummary).queryByText('事实漂移')).not.toBeInTheDocument()
    expect(within(commandSummary).queryByText('跟进阻塞')).not.toBeInTheDocument()
    expect(within(commandSummary).queryByText('回读缺证据')).not.toBeInTheDocument()
    expect(screen.getByText('决策记录暂不可用，当前只展示已成功加载的事实。')).toBeInTheDocument()
    expect(within(commandSummary).getByRole('heading', { name: '部分资产决策证据不可用' })).toBeInTheDocument()
    expect(within(commandSummary).getByText('证据待确认')).toBeInTheDocument()
    expect(within(commandSummary).queryByText('闭环稳定')).not.toBeInTheDocument()
  })

  it('opens group detail with member comparison, evidence, and single VPS entry', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30', body: groupDetail() },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '查看组' }))

    const dialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    expect(within(dialog).queryByRole('heading', { name: '场景推进建议' })).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('heading', { name: '证据矩阵 / 取舍对比' })).not.toBeInTheDocument()
    expect(within(dialog).queryByText('GROUP DECISION')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('MEMBERS')).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('证据评估刻度')).not.toBeInTheDocument()
    expect(within(dialog).queryByText(/支撑 \d+ · 风险 \d+ · 缺口 \d+/)).not.toBeInTheDocument()
    expect(within(dialog).queryByText('判断与关键成员')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('取舍卡片')).not.toBeInTheDocument()
    const command = within(dialog).getByLabelText('决策组当前判断')
    expect(within(command).queryByText('主力承载明确，备用仍需补齐订阅和监控证据')).not.toBeInTheDocument()
    expect(within(command).getByRole('button', { name: '创建组合' })).toBeInTheDocument()
    expect(within(dialog).queryByLabelText('决策组成员对比')).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('续费决策')).not.toBeInTheDocument()
    expect(within(dialog).queryByText(/原始明细|继续查看原始明细/)).not.toBeInTheDocument()
    expectAutomaticGroupDefaultCover(dialog)

    // Tab navigation replaces detail directory
    fireEvent.click(within(dialog).getByRole('tab', { name: '保存' }))
    expect(within(dialog).getByRole('heading', { name: '保存组合决策记录' })).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))

    // Tab navigation replaces detail directory
    const memberDecisions = openAutomaticGroupMembers(dialog)
    expect(within(dialog).getByRole('heading', { name: '成员取舍' })).toBeInTheDocument()
    expect(within(memberDecisions).getByText('主力候选：承载服务且监控证据可用')).toBeInTheDocument()
    expect(within(memberDecisions).getByText(/补证据候选：缺订阅和监控关联后再判断/)).toBeInTheDocument()
    expectAutomaticGroupMembersPanelIsCompact(dialog, memberDecisions)
    expectNoDetailCoverWhileInTaskPanel(dialog)
    expectTaskPanelDensity(dialog, { textMax: 180, interactiveMax: 7, memberRowsMax: 3 })
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30')

    fireEvent.click(within(dialog).getAllByRole('button', { name: '处理' })[0])
    expect(within(dialog).getAllByText('Germany Primary').length).toBeGreaterThan(0)
    expect(within(dialog).getByLabelText('续费决策')).toHaveValue('keep')
  })

  it('applies the same decision-cover default to cost pressure groups', async () => {
    const costSummary = groupSummary({
      group_id: 'adg_cost_001',
      group_type: 'cost_pressure',
      view: 'cost',
      title: '预算压力与弱承载',
      scope_label: '月度成本异常',
      primary_issue_summary: '成本偏高且成员承载薄弱，默认层不应展开成员报告。',
      evidence_chips: [
        { kind: 'budget_risk', label: '预算压力', tone: 'alert' },
        { kind: 'no_service_context', label: '弱承载', tone: 'notice' },
      ],
      comparison_insight: comparisonInsight({
        summary: '预算压力集中在弱承载成员，先创建自定义组合再确认削减路径',
        primary_axis: 'cost',
      }),
    })
    const costDetail = {
      ...groupDetail(),
      ...costSummary,
      members: groupDetail().members,
    }
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      groupsBody: [costSummary],
      routes: [
        { url: '/api/asset-decisions/groups/adg_cost_001?renew_within_days=30', body: costDetail },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('预算压力与弱承载').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '查看组' }))

    const dialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    expectAutomaticGroupDefaultCover(dialog)

    // Tab navigation replaces detail directory
    const memberDecisions = openAutomaticGroupMembers(dialog)
    expectAutomaticGroupMembersPanelIsCompact(dialog, memberDecisions)
    expectNoDetailCoverWhileInTaskPanel(dialog)
    expectTaskPanelDensity(dialog, { textMax: 180, interactiveMax: 7, memberRowsMax: 3 })
    expect(within(dialog).getAllByRole('button', { name: '处理' }).length).toBeGreaterThan(0)
  })

  it('caps automatic group member and save panels to preview rows for large groups', async () => {
    const largeDetail = groupDetailWithManyMembers(8)
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      groupsBody: [groupSummary({ member_count: largeDetail.members.length })],
      routes: [
        { url: '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30', body: largeDetail },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '查看组' }))

    const dialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    const memberDecisions = openAutomaticGroupMembers(dialog)
    expect(memberDecisions.querySelectorAll('.asset-decision-member-row')).toHaveLength(3)
    expect(within(memberDecisions).getByText('Bulk Member 1')).toBeInTheDocument()
    expect(within(memberDecisions).getByText('Bulk Member 2')).toBeInTheDocument()
    expect(within(memberDecisions).getByText('Bulk Member 3')).toBeInTheDocument()
    expect(within(memberDecisions).queryByText('Bulk Member 8')).not.toBeInTheDocument()
    expect(within(memberDecisions).getByText('另有 5 台在底稿中查看')).toBeInTheDocument()
    fireEvent.click(within(memberDecisions).getByRole('button', { name: '查看数据底稿' }))
    expect(within(dialog).getByLabelText('决策组成员对比')).toBeInTheDocument()
    expect(within(dialog).getByText('Bulk Member 8')).toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('tab', { name: '保存' }))
    const saveMembers = within(dialog).getByLabelText('保存记录成员复核')
    expect(saveMembers.querySelectorAll('.asset-decision-save-member')).toHaveLength(3)
    expect(within(saveMembers).getByRole('button', { name: '编辑 Bulk Member 1 成员理由' })).toBeInTheDocument()
    expect(within(saveMembers).queryByRole('button', { name: '编辑 Bulk Member 8 成员理由' })).not.toBeInTheDocument()
    expect(within(saveMembers).getByText('另有 5 台成员保留在保存底稿中')).toBeInTheDocument()
  })

  it('caps manual group member and save panels to preview rows for large groups', async () => {
    const largeManual = manualGroupDetailWithManyMembers(8)
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      manualGroupsBody: [manualGroupSummary({ member_count: largeManual.members.length })],
      routes: [
        { url: '/api/asset-decisions/manual-groups/admg_001', body: largeManual },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('场景与组合')
    await waitFor(() => expect(screen.getAllByText('德国主备自定义组合').length).toBeGreaterThan(0))
    const manualSection = screen.getByRole('heading', { name: '自定义组合' }).closest('section')
    fireEvent.click(within(manualSection!).getByText("德国主备自定义组合"))

    const dialog = await screen.findByRole('dialog', { name: '自定义资产组合详情' })
    const memberDecisions = openManualGroupMembers(dialog)
    expect(memberDecisions.querySelectorAll('.asset-decision-member-row')).toHaveLength(3)
    expect(within(memberDecisions).getByText('Bulk Member 1')).toBeInTheDocument()
    expect(within(memberDecisions).queryByText('Bulk Member 8')).not.toBeInTheDocument()
    expect(within(memberDecisions).getByText('另有 5 台在底稿中查看')).toBeInTheDocument()
    fireEvent.click(within(memberDecisions).getByRole('button', { name: '查看成员数据' }))
    expect(within(dialog).getByLabelText('自定义组合成员对比')).toBeInTheDocument()
    expect(within(dialog).getByText('Bulk Member 8')).toBeInTheDocument()
    const rawMemberRow = within(dialog).getByText('Bulk Member 8').closest('tr') as HTMLElement
    fireEvent.click(within(rawMemberRow).getByRole('button', { name: '移除' }))
    const removalConfirmation = await screen.findByRole('alertdialog', { name: '确认移除组合成员' })
    expect(within(dialog).queryByRole('alertdialog', { name: '确认移除组合成员' })).not.toBeInTheDocument()
    fireEvent.click(within(removalConfirmation).getByRole('button', { name: '取消' }))

    fireEvent.click(within(dialog).getByRole('tab', { name: '概览' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '保存记录' }))
    const saveMembers = within(dialog).getByLabelText('保存记录成员复核')
    expect(saveMembers.querySelectorAll('.asset-decision-save-member')).toHaveLength(3)
    expect(within(saveMembers).getByRole('button', { name: '编辑 Bulk Member 1 成员理由' })).toBeInTheDocument()
    expect(within(saveMembers).queryByRole('button', { name: '编辑 Bulk Member 8 成员理由' })).not.toBeInTheDocument()
    expect(within(saveMembers).getByText('另有 5 台成员保留在保存底稿中')).toBeInTheDocument()
  })

  it('keeps an automatic record draft aligned with group member changes before saving', async () => {
    const changedDetail = {
      ...groupDetail(),
      member_count: 1,
      members: [groupDetail().members[1]],
    }
    const updatedPrimary = {
      ...groupDetail().members[0].vps,
      renewal_decision: 'observe',
    }
    const created = decisionRecord({
      record_id: 'adr_auto_changed',
      title: '德国主力组合',
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        {
          url: '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30',
          responses: [
            { body: groupDetail() },
            { body: changedDetail },
          ],
        },
        { url: '/api/vps/vps_primary', method: 'PATCH', body: updatedPrimary },
        { url: '/api/asset-decisions/records', method: 'POST', body: created, status: 201 },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '查看组' }))
    const dialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    fireEvent.click(within(dialog).getByRole('tab', { name: '保存' }))
    fireEvent.change(within(dialog).getByLabelText('组合目标'), { target: { value: '保留备用观察' } })
    fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
    fireEvent.click(within(dialog).getAllByRole('button', { name: '处理' })[0])
    fireEvent.change(within(dialog).getByLabelText('续费决策'), { target: { value: 'observe' } })
    const renewalMutationStart = fetchMock.mock.calls.length
    fireEvent.click(within(dialog).getByRole('button', { name: '保存续费决策' }))

    await waitFor(() => expect(screen.getByText(/续费决策已保存：Germany Primary ->/)).toBeInTheDocument())
    await waitFor(() => expect(within(dialog).queryByText('Germany Primary')).not.toBeInTheDocument())
    await waitFor(() => expect(fetchMock.mock.calls.length).toBe(renewalMutationStart + 13))
    expect(fetchRequestInventory(fetchMock, renewalMutationStart)).toEqual([
      'GET /api/asset-decisions/groups/adg_auto_001?renew_within_days=30',
      'GET /api/asset-decisions/groups?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/manual-groups?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/overview?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/records?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/scenario-templates',
      'GET /api/subscriptions?renew_within_days=30&sort=renew_at&order=asc',
      'GET /api/subscriptions?sort=renew_at&order=asc',
      'GET /api/vps',
      'GET /api/vps?renewal_decision=cancel',
      'GET /api/vps?renewal_decision=migrate',
      'GET /api/vps?renewal_decision=unreviewed',
      'PATCH /api/vps/vps_primary',
    ])
    fireEvent.click(within(dialog).getByRole('tab', { name: '保存' }))
    expect(within(dialog).queryByRole('button', { name: '编辑 Germany Primary 成员理由' })).not.toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '编辑 Germany Standby 成员理由' })).toBeInTheDocument()
    expect(within(dialog).getByDisplayValue('保留备用观察')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '保存记录' }))

    await waitFor(() => expect(screen.getByText('已保存组合决策记录：德国主力组合')).toBeInTheDocument())
    const recordCall = findFetchCall(fetchMock, '/api/asset-decisions/records', 'POST')
    expect(recordCall?.[1]?.body).toBe(JSON.stringify({
      source_type: 'auto_group',
      source_group_id: 'adg_auto_001',
      renew_within_days: 30,
      title: '德国主力组合',
      goal: '保留备用观察',
      status: 'draft',
      members: [
        {
          vps_id: 'vps_standby',
          decided_role: 'evidence_needed',
          decided_action: 'complete_evidence',
          reason: '',
        },
      ],
    }))
  })

  it('saves a decision group as a persistent decision record', async () => {
    const created = decisionRecord({
      record_id: 'adr_created',
      title: '德国主备取舍',
      status: 'decided',
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30', body: groupDetail() },
        { url: '/api/asset-decisions/records', method: 'POST', body: created, status: 201 },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '查看组' }))
    const dialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    // Tab navigation replaces detail directory
    fireEvent.click(within(dialog).getByRole('tab', { name: '保存' }))
    expectAutomaticSavePanelIsBrief(dialog)
    fireEvent.change(within(dialog).getByLabelText('标题'), { target: { value: '德国主备取舍' } })
    fireEvent.change(within(dialog).getByLabelText('状态'), { target: { value: 'decided' } })
    fireEvent.change(within(dialog).getByLabelText('组合目标'), { target: { value: '保留主力，补齐备用证据' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存记录' }))

    await waitFor(() => expect(screen.getByText('已保存组合决策记录：德国主备取舍')).toBeInTheDocument())
    expect(await screen.findByRole('dialog', { name: '德国主备取舍' })).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '资产决策组详情' })).not.toBeInTheDocument()
    expect(screen.queryAllByRole('dialog')).toHaveLength(1)
    expect(findFetchCall(fetchMock, '/api/asset-decisions/records', 'POST')).toEqual([
      '/api/asset-decisions/records',
      {
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({
          source_type: 'auto_group',
          source_group_id: 'adg_auto_001',
          renew_within_days: 30,
          title: '德国主备取舍',
          goal: '保留主力，补齐备用证据',
          status: 'decided',
          members: [
            {
              vps_id: 'vps_primary',
              decided_role: 'primary_candidate',
              decided_action: 'keep',
              reason: '',
            },
            {
              vps_id: 'vps_standby',
              decided_role: 'evidence_needed',
              decided_action: 'complete_evidence',
              reason: '',
            },
          ],
        }),
      },
    ])
  })

  it('creates a manual scenario group from an automatic group and opens it', async () => {
    const createdManual = manualGroupDetail({
      manual_group_id: 'admg_created',
      title: '德国主力组合',
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30', body: groupDetail() },
        { url: '/api/asset-decisions/manual-groups', method: 'POST', body: createdManual, status: 201 },
        { url: '/api/asset-decisions/manual-groups/admg_created', body: createdManual },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '查看组' }))
    const dialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(16))
    const mutationStart = fetchMock.mock.calls.length
    fireEvent.click(within(dialog).getByRole('button', { name: '创建组合' }))

    await waitFor(() => expect(screen.getByText('已创建自定义组合：德国主力组合')).toBeInTheDocument())
    const manualDialog = await screen.findByRole('dialog', { name: '自定义资产组合详情' })
    await waitFor(() => expect(fetchMock.mock.calls.length).toBe(mutationStart + 6))
    expect(fetchRequestInventory(fetchMock, mutationStart)).toEqual([
      'GET /api/asset-decisions/groups?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/manual-groups/admg_created',
      'GET /api/asset-decisions/manual-groups?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/overview?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/records?view=needs_decision&renew_within_days=30',
      'POST /api/asset-decisions/manual-groups',
    ])
    expectTabPanelRelationship(manualDialog, '自定义组合详情分区')
    expect(screen.queryByRole('dialog', { name: '资产决策组详情' })).not.toBeInTheDocument()
    expect(screen.queryAllByRole('dialog')).toHaveLength(1)
    expect(within(manualDialog).queryByRole('heading', { name: '组合推进状态' })).not.toBeInTheDocument()
    expect(within(manualDialog).queryByRole('heading', { name: '自定义组合证据矩阵' })).not.toBeInTheDocument()
    expect(within(manualDialog).queryByText('SCENARIO DECISION')).not.toBeInTheDocument()
    expect(within(manualDialog).queryByLabelText('证据评估刻度')).not.toBeInTheDocument()
    expect(within(manualDialog).queryByText('判断与意图')).not.toBeInTheDocument()
    const manualCommand = within(manualDialog).getByLabelText('自定义组合当前判断')
    expect(within(manualCommand).getAllByText('自定义组合中主力证据清晰，可保存记录').length).toBeGreaterThan(0)
    expect(within(manualCommand).getAllByText(/可保存记录 5\/5|接近可保存|继续整理/).length).toBeGreaterThan(0)
    expect(within(manualDialog).queryByLabelText('自定义组合成员取舍')).not.toBeInTheDocument()
    expect(within(manualDialog).queryByRole('heading', { name: '组合场景' })).not.toBeInTheDocument()
    expect(within(manualDialog).queryByLabelText('自定义组合成员摘要')).not.toBeInTheDocument()
    expect(within(manualDialog).queryByText('Germany Primary')).not.toBeInTheDocument()
    expect(within(manualDialog).queryByText('意图匹配')).not.toBeInTheDocument()
    expect(within(manualDialog).queryByRole('button', { name: '另存为模板' })).not.toBeInTheDocument()
    expect(within(manualDialog).queryByRole('button', { name: '添加成员' })).not.toBeInTheDocument()
    expect(within(manualDialog).queryByRole('button', { name: '原始明细' })).not.toBeInTheDocument()
    expect(within(manualDialog).queryByRole('button', { name: '编辑组合' })).not.toBeInTheDocument()
    expect(within(manualDialog).queryByRole('button', { name: '保存为决策记录' })).not.toBeInTheDocument()
    expect(within(manualDialog).getByRole('tab', { name: '概览' })).toBeInTheDocument()
    const manualMembers = openManualGroupMembers(manualDialog)
    expect(within(manualDialog).queryByText('人工意图和当前证据并排呈现。')).not.toBeInTheDocument()
    expect(within(manualMembers).getByText('意图匹配')).toBeInTheDocument()
    expect(within(manualMembers).getByText('Germany Primary')).toBeInTheDocument()
    expect(within(manualMembers).getByText('主力候选：承载服务且监控证据可用')).toBeInTheDocument()
    fireEvent.click(within(manualDialog).getByRole('tab', { name: /编辑/ }))
    expect(within(manualDialog).getByRole('heading', { name: '组合场景' })).toBeInTheDocument()
    expect(within(manualDialog).getByDisplayValue('德国主力组合')).toBeInTheDocument()
    expect(within(manualDialog).getByDisplayValue('保留主力，观察备用')).toBeInTheDocument()
    expect(within(manualDialog).getByDisplayValue('从自动组创建')).toBeInTheDocument()
    expect(findFetchCall(fetchMock, '/api/asset-decisions/manual-groups', 'POST')).toEqual([
      '/api/asset-decisions/manual-groups',
      {
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({
          source_type: 'auto_group',
          source_group_id: 'adg_auto_001',
          renew_within_days: 30,
          scenario: 'general',
          title: '德国主力组合',
          goal: '',
          note: '由自动组 adg_auto_001 创建',
        }),
      },
    ])
  })

  it('keeps the scenarios workbench visible after closing a manual group created from an automatic group', async () => {
    const createdManual = manualGroupDetail({
      manual_group_id: 'admg_created',
      title: '德国主力组合',
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30', body: groupDetail() },
        { url: '/api/asset-decisions/manual-groups', method: 'POST', body: createdManual, status: 201 },
        { url: '/api/asset-decisions/manual-groups/admg_created', body: createdManual },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: '查看组' }))
    const groupDialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    fireEvent.click(within(groupDialog).getByRole('button', { name: '创建组合' }))

    await waitFor(() => expect(screen.getByText('已创建自定义组合：德国主力组合')).toBeInTheDocument())
    const manualDialog = await screen.findByRole('dialog', { name: '自定义资产组合详情' })
    expect(screen.getByRole('heading', { name: '场景工作区' })).toBeInTheDocument()
    fireEvent.click(within(manualDialog).getByRole('button', { name: '关闭' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '自定义资产组合详情' })).not.toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '场景工作区' })).toBeInTheDocument()
    const manualSection = screen.getByRole('heading', { name: '自定义组合' }).closest('section')
    expect(within(manualSection!).getByText('德国主力组合')).toBeInTheDocument()
  })

  it('keeps create-combo action usable from automatic group task panels', async () => {
    const cases: Array<{
      name: string
      openPanel: (dialog: HTMLElement) => void
      assertPanel: (dialog: HTMLElement) => void
    }> = [
      {
        name: 'members',
        openPanel: (dialog) => {
          fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
        },
        assertPanel: (dialog) => {
          expect(within(dialog).getByRole('heading', { name: '成员取舍' })).toBeInTheDocument()
        },
      },
      {
        name: 'save',
        openPanel: (dialog) => {
          fireEvent.click(within(dialog).getByRole('tab', { name: '保存' }))
        },
        assertPanel: (dialog) => {
          expect(within(dialog).getByRole('heading', { name: '保存组合决策记录' })).toBeInTheDocument()
        },
      },
      {
        name: 'vps',
        openPanel: (dialog) => {
          fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
          fireEvent.click(within(dialog).getAllByRole('button', { name: '处理' })[0])
        },
        assertPanel: (dialog) => {
          expect(within(dialog).getByLabelText('续费决策')).toBeInTheDocument()
        },
      },
    ]

    for (const taskPanel of cases) {
      const createdManual = manualGroupDetail({
        manual_group_id: `admg_created_${taskPanel.name}`,
        title: `德国主力组合 ${taskPanel.name}`,
      })
      const fetchMock = vi.fn()
      mockInitialWorkbench(fetchMock, {
        routes: [
          { url: '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30', body: groupDetail() },
          { url: '/api/asset-decisions/manual-groups', method: 'POST', body: createdManual, status: 201 },
          { url: `/api/asset-decisions/manual-groups/admg_created_${taskPanel.name}`, body: createdManual },
        ],
      })
      vi.stubGlobal('fetch', fetchMock)

      const { unmount } = render(
        <MemoryRouter>
          <AssetDecisionsPage />
        </MemoryRouter>,
      )

      await waitFor(() => expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0))
      fireEvent.click(screen.getByRole('button', { name: '查看组' }))
      const dialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
      taskPanel.openPanel(dialog)

      taskPanel.assertPanel(dialog)
      fireEvent.click(within(dialog).getByRole('tab', { name: '概览' }))
      fireEvent.click(within(dialog).getByRole('button', { name: '创建组合' }))

      await waitFor(() => expect(screen.getByText(`已创建自定义组合：德国主力组合 ${taskPanel.name}`)).toBeInTheDocument())
      expect(findFetchCall(fetchMock, '/api/asset-decisions/manual-groups', 'POST')).toBeDefined()
      expect(await screen.findByRole('dialog', { name: '自定义资产组合详情' })).toBeInTheDocument()
      expect(screen.queryByRole('dialog', { name: '资产决策组详情' })).not.toBeInTheDocument()
      expect(screen.queryAllByRole('dialog')).toHaveLength(1)
      expect(screen.getByRole('heading', { name: '场景工作区' })).toBeInTheDocument()

      unmount()
      vi.unstubAllGlobals()
    }
  })

  it('keeps a manual record draft aligned with member changes before saving', async () => {
    const addedManual = manualGroupDetail({
      member_count: 2,
      members: [
        ...manualGroupDetail().members,
        {
          ...manualGroupDetail().members[0],
          manual_group_id: 'admg_001',
          vps_id: 'vps_standby',
          vps: {
            ...manualGroupDetail().members[0].vps,
            vps_id: 'vps_standby',
            display_name: 'Germany Standby',
          },
          intended_role: 'observe_candidate',
          intended_action: 'review',
          reason: '新增备用观察',
          note: '',
          sort_order: 20,
          evidence_snapshot: { vps_id: 'vps_standby', service_count: 0 },
          current_fact_found: true,
        },
      ],
    })
    const created = decisionRecord({
      record_id: 'adr_manual_added',
      title: '德国主备自定义组合',
      source_type: 'manual_group',
      source_group_id: 'admg_001',
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/manual-groups/admg_001', body: manualGroupDetail() },
        { url: '/api/asset-decisions/manual-groups/admg_001/members', method: 'POST', body: addedManual, status: 201 },
        { url: '/api/asset-decisions/records', method: 'POST', body: created, status: 201 },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('场景与组合')
    await waitFor(() => expect(screen.getAllByText('德国主备自定义组合').length).toBeGreaterThan(0))
    const manualSection = screen.getByRole('heading', { name: '自定义组合' }).closest('section')
    fireEvent.click(within(manualSection!).getByText('德国主备自定义组合'))

    const dialog = await screen.findByRole('dialog', { name: '自定义资产组合详情' })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存记录' }))
    fireEvent.change(within(dialog).getByLabelText('组合目标'), { target: { value: '保留主力并观察备用' } })
    fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
    fireEvent.click(within(dialog).getByRole('button', { name: '添加成员' }))
    fireEvent.change(within(dialog).getByLabelText('VPS'), { target: { value: 'vps_standby' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '高级选项' }))
    fireEvent.change(within(dialog).getByLabelText('理由'), { target: { value: '新增备用观察' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '加入组合' }))

    await waitFor(() => expect(screen.getByText('自定义组合成员已加入')).toBeInTheDocument())
    fireEvent.click(within(dialog).getByRole('tab', { name: '概览' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '保存记录' }))
    expect(within(dialog).getByDisplayValue('保留主力并观察备用')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '编辑 Germany Standby 成员理由' })).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '保存记录' }))

    await waitFor(() => expect(screen.getByText('已保存组合决策记录：德国主备自定义组合')).toBeInTheDocument())
    const recordCall = findFetchCall(fetchMock, '/api/asset-decisions/records', 'POST')
    expect(recordCall?.[1]?.body).toBe(JSON.stringify({
      source_type: 'manual_group',
      source_group_id: 'admg_001',
      renew_within_days: 30,
      title: '德国主备自定义组合',
      goal: '保留主力并观察备用',
      status: 'draft',
      members: [
        {
          vps_id: 'vps_primary',
          decided_role: 'primary_candidate',
          decided_action: 'keep',
          reason: '主力稳定',
        },
        {
          vps_id: 'vps_standby',
          decided_role: 'observe_candidate',
          decided_action: 'review',
          reason: '新增备用观察',
        },
      ],
    }))
  })

  it('saves a manual scenario group as a decision record without touching business assets', async () => {
    const created = decisionRecord({
      record_id: 'adr_manual',
      title: '德国主备自定义组合',
      source_type: 'manual_group',
      source_group_id: 'admg_001',
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/manual-groups/admg_001', body: manualGroupDetail() },
        { url: '/api/asset-decisions/records', method: 'POST', body: created, status: 201 },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('场景与组合')
    await waitFor(() => expect(screen.getAllByText('德国主备自定义组合').length).toBeGreaterThan(0))
    const manualSection = screen.getByRole('heading', { name: '自定义组合' }).closest('section')
    expect(manualSection).not.toBeNull()
    fireEvent.click(within(manualSection!).getByText("德国主备自定义组合"))

    const dialog = await screen.findByRole('dialog', { name: '自定义资产组合详情' })
    expect(within(dialog).queryByRole('heading', { name: '组合推进状态' })).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('heading', { name: '自定义组合证据矩阵' })).not.toBeInTheDocument()
    expect(within(dialog).queryByText('SCENARIO DECISION')).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('证据评估刻度')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('判断与意图')).not.toBeInTheDocument()
    expect(within(dialog).getByLabelText('自定义组合当前判断')).toBeInTheDocument()
    expect(within(dialog).queryByLabelText('自定义组合成员取舍')).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('heading', { name: '组合场景' })).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('自定义组合成员对比')).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('自定义组合成员摘要')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('Germany Primary')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('意图匹配')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('已设置 1/1 个成员动作')).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('详情二级面板')).not.toBeInTheDocument()
    expect(within(dialog).getByRole('tab', { name: '概览' })).toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: '保存为决策记录' })).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: '另存为模板' })).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: '添加成员' })).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: '原始明细' })).not.toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '保存记录' }))
    expect(within(dialog).queryAllByLabelText('角色')).toHaveLength(0)
    expect(within(dialog).queryAllByLabelText('动作')).toHaveLength(0)
    expect(within(dialog).queryAllByLabelText('理由')).toHaveLength(0)
    expect(within(dialog).getByRole('button', { name: '编辑 Germany Primary 成员理由' })).toBeInTheDocument()
    fireEvent.change(within(dialog).getByLabelText('组合目标'), { target: { value: '保留主力并观察备用' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存记录' }))

    await waitFor(() => expect(screen.getByText('已保存组合决策记录：德国主备自定义组合')).toBeInTheDocument())
    expect(findFetchCall(fetchMock, '/api/asset-decisions/records', 'POST')).toEqual([
      '/api/asset-decisions/records',
      {
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({
          source_type: 'manual_group',
          source_group_id: 'admg_001',
          renew_within_days: 30,
          title: '德国主备自定义组合',
          goal: '保留主力并观察备用',
          status: 'draft',
          members: [
            {
              vps_id: 'vps_primary',
              decided_role: 'primary_candidate',
              decided_action: 'keep',
              reason: '主力稳定',
            },
          ],
        }),
      },
    ])
    const writeCalls = fetchMock.mock.calls.filter((call) => call[1]?.method && call[1]?.method !== 'GET')
    expect(writeCalls.some((call) => String(call[0]).startsWith('/api/vps/'))).toBe(false)
    expect(writeCalls.some((call) => String(call[0]).startsWith('/api/subscriptions/'))).toBe(false)
    expect(writeCalls.some((call) => String(call[0]).startsWith('/api/monitoring-instances/'))).toBe(false)
    expect(writeCalls.some((call) => String(call[0]).startsWith('/api/targets/'))).toBe(false)
  })

  it('keeps data-driven long copy and internal ids out of modal covers and directories', async () => {
    const longManualSummary = '这个自定义组合包含一段非常长的报告式判断。它解释预算压力、证据缺口、成员取舍、后续推进和多个字段来源。默认封面不应该把这段报告原样塞进弹窗。'
    const longTemplateGoal = '这个模板目标是一段很长的说明文字。它会描述如何从模板创建组合、如何重新读取事实、如何处理成员蓝图以及为什么要人工复核。默认层不能展示成说明文。'
    const longRecordSummary = '保存时判断是一段很长的记录摘要。它包含执行回读、成员跟进、来源连续性和证据矩阵的解释。默认层必须压缩成一句决策封面。'
    const longManual = manualGroupDetail({
      comparison_insight: comparisonInsight({ summary: longManualSummary }),
      decision_recommendation: { summary: longManualSummary, next_step: longManualSummary, reasons: [], blockers: [] },
    })
    const customTemplate = scenarioTemplate({
      template_id: 'adt_custom_primary_standby',
      builtin: false,
      title: '自定义长目标模板',
      goal: longTemplateGoal,
      source_manual_group_id: 'admg_001',
      member_count: 1,
      members: [
        {
          member_id: 'adtm_001',
          template_id: 'adt_custom_primary_standby',
          vps_id: 'vps_primary',
          intended_role: 'primary_candidate',
          intended_action: 'keep',
          reason: '',
          note: '',
          sort_order: 1,
        },
      ],
    })
    const longRecord = decisionRecord({
      evidence_snapshot: {
        ...decisionRecord().evidence_snapshot,
        comparison_insight: comparisonInsight({ summary: longRecordSummary }),
      },
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      manualGroupsBody: [manualGroupSummary(longManual)],
      templatesBody: [customTemplate],
      recordsBody: [longRecord],
      routes: [
        { url: '/api/asset-decisions/manual-groups/admg_001', body: longManual },
        { url: '/api/asset-decisions/scenario-templates/adt_custom_primary_standby', body: customTemplate },
        { url: '/api/asset-decisions/records/adr_001', body: longRecord },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('场景与组合')
    await waitFor(() => expect(screen.getAllByText('德国主备自定义组合').length).toBeGreaterThan(0))
    const manualSection = screen.getByRole('heading', { name: '自定义组合' }).closest('section')
    fireEvent.click(within(manualSection!).getByText("德国主备自定义组合"))
    const manualDialog = await screen.findByRole('dialog', { name: '自定义资产组合详情' })
    const manualCover = within(manualDialog).getByLabelText('自定义组合当前判断')
    expectDecisionCoverDensity(manualCover)
    expect(manualCover).not.toHaveTextContent(longManualSummary)
    fireEvent.click(within(manualDialog).getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '自定义资产组合详情' })).not.toBeInTheDocument())

    const templatesSection = screen.getByRole('heading', { name: '场景模板' }).closest('section')
    const templateArticle = within(templatesSection!).getByText('自定义长目标模板').closest('article')!
    fireEvent.click(within(templateArticle).getByRole('button', { name: '使用模板' }))
    const templateDialog = await screen.findByRole('dialog', { name: '资产决策场景模板详情' })
    expectTemplateDefaultCover(templateDialog)
    expect(within(templateDialog).getByLabelText('场景模板当前判断')).not.toHaveTextContent(longTemplateGoal)

    fireEvent.click(within(templateDialog).getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '资产决策场景模板详情' })).not.toBeInTheDocument())
    await openSecondaryWorkbench('保存记录')
    const recordsSection = screen.getByRole('heading', { name: '已保存组合决策' }).closest('section')
    fireEvent.click(within(recordsSection!).getByText("德国主备取舍记录"))
    const recordDialog = await screen.findByRole('dialog', { name: '德国主备取舍记录' })
    expectSavedRecordDefaultCover(recordDialog)
    expect(within(recordDialog).getByLabelText('保存记录当前判断')).not.toHaveTextContent(longRecordSummary)
  })

  it('requires an internal confirmation step before removing a manual group member', async () => {
    const updatedManual = manualGroupDetail({
      member_count: 0,
      members: [],
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/manual-groups/admg_001', body: manualGroupDetail() },
        {
          url: '/api/asset-decisions/manual-groups/admg_001/members/vps_primary',
          method: 'DELETE',
          body: updatedManual,
        },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('场景与组合')
    await waitFor(() => expect(screen.getAllByText('德国主备自定义组合').length).toBeGreaterThan(0))
    const manualSection = screen.getByRole('heading', { name: '自定义组合' }).closest('section')
    expect(manualSection).not.toBeNull()
    fireEvent.click(within(manualSection!).getByText("德国主备自定义组合"))

    const dialog = await screen.findByRole('dialog', { name: '自定义资产组合详情' })
    expect(within(dialog).queryByRole('button', { name: '移除' })).not.toBeInTheDocument()
    // Tab navigation replaces detail directory
    openManualGroupMembers(dialog)
    fireEvent.click(within(dialog).getByRole('button', { name: '移除' }))
    const confirmation = await screen.findByRole('alertdialog', { name: '确认移除组合成员' })
    expect(within(dialog).queryByRole('alertdialog', { name: '确认移除组合成员' })).not.toBeInTheDocument()
    expect(screen.queryAllByRole('dialog')).toHaveLength(0)
    expect(screen.queryAllByRole('dialog', { hidden: true })).toHaveLength(1)
    expect(dialog).toHaveAttribute('aria-hidden', 'true')
    expect(dialog).toHaveAttribute('inert')
    expect(findFetchCall(fetchMock, '/api/asset-decisions/manual-groups/admg_001/members/vps_primary', 'DELETE')).toBeUndefined()

    fireEvent.click(within(confirmation).getByRole('button', { name: '确认移除' }))

    await waitFor(() => expect(screen.getByText('成员已移出自定义组合：Germany Primary')).toBeInTheDocument())
    expect(findFetchCall(fetchMock, '/api/asset-decisions/manual-groups/admg_001/members/vps_primary', 'DELETE')).toEqual([
      '/api/asset-decisions/manual-groups/admg_001/members/vps_primary',
      {
        method: 'DELETE',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    ])
  })

  it('requires an internal confirmation step before archiving a custom scenario template', async () => {
    const customTemplate = scenarioTemplate({
      template_id: 'adt_custom_primary_standby',
      builtin: false,
      title: '自定义主备模板',
      status: 'active',
    })
    const archivedTemplate = scenarioTemplate({
      ...customTemplate,
      status: 'archived',
      archived_at: '2026-06-06T09:00:00Z',
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      templatesBody: [customTemplate],
      routes: [
        { url: '/api/asset-decisions/scenario-templates/adt_custom_primary_standby', body: customTemplate },
        {
          url: '/api/asset-decisions/scenario-templates/adt_custom_primary_standby',
          method: 'PATCH',
          body: archivedTemplate,
        },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
        <LocationProbe />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('场景与组合')
    await waitFor(() => expect(screen.getAllByText('自定义主备模板').length).toBeGreaterThan(0))
    const templatesSection = screen.getByRole('heading', { name: '场景模板' }).closest('section')
    expect(templatesSection).not.toBeNull()
    const templateArticle = within(templatesSection!).getByText("自定义主备模板").closest("article")!; fireEvent.click(within(templateArticle).getByRole("button", { name: "使用模板" }))

    const dialog = await screen.findByRole('dialog', { name: '资产决策场景模板详情' })
    expectTemplateDefaultCover(dialog)
    // Tab navigation replaces detail directory
    expect(within(dialog).queryByText('从模板启动一个新的自定义组合。')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('查看模板固定的成员意图。')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('归档或重新启用这个模板。')).not.toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('tab', { name: '状态' }))
    const archiveButton = within(dialog).getByRole('button', { name: '归档模板' })
    const urlBeforeConfirmation = screen.getByLabelText('current-url').textContent

    archiveButton.focus()
    fireEvent.click(archiveButton)
    let confirmation = await screen.findByRole('alertdialog', { name: '确认归档模板' })
    expect(within(dialog).queryByRole('alertdialog', { name: '确认归档模板' })).not.toBeInTheDocument()
    expect(within(confirmation).getByText('归档后不能直接从该模板创建新组合。')).toBeInTheDocument()
    expect(screen.queryAllByRole('dialog')).toHaveLength(0)
    expect(screen.queryAllByRole('dialog', { hidden: true })).toHaveLength(1)
    expect(dialog).toHaveAttribute('aria-hidden', 'true')
    expect(dialog).toHaveAttribute('inert')
    expect(findFetchCall(fetchMock, '/api/asset-decisions/scenario-templates/adt_custom_primary_standby', 'PATCH')).toBeUndefined()

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('alertdialog', { name: '确认归档模板' })).not.toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: '资产决策场景模板详情' })).toBe(dialog)
    expect(within(dialog).getByRole('tab', { name: '状态' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByLabelText('current-url')).toHaveTextContent(urlBeforeConfirmation ?? '')
    await waitFor(() => expect(archiveButton).toHaveFocus())

    fireEvent.click(archiveButton)
    confirmation = await screen.findByRole('alertdialog', { name: '确认归档模板' })

    fireEvent.click(within(confirmation).getByRole('button', { name: '确认归档模板' }))

    await waitFor(() => expect(screen.getByText('模板状态已更新：自定义主备模板 -> 已归档')).toBeInTheDocument())
    expect(findFetchCall(fetchMock, '/api/asset-decisions/scenario-templates/adt_custom_primary_standby', 'PATCH')).toEqual([
      '/api/asset-decisions/scenario-templates/adt_custom_primary_standby',
      {
        method: 'PATCH',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({ status: 'archived' }),
      },
    ])
  })

  it('creates a manual group from a scenario template through the template detail action', async () => {
    const customTemplate = scenarioTemplate({
      template_id: 'adt_custom_primary_standby',
      builtin: false,
      title: '自定义主备模板',
      status: 'active',
    })
    const createdManual = manualGroupDetail({
      manual_group_id: 'admg_from_template',
      title: '模板生成组合',
      source_type: 'scenario_template',
      source_group_id: 'adt_custom_primary_standby',
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      templatesBody: [customTemplate],
      routes: [
        { url: '/api/asset-decisions/scenario-templates/adt_custom_primary_standby', body: customTemplate },
        { url: '/api/asset-decisions/scenario-templates/adt_custom_primary_standby/manual-groups', method: 'POST', body: createdManual, status: 201 },
        { url: '/api/asset-decisions/manual-groups/admg_from_template', body: createdManual },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('场景与组合')
    await waitFor(() => expect(screen.getAllByText('自定义主备模板').length).toBeGreaterThan(0))
    const templatesSection = screen.getByRole('heading', { name: '场景模板' }).closest('section')
    fireEvent.click(within(templatesSection!).getByRole('button', { name: '使用模板' }))

    const dialog = await screen.findByRole('dialog', { name: '资产决策场景模板详情' })
    fireEvent.click(within(dialog).getByRole('button', { name: '创建组合' }))
    expect(within(dialog).getByRole('heading', { name: '从模板创建自定义组合' })).toBeInTheDocument()
    fireEvent.change(within(dialog).getByLabelText('标题'), { target: { value: '模板生成组合' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '创建组合' }))

    await waitFor(() => expect(screen.getByText('已从模板创建自定义组合：模板生成组合')).toBeInTheDocument())
    expect(await screen.findByRole('dialog', { name: '自定义资产组合详情' })).toBeInTheDocument()
    expect(findFetchCall(fetchMock, '/api/asset-decisions/scenario-templates/adt_custom_primary_standby/manual-groups', 'POST')).toEqual([
      '/api/asset-decisions/scenario-templates/adt_custom_primary_standby/manual-groups',
      {
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({
          title: '模板生成组合',
          goal: customTemplate.goal,
          note: '',
          scenario: 'primary_standby',
          status: 'active',
          renew_within_days: 30,
        }),
      },
    ])
  })

  it('opens saved decision records and patches record status', async () => {
    const patched = decisionRecord({
      status: 'in_progress',
      updated_at: '2026-06-05T09:00:00Z',
      decided_at: '2026-06-05T09:00:00Z',
    })
    const quickDone = decisionRecord({
      status: 'in_progress',
      followup_todo_count: 1,
      followup_done_count: 1,
      updated_at: '2026-06-05T09:05:00Z',
      decided_at: '2026-06-05T09:00:00Z',
      members: [
        {
          ...decisionRecord().members[0],
          followup_status: 'done',
          followup_updated_at: '2026-06-05T09:05:00Z',
        },
      ],
    })
    const followupPatched = decisionRecord({
      status: 'in_progress',
      followup_todo_count: 1,
      followup_blocked_count: 1,
      execution_readback: recordReadback({
        status: 'drift',
        summary: '1 台 VPS 与当前事实不一致',
        drift_count: 1,
        blocked_count: 0,
        needs_evidence_count: 0,
        aligned_count: 0,
      }),
      execution_plan: recordExecutionPlan({
        summary: '1 台 VPS 事实漂移，优先复核闭环',
        lane_counts: [{ lane: 'cancel_retire', count: 1 }],
        actionable_count: 1,
      }),
      updated_at: '2026-06-05T09:10:00Z',
      decided_at: '2026-06-05T09:00:00Z',
      members: [
        {
          ...decisionRecord().members[0],
          followup_status: 'blocked',
          followup_note: '等待迁移窗口',
          followup_updated_at: '2026-06-05T09:10:00Z',
          execution_readback: memberReadback({
            status: 'drift',
            summary: '跟进已完成，但当前事实仍未闭环',
            issues: [
              { kind: 'active_subscription_remaining', label: '仍有 active 订阅', tone: 'critical', details: 'active subscription: 1' },
              { kind: 'running_target_remaining', label: '仍有关联 Target 运行', tone: 'critical', details: 'running target: 1' },
            ],
          }),
          execution_plan: memberExecutionPlan({
            lane: 'cancel_retire',
            step_kind: 'open_cancellation_workbench',
            tone: 'critical',
            summary: '当前事实与判断不一致，需要复核闭环',
            step_label: '打开取消/退役工作台',
            issue_count: 2,
            actionable: true,
          }),
        },
      ],
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/records/adr_001', body: decisionRecord() },
        { url: '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30', body: groupDetail() },
        {
          url: '/api/asset-decisions/records/adr_001',
          method: 'PATCH',
          responses: [
            { body: patched },
            { body: quickDone },
            { body: followupPatched },
          ],
        },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('保存记录')
    await waitFor(() => expect(screen.getAllByText('德国主备取舍记录').length).toBeGreaterThan(0))
    const recordsSection = screen.getByRole('heading', { name: '已保存组合决策' }).closest('section')
    expect(recordsSection).not.toBeNull()
    fireEvent.click(within(recordsSection!).getByText("德国主备取舍记录"))

    const dialog = await screen.findByRole('dialog', { name: '德国主备取舍记录' })
    expect(within(dialog).getByLabelText('保存记录当前判断')).toBeInTheDocument()
    expect(within(dialog).queryByRole('heading', { name: '快照对比矩阵' })).not.toBeInTheDocument()
    expect(within(dialog).queryByText('GOAL')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('SAVED EVIDENCE')).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('证据评估刻度')).not.toBeInTheDocument()
    expectSavedRecordDefaultCover(dialog)
    expect(within(dialog).getByText(/草稿 · 跟进 0\/2 · 需补证据/)).toBeInTheDocument()
    expect(within(dialog).queryByText('执行编排')).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('heading', { name: '来源与当前闭环' })).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('Germany Primary 跟进状态')).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('决策记录成员')).not.toBeInTheDocument()
    // Tab navigation replaces detail directory
    fireEvent.click(within(dialog).getByRole('tab', { name: /执行/ }))
    expect(within(dialog).getByLabelText('执行编排')).toBeInTheDocument()
    expect(within(dialog).queryByLabelText('保存时判断依据')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('保存时判断依据')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('EXECUTION PLAN')).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('heading', { name: '执行编排' })).not.toBeInTheDocument()
    expectNoDetailCoverWhileInTaskPanel(dialog)
    expectTaskPanelDensity(dialog, { textMax: 260, interactiveMax: 9, inputsMax: 1, memberRowsMax: 3 })
    fireEvent.change(within(dialog).getByLabelText('推进状态'), { target: { value: 'in_progress' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '更新状态' }))

    await waitFor(() => expect(screen.getByText('决策记录状态已更新：德国主备取舍记录 -> 推进中')).toBeInTheDocument())
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/records/adr_001')
    const firstRecordPatchCalls = fetchMock.mock.calls.filter((call) => (
      call[0] === '/api/asset-decisions/records/adr_001' && call[1]?.method === 'PATCH'
    ))
    expect(firstRecordPatchCalls[0]).toEqual([
      '/api/asset-decisions/records/adr_001',
      {
        method: 'PATCH',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({ status: 'in_progress' }),
      },
    ])

    fireEvent.click(within(dialog).getByRole('button', { name: '标记完成' }))
    await waitFor(() => expect(screen.getByText('成员跟进已更新：Germany Primary -> 已完成')).toBeInTheDocument())
    const quickPatchCalls = fetchMock.mock.calls.filter((call) => (
      call[0] === '/api/asset-decisions/records/adr_001' && call[1]?.method === 'PATCH'
    ))
    expect(quickPatchCalls[1]).toEqual([
      '/api/asset-decisions/records/adr_001',
      {
        method: 'PATCH',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({
          members: [{
            vps_id: 'vps_primary',
            followup_status: 'done',
            followup_note: '',
          }],
        }),
      },
    ])

    fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
    expectSavedRecordMembersPanelIsCompact(dialog)
    expectNoDetailCoverWhileInTaskPanel(dialog)
    expectTaskPanelDensity(dialog, { textMax: 220, interactiveMax: 9, inputsMax: 0, memberRowsMax: 3 })
    expect(within(dialog).queryByLabelText('Germany Primary 跟进状态')).not.toBeInTheDocument()

    const followupPanel = within(dialog).getByLabelText('成员跟进列表')
    const primaryFollowupRow = within(followupPanel).getByText('Germany Primary').closest('.asset-decision-record-followup-row') as HTMLElement
    expect(primaryFollowupRow).not.toBeNull()
    fireEvent.click(within(primaryFollowupRow).getByRole('button', { name: '编辑跟进' }))

    const statusInput = within(primaryFollowupRow).getByLabelText('跟进状态')
    const noteInput = within(primaryFollowupRow).getByLabelText('跟进备注')
    fireEvent.change(statusInput, { target: { value: 'blocked' } })
    fireEvent.change(noteInput, { target: { value: '等待迁移窗口' } })
    fireEvent.click(within(primaryFollowupRow).getByRole('button', { name: '保存跟进' }))

    await waitFor(() => expect(screen.getByText('成员跟进已更新：Germany Primary -> 阻塞')).toBeInTheDocument())
    const secondRecordPatchCalls = fetchMock.mock.calls.filter((call) => (
      call[0] === '/api/asset-decisions/records/adr_001' && call[1]?.method === 'PATCH'
    ))
    expect(secondRecordPatchCalls[2]).toEqual([
      '/api/asset-decisions/records/adr_001',
      {
        method: 'PATCH',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({
          members: [{
            vps_id: 'vps_primary',
            followup_status: 'blocked',
            followup_note: '等待迁移窗口',
          }],
        }),
      },
    ])
    expect(within(primaryFollowupRow).getByLabelText('跟进备注')).toHaveValue('等待迁移窗口')
    expect(within(dialog).getAllByText('有漂移').length).toBeGreaterThan(0)
    expect(within(dialog).queryByText('仍有 active 订阅')).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('link', { name: '打开取消/退役工作台' })).not.toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('tab', { name: /执行/ }))
    const cancelLinks = within(dialog).getAllByRole('link', { name: '打开取消/退役工作台' })
    expect(cancelLinks[0]).toHaveAttribute('href', '/vps/vps_primary?workbench=cancellation')
    fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
    const hasRawPanel = openSavedRecordRawMembersPanel(dialog)
    if (hasRawPanel) {
      expect(within(dialog).getAllByText('仍有 active 订阅').length).toBeGreaterThan(0)
      const rawMembers = within(dialog).getByLabelText('决策记录成员')
      const rawPrimaryRow = within(rawMembers).getByText('Germany Primary').closest('tr') as HTMLElement
      const rawPrimaryEditButton = within(rawPrimaryRow).queryByRole('button', { name: '编辑' })
      if (rawPrimaryEditButton) {
        fireEvent.click(rawPrimaryEditButton)
      }
      expect(within(rawPrimaryRow).getByLabelText('跟进状态')).toBeInTheDocument()
      expect(within(rawPrimaryRow).getByLabelText('跟进备注')).toHaveValue('等待迁移窗口')
      fireEvent.click(within(rawPrimaryRow).getByRole('button', { name: '收起' }))
    }
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/vps/'))).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/subscriptions/') && call[1]?.method)).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/monitoring-instances/') && call[1]?.method)).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/targets/') && call[1]?.method)).toBe(false)

    fireEvent.click(within(dialog).getByRole('tab', { name: '概览' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '复核来源' }))
    expect(within(dialog).queryByText('SOURCE CONTINUITY')).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('heading', { name: '来源与当前闭环' })).not.toBeInTheDocument()
    const sourcePanel = within(dialog).getByLabelText('决策记录来源连续性')
    expect(within(sourcePanel).getByText('来自自动组 未来 30 天')).toBeInTheDocument()
    expect(within(sourcePanel).getByText('自动组 · 续费取舍 · 未来 30 天')).toBeInTheDocument()
    expect(sourcePanel).not.toHaveTextContent(/adg_auto_001|renewal_attention/)
    fireEvent.click(within(dialog).getByRole('button', { name: '复核来源' }))
    const sourceDialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    expect(within(sourceDialog).queryByRole('heading', { name: '场景推进建议' })).not.toBeInTheDocument()
    expect(within(sourceDialog).queryByRole('heading', { name: '证据矩阵 / 取舍对比' })).not.toBeInTheDocument()
    expect(within(sourceDialog).getByLabelText('决策组当前判断')).toBeInTheDocument()
    expectAutomaticGroupDefaultCover(sourceDialog)
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30')
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/vps/') && call[1]?.method === 'PATCH')).toBe(false)
  })

  it('caps saved record execution board to actionable preview cards for large records', async () => {
    const largeRecord = decisionRecordWithManyMembers(8)
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      recordsBody: [largeRecord],
      routes: [
        { url: '/api/asset-decisions/records/adr_001', body: largeRecord },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('保存记录')
    await waitFor(() => expect(screen.getAllByText('德国主备取舍记录').length).toBeGreaterThan(0))
    const recordsSection = screen.getByRole('heading', { name: '已保存组合决策' }).closest('section')
    fireEvent.click(within(recordsSection!).getByText("德国主备取舍记录"))

    const dialog = await screen.findByRole('dialog', { name: '德国主备取舍记录' })
    // Tab navigation replaces detail directory
    fireEvent.click(within(dialog).getByRole('tab', { name: /执行/ }))
    const executionBoard = within(dialog).getByLabelText('执行编排')
    expect(executionBoard.querySelectorAll('.asset-decision-execution-card')).toHaveLength(3)
    expect(within(executionBoard).getByText('Record Bulk 3')).toBeInTheDocument()
    expect(within(executionBoard).getByText('Record Bulk 6')).toBeInTheDocument()
    expect(within(executionBoard).getByText('Record Bulk 2')).toBeInTheDocument()
    expect(within(executionBoard).queryByText('Record Bulk 1')).not.toBeInTheDocument()
    expect(within(executionBoard).getByText('另有 5 台在成员跟进或底稿中查看')).toBeInTheDocument()
  })

  it('maps execution plan subscription CTA without writing business assets', async () => {
    const evidenceRecord = decisionRecord({
      execution_plan: recordExecutionPlan({
        summary: '1 台 VPS 需要补齐证据',
        lane_counts: [{ lane: 'evidence', count: 1 }],
        actionable_count: 1,
      }),
      members: [
        {
          ...decisionRecord().members[0],
          decided_action: 'complete_evidence',
          execution_readback: memberReadback({
            status: 'needs_evidence',
            summary: '仍需补齐证据',
            issues: [{ kind: 'evidence_gap', label: '缺订阅', tone: 'alert', details: '' }],
          }),
          execution_plan: memberExecutionPlan({
            lane: 'evidence',
            step_kind: 'open_subscription_context',
            tone: 'alert',
            summary: '证据仍未补齐，先补上下文再确认判断',
            step_label: '核对订阅上下文',
            issue_count: 1,
            actionable: true,
          }),
        },
      ],
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/records/adr_001', body: evidenceRecord },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('保存记录')
    await waitFor(() => expect(screen.getAllByText('德国主备取舍记录').length).toBeGreaterThan(0))
    const recordsSection = screen.getByRole('heading', { name: '已保存组合决策' }).closest('section')
    fireEvent.click(within(recordsSection!).getByText("德国主备取舍记录"))

    const dialog = await screen.findByRole('dialog', { name: '德国主备取舍记录' })
    // Tab navigation replaces detail directory
    fireEvent.click(within(dialog).getByRole('tab', { name: /执行/ }))
    expect(within(dialog).getAllByText('证据仍未补齐，先补上下文再确认判断').length).toBeGreaterThan(0)
    const subscriptionLinks = within(dialog).getAllByRole('link', { name: '核对订阅上下文' })
    expect(subscriptionLinks[0]).toHaveAttribute('href', '/subscriptions?vps_id=vps_primary')
    const writeCalls = fetchMock.mock.calls.filter((call) => call[1]?.method && call[1]?.method !== 'GET')
    expect(writeCalls).toEqual([])
  })

  it('renders IP quality evidence and current facts in saved decision readback', async () => {
    const ipRiskRecord = decisionRecord({
      execution_readback: recordReadback({
        status: 'blocked',
        summary: 'IP 质量阻塞迁移判断',
        blocked_count: 1,
        needs_evidence_count: 0,
      }),
      members: [
        {
          ...decisionRecord().members[0],
          decided_action: 'migrate',
          execution_readback: memberReadback({
            status: 'blocked',
            summary: 'IP 质量风险仍未解除',
            issues: [
              { kind: 'ip_quality_risk', label: 'IP 高风险', tone: 'critical', details: 'provider 风险过高' },
              { kind: 'media_unlock_blocked', label: 'ChatGPT 受阻', tone: 'alert', details: '解锁区域不可用' },
            ],
            current_facts: {
              found: true,
              lifecycle_status: 'active',
              usage_status: 'in_use',
              renewal_decision: 'migrate',
              active_subscription_count: 1,
              service_count: 2,
              domain_count: 1,
              target_count: 1,
              running_target_count: 1,
              monitoring_link_count: 1,
              running_monitoring_count: 1,
              abnormal_monitoring_count: 0,
              active_incident_count: 0,
              ip_quality_summary: {
                observed_at: '2026-06-07T08:00:00Z',
                ip_address: '203.0.113.9',
                ip_version: 4,
                status: 'success',
                risk_level: 'high',
                use_region_code: 'JP',
                use_region_name: 'Japan',
                asn: 'AS64500',
                organization: 'Example Transit',
                stale: false,
                ambiguous: false,
                assignment_mode: 'monitoring_link',
                provider_count: 3,
                unlockable_count: 1,
              },
              ip_quality_provider_risk_signal_count: 2,
              ip_quality_blocked_services: ['ChatGPT', 'Netflix'],
              source_availability: sourceAvailability,
            },
          }),
          execution_plan: memberExecutionPlan({
            lane: 'migration',
            step_kind: 'open_vps_detail',
            tone: 'critical',
            summary: '先复核 IP 质量再确认迁移意向',
            step_label: '打开 VPS 详情推进迁移',
            issue_count: 2,
            blocked: true,
            actionable: true,
          }),
        },
      ],
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/records/adr_001', body: ipRiskRecord },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('保存记录')
    await waitFor(() => expect(screen.getAllByText('德国主备取舍记录').length).toBeGreaterThan(0))
    const recordsSection = screen.getByRole('heading', { name: '已保存组合决策' }).closest('section')
    fireEvent.click(within(recordsSection!).getByText("德国主备取舍记录"))

    const dialog = await screen.findByRole('dialog', { name: '德国主备取舍记录' })
    // Tab navigation replaces detail directory
    fireEvent.click(within(dialog).getByRole('tab', { name: /执行/ }))
    expect(within(dialog).getAllByText('IP 高风险').length).toBeGreaterThan(0)
    expect(within(dialog).getAllByText('ChatGPT 受阻').length).toBeGreaterThan(0)
    expect(within(dialog).queryByText(/IP 203\.0\.113\.9/)).not.toBeInTheDocument()
    expect(within(dialog).queryByText(/风险 provider 2/)).not.toBeInTheDocument()
    expect(within(dialog).queryByText(/受阻 ChatGPT、Netflix/)).not.toBeInTheDocument()
    expect(within(dialog).queryByText('打开 VPS 详情推进迁移')).not.toBeInTheDocument()
    expect(within(dialog).getByRole('link', { name: '复核迁移意向' })).toHaveAttribute('href', '/vps/vps_primary')
    fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
    expectSavedRecordMembersPanelIsCompact(dialog)
    expect(within(dialog).queryByText(/IP 203\.0\.113\.9/)).not.toBeInTheDocument()
    expect(within(dialog).queryByText(/风险 provider 2/)).not.toBeInTheDocument()
    expect(within(dialog).queryByText(/受阻 ChatGPT、Netflix/)).not.toBeInTheDocument()
    const hasRawPanel = openSavedRecordRawMembersPanel(dialog)
    if (hasRawPanel) {
      expect(within(dialog).getAllByText(/IP 203\.0\.113\.9/).length).toBeGreaterThan(0)
      expect(within(dialog).getAllByText(/风险 provider 2/).length).toBeGreaterThan(0)
      expect(within(dialog).getAllByText(/受阻 ChatGPT、Netflix/).length).toBeGreaterThan(0)
      expect(within(dialog).queryByText('打开 VPS 详情推进迁移')).not.toBeInTheDocument()
      expect(within(dialog).getAllByText('复核迁移意向').length).toBeGreaterThan(0)
    }
  })

  it('falls back gracefully when saved record snapshots do not include comparison insight', async () => {
    const legacyRecord = decisionRecord({
      evidence_snapshot: {
        group_id: 'adg_auto_001',
        monthly_cost_base: 140,
        base_currency: 'CNY',
        evidence_assessment: evidenceAssessment({
          quality_tier: 'usable',
          decision_bias: 'review',
          summary: '旧记录证据可用',
        }),
      },
      members: [
        {
          ...decisionRecord().members[0],
          evidence_snapshot: {
            service_count: 2,
            domain_count: 1,
            running_monitoring_count: 1,
            monitoring_link_count: 1,
            primary_issue_summary: '',
            evidence_assessment: evidenceAssessment(),
          },
        },
      ],
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      recordsBody: [legacyRecord],
      routes: [
        { url: '/api/asset-decisions/records/adr_001', body: legacyRecord },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('保存记录')
    await waitFor(() => expect(screen.getAllByText('德国主备取舍记录').length).toBeGreaterThan(0))
    const recordsSection = screen.getByRole('heading', { name: '已保存组合决策' }).closest('section')
    fireEvent.click(within(recordsSection!).getByText("德国主备取舍记录"))

    const dialog = await screen.findByRole('dialog', { name: '德国主备取舍记录' })
    expect(within(dialog).getByLabelText('保存记录当前判断')).toBeInTheDocument()
    expect(within(dialog).queryByRole('heading', { name: '快照对比矩阵' })).not.toBeInTheDocument()
    expect(within(dialog).queryByText('保存时未记录对比洞察；当前仍保留证据评估快照、成员判断、执行回读和执行编排。')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('快照成员 1')).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('保存记录成员摘要')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('旧记录证据可用')).not.toBeInTheDocument()
    expect(within(dialog).queryByLabelText('证据评估刻度')).not.toBeInTheDocument()
    expect(within(dialog).getByText(/草稿 · 跟进 0\/2 · 需补证据/)).toBeInTheDocument()
    // Tab navigation replaces detail directory
    expect(within(dialog).queryByText('旧记录证据可用')).not.toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('tab', { name: /执行/ }))
    expect(within(dialog).queryByText('旧记录证据可用')).not.toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('tab', { name: /成员/ }))
    expectSavedRecordMembersPanelIsCompact(dialog)
    expect(within(dialog).queryByText('服务 2 · 域名 1')).not.toBeInTheDocument()
    const hasRawPanel = openSavedRecordRawMembersPanel(dialog)
    if (hasRawPanel) {
      expect(within(dialog).getByText('服务 2 · 域名 1')).toBeInTheDocument()
    }
  })

  it('keeps the single VPS renewal decision PATCH payload unchanged', async () => {
    const updated = {
      ...vps,
      renewal_decision: 'migrate',
      updated_at: '2026-05-09T09:00:00Z',
    }
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/vps/vps_review', method: 'PATCH', body: updated },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('单台队列')
    await waitFor(() => expect(screen.getAllByText('Tokyo Review').length).toBeGreaterThan(0))
    const singleQueue = screen.getByRole('heading', { name: '单台辅助队列' }).closest('section')
    expect(singleQueue).not.toBeNull()
    expectTabPanelRelationship(singleQueue!, '单台辅助队列视图')
    fireEvent.click(within(singleQueue!).getAllByRole('button', { name: '处理' })[0])
    const drawer = await screen.findByRole('dialog', { name: '续费决策处理' })
    fireEvent.change(within(drawer).getByLabelText('续费决策'), { target: { value: 'migrate' } })
    fireEvent.change(within(drawer).getByLabelText('决策理由'), { target: { value: 'move to Osaka' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '保存续费决策' }))

    await waitFor(() => expect(screen.getByText('续费决策已保存：Tokyo Review -> 迁移')).toBeInTheDocument())
    expect(findFetchCall(fetchMock, '/api/vps/vps_review', 'PATCH')).toEqual([
      '/api/vps/vps_review',
      {
        method: 'PATCH',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        cache: 'no-store',
        credentials: 'include',
        body: JSON.stringify({
          renewal_decision: 'migrate',
          renewal_reason: 'move to Osaka',
        }),
      },
    ])
  })

  it('replays the compatibility refresh inventory after a renewal decision', async () => {
    const updated = {
      ...vps,
      renewal_decision: 'migrate',
      updated_at: '2026-05-09T09:00:00Z',
    }
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/vps/vps_review', method: 'PATCH', body: updated },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('单台队列')
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(11))
    const mutationStart = fetchMock.mock.calls.length
    const singleQueue = screen.getByRole('heading', { name: '单台辅助队列' }).closest('section')
    fireEvent.click(within(singleQueue!).getAllByRole('button', { name: '处理' })[0])
    const drawer = await screen.findByRole('dialog', { name: '续费决策处理' })
    fireEvent.change(within(drawer).getByLabelText('续费决策'), { target: { value: 'migrate' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '保存续费决策' }))

    await waitFor(() => expect(fetchMock.mock.calls.length).toBe(mutationStart + 12))
    expect(fetchRequestInventory(fetchMock, mutationStart)).toEqual([
      'GET /api/asset-decisions/groups?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/manual-groups?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/overview?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/records?view=needs_decision&renew_within_days=30',
      'GET /api/asset-decisions/scenario-templates',
      'GET /api/subscriptions?renew_within_days=30&sort=renew_at&order=asc',
      'GET /api/subscriptions?sort=renew_at&order=asc',
      'GET /api/vps',
      'GET /api/vps?renewal_decision=cancel',
      'GET /api/vps?renewal_decision=migrate',
      'GET /api/vps?renewal_decision=unreviewed',
      'PATCH /api/vps/vps_review',
    ])
  })

  it('does not turn subscription evidence failure into missing-subscription decisions', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      groupsBody: [groupSummary({ evidence_chips: [] })],
      routes: [
        {
          url: '/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc',
          body: { error: 'subscription evidence unavailable' },
          status: 500,
        },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0))
    await openSecondaryWorkbench('续费窗口')
    expect(screen.getByRole('heading', { name: '续费候选不可用' })).toBeInTheDocument()
    expect(screen.getAllByText(/subscription evidence unavailable/).length).toBeGreaterThan(0)
    const groupList = screen.getByRole('heading', { name: '决策组扫描' }).closest('section')
    expect(groupList).not.toBeNull()
    expect(within(groupList!).queryByText('缺订阅')).not.toBeInTheDocument()
    expect(within(groupList!).queryByText('暂无证据标签')).not.toBeInTheDocument()
    expect(within(groupList!).queryByText('证据稳定')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '单台队列不可用' })).not.toBeInTheDocument()
  })
})

describe('AssetDecisionsPage 结构守护', () => {
  it('主文件不超过 800 行', async () => {
    const files = import.meta.glob('./AssetDecisionsPage.tsx', { query: '?raw', import: 'default', eager: true })
    expect(Object.keys(files)).toEqual(['./AssetDecisionsPage.tsx'])
    const content = files['./AssetDecisionsPage.tsx']
    expect(typeof content).toBe('string')
    expect(countSourceLines(content as string)).toBeLessThanOrEqual(800)
  })

  it('弹窗组件各自不超过 200 行', async () => {
    const files = import.meta.glob('./asset-decisions/modals/*.tsx', { query: '?raw', import: 'default', eager: true })
    const productionFiles = Object.fromEntries(
      Object.entries(files).filter(([path]) => !path.endsWith('.test.tsx')),
    )
    expect(Object.keys(productionFiles).sort()).toEqual([
      './asset-decisions/modals/GroupDetailModal.tsx',
      './asset-decisions/modals/ManualGroupDetailModal.tsx',
      './asset-decisions/modals/RecordDetailModal.tsx',
      './asset-decisions/modals/RenewalDecisionModal.tsx',
      './asset-decisions/modals/TemplateDetailModal.tsx',
    ])
    for (const [path, content] of Object.entries(productionFiles)) {
      expect(typeof content).toBe('string')
      const lineCount = countSourceLines(content as string)
      expect(lineCount, `${path} should be <= 200 lines`).toBeLessThanOrEqual(200)
    }
  })

  it('不再使用资产决策 primary/secondary/tertiary 碎片层级类', async () => {
    const sourceFiles = import.meta.glob([
      './AssetDecisionsPage.tsx',
      './AssetDecisionsPageContent.tsx',
      './asset-decisions/**/*.tsx',
      '../index.css',
    ], { query: '?raw', import: 'default', eager: true })

    for (const [path, content] of Object.entries(sourceFiles)) {
      expect(typeof content).toBe('string')
      expect(content as string, path).not.toMatch(/asset-decision-(primary|secondary|tertiary)-/)
    }
  })
})

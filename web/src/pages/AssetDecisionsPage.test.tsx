import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
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

describe('AssetDecisionsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the portfolio workbench as the primary surface', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock)
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('heading', { name: '资产组合决策' })).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '下一步导览' })).toBeInTheDocument()
    expect(screen.getByText('CLOSED LOOP')).toBeInTheDocument()
    expect(screen.getByText('回读缺证据')).toBeInTheDocument()
    expect(screen.getByText('AUTO')).toBeInTheDocument()
    expect(screen.getByText('DRIFT')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '决策组列表' })).toBeInTheDocument()
    expect(screen.getByLabelText('决策组扫描列表')).toBeInTheDocument()
    expect(screen.getAllByText('德国主力组合').length).toBeGreaterThan(0)
    expect(screen.getAllByText('证据强').length).toBeGreaterThan(0)
    expect(screen.getAllByText('窗口内 / 未评估').length).toBeGreaterThan(0)
    expect(screen.getByRole('heading', { name: '自定义组合' })).toBeInTheDocument()
    expect(screen.getAllByText('德国主备自定义组合').length).toBeGreaterThan(0)
    expect(screen.getByRole('heading', { name: '已保存组合决策' })).toBeInTheDocument()
    expect(screen.getAllByText('德国主备取舍记录').length).toBeGreaterThan(0)
    expect(screen.getAllByText('需补证据').length).toBeGreaterThan(0)
    expect(screen.getByText('缺口 1')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '场景模板' })).toBeInTheDocument()
    expect(screen.getAllByText('主备取舍模板').length).toBeGreaterThan(0)
    expect(screen.getByText('RENEWAL EVIDENCE')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '单台待处理队列' })).toBeInTheDocument()
    expect(screen.getAllByText('Tokyo Review').length).toBeGreaterThan(0)

    expectFetchCalledWith(fetchMock, '/api/asset-decisions/overview?view=needs_decision&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups?view=needs_decision&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/records?view=needs_decision&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/manual-groups?view=needs_decision&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/scenario-templates')
    expectFetchCalledWith(fetchMock, '/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc')
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

    await waitFor(() => expect(screen.getByRole('heading', { name: '决策组列表' })).toBeInTheDocument())
    expect(screen.getByRole('status')).toHaveTextContent('旧链接已承接到单台辅助队列')
    expect(screen.getByRole('link', { name: '查看单台队列' })).toHaveAttribute('href', '#single-vps-queue')
    expect(screen.queryByRole('tab', { name: /单台队列/ })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '单台待处理队列' })).toBeInTheDocument()
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/overview?view=needs_decision&renew_within_days=30')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups?view=needs_decision&renew_within_days=30')
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

    await waitFor(() => expect(screen.getByText('事实漂移')).toBeInTheDocument())
    const nextWork = screen.getByLabelText('资产决策下一步工作项')
    fireEvent.click(within(nextWork).getByRole('button', { name: '复核记录' }))

    expect(await screen.findByRole('dialog', { name: '资产组合决策记录详情' })).toBeInTheDocument()
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

    await waitFor(() => expect(screen.getByText('AUTO GROUP')).toBeInTheDocument())
    const nextWork = screen.getByLabelText('资产决策下一步工作项')
    fireEvent.click(within(nextWork).getByRole('button', { name: '打开决策组' }))

    expect(await screen.findByRole('dialog', { name: '资产决策组详情' })).toBeInTheDocument()
    expect(screen.getByLabelText('current-url')).toHaveTextContent('/asset-decisions?group_id=adg_auto_001')
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30')
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/vps/') && call[1]?.method === 'PATCH')).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/subscriptions/') && call[1]?.method)).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/monitoring-instances/') && call[1]?.method)).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/targets/') && call[1]?.method)).toBe(false)
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
    expect(screen.getByRole('button', { name: '打开决策组' })).toBeInTheDocument()
    expect(screen.getByText('组合概览暂不可用，导览只展示已成功加载的事实。')).toBeInTheDocument()
    expect(screen.getByText('组合概览不可用')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '决策组不可用' })).not.toBeInTheDocument()
  })

  it('does not invent readback next-work items when saved records fail to load', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
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

    await waitFor(() => expect(screen.getByRole('heading', { name: '下一步导览' })).toBeInTheDocument())
    const nextWork = screen.getByLabelText('资产决策下一步工作项')
    expect(within(nextWork).queryByText('事实漂移')).not.toBeInTheDocument()
    expect(within(nextWork).queryByText('跟进阻塞')).not.toBeInTheDocument()
    expect(within(nextWork).queryByText('回读缺证据')).not.toBeInTheDocument()
    expect(screen.getByText('决策记录暂不可用，导览只展示已成功加载的事实。')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '暂无需要置顶的组合工作' })).toBeInTheDocument()
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
    expect(within(dialog).getAllByText('Germany Primary').length).toBeGreaterThan(0)
    expect(within(dialog).getByText('Germany Standby')).toBeInTheDocument()
    expect(within(dialog).getByText('主力候选')).toBeInTheDocument()
    expect(within(dialog).getAllByText('保留').length).toBeGreaterThan(0)
    expect(within(dialog).getByText(/服务 2/)).toBeInTheDocument()
    expect(within(dialog).getByText(/监控 1/)).toBeInTheDocument()
    expect(within(dialog).getByText('证据质量')).toBeInTheDocument()
    expect(within(dialog).getAllByText('先补证据').length).toBeGreaterThan(0)
    expectFetchCalledWith(fetchMock, '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30')

    fireEvent.click(within(dialog).getAllByRole('button', { name: '处理' })[0])
    expect(within(dialog).getAllByText('Germany Primary').length).toBeGreaterThan(0)
    expect(within(dialog).getByLabelText('续费决策')).toHaveValue('keep')
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
    fireEvent.click(within(dialog).getByRole('button', { name: '保存为决策记录' }))
    fireEvent.change(within(dialog).getByLabelText('标题'), { target: { value: '德国主备取舍' } })
    fireEvent.change(within(dialog).getByLabelText('状态'), { target: { value: 'decided' } })
    fireEvent.change(within(dialog).getByLabelText('组合目标'), { target: { value: '保留主力，补齐备用证据' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存记录' }))

    await waitFor(() => expect(screen.getByText('已保存组合决策记录：德国主备取舍')).toBeInTheDocument())
    expect(await screen.findByRole('dialog', { name: '资产组合决策记录详情' })).toBeInTheDocument()
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
    fireEvent.click(within(dialog).getByRole('button', { name: '创建自定义组合' }))

    await waitFor(() => expect(screen.getByText('已创建自定义组合：德国主力组合')).toBeInTheDocument())
    const manualDialog = await screen.findByRole('dialog', { name: '自定义资产组合详情' })
    expect(within(manualDialog).getByRole('heading', { name: '组合场景' })).toBeInTheDocument()
    expect(within(manualDialog).getByDisplayValue('主力稳定')).toBeInTheDocument()
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

    await waitFor(() => expect(screen.getAllByText('德国主备自定义组合').length).toBeGreaterThan(0))
    const manualSection = screen.getByRole('heading', { name: '自定义组合' }).closest('section')
    expect(manualSection).not.toBeNull()
    fireEvent.click(within(manualSection!).getByRole('button', { name: '查看组合' }))

    const dialog = await screen.findByRole('dialog', { name: '自定义资产组合详情' })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存为决策记录' }))
    fireEvent.change(within(dialog).getAllByLabelText('组合目标')[1], { target: { value: '保留主力并观察备用' } })
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

    await waitFor(() => expect(screen.getAllByText('德国主备取舍记录').length).toBeGreaterThan(0))
    const recordsSection = screen.getByRole('heading', { name: '已保存组合决策' }).closest('section')
    expect(recordsSection).not.toBeNull()
    expect(within(recordsSection!).getByText('下一步导览')).toBeInTheDocument()
    fireEvent.click(within(recordsSection!).getByRole('button', { name: '查看记录' }))

    const dialog = await screen.findByRole('dialog', { name: '资产组合决策记录详情' })
    expect(within(dialog).getByText('主力保留')).toBeInTheDocument()
    expect(within(dialog).getByText('证据快照')).toBeInTheDocument()
    expect(within(dialog).getByText('执行回读')).toBeInTheDocument()
    expect(within(dialog).getByText('执行编排')).toBeInTheDocument()
    expect(within(dialog).getAllByText('保留观察').length).toBeGreaterThan(0)
    expect(within(dialog).getAllByText('当前事实已对齐，待确认跟进状态').length).toBeGreaterThan(0)
    expect(within(dialog).getAllByText('已对齐').length).toBeGreaterThan(0)
    expect(within(dialog).getAllByText(/订阅 1 · 服务 2/).length).toBeGreaterThan(0)
    expect(within(dialog).getAllByText('可决策').length).toBeGreaterThan(0)
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

    fireEvent.change(within(dialog).getByLabelText('Germany Primary 跟进状态'), { target: { value: 'blocked' } })
    fireEvent.change(within(dialog).getByLabelText('Germany Primary 跟进备注'), { target: { value: '等待迁移窗口' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存跟进' }))

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
    expect(within(dialog).getByLabelText('Germany Primary 跟进备注')).toHaveValue('等待迁移窗口')
    expect(within(dialog).getAllByText('有漂移').length).toBeGreaterThan(0)
    expect(within(dialog).getAllByText('仍有 active 订阅').length).toBeGreaterThan(0)
    const cancelLinks = within(dialog).getAllByRole('link', { name: '打开取消/退役工作台' })
    expect(cancelLinks[0]).toHaveAttribute('href', '/vps/vps_primary?workbench=cancellation')
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/vps/'))).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/subscriptions/') && call[1]?.method)).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/monitoring-instances/') && call[1]?.method)).toBe(false)
    expect(fetchMock.mock.calls.some((call) => String(call[0]).startsWith('/api/targets/') && call[1]?.method)).toBe(false)
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

    await waitFor(() => expect(screen.getAllByText('德国主备取舍记录').length).toBeGreaterThan(0))
    const recordsSection = screen.getByRole('heading', { name: '已保存组合决策' }).closest('section')
    fireEvent.click(within(recordsSection!).getByRole('button', { name: '查看记录' }))

    const dialog = await screen.findByRole('dialog', { name: '资产组合决策记录详情' })
    expect(within(dialog).getAllByText('证据仍未补齐，先补上下文再确认判断').length).toBeGreaterThan(0)
    const subscriptionLinks = within(dialog).getAllByRole('link', { name: '核对订阅上下文' })
    expect(subscriptionLinks[0]).toHaveAttribute('href', '/subscriptions?vps_id=vps_primary')
    const writeCalls = fetchMock.mock.calls.filter((call) => call[1]?.method && call[1]?.method !== 'GET')
    expect(writeCalls).toEqual([])
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

    await waitFor(() => expect(screen.getAllByText('Tokyo Review').length).toBeGreaterThan(0))
    const singleQueue = screen.getByRole('heading', { name: '单台待处理队列' }).closest('section')
    expect(singleQueue).not.toBeNull()
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
    expect(screen.getByRole('heading', { name: '续费候选不可用' })).toBeInTheDocument()
    expect(screen.getAllByText(/subscription evidence unavailable/).length).toBeGreaterThan(0)
    const groupList = screen.getByRole('heading', { name: '决策组列表' }).closest('section')
    expect(groupList).not.toBeNull()
    expect(within(groupList!).queryByText('缺订阅')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '单台队列不可用' })).not.toBeInTheDocument()
  })
})

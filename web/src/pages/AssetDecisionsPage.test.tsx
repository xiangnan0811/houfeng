import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AssetDecisionsPage } from './AssetDecisionsPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
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

function mockInitialWorkbench(fetchMock: ReturnType<typeof vi.fn>, options: {
  overviewBody?: unknown
  groupsBody?: unknown
  recordsBody?: unknown
  renewalEvidenceBody?: unknown
  subscriptionsBody?: unknown
  unreviewedBody?: unknown
  migrateBody?: unknown
  cancelBody?: unknown
} = {}) {
  fetchMock
    .mockResolvedValueOnce(mockJSONResponse(options.overviewBody ?? overview()))
    .mockResolvedValueOnce(mockJSONResponse(options.groupsBody ?? [groupSummary()]))
    .mockResolvedValueOnce(mockJSONResponse(options.recordsBody ?? [decisionRecord()]))
    .mockResolvedValueOnce(mockJSONResponse(options.renewalEvidenceBody ?? [subscription]))
    .mockResolvedValueOnce(mockJSONResponse(options.subscriptionsBody ?? [subscription]))
    .mockResolvedValueOnce(mockJSONResponse(options.unreviewedBody ?? [vps]))
    .mockResolvedValueOnce(mockJSONResponse(options.migrateBody ?? [migrateVPS]))
    .mockResolvedValueOnce(mockJSONResponse(options.cancelBody ?? [cancelVPS]))
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
    expect(screen.getByRole('heading', { name: '决策组列表' })).toBeInTheDocument()
    expect(screen.getByText('德国主力组合')).toBeInTheDocument()
    expect(screen.getByText('判断尺度')).toBeInTheDocument()
    expect(screen.getAllByText('证据强').length).toBeGreaterThan(0)
    expect(screen.getByRole('heading', { name: '已保存组合决策' })).toBeInTheDocument()
    expect(screen.getByText('德国主备取舍记录')).toBeInTheDocument()
    expect(screen.getByText('RENEWAL EVIDENCE')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '单台待处理队列' })).toBeInTheDocument()
    expect(screen.getAllByText('Tokyo Review').length).toBeGreaterThan(0)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/asset-decisions/overview?view=needs_decision&renew_within_days=30', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/asset-decisions/groups?view=needs_decision&renew_within_days=30', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/asset-decisions/records', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
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
    mockInitialWorkbench(fetchMock)
    fetchMock
      .mockResolvedValueOnce(mockJSONResponse(overview({ region_group_count: 1 })))
      .mockResolvedValueOnce(mockJSONResponse([regionGroup]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('德国主力组合')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('tab', { name: /同区比较/ }))

    await waitFor(() => expect(screen.getByText('日本同区取舍')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/asset-decisions/overview?view=region&renew_within_days=30', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/asset-decisions/groups?view=region&renew_within_days=30', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('opens group detail with member comparison, evidence, and single VPS entry', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock)
    fetchMock.mockResolvedValueOnce(mockJSONResponse(groupDetail()))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('德国主力组合')).toBeInTheDocument())
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
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/asset-decisions/groups/adg_auto_001?renew_within_days=30', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })

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
    mockInitialWorkbench(fetchMock)
    fetchMock
      .mockResolvedValueOnce(mockJSONResponse(groupDetail()))
      .mockResolvedValueOnce(mockJSONResponse(created, 201))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('德国主力组合')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '查看组' }))
    const dialog = await screen.findByRole('dialog', { name: '资产决策组详情' })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存为决策记录' }))
    fireEvent.change(within(dialog).getByLabelText('标题'), { target: { value: '德国主备取舍' } })
    fireEvent.change(within(dialog).getByLabelText('状态'), { target: { value: 'decided' } })
    fireEvent.change(within(dialog).getByLabelText('组合目标'), { target: { value: '保留主力，补齐备用证据' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存记录' }))

    await waitFor(() => expect(screen.getByText('已保存组合决策记录：德国主备取舍')).toBeInTheDocument())
    expect(await screen.findByRole('dialog', { name: '资产组合决策记录详情' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/asset-decisions/records', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
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
    })
  })

  it('opens saved decision records and patches record status', async () => {
    const patched = decisionRecord({
      status: 'in_progress',
      updated_at: '2026-06-05T09:00:00Z',
      decided_at: '2026-06-05T09:00:00Z',
    })
    const followupPatched = decisionRecord({
      status: 'in_progress',
      followup_todo_count: 1,
      followup_blocked_count: 1,
      updated_at: '2026-06-05T09:10:00Z',
      decided_at: '2026-06-05T09:00:00Z',
      members: [
        {
          ...decisionRecord().members[0],
          followup_status: 'blocked',
          followup_note: '等待迁移窗口',
          followup_updated_at: '2026-06-05T09:10:00Z',
        },
      ],
    })
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock)
    fetchMock
      .mockResolvedValueOnce(mockJSONResponse(decisionRecord()))
      .mockResolvedValueOnce(mockJSONResponse(patched))
      .mockResolvedValueOnce(mockJSONResponse(followupPatched))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('德国主备取舍记录')).toBeInTheDocument())
    const recordsSection = screen.getByRole('heading', { name: '已保存组合决策' }).closest('section')
    expect(recordsSection).not.toBeNull()
    fireEvent.click(within(recordsSection!).getByRole('button', { name: '查看记录' }))

    const dialog = await screen.findByRole('dialog', { name: '资产组合决策记录详情' })
    expect(within(dialog).getByText('主力保留')).toBeInTheDocument()
    expect(within(dialog).getByText('证据快照')).toBeInTheDocument()
    expect(within(dialog).getAllByText('可决策').length).toBeGreaterThan(0)
    fireEvent.change(within(dialog).getByLabelText('推进状态'), { target: { value: 'in_progress' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '更新状态' }))

    await waitFor(() => expect(screen.getByText('决策记录状态已更新：德国主备取舍记录 -> 推进中')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/asset-decisions/records/adr_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(10, '/api/asset-decisions/records/adr_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ status: 'in_progress' }),
    })

    fireEvent.change(within(dialog).getByLabelText('Germany Primary 跟进状态'), { target: { value: 'blocked' } })
    fireEvent.change(within(dialog).getByLabelText('Germany Primary 跟进备注'), { target: { value: '等待迁移窗口' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存跟进' }))

    await waitFor(() => expect(screen.getByText('成员跟进已更新：Germany Primary -> 阻塞')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(11, '/api/asset-decisions/records/adr_001', {
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
    })
    expect(within(dialog).getByLabelText('Germany Primary 跟进备注')).toHaveValue('等待迁移窗口')
  })

  it('keeps the single VPS renewal decision PATCH payload unchanged', async () => {
    const updated = {
      ...vps,
      renewal_decision: 'migrate',
      updated_at: '2026-05-09T09:00:00Z',
    }
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock)
    fetchMock.mockResolvedValueOnce(mockJSONResponse(updated))
    mockInitialWorkbench(fetchMock, {
      overviewBody: overview(),
      groupsBody: [groupSummary()],
      unreviewedBody: [],
      migrateBody: [updated, migrateVPS],
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
    expect(fetchMock).toHaveBeenNthCalledWith(9, '/api/vps/vps_review', {
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
    })
  })

  it('does not turn subscription evidence failure into missing-subscription decisions', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce(mockJSONResponse(overview()))
      .mockResolvedValueOnce(mockJSONResponse([groupSummary({ evidence_chips: [] })]))
      .mockResolvedValueOnce(mockJSONResponse([decisionRecord()]))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'subscription evidence unavailable' }, 500))
      .mockResolvedValueOnce(mockJSONResponse([subscription]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('德国主力组合')).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '续费候选不可用' })).toBeInTheDocument()
    expect(screen.getAllByText(/subscription evidence unavailable/).length).toBeGreaterThan(0)
    const groupList = screen.getByRole('heading', { name: '决策组列表' }).closest('section')
    expect(groupList).not.toBeNull()
    expect(within(groupList!).queryByText('缺订阅')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '单台队列不可用' })).not.toBeInTheDocument()
  })
})

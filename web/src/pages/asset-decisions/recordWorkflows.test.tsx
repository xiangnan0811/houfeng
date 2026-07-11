import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AssetDecisionsPage } from '../AssetDecisionsPage'
import {
  sourceAvailability,
  evidenceAssessment,
  groupDetail,
  recordReadback,
  memberReadback,
  recordExecutionPlan,
  memberExecutionPlan,
  decisionRecord,
  decisionRecordWithManyMembers,
  mockInitialWorkbench,
  expectFetchCalledWith,
  openSecondaryWorkbench,
  expectAutomaticGroupDefaultCover,
  expectSavedRecordDefaultCover,
  expectTaskPanelDensity,
  expectNoDetailCoverWhileInTaskPanel,
  expectSavedRecordMembersPanelIsCompact,
  openSavedRecordRawMembersPanel,
} from './testFixtures'

describe('Asset Decisions saved record workflows', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('restores focus to the opened record trigger after Escape closes the detail', async () => {
    const fetchMock = vi.fn()
    mockInitialWorkbench(fetchMock, {
      routes: [
        { url: '/api/asset-decisions/records/adr_001', body: decisionRecord() },
      ],
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter>
        <AssetDecisionsPage />
      </MemoryRouter>,
    )

    await openSecondaryWorkbench('保存记录')
    const recordsSection = (await screen.findByRole('heading', { name: '已保存组合决策' })).closest('section')
    const trigger = within(recordsSection!).getByRole('button', { name: '查看' })
    trigger.focus()
    fireEvent.click(trigger)

    expect(await screen.findByRole('dialog', { name: '德国主备取舍记录' })).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: '德国主备取舍记录' })).not.toBeInTheDocument()
      expect(within(recordsSection!).getByRole('button', { name: '查看' })).toHaveFocus()
    })
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
})

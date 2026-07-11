import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AssetDecisionsPage } from '../AssetDecisionsPage'
import {
  comparisonInsight,
  groupSummary,
  groupDetail,
  decisionRecord,
  manualGroupDetail,
  groupDetailWithManyMembers,
  mockInitialWorkbench,
  expectFetchCalledWith,
  findFetchCall,
  fetchRequestInventory,
  expectTabPanelRelationship,
  expectAutomaticGroupDefaultCover,
  openAutomaticGroupMembers,
  expectAutomaticGroupMembersPanelIsCompact,
  expectAutomaticSavePanelIsBrief,
  openManualGroupMembers,
  expectTaskPanelDensity,
  expectNoDetailCoverWhileInTaskPanel,
} from './testFixtures'

describe('Asset Decisions automatic group workflows', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('restores focus to the opened group trigger after Escape closes the detail', async () => {
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

    const trigger = await screen.findByRole('button', { name: '查看组' })
    trigger.focus()
    fireEvent.click(trigger)

    expect(await screen.findByRole('dialog', { name: '资产决策组详情' })).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: '资产决策组详情' })).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: '查看组' })).toHaveFocus()
    })
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
})

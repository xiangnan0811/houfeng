import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AssetDecisionsPage } from '../AssetDecisionsPage'
import {
  comparisonInsight,
  decisionRecord,
  manualGroupDetail,
  manualGroupDetailWithManyMembers,
  manualGroupSummary,
  scenarioTemplate,
  mockInitialWorkbench,
  findFetchCall,
  openSecondaryWorkbench,
  openManualGroupMembers,
  expectTemplateDefaultCover,
  expectSavedRecordDefaultCover,
  expectDecisionCoverDensity,
} from './testFixtures'

describe('Asset Decisions manual group workflows', () => {
  afterEach(() => {
    vi.restoreAllMocks()
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
})

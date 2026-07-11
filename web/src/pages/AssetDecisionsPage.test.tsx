import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AssetDecisionsPage } from './AssetDecisionsPage'
import {
  LocationProbe,
  HistoryControls,
  groupSummary,
  overview,
  groupDetail,
  recordReadback,
  recordExecutionPlan,
  decisionRecord,
  manualGroupDetail,
  manualGroupSummary,
  scenarioTemplate,
  type MockFetchRoute,
  mockInitialWorkbench,
  expectFetchCalledWith,
  fetchRequestInventory,
  getSecondaryWorkbenchButton,
  expectTabPanelRelationship,
  findSecondaryWorkbenchButton,
  expectAutomaticGroupDefaultCover,
  openAutomaticGroupMembers,
  normalizedText,
  expectNoAssetDecisionPageEnglishNoise,
} from './asset-decisions/testFixtures'

describe('Asset Decisions route and composition workflows', () => {
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
})

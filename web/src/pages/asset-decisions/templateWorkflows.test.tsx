import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AssetDecisionsPage } from '../AssetDecisionsPage'
import {
  LocationProbe,
  manualGroupDetail,
  scenarioTemplate,
  mockInitialWorkbench,
  findFetchCall,
  openSecondaryWorkbench,
  expectTemplateDefaultCover,
} from './testFixtures'

describe('Asset Decisions scenario template workflows', () => {
  afterEach(() => {
    vi.restoreAllMocks()
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
})

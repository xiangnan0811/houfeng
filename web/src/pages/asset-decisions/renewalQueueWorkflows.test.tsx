import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AssetDecisionsPage } from '../AssetDecisionsPage'
import {
  vps,
  groupSummary,
  mockInitialWorkbench,
  findFetchCall,
  fetchRequestInventory,
  expectTabPanelRelationship,
  openSecondaryWorkbench,
} from './testFixtures'

describe('Asset Decisions renewal queue workflows', () => {
  afterEach(() => {
    vi.restoreAllMocks()
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

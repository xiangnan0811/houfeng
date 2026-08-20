import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as recordsApi from '../lib/recordsApi'
import type { SubjectActivityListResponse } from '../lib/types'
import { SubjectActivityPage } from './SubjectActivityPage'

function mockPage(overrides: Partial<SubjectActivityListResponse> = {}): SubjectActivityListResponse {
  return {
    subject: {
      kind: 'vps',
      source_id: 'vps_001',
      identity: { display_name: '东京边缘' },
      live_route: '/vps/vps_001',
      status: 'live',
    },
    view: 'activity',
    snapshot_cursor: 'snap',
    freshness: {
      state: 'ready',
      visible_observed_at: null,
      new_items_available: false,
      reason_code: '',
    },
    items: [{
      activity_id: 'act_1',
      event_kind: 'record_created',
      event_at: '2026-08-10T08:00:00Z',
      recorded_at: '2026-08-10T08:00:01Z',
      source_kind: 'record_domain',
      backfilled: false,
      subjects: [],
      presentation: { version: 1, title: '首条记录' },
      record_id: 'rec_001',
      revision_id: 'rrv_001',
    }],
    source_statuses: [],
    ...overrides,
  }
}

function renderPage(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/vps/:vpsId/activity" element={<SubjectActivityPage />} />
        <Route path="/monitoring/:monitoringInstanceId/activity" element={<SubjectActivityPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('SubjectActivityPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads activity for a VPS subject and offers a preselected new-record link', async () => {
    vi.spyOn(recordsApi, 'listSubjectActivity').mockResolvedValue(mockPage())

    renderPage('/vps/vps_001/activity')

    await waitFor(() => expect(screen.getByText('首条记录')).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '新建记录' })).toHaveAttribute(
      'href',
      expect.stringContaining('/records/new?subject=vps%3Avps_001%3Aaffected%3Aprimary'),
    )
    expect(screen.getByRole('link', { name: '活动' })).toHaveAttribute('aria-current', 'page')
  })

  it('shows loading then empty for a subject with no events', async () => {
    vi.spyOn(recordsApi, 'listSubjectActivity').mockResolvedValue(mockPage({ items: [] }))

    renderPage('/vps/vps_001/activity')
    expect(screen.getByText('正在加载活动')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText('主体尚无活动')).toBeInTheDocument())
  })

  it('surfaces single-source degradation without claiming completeness', async () => {
    vi.spyOn(recordsApi, 'listSubjectActivity').mockResolvedValue(mockPage({
      source_statuses: [
        { source_kind: 'command_audit', state: 'unavailable', reason_code: 'adapter_error' },
      ],
    }))

    renderPage('/vps/vps_001/activity')
    await waitFor(() => expect(screen.getByText(/部分来源暂不可用/)).toBeInTheDocument())
  })

  it('shows refresh when authorized-scope new items are available', async () => {
    vi.spyOn(recordsApi, 'listSubjectActivity').mockResolvedValue(mockPage({
      freshness: {
        state: 'ready',
        visible_observed_at: null,
        new_items_available: true,
        reason_code: '',
      },
    }))

    renderPage('/monitoring/mi_001/activity')
    await waitFor(() => expect(screen.getByRole('button', { name: '有新活动，刷新' })).toBeInTheDocument())
  })

  it('renders tombstoned identity', async () => {
    vi.spyOn(recordsApi, 'listSubjectActivity').mockResolvedValue(mockPage({
      subject: {
        kind: 'vps',
        source_id: 'vps_001',
        identity: { display_name: '已删除 VPS' },
        status: 'tombstoned',
      },
    }))

    renderPage('/vps/vps_001/activity')
    await waitFor(() => expect(screen.getByText('已删除主体')).toBeInTheDocument())
  })
})

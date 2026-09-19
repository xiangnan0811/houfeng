import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as recordsApi from '../lib/recordsApi'
import type { SubjectActivityListResponse } from '../lib/types'
import { SubjectActivityPage } from './SubjectActivityPage'
import { SubjectEvidencePage } from './SubjectEvidencePage'

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

  it('carries validated return_vps on nested return and local nav without copying other query', async () => {
    vi.spyOn(recordsApi, 'listSubjectActivity').mockResolvedValue(mockPage({
      subject: {
        kind: 'monitoring_instance',
        source_id: 'mi_001',
        identity: { display_name: 'Tokyo Edge' },
        live_route: '/monitoring/mi_001',
        status: 'live',
      },
    }))

    renderPage('/monitoring/mi_001/activity?return_vps=vps_tokyo_origin&window=7d')

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument())
    expect(screen.getByRole('link', { name: '返回详情' })).toHaveAttribute(
      'href',
      '/monitoring/mi_001?return_vps=vps_tokyo_origin',
    )
    expect(screen.getByRole('link', { name: '记录' })).toHaveAttribute(
      'href',
      '/monitoring/mi_001/records?return_vps=vps_tokyo_origin',
    )
    expect(screen.getByRole('link', { name: '记录' }).getAttribute('href')).not.toContain('window=')
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

  it('clears the cursor when a real filter changes', async () => {
    const list = vi.spyOn(recordsApi, 'listSubjectActivity').mockResolvedValue(mockPage())

    renderPage('/vps/vps_001/activity?cursor=opaque-cursor')
    await waitFor(() => expect(screen.getByText('首条记录')).toBeInTheDocument())
    expect(list).toHaveBeenCalledWith('vps', 'vps_001', { cursor: 'opaque-cursor' })

    fireEvent.change(screen.getByLabelText('来源'), { target: { value: 'command_audit' } })
    await waitFor(() => expect(list).toHaveBeenCalledWith('vps', 'vps_001', { source: ['command_audit'] }))
  })

  it('lets evidence clear an activity event-kind carried across tabs so results render', async () => {
    const list = vi.spyOn(recordsApi, 'listSubjectActivity').mockImplementation(async (_kind, _id, query = {}) => {
      if (query.view === 'evidence' && !query.event_kind?.length) {
        return mockPage({
          view: 'evidence',
          items: [{
            activity_id: 'act_e',
            event_kind: 'evidence_captured',
            event_at: '2026-08-10T08:00:00Z',
            recorded_at: '2026-08-10T08:00:01Z',
            source_kind: 'evidence_snapshot',
            backfilled: false,
            subjects: [],
            presentation: { version: 1, title: '探针证据' },
            evidence_snapshot_id: 'evs_9',
          }],
        })
      }
      if (query.view === 'evidence') {
        return mockPage({ view: 'evidence', items: [] })
      }
      return mockPage()
    })

    render(
      <MemoryRouter initialEntries={['/vps/vps_001/activity']}>
        <Routes>
          <Route path="/vps/:vpsId/activity" element={<SubjectActivityPage />} />
          <Route path="/vps/:vpsId/evidence" element={<SubjectEvidencePage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('首条记录')).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText('事件类型'), { target: { value: 'command_executed' } })
    await waitFor(() => expect(list).toHaveBeenCalledWith('vps', 'vps_001', { event_kind: ['command_executed'] }))

    fireEvent.click(screen.getByRole('link', { name: '证据' }))
    await waitFor(() => expect(list).toHaveBeenCalledWith('vps', 'vps_001', {
      view: 'evidence',
      event_kind: ['command_executed'],
    }))
    await waitFor(() => expect(screen.getByLabelText('事件类型')).toBeEnabled())
    expect(screen.getByLabelText('事件类型')).toHaveValue('command_executed')
    expect(screen.getByRole('option', { name: '命令执行' })).toBeDisabled()
    expect(screen.getByRole('option', { name: '证据捕获' })).not.toBeDisabled()

    fireEvent.change(screen.getByLabelText('事件类型'), { target: { value: '' } })
    await waitFor(() => expect(list).toHaveBeenCalledWith('vps', 'vps_001', { view: 'evidence' }))
    await waitFor(() => expect(screen.getByText('探针证据')).toBeInTheDocument())
  })

})

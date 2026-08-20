import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as recordsApi from '../lib/recordsApi'
import type { SubjectActivityListResponse } from '../lib/types'
import { SubjectRecordsPage } from './SubjectRecordsPage'

function mockPage(): SubjectActivityListResponse {
  return {
    subject: {
      kind: 'vps',
      source_id: 'vps_001',
      identity: { display_name: '东京边缘' },
      status: 'live',
    },
    view: 'records',
    snapshot_cursor: 'snap',
    freshness: {
      state: 'ready',
      visible_observed_at: null,
      new_items_available: false,
      reason_code: '',
    },
    items: [{
      activity_id: 'act_r',
      event_kind: 'record_revised',
      event_at: '2026-08-10T08:00:00Z',
      recorded_at: '2026-08-10T08:00:01Z',
      source_kind: 'record_domain',
      backfilled: false,
      subjects: [],
      presentation: { version: 1, title: '修订记录' },
      record_id: 'rec_001',
      revision_id: 'rrv_002',
    }],
    source_statuses: [],
  }
}

describe('SubjectRecordsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('requests the records view and highlights local nav', async () => {
    const list = vi.spyOn(recordsApi, 'listSubjectActivity').mockResolvedValue(mockPage())

    render(
      <MemoryRouter initialEntries={['/vps/vps_001/records']}>
        <Routes>
          <Route path="/vps/:vpsId/records" element={<SubjectRecordsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('修订记录')).toBeInTheDocument())
    expect(list).toHaveBeenCalledWith('vps', 'vps_001', { view: 'records' })
    expect(screen.getByRole('link', { name: '记录' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: '查看修订' })).toHaveAttribute(
      'href',
      '/records/rec_001/revisions/rrv_002',
    )
  })
})

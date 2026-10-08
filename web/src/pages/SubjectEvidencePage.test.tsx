import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import * as authContext from '../lib/auth-context'
import * as recordsApi from '../lib/recordsApi'
import type { SubjectActivityListResponse } from '../lib/types'
import { SubjectEvidencePage } from './SubjectEvidencePage'

function mockPage(): SubjectActivityListResponse {
  return {
    subject: {
      kind: 'target',
      source_id: 'tg_001',
      identity: { display_name: 'Blog' },
      status: 'live',
    },
    view: 'evidence',
    snapshot_cursor: 'snap',
    freshness: {
      state: 'ready',
      visible_observed_at: null,
      new_items_available: false,
      reason_code: '',
    },
    items: [{
      activity_id: 'act_e',
      event_kind: 'evidence_captured',
      event_at: '2026-08-10T08:00:00Z',
      recorded_at: '2026-08-10T08:00:01Z',
      source_kind: 'evidence_snapshot',
      backfilled: false,
      subjects: [{
        kind: 'target',
        source_id: 'tg_001',
        role: 'affected',
        primary: true,
        identity: { coverage: 'probe', bucket: '5m', quality: 'high' },
        tombstoned: false,
      }],
      presentation: { version: 1, title: '探针证据' },
      evidence_snapshot_id: 'evs_9',
    }],
    source_statuses: [],
  }
}

describe('SubjectEvidencePage', () => {
  beforeEach(() => {
    vi.spyOn(authContext, 'useAuth').mockReturnValue({
      user: {
        user_id: 'u1',
        username: 'admin',
        role: 'admin',
        display_name: '',
        runtime_capabilities: { records: true, comparison: true, portability: true },
        management_capabilities: { access: false },
      },
      loading: false,
      status: 'ready',
      error: null,
      login: vi.fn(),
      logout: vi.fn(),
      refresh: vi.fn(),
      retry: vi.fn(),
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('requests the evidence view and shows immutable evidence rows', async () => {
    const list = vi.spyOn(recordsApi, 'listSubjectActivity').mockResolvedValue(mockPage())

    render(
      <MemoryRouter initialEntries={['/targets/tg_001/evidence']}>
        <Routes>
          <Route path="/targets/:targetId/evidence" element={<SubjectEvidencePage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('探针证据')).toBeInTheDocument())
    expect(list).toHaveBeenCalledWith('target', 'tg_001', { view: 'evidence' })
    expect(screen.getByText('不可变证据')).toBeInTheDocument()
    expect(screen.getByText(/覆盖 probe/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看证据' })).toHaveAttribute(
      'href',
      '/evidence/evs_9',
    )
    expect(screen.getByRole('link', { name: '横向比较' })).toHaveAttribute('href', '/records/compare')
    expect(screen.getByRole('link', { name: '加入横向比较' }).getAttribute('href')).toMatch(
      /^\/records\/compare\?state=/,
    )
  })

  it('keeps evidence rows and hides comparison entry points when comparison is off', async () => {
    const list = vi.spyOn(recordsApi, 'listSubjectActivity').mockResolvedValue(mockPage())
    vi.spyOn(authContext, 'useAuth').mockReturnValue({
      user: {
        user_id: 'u1',
        username: 'admin',
        role: 'admin',
        display_name: '',
        runtime_capabilities: { records: true, comparison: false, portability: true },
        management_capabilities: { access: false },
      },
      loading: false,
      status: 'ready',
      error: null,
      login: vi.fn(),
      logout: vi.fn(),
      refresh: vi.fn(),
      retry: vi.fn(),
    })

    render(
      <MemoryRouter initialEntries={['/targets/tg_001/evidence']}>
        <Routes>
          <Route path="/targets/:targetId/evidence" element={<SubjectEvidencePage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(list).toHaveBeenCalledWith('target', 'tg_001', { view: 'evidence' }))
    expect(screen.getByText('探针证据')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '新建记录' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '横向比较' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '加入横向比较' })).not.toBeInTheDocument()
  })
})

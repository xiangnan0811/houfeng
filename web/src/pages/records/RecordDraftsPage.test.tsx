import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { RecordDraft } from '../../lib/types'
import { RecordDraftsPage } from './RecordDraftsPage'

function mockJSONResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

function draft(overrides: Partial<RecordDraft> = {}): RecordDraft {
  return {
    draft_id: 'rdf_001',
    version: 2,
    etag: '"rdf_001:2"',
    warning_at: '2026-08-16T08:00:00Z',
    created_at: '2026-08-09T08:00:00Z',
    updated_at: '2026-08-09T09:30:00Z',
    expires_at: '2026-08-23T08:00:00Z',
    payload: {
      title: '东京节点扩容草稿',
      body_markdown: '待补充容量数据。',
      markdown_dialect_version: 1,
      record_type: 'maintenance',
      business_status: 'planned',
      impact_level: 'low',
      visibility: { kind: 'project', allowed_roles: [], allowed_group_ids: [] },
      subjects: [],
      tags: [],
      attachment_ids: [],
      owner_id: '',
      participant_ids: [],
      save_reason: '',
    },
    ...overrides,
  }
}

function renderPage() {
  render(
    <MemoryRouter initialEntries={['/records/drafts']}>
      <Routes>
        <Route path="/records/drafts" element={<RecordDraftsPage />} />
        <Route path="/records/new" element={<output aria-label="新建路由">new</output>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('RecordDraftsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('lists drafts with the title and destination each one would publish to', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse({
      items: [
        draft(),
        draft({
          draft_id: 'rdf_002',
          record_id: 'rec_777',
          base_revision_id: 'rev_777',
          payload: { ...draft().payload, title: '' },
        }),
      ],
    }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    expect(screen.getByRole('heading', { name: '记录草稿' })).toBeInTheDocument()
    await screen.findByText('东京节点扩容草稿')
    expect(fetchMock).toHaveBeenCalledWith('/api/record-drafts', expect.objectContaining({
      credentials: 'include',
    }))
    const table = screen.getByRole('table', { name: '记录草稿列表' })
    expect(within(table).getByText('新建记录')).toBeInTheDocument()
    expect(within(table).getByText('rec_777')).toBeInTheDocument()
    expect(within(table).getByText('未命名草稿')).toBeInTheDocument()
  })

  it('pages with the cursor and stops offering more at the end', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ items: [draft()], next_cursor: 'cursor-2' }))
      .mockResolvedValueOnce(mockJSONResponse({
        items: [draft({
          draft_id: 'rdf_003',
          payload: { ...draft().payload, title: '第二页草稿' },
        })],
      }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await screen.findByText('东京节点扩容草稿')

    fireEvent.click(screen.getByRole('button', { name: '加载更多' }))

    await screen.findByText('第二页草稿')
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/record-drafts?cursor=cursor-2',
      expect.any(Object),
    )
    expect(screen.getByText('东京节点扩容草稿')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument()
  })

  it('discards a draft and drops it from the list without a full reload', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({
        items: [
          draft(),
          draft({
            draft_id: 'rdf_004',
            payload: { ...draft().payload, title: '保留下来的草稿' },
          }),
        ],
      }))
      .mockResolvedValueOnce(mockJSONResponse({}, 204))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await screen.findByText('东京节点扩容草稿')
    const table = screen.getByRole('table', { name: '记录草稿列表' })
    expect(within(table).getAllByRole('row')).toHaveLength(3)

    fireEvent.click(within(table).getAllByRole('button', { name: '丢弃' })[0]!)

    await waitFor(() => expect(within(table).getAllByRole('row')).toHaveLength(2))
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/record-drafts/rdf_001',
      expect.objectContaining({ method: 'DELETE' }),
    )
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('keeps the row and reports the reason when discarding fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ items: [draft()] }))
      .mockResolvedValueOnce(mockJSONResponse({
        code: 'draft_conflict', message: 'draft changed',
      }, 409))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await screen.findByText('东京节点扩容草稿')

    fireEvent.click(screen.getByRole('button', { name: '丢弃' }))

    await screen.findByRole('alert')
    expect(screen.getByRole('alert')).toHaveTextContent('draft changed')
    expect(screen.getByText('东京节点扩容草稿')).toBeInTheDocument()
  })

  it('shows an empty state that points at creating a record', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse({ items: [] }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await screen.findByText('没有未发布的草稿')
    expect(screen.getByRole('link', { name: '新建记录' })).toHaveAttribute('href', '/records/new')
  })

  it('offers a retry when the draft list fails to load', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ code: 'internal_error', message: '服务不可用' }, 500))
      .mockResolvedValueOnce(mockJSONResponse({ items: [draft()] }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await screen.findByText('草稿列表不可用')
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await screen.findByText('东京节点扩容草稿')
  })
})

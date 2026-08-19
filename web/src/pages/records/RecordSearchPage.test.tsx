import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { RecordDetail } from '../../lib/types'
import { RecordSearchPage } from './RecordSearchPage'

function mockJSONResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

function searchResult(overrides: Partial<RecordDetail> = {}): RecordDetail {
  return {
    record_id: 'rec_001',
    lifecycle: 'active',
    current_revision_id: 'rev_001',
    lock_version: 3,
    authorization_epoch: 1,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-09T08:30:00Z',
    capabilities: {
      read: true, update: true, archive: true, restore: false, draft: true, permanent_delete: false,
    },
    current: {
      record_id: 'rec_001',
      revision_id: 'rev_001',
      revision_no: 3,
      title: '东京节点磁盘 IO 抖动',
      body_markdown: '磁盘队列在高峰时段堆积。',
      markdown_dialect_version: 1,
      record_type: 'troubleshooting',
      business_status: 'investigating',
      status_group: 'in_progress',
      impact_level: 'medium',
      occurred_at: '2026-08-08T22:10:00Z',
      visibility: { kind: 'project', allowed_roles: [], allowed_group_ids: [] },
      subjects: [{
        registry_version: 1,
        kind: 'vps',
        source_id: 'vps_alpha',
        role: 'affected',
        primary: true,
        identity: { display_name: 'Tokyo Edge' },
      }],
      tags: ['disk', 'io'],
      attachment_ids: [],
      participants: [],
      author_id: 'usr_000000000000000000000001',
      save_reason: '初始记录',
      created_at: '2026-08-09T08:30:00Z',
    },
    ...overrides,
  }
}

function LocationProbe() {
  const location = useLocation()
  return <output aria-label="当前查询参数">{location.search}</output>
}

function renderPage(initialEntry = '/records') {
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route
          path="/records"
          element={(
            <>
              <RecordSearchPage />
              <LocationProbe />
            </>
          )}
        />
        <Route path="/records/:recordId" element={<output aria-label="详情路由">detail</output>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('RecordSearchPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('searches without parameters on first load and renders a result row', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      mockJSONResponse({ items: [searchResult()], generation: 7 }),
    )
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    expect(screen.getByRole('heading', { name: '运维记录' })).toBeInTheDocument()
    await screen.findByText('东京节点磁盘 IO 抖动')
    expect(fetchMock).toHaveBeenCalledWith('/api/records/search', expect.objectContaining({
      credentials: 'include',
    }))
    // The filter panel also offers 排障 as an option, so scope to the results.
    const results = screen.getByRole('table', { name: '记录搜索结果' })
    expect(within(results).getByText('排障')).toBeInTheDocument()
    expect(within(results).getByText('排查中')).toBeInTheDocument()
    expect(within(results).getByText('VPS · Tokyo Edge')).toBeInTheDocument()
    expect(screen.getByLabelText('当前查询参数')).toHaveTextContent('')
  })

  it('sends the filters carried by the URL', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse({ items: [], generation: 7 }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/records?q=disk&type=troubleshooting&type=migration&tag=io&lifecycle=archived')

    await screen.findByText('没有匹配的记录')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/records/search?q=disk&type=troubleshooting&type=migration&lifecycle=archived&tag=io',
      expect.any(Object),
    )
  })

  it('rewrites an unusable URL filter instead of asking the server to reject it', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse({ items: [], generation: 7 }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/records?type=troubleshooting&type=not_a_type&limit=9000&unknown=1')

    await screen.findByText('没有匹配的记录')
    await waitFor(() => expect(screen.getByLabelText('当前查询参数')).toHaveTextContent(
      '?type=troubleshooting',
    ))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/records/search?type=troubleshooting',
      expect.any(Object),
    )
  })

  it('applies the text query through canonical URL state', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({ items: [], generation: 7 }))
      .mockResolvedValueOnce(mockJSONResponse({ items: [searchResult()], generation: 7 }))
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('没有匹配的记录')

    fireEvent.change(screen.getByLabelText('关键词'), { target: { value: '  磁盘 IO  ' } })
    fireEvent.change(screen.getByLabelText('记录类型'), { target: { value: 'troubleshooting' } })
    fireEvent.click(screen.getByRole('button', { name: '搜索' }))

    await screen.findByText('东京节点磁盘 IO 抖动')
    await waitFor(() => expect(screen.getByLabelText('当前查询参数')).toHaveTextContent(
      '?q=%E7%A3%81%E7%9B%98+IO&type=troubleshooting',
    ))
  })

  it('repeats the filters with the cursor, because a cursor is bound to its query', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({
        items: [searchResult()], next_cursor: 'cursor-2', generation: 7,
      }))
      .mockResolvedValueOnce(mockJSONResponse({
        items: [searchResult({
          record_id: 'rec_002',
          current: { ...searchResult().current, record_id: 'rec_002', title: '第二页记录' },
        })],
        generation: 7,
      }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/records?q=disk')
    await screen.findByText('东京节点磁盘 IO 抖动')

    fireEvent.click(screen.getByRole('button', { name: '加载更多' }))

    await screen.findByText('第二页记录')
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/records/search?q=disk&cursor=cursor-2',
      expect.any(Object),
    )
    expect(screen.getByText('东京节点磁盘 IO 抖动')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument()
  })

  it('restarts from the first page when the index was republished under a cursor', async () => {
    // The cursor is bound to one index generation. A republished index makes it
    // unusable, and the only honest recovery is to re-read from the top rather
    // than stitch pages from two different generations together.
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({
        items: [searchResult()], next_cursor: 'cursor-2', generation: 7,
      }))
      .mockResolvedValueOnce(mockJSONResponse({
        code: 'search_generation_superseded', message: 'search index was republished',
      }, 409))
      .mockResolvedValueOnce(mockJSONResponse({
        items: [searchResult({
          current: { ...searchResult().current, title: '重建后的首页记录' },
        })],
        generation: 8,
      }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/records?q=disk')
    await screen.findByText('东京节点磁盘 IO 抖动')

    fireEvent.click(screen.getByRole('button', { name: '加载更多' }))

    await screen.findByText('重建后的首页记录')
    expect(screen.getByText(/搜索索引已重建/)).toBeInTheDocument()
    expect(fetchMock).toHaveBeenLastCalledWith('/api/records/search?q=disk', expect.any(Object))
    expect(screen.queryByText('东京节点磁盘 IO 抖动')).not.toBeInTheDocument()
  })

  it('offers a retry when the index is unavailable', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse({
        code: 'search_unavailable', message: 'record search is unavailable',
      }, 503))
      .mockResolvedValueOnce(mockJSONResponse({ items: [searchResult()], generation: 7 }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await screen.findByText('记录搜索暂不可用')
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await screen.findByText('东京节点磁盘 IO 抖动')
  })

  it('drops results from a superseded filter while the next search is in flight', async () => {
    let releaseFirst: ((value: Response) => void) | undefined
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => new Promise<Response>((resolve) => { releaseFirst = resolve }))
      .mockResolvedValueOnce(mockJSONResponse({
        items: [searchResult({
          current: { ...searchResult().current, title: '第二次搜索的记录' },
        })],
        generation: 7,
      }))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    fireEvent.change(screen.getByLabelText('关键词'), { target: { value: '磁盘' } })
    fireEvent.click(screen.getByRole('button', { name: '搜索' }))

    await screen.findByText('第二次搜索的记录')
    releaseFirst?.(mockJSONResponse({ items: [searchResult()], generation: 7 }))

    await waitFor(() => expect(screen.queryByText('东京节点磁盘 IO 抖动')).not.toBeInTheDocument())
    expect(screen.getByText('第二次搜索的记录')).toBeInTheDocument()
  })
})

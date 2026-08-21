import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { RecordExportPanel } from './RecordExportPanel'

const api = vi.hoisted(() => ({
  previewRecordExport: vi.fn(),
  createRecordExport: vi.fn(),
  downloadRecordExportContent: vi.fn(),
}))

vi.mock('../../lib/recordsApi', () => api)

describe('RecordExportPanel', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('previews markdown and names unavailable material before download', async () => {
    api.previewRecordExport.mockResolvedValue({
      preview_id: 'rej_1',
      preview_token: 'tok',
      export_kind: 'markdown',
      export_mode: 'safe',
      inventory_digest: 'aa',
      expected_files: [{ name: 'record.md', media_type: 'text/markdown', byte_size: 12 }],
      unavailable: [{ kind: 'evidence', id: 'evs_denied', reason: 'unauthorized' }],
      expires_at: '2026-08-21T13:00:00Z',
    })
    render(<RecordExportPanel recordId="rec_001" snapshotIds={['evs_ok', 'evs_denied']} />)
    fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
    expect(await screen.findByText('evidence evs_denied：unauthorized')).toBeInTheDocument()
    expect(api.previewRecordExport).toHaveBeenCalledWith(
      expect.objectContaining({ record_id: 'rec_001', export_kind: 'markdown' }),
      expect.any(String),
    )
    expect(screen.getByRole('button', { name: '下载' })).toBeEnabled()
  })

  it('downloads after create and does not invent comparison csv', async () => {
    api.previewRecordExport.mockResolvedValue({
      preview_id: 'rej_2',
      preview_token: 'tok',
      export_kind: 'comparison_json',
      export_mode: 'safe',
      inventory_digest: 'bb',
      expected_files: [{ name: 'comparison.result_v1.json', media_type: 'application/json', byte_size: 2 }],
      unavailable: [],
      expires_at: '2026-08-21T13:00:00Z',
    })
    api.createRecordExport.mockResolvedValue({
      export_id: 'rej_2',
      job_state: 'published',
      export_kind: 'comparison_json',
      media_type: 'application/json',
      byte_size: 2,
      expires_at: '2026-08-21T13:00:00Z',
    })
    api.downloadRecordExportContent.mockResolvedValue(new Blob(['{}'], { type: 'application/json' }))
    vi.stubGlobal('URL', {
      createObjectURL: () => 'blob:export',
      revokeObjectURL: () => undefined,
    })
    render(<RecordExportPanel recordId="rec_001" snapshotIds={['evs_comparison01']} />)
    fireEvent.change(screen.getByLabelText('导出类型'), { target: { value: 'comparison_json' } })
    fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
    await screen.findByText('comparison.result_v1.json · 2 字节')
    fireEvent.click(screen.getByRole('button', { name: '下载' }))
    await waitFor(() => expect(api.downloadRecordExportContent).toHaveBeenCalledWith('rej_2'))
    expect(screen.queryByText(/csv/i)).toBeNull()
  })

  it('offers machine archive zip from the record export panel', async () => {
    api.previewRecordExport.mockResolvedValue({
      preview_id: 'rej_3',
      preview_token: 'tok',
      export_kind: 'archive',
      export_mode: 'safe',
      inventory_digest: 'cc',
      expected_files: [{ name: 'houfeng-record-archive-v1.zip', media_type: 'application/zip', byte_size: 64 }],
      unavailable: [{ kind: 'evidence', id: 'evs_denied', reason: 'unauthorized' }],
      expires_at: '2026-08-21T13:00:00Z',
    })
    render(<RecordExportPanel recordId="rec_001" snapshotIds={['evs_comparison01']} />)
    fireEvent.change(screen.getByLabelText('导出类型'), { target: { value: 'archive' } })
    fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
    expect(await screen.findByText('houfeng-record-archive-v1.zip · 64 字节')).toBeInTheDocument()
    expect(api.previewRecordExport).toHaveBeenCalledWith(
      expect.objectContaining({
        record_id: 'rec_001',
        export_kind: 'archive',
        snapshot_id: 'evs_comparison01',
      }),
      expect.any(String),
    )
    expect(screen.queryByText(/csv/i)).toBeNull()
  })

  it('offers derived pdf from the same export panel', async () => {
    api.previewRecordExport.mockResolvedValue({
      preview_id: 'rej_4',
      preview_token: 'tok',
      export_kind: 'pdf',
      export_mode: 'safe',
      inventory_digest: 'dd',
      expected_files: [{ name: 'record.pdf', media_type: 'application/pdf', byte_size: 32 }],
      unavailable: [],
      expires_at: '2026-08-21T13:00:00Z',
    })
    render(<RecordExportPanel recordId="rec_001" />)
    fireEvent.change(screen.getByLabelText('导出类型'), { target: { value: 'pdf' } })
    fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
    expect(await screen.findByText('record.pdf · 32 字节')).toBeInTheDocument()
    expect(api.previewRecordExport).toHaveBeenCalledWith(
      expect.objectContaining({ record_id: 'rec_001', export_kind: 'pdf' }),
      expect.any(String),
    )
  })

  it('invalidates a preview when the export kind changes', async () => {
    api.previewRecordExport.mockResolvedValue({
      preview_id: 'rej_old',
      preview_token: 'tok',
      export_kind: 'markdown',
      export_mode: 'safe',
      inventory_digest: 'aa',
      expected_files: [{ name: 'record.md', media_type: 'text/markdown', byte_size: 12 }],
      unavailable: [],
      expires_at: '2026-08-21T13:00:00Z',
    })
    render(<RecordExportPanel recordId="rec_001" recordLabel="磁盘笔记（rec_001）" />)
    expect(screen.getByText('当前导出：磁盘笔记（rec_001）')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
    expect(await screen.findByText('record.md · 12 字节')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('导出类型'), { target: { value: 'pdf' } })
    expect(screen.queryByText('record.md · 12 字节')).toBeNull()
    expect(screen.getByRole('button', { name: '下载' })).toBeDisabled()
  })

  it('invalidates a preview when the target record changes', async () => {
    api.previewRecordExport.mockResolvedValue({
      preview_id: 'rej_old',
      preview_token: 'tok',
      export_kind: 'markdown',
      export_mode: 'safe',
      inventory_digest: 'aa',
      expected_files: [{ name: 'record.md', media_type: 'text/markdown', byte_size: 12 }],
      unavailable: [],
      expires_at: '2026-08-21T13:00:00Z',
    })
    const { rerender } = render(
      <RecordExportPanel key="rec_001" recordId="rec_001" recordLabel="磁盘笔记（rec_001）" />,
    )
    fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
    expect(await screen.findByText('record.md · 12 字节')).toBeInTheDocument()
    rerender(
      <RecordExportPanel key="rec_002" recordId="rec_002" recordLabel="大阪笔记（rec_002）" />,
    )
    expect(screen.getByText('当前导出：大阪笔记（rec_002）')).toBeInTheDocument()
    expect(screen.queryByText('record.md · 12 字节')).toBeNull()
    expect(screen.getByRole('button', { name: '下载' })).toBeDisabled()
  })

  it('shows a revoked shell after a lease is revoked mid-download', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.previewRecordExport.mockResolvedValue({
      preview_id: 'rej_1',
      preview_token: 'tok',
      export_kind: 'markdown',
      export_mode: 'safe',
      inventory_digest: 'aa',
      expected_files: [{ name: 'record.md', media_type: 'text/markdown', byte_size: 12 }],
      unavailable: [],
      expires_at: '2026-08-21T13:00:00Z',
    })
    api.createRecordExport.mockRejectedValue(new ApiError(409, 'revoked', { code: 'export_lease_revoked' }))
    render(<RecordExportPanel recordId="rec_001" />)
    fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
    expect(await screen.findByText('record.md · 12 字节')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '下载' }))
    expect(await screen.findByRole('heading', { name: '导出访问已撤销' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '下载' })).toBeNull()
  })

  it('shows a deleted shell when a previewed export target disappears', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.previewRecordExport.mockResolvedValue({
      preview_id: 'rej_1',
      preview_token: 'tok',
      export_kind: 'markdown',
      export_mode: 'safe',
      inventory_digest: 'aa',
      expected_files: [{ name: 'record.md', media_type: 'text/markdown', byte_size: 12 }],
      unavailable: [],
      expires_at: '2026-08-21T13:00:00Z',
    })
    api.createRecordExport.mockRejectedValue(new ApiError(404, 'gone', { code: 'resource_not_found' }))
    render(<RecordExportPanel recordId="rec_001" />)
    fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
    expect(await screen.findByText('record.md · 12 字节')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '下载' }))
    expect(await screen.findByRole('heading', { name: '导出目标已删除' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '下载' })).toBeNull()
  })

  it('keeps the form when the first preview is unauthorized', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.previewRecordExport.mockRejectedValue(new ApiError(404, 'missing', { code: 'resource_not_found' }))
    render(<RecordExportPanel recordId="rec_001" />)
    fireEvent.click(screen.getByRole('button', { name: '预览导出' }))
    expect(await screen.findByText('导出未开放或无权访问该材料。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '预览导出' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '导出目标已删除' })).toBeNull()
  })
})

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { RecordImportPanel } from './RecordImportPanel'

const api = vi.hoisted(() => ({
  dryRunRecordImport: vi.fn(),
  applyRecordImport: vi.fn(),
}))

vi.mock('../../lib/recordsApi', () => api)

describe('RecordImportPanel', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('previews remaps and quarantined envelopes then applies', async () => {
    api.dryRunRecordImport.mockResolvedValue({
      plan_id: 'rip_1',
      job_state: 'planned',
      lock_version: 2,
      remaps: [{ entity_kind: 'record', source_id: 'rec_source01', target_id: 'rec_local01' }],
      quarantine: [{ kind: 'vendor.unknown', schema: 'vendor.unknown/v1', digest: 'aa', byte_size: 8, reason: 'cannot interpret' }],
      object_count: 2,
      expires_at: '2026-08-21T13:00:00Z',
    })
    api.applyRecordImport.mockResolvedValue({
      plan_id: 'rip_1',
      job_state: 'applied',
      record_ids: ['rec_local01'],
    })
    render(<RecordImportPanel />)
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['PK'], 'archive.zip', { type: 'application/zip' })] } })
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('record rec_source01 → rec_local01')).toBeInTheDocument()
    expect(screen.getByText('vendor.unknown vendor.unknown/v1：cannot interpret')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认应用' }))
    await waitFor(() => expect(api.applyRecordImport).toHaveBeenCalledWith('rip_1', 2))
    expect(await screen.findByText('已导入 1 条记录')).toBeInTheDocument()
  })

  it('names tombstoned origins instead of pretending restore succeeded', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport.mockRejectedValue(new ApiError(409, 'tombstoned', { code: 'origin_tombstoned' }))
    render(<RecordImportPanel />)
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['PK'], 'archive.zip', { type: 'application/zip' })] } })
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('该来源已墓碑化，不能官方恢复或再导入。')).toBeInTheDocument()
  })

  it('names an already-imported origin on dry-run instead of offering apply', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport.mockRejectedValue(new ApiError(409, 'already imported', { code: 'import_origin_conflict' }))
    render(<RecordImportPanel />)
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['PK'], 'archive.zip', { type: 'application/zip' })] } })
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('该归档已导入过，不能再次官方导入。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认应用' })).toBeDisabled()
  })

  it('names an already-imported origin when apply is rejected', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport.mockResolvedValue({
      plan_id: 'rip_1',
      job_state: 'planned',
      lock_version: 2,
      remaps: [{ entity_kind: 'record', source_id: 'rec_source01', target_id: 'rec_local01' }],
      quarantine: [],
      object_count: 1,
      expires_at: '2026-08-21T13:00:00Z',
    })
    api.applyRecordImport.mockRejectedValue(new ApiError(409, 'already imported', { code: 'import_origin_conflict' }))
    render(<RecordImportPanel />)
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['PK'], 'archive.zip', { type: 'application/zip' })] } })
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('record rec_source01 → rec_local01')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认应用' }))
    expect(await screen.findByText('该归档已导入过，不能再次官方导入。')).toBeInTheDocument()
  })

  it('shows a deleted shell when a previewed import plan disappears', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport.mockResolvedValue({
      plan_id: 'rip_1',
      job_state: 'planned',
      lock_version: 2,
      remaps: [{ entity_kind: 'record', source_id: 'rec_source01', target_id: 'rec_local01' }],
      quarantine: [],
      object_count: 1,
      expires_at: '2026-08-21T13:00:00Z',
    })
    api.applyRecordImport.mockRejectedValue(new ApiError(404, 'gone', { code: 'resource_not_found' }))
    render(<RecordImportPanel />)
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['PK'], 'archive.zip', { type: 'application/zip' })] } })
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('record rec_source01 → rec_local01')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认应用' }))
    expect(await screen.findByRole('heading', { name: '导入计划已删除' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '确认应用' })).toBeNull()
  })

  it('keeps the form when the first dry-run is unauthorized', async () => {
    const { ApiError } = await import('../../lib/apiRequest')
    api.dryRunRecordImport.mockRejectedValue(new ApiError(404, 'missing', { code: 'resource_not_found' }))
    render(<RecordImportPanel />)
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['PK'], 'archive.zip', { type: 'application/zip' })] } })
    fireEvent.click(screen.getByRole('button', { name: '预检导入' }))
    expect(await screen.findByText('导入未开放或无权访问。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '预检导入' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '导入计划已删除' })).toBeNull()
  })
})

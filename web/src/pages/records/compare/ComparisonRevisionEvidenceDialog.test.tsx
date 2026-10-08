import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import type { EvidenceSnapshotRead, RecordRevision } from '../../../lib/types'
import { ComparisonRevisionEvidenceDialog } from './ComparisonRevisionEvidenceDialog'

const api = vi.hoisted(() => ({
  revision: vi.fn(),
  snapshot: vi.fn(),
}))

vi.mock('../../../lib/recordsApi', () => ({
  getRecordRevision: (...args: unknown[]) => api.revision(...args),
  getEvidenceSnapshot: (...args: unknown[]) => api.snapshot(...args),
}))

function revision(ids: string[]): RecordRevision {
  return {
    record_id: 'rec_1',
    revision_id: 'rev_a',
    revision_no: 2,
    title: '第三晚修订',
    evidence_snapshot_ids: ids,
    subjects: [{
      registry_version: 1,
      kind: 'vps',
      role: 'affected',
      source_id: 'vps_a',
      primary: true,
      identity: { display_name: '边缘甲' },
    }],
  } as RecordRevision
}

function snapshot(id: string): EvidenceSnapshotRead {
  return {
    snapshot_id: id,
    title: id === 'evs_bad' ? '' : `快照 ${id}`,
    kind: 'monitoring.probe',
    observed_at: '2026-07-01T08:00:00Z',
    quality: { status: 'complete' },
    subject: { type: 'vps', id: 'vps_a', display_name: '边缘甲' },
  } as EvidenceSnapshotRead
}

function renderDialog(ids = ['evs_a', 'evs_b']) {
  const onApply = vi.fn()
  const onClose = vi.fn()
  render(
    <MemoryRouter>
      <ComparisonRevisionEvidenceDialog
        item={{ record_id: 'rec_1', revision_id: 'rev_a', snapshot_ids: ['evs_a'] }}
        onClose={onClose}
        onApply={onApply}
      />
    </MemoryRouter>,
  )
  return { onApply, onClose, ids }
}

describe('ComparisonRevisionEvidenceDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.revision.mockResolvedValue(revision(['evs_a', 'evs_b']))
    api.snapshot.mockImplementation(async (id: string) => {
      if (id === 'evs_b') throw new ApiError(404, 'hidden', { code: 'resource_not_found' })
      return snapshot(id)
    })
  })

  it('reads the revision snapshots and saves only the readable selection on that revision', async () => {
    const { onApply, onClose } = renderDialog()
    expect(onApply).not.toHaveBeenCalled()
    expect(await screen.findByRole('checkbox', { name: '选用 快照 evs_a' })).toBeChecked()
    const unreadable = await screen.findByRole('checkbox', { name: /不可读/ })
    expect(unreadable).toBeDisabled()
    expect(screen.getByText(/数据完整/)).toBeInTheDocument()
    const save = screen.getByRole('button', { name: '保存证据选择' })
    await waitFor(() => expect(save).toBeEnabled())
    fireEvent.click(save)
    expect(onApply).toHaveBeenCalledWith(
      { record_id: 'rec_1', revision_id: 'rev_a', snapshot_ids: ['evs_a'] },
      ['evs_a'],
    )
    expect(onClose).toHaveBeenCalled()
  })

  it('does not put a raw revision failure in the main copy', async () => {
    api.revision.mockRejectedValue(new Error('relation records does not exist'))
    renderDialog()
    expect(await screen.findByRole('heading', { name: '无法读取修订' })).toBeInTheDocument()
    expect(screen.getByText('无法读取该修订，请重试')).toBeInTheDocument()
    expect(screen.queryByText(/relation/)).not.toBeInTheDocument()
  })

  it('shows a permission failure as unreadable instead of an empty revision', async () => {
    api.revision.mockRejectedValue(new ApiError(404, 'hidden', { code: 'resource_not_found' }))
    renderDialog()
    expect(await screen.findByRole('heading', { name: '修订无法读取' })).toBeInTheDocument()
    expect(screen.getByText('该修订无法读取。')).toBeInTheDocument()
    expect(screen.queryByText(/没有关联证据/)).not.toBeInTheDocument()
  })

  it('links a revision with no evidence back to the subject workspace and keeps the basket', async () => {
    api.revision.mockResolvedValue(revision([]))
    const { onApply } = renderDialog()
    expect(await screen.findByRole('heading', { name: '该修订没有关联证据' })).toBeInTheDocument()
    expect(screen.getByText(/比较篮里的这项修订仍保留/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '打开证据工作区' })).toHaveAttribute('href', '/vps/vps_a/evidence')
    await waitFor(() => expect(api.snapshot).not.toHaveBeenCalled())
    expect(onApply).not.toHaveBeenCalled()
  })
})

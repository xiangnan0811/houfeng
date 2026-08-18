import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { RecordWorkspace } from './RecordWorkspace'
import { recordDetailFixture, recordRevisionFixture } from './testFixtures'

const api = vi.hoisted(() => ({
  getRecord: vi.fn(),
  getRecordRevision: vi.fn(),
  listRecordDrafts: vi.fn(),
  getRecordDraft: vi.fn(),
  createRecordDraft: vi.fn(),
  patchRecordDraft: vi.fn(),
  createRecord: vi.fn(),
  createRecordRevision: vi.fn(),
  restoreRecordRevision: vi.fn(),
}))

const collab = vi.hoisted(() => ({
  listRecordActions: vi.fn(),
  listRecordComments: vi.fn(),
  getRecordWatch: vi.fn(),
  createRecordAction: vi.fn(),
  updateRecordAction: vi.fn(),
  transitionRecordAction: vi.fn(),
  createRecordComment: vi.fn(),
  editRecordComment: vi.fn(),
  redactRecordComment: vi.fn(),
  setRecordWatch: vi.fn(),
}))

vi.mock('../../lib/auth-context', () => ({
  useAuth: () => ({
    user: { user_id: 'usr_1', username: 'admin', role: 'admin', display_name: '管理员' },
    loading: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  }),
}))

vi.mock('../../lib/recordsApi', () => api)
vi.mock('../../lib/recordCollaborationApi', () => collab)

describe('RecordWorkspace', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.listRecordDrafts.mockResolvedValue({ items: [] })
    collab.listRecordActions.mockResolvedValue({ items: [] })
    collab.listRecordComments.mockResolvedValue({ comments: [] })
    collab.getRecordWatch.mockResolvedValue({
      record_id: 'rec_001',
      preference: 'default',
      version: 1,
      sources: { author: true, owner: false, participant: false, comment: false, mention: false, action: false },
    })
  })

  it('does not offer checklist promotion on a record that does not exist yet', () => {
    render(<MemoryRouter><RecordWorkspace mode="new" /></MemoryRouter>)
    expect(screen.queryByRole('button', { name: '提升勾选为行动' })).toBeNull()
    expect(screen.getByRole('toolbar', { name: '编辑布局' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '插入模板' })).toBeInTheDocument()
  })

  it('defaults a valid business status when switching to a workflow type', () => {
    render(<MemoryRouter><RecordWorkspace mode="new" /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText('记录类型'), { target: { value: 'troubleshooting' } })
    expect(screen.getByLabelText('业务状态')).toHaveValue('pending_investigation')
    fireEvent.click(screen.getByRole('button', { name: '插入模板' }))
    expect(screen.getByLabelText('Markdown 源文')).toHaveValue('## 现象\n\n## 排查\n\n## 结论')
  })

  it('refreshes actions after an explicit checklist promotion', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current: {
        ...recordDetailFixture().current,
        body_markdown: '- [ ] inspect logs',
      },
    }))
    collab.createRecordAction.mockResolvedValue({ action_id: 'act_1' })
    collab.listRecordActions
      .mockResolvedValueOnce({ items: [] })
      .mockResolvedValueOnce({ items: [{
        action_id: 'act_1',
        record_id: 'rec_001',
        title: 'inspect logs',
        status: 'open',
        assignee_id: 'usr_1',
        version: 1,
      }] })
    render(<MemoryRouter><RecordWorkspace mode="edit" recordId="rec_001" /></MemoryRouter>)
    expect(await screen.findByLabelText('标题')).toHaveValue('Database outage')
    fireEvent.click(screen.getByRole('button', { name: '提升勾选为行动' }))
    fireEvent.click(screen.getByRole('button', { name: '预览行动项' }))
    fireEvent.click(screen.getByRole('button', { name: '确认创建行动项' }))
    await waitFor(() => expect(collab.createRecordAction).toHaveBeenCalled())
    await waitFor(() => expect(collab.listRecordActions).toHaveBeenCalledTimes(2))
  })

  it('lists only historical evidence and keeps materials read-only on a revision page', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current_revision_id: 'rrv_002',
      current: recordRevisionFixture({
        revision_id: 'rrv_002',
        title: 'current',
        evidence_snapshot_ids: ['ev_current'],
      }),
    }))
    api.getRecordRevision.mockResolvedValue(recordRevisionFixture({
      revision_id: 'rrv_001',
      evidence_snapshot_ids: ['ev_hist'],
    }))
    render(
      <MemoryRouter>
        <RecordWorkspace mode="revision" recordId="rec_001" revisionId="rrv_001" />
      </MemoryRouter>,
    )
    expect(await screen.findByRole('heading', { name: 'Database outage' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '材料与引用' }))
    expect(screen.getByText('ev_hist')).toBeInTheDocument()
    expect(screen.queryByText('ev_current')).toBeNull()
    expect(screen.getByRole('button', { name: '插入证据 ev_hist' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '移除证据 ev_hist' })).toBeDisabled()
  })

  it('navigates to the record after restoring a historical revision', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current_revision_id: 'rrv_002',
      current: recordRevisionFixture({ revision_id: 'rrv_002', title: 'current' }),
    }))
    api.getRecordRevision.mockResolvedValue(recordRevisionFixture())
    api.restoreRecordRevision.mockResolvedValue(recordDetailFixture())
    render(
      <MemoryRouter initialEntries={['/records/rec_001/revisions/rrv_001']}>
        <Routes>
          <Route path="/records/:recordId/revisions/:revisionId" element={<RecordWorkspace mode="revision" recordId="rec_001" revisionId="rrv_001" />} />
          <Route path="/records/:recordId" element={<p>record home</p>} />
        </Routes>
      </MemoryRouter>,
    )
    expect(await screen.findByRole('button', { name: '恢复为新修订' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '恢复为新修订' }))
    expect(await screen.findByText('record home')).toBeInTheDocument()
  })
})

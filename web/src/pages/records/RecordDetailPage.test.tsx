import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../lib/apiRequest'
import { recordDetailFixture } from './testFixtures'
import { RecordDetailPage } from './RecordDetailPage'

const api = vi.hoisted(() => ({
  getRecord: vi.fn(),
  getRecordRevision: vi.fn(),
  createRecordDraft: vi.fn(),
  patchRecordDraft: vi.fn(),
  createRecord: vi.fn(),
  createRecordRevision: vi.fn(),
  restoreRecordRevision: vi.fn(),
  listRecordDrafts: vi.fn().mockResolvedValue({ items: [] }),
  getRecordDraft: vi.fn(),
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

function renderDetail(path = '/records/rec_001') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/records/:recordId" element={<RecordDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('RecordDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    collab.listRecordActions.mockResolvedValue({ items: [] })
    collab.listRecordComments.mockResolvedValue({ comments: [] })
    collab.getRecordWatch.mockResolvedValue({
      record_id: 'rec_001',
      preference: 'default',
      version: 1,
      sources: { author: true, owner: false, participant: false, comment: false, mention: false, action: false },
    })
  })

  it('renders the current revision body from the allowlisted model', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    renderDetail()
    expect(screen.getByText('正在读取运维记录')).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Database outage' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Details' })).toBeInTheDocument()
  })

  it('shows a revoked empty shell when authorization is closed', async () => {
    api.getRecord.mockRejectedValue(new ApiError(403, 'forbidden'))
    renderDetail()
    await waitFor(() => expect(screen.getByText('记录访问已撤销')).toBeInTheDocument())
    expect(screen.queryByText('Database outage')).toBeNull()
  })
})

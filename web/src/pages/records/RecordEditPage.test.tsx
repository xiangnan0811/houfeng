import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { recordDetailFixture } from './testFixtures'
import { RecordEditPage } from './RecordEditPage'

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
vi.mock('../../lib/recordCollaborationApi', () => ({
  listRecordActions: vi.fn().mockResolvedValue({ items: [] }),
  listRecordComments: vi.fn().mockResolvedValue({ comments: [] }),
  getRecordWatch: vi.fn().mockResolvedValue({
    record_id: 'rec_001',
    preference: 'default',
    version: 1,
    sources: { author: true, owner: false, participant: false, comment: false, mention: false, action: false },
  }),
  createRecordAction: vi.fn(),
  updateRecordAction: vi.fn(),
  transitionRecordAction: vi.fn(),
  createRecordComment: vi.fn(),
  editRecordComment: vi.fn(),
  redactRecordComment: vi.fn(),
  setRecordWatch: vi.fn(),
}))

describe('RecordEditPage', () => {
  beforeEach(() => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
  })

  it('loads the record into the metadata form and source editor', async () => {
    render(
      <MemoryRouter initialEntries={['/records/rec_001/edit']}>
        <Routes>
          <Route path="/records/:recordId/edit" element={<RecordEditPage />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(await screen.findByLabelText('标题')).toHaveValue('Database outage')
    expect(screen.getByLabelText('Markdown 源文')).toHaveValue('# Details\nRecovered.')
    expect(screen.getByRole('button', { name: '发布修订' })).toBeInTheDocument()
  })
})

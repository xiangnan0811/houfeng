import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { recordDetailFixture, recordRevisionFixture } from './testFixtures'
import { RecordRevisionPage } from './RecordRevisionPage'

vi.mock('../../lib/auth-context', () => ({
  useAuth: () => ({
    user: { user_id: 'usr_1', username: 'admin', role: 'admin', display_name: '管理员' },
    loading: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  }),
}))

vi.mock('../../lib/recordsApi', () => ({
  getRecord: vi.fn().mockResolvedValue(recordDetailFixture({
    current_revision_id: 'rrv_002',
    current: recordRevisionFixture({ revision_id: 'rrv_002', title: 'current' }),
  })),
  getRecordRevision: vi.fn().mockResolvedValue(recordRevisionFixture()),
  listRecordDrafts: vi.fn().mockResolvedValue({ items: [] }),
  getRecordDraft: vi.fn(),
  createRecordDraft: vi.fn(),
  patchRecordDraft: vi.fn(),
  createRecord: vi.fn(),
  createRecordRevision: vi.fn(),
  restoreRecordRevision: vi.fn(),
}))

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

describe('RecordRevisionPage', () => {
  it('loads a historical revision and offers restore as a new revision', async () => {
    render(
      <MemoryRouter initialEntries={['/records/rec_001/revisions/rrv_001']}>
        <Routes>
          <Route path="/records/:recordId/revisions/:revisionId" element={<RecordRevisionPage />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(await screen.findByRole('heading', { name: 'Database outage' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '恢复为新修订' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '横向比较' })).toHaveAttribute(
      'href',
      expect.stringMatching(/^\/records\/compare\?state=/),
    )
  })
})

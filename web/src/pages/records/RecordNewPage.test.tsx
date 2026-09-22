import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { RecordNewPage } from './RecordNewPage'

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

describe('RecordNewPage', () => {
  it('renders the new record workspace without loading an existing record', () => {
    render(<MemoryRouter><RecordNewPage /></MemoryRouter>)
    expect(screen.getByRole('heading', { name: '新建运维记录' })).toBeInTheDocument()
    expect(screen.getByLabelText('标题')).toBeInTheDocument()
    expect(screen.getByLabelText('Markdown 源文')).toBeInTheDocument()
  })

  it('preselects the subject carried by the existing new-record codec', () => {
    render(
      <MemoryRouter initialEntries={['/records/new?subject=vps%3Avps_001%3Aaffected%3Aprimary&return_to=%2Fvps%2Fvps_001%2Factivity']}>
        <Routes>
          <Route path="/records/new" element={<RecordNewPage />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByLabelText('主体 ID')).toHaveValue('vps_001')
    expect(screen.getByRole('link', { name: '返回主体' })).toHaveAttribute('href', '/vps/vps_001/activity')
  })

  it('ignores a non-canonical return_to', () => {
    render(
      <MemoryRouter initialEntries={['/records/new?return_to=%2Frecords%2Fnew']}>
        <Routes>
          <Route path="/records/new" element={<RecordNewPage />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.queryByRole('link', { name: '返回主体' })).not.toBeInTheDocument()
  })
})

import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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
})

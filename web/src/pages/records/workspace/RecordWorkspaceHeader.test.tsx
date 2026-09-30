import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { RecordWorkspaceState } from '../hooks/useRecordDraft'
import { emptyRecordDraftPayload, payloadFromRevision, recordDetailFixture, recordRevisionFixture } from '../testFixtures'
import { RecordWorkspaceHeader } from './RecordWorkspaceHeader'

function workspaceState(overrides: Partial<RecordWorkspaceState>): RecordWorkspaceState {
  return {
    status: 'ready',
    mode: 'read',
    payload: emptyRecordDraftPayload('usr_1'),
    record: null,
    revision: null,
    draft: null,
    dirty: false,
    saving: false,
    publishing: false,
    message: '',
    conflictPayload: null,
    conflictServer: null,
    publishedRecordId: null,
    restoredToRecordId: null,
    ...overrides,
  }
}

function renderHeader(state: RecordWorkspaceState, extra: { revisionId?: string } = {}) {
  const callbacks = { onSave: vi.fn(), onPublish: vi.fn(), onExport: vi.fn(), onImport: vi.fn() }
  render(
    <MemoryRouter>
      <RecordWorkspaceHeader
        state={state}
        recordId="rec_001"
        revisionId={extra.revisionId}
        recordHref={(path) => path}
        subjectReturnHref={null}
        subjectReturnState={null}
        ownerLabel="值班"
        {...callbacks}
      />
    </MemoryRouter>,
  )
  return callbacks
}

describe('RecordWorkspaceHeader', () => {
  it('shows identity facts without draft status or a self link while reading', () => {
    const record = recordDetailFixture({
      current: recordRevisionFixture({ record_type: 'troubleshooting', business_status: 'investigating', status_group: 'in_progress', revision_no: 4 }),
    })
    renderHeader(workspaceState({ record, revision: record.current, payload: payloadFromRevision(record.current) }))

    expect(screen.getByRole('heading', { level: 1, name: 'Database outage' })).toBeInTheDocument()
    expect(screen.getByText('排障')).toBeInTheDocument()
    expect(screen.getByText('排查中')).toBeInTheDocument()
    expect(screen.getByText('影响 high')).toHaveClass('tone--alert')
    expect(screen.getByText('VPS · VPS Alpha')).toBeInTheDocument()
    expect(screen.getByText('#4')).toBeInTheDocument()
    expect(screen.queryByText('尚未创建草稿')).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '阅读' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '编辑' })).toHaveAttribute('href', '/records/rec_001/edit')
    expect(screen.getByRole('button', { name: '导出' })).toBeInTheDocument()
  })

  it('reports draft sync status and commit commands while editing', () => {
    const callbacks = renderHeader(workspaceState({ mode: 'edit', dirty: true, payload: { ...emptyRecordDraftPayload('usr_1'), title: '磁盘告警' } }))
    expect(screen.getByRole('heading', { level: 1, name: '编辑运维记录' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('本地未同步')
    expect(screen.getByRole('link', { name: '阅读' })).toHaveAttribute('href', '/records/rec_001')
    screen.getByRole('button', { name: '发布修订' }).click()
    expect(callbacks.onPublish).toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: '导出' })).not.toBeInTheDocument()
  })

  it('marks historical revisions and offers current version plus comparison', () => {
    const revision = recordRevisionFixture({ revision_no: 2 })
    renderHeader(workspaceState({
      mode: 'revision',
      revision,
      record: recordDetailFixture(),
      payload: payloadFromRevision(revision),
    }), { revisionId: 'rrv_001' })
    expect(screen.getByText('历史修订 #2')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '当前版本' })).toHaveAttribute('href', '/records/rec_001')
    expect(screen.getByRole('link', { name: '横向比较' }).getAttribute('href')).toContain('/records/compare')
  })
})

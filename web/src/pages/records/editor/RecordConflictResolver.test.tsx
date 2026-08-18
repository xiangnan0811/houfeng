import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { emptyRecordDraftPayload, payloadFromRevision, recordRevisionFixture } from '../testFixtures'
import { RecordConflictResolver } from './RecordConflictResolver'

describe('RecordConflictResolver', () => {
  it('does not emit a save until the operator chooses a side', () => {
    const onResolve = vi.fn()
    const local = { ...emptyRecordDraftPayload(), title: 'local', body_markdown: 'mine' }
    render(
      <RecordConflictResolver
        open
        local={local}
        server={payloadFromRevision(recordRevisionFixture({ title: 'server' }))}
        onClose={vi.fn()}
        onResolve={onResolve}
      />,
    )
    expect(onResolve).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '全部保留本地' }))
    expect(onResolve).toHaveBeenCalledWith(expect.objectContaining({ title: 'local' }))
  })

  it('merges only the fields the operator assigns to the server', () => {
    const onResolve = vi.fn()
    const local = { ...emptyRecordDraftPayload(), title: 'local', body_markdown: 'mine', impact_level: 'low' }
    render(
      <RecordConflictResolver
        open
        local={local}
        server={payloadFromRevision(recordRevisionFixture({
          title: 'server',
          body_markdown: 'theirs',
          impact_level: 'high',
        }))}
        onClose={vi.fn()}
        onResolve={onResolve}
      />,
    )
    const choices = screen.getByLabelText('逐字段选择')
    fireEvent.click(within(choices).getByRole('radio', { name: /服务端：server/u }))
    fireEvent.click(screen.getByRole('button', { name: '应用所选合并' }))
    expect(onResolve).toHaveBeenCalledWith(expect.objectContaining({
      title: 'server',
      body_markdown: 'mine',
      impact_level: 'low',
    }))
  })

  it('starts every field on the local side so nothing is taken from the server by default', () => {
    const onResolve = vi.fn()
    const local = { ...emptyRecordDraftPayload(), title: 'local', body_markdown: 'mine' }
    render(
      <RecordConflictResolver
        open
        local={local}
        server={payloadFromRevision(recordRevisionFixture({ title: 'server', body_markdown: 'theirs' }))}
        onClose={vi.fn()}
        onResolve={onResolve}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: '应用所选合并' }))
    expect(onResolve).toHaveBeenCalledWith(expect.objectContaining({ title: 'local', body_markdown: 'mine' }))
  })

  it('shows field and markdown hunks before the operator chooses a side', () => {
    render(
      <RecordConflictResolver
        open
        local={{ ...emptyRecordDraftPayload(), title: 'local', body_markdown: 'mine' }}
        server={payloadFromRevision(recordRevisionFixture({ title: 'server', body_markdown: 'theirs' }))}
        onClose={vi.fn()}
        onResolve={vi.fn()}
      />,
    )
    expect(screen.getByLabelText('修订差异')).toBeInTheDocument()
    expect(screen.getByLabelText('正文差异')).toBeInTheDocument()
  })
})

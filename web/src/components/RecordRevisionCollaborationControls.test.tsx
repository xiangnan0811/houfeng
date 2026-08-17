import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { RecordRevisionCollaborationControls } from './RecordRevisionCollaborationControls'

const members = [
  { id: 'usr_owner', label: '林岚' },
  { id: 'usr_peer', label: '周衡' },
]

describe('RecordRevisionCollaborationControls', () => {
  it('keeps owner, participants, and follow-up independently controlled', () => {
    const onOwnerChange = vi.fn()
    const onParticipantToggle = vi.fn()
    const onFollowUpChange = vi.fn()
    render(<RecordRevisionCollaborationControls
      state="ready"
      members={members}
      ownerId="usr_owner"
      participantIds={['usr_peer']}
      followUpAt="2026-08-19T09:30"
      onOwnerChange={onOwnerChange}
      onParticipantToggle={onParticipantToggle}
      onFollowUpChange={onFollowUpChange}
    />)

    fireEvent.change(screen.getByLabelText('负责人'), { target: { value: 'usr_peer' } })
    fireEvent.click(screen.getByRole('checkbox', { name: '林岚' }))
    fireEvent.change(screen.getByLabelText('跟进时间'), { target: { value: '2026-08-20T10:00' } })

    expect(onOwnerChange).toHaveBeenCalledWith('usr_peer')
    expect(onParticipantToggle).toHaveBeenCalledWith('usr_owner', true)
    expect(onFollowUpChange).toHaveBeenCalledWith('2026-08-20T10:00')
  })

  it('renders a non-interactive revoked state', () => {
    render(<RecordRevisionCollaborationControls
      state="revoked" members={members} ownerId="usr_owner" participantIds={[]}
      followUpAt="" onOwnerChange={vi.fn()} onParticipantToggle={vi.fn()} onFollowUpChange={vi.fn()}
    />)
    expect(screen.getByRole('status')).toHaveTextContent('协作权限已撤销')
    expect(screen.queryByLabelText('负责人')).not.toBeInTheDocument()
  })

  it('renders an explicit empty state without revision controls', () => {
    render(<RecordRevisionCollaborationControls
      state="empty" members={[]} ownerId="" participantIds={[]}
      followUpAt="" onOwnerChange={vi.fn()} onParticipantToggle={vi.fn()} onFollowUpChange={vi.fn()}
    />)
    expect(screen.getByText('暂无协作字段')).toBeInTheDocument()
    expect(screen.queryByLabelText('负责人')).not.toBeInTheDocument()
  })
})

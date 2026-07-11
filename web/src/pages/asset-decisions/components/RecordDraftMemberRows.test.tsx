import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { RecordDraft } from '../types'
import {
  RecordDraftMemberRows,
  type RecordDraftMemberRow,
} from './RecordDraftMemberRows'

const MEMBERS: RecordDraftMemberRow[] = [
  { vpsID: 'vps_1', displayName: 'Node 1', fallbackRole: 'primary_candidate', fallbackAction: 'keep' },
  { vpsID: 'vps_2', displayName: 'Node 2', fallbackRole: 'standby_candidate', fallbackAction: 'review' },
  { vpsID: 'vps_3', displayName: 'Node 3', fallbackRole: 'observe_candidate', fallbackAction: 'review' },
  { vpsID: 'vps_4', displayName: 'Node 4', fallbackRole: 'retire_candidate', fallbackAction: 'cancel' },
]

const DRAFT: RecordDraft = {
  sourceType: 'auto_group',
  sourceGroupID: 'adg_001',
  renewWithinDays: 30,
  title: '德国主备组合',
  goal: '',
  status: 'draft',
  memberOrder: MEMBERS.map((member) => member.vpsID),
  members: Object.fromEntries(MEMBERS.map((member) => [member.vpsID, {
    decidedRole: member.fallbackRole,
    decidedAction: member.fallbackAction,
    reason: '',
  }])),
}

describe('RecordDraftMemberRows', () => {
  it('limits the preview and emits semantic edit/member patch callbacks', () => {
    const onEditMember = vi.fn()
    const onUpdateMember = vi.fn()
    const { rerender } = render(
      <RecordDraftMemberRows
        members={MEMBERS}
        draft={DRAFT}
        editingMemberID={null}
        onEditMember={onEditMember}
        onUpdateMember={onUpdateMember}
      />,
    )

    expect(screen.getByText('Node 1')).toBeInTheDocument()
    expect(screen.queryByText('Node 4')).not.toBeInTheDocument()
    expect(screen.getByText('另有 1 台成员保留在保存底稿中')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '编辑 Node 1 成员理由' }))
    expect(onEditMember).toHaveBeenCalledWith('vps_1')

    rerender(
      <RecordDraftMemberRows
        members={MEMBERS}
        draft={DRAFT}
        editingMemberID="vps_1"
        onEditMember={onEditMember}
        onUpdateMember={onUpdateMember}
      />,
    )
    fireEvent.change(screen.getByLabelText('角色'), { target: { value: 'standby_candidate' } })
    fireEvent.change(screen.getByLabelText('动作'), { target: { value: 'migrate' } })
    fireEvent.change(screen.getByLabelText('理由'), { target: { value: '成本过高' } })
    fireEvent.click(screen.getByRole('button', { name: '收起编辑' }))

    expect(onUpdateMember).toHaveBeenNthCalledWith(1, 'vps_1', { decidedRole: 'standby_candidate' })
    expect(onUpdateMember).toHaveBeenNthCalledWith(2, 'vps_1', { decidedAction: 'migrate' })
    expect(onUpdateMember).toHaveBeenNthCalledWith(3, 'vps_1', { reason: '成本过高' })
    expect(onEditMember).toHaveBeenLastCalledWith(null)
  })
})

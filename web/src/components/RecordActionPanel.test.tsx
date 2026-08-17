import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { RecordAction } from '../lib/types'
import { RecordActionPanel } from './RecordActionPanel'

const action: RecordAction = {
  action_id: 'ract_one', record_id: 'rec_one', version: 2, status: 'open', title: '复核证据',
  assignee_id: 'usr_peer', due_at: '2026-08-20T09:00:00Z', completed_at: null,
  subject_revision_id: '', created_at: '2026-08-17T09:00:00Z', updated_at: '2026-08-17T10:00:00Z',
}

describe('RecordActionPanel', () => {
  it('creates a bounded action and exposes native transition commands', () => {
    const onCreate = vi.fn()
    const onTransition = vi.fn()
    render(<RecordActionPanel state="ready" actions={[action]} members={[{ id: 'usr_peer', label: '周衡' }]}
      busy={false} onCreate={onCreate} onTransition={onTransition} />)

    fireEvent.change(screen.getByLabelText('行动标题'), { target: { value: '确认修复窗口' } })
    fireEvent.change(screen.getByLabelText('指派给'), { target: { value: 'usr_peer' } })
    fireEvent.click(screen.getByRole('button', { name: '新增行动' }))
    expect(onCreate).toHaveBeenCalledWith(expect.objectContaining({ title: '确认修复窗口', assignee_id: 'usr_peer' }))

    fireEvent.click(screen.getByRole('button', { name: '完成“复核证据”' }))
    expect(onTransition).toHaveBeenCalledWith(action, 'complete')
  })

  it.each([
    ['loading', '正在读取行动项'],
    ['empty', '暂无行动项'],
    ['error', '行动项暂不可用'],
    ['deleted', '记录已删除'],
  ] as const)('renders %s without action commands', (state, label) => {
    render(<RecordActionPanel state={state} actions={[]} members={[]} busy={false}
      onCreate={vi.fn()} onTransition={vi.fn()} />)
    expect(screen.getByText(label)).toBeInTheDocument()
  })
})

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { RecordWatch } from '../lib/types'
import { RecordWatchControl } from './RecordWatchControl'

const watch: RecordWatch = {
  record_id: 'rec_one', user_id: 'usr_self', version: 3, preference: 'default',
  sources: { author: false, owner: true, participant: false, comment: true, mention: false, action: false },
  updated_at: '2026-08-17T10:00:00Z',
}

describe('RecordWatchControl', () => {
  it('changes explicit preference while preserving mandatory-notification explanation', () => {
    const onChange = vi.fn()
    render(<RecordWatchControl state="ready" watch={watch} busy={false} onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: '关注全部更新' }))
    expect(onChange).toHaveBeenCalledWith('watching')
    expect(screen.getByText(/直接指派、安全提醒与提及仍会送达/)).toBeInTheDocument()
    expect(screen.getByText('负责人、评论参与')).toBeInTheDocument()
  })

  it('shows loading without preference controls', () => {
    render(<RecordWatchControl state="loading" watch={null} busy={false} onChange={vi.fn()} />)
    expect(screen.getByText('正在读取关注状态')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '关注全部更新' })).not.toBeInTheDocument()
  })

  it('shows an explicit empty state without treating it as an error', () => {
    render(<RecordWatchControl state="empty" watch={null} busy={false} onChange={vi.fn()} />)
    expect(screen.getByText('暂无关注状态')).toBeInTheDocument()
    expect(screen.queryByText('关注状态暂不可用')).not.toBeInTheDocument()
  })
})

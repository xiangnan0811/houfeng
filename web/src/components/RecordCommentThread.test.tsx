import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { RecordComment } from '../lib/types'
import { RecordCommentThread } from './RecordCommentThread'

const comment: RecordComment = {
  comment_id: 'rcm_one', record_id: 'rec_one', author_id: 'usr_self', version: 2, state: 'active',
  body_markdown: '**已验证**', render_model: { version: 'comment_markdown/v1', nodes: [{
    type: 'paragraph', children: [{ type: 'strong', children: [{ type: 'text', text: '已验证' }] }],
  }] }, reply_to_comment_id: '', mention_user_ids: [], created_at: '2026-08-17T09:00:00Z',
  updated_at: '2026-08-17T10:00:00Z', redacted_at: null,
}

describe('RecordCommentThread', () => {
  it('supports reply, mention, fresh replacement edit, and modal redaction confirmation', async () => {
    const onSubmit = vi.fn()
    const onRedact = vi.fn()
    render(<RecordCommentThread state="ready" comments={[comment]} currentUserId="usr_self"
      members={[{ id: 'usr_peer', label: '周衡' }]} busy={false} onSubmit={onSubmit} onRedact={onRedact} />)

    expect(screen.getByText('已验证').closest('strong')).not.toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '回复该评论' }))
    fireEvent.click(screen.getByRole('checkbox', { name: '提及周衡' }))
    fireEvent.change(screen.getByLabelText('评论内容'), { target: { value: '收到，继续跟进。' } })
    fireEvent.click(screen.getByRole('button', { name: '发布回复' }))
    expect(onSubmit).toHaveBeenCalledWith({ mode: 'create', comment_id: '', version: 0,
      body_markdown: '收到，继续跟进。', reply_to_comment_id: 'rcm_one', mention_user_ids: ['usr_peer'] })

    fireEvent.click(screen.getByRole('button', { name: '编辑该评论' }))
    expect(screen.getByLabelText('评论内容')).toHaveValue('')
    fireEvent.change(screen.getByLabelText('评论内容'), { target: { value: '替换后的内容' } })
    fireEvent.click(screen.getByRole('button', { name: '保存编辑' }))
    expect(onSubmit).toHaveBeenLastCalledWith(expect.objectContaining({
      mode: 'edit', comment_id: 'rcm_one', version: 2, body_markdown: '替换后的内容',
    }))

    const trigger = screen.getByRole('button', { name: '请求遮盖该评论' })
    trigger.focus()
    fireEvent.click(trigger)
    const dialog = screen.getByRole('alertdialog', { name: '确认永久遮盖评论' })
    await waitFor(() => expect(dialog).toContainElement(document.activeElement as HTMLElement))
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
    expect(trigger).toHaveFocus()

    fireEvent.click(trigger)
    fireEvent.click(screen.getByRole('button', { name: '确认永久遮盖' }))
    expect(onRedact).toHaveBeenCalledWith(comment)
  })

  it('renders redacted and revoked states without unsafe fallback', () => {
    render(<RecordCommentThread state="ready" comments={[{ ...comment, state: 'redacted', body_markdown: null,
      render_model: null, mention_user_ids: [], redacted_at: '2026-08-17T11:00:00Z' }]}
      currentUserId="usr_self" members={[]} busy={false} onSubmit={vi.fn()} onRedact={vi.fn()} />)
    expect(screen.getByText('评论内容已永久遮盖')).toBeInTheDocument()
    expect(screen.queryByText('**已验证**')).not.toBeInTheDocument()
  })

  it('clears composer and redaction state when access leaves ready', () => {
    const props = {
      comments: [comment], currentUserId: 'usr_self', members: [], busy: false,
      onSubmit: vi.fn(), onRedact: vi.fn(),
    }
    const { rerender } = render(<RecordCommentThread state="ready" {...props} />)
    fireEvent.click(screen.getByRole('button', { name: '编辑该评论' }))
    fireEvent.change(screen.getByLabelText('评论内容'), { target: { value: '仅本地草稿' } })
    fireEvent.click(screen.getByRole('button', { name: '请求遮盖该评论' }))
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()

    rerender(<RecordCommentThread state="revoked" {...props} />)
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    rerender(<RecordCommentThread state="ready" {...props} />)
    expect(screen.getByLabelText('评论内容')).toHaveValue('')
    expect(screen.getByText('发布新评论')).toBeInTheDocument()
  })

  it('discards stale edit and redaction state when the active comment version advances', () => {
    const onSubmit = vi.fn()
    const props = {
      currentUserId: 'usr_self', members: [], busy: false, onSubmit, onRedact: vi.fn(),
    }
    const { rerender } = render(<RecordCommentThread state="ready" comments={[comment]} {...props} />)
    fireEvent.click(screen.getByRole('button', { name: '编辑该评论' }))
    fireEvent.change(screen.getByLabelText('评论内容'), { target: { value: '基于 v2 的旧草稿' } })
    fireEvent.click(screen.getByRole('button', { name: '请求遮盖该评论' }))
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()

    rerender(<RecordCommentThread state="ready" comments={[{
      ...comment,
      version: 3,
      body_markdown: '服务端已更新',
      updated_at: '2026-08-17T10:05:00Z',
    }]} {...props} />)

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(screen.getByText('发布新评论')).toBeInTheDocument()
    expect(screen.getByLabelText('评论内容')).toHaveValue('')
    fireEvent.click(screen.getByRole('button', { name: '发布评论' }))
    expect(onSubmit).not.toHaveBeenCalled()
  })
})

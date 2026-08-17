import { fireEvent, render, screen } from '@testing-library/react'
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
  it('supports reply, mention, edit, and explicit redaction confirmation', () => {
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
    expect(screen.getByLabelText('评论内容')).toHaveValue('**已验证**')
    fireEvent.click(screen.getByRole('button', { name: '保存编辑' }))
    expect(onSubmit).toHaveBeenLastCalledWith(expect.objectContaining({ mode: 'edit', comment_id: 'rcm_one', version: 2 }))

    fireEvent.click(screen.getByRole('button', { name: '请求遮盖该评论' }))
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
})

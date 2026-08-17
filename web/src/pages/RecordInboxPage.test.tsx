import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../lib/apiRequest'
import type { RecordNotification } from '../lib/types'
import { RecordInboxPage } from './RecordInboxPage'

const api = vi.hoisted(() => ({
  list: vi.fn(),
  target: vi.fn(),
  read: vi.fn(),
  unread: vi.fn(),
  dismiss: vi.fn(),
}))

vi.mock('../lib/recordCollaborationApi', () => ({
  listRecordNotifications: api.list,
  getRecordNotificationTarget: api.target,
  markRecordNotificationRead: api.read,
  markRecordNotificationUnread: api.unread,
  dismissRecordNotification: api.dismiss,
}))

const unreadNotification: RecordNotification = {
  notification_id: 'rnt_001',
  record_id: 'rec_001',
  event_kind: 'record_comment_mentioned',
  subject_kind: 'comment',
  subject_id: 'rcm_001',
  source_version: 3,
  reason: 'mention',
  mandatory: true,
  event_at: '2026-08-17T09:30:00Z',
  read_at: null,
  dismissed_at: null,
}

function renderPage() {
  return render(<MemoryRouter><RecordInboxPage /></MemoryRouter>)
}

describe('RecordInboxPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.target.mockResolvedValue({ record_id: 'rec_001', subject_kind: 'comment', subject_id: 'rcm_001' })
    api.read.mockImplementation(async () => ({ ...unreadNotification, read_at: '2026-08-17T10:00:00Z' }))
    api.unread.mockImplementation(async () => ({ ...unreadNotification, read_at: null }))
    api.dismiss.mockImplementation(async () => ({ ...unreadNotification, dismissed_at: '2026-08-17T10:01:00Z' }))
  })

  it('renders a dense closed inbox and resolves only the selected safe target', async () => {
    api.list.mockResolvedValue({ items: [unreadNotification] })
    renderPage()

    expect(screen.getByText('正在读取记录通知')).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: '记录协作收件箱' })).toBeInTheDocument()
    expect(screen.getByText('评论提及')).toBeInTheDocument()
    expect(screen.getByText('必要送达')).toBeInTheDocument()
    expect(screen.getByText('rec_001')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '查看“评论提及”的对象' }))
    expect(await screen.findByText('目标：评论 rcm_001')).toBeInTheDocument()
    expect(api.target).toHaveBeenCalledWith('rnt_001')
  })

  it('updates read state and removes a dismissed item without exposing mutable record content', async () => {
    api.list.mockResolvedValue({ items: [unreadNotification] })
    renderPage()
    await screen.findByText('评论提及')

    fireEvent.click(screen.getByRole('button', { name: '标记“评论提及”为已读' }))
    await waitFor(() => expect(screen.getByText('已读')).toBeInTheDocument())
    expect(api.read).toHaveBeenCalledWith('rnt_001')

    fireEvent.click(screen.getByRole('button', { name: '移除“评论提及”' }))
    expect(await screen.findByText('当前没有待处理通知')).toBeInTheDocument()
    expect(api.dismiss).toHaveBeenCalledWith('rnt_001')
  })

  it('renders explicit empty, revoked, and opaque error states', async () => {
    api.list.mockResolvedValueOnce({ items: [] })
    const { unmount } = renderPage()
    expect(await screen.findByText('当前没有待处理通知')).toBeInTheDocument()
    unmount()

    api.list.mockRejectedValueOnce(new ApiError(404, 'private-record-title'))
    const revoked = renderPage()
    expect(await screen.findByText('收件箱访问已撤销')).toBeInTheDocument()
    expect(screen.queryByText('private-record-title')).not.toBeInTheDocument()
    revoked.unmount()

    api.list.mockRejectedValueOnce(new ApiError(503, 'postgres.internal:5432'))
    renderPage()
    expect(await screen.findByRole('alert')).toHaveTextContent('记录通知暂不可用')
    expect(screen.queryByText(/postgres\.internal/)).not.toBeInTheDocument()
  })
})

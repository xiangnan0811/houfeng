import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
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
  event_kind: 'comment_mentioned',
  subject_kind: 'comment',
  subject_id: 'rcm_001',
  source_version: 3,
  reason: 'mention',
  mandatory: true,
  event_at: '2026-08-17T09:30:00Z',
  read_at: null,
  dismissed_at: null,
}

const actionNotification: RecordNotification = {
  ...unreadNotification,
  notification_id: 'rnt_002',
  event_kind: 'action_assigned',
  subject_kind: 'action',
  subject_id: 'ract_002',
  reason: 'assignee',
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
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
    expect(screen.getByRole('link', { name: '打开记录' })).toHaveAttribute('href', '/records/rec_001')
  })

  it('updates read state and removes a dismissed item without exposing mutable record content', async () => {
    const invalidated = vi.fn()
    window.addEventListener('houfeng:record-inbox-unread-invalidated', invalidated)
    api.list.mockResolvedValue({ items: [unreadNotification] })
    renderPage()
    await screen.findByText('评论提及')

    fireEvent.click(screen.getByRole('button', { name: '标记“评论提及”为已读' }))
    await waitFor(() => expect(screen.getByText('已读')).toBeInTheDocument())
    expect(api.read).toHaveBeenCalledWith('rnt_001')
    expect(invalidated).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '移除“评论提及”' }))
    expect(await screen.findByText('当前没有待处理通知')).toBeInTheDocument()
    expect(api.dismiss).toHaveBeenCalledWith('rnt_001')
    expect(invalidated).toHaveBeenCalledTimes(2)
    window.removeEventListener('houfeng:record-inbox-unread-invalidated', invalidated)
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

  it('does not let an older target response overwrite the latest selection', async () => {
    const first = deferred<{ record_id: string; subject_kind: 'comment'; subject_id: string }>()
    const second = deferred<{ record_id: string; subject_kind: 'action'; subject_id: string }>()
    api.list.mockResolvedValue({ items: [unreadNotification, actionNotification] })
    api.target.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    renderPage()
    await screen.findByText('评论提及')

    fireEvent.click(screen.getByRole('button', { name: '查看“评论提及”的对象' }))
    fireEvent.click(screen.getByRole('button', { name: '查看“行动指派”的对象' }))
    await act(async () => second.resolve({ record_id: 'rec_001', subject_kind: 'action', subject_id: 'ract_002' }))
    expect(await screen.findByText('目标：行动 ract_002')).toBeInTheDocument()
    await act(async () => first.resolve({ record_id: 'rec_001', subject_kind: 'comment', subject_id: 'rcm_001' }))
    expect(screen.queryByText('目标：评论 rcm_001')).not.toBeInTheDocument()
  })

	it('does not expose a pending target after a same-item dismiss supersedes it', async () => {
		const pendingTarget = deferred<{ record_id: string; subject_kind: 'comment'; subject_id: string }>()
		const pendingDismiss = deferred<RecordNotification>()
		api.list.mockResolvedValue({ items: [unreadNotification] })
		api.target.mockReturnValue(pendingTarget.promise)
		api.dismiss.mockReturnValue(pendingDismiss.promise)
		renderPage()
		await screen.findByText('评论提及')

		const viewButton = screen.getByRole('button', { name: '查看“评论提及”的对象' })
		const dismissButton = screen.getByRole('button', { name: '移除“评论提及”' })
		act(() => {
			viewButton.click()
			dismissButton.click()
		})
		expect(api.target).toHaveBeenCalledTimes(1)
		expect(api.dismiss).toHaveBeenCalledTimes(1)
		await act(async () => pendingTarget.resolve({ record_id: 'rec_001', subject_kind: 'comment', subject_id: 'rcm_001' }))
		expect(screen.queryByText('目标：评论 rcm_001')).not.toBeInTheDocument()
		await act(async () => pendingDismiss.resolve({ ...unreadNotification, dismissed_at: '2026-08-17T10:01:00Z' }))
		expect(await screen.findByText('当前没有待处理通知')).toBeInTheDocument()
	})

	it('does not let a superseded target failure overwrite a successful dismiss', async () => {
		const pendingTarget = deferred<{ record_id: string; subject_kind: 'comment'; subject_id: string }>()
		const pendingDismiss = deferred<RecordNotification>()
		api.list.mockResolvedValue({ items: [unreadNotification] })
		api.target.mockReturnValue(pendingTarget.promise)
		api.dismiss.mockReturnValue(pendingDismiss.promise)
		renderPage()
		await screen.findByText('评论提及')

		act(() => {
			screen.getByRole('button', { name: '查看“评论提及”的对象' }).click()
			screen.getByRole('button', { name: '移除“评论提及”' }).click()
		})
		await act(async () => pendingDismiss.resolve({ ...unreadNotification, dismissed_at: '2026-08-17T10:01:00Z' }))
		expect(await screen.findByText('当前没有待处理通知')).toBeInTheDocument()
		await act(async () => pendingTarget.reject(new ApiError(503, 'private target failure')))
		expect(screen.getByText('当前没有待处理通知')).toBeInTheDocument()
		expect(screen.queryByText('记录通知暂不可用')).not.toBeInTheDocument()
	})

  it.each([
    [404, '收件箱访问已撤销'],
    [503, '记录通知暂不可用'],
  ])('does not suppress target A failure after unrelated item B mutation (%i)', async (status, expectedState) => {
    const pendingTarget = deferred<{ record_id: string; subject_kind: 'comment'; subject_id: string }>()
    api.list.mockResolvedValue({ items: [unreadNotification, actionNotification] })
    api.target.mockReturnValue(pendingTarget.promise)
    api.dismiss.mockResolvedValue({ ...actionNotification, dismissed_at: '2026-08-17T10:01:00Z' })
    renderPage()
    await screen.findByText('评论提及')

    fireEvent.click(screen.getByRole('button', { name: '查看“评论提及”的对象' }))
    fireEvent.click(screen.getByRole('button', { name: '移除“行动指派”' }))
    await waitFor(() => expect(screen.queryByText('行动指派')).not.toBeInTheDocument())
    await act(async () => pendingTarget.reject(new ApiError(status, 'private target failure')))

    expect(await screen.findByText(expectedState)).toBeInTheDocument()
    expect(screen.getByLabelText('通知摘要')).toHaveTextContent('0')
  })

  it('does not suppress target A failure after selecting unrelated target B', async () => {
    const pendingTargetA = deferred<{ record_id: string; subject_kind: 'comment'; subject_id: string }>()
    api.list.mockResolvedValue({ items: [unreadNotification, actionNotification] })
    api.target
      .mockReturnValueOnce(pendingTargetA.promise)
      .mockResolvedValueOnce({ record_id: 'rec_001', subject_kind: 'action', subject_id: 'ract_002' })
    renderPage()
    await screen.findByText('评论提及')

    fireEvent.click(screen.getByRole('button', { name: '查看“评论提及”的对象' }))
    fireEvent.click(screen.getByRole('button', { name: '查看“行动指派”的对象' }))
    expect(await screen.findByText('目标：行动 ract_002')).toBeInTheDocument()
    await act(async () => pendingTargetA.reject(new ApiError(404, 'private target failure')))

    expect(await screen.findByText('收件箱访问已撤销')).toBeInTheDocument()
    expect(screen.getByLabelText('通知摘要')).toHaveTextContent('0')
  })

  it('keeps a newer item busy when an older operation settles', async () => {
    const first = deferred<RecordNotification>()
    const second = deferred<RecordNotification>()
    api.list.mockResolvedValue({ items: [unreadNotification, actionNotification] })
    api.read.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    renderPage()
    await screen.findByText('评论提及')

    const firstButton = screen.getByRole('button', { name: '标记“评论提及”为已读' })
    const secondButton = screen.getByRole('button', { name: '标记“行动指派”为已读' })
    fireEvent.click(firstButton)
    fireEvent.click(secondButton)
    expect(secondButton).toBeDisabled()
    await act(async () => first.resolve({ ...unreadNotification, read_at: '2026-08-17T10:00:00Z' }))
    expect(secondButton).toBeDisabled()
    await act(async () => second.resolve({ ...actionNotification, read_at: '2026-08-17T10:00:00Z' }))
    await waitFor(() => expect(secondButton).not.toBeDisabled())
  })

  it('clears prior items and target on a revoked transition', async () => {
    api.list.mockResolvedValue({ items: [unreadNotification] })
    api.read.mockRejectedValue(new ApiError(404, 'private target'))
    renderPage()
    await screen.findByText('评论提及')
    fireEvent.click(screen.getByRole('button', { name: '查看“评论提及”的对象' }))
    expect(await screen.findByText('目标：评论 rcm_001')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '标记“评论提及”为已读' }))

    expect(await screen.findByText('收件箱访问已撤销')).toBeInTheDocument()
    expect(screen.getByLabelText('通知摘要')).toHaveTextContent('0')
  })

  it('fails closed when a target response does not match the selected notification', async () => {
    api.list.mockResolvedValue({ items: [unreadNotification] })
    api.target.mockResolvedValue({ record_id: 'rec_other', subject_kind: 'comment', subject_id: 'rcm_001' })
    renderPage()
    await screen.findByText('评论提及')
    fireEvent.click(screen.getByRole('button', { name: '查看“评论提及”的对象' }))

    expect(await screen.findByText('记录通知暂不可用')).toBeInTheDocument()
    expect(screen.getByLabelText('通知摘要')).toHaveTextContent('0')
  })
})

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ChangePasswordModal } from './ChangePasswordModal'
import * as client from '../../lib/auth-client'

describe('ChangePasswordModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('submits old + new password', async () => {
    const spy = vi.spyOn(client, 'changePassword').mockResolvedValue()
    render(<ChangePasswordModal onClose={() => {}} />)
    fireEvent.change(screen.getByLabelText('当前密码'), { target: { value: 'old-correct-horse' } })
    fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'new-correct-horse' } })
    fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'new-correct-horse' } })
    fireEvent.click(screen.getByRole('button', { name: '确认修改' }))
    await waitFor(() =>
      expect(spy).toHaveBeenCalledWith('old-correct-horse', 'new-correct-horse'),
    )
  })

  it('rejects mismatch without calling API', () => {
    const spy = vi.spyOn(client, 'changePassword').mockResolvedValue()
    render(<ChangePasswordModal onClose={() => {}} />)
    fireEvent.change(screen.getByLabelText('当前密码'), { target: { value: 'old-correct-horse' } })
    fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'new-correct-horse' } })
    fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'wrong-confirm-pwd' } })
    fireEvent.click(screen.getByRole('button', { name: '确认修改' }))
    expect(screen.getByRole('alert')).toHaveTextContent('两次输入不一致')
    expect(spy).not.toHaveBeenCalled()
  })

  it('rejects too-short new password', () => {
    render(<ChangePasswordModal onClose={() => {}} />)
    fireEvent.change(screen.getByLabelText('当前密码'), { target: { value: 'old-correct-horse' } })
    fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'short' } })
    fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'short' } })
    fireEvent.click(screen.getByRole('button', { name: '确认修改' }))
    expect(screen.getByRole('alert')).toHaveTextContent('至少')
  })
})

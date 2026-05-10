import { useState } from 'react'
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

  it('renders through a portal and focuses the first password field', async () => {
    const { container } = render(<ChangePasswordModal onClose={() => {}} />)

    const dialog = screen.getByRole('dialog', { name: '修改密码' })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(container.querySelector('.modal')).toBeNull()
    expect(document.body.querySelector('.modal')).toBe(dialog)
    await waitFor(() => expect(screen.getByLabelText('当前密码')).toHaveFocus())
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

  it('closes on Escape and overlay mouse down', () => {
    const onClose = vi.fn()
    const { rerender } = render(<ChangePasswordModal onClose={onClose} />)

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)

    rerender(<ChangePasswordModal onClose={onClose} />)
    const overlay = document.body.querySelector('.modal-backdrop')
    expect(overlay).not.toBeNull()
    fireEvent.mouseDown(overlay!)
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('contains Tab navigation and restores focus to the change-password trigger', async () => {
    function ModalHarness() {
      const [open, setOpen] = useState(false)

      return (
        <>
          <button type="button" onClick={() => setOpen(true)}>
            修改密码
          </button>
          {open && <ChangePasswordModal onClose={() => setOpen(false)} />}
        </>
      )
    }

    render(<ModalHarness />)
    const opener = screen.getByRole('button', { name: '修改密码' })
    opener.focus()
    fireEvent.click(opener)

    const currentPassword = screen.getByLabelText('当前密码')
    const submitButton = screen.getByRole('button', { name: '确认修改' })
    await waitFor(() => expect(currentPassword).toHaveFocus())

    submitButton.focus()
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(currentPassword).toHaveFocus()

    currentPassword.focus()
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(submitButton).toHaveFocus()

    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: '修改密码' })).not.toBeInTheDocument(),
    )
    expect(opener).toHaveFocus()
  })
})

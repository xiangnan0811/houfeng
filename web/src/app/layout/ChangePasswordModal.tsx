import { useId, useState, type FormEvent } from 'react'
import { createPortal } from 'react-dom'

import { Button, Input } from '../../components/atoms'
import { changePassword } from '../../lib/auth-client'
import { useModalFocus } from '../../lib/useModalFocus'

export interface ChangePasswordModalProps {
  onClose: () => void
}

export function ChangePasswordModal({ onClose }: ChangePasswordModalProps) {
  const titleId = useId()
  const modalRef = useModalFocus<HTMLFormElement>(true, onClose)
  const [oldPw, setOldPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [done, setDone] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    if (newPw !== confirmPw) {
      setError('两次输入不一致')
      return
    }
    if (newPw.length < 8) {
      setError('新密码至少 8 个字符')
      return
    }
    setSubmitting(true)
    try {
      await changePassword(oldPw, newPw)
      setDone(true)
      setTimeout(onClose, 1200)
    } catch {
      setError('修改失败：当前密码可能不正确')
    } finally {
      setSubmitting(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={onClose}>
      <form
        ref={modalRef}
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        onSubmit={onSubmit}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <h2 id={titleId} className="modal__title">
          修改密码
        </h2>
        {error && (
          <div role="alert" className="modal__error">
            {error}
          </div>
        )}
        {done && (
          <div role="status" className="modal__success">
            已修改
          </div>
        )}
        <Input
          label="当前密码"
          type="password"
          value={oldPw}
          onChange={(e) => setOldPw(e.target.value)}
        />
        <Input
          label="新密码"
          type="password"
          value={newPw}
          onChange={(e) => setNewPw(e.target.value)}
        />
        <Input
          label="确认新密码"
          type="password"
          value={confirmPw}
          onChange={(e) => setConfirmPw(e.target.value)}
        />
        <div className="modal__actions">
          <Button type="button" variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" variant="primary" disabled={submitting}>
            确认修改
          </Button>
        </div>
      </form>
    </div>,
    document.body,
  )
}

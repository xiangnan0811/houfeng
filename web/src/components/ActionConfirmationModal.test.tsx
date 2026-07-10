import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { ActionConfirmationModal } from './ActionConfirmationModal'
import { Modal } from './atoms'

function renderModal(overrides: Partial<Parameters<typeof ActionConfirmationModal>[0]> = {}) {
  return render(
    <ActionConfirmationModal
      open
      title="确认切换"
      current="当前：A"
      result="结果：B"
      impact="影响：立即生效"
      unchanged="不变：其他配置"
      confirmLabel="确认"
      onConfirm={vi.fn()}
      onCancel={vi.fn()}
      {...overrides}
    />,
  )
}

describe('ActionConfirmationModal', () => {
  it('renders a named alertdialog with the existing confirmation copy', () => {
    renderModal()

    expect(screen.getByRole('alertdialog', { name: '确认切换' })).toBeInTheDocument()
    expect(screen.getByText('操作确认')).toBeInTheDocument()
    expect(screen.getByText('当前：A')).toBeInTheDocument()
    expect(screen.getByText('结果：B')).toBeInTheDocument()
    expect(screen.getByText('影响：立即生效')).toBeInTheDocument()
    expect(screen.getByText('不变：其他配置')).toBeInTheDocument()
  })

  it('keeps errors inside the modal', () => {
    renderModal({ error: '操作失败' })

    const dialog = screen.getByRole('alertdialog', { name: '确认切换' })
    expect(dialog).toContainElement(screen.getByRole('alert'))
    expect(screen.getByText('操作失败')).toBeInTheDocument()
  })

  it('invokes cancel and confirm callbacks', () => {
    const onCancel = vi.fn()
    const onConfirm = vi.fn()
    renderModal({ onCancel, onConfirm })

    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    fireEvent.click(screen.getByRole('button', { name: '确认' }))

    expect(onCancel).toHaveBeenCalledTimes(1)
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('disables both actions while submitting', () => {
    renderModal({ disabled: true })

    expect(screen.getByRole('button', { name: '取消' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '确认' })).toBeDisabled()
  })

  it('closes only the confirmation layer and restores its parent trigger on Escape', async () => {
    function Harness() {
      const [parentOpen, setParentOpen] = useState(false)
      const [confirmOpen, setConfirmOpen] = useState(false)
      return (
        <>
          <button type="button" onClick={() => setParentOpen(true)}>打开详情</button>
          <Modal open={parentOpen} onClose={() => setParentOpen(false)} title="详情">
            <button type="button" onClick={() => setConfirmOpen(true)}>请求确认</button>
            <ActionConfirmationModal
              open={confirmOpen}
              title="确认嵌套操作"
              current="当前"
              result="之后"
              impact="影响"
              unchanged="其他不变"
              confirmLabel="确认操作"
              onConfirm={vi.fn()}
              onCancel={() => setConfirmOpen(false)}
            />
          </Modal>
        </>
      )
    }

    render(<Harness />)
    fireEvent.click(screen.getByRole('button', { name: '打开详情' }))
    const confirmationTrigger = screen.getByRole('button', { name: '请求确认' })
    confirmationTrigger.focus()
    fireEvent.click(confirmationTrigger)

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('alertdialog', { name: '确认嵌套操作' })).not.toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: '详情' })).toBeInTheDocument()
    await waitFor(() => expect(confirmationTrigger).toHaveFocus())
  })
})

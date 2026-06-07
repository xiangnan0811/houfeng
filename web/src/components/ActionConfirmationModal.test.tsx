import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ActionConfirmationModal } from './ActionConfirmationModal'

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
})

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { PromoteChecklistActionDialog } from './PromoteChecklistActionDialog'

describe('PromoteChecklistActionDialog', () => {
  it('requires explicit preview before creating an action and does not rewrite markdown', () => {
    const onConfirm = vi.fn()
    render(
      <PromoteChecklistActionDialog
        open
        source={'- [x] execute\n- [ ] verify'}
        onClose={vi.fn()}
        onConfirm={onConfirm}
      />,
    )
    expect(screen.queryByRole('button', { name: '确认创建行动项' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '预览行动项' }))
    expect(screen.getByRole('status')).toHaveTextContent('不会改写正文')
    fireEvent.click(screen.getByRole('button', { name: '确认创建行动项' }))
    expect(onConfirm).toHaveBeenCalledWith({ title: 'execute', details: 'execute' })
  })
})

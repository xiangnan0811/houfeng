import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { insertMaterialToken } from '../../../lib/documentMarkdown'
import { RecordMaterialDrawer } from './RecordMaterialDrawer'

describe('RecordMaterialDrawer', () => {
  it('inserts authorized tokens and can remove current materials without rewriting history', () => {
    const onInsert = vi.fn()
    const onRemove = vi.fn()
    render(
      <RecordMaterialDrawer
        open
        onClose={vi.fn()}
        onInsert={onInsert}
        onRemove={onRemove}
        items={[
          { kind: 'evidence', id: 'ev_7K2P', label: '第三晚 TCP 观测', available: true },
          { kind: 'attachment', id: 'att_old', label: '失效附件', available: false },
        ]}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: '插入第三晚 TCP 观测' }))
    expect(onInsert).toHaveBeenCalledWith(expect.objectContaining({ id: 'ev_7K2P' }))
    fireEvent.click(screen.getByRole('button', { name: '移除第三晚 TCP 观测' }))
    expect(onRemove).toHaveBeenCalled()
    expect(screen.getByText('引用已失效')).toBeInTheDocument()
    expect(insertMaterialToken('', {
      kind: 'evidence', id: 'ev_7K2P', label: '第三晚 TCP 观测',
    })).toContain('houfeng-evidence:ev_7K2P')
  })

  it('disables insert and remove when the workspace is read-only', () => {
    const onInsert = vi.fn()
    const onRemove = vi.fn()
    render(
      <RecordMaterialDrawer
        open
        readOnly
        onClose={vi.fn()}
        onInsert={onInsert}
        onRemove={onRemove}
        items={[{ kind: 'evidence', id: 'ev_7K2P', label: '第三晚 TCP 观测', available: true }]}
      />,
    )
    expect(screen.getByRole('button', { name: '插入第三晚 TCP 观测' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '移除第三晚 TCP 观测' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '插入第三晚 TCP 观测' }))
    fireEvent.click(screen.getByRole('button', { name: '移除第三晚 TCP 观测' }))
    expect(onInsert).not.toHaveBeenCalled()
    expect(onRemove).not.toHaveBeenCalled()
  })
})

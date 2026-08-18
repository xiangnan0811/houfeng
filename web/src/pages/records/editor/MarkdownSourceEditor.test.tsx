import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { MarkdownSourceEditor } from './MarkdownSourceEditor'

describe('MarkdownSourceEditor', () => {
  it('exposes a controlled source surface in tests', () => {
    const onChange = vi.fn()
    render(<MarkdownSourceEditor value="# Details" onChange={onChange} />)
    const source = screen.getByRole('textbox', { name: 'Markdown 源文' })
    expect(source).toHaveValue('# Details')
    fireEvent.change(source, { target: { value: '# Next' } })
    expect(onChange).toHaveBeenCalledWith('# Next')
  })

  it('wraps the current selection from the toolbar without replacing the rest of the body', () => {
    const onChange = vi.fn()
    render(<MarkdownSourceEditor value="keep title" onChange={onChange} onInsertTemplate={vi.fn()} />)
    const source = screen.getByRole('textbox', { name: 'Markdown 源文' }) as HTMLTextAreaElement
    source.focus()
    source.setSelectionRange(5, 10)
    fireEvent.click(screen.getByRole('button', { name: '加粗' }))
    expect(onChange).toHaveBeenCalledWith('keep **title**')
    expect(screen.getByRole('button', { name: '插入模板' })).toBeInTheDocument()
  })

  it('saves on the source shortcut', () => {
    const onSave = vi.fn()
    render(<MarkdownSourceEditor value="body" onChange={vi.fn()} onSave={onSave} />)
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Markdown 源文' }), { key: 's', ctrlKey: true })
    expect(onSave).toHaveBeenCalled()
  })
})

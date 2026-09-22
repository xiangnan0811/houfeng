import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { SubjectActivityFilters } from './SubjectActivityFilters'

describe('SubjectActivityFilters', () => {
  it('offers only record-domain sources and record-lifecycle kinds on the records view', () => {
    render(
      <SubjectActivityFilters
        view="records"
        value={{}}
        onChange={vi.fn()}
      />,
    )
    const source = screen.getByLabelText('来源')
    expect(source).toHaveTextContent('全部来源')
    expect(source).toHaveTextContent('人工记录')
    expect(source).not.toHaveTextContent('命令审计')
    expect(source).not.toHaveTextContent('证据快照')
    const kinds = screen.getByLabelText('事件类型')
    expect(kinds).toHaveTextContent('记录修订')
    expect(kinds).not.toHaveTextContent('评论创建')
    expect(kinds).not.toHaveTextContent('待办创建')
    expect(kinds).not.toHaveTextContent('证据捕获')
  })

  it('offers only evidence-snapshot sources and hides event kinds on the evidence view', () => {
    render(
      <SubjectActivityFilters
        view="evidence"
        value={{}}
        onChange={vi.fn()}
      />,
    )
    const source = screen.getByLabelText('来源')
    expect(source).toHaveTextContent('证据快照')
    expect(source).not.toHaveTextContent('人工记录')
    expect(source).not.toHaveTextContent('命令审计')
    expect(screen.queryByLabelText('事件类型')).not.toBeInTheDocument()
  })

  it('keeps an out-of-view current source visible and disabled on the records view', () => {
    render(
      <SubjectActivityFilters
        view="records"
        value={{ source: ['command_audit'] }}
        onChange={vi.fn()}
      />,
    )
    const source = screen.getByLabelText('来源')
    expect(source).toHaveValue('command_audit')
    const option = screen.getByRole('option', { name: '命令审计' })
    expect(option).toBeDisabled()
    expect(screen.getByRole('option', { name: '人工记录' })).not.toBeDisabled()
  })

  it('shows a carried event-kind on evidence with only evidence_captured selectable', () => {
    const onChange = vi.fn()
    render(
      <SubjectActivityFilters
        view="evidence"
        value={{ event_kind: ['command_executed'] }}
        onChange={onChange}
      />,
    )
    const kinds = screen.getByLabelText('事件类型')
    expect(kinds).toHaveValue('command_executed')
    expect(screen.getByRole('option', { name: '命令执行' })).toBeDisabled()
    expect(screen.getByRole('option', { name: '证据捕获' })).not.toBeDisabled()
    fireEvent.change(kinds, { target: { value: '' } })
    expect(onChange).toHaveBeenCalledWith({})
  })


})

import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { Input } from './Input'

describe('Input', () => {
  it('renders label and input', () => {
    render(<Input label="用户名" />)
    expect(screen.getByLabelText('用户名')).toBeInTheDocument()
  })

  it('shows error message and applies error class', () => {
    render(<Input label="X" error="格式不正确" />)
    expect(screen.getByText('格式不正确')).toBeInTheDocument()
    expect(screen.getByRole('textbox')).toHaveClass('input--error')
  })

  it('shows hint when no error', () => {
    render(<Input label="X" hint="可填中文" />)
    expect(screen.getByText('可填中文')).toBeInTheDocument()
  })

  it('forwards onChange', () => {
    const onChange = vi.fn()
    render(<Input label="X" onChange={onChange} />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'a' } })
    expect(onChange).toHaveBeenCalled()
  })
})

import { createRef } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { Input } from './Input'

describe('Input', () => {
  it('passes required to the native input and marks the label', () => {
    render(<Input label="用户名" required />)

    expect(screen.getByLabelText('用户名')).toBeRequired()
    expect(screen.getByText('用户名')).toHaveClass('input-field__label--required')
  })

  it('associates a generated hint id with the input', () => {
    render(<Input label="用户名" hint="可填中文" />)

    const input = screen.getByRole('textbox')
    const hint = screen.getByText('可填中文')
    expect(input.id).not.toBe('')
    expect(hint).toHaveAttribute('id', `${input.id}-hint`)
    expect(input).toHaveAttribute('aria-describedby', `${input.id}-hint`)
    expect(input).toHaveAccessibleDescription('可填中文')
  })

  it('associates an explicit error id, hides the hint, and marks the input invalid', () => {
    render(<Input id="username" label="用户名" hint="可填中文" error="格式不正确" aria-invalid="false" />)

    const input = screen.getByRole('textbox')
    const error = screen.getByText('格式不正确')
    expect(error).toHaveAttribute('id', 'username-error')
    expect(input).toHaveAttribute('aria-describedby', 'username-error')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveAccessibleDescription('格式不正确')
    expect(input).toHaveClass('input--error')
    expect(screen.queryByText('可填中文')).not.toBeInTheDocument()
  })

  it('merges caller description ids before the visible hint and removes duplicates', () => {
    render(
      <>
        <p id="external">外部说明</p>
        <p id="shared">共享说明</p>
        <Input
          id="username"
          label="用户名"
          hint="内部提示"
          aria-describedby="external username-hint shared external"
        />
      </>,
    )

    const input = screen.getByRole('textbox')
    expect(input).toHaveAttribute('aria-describedby', 'external username-hint shared')
    expect(input).toHaveAccessibleDescription('外部说明 内部提示 共享说明')
  })

  it('preserves a caller invalid state when no error is present', () => {
    render(<Input label="用户名" aria-invalid="spelling" />)

    expect(screen.getByRole('textbox')).toHaveAttribute('aria-invalid', 'spelling')
  })

  it('forwards the ref to the native input', () => {
    const ref = createRef<HTMLInputElement>()
    render(<Input ref={ref} label="用户名" />)

    expect(ref.current).toBe(screen.getByRole('textbox'))
    expect(ref.current).toBeInstanceOf(HTMLInputElement)
  })

  it('forwards native attributes, class names, and onChange', () => {
    const onChange = vi.fn()
    render(<Input label="用户名" name="username" className="custom-input" data-scope="profile" onChange={onChange} />)

    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'a' } })
    expect(input).toHaveAttribute('name', 'username')
    expect(input).toHaveAttribute('data-scope', 'profile')
    expect(input).toHaveClass('input', 'custom-input')
    expect(onChange).toHaveBeenCalled()
  })
})

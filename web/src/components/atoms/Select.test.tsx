import { createRef } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Select } from './Select'

describe('Select', () => {
  it('passes required to the native select and marks the label', () => {
    render(<Select label="地区" required options={[{ value: 'cn', label: '中国' }]} />)

    expect(screen.getByLabelText('地区')).toBeRequired()
    expect(screen.getByText('地区')).toHaveClass('input-field__label--required')
  })

  it('associates a generated hint id with the select', () => {
    render(<Select label="地区" hint="请选择常驻地区" options={[{ value: 'cn', label: '中国' }]} />)

    const select = screen.getByRole('combobox')
    const hint = screen.getByText('请选择常驻地区')
    expect(select.id).not.toBe('')
    expect(hint).toHaveAttribute('id', `${select.id}-hint`)
    expect(select).toHaveAttribute('aria-describedby', `${select.id}-hint`)
    expect(select).toHaveAccessibleDescription('请选择常驻地区')
  })

  it('associates an explicit error id, hides the hint, and marks the select invalid', () => {
    render(
      <Select
        id="region"
        label="地区"
        hint="请选择常驻地区"
        error="地区无效"
        aria-invalid="false"
        options={[{ value: 'cn', label: '中国' }]}
      />,
    )

    const select = screen.getByRole('combobox')
    const error = screen.getByText('地区无效')
    expect(error).toHaveAttribute('id', 'region-error')
    expect(select).toHaveAttribute('aria-describedby', 'region-error')
    expect(select).toHaveAttribute('aria-invalid', 'true')
    expect(select).toHaveAccessibleDescription('地区无效')
    expect(select).toHaveClass('input--error')
    expect(screen.queryByText('请选择常驻地区')).not.toBeInTheDocument()
  })

  it('merges caller description ids before the visible hint and removes duplicates', () => {
    render(
      <>
        <p id="external">外部说明</p>
        <p id="shared">共享说明</p>
        <Select
          id="region"
          label="地区"
          hint="内部提示"
          aria-describedby="external region-hint shared external"
          options={[{ value: 'cn', label: '中国' }]}
        />
      </>,
    )

    const select = screen.getByRole('combobox')
    expect(select).toHaveAttribute('aria-describedby', 'external region-hint shared')
    expect(select).toHaveAccessibleDescription('外部说明 内部提示 共享说明')
  })

  it('preserves a caller invalid state when no error is present', () => {
    render(<Select label="地区" aria-invalid="spelling" options={[{ value: 'cn', label: '中国' }]} />)

    expect(screen.getByRole('combobox')).toHaveAttribute('aria-invalid', 'spelling')
  })

  it('forwards the ref to the native select', () => {
    const ref = createRef<HTMLSelectElement>()
    render(<Select ref={ref} label="地区" options={[{ value: 'cn', label: '中国' }]} />)

    expect(ref.current).toBe(screen.getByRole('combobox'))
    expect(ref.current).toBeInstanceOf(HTMLSelectElement)
  })

  it('supports options and forwards native attributes, class names, and onChange', () => {
    const onChange = vi.fn()
    render(
      <Select
        label="地区"
        name="region"
        className="custom-select"
        data-scope="profile"
        options={[
          { value: 'cn', label: '中国' },
          { value: 'jp', label: '日本' },
        ]}
        onChange={onChange}
      />,
    )

    const select = screen.getByRole('combobox')
    fireEvent.change(select, { target: { value: 'jp' } })
    expect(select).toHaveValue('jp')
    expect(select).toHaveAttribute('name', 'region')
    expect(select).toHaveAttribute('data-scope', 'profile')
    expect(select).toHaveClass('input', 'custom-select')
    expect(onChange).toHaveBeenCalled()
  })

  it('supports caller-provided option children', () => {
    render(
      <Select label="地区">
        <option value="cn">中国</option>
        <option value="jp">日本</option>
      </Select>,
    )

    expect(screen.getByRole('option', { name: '中国' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: '日本' })).toBeInTheDocument()
  })
})

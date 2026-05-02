import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { FilterToggle } from './FilterToggle'

describe('FilterToggle', () => {
  it('renders the label text and reflects checked state', () => {
    render(<FilterToggle label="仅看异常" checked onChange={() => {}} />)
    expect(screen.getByText('仅看异常')).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: '仅看异常' })).toHaveAttribute(
      'aria-checked',
      'true',
    )
  })

  it('calls onChange with the inverse value when clicked', () => {
    const onChange = vi.fn()
    render(<FilterToggle label="仅看异常" checked={false} onChange={onChange} />)
    fireEvent.click(screen.getByRole('switch', { name: '仅看异常' }))
    expect(onChange).toHaveBeenCalledWith(true)
  })
})

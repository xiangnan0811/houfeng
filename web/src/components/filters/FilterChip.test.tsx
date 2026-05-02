import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { FilterChip } from './FilterChip'

describe('FilterChip', () => {
  it('renders the label and a remove button', () => {
    render(<FilterChip label="类型: service" onRemove={() => {}} />)
    expect(screen.getByText('类型: service')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: '移除筛选 类型: service' }),
    ).toBeInTheDocument()
  })

  it('invokes onRemove when the × button is clicked', () => {
    const onRemove = vi.fn()
    render(<FilterChip label="类型: service" onRemove={onRemove} />)
    fireEvent.click(screen.getByRole('button', { name: '移除筛选 类型: service' }))
    expect(onRemove).toHaveBeenCalledTimes(1)
  })
})

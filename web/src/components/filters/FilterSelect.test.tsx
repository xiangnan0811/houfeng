import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { FilterSelect } from './FilterSelect'

const OPTIONS = [
  { value: 'service', label: 'service' },
  { value: 'china_reference', label: 'china_reference' },
] as const

describe('FilterSelect', () => {
  it('renders the placeholder option for null value', () => {
    render(
      <FilterSelect label="类型" value={null} options={OPTIONS} onChange={() => {}} />,
    )
    const select = screen.getByLabelText('类型') as HTMLSelectElement
    expect(select.value).toBe('')
    expect(screen.getByRole('option', { name: '全部' })).toBeInTheDocument()
  })

  it('emits onChange with the selected value', () => {
    const onChange = vi.fn()
    render(
      <FilterSelect label="类型" value={null} options={OPTIONS} onChange={onChange} />,
    )
    fireEvent.change(screen.getByLabelText('类型'), { target: { value: 'service' } })
    expect(onChange).toHaveBeenCalledWith('service')
  })

  it('emits null when the placeholder option is picked', () => {
    const onChange = vi.fn()
    render(
      <FilterSelect label="类型" value="service" options={OPTIONS} onChange={onChange} />,
    )
    fireEvent.change(screen.getByLabelText('类型'), { target: { value: '' } })
    expect(onChange).toHaveBeenCalledWith(null)
  })
})

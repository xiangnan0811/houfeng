import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { FilterMultiSelect } from './FilterMultiSelect'

const OPTIONS = [
  { value: 'edge', label: 'edge' },
  { value: 'core', label: 'core' },
] as const

describe('FilterMultiSelect', () => {
  it('shows summary "全部" when no values selected and is closed by default', () => {
    render(
      <FilterMultiSelect label="标签" values={[]} options={OPTIONS} onChange={() => {}} />,
    )
    expect(screen.getByRole('button', { name: '全部' })).toBeInTheDocument()
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('shows count summary when values are selected', () => {
    render(
      <FilterMultiSelect
        label="标签"
        values={['edge']}
        options={OPTIONS}
        onChange={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: '已选 1' })).toBeInTheDocument()
  })

  it('opens popover and toggles a value', () => {
    const onChange = vi.fn()
    render(
      <FilterMultiSelect
        label="标签"
        values={[]}
        options={OPTIONS}
        onChange={onChange}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: '全部' }))
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('edge'))
    expect(onChange).toHaveBeenCalledWith(['edge'])
  })

  it('removes a value when toggled off', () => {
    const onChange = vi.fn()
    render(
      <FilterMultiSelect
        label="标签"
        values={['edge', 'core']}
        options={OPTIONS}
        onChange={onChange}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: '已选 2' }))
    fireEvent.click(screen.getByLabelText('edge'))
    expect(onChange).toHaveBeenCalledWith(['core'])
  })

  it('renders the empty label when no options', () => {
    render(
      <FilterMultiSelect
        label="标签"
        values={[]}
        options={[]}
        onChange={() => {}}
        emptyLabel="尚无标签"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: '全部' }))
    expect(screen.getByText('尚无标签')).toBeInTheDocument()
  })
})

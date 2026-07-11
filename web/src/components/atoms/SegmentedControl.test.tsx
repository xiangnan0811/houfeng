import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { SegmentedControl } from './SegmentedControl'

const items = [
  { value: 'all', label: '全部' },
  { value: 'unlinked', label: '未关联', count: 3 },
  { value: 'empty', label: '空分组', count: 0 },
] as const

describe('SegmentedControl', () => {
  it('renders a named group of native pressed buttons without tab roles', () => {
    render(<SegmentedControl label="VPS 快速视图" items={items} value="all" onChange={() => {}} />)

    const group = screen.getByRole('group', { name: 'VPS 快速视图' })
    expect(group).toHaveClass('tabs--pill')
    const buttons = screen.getAllByRole('button')
    expect(buttons).toHaveLength(3)
    expect(buttons[0]).toHaveAttribute('aria-pressed', 'true')
    expect(buttons[1]).toHaveAttribute('aria-pressed', 'false')
    expect(buttons.map((button) => button.tabIndex)).toEqual([0, 0, 0])
    expect(screen.queryByRole('tablist')).not.toBeInTheDocument()
    expect(screen.queryByRole('tab')).not.toBeInTheDocument()
    expect(screen.queryByRole('tabpanel')).not.toBeInTheDocument()
  })

  it('passes the generic value to onChange when clicked', () => {
    const onChange = vi.fn()
    render(<SegmentedControl label="VPS 快速视图" items={items} value="all" onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: /未关联/ }))
    expect(onChange).toHaveBeenCalledTimes(1)
    expect(onChange).toHaveBeenCalledWith('unlinked')
  })

  it('renders only positive count badges and includes them in accessible names', () => {
    render(<SegmentedControl label="VPS 快速视图" items={items} value="all" onChange={() => {}} />)

    expect(screen.getByRole('button', { name: /未关联.*3/ })).toBeInTheDocument()
    expect(screen.getByText('3')).toHaveClass('badge--count')
    expect(screen.getByRole('button', { name: '空分组' })).toBeInTheDocument()
    expect(screen.queryByText('0')).not.toBeInTheDocument()
  })
})

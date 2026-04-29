import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { Tabs } from './Tabs'

const items = [
  { value: 'a', label: '概览' },
  { value: 'b', label: '指标趋势' },
  { value: 'c', label: '活跃异常', count: 2 },
]

describe('Tabs', () => {
  it('renders all tabs and marks active', () => {
    render(<Tabs items={items} value="a" onChange={() => {}} />)
    const active = screen.getByRole('tab', { selected: true })
    expect(active).toHaveTextContent('概览')
  })

  it('calls onChange on click', () => {
    const onChange = vi.fn()
    render(<Tabs items={items} value="a" onChange={onChange} />)
    fireEvent.click(screen.getByText('指标趋势'))
    expect(onChange).toHaveBeenCalledWith('b')
  })

  it('renders count badge with count > 0', () => {
    render(<Tabs items={items} value="a" onChange={() => {}} />)
    expect(screen.getByText('2')).toHaveClass('badge--count')
  })

  it('pill variant uses pill class', () => {
    render(<Tabs items={items} value="a" onChange={() => {}} variant="pill" />)
    expect(screen.getByRole('tablist')).toHaveClass('tabs--pill')
  })
})

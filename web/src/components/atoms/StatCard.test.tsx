import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { StatCard } from './StatCard'

describe('StatCard', () => {
  it('renders value and label inside a card (non-interactive)', () => {
    render(<StatCard value={12} label="异常监控实例" />)
    expect(screen.getByText('12')).toHaveClass('stat-value')
    expect(screen.getByText('异常监控实例')).toHaveClass('stat-label')
    // Non-interactive cards must not expose a button role.
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('applies tone class to the value', () => {
    render(<StatCard value={3} label="续费" tone="warn" />)
    expect(screen.getByText('3')).toHaveClass('stat-value', 'is-warn')
  })

  it('renders a real button with type=button when onClick is provided', () => {
    const onClick = vi.fn()
    render(<StatCard value={5} label="异常" tone="err" onClick={onClick} />)
    const btn = screen.getByRole('button')
    expect(btn).toHaveAttribute('type', 'button')
    expect(btn).toHaveClass('stat', 'stat--clickable')
  })

  it('invokes onClick when the interactive card is clicked', () => {
    const onClick = vi.fn()
    render(<StatCard value={5} label="异常" onClick={onClick} />)
    fireEvent.click(screen.getByRole('button'))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('renders sub content when provided', () => {
    render(<StatCard value={5} label="月均成本" sub="年化 120" />)
    expect(screen.getByText('年化 120')).toHaveClass('stat-sub')
  })
})

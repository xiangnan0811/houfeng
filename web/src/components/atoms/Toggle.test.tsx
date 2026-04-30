import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { Toggle } from './Toggle'

describe('Toggle', () => {
  it('renders aria-checked', () => {
    render(<Toggle checked label="启用" onChange={() => {}} />)
    const t = screen.getByRole('switch', { name: '启用' })
    expect(t).toHaveAttribute('aria-checked', 'true')
  })

  it('clicking calls onChange with inverse', () => {
    const onChange = vi.fn()
    render(<Toggle checked={false} label="X" onChange={onChange} />)
    fireEvent.click(screen.getByRole('switch'))
    expect(onChange).toHaveBeenCalledWith(true)
  })

  it('respects disabled', () => {
    render(<Toggle checked={false} label="X" disabled onChange={() => {}} />)
    expect(screen.getByRole('switch')).toBeDisabled()
  })
})

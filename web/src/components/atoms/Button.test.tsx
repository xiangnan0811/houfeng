import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Button } from './Button'

describe('Button', () => {
  it('renders children', () => {
    render(<Button>登录</Button>)
    expect(screen.getByRole('button', { name: '登录' })).toBeInTheDocument()
  })

  it('applies variant class', () => {
    render(<Button variant="danger">清空</Button>)
    expect(screen.getByRole('button')).toHaveClass('danger')
  })

  it('applies size class', () => {
    render(<Button size="sm">小</Button>)
    expect(screen.getByRole('button')).toHaveClass('sm')
  })

  it('default variant is primary, default size is md', () => {
    render(<Button>X</Button>)
    const b = screen.getByRole('button')
    expect(b).toHaveClass('primary')
    expect(b).toHaveClass('md')
  })

  it('disabled passes through', () => {
    render(<Button disabled>X</Button>)
    expect(screen.getByRole('button')).toBeDisabled()
  })

  it('default type is button (not submit)', () => {
    render(<Button>X</Button>)
    expect(screen.getByRole('button')).toHaveAttribute('type', 'button')
  })
})

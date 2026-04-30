import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Card } from './Card'

describe('Card', () => {
  it('default role', () => {
    render(<Card>X</Card>)
    expect(screen.getByText('X')).toHaveClass('card', 'card--default')
  })

  it('state card with tone', () => {
    render(
      <Card cardRole="state" tone="alert">
        X
      </Card>,
    )
    expect(screen.getByText('X')).toHaveClass('card--state', 'tone--alert')
  })

  it('accent card', () => {
    render(<Card cardRole="accent">X</Card>)
    expect(screen.getByText('X')).toHaveClass('card--accent')
  })

  it('warning card', () => {
    render(<Card cardRole="warning">X</Card>)
    expect(screen.getByText('X')).toHaveClass('card--warning')
  })
})

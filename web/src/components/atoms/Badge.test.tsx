import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Badge } from './Badge'

describe('Badge', () => {
  it('state variant with critical tone', () => {
    render(
      <Badge variant="state" tone="critical">
        严重
      </Badge>,
    )
    const el = screen.getByText('严重')
    expect(el).toHaveClass('badge--state', 'tone--critical')
  })

  it('info variant default', () => {
    render(<Badge>tcp</Badge>)
    expect(screen.getByText('tcp')).toHaveClass('badge--info')
  })

  it('count variant', () => {
    render(
      <Badge variant="count" tone="critical">
        3
      </Badge>,
    )
    expect(screen.getByText('3')).toHaveClass('badge--count')
  })

  it('renders dot when withDot=true', () => {
    const { container } = render(
      <Badge variant="state" tone="alert" withDot>
        告警
      </Badge>,
    )
    expect(container.querySelector('.badge__dot')).toBeInTheDocument()
  })
})

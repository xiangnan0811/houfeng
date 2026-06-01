import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Hostname, MonoDigits, Timestamp } from './Mono'

describe('MonoDigits', () => {
  it('wraps content with mono + tnum classes', () => {
    const { container } = render(<MonoDigits>{42.7}</MonoDigits>)
    const span = container.querySelector('.mono-digits')!
    expect(span.classList.contains('mono')).toBe(true)
    expect(span.classList.contains('tnum')).toBe(true)
    expect(span.textContent).toBe('42.7')
  })
})

describe('Hostname', () => {
  it('renders full text by default', () => {
    render(<Hostname>{'mi_89b816442244efb4'}</Hostname>)
    expect(screen.getByText('mi_89b816442244efb4')).toBeInTheDocument()
  })

  it('truncates with ellipsis when truncate + maxChars set', () => {
    render(
      <Hostname truncate maxChars={6}>
        {'mi_89b816442244efb4'}
      </Hostname>,
    )
    expect(screen.getByText('mi_89b…')).toBeInTheDocument()
  })

  it('keeps full id in title attribute', () => {
    render(
      <Hostname truncate maxChars={6}>
        {'mi_89b816442244efb4'}
      </Hostname>,
    )
    const span = screen.getByText('mi_89b…')
    expect(span.getAttribute('title')).toBe('mi_89b816442244efb4')
  })
})

describe('Timestamp', () => {
  const ref = new Date('2026-04-30T16:00:00Z')

  it('renders absolute by default', () => {
    const { container } = render(
      <Timestamp value="2026-04-30T15:33:00Z" mode="absolute" now={ref} />,
    )
    const span = container.querySelector('.timestamp--absolute')!
    expect(span.textContent).toMatch(/2026\/04\/30/)
  })

  it('renders relative form', () => {
    const { container } = render(
      <Timestamp value="2026-04-30T15:33:00Z" mode="relative" now={ref} />,
    )
    const span = container.querySelector('.timestamp--relative')!
    expect(span.textContent).toMatch(/分钟前/)
  })

  it('renders em-dash for null', () => {
    render(<Timestamp value={null} />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('renders em-dash for invalid date', () => {
    render(<Timestamp value="not-a-date" />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })
})

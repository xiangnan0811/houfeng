import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { SyncStatus } from './SyncStatus'

describe('SyncStatus', () => {
  it('shows ok with version and timestamp', () => {
    const { container } = render(
      <SyncStatus state="ok" version="v1.0" lastSync="2026-04-29T14:32:01Z" />,
    )
    expect(screen.getByText('中心运行正常')).toBeInTheDocument()
    expect(container.textContent).toMatch(/v1\.0/)
  })

  it('shows degraded label', () => {
    render(<SyncStatus state="degraded" version="v1.0" lastSync="2026-04-29T14:32:01Z" />)
    expect(screen.getByText('中心运行降级')).toBeInTheDocument()
  })

  it('shows down label', () => {
    render(<SyncStatus state="down" version="v1.0" lastSync="2026-04-29T14:32:01Z" />)
    expect(screen.getByText('中心不可达')).toBeInTheDocument()
  })
})

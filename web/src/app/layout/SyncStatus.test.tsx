import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { SyncStatus } from './SyncStatus'

describe('SyncStatus', () => {
  it('shows custom label and meta without fabricating sync copy', () => {
    const { container } = render(
      <SyncStatus state="ok" label="摘要已加载" meta="v1.0 · dashboard 14:32:01" />,
    )
    expect(screen.getByText('摘要已加载')).toBeInTheDocument()
    expect(screen.getByText('v1.0 · dashboard 14:32:01')).toBeInTheDocument()
    expect(container.textContent).not.toMatch(/中心运行正常|sync/)
  })

  it('keeps the ok state class contract', () => {
    const { container } = render(
      <SyncStatus state="ok" label="摘要已加载" meta="v1.0 · dashboard 14:32:01" />,
    )
    expect(container.firstElementChild).toHaveClass('sync-status', 'sync-status--ok')
  })

  it('keeps the degraded state class contract', () => {
    const { container } = render(
      <SyncStatus state="degraded" label="正在读取系统摘要" meta="v1.0 · dashboard loading" />,
    )
    expect(container.firstElementChild).toHaveClass('sync-status', 'sync-status--degraded')
  })

  it('keeps the down state class contract', () => {
    const { container } = render(
      <SyncStatus state="down" label="摘要不可用" meta="v1.0 · dashboard unavailable" />,
    )
    expect(container.firstElementChild).toHaveClass('sync-status', 'sync-status--down')
  })
})

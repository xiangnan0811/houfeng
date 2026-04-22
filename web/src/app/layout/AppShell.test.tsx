import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { AppShell } from './AppShell'

describe('AppShell', () => {
  it('renders the frozen top-level navigation labels', () => {
    render(
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: '集群概览' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '节点' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '目标' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '事件' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '设置' })).toBeInTheDocument()
  })
})

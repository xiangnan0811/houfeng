import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { Breadcrumb } from './Breadcrumb'

function renderAt(path: string, routePattern: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path={routePattern} element={<Breadcrumb />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('Breadcrumb', () => {
  it('hides on root', () => {
    const { container } = renderAt('/', '/')
    expect(container.querySelector('.breadcrumb')).toBeNull()
  })

  it('hides on level-1 routes (no duplicate links with sidebar)', () => {
    const { container } = renderAt('/nodes', '/nodes')
    expect(container.querySelector('.breadcrumb')).toBeNull()
  })

  it('renders parent link + current id on /nodes/:id', () => {
    renderAt('/nodes/nd_001', '/nodes/:nodeId')
    const link = screen.getByRole('link', { name: '节点' })
    expect(link).toHaveAttribute('href', '/nodes')
    expect(screen.getByText('nd_001')).toBeInTheDocument()
  })

  it('renders three segments on /nodes/:id/onboarding', () => {
    renderAt('/nodes/nd_001/onboarding', '/nodes/:nodeId/onboarding')
    expect(screen.getByRole('link', { name: '节点' })).toHaveAttribute('href', '/nodes')
    expect(screen.getByRole('link', { name: 'nd_001' })).toHaveAttribute(
      'href',
      '/nodes/nd_001',
    )
    expect(screen.getByText('接入工作台')).toBeInTheDocument()
  })

  it('truncates long detail ids in the current segment', () => {
    renderAt('/nodes/nd_8901234567890123456', '/nodes/:nodeId')
    expect(screen.getByText(/^nd_8901234567…$/)).toBeInTheDocument()
  })
})

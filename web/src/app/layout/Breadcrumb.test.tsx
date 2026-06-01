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
    const { container } = renderAt('/monitoring', '/monitoring')
    expect(container.querySelector('.breadcrumb')).toBeNull()
  })

  it('renders parent link + current id on /monitoring/:id', () => {
    renderAt('/monitoring/mi_001', '/monitoring/:monitoringInstanceId')
    const link = screen.getByRole('link', { name: '监控' })
    expect(link).toHaveAttribute('href', '/monitoring')
    expect(screen.getByText('mi_001')).toBeInTheDocument()
  })

  it('renders parent link + current id on /vps/:id', () => {
    renderAt('/vps/vps_tokyo_001', '/vps/:vpsId')
    const link = screen.getByRole('link', { name: 'VPS' })
    expect(link).toHaveAttribute('href', '/vps')
    expect(screen.getByText('vps_tokyo_001')).toBeInTheDocument()
  })

  it('uses current navigation wording on nested observation routes', () => {
    renderAt('/targets/tg_001', '/targets/:targetId')
    expect(screen.getByRole('link', { name: '入口探测' })).toHaveAttribute('href', '/targets')
    expect(screen.queryByRole('link', { name: '目标' })).not.toBeInTheDocument()
  })

  it('hides on level-1 asset routes', () => {
    const { container } = renderAt('/asset-decisions', '/asset-decisions')
    expect(container.querySelector('.breadcrumb')).toBeNull()
  })

  it('truncates long detail ids in the current segment', () => {
    renderAt('/monitoring/mi_8901234567890123456', '/monitoring/:monitoringInstanceId')
    expect(screen.getByText(/^mi_8901234567…$/)).toBeInTheDocument()
  })
})

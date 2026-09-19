import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
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

  it('keeps records compare ahead of the dynamic record id', () => {
    renderAt('/records/compare', '/records/compare')
    expect(screen.getByRole('link', { name: '运维记录' })).toHaveAttribute('href', '/records')
    expect(screen.getByText('横向比较')).toBeInTheDocument()
  })

  it('adds a third crumb for subject activity routes', () => {
    renderAt('/vps/vps_tokyo_001/activity', '/vps/:vpsId/activity')
    expect(screen.getByRole('link', { name: 'VPS' })).toHaveAttribute('href', '/vps')
    expect(screen.getByRole('link', { name: 'vps_tokyo_001' })).toHaveAttribute(
      'href',
      '/vps/vps_tokyo_001',
    )
    expect(screen.getByText('活动')).toBeInTheDocument()
  })
  it('links monitoring crumb to validated monitoringListHref and carries location state on click', () => {
    const navState = {
      monitoringListHref: '/monitoring?view=abnormal&selected=mi_001&q=Tokyo&sort=name_asc',
      return_vps: 'vps_001',
      vpsInventoryHref: '/vps?workspace=workbench',
    }

    function LocationProbe() {
      const loc = useLocation()
      return (
        <div data-testid="probe" data-state={JSON.stringify(loc.state)}>
          {loc.pathname}{loc.search}
        </div>
      )
    }

    render(
      <MemoryRouter
        initialEntries={[{
          pathname: '/monitoring/mi_001',
          state: navState,
        }]}
      >
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<Breadcrumb />} />
          <Route path="/monitoring" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    const link = screen.getByRole('link', { name: '监控' })
    expect(link).toHaveAttribute(
      'href',
      '/monitoring?view=abnormal&selected=mi_001&q=Tokyo&sort=name_asc',
    )
    fireEvent.click(link)

    expect(screen.getByTestId('probe')).toHaveTextContent(
      '/monitoring?view=abnormal&selected=mi_001&q=Tokyo&sort=name_asc',
    )
    expect(JSON.parse(screen.getByTestId('probe').getAttribute('data-state')!)).toEqual(navState)
  })

  it.each([
    'https://example.invalid/monitoring',
    '/settings',
    '/monitoring/mi_001',
    '/monitoring#fragment',
  ])('falls back to /monitoring when location state is not a valid list href: %s', (monitoringListHref) => {
    render(
      <MemoryRouter
        initialEntries={[{
          pathname: '/monitoring/mi_001',
          state: { monitoringListHref },
        }]}
      >
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId" element={<Breadcrumb />} />
        </Routes>
      </MemoryRouter>,
    )
    const link = screen.getByRole('link', { name: '监控' })
    expect(link).toHaveAttribute('href', '/monitoring')
  })

  it('carries location state through nested detail crumb on activity routes', () => {
    const navState = {
      monitoringListHref: '/monitoring?view=abnormal&selected=mi_001',
      vpsInventoryHref: '/vps?workspace=workbench',
      return_vps: 'vps_SHOULD_NOT_USE',
    }

    function DetailProbe() {
      const loc = useLocation()
      return (
        <div data-testid="detail-probe" data-state={JSON.stringify(loc.state)}>
          {loc.pathname}{loc.search}
        </div>
      )
    }

    render(
      <MemoryRouter
        initialEntries={[{
          pathname: '/monitoring/mi_001/activity',
          search: '?return_vps=vps_tokyo_origin&window=7d&cursor=opaque',
          state: navState,
        }]}
      >
        <Routes>
          <Route path="/monitoring/:monitoringInstanceId/activity" element={<Breadcrumb />} />
          <Route path="/monitoring/:monitoringInstanceId" element={<DetailProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    const monitoringLink = screen.getByRole('link', { name: '监控' })
    expect(monitoringLink).toHaveAttribute('href', '/monitoring?view=abnormal&selected=mi_001')

    const detailLink = screen.getByRole('link', { name: 'mi_001' })
    expect(detailLink).toHaveAttribute('href', '/monitoring/mi_001?return_vps=vps_tokyo_origin')
    fireEvent.click(detailLink)

    expect(screen.getByTestId('detail-probe')).toHaveTextContent('/monitoring/mi_001?return_vps=vps_tokyo_origin')
    expect(screen.getByTestId('detail-probe')).not.toHaveTextContent('window=')
    expect(screen.getByTestId('detail-probe')).not.toHaveTextContent('cursor=')
    expect(JSON.parse(screen.getByTestId('detail-probe').getAttribute('data-state')!)).toEqual(navState)
  })
})

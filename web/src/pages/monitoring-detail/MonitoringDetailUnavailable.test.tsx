import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { MonitoringDetailUnavailable } from './MonitoringDetailUnavailable'

describe('MonitoringDetailUnavailable', () => {
  it('renders standard fallback without return action when no origin is present', () => {
    render(
      <MemoryRouter initialEntries={['/monitoring/mi_missing']}>
        <Routes>
          <Route path="/monitoring/:id" element={<MonitoringDetailUnavailable message="未找到监控实例" />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { name: '监控实例详情不可用' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回监控实例列表' })).toHaveAttribute('href', '/monitoring')
    expect(screen.queryByRole('link', { name: '返回来源 VPS' })).not.toBeInTheDocument()
  })

  it('renders "返回来源 VPS" when returnVPSId prop is provided', () => {
    const navState = { from: 'vps-workbench' }
    render(
      <MemoryRouter initialEntries={[{ pathname: '/monitoring/mi_missing', state: navState }]}>
        <Routes>
          <Route
            path="/monitoring/:id"
            element={<MonitoringDetailUnavailable message="未找到监控实例" returnVPSId="vps_origin_001" />}
          />
        </Routes>
      </MemoryRouter>,
    )

    const returnLink = screen.getByRole('link', { name: '返回来源 VPS' })
    expect(returnLink).toHaveAttribute('href', '/vps/vps_origin_001')
    expect(screen.getByRole('link', { name: '返回监控实例列表' })).toHaveAttribute('href', '/monitoring')
  })

  it('renders standard fallback when returnVPSId is null', () => {
    render(
      <MemoryRouter initialEntries={['/monitoring/mi_missing']}>
        <Routes>
          <Route path="/monitoring/:id" element={<MonitoringDetailUnavailable message="未找到监控实例" returnVPSId={null} />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(screen.queryByRole('link', { name: '返回来源 VPS' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回监控实例列表' })).toHaveAttribute('href', '/monitoring')
  })
})

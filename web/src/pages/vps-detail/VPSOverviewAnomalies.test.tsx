import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { VPSOverviewAnomaly } from '../../lib/types'
import { VPSOverviewAnomalies } from './VPSOverviewAnomalies'

function anomaly(overrides: Partial<VPSOverviewAnomaly> = {}): VPSOverviewAnomaly {
  return {
    rule_id: 'monitoring.health.abnormal.v1',
    severity: 'critical',
    title: '监控异常',
    source: 'monitoring',
    primary_action: {
      id: 'open_monitoring',
      label: '查看监控',
      route: '/monitoring?abnormal=1',
    },
    secondary_actions: [],
    ...overrides,
  }
}

describe('VPSOverviewAnomalies contract', () => {
  it('keeps healthy query counts at zero', () => {
    const { container, rerender } = render(
      <VPSOverviewAnomalies vpsId="vps_001" anomalies={[]} onCommand={vi.fn()} />,
    )
    expect(container.querySelectorAll('.vps-overview-anomalies').length).toBe(0)
    expect(container.querySelectorAll('[aria-labelledby="vps-overview-anomalies-title"]').length).toBe(0)
    expect(screen.queryByText('动作：无')).toBeNull()

    rerender(
      <MemoryRouter>
        <VPSOverviewAnomalies
          vpsId="vps_001"
          anomalies={[anomaly()]}
          onCommand={vi.fn()}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('heading', { name: '需要关注' })).toBeInTheDocument()

    rerender(<VPSOverviewAnomalies vpsId="vps_001" anomalies={[]} onCommand={vi.fn()} />)
    expect(container.querySelectorAll('.vps-overview-anomalies').length).toBe(0)
  })

  it('renders allowlisted routes as links and exact commands as buttons', () => {
    const onCommand = vi.fn()
    render(
      <MemoryRouter>
        <VPSOverviewAnomalies
          vpsId="vps_001"
          anomalies={[
            anomaly(),
            anomaly({
              rule_id: 'renewal.subscription.missing.v1',
              title: '缺少有效订阅',
              primary_action: { id: 'open_subscription', label: '管理订阅' },
            }),
          ]}
          onCommand={onCommand}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: '查看监控' })).toHaveAttribute(
      'href',
      '/monitoring?abnormal=1',
    )
    fireEvent.click(screen.getByRole('button', { name: '管理订阅' }))
    expect(onCommand).toHaveBeenCalledTimes(1)
    expect(onCommand).toHaveBeenCalledWith('open_subscription')
  })

  it('fails closed when the API route does not exactly match its stable token', () => {
    render(
      <MemoryRouter>
        <VPSOverviewAnomalies
          vpsId="vps_001"
          anomalies={[anomaly({
            primary_action: {
              id: 'open_monitoring',
              label: '恶意监控入口',
              route: '/vps/vps_001',
            },
          })]}
          onCommand={vi.fn()}
        />
      </MemoryRouter>,
    )

    expect(screen.getByText('恶意监控入口')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '恶意监控入口' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '恶意监控入口' })).not.toBeInTheDocument()
  })
})

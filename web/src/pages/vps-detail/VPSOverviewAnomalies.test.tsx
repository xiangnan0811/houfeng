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

  it('compacts an unlinked monitoring fact without repeating source chrome', () => {
    const onCommand = vi.fn()
    render(
      <MemoryRouter>
        <VPSOverviewAnomalies
          vpsId="vps_001"
          anomalies={[anomaly({
            rule_id: 'monitoring.unlinked.v1',
            severity: 'notice',
            title: '未关联监控实例',
            source: 'monitoring',
            primary_action: { id: 'open_monitoring_instances', label: '创建并接入 agent' },
          })]}
          onCommand={onCommand}
        />
      </MemoryRouter>,
    )
    expect(screen.queryByRole('heading', { name: '需要关注' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '未关联监控实例' })).toBeInTheDocument()
    expect(screen.queryByText('当前 VPS 没有关联监控实例。')).not.toBeInTheDocument()
    expect(screen.getByText('运行观测缺少心跳、健康与异常证据。')).toBeInTheDocument()
    expect(screen.queryByText('监控', { selector: '.vps-overview-anomalies__source' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '创建并接入 agent' }))
    expect(onCommand).toHaveBeenCalledWith('open_monitoring_onboarding')
  })

  it('keeps a distinct provided unlinked detail instead of the conservative fallback', () => {
    render(
      <MemoryRouter>
        <VPSOverviewAnomalies
          vpsId="vps_001"
          anomalies={[anomaly({
            rule_id: 'monitoring.unlinked.v1',
            title: '未关联监控实例',
            detail: '指定实例已解除，待人工确认是否重建。',
            source: 'monitoring',
            primary_action: { id: 'open_monitoring_instances', label: '创建并接入 agent' },
          })]}
          onCommand={vi.fn()}
        />
      </MemoryRouter>,
    )

    expect(screen.queryByRole('heading', { name: '需要关注' })).not.toBeInTheDocument()
    expect(screen.getByText('指定实例已解除，待人工确认是否重建。')).toBeInTheDocument()
    expect(screen.queryByText('当前 VPS 没有关联监控实例。')).not.toBeInTheDocument()
    expect(screen.queryByText('运行观测缺少心跳、健康与异常证据。')).not.toBeInTheDocument()
  })
})

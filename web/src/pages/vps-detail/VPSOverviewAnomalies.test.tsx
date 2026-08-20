import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { VPSOverviewAnomalies } from './VPSOverviewAnomalies'

describe('VPSOverviewAnomalies contract', () => {
  it('keeps healthy query counts at zero', () => {
    const { container, rerender } = render(<VPSOverviewAnomalies anomalies={[]} />)
    expect(container.querySelectorAll('.vps-overview-anomalies').length).toBe(0)
    expect(container.querySelectorAll('[aria-labelledby="vps-overview-anomalies-title"]').length).toBe(0)
    expect(screen.queryByText('动作：无')).toBeNull()

    rerender(
      <MemoryRouter>
        <VPSOverviewAnomalies
          anomalies={[{
            rule_id: 'monitoring.unhealthy',
            severity: 'critical',
            title: '监控异常',
            source: 'monitoring',
            secondary_actions: [],
          }]}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('heading', { name: '需要关注' })).toBeInTheDocument()

    rerender(<VPSOverviewAnomalies anomalies={[]} />)
    expect(container.querySelectorAll('.vps-overview-anomalies').length).toBe(0)
  })
})

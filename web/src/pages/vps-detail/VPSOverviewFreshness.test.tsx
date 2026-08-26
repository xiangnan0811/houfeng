import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { VPSOverviewFreshness } from './VPSOverviewFreshness'

describe('VPSOverviewFreshness', () => {
  it('shows a stale source with bounded reason, source times, and an accessible retry', () => {
    const retry = vi.fn()
    render(
      <VPSOverviewFreshness
        section={{
          state: 'stale',
          observed_at: '2026-08-24T07:00:00Z',
          last_success_at: '2026-08-24T06:30:00Z',
          reason_code: 'ip_quality_stale',
        }}
        sourceLabel="IP 质量"
        onRetry={retry}
        retrying={false}
      />,
    )

    expect(screen.getByText('数据陈旧')).toBeInTheDocument()
    expect(screen.getByText('IP 质量数据超过新鲜度阈值。')).toBeInTheDocument()
    expect(screen.getByText('观测')).toBeInTheDocument()
    expect(screen.getByText('最近成功')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试 IP 质量' }))
    expect(retry).toHaveBeenCalledTimes(1)
  })

  it('uses a generic unavailable explanation and never renders an unknown raw reason', () => {
    render(
      <VPSOverviewFreshness
        section={{
          state: 'unavailable',
          observed_at: null,
          last_success_at: null,
          reason_code: 'postgres_password=raw-secret',
        }}
        sourceLabel="服务"
        onRetry={vi.fn()}
        retrying
      />,
    )

    expect(screen.getByText('暂不可用')).toBeInTheDocument()
    expect(screen.getByText('服务数据暂不可用，请稍后重试。')).toBeInTheDocument()
    expect(screen.queryByText(/postgres_password|raw-secret/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试 服务' })).toBeDisabled()
  })

  it('shows ready state without a retry action', () => {
    render(
      <VPSOverviewFreshness
        section={{ state: 'ready', observed_at: null, last_success_at: null, reason_code: '' }}
        sourceLabel="监控"
        onRetry={vi.fn()}
        retrying={false}
      />,
    )

    expect(screen.getByText('数据正常')).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('notes leftover IP quality history without judging or claiming unavailability', () => {
    render(
      <VPSOverviewFreshness
        section={{
          state: 'ready',
          observed_at: null,
          last_success_at: null,
          reason_code: 'ip_quality_disabled_has_history',
        }}
        sourceLabel="IP 质量"
        onRetry={vi.fn()}
        retrying={false}
      />,
    )

    expect(screen.getByText('数据正常')).toBeInTheDocument()
    expect(screen.getByText('存在历史报告（当前未启用）。')).toBeInTheDocument()
    expect(screen.queryByText('暂不可用')).not.toBeInTheDocument()
    expect(screen.queryByText(/ip_quality_disabled_has_history/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('does not treat an unknown ready reason as a source failure', () => {
    render(
      <VPSOverviewFreshness
        section={{
          state: 'ready',
          observed_at: null,
          last_success_at: null,
          reason_code: 'future_non_judging_note',
        }}
        sourceLabel="IP 质量"
        onRetry={vi.fn()}
        retrying={false}
      />,
    )

    expect(screen.getByText('数据正常')).toBeInTheDocument()
    expect(screen.queryByText(/暂不可用/)).not.toBeInTheDocument()
    expect(screen.queryByText('future_non_judging_note')).not.toBeInTheDocument()
  })
})

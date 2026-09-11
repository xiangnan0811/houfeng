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
    expect(screen.getByText('数据时间')).toBeInTheDocument()
    expect(screen.getByText('最近成功')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '刷新 概览 IP 质量' }))

    expect(retry).toHaveBeenCalledTimes(1)
  })

  it('never renders an unknown raw reason as a failed-read explanation', () => {
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
    expect(screen.queryByText('服务数据暂不可用，请稍后重试。')).not.toBeInTheDocument()
    expect(screen.queryByText(/postgres_password|raw-secret/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试 概览 服务' })).toBeDisabled()

  })

  it('omits ready state when there is no extra evidence', () => {
    const { container } = render(
      <VPSOverviewFreshness
        section={{ state: 'ready', observed_at: null, last_success_at: null, reason_code: '' }}
        sourceLabel="监控"
        onRetry={vi.fn()}
        retrying={false}
      />,
    )

    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByText('数据正常')).not.toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('shows one quiet timestamp for a ready source with matching observed and last-success times', () => {
    render(
      <VPSOverviewFreshness
        section={{
          state: 'ready',
          observed_at: '2026-09-10T02:00:00Z',
          last_success_at: '2026-09-10T02:00:00Z',
          reason_code: '',
        }}
        sourceLabel="续费"
        onRetry={vi.fn()}
        retrying={false}
      />,
    )
    expect(screen.queryByText('观测')).not.toBeInTheDocument()
    expect(screen.queryByText('最近成功')).not.toBeInTheDocument()
    expect(screen.getByText(/续费更新/)).toBeInTheDocument()
    expect(screen.getByText(/2026/)).toBeInTheDocument()
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

    expect(screen.queryByText('数据正常')).not.toBeInTheDocument()
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

    expect(screen.queryByText('数据正常')).not.toBeInTheDocument()
    expect(screen.queryByText(/暂不可用/)).not.toBeInTheDocument()
    expect(screen.queryByText('future_non_judging_note')).not.toBeInTheDocument()
  })

  it('does not treat an unknown stale reason as a failed read', () => {
    render(
      <VPSOverviewFreshness
        section={{
          state: 'stale',
          observed_at: '2026-09-10T06:00:00Z',
          last_success_at: '2026-09-10T06:00:00Z',
          reason_code: 'sample_heartbeat_stale',
        }}
        sourceLabel="最近活动"
        retrying={false}
      />,
    )

    expect(screen.getByText('数据陈旧')).toBeInTheDocument()
    expect(screen.queryByText(/读取失败/)).not.toBeInTheDocument()
    expect(screen.queryByText(/暂不可用/)).not.toBeInTheDocument()
    expect(screen.queryByText('sample_heartbeat_stale')).not.toBeInTheDocument()
  })

  it('says last available result remains when a recognized source read fails', () => {
    render(
      <VPSOverviewFreshness
        section={{
          state: 'unavailable',
          observed_at: '2026-09-10T06:00:00Z',
          last_success_at: '2026-09-10T06:00:00Z',
          reason_code: 'ip_quality_timeout',
        }}
        sourceLabel="IP 质量"
        onRetry={vi.fn()}
        retrying={false}
        retained
      />,
    )

    expect(screen.getByText(/仍展示上次可用结果/)).toBeInTheDocument()
    expect(screen.getByText(/读取超时/)).toBeInTheDocument()
    expect(screen.queryByText('IP 质量数据暂不可用，请稍后重试。')).not.toBeInTheDocument()
  })
})

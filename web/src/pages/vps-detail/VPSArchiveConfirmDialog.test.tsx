import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { formatDateTime } from '../../lib/format'
import type { ArchiveReview } from '../../lib/types'
import { VPSArchiveConfirmDialog, type VPSArchiveConfirmDialogProps } from './VPSArchiveConfirmDialog'

const HEALTHY_SINCE = '2026-09-28T03:31:47.362882Z'
const EARLIEST = '2026-09-28T06:31:47.362882Z'
const LAST_ONLINE = '2026-09-28T02:11:47.362882Z'

function review(overrides: Partial<ArchiveReview> = {}): ArchiveReview {
  return {
    vps: { display_name: '东京边缘', vps_id: 'vps_a' },
    subscriptions: [],
    monitoring_instance_links: [],
    services: [],
    domains: [],
    target_links: [],
    warnings: [],
    blockers: [],
    blocker_details: [],
    eligible: true,
    preview_digest: 'digest',
    ...overrides,
  } as ArchiveReview
}

function renderDialog(overrides: Partial<VPSArchiveConfirmDialogProps> = {}) {
  const props: VPSArchiveConfirmDialogProps = {
    open: true,
    title: '结束使用并归档',
    confirmLabel: '结束使用并归档',
    submitting: false,
    displayName: '东京边缘',
    review: review(),
    loading: false,
    error: null,
    reason: '',
    confirmationName: '',
    neverConnectedConfirmed: false,
    confirmDisabled: true,
    onReasonChange: vi.fn(),
    onConfirmationNameChange: vi.fn(),
    onNeverConnectedChange: vi.fn(),
    onConfirm: vi.fn(),
    onCancel: vi.fn(),
    onRetry: vi.fn(),
    vpsId: 'vps_a',
    onInlineBlocker: vi.fn(),
    ...overrides,
  }
  return {
    props,
    ...render(
      <MemoryRouter>
        <VPSArchiveConfirmDialog {...props} />
      </MemoryRouter>,
    ),
  }
}

describe('VPSArchiveConfirmDialog', () => {
  it('keeps the never-connected confirmation inline and drops receiver-health noise', () => {
    const { props } = renderDialog({
      review: review({
        warnings: [
          '没有订阅记录；资源有效期与服务商扣费请独立核对。',
          '没有服务关联。',
          '没有域名关联。',
          '此 VPS 从未形成有效 Agent 会话，归档须人工确认并说明原因。',
        ],
        online_evidence: {
          observed_at: HEALTHY_SINCE,
          receiver_generation: 'boot',
          receiver_healthy: true,
          healthy_since: HEALTHY_SINCE,
          last_health_check_at: HEALTHY_SINCE,
          earliest_archive_at: null,
          never_connected: true,
          manual_confirmation_required: true,
          instances: [],
        },
      }),
    })

    const dialog = screen.getByRole('alertdialog', { name: '结束使用并归档' })
    expect(dialog.querySelector('.page-stack')).toBeNull()
    expect(dialog.querySelector('.asset-lifecycle-confirm')).toBeNull()
    expect(screen.queryByText('操作确认')).not.toBeInTheDocument()
    expect(screen.queryByText('归档审查')).not.toBeInTheDocument()
    const text = dialog.textContent ?? ''
    expect(text).not.toContain('接收链路')
    expect(text).not.toContain('等待安全观察')
    expect(text).not.toContain('最早可归档')
    expect(text).not.toContain(HEALTHY_SINCE)
    expect(text).not.toContain(formatDateTime(HEALTHY_SINCE))
    expect(text).not.toContain('此 VPS 从未形成有效 Agent 会话')
    expect(text).toContain('这台 VPS 从未接入过 Agent，不用等待 180 分钟安全观察。')
    expect(screen.getByText('没有订阅记录；资源有效期与服务商扣费请独立核对。')).toBeInTheDocument()
    expect(screen.queryByText('没有服务关联。')).not.toBeInTheDocument()
    expect(screen.getByText('无当前实例')).toBeInTheDocument()

    const checkbox = screen.getByRole('checkbox', { name: '这台 VPS 从未接入过 Agent。我已确认它不再使用。' })
    const label = checkbox.closest('label')
    expect(label).toHaveClass('asset-archive-dialog__check')
    expect(label).toContainElement(checkbox)
    expect(checkbox).toHaveAttribute('aria-required', 'true')
    expect(screen.getByText('必填')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '结束使用并归档' })).toBeDisabled()

    fireEvent.click(checkbox)
    expect(props.onNeverConnectedChange).toHaveBeenCalledWith(true)
    fireEvent.click(screen.getByRole('button', { name: '结束使用并归档' }))
    expect(props.onConfirm).not.toHaveBeenCalled()
  })

  it('states connected evidence once and keeps unrelated notes informational', () => {
    renderDialog({
      confirmDisabled: false,
      review: review({
        warnings: ['订阅仍可能产生费用，归档不会更改服务商续费事实。'],
        services: [{ name: '边缘网关' } as ArchiveReview['services'][number]],
        domains: [{ domain_name: 'edge.example.com' } as ArchiveReview['domains'][number]],
        monitoring_instance_links: [
          { monitoring_instance_id: 'mi_1', display_name: '东京探针', lifecycle_status: '在用' },
          { monitoring_instance_id: 'mi_old', display_name: '已退役探针', lifecycle_status: '已退役' },
        ] as ArchiveReview['monitoring_instance_links'],
        target_links: [{ name: '专属探测' } as ArchiveReview['target_links'][number]],
        online_evidence: {
          observed_at: HEALTHY_SINCE,
          receiver_generation: 'boot',
          receiver_healthy: true,
          healthy_since: HEALTHY_SINCE,
          last_health_check_at: HEALTHY_SINCE,
          earliest_archive_at: EARLIEST,
          never_connected: false,
          manual_confirmation_required: false,
          instances: [{
            monitoring_instance_id: 'mi_1',
            session_id: 'sess_1',
            ever_connected: true,
            last_trusted_online_at: LAST_ONLINE,
          }],
        },
      }),
    })

    const dialog = screen.getByRole('alertdialog', { name: '结束使用并归档' })
    const text = dialog.textContent ?? ''
    expect(text.split('接收链路').length - 1).toBe(1)
    expect(text.split('最早可').length - 1).toBe(1)
    expect(text.split(formatDateTime(HEALTHY_SINCE)).length - 1).toBe(1)
    expect(text.split(formatDateTime(EARLIEST)).length - 1).toBe(1)
    expect(text.split(formatDateTime(LAST_ONLINE)).length - 1).toBe(1)
    expect(text).toContain(`东京探针，会话 sess_1，最后可信在线 ${formatDateTime(LAST_ONLINE)}。`)
    expect(text).not.toContain(HEALTHY_SINCE)
    expect(text).not.toContain('等待安全观察')
    expect(text).not.toContain('已退役探针')
    expect(text).toContain('边缘网关')
    expect(text).toContain('edge.example.com')
    expect(text).toContain('专属探测')
    expect(text).toContain('订阅仍可能产生费用，归档不会更改服务商续费事实。')
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '结束使用并归档' })).toBeEnabled()
  })

  it('shows blockers prominently and withholds the confirmation form', () => {
    const onInlineBlocker = vi.fn()
    renderDialog({
      onInlineBlocker,
      review: review({
        eligible: false,
        blockers: ['接收链路连续健康且所有接入会话无可信在线信号的时间尚未达到 180 分钟。'],
        blocker_details: [{
          code: 'recent_trusted_online_signal',
          object_type: 'service',
          object_id: 'svc_1',
          display_name: '边缘网关',
          current_state: '使用中',
          blocked_action: 'archive',
          resolution_action: 'correct_status',
        }],
        warnings: ['没有订阅记录；资源有效期与服务商扣费请独立核对。'],
        online_evidence: {
          observed_at: HEALTHY_SINCE,
          receiver_generation: 'boot',
          receiver_healthy: false,
          healthy_since: null,
          last_health_check_at: null,
          earliest_archive_at: null,
          never_connected: false,
          manual_confirmation_required: false,
          instances: [],
        },
      }),
    })

    expect(screen.getByRole('heading', { name: '归档前仍有需要处理的事项。' })).toBeInTheDocument()
    expect(screen.getByText(/边缘网关/)).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '归档原因' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '结束使用并归档' })).toBeDisabled()
    expect(screen.getByText('接收链路健康观察不足。')).toBeInTheDocument()
    expect(screen.getByText('最早可归档时间尚未确定。')).toBeInTheDocument()
    const dialog = screen.getByRole('alertdialog', { name: '结束使用并归档' })
    expect((dialog.textContent ?? '').split('接收链路健康观察不足').length - 1).toBe(1)
    expect((dialog.textContent ?? '').split('最早可归档时间尚未确定').length - 1).toBe(1)

    fireEvent.click(screen.getByRole('button', { name: '纠正服务状态' }))
    expect(onInlineBlocker).toHaveBeenCalledWith(
      expect.objectContaining({ object_id: 'svc_1' }),
      'service-status',
    )
  })

  it('explains nameless safety blockers in plain language without a dead object link', () => {
    renderDialog({
      review: review({
        eligible: false,
        blocker_details: [
          { code: 'continuous_offline_window_incomplete', object_type: 'monitoring_instance', object_id: '', display_name: '', current_state: '', blocked_action: 'archive', resolution_action: 'wait_for_continuous_offline_observation' },
          { code: 'recent_trusted_online_signal', object_type: 'monitoring_instance', object_id: 'mi_1', display_name: '', current_state: '', blocked_action: 'archive', resolution_action: 'wait_for_continuous_offline_observation' },
        ],
      }),
    })

    const dialog = screen.getByRole('alertdialog', { name: '结束使用并归档' })
    expect(dialog).toHaveTextContent('所有接入会话无可信在线信号的时间尚未达到 180 分钟。')
    expect(dialog).toHaveTextContent('mi_1 · 最近 180 分钟内仍有可信在线信号')
    expect(dialog).not.toHaveTextContent('continuous_offline_window_incomplete')
    const links = screen.getAllByRole('link', { name: '打开监控实例' })
    expect(links).toHaveLength(1)
    expect(links[0]).toHaveAttribute('href', '/monitoring/mi_1')
  })

  it('keeps a blocker list when structured details are absent', () => {
    renderDialog({
      review: review({
        eligible: false,
        blockers: ['收到新的实时在线信号'],
      }),
    })

    expect(screen.getByText('收到新的实时在线信号')).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '输入 VPS 名称确认归档' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '结束使用并归档' })).toBeDisabled()
  })

  it('explains an ineligible review that has no blocker rows', () => {
    renderDialog({
      review: review({ eligible: false }),
    })

    expect(screen.getByRole('alert')).toHaveTextContent('当前不能归档。请根据审查处理对象，不要把这次拒绝理解成归档已经失败。')
    expect(screen.queryByRole('textbox', { name: '归档原因' })).not.toBeInTheDocument()
  })

  it('shows loading, then a retryable failure, without enabling confirm', () => {
    const onRetry = vi.fn()
    const { rerender, props } = renderDialog({ loading: true, review: null, onRetry })
    expect(screen.getByRole('status')).toHaveTextContent('正在检查归档资格…')
    expect(screen.queryByRole('textbox', { name: '归档原因' })).not.toBeInTheDocument()

    rerender(
      <MemoryRouter>
        <VPSArchiveConfirmDialog {...props} loading={false} review={null} error="review unavailable" onRetry={onRetry} />
      </MemoryRouter>,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('review unavailable')
    fireEvent.click(screen.getByRole('button', { name: '重试加载' }))
    expect(onRetry).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: '结束使用并归档' })).toBeDisabled()
  })
})

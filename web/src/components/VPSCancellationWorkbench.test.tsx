import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { VPSCancellationWorkbench } from './VPSCancellationWorkbench'
import type { CancellationPreview } from '../lib/types'

function previewFixture(): CancellationPreview {
  return {
    vps: {
      vps_id: 'vps_001',
      display_name: 'Tokyo Edge',
      provider_id: null,
      provider_name: 'Hetzner',
      product_name: 'cx22',
      order_ref: '',
      country: 'JP',
      region: 'Kanto',
      city: 'Tokyo',
      datacenter: '',
      ipv4: '',
      ipv6: '',
      ssh_host: '',
      ssh_port: 22,
      ssh_user: 'root',
      os_name: '',
      virtualization: '',
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'cancel',
      importance: 'normal',
      labels: [],
      note: '',
      active_monitoring_instance_link_count: 1,
      created_at: '2026-05-30T08:00:00Z',
      updated_at: '2026-05-30T08:00:00Z',
      archived_at: null,
    },
    subscriptions: [{
      record: {
        subscription_id: 'sub_001',
        vps_id: 'vps_001',
        price: 12,
        currency: 'USD',
        billing_cycle: 'monthly',
        billing_months: 1,
        monthly_price: 12,
        renew_at: '2026-05-01',
        started_at: '2026-01-01',
        auto_renew: true,
        auto_renew_cancelled: false,
        status: 'active',
        payment_method: '',
        note: '',
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      },
      role: 'active',
      recommended_action: 'cancel_auto_renew_and_mark_cancelled',
      message: '订阅账单记录仍显示自动续费有效，需要显式确认取消自动续费。',
    }],
    monitoring_instance_links: [{
      monitoring_instance_id: 'mi_001',
      display_name: 'Tokyo Monitoring Instance',
      group: 'prod',
      region: 'Kanto',
      city: 'Tokyo',
      provider: 'agent',
      lifecycle_status: '在用',
      monitoring_status: '启用',
      binding_status: '已绑定',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      linked_at: '2026-02-01T00:00:00Z',
      note: '',
    }],
    services: [],
    domains: [],
    target_links: [{
      target_id: 'tg_001',
      name: 'Blog',
      run_status: '启用',
      service_ids: ['svc_001'],
      domain_ids: [],
      last_linked_at: '2026-05-01T00:00:00Z',
    }],
    recommended_steps: [{
      object_type: 'vps',
      object_id: 'vps_001',
      step_type: 'vps_lifecycle',
      from_state: 'active/cancel',
      to_state: 'cancelled/cancel',
      required: true,
      message: '将 VPS 续费决策设为 cancel，并根据订阅到期情况设置生命周期。',
    }],
    warnings: ['订阅账单记录已无续费动作，但 VPS 尚未进入 to_cancel/cancelled，存在状态割裂。'],
    blockers: [],
    preview_digest: 'preview-digest-test',
  }
}

describe('VPSCancellationWorkbench', () => {
  it('submits only user-confirmed monitoring instance and target steps', async () => {
    const onSubmit = vi.fn()
    render(
      <VPSCancellationWorkbench
        preview={previewFixture()}
        submitting={false}
        error={null}
        result={null}
        onSubmit={onSubmit}
      />,
    )

    fireEvent.change(screen.getByLabelText('原因'), {
      target: { value: '已过期且不准备续费' },
    })
    fireEvent.click(within(screen.getByText('sub_001').closest('.asset-cancel-workbench__row')!).getByRole('checkbox'))
    fireEvent.click(within(screen.getByText('Tokyo Monitoring Instance').closest('.asset-checkbox-line')!).getByRole('checkbox'))
    fireEvent.click(within(screen.getByText('Blog').closest('.asset-checkbox-line')!).getByRole('checkbox'))
    fireEvent.click(screen.getByRole('button', { name: '确认取消/退役' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1))
    expect(onSubmit).toHaveBeenCalledWith({
      reason: '已过期且不准备续费',
      effective_date: expect.any(String),
      subscription_ids: ['sub_001'],
      vps_lifecycle_status: 'cancelled',
      monitoring_instance_actions: [{ monitoring_instance_id: 'mi_001', lifecycle_status: '已退役', monitoring_status: '暂停' }],
      target_actions: [{ target_id: 'tg_001', run_status: '已归档' }],
      preview_digest: 'preview-digest-test',
    })
  })

  it('omits monitoring status when the selected instance remains enabled', async () => {
    const onSubmit = vi.fn()
    const preview = previewFixture()
    const recommendedStep = preview.recommended_steps[0]
    if (!recommendedStep) throw new Error('fixture must include a VPS recommendation')
    preview.recommended_steps[0] = {
      ...recommendedStep,
      to_state: 'to_cancel/cancel',
    }
    render(
      <VPSCancellationWorkbench
        preview={preview}
        submitting={false}
        error={null}
        result={null}
        onSubmit={onSubmit}
      />,
    )

    fireEvent.change(screen.getByLabelText('原因'), {
      target: { value: '到期后停止续费' },
    })
    const subscriptionRow = screen.getByText('sub_001').closest<HTMLElement>('.asset-cancel-workbench__row')
    const monitoringRow = screen.getByText('Tokyo Monitoring Instance').closest<HTMLElement>('.asset-checkbox-line')
    if (!subscriptionRow || !monitoringRow) {
      throw new Error('workbench fixture rows must be rendered')
    }
    fireEvent.click(within(subscriptionRow).getByRole('checkbox'))
    fireEvent.click(within(monitoringRow).getByRole('checkbox'))
    fireEvent.click(screen.getByRole('button', { name: '确认取消/退役' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1))
    expect(onSubmit.mock.calls[0]?.[0].monitoring_instance_actions).toStrictEqual([
      { monitoring_instance_id: 'mi_001', lifecycle_status: '不续费' },
    ])
  })

  it('requires explicit active subscription selection before submitting', async () => {
    const onSubmit = vi.fn()
    render(
      <VPSCancellationWorkbench
        preview={previewFixture()}
        submitting={false}
        error={null}
        result={null}
        onSubmit={onSubmit}
      />,
    )

    fireEvent.change(screen.getByLabelText('原因'), {
      target: { value: '已过期且不准备续费' },
    })
    fireEvent.click(screen.getByRole('button', { name: '确认取消/退役' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('请显式选择要取消自动续费的 active 订阅。')
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('does not preselect multiple active subscriptions', async () => {
    const onSubmit = vi.fn()
    const preview = previewFixture()
    const firstSubscription = preview.subscriptions[0]
    if (!firstSubscription) throw new Error('fixture must include an active subscription')
    preview.subscriptions = [
      firstSubscription,
      {
        ...firstSubscription,
        record: {
          ...firstSubscription.record,
          subscription_id: 'sub_002',
        },
      },
    ]

    render(
      <VPSCancellationWorkbench
        preview={preview}
        submitting={false}
        error={null}
        result={null}
        onSubmit={onSubmit}
      />,
    )

    const firstRow = screen.getByText('sub_001').closest('.asset-cancel-workbench__row') as HTMLElement
    const secondRow = screen.getByText('sub_002').closest('.asset-cancel-workbench__row') as HTMLElement
    const first = within(firstRow).getByRole('checkbox')
    const second = within(secondRow).getByRole('checkbox')
    expect(first).not.toBeChecked()
    expect(second).not.toBeChecked()
    expect(firstRow).toHaveTextContent('需要显式确认取消自动续费')
    expect(secondRow).toHaveTextContent('需要显式确认取消自动续费')

    fireEvent.change(screen.getByLabelText('原因'), {
      target: { value: '已过期且不准备续费' },
    })
    fireEvent.click(screen.getByRole('button', { name: '确认取消/退役' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('请显式选择要取消自动续费的 active 订阅。')
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('rebuilds confirmation state when the preview digest changes', () => {
    const onSubmit = vi.fn()
    const preview = previewFixture()
    const { rerender } = render(
      <VPSCancellationWorkbench
        key={preview.preview_digest}
        preview={preview}
        submitting={false}
        error={null}
        result={null}
        onSubmit={onSubmit}
      />,
    )
    fireEvent.change(screen.getByLabelText('原因'), { target: { value: '旧确认' } })
    const next = { ...preview, preview_digest: 'preview-digest-next' }
    rerender(
      <VPSCancellationWorkbench
        key={next.preview_digest}
        preview={next}
        submitting={false}
        error={null}
        result={null}
        onSubmit={onSubmit}
      />,
    )
    expect(screen.getByLabelText('原因')).toHaveValue('')
  })
})

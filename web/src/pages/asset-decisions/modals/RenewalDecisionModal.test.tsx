import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { VPSAssetRecord } from '../../../lib/types'
import { RenewalDecisionModal } from './RenewalDecisionModal'

const VPS: VPSAssetRecord = {
  vps_id: 'vps_001',
  display_name: 'Tokyo Review',
  provider_id: 'pv_001',
  provider_name: 'Provider A',
  product_name: 'KVM',
  order_ref: 'order-001',
  country: 'JP',
  region: 'Kanto',
  city: 'Tokyo',
  datacenter: 'NRT1',
  ipv4: '192.0.2.1',
  ipv6: '',
  ssh_host: '192.0.2.1',
  ssh_port: 22,
  ssh_user: 'root',
  os_name: 'Debian',
  virtualization: 'kvm',
  lifecycle_status: 'active',
  usage_status: 'in_use',
  renewal_decision: 'unreviewed',
  importance: 'normal',
  labels: [],
  note: '',
  active_monitoring_instance_link_count: 1,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
}

describe('RenewalDecisionModal', () => {
  it('emits typed draft values and semantic submit/close commands', () => {
    const onUpdateDraft = vi.fn()
    const onSubmitDecision = vi.fn()
    const onClose = vi.fn()
    render(
      <RenewalDecisionModal
        open
        selectedVPS={VPS}
        decisionDraft={{ renewalDecision: 'unreviewed', reason: '' }}
        submitting={false}
        error={null}
        onUpdateDraft={onUpdateDraft}
        onSubmitDecision={onSubmitDecision}
        onClose={onClose}
      />,
    )

    fireEvent.change(screen.getByLabelText('续费决策'), { target: { value: 'cancel' } })
    fireEvent.change(screen.getByLabelText('决策理由'), { target: { value: '不再续费' } })
    fireEvent.submit(screen.getByRole('button', { name: '保存续费决策' }).closest('form')!)
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    expect(onUpdateDraft).toHaveBeenNthCalledWith(1, { renewalDecision: 'cancel', reason: '' })
    expect(onUpdateDraft).toHaveBeenNthCalledWith(2, { renewalDecision: 'unreviewed', reason: '不再续费' })
    expect(onSubmitDecision).toHaveBeenCalledOnce()
    expect(onClose).toHaveBeenCalledOnce()
  })
})

import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { VPSAssetDetail } from '../../lib/types'
import { VPSRenewalDecisionForm } from './VPSRenewalDecisionForm'

function detailFixture(): VPSAssetDetail {
  return {
    vps_id: 'vps_a',
    display_name: '东京边缘',
    provider_id: null,
    provider_name: 'Example',
    product_name: 'VPS',
    order_ref: '',
    country: 'JP',
    region: 'Tokyo',
    city: 'Tokyo',
    datacenter: 'TK1',
    ipv4: '192.0.2.1',
    ipv6: '',
    ssh_host: '192.0.2.1',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian',
    virtualization: 'KVM',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: 'keep',
    importance: 'high',
    labels: [],
    note: '',
    active_monitoring_instance_link_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    monitoring_instance_links: [],
  }
}

describe('VPSRenewalDecisionForm', () => {
  it('disables decision controls while submitting / loading latest', () => {
    render(
      <VPSRenewalDecisionForm
        detail={detailFixture()}
        draft={{ renewalDecision: 'cancel', reason: '准备取消' }}
        submitting
        error={null}
        notice={null}
        decisionChanged
        onCancel={vi.fn()}
        onDraftChange={vi.fn()}
        onFeedbackClear={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )

    expect(screen.getByRole('combobox', { name: '续费决策' })).toBeDisabled()
    expect(screen.getByRole('textbox', { name: '决策理由' })).toBeDisabled()
  })
})

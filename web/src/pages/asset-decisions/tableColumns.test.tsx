import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { DataTable } from '../../components/atoms'
import type { AssetDecisionManualGroupMember } from '../../lib/types'
import { createManualMemberColumns } from './tableColumns'

const MEMBER = {
  manual_group_id: 'admg_001',
  vps_id: 'vps_001',
  current_fact_found: true,
  intended_role: 'primary_candidate',
  intended_action: 'keep',
  reason: '主力稳定',
  note: '',
  sort_order: 10,
  service_count: 1,
  domain_count: 0,
  target_count: 1,
  running_target_count: 1,
  monitoring_link_count: 1,
  running_monitoring_count: 1,
  primary_issue_summary: '',
  source_availability: {
    subscriptions: true,
    services: true,
    domains: true,
    monitoring: true,
    targets: true,
  },
  evidence_chips: [],
  evidence_assessment: {
    confidence_score: 80,
    pressure_score: 20,
    readiness_score: 70,
    quality_tier: 'strong',
    decision_bias: 'keep',
    support_signal_count: 2,
    risk_signal_count: 0,
    gap_signal_count: 0,
    summary: '证据可用',
  },
  vps: {
    vps_id: 'vps_001',
    display_name: 'Frankfurt Primary',
    provider_name: 'Provider One',
    product_name: 'Compute',
    country: 'DE',
    region: 'Hesse',
    city: 'Frankfurt',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: 'unreviewed',
  },
} as unknown as AssetDecisionManualGroupMember

describe('createManualMemberColumns', () => {
  it('keeps the existing read-only member columns and emits a semantic removal command', () => {
    const requestRemoval = vi.fn()
    const columns = createManualMemberColumns({
      saving: {},
      onRequestRemoval: requestRemoval,
    })

    render(
      <MemoryRouter>
        <DataTable
          columns={columns}
          rows={[MEMBER]}
          rowKey={(member) => member.vps_id}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: 'Frankfurt Primary' })).toHaveAttribute('href', '/vps/vps_001')
    expect(screen.getByText('主力候选')).toBeInTheDocument()
    expect(screen.getByText('保留')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '移除' }))
    expect(requestRemoval).toHaveBeenCalledWith(MEMBER)
  })
})

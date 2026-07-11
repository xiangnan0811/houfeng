import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AssetDecisionRecordMember } from '../../../lib/types'
import type { RecordFollowupDraft } from '../types'
import { RecordFollowupRows } from './RecordFollowupRows'

function member(vpsID: string): AssetDecisionRecordMember {
  return {
    record_id: 'adr_001',
    vps_id: vpsID,
    display_name: `Node ${vpsID.slice(-1)}`,
    suggested_role: 'observe_candidate',
    decided_role: 'observe_candidate',
    suggested_action: 'review',
    decided_action: 'review',
    reason: '',
    followup_status: 'todo',
    followup_note: '',
    followup_updated_at: null,
    evidence_snapshot: {},
    execution_readback: {
      status: 'open',
      summary: '等待执行回读',
      issues: [],
      current_facts: {
        found: true,
        lifecycle_status: 'active',
        usage_status: 'in_use',
        renewal_decision: 'keep',
        active_subscription_count: 1,
        service_count: 0,
        domain_count: 0,
        target_count: 0,
        running_target_count: 0,
        monitoring_link_count: 0,
        running_monitoring_count: 0,
        abnormal_monitoring_count: 0,
        active_incident_count: 0,
        source_availability: {
          subscriptions: true,
          services: true,
          domains: true,
          monitoring: true,
          targets: true,
        },
      },
    },
    execution_plan: {
      lane: 'review',
      step_kind: 'review_record',
      tone: 'notice',
      summary: '复核当前记录',
      step_label: '复核记录',
      issue_count: 0,
      blocked: false,
      actionable: true,
    },
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
  }
}

const MEMBERS = [member('vps_1'), member('vps_2'), member('vps_3'), member('vps_4')]
const DRAFTS: Record<string, RecordFollowupDraft> = Object.fromEntries(MEMBERS.map((row) => [
  row.vps_id,
  { status: row.followup_status, note: row.followup_note },
]))

describe('RecordFollowupRows', () => {
  it('limits the preview and emits controlled edit, patch, save, and raw-panel commands', () => {
    const onUpdateDraft = vi.fn()
    const onEditMember = vi.fn()
    const onSave = vi.fn()
    const onShowRaw = vi.fn()
    const { rerender } = render(
      <RecordFollowupRows
        members={MEMBERS}
        drafts={DRAFTS}
        saving={{}}
        editingMemberID={null}
        onUpdateDraft={onUpdateDraft}
        onEditMember={onEditMember}
        onSave={onSave}
        onShowRaw={onShowRaw}
      />,
    )

    expect(screen.getByText('Node 1')).toBeInTheDocument()
    expect(screen.queryByText('Node 4')).not.toBeInTheDocument()
    fireEvent.click(screen.getAllByRole('button', { name: '编辑跟进' })[0]!)
    expect(onEditMember).toHaveBeenCalledWith('vps_1')
    fireEvent.click(screen.getByRole('button', { name: '查看成员底稿' }))
    expect(onShowRaw).toHaveBeenCalledOnce()

    rerender(
      <RecordFollowupRows
        members={MEMBERS}
        drafts={DRAFTS}
        saving={{}}
        editingMemberID="vps_1"
        onUpdateDraft={onUpdateDraft}
        onEditMember={onEditMember}
        onSave={onSave}
        onShowRaw={onShowRaw}
      />,
    )
    fireEvent.change(screen.getByLabelText('跟进状态'), { target: { value: 'blocked' } })
    fireEvent.change(screen.getByLabelText('跟进备注'), { target: { value: '等待窗口' } })
    fireEvent.submit(screen.getByRole('button', { name: '保存跟进' }).closest('form')!)
    fireEvent.click(screen.getByRole('button', { name: '收起' }))

    expect(onUpdateDraft).toHaveBeenNthCalledWith(1, 'vps_1', { status: 'blocked' })
    expect(onUpdateDraft).toHaveBeenNthCalledWith(2, 'vps_1', { note: '等待窗口' })
    expect(onSave).toHaveBeenCalledWith(MEMBERS[0])
    expect(onEditMember).toHaveBeenLastCalledWith(null)
  })

  it('renders the same controlled editor for a raw table cell', () => {
    const onSave = vi.fn()
    render(
      <RecordFollowupRows
        surface="cell"
        members={[MEMBERS[0]!]}
        drafts={DRAFTS}
        saving={{}}
        editingMemberID="vps_1"
        onUpdateDraft={vi.fn()}
        onEditMember={vi.fn()}
        onSave={onSave}
        onShowRaw={vi.fn()}
      />,
    )

    expect(screen.getByRole('button', { name: '保存跟进' })).toBeDisabled()
    expect(screen.queryByRole('region', { name: '成员跟进列表' })).not.toBeInTheDocument()
  })
})

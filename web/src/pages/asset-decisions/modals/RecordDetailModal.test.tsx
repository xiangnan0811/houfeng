import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { AssetDecisionRecordDetail } from '../../../lib/types'
import { RecordDetailModal } from './RecordDetailModal'

function recordDetail(): AssetDecisionRecordDetail {
  return {
    record_id: 'adr_001',
    title: '德国主备取舍记录',
    goal: '保留主力',
    status: 'draft',
    source_type: 'auto_group',
    source_group_id: 'adg_001',
    source_group_type: 'renewal_attention',
    source_view: 'renewal',
    scope_key: 'renewal-window',
    scope_label: '未来 30 天',
    renew_within_days: 30,
    member_count: 1,
    followup_todo_count: 1,
    followup_in_progress_count: 0,
    followup_blocked_count: 0,
    followup_done_count: 0,
    followup_skipped_count: 0,
    evidence_snapshot: {},
    execution_readback: {
      status: 'open',
      summary: '1 台等待跟进',
      open_count: 1,
      aligned_count: 0,
      drift_count: 0,
      blocked_count: 0,
      needs_evidence_count: 0,
    },
    execution_plan: {
      summary: '复核成员',
      lane_counts: [{ lane: 'review', count: 1 }],
      actionable_count: 1,
      blocked_count: 0,
    },
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
    decided_at: null,
    completed_at: null,
    members: [{
      record_id: 'adr_001',
      vps_id: 'vps_001',
      display_name: 'Germany Primary',
      suggested_role: 'primary_candidate',
      decided_role: 'primary_candidate',
      suggested_action: 'keep',
      decided_action: 'keep',
      reason: '主力保留',
      followup_status: 'todo',
      followup_note: '',
      followup_updated_at: null,
      evidence_snapshot: {},
      execution_readback: {
        status: 'open',
        summary: '等待复核',
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
        summary: '复核记录',
        step_label: '复核记录',
        issue_count: 0,
        blocked: false,
        actionable: true,
      },
      created_at: '2026-07-01T00:00:00Z',
      updated_at: '2026-07-01T00:00:00Z',
    }],
  }
}

function modalProps(panel: 'execution' | 'members' | 'raw') {
  const detail = recordDetail()
  return {
    open: true,
    recordDetailState: { loading: false, error: null, detail },
    recordDetailPanel: panel,
    recordPatchError: null,
    recordPatchStatus: 'draft' as const,
    recordPatching: false,
    selectedRecordAssessment: null,
    followupDrafts: { vps_001: { status: 'todo' as const, note: '' } },
    followupSaving: {},
    followupEditingMemberID: null,
    onClose: vi.fn(),
    onSetRecordDetailPanel: vi.fn(),
    onPatchRecordStatus: vi.fn(),
    onSetRecordPatchStatus: vi.fn(),
    onOpenRecordSource: vi.fn(),
    onUpdateFollowupDraft: vi.fn(),
    onEditFollowupMember: vi.fn(),
    onSaveFollowup: vi.fn(),
    onReviewRecord: vi.fn(),
  }
}

describe('RecordDetailModal', () => {
  it('owns execution presentation while emitting semantic status and review commands', () => {
    const props = modalProps('execution')
    render(<MemoryRouter><RecordDetailModal {...props} /></MemoryRouter>)

    fireEvent.change(screen.getByLabelText('推进状态'), { target: { value: 'in_progress' } })
    fireEvent.submit(screen.getByRole('button', { name: '更新状态' }).closest('form')!)
    fireEvent.click(screen.getByRole('button', { name: '复核记录' }))

    expect(props.onSetRecordPatchStatus).toHaveBeenCalledWith('in_progress')
    expect(props.onPatchRecordStatus).toHaveBeenCalledOnce()
    expect(props.onReviewRecord).toHaveBeenCalledWith(props.recordDetailState.detail.members[0])
  })

  it('owns follow-up list and raw-table cell presentation without render callbacks', () => {
    const props = modalProps('members')
    const { rerender } = render(<MemoryRouter><RecordDetailModal {...props} /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: '编辑跟进' }))
    expect(props.onEditFollowupMember).toHaveBeenCalledWith('vps_001')

    const rawProps = { ...props, recordDetailPanel: 'raw' as const }
    rerender(<MemoryRouter><RecordDetailModal {...rawProps} /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    expect(props.onEditFollowupMember).toHaveBeenLastCalledWith('vps_001')
  })
})

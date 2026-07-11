import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type {
  AssetDecisionRecordDetail,
  AssetDecisionRecordMember,
  AssetDecisionRecordExecutionReadback,
} from '../../../lib/types'
import { RecordExecutionBoard } from './RecordExecutionBoard'

const RECORD_READBACK: AssetDecisionRecordExecutionReadback = {
  status: 'open',
  summary: '成员等待跟进',
  open_count: 4,
  aligned_count: 0,
  drift_count: 2,
  blocked_count: 0,
  needs_evidence_count: 0,
}

function member(
  vpsID: string,
  overrides: Partial<AssetDecisionRecordMember> = {},
): AssetDecisionRecordMember {
  return {
    record_id: 'adr_001',
    vps_id: vpsID,
    display_name: `Node ${vpsID.slice(-1)}`,
    suggested_role: 'observe_candidate',
    decided_role: 'observe_candidate',
    suggested_action: 'review',
    decided_action: 'review',
    reason: '',
    followup_status: 'in_progress',
    followup_note: '',
    followup_updated_at: null,
    evidence_snapshot: {},
    execution_readback: {
      status: 'drift',
      summary: '当前事实漂移',
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
    ...overrides,
  }
}

function detail(members: AssetDecisionRecordMember[]): AssetDecisionRecordDetail {
  return {
    record_id: 'adr_001',
    title: '执行编排',
    goal: '',
    status: 'in_progress',
    source_type: 'auto_group',
    source_group_id: 'adg_001',
    source_group_type: 'renewal_attention',
    source_view: 'renewal',
    scope_key: 'renewal-window',
    scope_label: '未来 30 天',
    renew_within_days: 30,
    member_count: members.length,
    followup_todo_count: 0,
    followup_in_progress_count: members.length,
    followup_blocked_count: 0,
    followup_done_count: 0,
    followup_skipped_count: 0,
    evidence_snapshot: {},
    execution_readback: RECORD_READBACK,
    execution_plan: {
      summary: '按 lane 推进成员',
      lane_counts: [{ lane: 'review', count: members.length }],
      actionable_count: members.length,
      blocked_count: 0,
    },
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
    decided_at: null,
    completed_at: null,
    members,
  }
}

describe('RecordExecutionBoard', () => {
  it('maps execution CTAs locally, limits the preview, and emits review commands', () => {
    const onReviewRecord = vi.fn()
    const rows = [
      member('vps_1', {
        execution_plan: {
          lane: 'cancel_retire',
          step_kind: 'open_cancellation_workbench',
          tone: 'critical',
          summary: '处理取消退役',
          step_label: '打开取消/退役工作台',
          issue_count: 1,
          blocked: false,
          actionable: true,
        },
      }),
      member('vps_2', {
        execution_plan: {
          lane: 'evidence',
          step_kind: 'open_subscription_context',
          tone: 'alert',
          summary: '补订阅事实',
          step_label: '打开订阅上下文',
          issue_count: 1,
          blocked: false,
          actionable: true,
        },
      }),
      member('vps_3'),
      member('vps_4'),
    ]

    render(
      <MemoryRouter>
        <RecordExecutionBoard
          detail={detail(rows)}
          followupSaving={{}}
          onSaveFollowup={vi.fn()}
          onReviewRecord={onReviewRecord}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: '打开取消/退役工作台' })).toHaveAttribute('href', '/vps/vps_1?workbench=cancellation')
    expect(screen.getByRole('link', { name: '打开订阅上下文' })).toHaveAttribute('href', '/subscriptions?vps_id=vps_2')
    fireEvent.click(screen.getByRole('button', { name: '复核记录' }))
    expect(onReviewRecord).toHaveBeenCalledWith(rows[2])
    expect(screen.queryByText('Node 4')).not.toBeInTheDocument()
    expect(screen.getByText('另有 1 台在成员跟进或底稿中查看')).toBeInTheDocument()
  })

  it('emits the quick completed follow-up for an aligned member', () => {
    const aligned = member('vps_1', {
      execution_readback: {
        ...member('vps_1').execution_readback,
        status: 'aligned',
        summary: '当前事实与判断一致',
      },
      followup_status: 'in_progress',
    })
    const onSaveFollowup = vi.fn()
    render(
      <MemoryRouter>
        <RecordExecutionBoard
          detail={detail([aligned])}
          followupSaving={{}}
          onSaveFollowup={onSaveFollowup}
          onReviewRecord={vi.fn()}
        />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '标记完成' }))
    expect(onSaveFollowup).toHaveBeenCalledWith(aligned, 'done')
  })
})

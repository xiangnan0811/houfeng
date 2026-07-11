import { Link } from 'react-router-dom'

import { Badge } from '../../../components/atoms'
import type {
  AssetDecisionFollowupStatus,
  AssetDecisionRecordDetail,
  AssetDecisionRecordMember,
} from '../../../lib/types'
import { EXECUTION_PLAN_LANE_ORDER } from '../constants'
import {
  actionLabelForMember,
  chipTone,
  compactMemberPlanSummary,
} from '../formatters'
import { vpsDetailPath, vpsWorkbenchPath } from '../paths'
import {
  previewItems,
  renderExecutionPlanBadge,
  renderReadbackBadge,
} from '../renderHelpers'

type RecordExecutionBoardProps = {
  detail: AssetDecisionRecordDetail
  followupSaving: Readonly<Record<string, boolean>>
  onSaveFollowup: (
    member: AssetDecisionRecordMember,
    nextStatus?: AssetDecisionFollowupStatus,
  ) => void
  onReviewRecord: (member: AssetDecisionRecordMember) => void
}

function actionHrefForMember(member: AssetDecisionRecordMember): string {
  if (member.execution_plan?.step_kind === 'open_cancellation_workbench') {
    return vpsWorkbenchPath(member.vps_id, 'cancellation')
  }
  if (member.execution_plan?.step_kind === 'open_subscription_context') {
    return `/subscriptions?vps_id=${encodeURIComponent(member.vps_id)}`
  }
  return vpsDetailPath(member.vps_id)
}

function sortedExecutionMembers(members: AssetDecisionRecordMember[]): AssetDecisionRecordMember[] {
  function executionLaneRank(member: AssetDecisionRecordMember): number {
    const lane = member.execution_plan?.lane ?? 'review'
    const rank = EXECUTION_PLAN_LANE_ORDER.indexOf(lane)
    return rank >= 0 ? rank : EXECUTION_PLAN_LANE_ORDER.length
  }
  return members
    .map((member, index) => ({ member, index }))
    .sort((left, right) => executionLaneRank(left.member) - executionLaneRank(right.member) || left.index - right.index)
    .map(({ member }) => member)
}

export function RecordExecutionBoard({
  detail,
  followupSaving,
  onSaveFollowup,
  onReviewRecord,
}: RecordExecutionBoardProps) {
  const executionPreview = previewItems(sortedExecutionMembers(detail.members))
  const visibleExecutionMembers = executionPreview.visible

  if (visibleExecutionMembers.length === 0) {
    return (
      <section className="asset-decision-execution-board" aria-label="执行编排">
        <div className="asset-decision-execution-board__empty">
          <strong>暂无执行编排</strong>
        </div>
      </section>
    )
  }

  return (
    <section className="asset-decision-execution-board" aria-label="执行编排">
      <div className="asset-decision-execution-board__header">
        <div>
          <strong>{detail.execution_plan?.summary || '等待执行步骤'}</strong>
        </div>
        <div className="asset-decision-execution-board__counts">
          <span>可推进 {detail.execution_plan?.actionable_count ?? 0}</span>
          {detail.execution_plan?.blocked_count > 0 && (
            <Badge variant="count" tone="critical">
              阻塞 {detail.execution_plan.blocked_count}
            </Badge>
          )}
        </div>
      </div>
      <div className="asset-decision-execution-board__members">
        {visibleExecutionMembers.map((member) => {
          const isSaving = Boolean(followupSaving[member.vps_id])
          const canStart = member.followup_status === 'todo'
          const canMarkDone = member.execution_readback?.status === 'aligned' &&
            member.followup_status !== 'done' &&
            member.followup_status !== 'skipped'
          let primaryAction = null
          if (canMarkDone) {
            primaryAction = (
              <button className="btn sm primary" type="button" disabled={isSaving} onClick={() => onSaveFollowup(member, 'done')}>
                标记完成
              </button>
            )
          } else if (member.execution_plan?.step_kind === 'review_record') {
            primaryAction = (
              <button className="btn sm secondary" type="button" onClick={() => onReviewRecord(member)}>
                {actionLabelForMember(member)}
              </button>
            )
          } else if (member.execution_plan?.step_kind) {
            primaryAction = (
              <Link className="btn sm secondary" to={actionHrefForMember(member)}>
                {actionLabelForMember(member)}
              </Link>
            )
          } else if (canStart) {
            primaryAction = (
              <button className="btn sm secondary" type="button" disabled={isSaving} onClick={() => onSaveFollowup(member, 'in_progress')}>
                开始跟进
              </button>
            )
          }
          return (
            <article key={member.vps_id} className="asset-decision-execution-card">
              <div className="asset-decision-execution-card__head">
                <strong>{member.display_name || member.vps_id}</strong>
                <span className="asset-decision-chip-row">
                  {renderExecutionPlanBadge(member.execution_plan)}
                  {renderReadbackBadge(member.execution_readback)}
                </span>
              </div>
              <p>{compactMemberPlanSummary(member)}</p>
              {member.execution_readback?.issues?.length > 0 && (
                <span className="asset-decision-chip-row">
                  {member.execution_readback.issues.slice(0, 2).map((issue) => (
                    <Badge key={`${member.vps_id}-${issue.kind}-${issue.label}`} variant="info" tone={chipTone(issue.tone)}>
                      {issue.label}
                    </Badge>
                  ))}
                  {member.execution_readback.issues.length > 2 && (
                    <span>+{member.execution_readback.issues.length - 2}</span>
                  )}
                </span>
              )}
              <div className="asset-decision-execution-card__actions">
                {primaryAction}
              </div>
            </article>
          )
        })}
      </div>
      {executionPreview.hiddenCount > 0 && (
        <div className="asset-decision-preview-more" role="note">
          另有 {executionPreview.hiddenCount} 台在成员跟进或底稿中查看
        </div>
      )}
    </section>
  )
}

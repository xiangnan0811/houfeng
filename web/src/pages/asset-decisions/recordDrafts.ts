import type {
  AssetDecisionGroupDetail,
  AssetDecisionManualGroupDetail,
  AssetDecisionRecordDetail,
} from '../../lib/types'
import type { RecordDraft, RecordFollowupDraft, RecordMemberDraft } from './types'

export function buildRecordFollowupDrafts(
  detail: AssetDecisionRecordDetail | null,
): Record<string, RecordFollowupDraft> {
  return Object.fromEntries((detail?.members ?? []).map((member) => [
    member.vps_id,
    {
      status: member.followup_status,
      note: member.followup_note,
    },
  ]))
}

function groupMemberDrafts(detail: AssetDecisionGroupDetail): Record<string, RecordMemberDraft> {
  return Object.fromEntries(detail.members.map((member) => [
    member.vps.vps_id,
    {
      decidedRole: member.suggested_role,
      decidedAction: member.suggested_action,
      reason: '',
    },
  ]))
}

function manualMemberDrafts(detail: AssetDecisionManualGroupDetail): Record<string, RecordMemberDraft> {
  return Object.fromEntries(detail.members.map((member) => [
    member.vps_id,
    {
      decidedRole: member.intended_role,
      decidedAction: member.intended_action,
      reason: member.reason,
    },
  ]))
}

export function completeRecordDraftFromGroupDetail(
  current: RecordDraft | null,
  detail: AssetDecisionGroupDetail,
  renewWithinDays: number,
): RecordDraft {
  const detailIDs = detail.members.map((member) => member.vps.vps_id)
  const activeIDs = new Set(detailIDs)
  const fallbackMembers = groupMemberDrafts(detail)
  const baseDraft = current?.sourceType === 'auto_group' && current.sourceGroupID === detail.group_id
    ? current
    : {
      sourceType: 'auto_group' as const,
      sourceGroupID: detail.group_id,
      renewWithinDays,
      title: detail.title,
      goal: '',
      status: 'draft' as const,
      memberOrder: detailIDs,
      members: fallbackMembers,
    }

  return {
    ...baseDraft,
    renewWithinDays,
    memberOrder: [
      ...baseDraft.memberOrder.filter((vpsID) => activeIDs.has(vpsID)),
      ...detailIDs.filter((vpsID) => !baseDraft.memberOrder.includes(vpsID)),
    ],
    members: Object.fromEntries(detailIDs.map((vpsID) => [vpsID, baseDraft.members[vpsID] ?? fallbackMembers[vpsID]])),
  }
}

export function completeRecordDraftFromManualDetail(
  current: RecordDraft | null,
  detail: AssetDecisionManualGroupDetail,
): RecordDraft {
  const detailIDs = detail.members.map((member) => member.vps_id)
  const activeIDs = new Set(detailIDs)
  const fallbackMembers = manualMemberDrafts(detail)
  const baseDraft = current?.sourceType === 'manual_group' && current.sourceGroupID === detail.manual_group_id
    ? current
    : {
      sourceType: 'manual_group' as const,
      sourceGroupID: detail.manual_group_id,
      renewWithinDays: detail.renew_within_days,
      title: detail.title,
      goal: detail.goal,
      status: 'draft' as const,
      memberOrder: detailIDs,
      members: fallbackMembers,
    }

  return {
    ...baseDraft,
    memberOrder: [
      ...baseDraft.memberOrder.filter((vpsID) => activeIDs.has(vpsID)),
      ...detailIDs.filter((vpsID) => !baseDraft.memberOrder.includes(vpsID)),
    ],
    members: Object.fromEntries(detailIDs.map((vpsID) => [vpsID, baseDraft.members[vpsID] ?? fallbackMembers[vpsID]])),
  }
}

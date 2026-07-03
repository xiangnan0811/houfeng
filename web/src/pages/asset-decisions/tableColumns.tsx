import { Link } from 'react-router-dom'

import {
  Badge,
  type DataTableColumn,
  MonoDigits,
} from '../../components/atoms'
import { formatDateTime, formatMoney, formatOptional } from '../../lib/format'
import {
  type AssetDecisionEvidenceAssessment,
  type AssetDecisionGroupMember,
  type AssetDecisionManualGroupMember,
  type AssetDecisionManualGroupSummary,
  type AssetDecisionRecordMember,
  type AssetDecisionRecordSummary,
  type AssetDecisionSuggestedAction,
  type AssetDecisionSuggestedRole,
} from '../../lib/types'
import {
  LifecycleBadge,
  RenewalBadge,
  SubscriptionStatusBadge,
  UsageBadge,
} from '../assetPageBadges'
import { daysUntilDate, usageLabel, vpsLocationLabel } from '../assetPageUtils'
import {
  ACTION_LABELS,
  MANUAL_GROUP_SCENARIO_LABELS,
  MANUAL_GROUP_STATUS_LABELS,
  RECORD_STATUS_LABELS,
  ROLE_LABELS,
  VIEW_LABELS,
} from './constants'
import {
  actionTone,
  countSummary,
  executionPlanCountSummary,
  formatGroupMonthlyCost,
  formatGroupYearlyCost,
  manualGroupStatusTone,
  memberContextLabel,
  readbackCountSummary,
  recordFollowupDoneCount,
  recordFollowupOpenCount,
  recordStatusTone,
  roleTone,
  sourceAvailabilityLabel,
} from './formatters'
import {
  renderDecisionRecommendation,
  renderEvidenceAssessment,
  renderEvidenceChips,
  renderMemberExecutionPlan,
  renderMemberReadback,
  renderReadbackBadge,
} from './renderHelpers'

export function createManualGroupColumns(options: {
  onOpen: (manualGroupID: string) => void
}): DataTableColumn<AssetDecisionManualGroupSummary>[] {
  return [
    {
      key: 'group',
      label: '自定义组合',
      width: '286px',
      render: (group) => (
        <div className="asset-table__identity asset-decision-group-cell">
          <strong>{group.title}</strong>
          <span>{MANUAL_GROUP_SCENARIO_LABELS[group.scenario]} · {group.goal || group.scope_label || '用户场景'}</span>
          <span className="asset-decision-chip-row">
            <Badge variant="state" tone={manualGroupStatusTone(group.status)}>
              {MANUAL_GROUP_STATUS_LABELS[group.status]}
            </Badge>
            <Badge variant="info" tone="neutral">
              {group.source_type === 'auto_group' ? '来自自动组' : '手工创建'}
            </Badge>
          </span>
          {renderEvidenceChips(group.evidence_chips, 3)}
          {renderDecisionRecommendation(group.decision_recommendation)}
        </div>
      ),
    },
    {
      key: 'portfolio',
      label: '组合事实',
      width: '220px',
      render: (group) => (
        <div className="asset-table__stack">
          <strong><MonoDigits>{group.member_count}</MonoDigits> 台 VPS</strong>
          <span>{countSummary(group.usage_counts, ['in_use', 'standby', 'idle'], usageLabel)}</span>
          <span>{countSummary(group.renewal_decision_counts, ['unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced'], (value) => value)}</span>
        </div>
      ),
    },
    {
      key: 'evidence',
      label: '证据',
      width: '236px',
      render: (group) => (
        <div className="asset-table__stack">
          <strong>
            服务 {group.service_count} · 域名 {group.domain_count}
          </strong>
          <span>Target {group.running_target_count}/{group.target_count} · 监控 {group.monitoring_link_count}</span>
          <span>资料缺口 {group.evidence_assessment.gap_signal_count} · 风险 {group.evidence_assessment.risk_signal_count}</span>
        </div>
      ),
    },
    {
      key: 'assessment',
      label: '判断尺度',
      width: '238px',
      render: (group) => renderEvidenceAssessment(group.evidence_assessment),
    },
    {
      key: 'cost',
      label: '成本',
      width: '176px',
      render: (group) => (
        <div className="asset-table__stack">
          <strong>{formatGroupMonthlyCost(group)}</strong>
          <span>{formatGroupYearlyCost(group)}</span>
        </div>
      ),
    },
    {
      key: 'updated',
      label: '更新',
      width: '160px',
      render: (group) => (
        <div className="asset-table__stack">
          <strong>{formatDateTime(group.updated_at)}</strong>
          <span>{group.archived_at ? `归档 ${formatDateTime(group.archived_at)}` : '持续跟进'}</span>
        </div>
      ),
    },
    {
      key: 'actions',
      label: '入口',
      align: 'right',
      width: '128px',
      render: (group) => (
        <button className="btn sm primary" type="button" onClick={() => options.onOpen(group.manual_group_id)}>
          查看组合
        </button>
      ),
    },
  ]
}

export function createRecordColumns(options: {
  onOpen: (recordID: string) => void
}): DataTableColumn<AssetDecisionRecordSummary>[] {
  return [
    {
      key: 'record',
      label: '决策记录',
      width: '286px',
      render: (record) => (
        <div className="asset-table__identity asset-decision-record-cell">
          <strong>{record.title}</strong>
          <span>{VIEW_LABELS[record.source_view]} · {record.scope_label || record.source_group_id}</span>
          <span>{record.source_group_type} · {record.source_group_id}</span>
          <span>{record.goal || '暂无目标备注'}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '状态',
      width: '136px',
      render: (record) => (
        <div className="asset-table__stack">
          <Badge variant="state" tone={recordStatusTone(record.status)}>
            {RECORD_STATUS_LABELS[record.status]}
          </Badge>
          <span>{record.renew_within_days} 天窗口</span>
        </div>
      ),
    },
    {
      key: 'scope',
      label: '推进',
      width: '220px',
      render: (record) => (
        <div className="asset-table__stack">
          <strong>
            推进 <MonoDigits>{recordFollowupDoneCount(record)}</MonoDigits>/<MonoDigits>{record.member_count}</MonoDigits>
          </strong>
          <span>
            待处理 {record.followup_todo_count ?? 0} · 处理中 {record.followup_in_progress_count ?? 0}
          </span>
          <span>
            阻塞 {record.followup_blocked_count ?? 0} · 未关闭 {recordFollowupOpenCount(record)}
          </span>
        </div>
      ),
    },
    {
      key: 'readback',
      label: '执行回读',
      width: '228px',
      render: (record) => (
        <div className="asset-table__stack asset-decision-readback-cell">
          <span className="asset-decision-chip-row">
            {renderReadbackBadge(record.execution_readback)}
          </span>
          <strong>{record.execution_readback?.summary || '等待执行证据回读'}</strong>
          <span>{readbackCountSummary(record.execution_readback)}</span>
        </div>
      ),
    },
    {
      key: 'plan',
      label: '执行编排',
      width: '260px',
      render: (record) => (
        <div className="asset-table__stack asset-decision-plan-cell">
          <span className="asset-decision-chip-row">
            <Badge variant="state" tone={record.execution_plan?.blocked_count > 0 ? 'critical' : record.execution_plan?.actionable_count > 0 ? 'maintenance' : 'normal'}>
              执行编排
            </Badge>
            {record.execution_plan?.actionable_count > 0 && (
              <Badge variant="count" tone="maintenance">
                {record.execution_plan.actionable_count} 项
              </Badge>
            )}
          </span>
          <strong>{record.execution_plan?.summary || '等待执行编排'}</strong>
          <span>{executionPlanCountSummary(record.execution_plan)}</span>
        </div>
      ),
    },
    {
      key: 'updated',
      label: '更新时间',
      width: '168px',
      render: (record) => (
        <div className="asset-table__stack">
          <strong>{formatDateTime(record.updated_at)}</strong>
          <span>{record.completed_at ? `完成 ${formatDateTime(record.completed_at)}` : record.decided_at ? `决策 ${formatDateTime(record.decided_at)}` : '尚未决策'}</span>
        </div>
      ),
    },
    {
      key: 'actions',
      label: '入口',
      align: 'right',
      width: '112px',
      render: (record) => (
        <button className="btn sm primary" type="button" onClick={() => options.onOpen(record.record_id)}>
          查看记录
        </button>
      ),
    },
  ]
}

export function createMemberColumns(options: {
  onSelect: (member: AssetDecisionGroupMember) => void
}): DataTableColumn<AssetDecisionGroupMember>[] {
  return [
    {
      key: 'vps',
      label: 'VPS',
      width: '240px',
      render: (member) => (
        <div className="asset-table__identity">
          <strong><Link className="name" to={`/vps/${member.vps.vps_id}`}>{member.vps.display_name}</Link></strong>
          <span>{formatOptional(member.vps.provider_name)} · {vpsLocationLabel(member.vps)}</span>
          <span>{member.vps.product_name || member.vps.vps_id}</span>
          <span className="asset-decision-chip-row">
            <LifecycleBadge value={member.vps.lifecycle_status} />
            <UsageBadge value={member.vps.usage_status} />
            <RenewalBadge value={member.vps.renewal_decision} />
          </span>
        </div>
      ),
    },
    {
      key: 'subscription',
      label: '订阅',
      width: '188px',
      render: (member) => {
        const sub = member.primary_subscription
        if (!member.source_availability.subscriptions) {
          return (
            <div className="asset-subscription-cell asset-subscription-cell--unknown">
              <strong>订阅证据不可用</strong>
              <span>不会按缺订阅误判</span>
            </div>
          )
        }
        if (!sub) {
          return (
            <div className="asset-subscription-cell asset-subscription-cell--missing">
              <strong>缺订阅</strong>
              <span>需回 VPS 详情补齐</span>
            </div>
          )
        }
        const daysLeft = daysUntilDate(sub.renew_at)
        return (
          <div className="asset-subscription-cell">
            <strong>{formatMoney(sub.monthly_price, sub.currency)}/月</strong>
            <span>{formatDateTime(sub.renew_at)} {daysLeft != null ? `· ${daysLeft}天` : ''}</span>
            <SubscriptionStatusBadge value={sub.status} />
          </div>
        )
      },
    },
    {
      key: 'context',
      label: '上下文',
      width: '248px',
      render: (member) => (
        <div className="asset-table__stack">
          <strong>{memberContextLabel(member)}</strong>
          <span>{sourceAvailabilityLabel(member.source_availability)}</span>
          <span>{member.primary_issue_summary || '暂无主要问题'}</span>
        </div>
      ),
    },
    {
      key: 'suggestion',
      label: '建议',
      width: '244px',
      render: (member) => (
        <div className="asset-table__stack">
          <span className="asset-decision-chip-row">
            <Badge variant="state" tone={roleTone(member.suggested_role)}>
              {ROLE_LABELS[member.suggested_role]}
            </Badge>
            <Badge variant="state" tone={actionTone(member.suggested_action)}>
              {ACTION_LABELS[member.suggested_action]}
            </Badge>
          </span>
          {renderDecisionRecommendation(member.decision_recommendation)}
          {renderEvidenceAssessment(member.evidence_assessment)}
          {member.cancellation_attention_reason && <span>{member.cancellation_attention_reason}</span>}
          {renderEvidenceChips(member.evidence_chips, 4)}
        </div>
      ),
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      width: '172px',
      render: (member) => (
        <div className="asset-decision-member-actions">
          <button className="btn sm primary" type="button" onClick={() => options.onSelect(member)}>
            处理
          </button>
          {member.suggested_action === 'open_cancellation_workbench' ? (
            <Link className="btn sm secondary" to={`/vps/${member.vps.vps_id}?workbench=cancellation`}>
              取消/退役
            </Link>
          ) : (
            <Link className="btn sm secondary" to={`/vps/${member.vps.vps_id}`}>
              VPS
            </Link>
          )}
        </div>
      ),
    },
  ]
}

export function createManualMemberColumns(options: {
  drafts: Record<string, {
    intendedRole: AssetDecisionSuggestedRole
    intendedAction: AssetDecisionSuggestedAction
    reason: string
    note: string
    sortOrder: string
  }>
  saving: Record<string, boolean>
  onUpdateDraft: (vpsID: string, patch: Partial<{
    intendedRole: AssetDecisionSuggestedRole
    intendedAction: AssetDecisionSuggestedAction
    reason: string
    note: string
    sortOrder: string
  }>) => void
  onSubmit: (event: React.FormEvent<HTMLFormElement>, member: AssetDecisionManualGroupMember) => void
  onRequestRemoval: (member: AssetDecisionManualGroupMember) => void
  roleOptions: ReadonlyArray<{ value: AssetDecisionSuggestedRole; label: string }>
  actionOptions: ReadonlyArray<{ value: AssetDecisionSuggestedAction; label: string }>
}): DataTableColumn<AssetDecisionManualGroupMember>[] {
  return [
    {
      key: 'vps',
      label: 'VPS',
      width: '236px',
      render: (member) => {
        const displayName = member.current_fact_found ? member.vps.display_name : member.vps_id
        return (
          <div className="asset-table__identity">
            <strong>
              {member.current_fact_found ? (
                <Link className="name" to={`/vps/${member.vps_id}`}>{displayName}</Link>
              ) : (
                displayName
              )}
            </strong>
            <span>{member.current_fact_found ? `${formatOptional(member.vps.provider_name)} · ${vpsLocationLabel(member.vps)}` : '当前资产事实缺失'}</span>
            <span>{member.current_fact_found ? member.vps.product_name || member.vps_id : member.vps_id}</span>
            <span className="asset-decision-chip-row">
              {member.current_fact_found ? (
                <>
                  <LifecycleBadge value={member.vps.lifecycle_status} />
                  <UsageBadge value={member.vps.usage_status} />
                  <RenewalBadge value={member.vps.renewal_decision} />
                </>
              ) : (
                <Badge variant="state" tone="critical">事实缺失</Badge>
              )}
            </span>
          </div>
        )
      },
    },
    {
      key: 'context',
      label: '当前上下文',
      width: '248px',
      render: (member) => (
        <div className="asset-table__stack">
          <strong>{member.current_fact_found ? memberContextLabel(member) : '无法回读当前事实'}</strong>
          <span>{member.current_fact_found ? sourceAvailabilityLabel(member.source_availability) : '手工组合成员仍在，但当前 facts 未返回该 VPS'}</span>
          <span>{member.primary_issue_summary || '暂无主要问题'}</span>
        </div>
      ),
    },
    {
      key: 'intent',
      label: '组合意图',
      width: '336px',
      render: (member) => {
        const draft = options.drafts[member.vps_id] ?? {
          intendedRole: member.intended_role,
          intendedAction: member.intended_action,
          reason: member.reason,
          note: member.note,
          sortOrder: String(member.sort_order),
        }
        const isSaving = Boolean(options.saving[member.vps_id])
        return (
          <form className="asset-decision-manual-member-form" onSubmit={(event) => options.onSubmit(event, member)}>
            <label className="visually-hidden" htmlFor={`manual-role-${member.manual_group_id}-${member.vps_id}`}>
              {member.vps_id} 组合角色
            </label>
            <select
              id={`manual-role-${member.manual_group_id}-${member.vps_id}`}
              aria-label={`${member.vps_id} 组合角色`}
              className="input"
              value={draft.intendedRole}
              onChange={(event) => options.onUpdateDraft(member.vps_id, { intendedRole: event.target.value as AssetDecisionSuggestedRole })}
            >
              {options.roleOptions.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
            <label className="visually-hidden" htmlFor={`manual-action-${member.manual_group_id}-${member.vps_id}`}>
              {member.vps_id} 组合动作
            </label>
            <select
              id={`manual-action-${member.manual_group_id}-${member.vps_id}`}
              aria-label={`${member.vps_id} 组合动作`}
              className="input"
              value={draft.intendedAction}
              onChange={(event) => options.onUpdateDraft(member.vps_id, { intendedAction: event.target.value as AssetDecisionSuggestedAction })}
            >
              {options.actionOptions.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
            <label className="visually-hidden" htmlFor={`manual-reason-${member.manual_group_id}-${member.vps_id}`}>
              {member.vps_id} 意图理由
            </label>
            <input
              id={`manual-reason-${member.manual_group_id}-${member.vps_id}`}
              aria-label={`${member.vps_id} 意图理由`}
              className="input"
              value={draft.reason}
              placeholder="理由"
              onChange={(event) => options.onUpdateDraft(member.vps_id, { reason: event.target.value })}
            />
            <label className="visually-hidden" htmlFor={`manual-note-${member.manual_group_id}-${member.vps_id}`}>
              {member.vps_id} 备注
            </label>
            <input
              id={`manual-note-${member.manual_group_id}-${member.vps_id}`}
              aria-label={`${member.vps_id} 备注`}
              className="input"
              value={draft.note}
              placeholder="备注"
              onChange={(event) => options.onUpdateDraft(member.vps_id, { note: event.target.value })}
            />
            <div className="asset-decision-manual-member-form__actions">
              <button className="btn sm primary" type="submit" disabled={isSaving}>
                {isSaving ? '保存中…' : '保存意图'}
              </button>
              <button className="btn sm secondary" type="button" onClick={() => options.onRequestRemoval(member)} disabled={isSaving}>
                移除
              </button>
            </div>
          </form>
        )
      },
    },
    {
      key: 'evidence',
      label: '证据',
      width: '250px',
      render: (member) => (
        <div className="asset-table__stack">
          <span className="asset-decision-chip-row">
            <Badge variant="state" tone={roleTone(member.intended_role)}>
              {ROLE_LABELS[member.intended_role]}
            </Badge>
            <Badge variant="state" tone={actionTone(member.intended_action)}>
              {ACTION_LABELS[member.intended_action]}
            </Badge>
          </span>
          {renderDecisionRecommendation(member.decision_recommendation)}
          {renderEvidenceAssessment(member.evidence_assessment)}
          {renderEvidenceChips(member.evidence_chips, 3)}
        </div>
      ),
    },
  ]
}

export function createRecordMemberColumns(options: {
  onRenderFollowupForm: (member: AssetDecisionRecordMember) => React.ReactNode
}): DataTableColumn<AssetDecisionRecordMember>[] {
  return [
    {
      key: 'vps',
      label: 'VPS',
      width: '220px',
      render: (member) => (
        <div className="asset-table__identity">
          <strong><Link className="name" to={`/vps/${member.vps_id}`}>{member.display_name || member.vps_id}</Link></strong>
          <span>{member.vps_id}</span>
          <span>保存于 {formatDateTime(member.created_at)}</span>
        </div>
      ),
    },
    {
      key: 'suggested',
      label: '系统建议',
      width: '180px',
      render: (member) => (
        <span className="asset-decision-chip-row">
          <Badge variant="state" tone={roleTone(member.suggested_role)}>
            {ROLE_LABELS[member.suggested_role]}
          </Badge>
          <Badge variant="state" tone={actionTone(member.suggested_action)}>
            {ACTION_LABELS[member.suggested_action]}
          </Badge>
        </span>
      ),
    },
    {
      key: 'decided',
      label: '用户判断',
      width: '220px',
      render: (member) => (
        <div className="asset-table__stack">
          <span className="asset-decision-chip-row">
            <Badge variant="state" tone={roleTone(member.decided_role)}>
              {ROLE_LABELS[member.decided_role]}
            </Badge>
            <Badge variant="state" tone={actionTone(member.decided_action)}>
              {ACTION_LABELS[member.decided_action]}
            </Badge>
          </span>
          <span>{member.reason || '未填写成员理由'}</span>
        </div>
      ),
    },
    {
      key: 'evidence',
      label: '快照证据',
      width: '308px',
      render: (member) => {
        const assessment = member.evidence_snapshot.evidence_assessment as AssetDecisionEvidenceAssessment | null
        return (
          <div className="asset-table__stack">
            {renderEvidenceAssessment(assessment)}
            <strong>
              服务 {String(member.evidence_snapshot.service_count ?? '—')} · 域名 {String(member.evidence_snapshot.domain_count ?? '—')}
            </strong>
            <span>
              监控 {String(member.evidence_snapshot.running_monitoring_count ?? '—')}/{String(member.evidence_snapshot.monitoring_link_count ?? '—')}
            </span>
            <span>{String(member.evidence_snapshot.primary_issue_summary || '暂无主要问题')}</span>
          </div>
        )
      },
    },
    {
      key: 'readback',
      label: '当前回读',
      width: '312px',
      render: (member) => renderMemberReadback(member.execution_readback),
    },
    {
      key: 'plan',
      label: '下一步',
      width: '248px',
      render: (member) => renderMemberExecutionPlan(member),
    },
    {
      key: 'actions',
      label: '跟进',
      align: 'right',
      width: '286px',
      render: (member) => options.onRenderFollowupForm(member),
    },
  ]
}

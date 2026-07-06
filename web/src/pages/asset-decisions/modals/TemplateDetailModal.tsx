import { Modal, Tabs, Badge } from '../../../components/atoms'
import { PageState as PageStateView } from '../../../components/PageState'
import type {
  AssetDecisionScenarioTemplateStatus,
} from '../../../lib/types'
import {
  MANUAL_GROUP_SCENARIO_LABELS,
  RENEWAL_WINDOWS,
  ROLE_LABELS,
  ACTION_LABELS,
  SCENARIO_TEMPLATE_STATUS_LABELS,
} from '../constants'
import {
  compactDecisionText,
  scenarioTemplateStatusTone,
  roleTone,
  actionTone,
} from '../formatters'
import { parseRenewalWindow } from '../utils'
import {
  renderDetailCommand,
  renderDetailPanel,
  renderCompactTaskHeader,
} from '../renderHelpers'
import type {
  FormSubmitEvent,
  TemplateDetailPanel,
  TemplateDetailState,
  TemplateManualGroupDraft,
} from '../types'

type TemplateDetailModalProps = {
  open: boolean
  templateDetailState: TemplateDetailState
  templateDetailPanel: TemplateDetailPanel
  templateError: string | null
  templateSaving: boolean
  pendingTemplateStatus: AssetDecisionScenarioTemplateStatus | null
  templateManualDraft: TemplateManualGroupDraft
  onClose: () => void
  onSetTemplateDetailPanel: (panel: TemplateDetailPanel) => void
  onRequestTemplateStatusUpdate: (status: AssetDecisionScenarioTemplateStatus) => void
  onCancelTemplateStatusUpdate: () => void
  onUpdateTemplateStatus: (status: AssetDecisionScenarioTemplateStatus) => void
  onSubmitTemplateManualGroup: (event: FormSubmitEvent) => void
  onSetTemplateManualDraft: React.Dispatch<React.SetStateAction<TemplateManualGroupDraft>>
}

export function TemplateDetailModal({
  open,
  templateDetailState,
  templateDetailPanel,
  templateError,
  templateSaving,
  pendingTemplateStatus,
  templateManualDraft,
  onClose,
  onSetTemplateDetailPanel,
  onRequestTemplateStatusUpdate,
  onCancelTemplateStatusUpdate,
  onUpdateTemplateStatus,
  onSubmitTemplateManualGroup,
  onSetTemplateManualDraft,
}: TemplateDetailModalProps) {
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={templateDetailState.detail?.title ?? '场景模板'}
      ariaLabel="资产决策场景模板详情"
      size="xl"
      contentClassName="asset-decision-template-modal"
    >
      {templateDetailState.loading ? (
        <PageStateView kind="loading" title="正在加载场景模板…" surface="empty" compact />
      ) : templateDetailState.error ? (
        <PageStateView
          kind="error"
          title="场景模板不可用"
          surface="empty"
          compact
        />
      ) : templateDetailState.detail ? (
        <div className="asset-decision-detail asset-decision-template-detail">
          <Tabs
            items={[
              { value: 'overview', label: '概览' },
              { value: templateDetailPanel === 'create' ? 'create' : 'members', label: templateDetailPanel === 'create' ? '创建' : '成员', count: templateDetailState.detail.member_count },
              ...(!templateDetailState.detail.builtin ? [{ value: 'status' as const, label: '状态' }] : []),
            ]}
            value={templateDetailPanel}
            onChange={(value) => onSetTemplateDetailPanel(value as TemplateDetailPanel)}
          />
          {templateDetailPanel === 'overview' && renderDetailCommand({
            ariaLabel: '场景模板当前判断',
            title: '当前模板',
            summary: compactDecisionText(templateDetailState.detail.goal, '创建组合后细化'),
            footer: <span className="asset-decision-detail-command__context">
              {MANUAL_GROUP_SCENARIO_LABELS[templateDetailState.detail.scenario]} · {SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status]} · 蓝图 {templateDetailState.detail.member_count}
            </span>,
            badge: (
              <Badge variant="state" tone={scenarioTemplateStatusTone(templateDetailState.detail.status)}>
                {SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status]}
              </Badge>
            ),
            actions: (
              <button
                className="btn md primary"
                type="button"
                onClick={() => onSetTemplateDetailPanel('create')}
                disabled={templateDetailState.detail.status !== 'active'}
              >
                创建组合
              </button>
            ),
          })}
          {templateError && <div className="inline-alert danger">{templateError}</div>}

          {templateDetailPanel === 'status' && !templateDetailState.detail.builtin && renderDetailPanel('状态维护',
            <>
              <div className="asset-decision-template-status-actions">
                <Badge variant="state" tone={scenarioTemplateStatusTone(templateDetailState.detail.status)}>
                  {SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status]}
                </Badge>
                <button
                  className="btn sm secondary"
                  type="button"
                  onClick={() => onRequestTemplateStatusUpdate(templateDetailState.detail!.status === 'active' ? 'archived' : 'active')}
                  disabled={templateSaving}
                >
                  {templateSaving ? '更新中…' : templateDetailState.detail.status === 'active' ? '归档模板' : '重新启用'}
                </button>
              </div>
              {pendingTemplateStatus ? (
          <section className="asset-lifecycle-confirm" role="alertdialog" aria-label={pendingTemplateStatus === 'archived' ? '确认归档模板' : '确认重新启用模板'}>
            <p className="asset-lifecycle-confirm__eyebrow">操作确认</p>
            <h3>{pendingTemplateStatus === 'archived' ? '确认归档模板' : '确认重新启用模板'}</h3>
            <div className="asset-lifecycle-confirm__flow">
              <span>当前：模板状态为 {SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status]}。</span>
              <span>操作后：模板状态变为 {SCENARIO_TEMPLATE_STATUS_LABELS[pendingTemplateStatus]}。</span>
            </div>
            <div className="asset-lifecycle-confirm__callouts">
              <p>{pendingTemplateStatus === 'archived' ? '归档后不能直接从该模板创建新组合。' : '重新启用后可继续从该模板创建自定义组合。'}</p>
              <p>不会修改已经创建的自定义组合、决策记录或任何 VPS 资产事实。</p>
            </div>
            <div className="asset-operation-actions">
              <button className="btn sm secondary" type="button" disabled={templateSaving} onClick={onCancelTemplateStatusUpdate}>
                取消
              </button>
              <button className="btn sm primary" type="button" disabled={templateSaving} onClick={() => onUpdateTemplateStatus(pendingTemplateStatus)}>
                {templateSaving ? '更新中…' : pendingTemplateStatus === 'archived' ? '确认归档模板' : '确认重新启用'}
              </button>
            </div>
          </section>
              ) : null}
            </>,
          )}

          {templateDetailPanel === 'create' && renderDetailPanel('创建组合',
            <form className="asset-decision-template-form" onSubmit={onSubmitTemplateManualGroup}>
            <div className="asset-decision-record-form__header">
              {renderCompactTaskHeader('从模板创建自定义组合', SCENARIO_TEMPLATE_STATUS_LABELS[templateDetailState.detail.status])}
              <button className="btn md primary" type="submit" disabled={templateSaving || templateDetailState.detail.status !== 'active'}>
                {templateSaving ? '创建中…' : '创建组合'}
              </button>
            </div>
            {templateError && <div className="inline-alert danger">{templateError}</div>}
            <div className="asset-decision-template-form__grid">
              <label className="input-field">
                <span>标题</span>
                <input
                  className="input"
                  value={templateManualDraft.title}
                  onChange={(event) => onSetTemplateManualDraft((current) => ({ ...current, title: event.target.value }))}
                />
              </label>
              <label className="input-field">
                <span>续费窗口</span>
                <select
                  className="input"
                  value={String(templateManualDraft.renewWithinDays)}
                  onChange={(event) => onSetTemplateManualDraft((current) => ({ ...current, renewWithinDays: parseRenewalWindow(event.target.value) }))}
                >
                  {RENEWAL_WINDOWS.map((value) => (
                    <option key={value} value={value}>未来 {value} 天</option>
                  ))}
                </select>
              </label>
              <label className="input-field asset-decision-record-form__goal">
                <span>组合目标</span>
                <textarea
                  className="input"
                  rows={2}
                  value={templateManualDraft.goal}
                  onChange={(event) => onSetTemplateManualDraft((current) => ({ ...current, goal: event.target.value }))}
                />
              </label>
              <label className="input-field asset-decision-record-form__goal">
                <span>备注</span>
                <textarea
                  className="input"
                  rows={2}
                  value={templateManualDraft.note}
                  onChange={(event) => onSetTemplateManualDraft((current) => ({ ...current, note: event.target.value }))}
                />
              </label>
            </div>
          </form>,
          )}

          {templateDetailPanel === 'members' && renderDetailPanel('成员蓝图',
            <div className="asset-decision-template-members">
            <div className="asset-decision-record-form__header">
              {renderCompactTaskHeader('成员蓝图', `成员 ${templateDetailState.detail.members.length}`)}
            </div>
            {templateDetailState.detail.members.length === 0 ? (
              <PageStateView
                kind="empty"
                title="模板未固定成员"
                description="创建自定义组合后可在组合详情中加入 VPS 并保存成员意图。"
                surface="empty"
                compact
              />
            ) : (
              <div className="asset-decision-template-member-list">
                {templateDetailState.detail.members.map((member, index) => (
                  <div className="asset-decision-template-member" key={member.member_id || `${member.vps_id}-${member.sort_order}`}>
                    <div className="asset-table__identity">
                      <strong>{member.vps_id ? `成员 ${index + 1}` : '待补成员'}</strong>
                      <span>{ROLE_LABELS[member.intended_role ?? 'observe_candidate']} · {ACTION_LABELS[member.intended_action ?? 'review']}</span>
                    </div>
                    <span className="asset-decision-chip-row">
                      <Badge variant="state" tone={roleTone(member.intended_role ?? 'observe_candidate')}>
                        {ROLE_LABELS[member.intended_role ?? 'observe_candidate']}
                      </Badge>
                      <Badge variant="state" tone={actionTone(member.intended_action ?? 'review')}>
                        {ACTION_LABELS[member.intended_action ?? 'review']}
                      </Badge>
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>,
          )}
        </div>
      ) : null}
    </Modal>
  )
}

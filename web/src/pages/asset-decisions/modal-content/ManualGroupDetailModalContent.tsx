import type { ReactNode } from 'react'

import { ActionConfirmationModal } from '../../../components/ActionConfirmationModal'
import { Modal, TabPanel, Tabs, Badge, DataTable, type DataTableColumn } from '../../../components/atoms'
import { PageState as PageStateView } from '../../../components/PageState'
import type {
  AssetDecisionManualGroupDetail,
  AssetDecisionManualGroupMember,
  AssetDecisionRecordStatus,
  AssetDecisionSuggestedAction,
  AssetDecisionSuggestedRole,
  VPSAssetRecord,
} from '../../../lib/types'
import { formatOptional } from '../../../lib/format'
import { vpsLocationLabel } from '../../assetPageUtils'
import {
  MANUAL_GROUP_SCENARIO_OPTIONS,
  MANUAL_GROUP_STATUS_LABELS,
  RECORD_STATUS_OPTIONS,
  ROLE_OPTIONS,
  ACTION_OPTIONS,
} from '../constants'
import {
  compactDecisionText,
  compactVPSOptionLabel,
  manualCoverMeta,
} from '../formatters'
import {
  renderDetailCommand,
  renderDetailPanel,
  renderMemberDecisionRows,
  renderCompactTaskHeader,
  manualMemberComparisonMatrixMember,
} from '../renderHelpers'
import type {
  FormSubmitEvent,
  ManualDetailPanel,
  ManualDetailState,
  ManualGroupProgress,
  ManualMemberAddDraft,
  RecordDraft,
} from '../types'

type VpsCatalogState = {
  loading: boolean
  error: string | null
}

type ManualGroupDetailModalProps = {
  open: boolean
  manualDetailState: ManualDetailState
  manualDetailPanel: ManualDetailPanel
  manualGroupProgress: ManualGroupProgress | null
  manualGroupError: string | null
  manualGroupSaving: boolean
  templateSaving: boolean
  manualMemberSaving: Record<string, boolean>
  pendingManualMemberRemoval: AssetDecisionManualGroupMember | null
  manualMemberAddDraft: ManualMemberAddDraft
  manualMemberAddAdvanced: boolean
  vpsCatalogState: VpsCatalogState
  manualMemberCandidateRows: VPSAssetRecord[]
  recordDraft: RecordDraft | null
  recordSaving: boolean
  recordSaveError: string | null
  manualMemberColumns: DataTableColumn<AssetDecisionManualGroupMember>[]
  onClose: () => void
  onSelectManualDetailPanel: (panel: ManualDetailPanel) => void
  onStartManualRecordSave: (detail: AssetDecisionManualGroupDetail) => void
  onSubmitRecordSave: (event: FormSubmitEvent) => void
  onCancelRecordSave: () => void
  onSubmitManualGroupPatch: (event: FormSubmitEvent) => void
  onSaveManualGroupAsTemplate: (detail: AssetDecisionManualGroupDetail) => void
  onSubmitManualMemberAdd: (event: FormSubmitEvent) => void
  onRequestManualMemberRemoval: (member: AssetDecisionManualGroupMember) => void
  onCancelManualMemberRemoval: () => void
  onDeleteManualMember: (member: AssetDecisionManualGroupMember) => void
  onUpdateMemberAddDraft: (patch: Partial<ManualMemberAddDraft>) => void
  onSetManualMemberAddAdvancedVisible: (visible: boolean) => void
  onUpdateRecordDraft: (patch: Partial<RecordDraft>) => void
  renderRecordDraftMemberRows: (members: Array<{
    vpsID: string
    displayName: string
    fallbackRole: AssetDecisionSuggestedRole
    fallbackAction: AssetDecisionSuggestedAction
    meta?: string
  }>) => ReactNode
}

export function ManualGroupDetailModal({
  open,
  manualDetailState,
  manualDetailPanel,
  manualGroupProgress,
  manualGroupError,
  manualGroupSaving,
  templateSaving,
  manualMemberSaving,
  pendingManualMemberRemoval,
  manualMemberAddDraft,
  manualMemberAddAdvanced,
  vpsCatalogState,
  manualMemberCandidateRows,
  recordDraft,
  recordSaving,
  recordSaveError,
  manualMemberColumns,
  onClose,
  onSelectManualDetailPanel,
  onStartManualRecordSave,
  onSubmitRecordSave,
  onCancelRecordSave,
  onSubmitManualGroupPatch,
  onSaveManualGroupAsTemplate,
  onSubmitManualMemberAdd,
  onRequestManualMemberRemoval,
  onCancelManualMemberRemoval,
  onDeleteManualMember,
  onUpdateMemberAddDraft,
  onSetManualMemberAddAdvancedVisible,
  onUpdateRecordDraft,
  renderRecordDraftMemberRows,
}: ManualGroupDetailModalProps) {
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={manualDetailState.detail?.title ?? '自定义组合详情'}
      ariaLabel="自定义资产组合详情"
      size="xl"
      contentClassName="asset-decision-manual-modal"
    >
      {manualDetailState.loading ? (
        <PageStateView kind="loading" title="正在加载自定义组合详情…" surface="empty" compact />
      ) : manualDetailState.error ? (
        <PageStateView
          kind="error"
          title="自定义组合详情不可用"
          surface="empty"
          compact
        />
      ) : manualDetailState.detail && manualGroupProgress ? (
        <div className="asset-decision-detail asset-decision-manual-detail">
          <Tabs
            label="自定义组合详情分区"
            idBase="asset-manual-group-detail"
            items={[
              { value: 'overview', label: '概览' },
              { value: manualDetailPanel === 'add' || manualDetailPanel === 'raw' ? manualDetailPanel : 'members', label: manualDetailPanel === 'add' ? '添加' : manualDetailPanel === 'raw' ? '底稿' : '成员', count: manualDetailState.detail.members.length },
              { value: manualDetailPanel === 'save' ? 'save' : 'edit', label: manualDetailPanel === 'save' ? '保存' : '编辑' },
            ]}
            value={manualDetailPanel}
            onChange={(value) => onSelectManualDetailPanel(value as ManualDetailPanel)}
          />
          <TabPanel
            idBase="asset-manual-group-detail"
            value={manualDetailPanel}
            className="asset-decision-tab-panel"
          >
          {manualDetailPanel === 'overview' && renderDetailCommand({
            ariaLabel: '自定义组合当前判断',
            title: '当前判断',
            summary: compactDecisionText(
              manualDetailState.detail.comparison_insight?.summary
                || manualDetailState.detail.decision_recommendation?.summary
                || manualDetailState.detail.goal,
              '继续整理组合',
            ),
            footer: <span className="asset-decision-detail-command__context">{manualCoverMeta(manualDetailState.detail, manualGroupProgress)}</span>,
            assessment: manualDetailState.detail.evidence_assessment,
            recommendation: manualDetailState.detail.decision_recommendation,
            insight: manualDetailState.detail.comparison_insight,
            chips: manualDetailState.detail.evidence_chips,
            badge: (
              <Badge variant="state" tone={manualGroupProgress.readinessTone}>
                {manualGroupProgress.readinessLabel} {manualGroupProgress.doneCount}/{manualGroupProgress.totalCount}
              </Badge>
            ),
            actions: (
              <button
                className="btn md primary"
                type="button"
                onClick={() => onStartManualRecordSave(manualDetailState.detail!)}
              >
                保存记录
              </button>
            ),
          })}
          {manualGroupError && <div className="inline-alert danger">{manualGroupError}</div>}
          {manualDetailPanel === 'members' && renderDetailPanel('成员维护',
            renderMemberDecisionRows(
              manualDetailState.detail.members.map(manualMemberComparisonMatrixMember),
              {
                title: '成员取舍',
                ariaLabel: '自定义组合成员取舍',
                showIntent: true,
                action: (member) => {
                  const sourceMember = manualDetailState.detail?.members.find((item) => item.vps_id === member.key)
                  if (!sourceMember) return null
                  return (
                    <button
                      className="btn sm secondary"
                      type="button"
                      disabled={Boolean(manualMemberSaving[sourceMember.vps_id])}
                      onClick={() => onRequestManualMemberRemoval(sourceMember)}
                    >
                      移除
                    </button>
                  )
                },
                hiddenAction: (
                  <button className="btn-text sm secondary" type="button" onClick={() => onSelectManualDetailPanel('raw')}>
                    查看成员数据
                  </button>
                ),
                footerAction: (
                  <button className="btn sm primary" type="button" onClick={() => onSelectManualDetailPanel('add')}>
                    添加成员
                  </button>
                ),
              },
            ),
          )}

          {manualDetailPanel === 'edit' && renderDetailPanel('编辑组合',
            <form id="asset-decision-manual-group-form" className="asset-decision-manual-group-form" onSubmit={onSubmitManualGroupPatch}>
            <div className="asset-decision-record-form__header">
              {renderCompactTaskHeader('组合场景', MANUAL_GROUP_STATUS_LABELS[manualDetailState.detail.status])}
            </div>
            {manualGroupError && <div className="inline-alert danger">{manualGroupError}</div>}
            <div className="asset-decision-manual-group-form__grid">
              <label className="input-field">
                <span>标题</span>
                <input className="input" name="title" defaultValue={manualDetailState.detail.title} />
              </label>
              <label className="input-field">
                <span>场景</span>
                <select className="input" name="scenario" defaultValue={manualDetailState.detail.scenario}>
                  {MANUAL_GROUP_SCENARIO_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>{option.label}</option>
                  ))}
                </select>
              </label>
              <label className="input-field">
                <span>状态</span>
                <select className="input" name="status" defaultValue={manualDetailState.detail.status}>
                  <option value="active">进行中</option>
                  <option value="archived">已归档</option>
                </select>
              </label>
              <label className="input-field asset-decision-record-form__goal">
                <span>组合目标</span>
                <textarea className="input" name="goal" rows={2} defaultValue={manualDetailState.detail.goal} />
              </label>
              <label className="input-field asset-decision-record-form__goal">
                <span>备注</span>
                <textarea className="input" name="note" rows={2} defaultValue={manualDetailState.detail.note} />
              </label>
            </div>
            <div className="asset-operation-actions">
              <button className="btn md primary" type="submit" disabled={manualGroupSaving}>
                {manualGroupSaving ? '保存中…' : '保存组合'}
              </button>
              <button
                className="btn md secondary"
                type="button"
                onClick={() => onSaveManualGroupAsTemplate(manualDetailState.detail!)}
                disabled={templateSaving}
              >
                {templateSaving ? '保存中…' : '另存为模板'}
              </button>
            </div>
          </form>,
          )}

          {manualDetailPanel === 'add' && renderDetailPanel('添加成员',
            <form className="asset-decision-manual-add-form" onSubmit={onSubmitManualMemberAdd}>
            <div className="asset-decision-record-form__header">
              {renderCompactTaskHeader('加入 VPS', `候选 ${manualMemberCandidateRows.length}`)}
              <div className="asset-decision-member-actions">
                <button
                  className="btn sm secondary"
                  type="button"
                  aria-expanded={manualMemberAddAdvanced}
                  onClick={() => onSetManualMemberAddAdvancedVisible(!manualMemberAddAdvanced)}
                >
                  高级选项
                </button>
                <button className="btn md primary" type="submit" disabled={manualGroupSaving || vpsCatalogState.loading || !manualMemberAddDraft.vpsID}>
                  {manualGroupSaving ? '加入中…' : '加入组合'}
                </button>
              </div>
            </div>
            {vpsCatalogState.error && <div className="inline-alert danger">{vpsCatalogState.error}</div>}
            <div className="asset-decision-manual-add-form__grid">
              <label className="input-field">
                <span>VPS</span>
                <select
                  className="input"
                  value={manualMemberAddDraft.vpsID}
                  onChange={(event) => onUpdateMemberAddDraft({ vpsID: event.target.value })}
                  disabled={vpsCatalogState.loading || Boolean(vpsCatalogState.error)}
                >
                  <option value="">{vpsCatalogState.loading ? '正在加载 VPS…' : manualMemberCandidateRows.length === 0 ? '暂无可加入 VPS' : '选择 VPS'}</option>
                  {manualMemberCandidateRows.map((vps) => (
                    <option key={vps.vps_id} value={vps.vps_id}>
                      {compactVPSOptionLabel(vps)}
                    </option>
                  ))}
                </select>
              </label>
              <label className="input-field">
                <span>角色</span>
                <select
                  className="input"
                  value={manualMemberAddDraft.intendedRole}
                  onChange={(event) => onUpdateMemberAddDraft({ intendedRole: event.target.value as AssetDecisionSuggestedRole })}
                >
                  {ROLE_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>{option.label}</option>
                  ))}
                </select>
              </label>
              <label className="input-field">
                <span>动作</span>
                <select
                  className="input"
                  value={manualMemberAddDraft.intendedAction}
                  onChange={(event) => onUpdateMemberAddDraft({ intendedAction: event.target.value as AssetDecisionSuggestedAction })}
                >
                  {ACTION_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>{option.label}</option>
                  ))}
                </select>
              </label>
              {manualMemberAddAdvanced && (
                <>
                  <label className="input-field">
                    <span>排序</span>
                    <input
                      className="input"
                      inputMode="numeric"
                      value={manualMemberAddDraft.sortOrder}
                      onChange={(event) => onUpdateMemberAddDraft({ sortOrder: event.target.value })}
                      placeholder="自动"
                    />
                  </label>
                  <label className="input-field">
                    <span>理由</span>
                    <input
                      className="input"
                      value={manualMemberAddDraft.reason}
                      onChange={(event) => onUpdateMemberAddDraft({ reason: event.target.value })}
                      placeholder="加入组合的原因"
                    />
                  </label>
                  <label className="input-field">
                    <span>备注</span>
                    <input
                      className="input"
                      value={manualMemberAddDraft.note}
                      onChange={(event) => onUpdateMemberAddDraft({ note: event.target.value })}
                      placeholder="可选"
                    />
                  </label>
                </>
              )}
            </div>
          </form>,
          )}

          {manualDetailPanel === 'save' && recordDraft && recordDraft.sourceType === 'manual_group' && renderDetailPanel('保存记录',
            <form className="asset-decision-record-form" onSubmit={onSubmitRecordSave}>
              <div className="asset-decision-record-form__header">
                {renderCompactTaskHeader('保存自定义组合决策', `成员 ${manualDetailState.detail.members.length}`)}
                <div className="asset-decision-member-actions">
                  <button className="btn md primary" type="submit" disabled={recordSaving}>
                    {recordSaving ? '保存中…' : '保存记录'}
                  </button>
                  <button className="btn md secondary" type="button" onClick={onCancelRecordSave} disabled={recordSaving}>
                    取消
                  </button>
                </div>
              </div>
              {recordSaveError && <div className="inline-alert danger">{recordSaveError}</div>}
              <div className="asset-decision-record-form__grid">
                <label className="input-field">
                  <span>标题</span>
                  <input
                    className="input"
                    value={recordDraft.title}
                    onChange={(event) => onUpdateRecordDraft({ title: event.target.value })}
                  />
                </label>
                <label className="input-field">
                  <span>状态</span>
                  <select
                    className="input"
                    value={recordDraft.status}
                    onChange={(event) => onUpdateRecordDraft({ status: event.target.value as AssetDecisionRecordStatus })}
                  >
                    {RECORD_STATUS_OPTIONS.map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                </label>
                <label className="input-field asset-decision-record-form__goal">
                  <span>组合目标</span>
                  <textarea
                    className="input"
                    value={recordDraft.goal}
                    rows={2}
                    onChange={(event) => onUpdateRecordDraft({ goal: event.target.value })}
                  />
                </label>
              </div>
              {renderRecordDraftMemberRows(manualDetailState.detail.members.map((member) => ({
                vpsID: member.vps_id,
                displayName: member.current_fact_found ? member.vps.display_name || member.vps_id : member.vps_id,
                fallbackRole: member.intended_role,
                fallbackAction: member.intended_action,
                meta: member.current_fact_found ? `${formatOptional(member.vps.provider_name)} · ${vpsLocationLabel(member.vps)}` : '当前事实缺失',
              })))}
            </form>,
          )}

          <ActionConfirmationModal
            open={pendingManualMemberRemoval != null}
            title="确认移除组合成员"
            current={pendingManualMemberRemoval
              ? `${pendingManualMemberRemoval.current_fact_found ? pendingManualMemberRemoval.vps.display_name : pendingManualMemberRemoval.vps_id} 在当前组合中`
              : '成员在当前组合中'}
            result="该 VPS 从自定义组合移除"
            impact="会删除这个组合里的成员意图、理由、备注和排序。"
            unchanged="不会修改 VPS、订阅、监控实例、Target 或已保存决策记录。"
            confirmLabel={pendingManualMemberRemoval && manualMemberSaving[pendingManualMemberRemoval.vps_id] ? '移除中…' : '确认移除'}
            disabled={Boolean(pendingManualMemberRemoval && manualMemberSaving[pendingManualMemberRemoval.vps_id])}
            onCancel={onCancelManualMemberRemoval}
            onConfirm={() => {
              if (pendingManualMemberRemoval) onDeleteManualMember(pendingManualMemberRemoval)
            }}
          />

          {manualDetailPanel === 'raw' && renderDetailPanel('成员数据',
            <div className="asset-table-scroll" role="region" aria-label="自定义组合成员对比" tabIndex={0}>
              <DataTable
                className="asset-table asset-decision-manual-members-table"
                columns={manualMemberColumns}
                rows={manualDetailState.detail.members}
                rowKey={(member) => member.vps_id}
              />
            </div>,
            true,
          )}
          </TabPanel>
        </div>
      ) : null}
    </Modal>
  )
}

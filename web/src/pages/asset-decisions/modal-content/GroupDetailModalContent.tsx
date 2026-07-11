import { Link } from 'react-router-dom'

import { Modal, TabPanel, Tabs, DataTable, type DataTableColumn } from '../../../components/atoms'
import { PageState as PageStateView } from '../../../components/PageState'
import { AssetDecisionWorkPanel, type AssetDecisionDraft } from '../../../components/AssetDecisionWorkPanel'
import type {
  AssetDecisionGroupDetail,
  AssetDecisionGroupMember,
  AssetDecisionRecordStatus,
  VPSAssetRecord,
} from '../../../lib/types'
import { formatOptional } from '../../../lib/format'
import { vpsLocationLabel } from '../../assetPageUtils'
import { RECORD_STATUS_OPTIONS } from '../constants'
import { vpsWorkbenchPath } from '../paths'
import { compactGroupJudgement } from '../formatters'
import { RecordDraftMemberRows } from '../components/RecordDraftMemberRows'
import {
  renderDetailCommand,
  renderDetailPanel,
  renderMemberDecisionRows,
  renderCompactTaskHeader,
  groupMemberComparisonMatrixMember,
} from '../renderHelpers'
import type {
  DetailState,
  FormSubmitEvent,
  GroupDetailPanel,
  RecordDraft,
  RecordMemberDraft,
} from '../types'

type GroupDetailModalProps = {
  open: boolean
  detailState: DetailState
  groupDetailPanel: GroupDetailPanel
  recordDraft: RecordDraft | null
  recordDraftEditingMemberID: string | null
  recordSaving: boolean
  recordSaveError: string | null
  manualGroupCreating: boolean
  manualGroupError: string | null
  selectedVPS: VPSAssetRecord | null
  decisionDraft: AssetDecisionDraft
  decisionSubmitting: boolean
  decisionError: string | null
  memberColumns: DataTableColumn<AssetDecisionGroupMember>[]
  onClose: () => void
  onSetGroupDetailPanel: (panel: GroupDetailPanel) => void
  onUpdateRecordDraft: (patch: Partial<Pick<RecordDraft, 'title' | 'goal' | 'status'>>) => void
  onUpdateRecordDraftMember: (vpsID: string, patch: Partial<RecordMemberDraft>) => void
  onEditRecordDraftMember: (vpsID: string | null) => void
  onStartRecordSave: (detail: AssetDecisionGroupDetail) => void
  onSubmitRecordSave: (event: FormSubmitEvent) => void
  onCancelRecordSave: () => void
  onCreateManualGroupFromAuto: (detail: AssetDecisionGroupDetail) => void
  onSelectVPS: (vps: VPSAssetRecord) => void
  onCloseDecisionDrawer: () => void
  onHandleDecisionSubmit: (event: FormSubmitEvent) => void
  onSetDecisionDraft: React.Dispatch<React.SetStateAction<AssetDecisionDraft>>
}

export function GroupDetailModal({
  open,
  detailState,
  groupDetailPanel,
  recordDraft,
  recordDraftEditingMemberID,
  recordSaving,
  recordSaveError,
  manualGroupCreating,
  manualGroupError,
  selectedVPS,
  decisionDraft,
  decisionSubmitting,
  decisionError,
  memberColumns,
  onClose,
  onSetGroupDetailPanel,
  onUpdateRecordDraft,
  onUpdateRecordDraftMember,
  onEditRecordDraftMember,
  onStartRecordSave,
  onSubmitRecordSave,
  onCancelRecordSave,
  onCreateManualGroupFromAuto,
  onSelectVPS,
  onCloseDecisionDrawer,
  onHandleDecisionSubmit,
  onSetDecisionDraft,
}: GroupDetailModalProps) {
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={detailState.detail?.title ?? '决策组详情'}
      ariaLabel="资产决策组详情"
      size="xl"
      contentClassName="asset-decision-group-modal"
    >
      {detailState.loading ? (
        <PageStateView kind="loading" title="正在加载决策组详情…" surface="empty" compact />
      ) : detailState.error ? (
        <PageStateView
          kind="error"
          title="决策组详情不可用"
          surface="empty"
          compact
        />
      ) : detailState.detail ? (
        <div className="asset-decision-detail">
          <Tabs
            label="决策组详情分区"
            idBase="asset-group-detail"
            items={[
              { value: 'overview', label: '概览' },
              {
                value: groupDetailPanel === 'raw' ? 'raw' : 'members',
                label: groupDetailPanel === 'raw' ? '底稿' : '成员',
                count: detailState.detail.members.length,
              },
              {
                value: groupDetailPanel === 'vps' ? 'vps' : 'save',
                label: groupDetailPanel === 'vps' ? '处理' : '保存',
              },
            ]}
            value={groupDetailPanel}
            onChange={(value) => {
              if (value === 'save') {
                onStartRecordSave(detailState.detail!)
                return
              }
              onSetGroupDetailPanel(value as GroupDetailPanel)
            }}
          />
          <TabPanel
            idBase="asset-group-detail"
            value={groupDetailPanel}
            className="asset-decision-tab-panel"
          >
          {groupDetailPanel === 'overview' && renderDetailCommand({
            ariaLabel: '决策组当前判断',
            title: '',
            summary: compactGroupJudgement(detailState.detail),
            assessment: detailState.detail.evidence_assessment,
            recommendation: detailState.detail.decision_recommendation,
            insight: detailState.detail.comparison_insight,
            chips: detailState.detail.evidence_chips,
            actions: (
              <button
                className="btn md primary"
                type="button"
                onClick={() => onCreateManualGroupFromAuto(detailState.detail!)}
                disabled={manualGroupCreating}
              >
                {manualGroupCreating ? '创建中…' : '创建组合'}
              </button>
            ),
          })}
          {manualGroupError && <div className="inline-alert danger">{manualGroupError}</div>}
          {groupDetailPanel === 'members' && renderDetailPanel('成员明细',
            renderMemberDecisionRows(
              detailState.detail.members.map(groupMemberComparisonMatrixMember),
              {
                title: '成员取舍',
                ariaLabel: '成员取舍列表',
                action: (member) => {
                  const sourceMember = detailState.detail?.members.find((item) => item.vps.vps_id === member.key)
                  if (!sourceMember) return null
                  if (sourceMember.suggested_action === 'open_cancellation_workbench') {
                    return (
                      <Link className="btn sm primary" to={vpsWorkbenchPath(sourceMember.vps.vps_id, 'cancellation')}>
                        取消/退役
                      </Link>
                    )
                  }
                  return (
                    <button className="btn sm primary" type="button" onClick={() => onSelectVPS(sourceMember.vps)}>
                      处理
                    </button>
                  )
                },
                hiddenAction: (
                  <button className="btn-text sm secondary" type="button" onClick={() => onSetGroupDetailPanel('raw')}>
                    查看数据底稿
                  </button>
                ),
              },
            ),
          )}
          {groupDetailPanel === 'save' && recordDraft && renderDetailPanel('保存记录',
            <form className="asset-decision-record-form" onSubmit={onSubmitRecordSave}>
              <div className="asset-decision-record-form__header">
                {renderCompactTaskHeader('保存组合决策记录', `成员 ${detailState.detail.members.length}`)}
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
              <RecordDraftMemberRows
                members={detailState.detail.members.map((member) => ({
                  vpsID: member.vps.vps_id,
                  displayName: member.vps.display_name || member.vps.vps_id,
                  fallbackRole: member.suggested_role,
                  fallbackAction: member.suggested_action,
                  meta: `${formatOptional(member.vps.provider_name)} · ${vpsLocationLabel(member.vps)}`,
                }))}
                draft={recordDraft}
                editingMemberID={recordDraftEditingMemberID}
                onEditMember={onEditRecordDraftMember}
                onUpdateMember={onUpdateRecordDraftMember}
              />
            </form>,
          )}
          {groupDetailPanel === 'raw' && renderDetailPanel('数据底稿',
            <div className="asset-table-scroll" role="region" aria-label="决策组成员对比" tabIndex={0}>
              <DataTable
                className="asset-table asset-decision-members-table"
                columns={memberColumns}
                rows={detailState.detail.members}
                rowKey={(member) => member.vps.vps_id}
              />
            </div>,
            true,
          )}
          {groupDetailPanel === 'vps' && selectedVPS && (
            <div className="asset-decision-detail__work-panel">
              <AssetDecisionWorkPanel
                surface="drawer"
                selectedVPS={selectedVPS}
                decisionDraft={decisionDraft}
                submitting={decisionSubmitting}
                error={decisionError}
                notice={null}
                onDraftChange={onSetDecisionDraft}
                onSubmit={onHandleDecisionSubmit}
                onCancel={onCloseDecisionDrawer}
              />
            </div>
          )}
          </TabPanel>
        </div>
      ) : null}
    </Modal>
  )
}

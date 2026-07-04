import { Modal, Tabs, Badge, DataTable, type DataTableColumn } from '../../../components/atoms'
import { PageState as PageStateView } from '../../../components/PageState'
import type {
  AssetDecisionEvidenceAssessment,
  AssetDecisionRecordDetail,
  AssetDecisionRecordMember,
  AssetDecisionRecordStatus,
} from '../../../lib/types'
import {
  READBACK_STATUS_LABELS,
  RECORD_STATUS_OPTIONS,
} from '../constants'
import {
  readbackStatusTone,
  recordCoverSummary,
  recordCoverMeta,
  recordSourceLabel,
  recordSourceDetail,
} from '../formatters'
import { parseComparisonInsight } from '../utils'
import {
  renderDetailCommand,
  renderDetailPanel,
} from '../renderHelpers'
import type { RecordDetailPanel } from '../types'

type RecordDetailState = {
  loading: boolean
  error: string | null
  detail: AssetDecisionRecordDetail | null
}

type RecordDetailModalProps = {
  open: boolean
  recordDetailState: RecordDetailState
  recordDetailPanel: RecordDetailPanel
  recordPatchError: string | null
  recordPatchStatus: AssetDecisionRecordStatus
  recordPatching: boolean
  selectedRecordAssessment: AssetDecisionEvidenceAssessment | null
  recordMemberColumns: DataTableColumn<AssetDecisionRecordMember>[]
  onClose: () => void
  onSetRecordDetailPanel: (panel: RecordDetailPanel) => void
  onSubmitRecordStatus: (event: React.FormEvent<HTMLFormElement>) => void
  onSetRecordPatchStatus: (status: AssetDecisionRecordStatus) => void
  onOpenRecordSource: (detail: AssetDecisionRecordDetail) => void
  renderRecordExecutionBoard: (detail: AssetDecisionRecordDetail) => React.ReactNode
  renderRecordMemberFollowupRows: (detail: AssetDecisionRecordDetail) => React.ReactNode
}

export function RecordDetailModal({
  open,
  recordDetailState,
  recordDetailPanel,
  recordPatchError,
  recordPatchStatus,
  recordPatching,
  selectedRecordAssessment,
  recordMemberColumns,
  onClose,
  onSetRecordDetailPanel,
  onSubmitRecordStatus,
  onSetRecordPatchStatus,
  onOpenRecordSource,
  renderRecordExecutionBoard,
  renderRecordMemberFollowupRows,
}: RecordDetailModalProps) {
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={recordDetailState.detail?.title ?? '组合决策记录'}
      ariaLabel="资产组合决策记录详情"
      size="xl"
      contentClassName="asset-decision-record-modal"
    >
      {recordDetailState.loading ? (
        <PageStateView kind="loading" title="正在加载决策记录…" surface="empty" compact />
      ) : recordDetailState.error ? (
        <PageStateView
          kind="error"
          title="决策记录不可用"
          surface="empty"
          compact
        />
      ) : recordDetailState.detail ? (
        <div className="asset-decision-detail asset-decision-record-detail">
          <Tabs
            items={[
              { value: 'overview', label: '概览' },
              { value: 'execution', label: '执行', count: recordDetailState.detail.execution_plan?.actionable_count ?? 0 },
              { value: 'members', label: '成员', count: recordDetailState.detail.members.length },
              { value: 'source', label: '来源' },
            ]}
            value={recordDetailPanel}
            onChange={(value) => onSetRecordDetailPanel(value as RecordDetailPanel)}
          />
          {recordDetailPanel === 'overview' && renderDetailCommand({
            ariaLabel: '保存记录当前判断',
            title: '当前记录',
            summary: recordCoverSummary(recordDetailState.detail),
            footer: <span className="asset-decision-detail-command__context">{recordCoverMeta(recordDetailState.detail)}</span>,
            assessment: selectedRecordAssessment,
            insight: parseComparisonInsight(recordDetailState.detail.evidence_snapshot),
            chips: [],
          })}
          {recordPatchError && <div className="inline-alert danger">{recordPatchError}</div>}
          {recordDetailPanel === 'execution' && renderDetailPanel('执行跟进',
            <>
              <form className="asset-decision-record-status-form" onSubmit={onSubmitRecordStatus}>
                <label className="input-field">
                  <span>推进状态</span>
                  <select
                    aria-label="推进状态"
                    className="input"
                    value={recordPatchStatus}
                    onChange={(event) => onSetRecordPatchStatus(event.target.value as AssetDecisionRecordStatus)}
                  >
                    {RECORD_STATUS_OPTIONS.map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                </label>
                <button className="btn sm primary" type="submit" disabled={recordPatching || recordPatchStatus === recordDetailState.detail.status}>
                  {recordPatching ? '保存中…' : '更新状态'}
                </button>
              </form>
              {renderRecordExecutionBoard(recordDetailState.detail)}
            </>,
          )}
          {recordDetailPanel === 'source' && renderDetailPanel('来源复核',
            <section className="asset-decision-record-continuity" aria-label="决策记录来源连续性">
              <div>
                <strong>{recordSourceLabel(recordDetailState.detail)}</strong>
                <span>{recordSourceDetail(recordDetailState.detail)}</span>
              </div>
              <div className="asset-decision-record-continuity__state">
                <Badge variant="state" tone={readbackStatusTone(recordDetailState.detail.execution_readback?.status)}>
                  {recordDetailState.detail.execution_readback?.status ? READBACK_STATUS_LABELS[recordDetailState.detail.execution_readback.status] : '等待回读'}
                </Badge>
                <Badge variant="count" tone={recordDetailState.detail.execution_plan?.actionable_count > 0 ? 'maintenance' : 'normal'}>
                  可推进 {recordDetailState.detail.execution_plan?.actionable_count ?? 0}
                </Badge>
                <button className="btn sm secondary" type="button" onClick={() => onOpenRecordSource(recordDetailState.detail!)}>
                  复核来源
                </button>
              </div>
            </section>,
          )}
          {recordDetailPanel === 'members' && renderDetailPanel('成员跟进',
            renderRecordMemberFollowupRows(recordDetailState.detail),
          )}
          {recordDetailPanel === 'raw' && renderDetailPanel('成员底稿',
            <div className="asset-table-scroll" role="region" aria-label="决策记录成员" tabIndex={0}>
              <DataTable
                className="asset-table asset-decision-record-members-table"
                columns={recordMemberColumns}
                rows={recordDetailState.detail.members}
                rowKey={(member) => member.vps_id}
              />
            </div>,
            true,
          )}
        </div>
      ) : null}
    </Modal>
  )
}

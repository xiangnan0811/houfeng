import { Badge, type BadgeTone, MonoDigits } from '../../../../components/atoms'
import { PageState as PageStateView } from '../../../../components/PageState'
import type { AssetDecisionRecordSummary } from '../../../../lib/types'
import { READBACK_STATUS_LABELS, RECORD_STATUS_LABELS, VIEW_LABELS } from '../../constants'
import { readbackStatusTone, recordFollowupDoneCount, recordStatusTone } from '../../formatters'
import type { RecordsState } from '../../types'
import { localDay } from './workbenchFormat'
import { ScanName, ScanRow, WorkbenchPanel } from './WorkbenchPanel'

// 已对齐 / 待回读 / 不活跃是安静状态，不占视觉；需要复核的状态带计数；未知状态原样显示，不当作已对齐。
const QUIET_READBACK = new Set(['aligned', 'open', 'inactive'])

function readbackAttention(record: AssetDecisionRecordSummary): { label: string; tone: BadgeTone } | null {
  const readback = record.execution_readback
  if (!readback?.status || QUIET_READBACK.has(readback.status)) return null
  const tone = readbackStatusTone(readback.status)
  if (readback.status === 'drift') return { tone, label: `${READBACK_STATUS_LABELS.drift} ${readback.drift_count}` }
  if (readback.status === 'blocked') return { tone, label: `${READBACK_STATUS_LABELS.blocked} ${readback.blocked_count}` }
  if (readback.status === 'needs_evidence') return { tone, label: `${READBACK_STATUS_LABELS.needs_evidence} ${readback.needs_evidence_count}` }
  return { tone: 'neutral', label: String(readback.status) }
}

export function RecordsWorkbench({ recordsState, onOpenRecord }: { recordsState: RecordsState; onOpenRecord: (recordID: string) => void }) {
  return (
    <WorkbenchPanel title="保存记录" className="asset-workbench--records">
      {recordsState.loading ? (
        <PageStateView kind="loading" title="正在加载决策记录…" surface="empty" compact />
      ) : recordsState.error ? (
        <PageStateView kind="error" title="决策记录不可用" surface="empty" compact />
      ) : recordsState.records.length === 0 ? (
        <PageStateView kind="empty" title="尚未保存组合决策" surface="empty" compact />
      ) : (
        <ul className="asset-scan-list asset-scan-list--records" aria-label="已保存组合决策">
          {recordsState.records.map((record) => {
            const attention = readbackAttention(record)
            const open = () => onOpenRecord(record.record_id)
            return (
              <ScanRow key={record.record_id} clickable>
                <ScanName name={record.title} meta={VIEW_LABELS[record.source_view] ?? record.source_view} />
                <Badge variant="state" tone={recordStatusTone(record.status)}>{RECORD_STATUS_LABELS[record.status] ?? record.status}</Badge>
                {attention ? (
                  <Badge variant="state" tone={attention.tone}>{attention.label}</Badge>
                ) : <span aria-hidden="true" />}
                <span className="asset-scan-row__muted">
                  跟进 <MonoDigits>{recordFollowupDoneCount(record)}</MonoDigits>/<MonoDigits>{record.member_count}</MonoDigits>
                </span>
                <time className="asset-scan-row__muted mono tnum" dateTime={record.updated_at}>{localDay(record.updated_at)}</time>
                <span className="asset-scan-row__actions">
                  <button className="btn sm secondary" type="button" data-row-primary onClick={open}>查看</button>
                </span>
              </ScanRow>
            )
          })}
        </ul>
      )}
    </WorkbenchPanel>
  )
}

import { StatusBadge } from '../../components/StatusBadge'
import {
  type DataTableColumn,
  Hostname,
  MonoDigits,
  StatusGlyph,
  Timestamp,
} from '../../components/atoms'
import { formatLabelList } from '../../lib/format'
import type { TargetRecord, TargetSparklinesResponse } from '../../lib/types'
import { targetGlyphState } from './targetHelpers'
import { TargetsActionsCell } from './TargetsActionsCell'
import { TargetsLabelsCell } from './TargetsLabelsCell'
import { TargetsTrendCell } from './TargetsTrendCell'
import type { TargetRuntimeAction } from './types'

type BuildTargetsTableColumnsArgs = {
  sparklines: TargetSparklinesResponse | null
  metadataEditingTargetId: string | null
  metadataLabelInput: string
  metadataGroupInput: string
  metadataSavingTargetId: string | null
  metadataErrors: Record<string, string>
  runtimeBusyTargetId: string | null
  actionButtonRefs: { current: Record<string, HTMLButtonElement | null> }
  onMetadataGroupInputChange: (value: string) => void
  onMetadataLabelInputChange: (value: string) => void
  onSaveMetadata: (target: TargetRecord) => void
  onCancelMetadata: (targetId: string) => void
  onStartMetadataEdit: (target: TargetRecord) => void
  onRuntimeAction: (target: TargetRecord, action: TargetRuntimeAction) => void
}

export function buildTargetsTableColumns({
  sparklines,
  metadataEditingTargetId,
  metadataLabelInput,
  metadataGroupInput,
  metadataSavingTargetId,
  metadataErrors,
  runtimeBusyTargetId,
  actionButtonRefs,
  onMetadataGroupInputChange,
  onMetadataLabelInputChange,
  onSaveMetadata,
  onCancelMetadata,
  onStartMetadataEdit,
  onRuntimeAction,
}: BuildTargetsTableColumnsArgs): DataTableColumn<TargetRecord>[] {
  return [
    {
      key: 'glyph',
      label: '',
      width: 32,
      align: 'center',
      render: (target) => (
        <StatusGlyph
          state={targetGlyphState(target)}
          size="md"
          ariaLabel={`${target.name} 健康 ${target.current_health_status}`}
        />
      ),
    },
    {
      key: 'identity',
      label: '目标',
      render: (target) => (
        <div className="targets-table__identity">
          <span className="targets-table__name">{target.name}</span>
          <Hostname truncate maxChars={14} className="targets-table__id">
            {target.target_id}
          </Hostname>
          <span className="targets-table__freshness">
            成功 <Timestamp value={target.last_success_at ?? null} mode="relative" />
            {' '}· 失败 <Timestamp value={target.last_failure_at ?? null} mode="relative" />
          </span>
        </div>
      ),
    },
    {
      key: 'type',
      label: '类型',
      render: (target) => <span className="targets-table__type">{target.target_type}</span>,
    },
    {
      key: 'host',
      label: 'Host',
      render: (target) => {
        const hostDisplay = target.base_port ? `${target.host}:${target.base_port}` : target.host

        return (
          <span className="targets-table__host">
            {target.group ? <span className="targets-table__group">{target.group} · </span> : null}
            <Hostname>{hostDisplay}</Hostname>
          </span>
        )
      },
    },
    {
      key: 'labels',
      label: '标签',
      render: (target) => (
        <TargetsLabelsCell
          target={target}
          editing={metadataEditingTargetId === target.target_id}
          metadataGroupInput={metadataGroupInput}
          metadataLabelInput={metadataLabelInput}
          metadataSavingTargetId={metadataSavingTargetId}
          metadataError={metadataErrors[target.target_id]}
          onGroupInputChange={onMetadataGroupInputChange}
          onLabelInputChange={onMetadataLabelInputChange}
          onSaveMetadata={onSaveMetadata}
          onCancelMetadata={onCancelMetadata}
        />
      ),
    },
    {
      key: 'status',
      label: '状态',
      render: (target) => (
        <span className="targets-table__status">
          <StatusBadge label={target.run_status} />
          <StatusBadge label={target.current_health_status} />
          {target.execution_node_labels.length > 0 ? (
            <span className="targets-table__exec-labels">
              {formatLabelList(target.execution_node_labels)}
            </span>
          ) : null}
        </span>
      ),
    },
    {
      key: 'trends',
      label: '近 24h',
      cellClassName: 'targets-table__trends',
      render: (target) => <TargetsTrendCell target={target} sparklines={sparklines} />,
    },
    {
      key: 'issue',
      label: '当前主问题',
      render: (target) => (
        <div className="targets-table__issue">
          <MonoDigits className="targets-table__issue-count">
            {target.current_active_incident_count}
          </MonoDigits>
          <span className="targets-table__issue-summary">
            {target.current_primary_issue_summary || '暂无明显异常'}
          </span>
        </div>
      ),
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      cellClassName: 'targets-table__actions-cell',
      render: (target) => (
        <TargetsActionsCell
          target={target}
          metadataEditingTargetId={metadataEditingTargetId}
          metadataSavingTargetId={metadataSavingTargetId}
          runtimeBusyTargetId={runtimeBusyTargetId}
          actionButtonRefs={actionButtonRefs}
          onStartMetadataEdit={onStartMetadataEdit}
          onRuntimeAction={onRuntimeAction}
        />
      ),
    },
  ]
}

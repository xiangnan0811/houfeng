import { ActionConfirmationModal } from '../ActionConfirmationModal'
import { PageState } from '../PageState'
import { StatusBadge } from '../StatusBadge'
import {
  DataTable,
  type DataTableColumn,
  MonoDigits,
  Timestamp,
} from '../atoms'
import { formatConfigSummary, formatLatency } from '../../lib/format'
import type { ProbeItemRecord, ProbeObservation } from '../../lib/types'
import { probeItemObservationEmptyCopy } from './probeObservationGap'

export type PendingProbeConfirmation = {
  probeItemId: string
  action: 'delete'
}

function probeActionAccessibleName(action: string, probeItem: ProbeItemRecord): string {
  return `${action} 探测项 ${probeItem.probe_item_id} ${probeItem.probe_kind.toUpperCase()} ${formatConfigSummary(probeItem.config)}`
}

function latestObservation(observations: ProbeObservation[]): ProbeObservation | null {
  const [first, ...rest] = observations
  if (!first) return null
  return rest.reduce(
    (latest, observation) =>
      new Date(observation.observed_at).getTime() > new Date(latest.observed_at).getTime()
        ? observation
        : latest,
    first,
  )
}

function resultKindLabel(kind: string): { label: string; raw: string | null } {
  if (kind === 'success') return { label: '成功', raw: null }
  if (kind === 'failure') return { label: '失败', raw: null }
  if (kind === 'timeout') return { label: '超时', raw: null }
  return { label: '结果未知', raw: kind }
}

function ProbeLatestResult({
  observation,
  enabled,
}: {
  observation: ProbeObservation | null
  enabled: boolean
}) {
  if (!observation) {
    return (
      <span className="target-probe-table__muted">
        {probeItemObservationEmptyCopy(enabled).body}
      </span>
    )
  }
  const errorText = observation.error_summary?.trim() || observation.error_code?.trim() || ''
  const result = resultKindLabel(observation.result_kind)
  const diagnostics = [result.raw, errorText].filter(Boolean)
  return (
    <span className="target-probe-table__result">
      <span>{result.label}</span>
      {observation.latency_ms != null ? (
        <>
          <span aria-hidden>·</span>
          <MonoDigits>{formatLatency(observation.latency_ms)}</MonoDigits>
        </>
      ) : null}
      {observation.http_status != null ? (
        <>
          <span aria-hidden>·</span>
          <MonoDigits>{observation.http_status}</MonoDigits>
        </>
      ) : observation.tls_expiry_days != null ? (
        <>
          <span aria-hidden>·</span>
          <MonoDigits>{observation.tls_expiry_days} 天</MonoDigits>
        </>
      ) : null}
      {diagnostics.length > 0 ? (
        <details className="target-probe-table__diagnostic">
          <summary>诊断信息</summary>
          {diagnostics.map((detail, index) => <p key={`${index}-${detail}`}>{detail}</p>)}
        </details>
      ) : null}
    </span>
  )
}

type TargetProbeListProps = {
  probeItems: ProbeItemRecord[]
  observationsByProbe: Map<string, ProbeObservation[]>
  actionsDisabled: boolean
  pendingProbeConfirmation: PendingProbeConfirmation | null
  confirmationCardDisabled: boolean
  registerDeleteButtonRef: (probeItemId: string, element: HTMLButtonElement | null) => void
  onEdit: (probeItem: ProbeItemRecord) => void
  onToggle: (probeItem: ProbeItemRecord) => void
  onDelete: (probeItem: ProbeItemRecord) => void
  onConfirmDelete: (probeItem: ProbeItemRecord) => void
  onCancelDeleteConfirmation: (probeItem: ProbeItemRecord) => void
  onAddProbe?: () => void
  hideActions?: boolean
}

export function TargetProbeList({
  probeItems,
  observationsByProbe,
  actionsDisabled,
  pendingProbeConfirmation,
  confirmationCardDisabled,
  registerDeleteButtonRef,
  onEdit,
  onToggle,
  onDelete,
  onConfirmDelete,
  onCancelDeleteConfirmation,
  onAddProbe,
  hideActions = false,
}: TargetProbeListProps) {
  if (probeItems.length === 0) {
    return (
      <PageState
        kind="empty"
        surface="empty"
        title="目标尚未配置探测项"
        description="请为该入口添加至少一种观测方式。"
        action={
          onAddProbe && !hideActions ? (
            <button type="button" className="btn md primary" onClick={() => onAddProbe()}>
              添加探测项
            </button>
          ) : null
        }
      />
    )
  }

  const pendingItem = pendingProbeConfirmation
    ? probeItems.find((item) => item.probe_item_id === pendingProbeConfirmation.probeItemId) ?? null
    : null

  const columns: DataTableColumn<ProbeItemRecord>[] = [
    {
      key: 'method',
      label: '方式',
      render: (probeItem) => (
        <div className="target-probe-table__method">
          <span className="target-probe-table__kind">{probeItem.probe_kind.toUpperCase()}</span>
          <span className="target-probe-table__config">{formatConfigSummary(probeItem.config)}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '状态',
      render: (probeItem) => <StatusBadge label={probeItem.enabled ? '启用' : '停用'} />,
    },
    {
      key: 'frequency',
      label: '频率',
      render: (probeItem) => <MonoDigits>{probeItem.frequency_tier}</MonoDigits>,
    },
    {
      key: 'latest-result',
      label: '最近结果',
      render: (probeItem) => (
        <ProbeLatestResult
          observation={latestObservation(observationsByProbe.get(probeItem.probe_item_id) ?? [])}
          enabled={probeItem.enabled}
        />
      ),
    },
    {
      key: 'latest-at',
      label: '最近观测',
      render: (probeItem) => {
        const observation = latestObservation(observationsByProbe.get(probeItem.probe_item_id) ?? [])
        if (observation) return <Timestamp value={observation.observed_at} mode="relative" />
        return <span className="target-probe-table__muted">—</span>
      },
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      cellClassName: 'target-probe-table__actions-cell',
      render: (probeItem) => (
        <div className="target-probe-table__actions">
          <button
            type="button"
            className="btn sm secondary"
            aria-label={probeActionAccessibleName('编辑', probeItem)}
            disabled={actionsDisabled}
            onClick={() => onEdit(probeItem)}
          >
            编辑
          </button>
          <button
            type="button"
            className="btn sm secondary"
            aria-label={probeActionAccessibleName(
              probeItem.enabled ? '停用' : '启用',
              probeItem,
            )}
            disabled={actionsDisabled}
            onClick={() => onToggle(probeItem)}
          >
            {probeItem.enabled ? '停用' : '启用'}
          </button>
          <button
            ref={(element) => {
              registerDeleteButtonRef(probeItem.probe_item_id, element)
            }}
            type="button"
            className="btn sm danger"
            aria-label={probeActionAccessibleName('删除', probeItem)}
            disabled={actionsDisabled}
            onClick={() => onDelete(probeItem)}
          >
            删除
          </button>
        </div>
      ),
    },
  ]

  return (
    <>
      <div className="target-probe-table-wrap">
        <DataTable<ProbeItemRecord>
          className="target-probe-table"
          density="compact"
          columns={hideActions ? columns.filter((column) => column.key !== 'actions') : columns}
          rows={probeItems}
          rowKey={(item) => item.probe_item_id}
        />
      </div>
      {pendingItem ? (
        <ActionConfirmationModal
          open
          title="确认删除探测项"
          current="当前：这条探测项仍属于当前目标。"
          result="操作后：这条观测方式会被移除。"
          impact="仅用于误建场景。删除后该探测项不再产生新的观测记录。"
          unchanged="不会删除目标，也不会删除既有事件或历史观测记录。"
          confirmLabel="确认删除探测项"
          disabled={confirmationCardDisabled}
          onConfirm={() => onConfirmDelete(pendingItem)}
          onCancel={() => onCancelDeleteConfirmation(pendingItem)}
        >
          <p>{formatConfigSummary(pendingItem.config)}</p>
        </ActionConfirmationModal>
      ) : null}
    </>
  )
}

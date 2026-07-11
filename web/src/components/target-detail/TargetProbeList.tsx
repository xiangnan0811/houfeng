import { ActionConfirmationModal } from '../ActionConfirmationModal'
import { PageState } from '../PageState'
import { StatusBadge } from '../StatusBadge'
import {
  DataTable,
  type DataTableColumn,
  Hostname,
  MonoDigits,
  StatusGlyph,
  Timestamp,
} from '../atoms'
import { formatConfigSummary, formatLatency } from '../../lib/format'
import type { ProbeItemRecord, ProbeObservation } from '../../lib/types'

export type PendingProbeConfirmation = {
  probeItemId: string
  action: 'delete'
}

function probeActionAccessibleName(action: string, probeItem: ProbeItemRecord): string {
  return `${action} ProbeItem ${probeItem.probe_item_id} ${probeItem.probe_kind.toUpperCase()} ${formatConfigSummary(probeItem.config)}`
}

const observationColumns: DataTableColumn<ProbeObservation>[] = [
  {
    key: 'glyph',
    label: '',
    width: 36,
    cellClassName: 'probe-observations__glyph-cell',
    render: (obs) => (
      <StatusGlyph
        state={obs.result_kind === 'success' ? 'normal' : 'critical'}
        size="md"
        ariaLabel={obs.result_kind === 'success' ? '成功' : '失败'}
      />
    ),
  },
  {
    key: 'monitoring-instance',
    label: '执行监控实例',
    render: (obs) => (
      <Hostname truncate maxChars={14}>
        {obs.monitoring_instance_id}
      </Hostname>
    ),
  },
  {
    key: 'time',
    label: '观测时间',
    render: (obs) => <Timestamp value={obs.observed_at} mode="relative" />,
  },
  {
    key: 'latency',
    label: '延迟',
    align: 'right',
    render: (obs) =>
      obs.latency_ms != null ? (
        <MonoDigits>{formatLatency(obs.latency_ms)}</MonoDigits>
      ) : (
        <span className="probe-observations__muted">—</span>
      ),
  },
  {
    key: 'meta',
    label: 'HTTP / TLS',
    align: 'right',
    render: (obs) => {
      if (obs.http_status != null) return <MonoDigits>{obs.http_status}</MonoDigits>
      if (obs.tls_expiry_days != null)
        return <MonoDigits>{obs.tls_expiry_days} 天</MonoDigits>
      return <span className="probe-observations__muted">—</span>
    },
  },
  {
    key: 'error',
    label: '错误摘要',
    render: (obs) =>
      obs.error_summary || obs.error_code ? (
        <span
          className="probe-observations__error"
          title={obs.error_summary ?? obs.error_code ?? ''}
        >
          {obs.error_summary ?? obs.error_code}
        </span>
      ) : (
        <span className="probe-observations__muted">—</span>
      ),
  },
]

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
}: TargetProbeListProps) {
  if (probeItems.length === 0) {
    return (
      <PageState
        kind="empty"
        surface="empty"
        title="目标尚未配置 ProbeItem"
        description="请为该入口添加至少一种观测方式。"
        action={
          onAddProbe ? (
            <button type="button" className="btn md primary" onClick={() => onAddProbe()}>
              添加 Probe
            </button>
          ) : null
        }
      />
    )
  }

  return (
    <div className="probe-list">
      {probeItems.map((probeItem) => {
        const observations = observationsByProbe.get(probeItem.probe_item_id) ?? []
        const latestObservedAt = observations[0]?.observed_at ?? null
        return (
          <article key={probeItem.probe_item_id} className="probe-card">
            <header className="probe-card__header">
              <div>
                <h3>{probeItem.probe_kind.toUpperCase()}</h3>
                <p>{formatConfigSummary(probeItem.config)}</p>
              </div>
              <div className="badge-row">
                <StatusBadge label={probeItem.enabled ? '启用' : '停用'} />
                <StatusBadge label={probeItem.frequency_tier} tone="cyan" />
              </div>
            </header>

            <div className="badge-row badge-row--wrap">
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
            {pendingProbeConfirmation?.probeItemId === probeItem.probe_item_id ? (
              <ActionConfirmationModal
                open
                title="确认删除 ProbeItem"
                current="当前：这条 ProbeItem 仍属于当前目标。"
                result="操作后：这条观测方式会被移除。"
                impact="仅用于误建场景。删除后该 ProbeItem 不再产生新的观测记录。"
                unchanged="不会删除目标，也不会删除既有事件或历史观测记录。"
                confirmLabel="确认删除 ProbeItem"
                disabled={confirmationCardDisabled}
                onConfirm={() => onConfirmDelete(probeItem)}
                onCancel={() => onCancelDeleteConfirmation(probeItem)}
              >
                <p>{formatConfigSummary(probeItem.config)}</p>
              </ActionConfirmationModal>
            ) : null}

            <dl className="probe-card__meta">
              <div>
                <dt>超时</dt>
                <dd>
                  <MonoDigits>{probeItem.timeout_seconds}</MonoDigits>s
                </dd>
              </div>
              <div>
                <dt>最近观测</dt>
                <dd>
                  {latestObservedAt ? (
                    <Timestamp value={latestObservedAt} mode="both" />
                  ) : (
                    <span className="probe-observations__muted">尚无观测结果</span>
                  )}
                </dd>
              </div>
            </dl>

            {observations.length > 0 ? (
              <DataTable<ProbeObservation>
                className="probe-observations"
                density="compact"
                columns={observationColumns}
                rows={observations}
                rowKey={(obs) => `${obs.probe_item_id}-${obs.monitoring_instance_id}-${obs.observed_at}`}
              />
            ) : (
              <div className="probe-card__observations-empty">尚未收到观测</div>
            )}
          </article>
        )
      })}
    </div>
  )
}

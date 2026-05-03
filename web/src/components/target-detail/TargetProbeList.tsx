import type { RefObject } from 'react'

import { ActionConfirmationCard } from '../ActionConfirmationCard'
import { StatusBadge } from '../StatusBadge'
import { formatConfigSummary, formatDateTime, formatLatency } from '../../lib/format'
import type { ProbeItemRecord, ProbeObservation } from '../../lib/types'

export type PendingProbeConfirmation = {
  probeItemId: string
  action: 'delete'
}

function probeActionAccessibleName(action: string, probeItem: ProbeItemRecord): string {
  return `${action} ProbeItem ${probeItem.probe_item_id} ${probeItem.probe_kind.toUpperCase()} ${formatConfigSummary(probeItem.config)}`
}

type TargetProbeListProps = {
  probeItems: ProbeItemRecord[]
  observationsByProbe: Map<string, ProbeObservation[]>
  actionsDisabled: boolean
  pendingProbeConfirmation: PendingProbeConfirmation | null
  confirmationCardDisabled: boolean
  pendingProbeConfirmationCardRef: RefObject<HTMLDivElement | null>
  registerDeleteButtonRef: (probeItemId: string, element: HTMLButtonElement | null) => void
  onEdit: (probeItem: ProbeItemRecord) => void
  onToggle: (probeItem: ProbeItemRecord) => void
  onDelete: (probeItem: ProbeItemRecord) => void
  onConfirmDelete: (probeItem: ProbeItemRecord) => void
  onCancelDeleteConfirmation: (probeItem: ProbeItemRecord) => void
}

export function TargetProbeList({
  probeItems,
  observationsByProbe,
  actionsDisabled,
  pendingProbeConfirmation,
  confirmationCardDisabled,
  pendingProbeConfirmationCardRef,
  registerDeleteButtonRef,
  onEdit,
  onToggle,
  onDelete,
  onConfirmDelete,
  onCancelDeleteConfirmation,
}: TargetProbeListProps) {
  if (probeItems.length === 0) {
    return (
      <div className="empty-state">
        <h3>当前还没有 ProbeItem</h3>
        <p>当前还没有 ProbeItem，请为该入口添加至少一种观测方式。</p>
      </div>
    )
  }

  return (
    <div className="probe-list">
      {probeItems.map((probeItem) => {
        const observations = observationsByProbe.get(probeItem.probe_item_id) ?? []
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
                aria-label={probeActionAccessibleName('编辑', probeItem)}
                disabled={actionsDisabled}
                onClick={() => onEdit(probeItem)}
              >
                编辑
              </button>
              <button
                type="button"
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
                aria-label={probeActionAccessibleName('删除', probeItem)}
                disabled={actionsDisabled}
                onClick={() => onDelete(probeItem)}
              >
                删除
              </button>
            </div>
            {pendingProbeConfirmation?.probeItemId === probeItem.probe_item_id ? (
              <div ref={pendingProbeConfirmationCardRef}>
                <ActionConfirmationCard
                  title="确认删除 ProbeItem"
                  current="当前：这条 ProbeItem 仍属于当前目标。"
                  result="操作后：这条观测方式会被移除。"
                  impact="仅用于误建场景。删除后该 ProbeItem 不再产生新的观测记录。"
                  unchanged="不会删除目标，也不会删除既有事件或历史观测记录。"
                  confirmLabel="确认删除 ProbeItem"
                  disabled={confirmationCardDisabled}
                  onConfirm={() => onConfirmDelete(probeItem)}
                  onCancel={() => onCancelDeleteConfirmation(probeItem)}
                />
              </div>
            ) : null}

            <dl className="probe-card__meta">
              <div>
                <dt>超时</dt>
                <dd>{probeItem.timeout_seconds}s</dd>
              </div>
              <div>
                <dt>最近观测</dt>
                <dd>
                  {observations.length > 0
                    ? formatDateTime(observations[0].observed_at)
                    : '尚无观测结果'}
                </dd>
              </div>
            </dl>

            {observations.length > 0 ? (
              <div className="observation-list">
                {observations.map((observation) => (
                  <div
                    key={`${observation.probe_item_id}-${observation.node_id}`}
                    className="observation-row"
                  >
                    <div>
                      <strong>{observation.node_id}</strong>
                      <p>{formatDateTime(observation.observed_at)}</p>
                    </div>
                    <div>
                      <StatusBadge
                        label={observation.result_kind === 'success' ? '成功' : '失败'}
                        tone={observation.result_kind === 'success' ? 'green' : 'red'}
                      />
                    </div>
                    <div>
                      <span>延迟</span>
                      <strong>{formatLatency(observation.latency_ms)}</strong>
                    </div>
                    <div>
                      <span>HTTP / TLS</span>
                      <strong>
                        {observation.http_status ?? observation.tls_expiry_days ?? '—'}
                      </strong>
                    </div>
                    <div>
                      <span>错误摘要</span>
                      <strong>
                        {observation.error_summary || observation.error_code || '—'}
                      </strong>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="empty-inline">尚无观测结果</div>
            )}
          </article>
        )
      })}
    </div>
  )
}

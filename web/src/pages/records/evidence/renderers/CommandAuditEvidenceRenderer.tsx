import { useId } from 'react'

import { Badge, type BadgeTone, Timestamp } from '../../../../components/atoms'
import { lookup, toneOf } from '../evidencePresentation'
import type { CommandAuditEvidenceReadModel } from '../evidenceReadModels'

type Props = {
  model: CommandAuditEvidenceReadModel
}

const OUTCOME_LABELS: Record<string, string> = {
  queued: '已排队',
  dispatched: '已下发',
  succeeded: '成功',
  failed: '失败',
  rejected: '已拒绝',
}

const OUTCOME_TONES: Record<string, BadgeTone> = {
  succeeded: 'normal',
  failed: 'critical',
  rejected: 'alert',
}

const SOURCE_LABELS: Record<string, string> = {
  web: '网页',
  agent_sync: 'Agent 回传',
}

export function CommandAuditEvidenceRenderer({ model }: Props) {
  const titleId = useId()
  return (
    <section className="record-section record-evidence__body" aria-labelledby={titleId}>
      <div className="record-section__head">
        <h2 className="record-section__title" id={titleId}>
          命令审计 <span className="record-count">{model.audit_count}</span>
        </h2>
      </div>
      <ol className="record-evidence__timeline">
        {model.audits.map((audit) => (
          <li key={audit.audit_id} className="record-evidence__event">
            <Timestamp value={audit.occurred_at} className="record-evidence__event-time" />
            <div className="record-evidence__event-body">
              <strong className="mono">{audit.command_id}</strong>
              <span className="record-evidence__event-meta">
                <Badge variant="info" tone={toneOf(OUTCOME_TONES, audit.outcome)}>{lookup(OUTCOME_LABELS, audit.outcome)}</Badge>
                {audit.exit_code === undefined || audit.exit_code === 0 ? null : <span className="mono">退出码 {audit.exit_code}</span>}
                <span>{lookup(SOURCE_LABELS, audit.source)}</span>
                {audit.actor_display_name || audit.actor_username
                  ? <span>{audit.actor_display_name || audit.actor_username}</span>
                  : null}
                {audit.monitoring_instance_name ? <span>{audit.monitoring_instance_name}</span> : null}
              </span>
            </div>
          </li>
        ))}
      </ol>
    </section>
  )
}
